package inference

import (
	"context"
	"encoding/json"
	"fmt"

	"strings"

	"github.com/buzzhpc/buzz-cli/internal/app"
	"github.com/buzzhpc/buzz-cli/internal/client"
	"github.com/buzzhpc/buzz-cli/internal/cmdutil"
	"github.com/buzzhpc/buzz-cli/internal/output"
	"github.com/spf13/cobra"
)

func NewCmd(a app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "inference",
		Aliases: []string{"llm", "vllm", "ai"},
		Short:   "Manage LLM Inference endpoints",
	}
	cmd.AddCommand(
		newCreateCmd(a),
		newListCmd(a),
		newGetCmd(a),
		newDeleteCmd(a),
	)
	return cmd
}

func newCreateCmd(a app.App) *cobra.Command {
	var name, sku, nodeType, modelID, hfToken, extraArgs string
	var gpuCount int
	var noDeploy bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and deploy an LLM inference endpoint",
		Example: `  buzz llm create --name my-llm --model meta-llama/Llama-3.1-8B-Instruct
  buzz inference create --name my-llm --model facebook/opt-125m
  buzz llm create --name gated-model --model meta-llama/Llama-3.1-8B-Instruct --hf-token hf_xxx
  buzz llm create --name my-llm --model facebook/opt-125m --no-deploy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			output.Info(fmt.Sprintf("Creating LLM inference %q (model: %s)...", name, modelID))
			res := &client.CommonResource{
				APIVersion: "paas.envmgmt.io/v1",
				Kind:       "Service",
				Metadata:   client.Metadata{Name: name, Project: ref.Project},
				Spec: mustMarshal(map[string]interface{}{
					"serviceProfile": map[string]interface{}{"name": sku, "systemCatalog": true},
					"variables": []map[string]string{
						{"name": "KeyX", "valueType": "text", "value": modelID},
						{"name": "KeyAlpha", "valueType": "text", "value": hfToken},
						{"name": "KeyY", "valueType": "text", "value": extraArgs},
						{"name": "GPU Count", "valueType": "text", "value": fmt.Sprintf("%d", gpuCount)},
						{"name": "Node Type", "valueType": "text", "value": nodeType},
					},
				}),
			}
			path := client.ServicePath(ref.Project, ref.Name, "") + "?fail-on-exists=true"
			if _, err := a.Client().Post(context.Background(), path, res); err != nil {
				return err
			}
			if noDeploy {
				output.Success(fmt.Sprintf("Inference endpoint %q created (not deployed)", name))
				return nil
			}
			output.Info(fmt.Sprintf("Deploying inference endpoint %q...", name))
			if err := a.Client().PublishService(context.Background(), ref.Project, ref.Name, name); err != nil {
				return fmt.Errorf("created but deploy failed: %w", err)
			}
			output.Success(fmt.Sprintf("Inference endpoint %q deployed.", name))
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "Name of the inference endpoint (required)")
	cmd.Flags().StringVarP(&modelID, "model", "m", "facebook/opt-125m", "HuggingFace model ID")
	cmd.Flags().StringVar(&sku, "sku", "inference-vllm-v1", "SKU: inference-vllm-v1 (H200) or inference-vllm-v1-h100 (A40/H100)")
	cmd.Flags().StringVar(&nodeType, "node-type", "H200", "GPU node type")
	cmd.Flags().IntVar(&gpuCount, "gpu-count", 1, "Number of GPUs (use >1 for tensor parallelism)")
	cmd.Flags().StringVar(&hfToken, "hf-token", "", "HuggingFace token for gated/private models")
	cmd.Flags().StringVar(&extraArgs, "extra-args", "", "Extra vLLM CLI args (e.g. '--max-model-len 8192')")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "Create resource without deploying it")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newListCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all LLM inference endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := a.WorkspaceRefs(cmd.Context())
			if err != nil || len(refs) == 0 {
				output.Info("No inference endpoints found.")
				return nil
			}
			results, err := a.Client().ListAcrossWorkspaces(cmd.Context(), refs, func(project, ws string) string {
				return client.ServicePath(project, ws, "")
			})
			if err != nil {
				return err
			}
			var rows [][]string
			for _, r := range results {
				filtered := client.FilterBySKU(r.Items, client.InferenceSKUs)
				rows = append(rows, parseRows(filtered, r.Project, r.Workspace)...)
			}
			if len(rows) == 0 {
				output.Info("No inference endpoints found.")
				return nil
			}
			output.Table([]string{"NAME", "PROJECT", "WORKSPACE", "STATUS"}, rows)
			return nil
		},
	}
}

func newGetCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "get <name>",
		Aliases: []string{"describe", "show"},
		Short:   "Get details of an inference endpoint",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			b, err := a.Client().Get(context.Background(), client.ServicePath(ref.Project, ref.Name, args[0]))
			if err != nil {
				return err
			}
			var res client.CommonResource
			json.Unmarshal(b, &res)

			vars := extractVars(res.Spec)
			endpointURL, endpointToken := extractGenericOutputFields(res.Status)

			rows := [][]string{
				{"Name", res.Metadata.Name},
				{"Project", ref.Project},
				{"Workspace", ref.Name},
				{"Status", output.StatusColor(output.ExtractStatus(res.Status))},
				{"SKU", extractProfileName(res.Spec)},
				{"GPU Type", vars["Node Type"]},
				{"GPU Count", vars["GPU Count"]},
			}
			// Model ID is stored under variable key "KeyX"
			if m := vars["KeyX"]; m != "" {
				rows = append(rows, []string{"Model", m})
			}
			if endpointURL != "" {
				rows = append(rows, []string{"Endpoint URL", endpointURL})
			}
			if endpointToken != "" {
				rows = append(rows, []string{"API Token", endpointToken})
			}
			output.Table([]string{"FIELD", "VALUE"}, rows)
			return nil
		},
	}
}

func newDeleteCmd(a app.App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"destroy", "rm"},
		Short:   "Delete an LLM inference endpoint",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			if !force {
				fmt.Printf("Delete inference endpoint %q? [y/N] ", args[0])
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					output.Info("Cancelled.")
					return nil
				}
			}
			if err := a.Client().Delete(context.Background(), client.ServicePath(ref.Project, ref.Name, args[0])); err != nil {
				return err
			}
			output.Success(fmt.Sprintf("Inference endpoint %q deleted.", args[0]))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}

func parseRows(items []json.RawMessage, project, workspace string) [][]string {
	var rows [][]string
	for _, raw := range items {
		var res client.CommonResource
		if err := json.Unmarshal(raw, &res); err != nil {
			continue
		}
		rows = append(rows, []string{res.Metadata.Name, project, workspace, output.StatusColor(output.ExtractStatus(res.Status))})
	}
	return rows
}

func extractVars(spec json.RawMessage) map[string]string {
	var s struct {
		Variables []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"variables"`
	}
	if err := json.Unmarshal(spec, &s); err != nil {
		var str string
		if json.Unmarshal(spec, &str) == nil {
			json.Unmarshal([]byte(str), &s)
		}
	}
	m := make(map[string]string)
	for _, v := range s.Variables {
		if v.Value != "" {
			m[v.Name] = v.Value
		}
	}
	return m
}

