package notebook

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/buzzhpc/buzz-cli/internal/app"
	"github.com/buzzhpc/buzz-cli/internal/client"
	"github.com/buzzhpc/buzz-cli/internal/cmdutil"
	"github.com/buzzhpc/buzz-cli/internal/output"
	"github.com/spf13/cobra"
)

func NewCmd(a app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "notebook",
		Aliases: []string{"nb", "jupyter"},
		Short:   "Manage Jupyter Notebooks",
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
	var name, sku, nodeType, image string
	var gpuCount int
	var noDeploy, wait bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and deploy a Jupyter Notebook",
		Example: `  buzz notebook create --name my-nb
  buzz jupyter create --name my-nb --node-type H100 --gpu-count 2 --image jupyter/scipy-notebook:latest
  buzz notebook create --name my-nb --no-deploy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			output.Info(fmt.Sprintf("Creating Notebook %q in %s/%s...", name, ref.Project, ref.Name))
			res := &client.CommonResource{
				APIVersion: "paas.envmgmt.io/v1",
				Kind:       "Service",
				Metadata:   client.Metadata{Name: name, Project: ref.Project},
				Spec: mustMarshal(map[string]interface{}{
					"serviceProfile": map[string]interface{}{"name": sku, "systemCatalog": true},
					"variables": []map[string]string{
						{"name": "GPU Count", "valueType": "text", "value": fmt.Sprintf("%d", gpuCount)},
						{"name": "Node Type", "valueType": "text", "value": nodeType},
						{"name": "Pod Image", "valueType": "text", "value": image},
					},
				}),
			}
			path := client.ServicePath(ref.Project, ref.Name, "") + "?fail-on-exists=true"
			if _, err := a.Client().Post(context.Background(), path, res); err != nil {
				return err
			}
			if noDeploy {
				output.Success(fmt.Sprintf("Notebook %q created (not deployed)", name))
				return nil
			}
			output.Info(fmt.Sprintf("Deploying Notebook %q...", name))
			if err := a.Client().PublishService(context.Background(), ref.Project, ref.Name, name); err != nil {
				return fmt.Errorf("created but deploy failed: %w", err)
			}
			output.Success(fmt.Sprintf("Notebook %q deployed.", name))
			output.Info(fmt.Sprintf("Access at: https://%s.notebook.buzzperformancecloud.com", name))
			if wait {
				return cmdutil.WaitForReady(context.Background(), a.Client(), client.ServicePath(ref.Project, ref.Name, name), fmt.Sprintf("Notebook %q", name))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "Name of the notebook — also sets the URL subdomain (required)")
	cmd.Flags().StringVar(&sku, "sku", "jupyter-notebook-v4-ca-qc-2", "SKU: jupyter-notebook-v4-ca-qc-2 (H200) or jupyter-notebook-v4 (A40/H100)")
	cmd.Flags().StringVar(&nodeType, "node-type", "H200", "GPU node type")
	cmd.Flags().IntVar(&gpuCount, "gpu-count", 1, "Number of GPUs")
	cmd.Flags().StringVar(&image, "image", "jupyter/minimal-notebook:latest", "Jupyter container image")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "Create resource without deploying it")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for resource to be ready after deploying")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newListCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Jupyter Notebooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := a.WorkspaceRefs(cmd.Context())
			if err != nil || len(refs) == 0 {
				output.Info("No notebooks found.")
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
				filtered := client.FilterBySKU(r.Items, client.NotebookSKUs)
				for _, raw := range filtered {
					var res client.CommonResource
					if err := json.Unmarshal(raw, &res); err != nil {
						continue
					}
					rows = append(rows, []string{
						res.Metadata.Name,
						r.Project,
						r.Workspace,
						output.StatusColor(output.ExtractStatus(res.Status)),
						fmt.Sprintf("https://%s.notebook.buzzperformancecloud.com", res.Metadata.Name),
					})
				}
			}
			if len(rows) == 0 {
				output.Info("No notebooks found.")
				return nil
			}
			output.Table([]string{"NAME", "PROJECT", "WORKSPACE", "STATUS", "URL"}, rows)
			return nil
		},
	}
}

func newGetCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "get <name>",
		Aliases: []string{"describe", "show"},
		Short:   "Get details of a Notebook",
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
			accessURL, token := extractNotebookStatus(res.Status)
			if accessURL == "" {
				accessURL = fmt.Sprintf("https://%s.notebook.buzzperformancecloud.com", args[0])
			}

			rows := [][]string{
				{"Name", res.Metadata.Name},
				{"Project", ref.Project},
				{"Workspace", ref.Name},
				{"Status", output.StatusColor(output.ExtractStatus(res.Status))},
				{"SKU", extractProfileName(res.Spec)},
				{"GPU Count", vars["GPU Count"]},
				{"GPU Type", vars["Node Type"]},
				{"Image", vars["Pod Image"]},
				{"URL", accessURL},
			}
			if token != "" {
				rows = append(rows, []string{"Token", token})
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
		Short:   "Delete a Jupyter Notebook",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			var details [][2]string
			if b, err := a.Client().Get(context.Background(), client.ServicePath(ref.Project, ref.Name, args[0])); err == nil {
				var res client.CommonResource
				json.Unmarshal(b, &res)
				details = [][2]string{
					{"Status", output.ExtractStatus(res.Status)},
					{"Workspace", ref.Name},
				}
			}
			ok, err := cmdutil.ConfirmDelete(force, "Notebook", args[0], details)
			if err != nil {
				return err
			}
			if !ok {
				output.Info("Cancelled.")
				return nil
			}
			if err := a.Client().Delete(context.Background(), client.ServicePath(ref.Project, ref.Name, args[0])); err != nil {
				return err
			}
			output.Success(fmt.Sprintf("Notebook %q deleted.", args[0]))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
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

func extractNotebookStatus(status json.RawMessage) (accessURL, token string) {
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
			if v, ok := task["Host Name"]; ok {
				accessURL = v.Value
			}
			if v, ok := task["Token"]; ok {
				token = v.Value
			}
		}
	}
	return
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
