package objectstorage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/buzzhpc/buzz-cli/internal/app"
	"github.com/buzzhpc/buzz-cli/internal/client"
	"github.com/buzzhpc/buzz-cli/internal/cmdutil"
	"github.com/buzzhpc/buzz-cli/internal/output"
	"github.com/buzzhpc/buzz-cli/internal/region"
	"github.com/spf13/cobra"
)

func NewCmd(a app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "object-storage",
		Aliases: []string{"s3", "obs", "bucket"},
		Short:   "Manage Object Storage buckets",
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
	var name, regionStr string
	var quotaGB int
	var noDeploy, wait bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an Object Storage bucket",
		Example: `  buzz object-storage create --name my-bucket --region ca-qc-2
  buzz s3 create --name my-bucket --region ca-qc-1 --size 100`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := region.Parse(regionStr)
			if err != nil {
				return err
			}

			sku, err := region.SKU("object-storage", r)
			if err != nil {
				return err
			}

			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}

			output.Info(fmt.Sprintf("Creating Object Storage bucket %q (%dGB, region: %s)...", name, quotaGB, r))

			res := &client.CommonResource{
				APIVersion: "paas.envmgmt.io/v1",
				Kind:       "Service",
				Metadata:   client.Metadata{Name: name, Project: ref.Project},
				Spec: mustMarshal(map[string]interface{}{
					"serviceProfile": map[string]interface{}{"name": sku, "systemCatalog": true},
					"variables": []map[string]string{
						{"name": "quota_max_size_gb", "valueType": "json", "value": fmt.Sprintf("%d", quotaGB)},
					},
				}),
			}

			path := client.ServicePath(ref.Project, ref.Name, "") + "?fail-on-exists=true"
			if _, err := a.Client().Post(context.Background(), path, res); err != nil {
				return err
			}

			if noDeploy {
				output.Success(fmt.Sprintf("Bucket %q created (not deployed)", name))
				return nil
			}

			output.Info(fmt.Sprintf("Deploying bucket %q...", name))
			if err := a.Client().PublishService(context.Background(), ref.Project, ref.Name, name); err != nil {
				return fmt.Errorf("created but deploy failed: %w", err)
			}
			output.Success(fmt.Sprintf("Bucket %q deployed.", name))

			if wait {
				return cmdutil.WaitForReady(context.Background(), a.Client(), client.ServicePath(ref.Project, ref.Name, name), fmt.Sprintf("Bucket %q", name))
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Name of the bucket (required)")
	cmd.Flags().StringVarP(&regionStr, "region", "r", "", "Deployment region: ca-qc-1 or ca-qc-2 (required)")
	cmd.Flags().IntVarP(&quotaGB, "size", "s", 10, "Storage quota in GB")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "Create resource without deploying it")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for resource to be ready after deploying")

	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("region")

	return cmd
}

func newListCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Object Storage buckets",
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := a.WorkspaceRefs(cmd.Context())
			if err != nil || len(refs) == 0 {
				output.Info("No buckets found.")
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
				filtered := client.FilterBySKU(r.Items, client.ObjectStorageSKUs)
				rows = append(rows, parseRows(filtered, r.Project, r.Workspace)...)
			}
			if len(rows) == 0 {
				output.Info("No buckets found.")
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
		Short:   "Get details of an Object Storage bucket",
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
			output.Table([]string{"FIELD", "VALUE"}, [][]string{
				{"Name", res.Metadata.Name},
				{"Project", ref.Project},
				{"Workspace", ref.Name},
				{"Status", output.StatusColor(output.ExtractStatus(res.Status))},
			})
			return nil
		},
	}
}

func newDeleteCmd(a app.App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"destroy", "rm", "remove"},
		Short:   "Delete an Object Storage bucket",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			ok, err := cmdutil.ConfirmDelete(force, "Bucket", args[0], nil)
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
			output.Success(fmt.Sprintf("Bucket %q deleted.", args[0]))
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