func extractProfileName(spec json.RawMessage) string {
	var s struct {
		ComputeProfile struct{ Name string `json:"name"` } `json:"computeProfile"`
		ServiceProfile struct{ Name string `json:"name"` } `json:"serviceProfile"`
	}
	if err := json.Unmarshal(spec, &s); err != nil {
		var str string
		if json.Unmarshal(spec, &str) == nil {
			json.Unmarshal([]byte(str), &s)
		}
	}
	if s.ComputeProfile.Name != "" {
		return s.ComputeProfile.Name
	}
	return s.ServiceProfile.Name
}

// extractGenericOutputFields walks output tasks looking for URL/token-like fields.
func extractGenericOutputFields(status json.RawMessage) (endpointURL, token string) {
	var raw struct {
		Output map[string]struct {
			Tasks map[string]map[string]struct {
				Value string `json:"value"`
			} `json:"tasks"`
		} `json:"output"`
	}
	if err := json.Unmarshal(status, &raw); err != nil {
		return
	}
	for _, res := range raw.Output {
		for _, task := range res.Tasks {
			for k, v := range task {
				lower := strings.ToLower(k)
				if endpointURL == "" && (strings.Contains(lower, "url") || strings.Contains(lower, "endpoint") || strings.Contains(lower, "host")) {
					endpointURL = v.Value
				}
				if token == "" && (strings.Contains(lower, "token") || strings.Contains(lower, "key")) {
					token = v.Value
				}
			}
		}
	}
	return
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
