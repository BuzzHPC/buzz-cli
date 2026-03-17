package objectstorage

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
		Use:     "object-storage",
		Aliases: []string{"s3", "obs", "bucket"},
		Short:   "Manage S3-compatible Object Storage buckets",
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
	var name, sku string
	var quotaGB int
	var noDeploy, wait bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and deploy an Object Storage bucket",
		Example: `  buzz object-storage create --name my-bucket --size 50
  buzz s3 create --name my-bucket --size 100
  buzz bucket create --name my-bucket --sku object-storage-vast --size 200
  buzz s3 create --name my-bucket --no-deploy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			output.Info(fmt.Sprintf("Creating Object Storage bucket %q (%dGB)...", name, quotaGB))
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
	cmd.Flags().StringVar(&sku, "sku", "object-storage-vast-ca-qc-2", "SKU: object-storage-vast-ca-qc-2 (CA-QC-2) or object-storage-vast (CA-QC-1)")
	cmd.Flags().IntVarP(&quotaGB, "size", "s", 10, "Storage quota in GB")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "Create resource without deploying it")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for resource to be ready after deploying")
	cmd.MarkFlagRequired("name")
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
		Short:   "Get details of a bucket",
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
			s3URL, bucketName, accessKey, secretKey, datacenter := extractS3Status(res.Status, res.Spec)

			rows := [][]string{
				{"Name", res.Metadata.Name},
				{"Project", ref.Project},
				{"Workspace", ref.Name},
				{"Status", output.StatusColor(output.ExtractStatus(res.Status))},
				{"SKU", extractProfileName(res.Spec)},
			}
			if datacenter != "" {
				rows = append(rows, []string{"Datacenter", datacenter})
			}
			if q := vars["quota_max_size_gb"]; q != "" {
				rows = append(rows, []string{"Quota (GB)", q})
			}
			if bucketName != "" {
				rows = append(rows, []string{"Bucket Name", bucketName})
			}
			if s3URL != "" {
				rows = append(rows, []string{"S3 URL", s3URL})
			}
			if accessKey != "" {
				rows = append(rows, []string{"Access Key", accessKey})
			}
			if secretKey != "" {
				rows = append(rows, []string{"Secret Key", secretKey})
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
		Short:   "Delete an Object Storage bucket",
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
			ok, err := cmdutil.ConfirmDelete(force, "Bucket", args[0], details)
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

func extractS3Status(status, spec json.RawMessage) (s3URL, bucketName, accessKey, secretKey, datacenter string) {
	// Extract datacenter from spec
	var sp struct {
		Datacenter struct{ Name string `json:"name"` } `json:"datacenter"`
	}
	if err := json.Unmarshal(spec, &sp); err == nil {
		datacenter = sp.Datacenter.Name
	}

	// Walk output tasks — values are objects with a "value" field
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
			if v, ok := task["s3_url"]; ok {
				s3URL = v.Value
			}
			if v, ok := task["bucket_name"]; ok {
				bucketName = v.Value
			}
			if v, ok := task["access_key_id"]; ok {
				accessKey = v.Value
			}
			if v, ok := task["secret_access_key"]; ok {
				secretKey = v.Value
			}
		}
	}
	return
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

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
