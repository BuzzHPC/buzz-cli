package kubernetes

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
		Use:     "kubernetes",
		Aliases: []string{"k8s", "mks", "cluster"},
		Short:   "Manage Managed Kubernetes clusters",
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
	var nodeCount int
	var noDeploy bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and deploy a Managed Kubernetes cluster",
		Example: `  buzz k8s create --name my-cluster
  buzz k8s create --name my-cluster --node-type A40 --nodes 2
  buzz k8s create --name my-cluster --node-type H200 --nodes 4
  buzz k8s create --name my-cluster --no-deploy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			output.Info(fmt.Sprintf("Creating Kubernetes cluster %q in %s/%s...", name, ref.Project, ref.Name))
			res := &client.CommonResource{
				APIVersion: "paas.envmgmt.io/v1",
				Kind:       "ComputeInstance",
				Metadata:   client.Metadata{Name: name, Project: ref.Project},
				Spec: mustMarshal(map[string]interface{}{
					"computeProfile": map[string]interface{}{"name": sku, "systemCatalog": true},
					"variables": []map[string]string{
						{"name": "GPU Type", "valueType": "text", "value": nodeType},
						{"name": "No Of Nodes", "valueType": "text", "value": fmt.Sprintf("%d", nodeCount)},
					},
				}),
			}
			path := client.ComputeInstancePath(ref.Project, ref.Name, "") + "?fail-on-exists=true"
			if _, err := a.Client().Post(context.Background(), path, res); err != nil {
				return err
			}
			if noDeploy {
				output.Success(fmt.Sprintf("Cluster %q created (not deployed)", name))
				return nil
			}
			output.Info(fmt.Sprintf("Deploying cluster %q...", name))
			if err := a.Client().PublishComputeInstance(context.Background(), ref.Project, ref.Name, name); err != nil {
				return fmt.Errorf("created but deploy failed: %w", err)
			}
			output.Success(fmt.Sprintf("Cluster %q deployed.", name))
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "Name of the cluster (required)")
	cmd.Flags().StringVar(&sku, "sku", "mks-oneclick", "SKU: mks-oneclick (default), mks-k8s-ca-qc-2, mks-k8s")
	cmd.Flags().StringVar(&nodeType, "node-type", "H200", "GPU node type: H200, A40, H100, CPU")
	cmd.Flags().IntVar(&nodeCount, "nodes", 1, "Number of nodes")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "Create resource without deploying it")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newListCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all Kubernetes clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := a.WorkspaceRefs(cmd.Context())
			if err != nil || len(refs) == 0 {
				output.Info("No clusters found.")
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
				filtered := client.FilterBySKU(r.Items, client.KubernetesSKUs)
				rows = append(rows, parseRows(filtered, r.Project, r.Workspace)...)
			}
			if len(rows) == 0 {
				output.Info("No clusters found.")
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
		Short:   "Get details of a Kubernetes cluster",
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

			vars := extractVars(res.Spec)
			kubeconfigURL, clusterName, nodes := extractK8sStatus(res.Status)

			rows := [][]string{
				{"Name", res.Metadata.Name},
				{"Project", ref.Project},
				{"Workspace", ref.Name},
				{"Status", output.StatusColor(output.ExtractStatus(res.Status))},
				{"SKU", extractProfileName(res.Spec)},
				{"GPU Type", vars["GPU Type"]},
				{"Nodes", vars["No Of Nodes"]},
			}
			if clusterName != "" {
				rows = append(rows, []string{"Cluster Name", clusterName})
			}
			if kubeconfigURL != "" {
				rows = append(rows, []string{"Kubeconfig URL", kubeconfigURL})
			}
			if nodes != "" {
				rows = append(rows, []string{"Node List", nodes})
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
		Short:   "Delete a Kubernetes cluster",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			if !force {
				fmt.Printf("Delete cluster %q? [y/N] ", args[0])
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					output.Info("Cancelled.")
					return nil
				}
			}
			if err := a.Client().Delete(context.Background(), client.ComputeInstancePath(ref.Project, ref.Name, args[0])); err != nil {
				return err
			}
			output.Success(fmt.Sprintf("Cluster %q deleted.", args[0]))
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

func extractK8sStatus(status json.RawMessage) (kubeconfigURL, clusterName, nodes string) {
	var raw struct {
		Output map[string]struct {
			Tasks map[string]map[string]json.RawMessage `json:"tasks"`
		} `json:"output"`
	}
	if err := json.Unmarshal(status, &raw); err != nil {
		return
	}
	for _, res := range raw.Output {
		for _, task := range res.Tasks {
			if v, ok := task["cluster_kubeconfig"]; ok {
				json.Unmarshal(v, &kubeconfigURL)
			}
			if v, ok := task["Clusterkubeconfig"]; ok && kubeconfigURL == "" {
				json.Unmarshal(v, &kubeconfigURL)
			}
			if v, ok := task["cluster_name"]; ok {
				json.Unmarshal(v, &clusterName)
			}
			if v, ok := task["Clustername"]; ok && clusterName == "" {
				json.Unmarshal(v, &clusterName)
			}
			if v, ok := task["Nodes"]; ok {
				json.Unmarshal(v, &nodes)
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
