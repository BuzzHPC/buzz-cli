package devpod

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
		Use:     "devpod",
		Aliases: []string{"pod", "pods", "devpods"},
		Short:   "Manage Developer Pods",
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
	var name, sku, nodeType string
	var gpuCount int
	var noDeploy, wait bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and deploy a Developer Pod",
		Example: `  buzz devpod create --name my-pod
  buzz pod create --name my-pod --node-type H100 --gpu-count 2
  buzz devpod create --name my-pod --wait
  buzz devpod create --name my-pod --no-deploy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			output.Info(fmt.Sprintf("Creating DevPod %q in %s/%s...", name, ref.Project, ref.Name))
			res := &client.CommonResource{
				APIVersion: "paas.envmgmt.io/v1",
				Kind:       "ComputeInstance",
				Metadata:   client.Metadata{Name: name, Project: ref.Project},
				Spec: mustMarshal(map[string]interface{}{
					"computeProfile": map[string]interface{}{"name": sku, "systemCatalog": true},
					"variables": []map[string]string{
						{"name": "GPU Count", "valueType": "text", "value": fmt.Sprintf("%d", gpuCount)},
						{"name": "Node Type", "valueType": "text", "value": nodeType},
					},
				}),
			}
			path := client.ComputeInstancePath(ref.Project, ref.Name, "") + "?fail-on-exists=true"
			b, err := a.Client().Post(context.Background(), path, res)
			if err != nil {
				return err
			}
			var created client.CommonResource
			json.Unmarshal(b, &created)
			if noDeploy {
				output.Success(fmt.Sprintf("DevPod %q created (not deployed)", name))
				return nil
			}
			output.Info(fmt.Sprintf("Deploying DevPod %q...", name))
			if err := a.Client().PublishComputeInstance(context.Background(), ref.Project, ref.Name, name); err != nil {
				return fmt.Errorf("created but deploy failed: %w", err)
			}
			output.Success(fmt.Sprintf("DevPod %q deployed.", name))
			if wait {
				return cmdutil.WaitForReady(context.Background(), a.Client(), client.ComputeInstancePath(ref.Project, ref.Name, name), fmt.Sprintf("DevPod %q", name))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "Name of the DevPod (required)")
	cmd.Flags().StringVar(&sku, "sku", "managed-developer-pods-v2-ca-qc-2", "SKU: managed-developer-pods-v2-ca-qc-2 (H200) or managed-developer-pods-v2 (A40/H100)")
	cmd.Flags().StringVar(&nodeType, "node-type", "H200", "GPU node type")
	cmd.Flags().IntVar(&gpuCount, "gpu-count", 1, "Number of GPUs")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "Create resource without deploying it")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for resource to be ready after deploying")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newListCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Developer Pods",
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := a.WorkspaceRefs(cmd.Context())
			if err != nil || len(refs) == 0 {
				output.Info("No DevPods found.")
				return nil
			}
			results, err := a.Client().ListAcrossWorkspaces(cmd.Context(), refs, func(project, ws string) string {
				return client.ComputeInstancePath(project, ws, "")
			})
			if err != nil {
				return err
			}
			var rows [][]string
			for _, r := range results {
				filtered := client.FilterBySKU(r.Items, client.DevPodSKUs)
				rows = append(rows, parseRows(filtered, r.Project, r.Workspace)...)
			}
			if len(rows) == 0 {
				output.Info("No DevPods found.")
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
		Short:   "Get details of a Developer Pod",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			b, err := a.Client().Get(context.Background(), client.ComputeInstancePath(ref.Project, ref.Name, args[0]))
			if err != nil {
				return err
			}
			var res client.CommonResource
			json.Unmarshal(b, &res)

			// Parse spec for GPU info
			gpuCount, nodeType, podImage := extractSpecDetails(res.Spec)

			// Parse status for SSH details
			sshCmd, privateKey := extractSSHDetails(res.Status)

			rows := [][]string{
				{"Name", res.Metadata.Name},
				{"Project", ref.Project},
				{"Workspace", ref.Name},
				{"Status", output.StatusColor(output.ExtractStatus(res.Status))},
				{"GPU Count", gpuCount},
				{"GPU Type", nodeType},
				{"Pod Image", podImage},
			}
			if sshCmd != "" {
				rows = append(rows, []string{"SSH Command", sshCmd})
			}
			output.Table([]string{"FIELD", "VALUE"}, rows)

			if privateKey != "" {
				fmt.Println()
				output.Info("To save your SSH private key, run:")
				fmt.Println(privateKey)
			}
			return nil
		},
	}
}

func extractSpecDetails(spec json.RawMessage) (gpuCount, nodeType, podImage string) {
	var s struct {
		Variables []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"variables"`
	}
	// spec may be a JSON string or object
	if err := json.Unmarshal(spec, &s); err != nil {
		var str string
		if err2 := json.Unmarshal(spec, &str); err2 == nil {
			json.Unmarshal([]byte(str), &s)
		}
	}
	for _, v := range s.Variables {
		switch v.Name {
		case "GPU Count":
			gpuCount = v.Value
		case "Node Type":
			nodeType = v.Value
		case "Pod Image":
			podImage = v.Value
		}
	}
	if gpuCount == "" {
		gpuCount = "—"
	}
	if nodeType == "" {
		nodeType = "—"
	}
	if podImage == "" {
		podImage = "—"
	}
	return
}

func extractSSHDetails(status json.RawMessage) (sshCmd, privateKeyCmd string) {
	var raw struct {
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(status, &raw); err != nil || raw.Output == nil {
		return
	}
	// Walk the nested output map generically
	var outputMap map[string]json.RawMessage
	if err := json.Unmarshal(raw.Output, &outputMap); err != nil {
		return
	}
	for _, resourceRaw := range outputMap {
		var resource struct {
			Tasks map[string]json.RawMessage `json:"tasks"`
		}
		if err := json.Unmarshal(resourceRaw, &resource); err != nil {
			continue
		}
		for _, taskRaw := range resource.Tasks {
			var task map[string]struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(taskRaw, &task); err != nil {
				continue
			}
			if v, ok := task["SSH Command"]; ok {
				sshCmd = v.Value
			}
			if v, ok := task["Create Private Key File"]; ok {
				privateKeyCmd = v.Value
			}
		}
	}
	return
}

func newDeleteCmd(a app.App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"destroy", "rm", "remove"},
		Short:   "Delete a Developer Pod",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			// Fetch resource details for confirmation prompt
			var details [][2]string
			if b, err := a.Client().Get(context.Background(), client.ComputeInstancePath(ref.Project, ref.Name, args[0])); err == nil {
				var res client.CommonResource
				json.Unmarshal(b, &res)
				gpuCount, nodeType, _ := extractSpecDetails(res.Spec)
				details = [][2]string{
					{"Status", output.ExtractStatus(res.Status)},
					{"GPU", fmt.Sprintf("%s x %s", nodeType, gpuCount)},
					{"Workspace", ref.Name},
				}
			}
			ok, err := cmdutil.ConfirmDelete(force, "DevPod", args[0], details)
			if err != nil {
				return err
			}
			if !ok {
				output.Info("Cancelled.")
				return nil
			}
			if err := a.Client().Delete(context.Background(), client.ComputeInstancePath(ref.Project, ref.Name, args[0])); err != nil {
				return err
			}
			output.Success(fmt.Sprintf("DevPod %q deleted.", args[0]))
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
		rows = append(rows, []string{
			res.Metadata.Name,
			project,
			workspace,
			output.StatusColor(output.ExtractStatus(res.Status)),
		})
	}
	return rows
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

