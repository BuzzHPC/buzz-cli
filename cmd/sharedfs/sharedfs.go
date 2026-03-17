package sharedfs

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
		Use:     "shared-fs",
		Aliases: []string{"nfs", "shared-filesystem", "fs"},
		Short:   "Manage Shared Filesystems",
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
	var name string
	var sizeGB int
	var noDeploy, wait bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and deploy a Shared Filesystem",
		Example: `  buzz shared-fs create --name my-fs --size 50
  buzz nfs create --name datasets --size 500
  buzz fs create --name model-weights --size 200
  buzz shared-fs create --name my-fs --no-deploy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			output.Info(fmt.Sprintf("Creating Shared Filesystem %q (%dGB)...", name, sizeGB))
			res := &client.CommonResource{
				APIVersion: "paas.envmgmt.io/v1",
				Kind:       "Service",
				Metadata:   client.Metadata{Name: name, Project: ref.Project},
				Spec: mustMarshal(map[string]interface{}{
					"serviceProfile": map[string]interface{}{"name": "shared-filesystem", "systemCatalog": true},
					"variables": []map[string]string{
						{"name": "block_storage_volume_size_gb", "valueType": "json", "value": fmt.Sprintf("%d", sizeGB)},
					},
				}),
			}
			path := client.ServicePath(ref.Project, ref.Name, "") + "?fail-on-exists=true"
			if _, err := a.Client().Post(context.Background(), path, res); err != nil {
				return err
			}
			if noDeploy {
				output.Success(fmt.Sprintf("Shared Filesystem %q created (not deployed)", name))
				return nil
			}
			output.Info(fmt.Sprintf("Deploying Shared Filesystem %q...", name))
			if err := a.Client().PublishService(context.Background(), ref.Project, ref.Name, name); err != nil {
				return fmt.Errorf("created but deploy failed: %w", err)
			}
			output.Success(fmt.Sprintf("Shared Filesystem %q deployed.", name))
			if wait {
				return cmdutil.WaitForReady(context.Background(), a.Client(), client.ServicePath(ref.Project, ref.Name, name), fmt.Sprintf("Shared Filesystem %q", name))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "Name of the filesystem (required)")
	cmd.Flags().IntVarP(&sizeGB, "size", "s", 50, "Volume size in GB")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "Create resource without deploying it")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for resource to be ready after deploying")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newListCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Shared Filesystems",
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := a.WorkspaceRefs(cmd.Context())
			if err != nil || len(refs) == 0 {
				output.Info("No shared filesystems found.")
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
				filtered := client.FilterBySKU(r.Items, client.SharedFSSKUs)
				rows = append(rows, parseRows(filtered, r.Project, r.Workspace)...)
			}
			if len(rows) == 0 {
				output.Info("No shared filesystems found.")
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
		Short:   "Get details of a Shared Filesystem",
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
			mountPath, serverIP := extractFSStatus(res.Status)

			rows := [][]string{
				{"Name", res.Metadata.Name},
				{"Project", ref.Project},
				{"Workspace", ref.Name},
				{"Status", output.StatusColor(output.ExtractStatus(res.Status))},
				{"SKU", extractProfileName(res.Spec)},
			}
			if sz := vars["block_storage_volume_size_gb"]; sz != "" {
				rows = append(rows, []string{"Size (GB)", sz})
			}
			if serverIP != "" {
				rows = append(rows, []string{"Server IP", serverIP})
			}
			if mountPath != "" {
				rows = append(rows, []string{"Mount Path", mountPath})
			}
			if serverIP != "" && mountPath != "" {
				rows = append(rows, []string{"Mount Command", fmt.Sprintf("mount -t nfs %s:%s /mnt", serverIP, mountPath)})
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
		Short:   "Delete a Shared Filesystem",
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
			ok, err := cmdutil.ConfirmDelete(force, "Shared Filesystem", args[0], details)
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
			output.Success(fmt.Sprintf("Shared Filesystem %q deleted.", args[0]))
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

func extractFSStatus(status json.RawMessage) (mountPath, serverIP string) {
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
				if strings.Contains(lower, "mount") || strings.Contains(lower, "path") || strings.Contains(lower, "export") {
					mountPath = v.Value
				}
				if strings.Contains(lower, "server") || strings.Contains(lower, "ip") || strings.Contains(lower, "host") {
					serverIP = v.Value
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
