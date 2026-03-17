package kubernetes

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
		Use:     "kubernetes",
		Aliases: []string{"k8s", "mks", "cluster"},
		Short:   "Manage Managed Kubernetes clusters",
	}
	cmd.AddCommand(
		newCreateCmd(a),
		newListCmd(a),
		newGetCmd(a),
		newDeleteCmd(a),
		newTagsCmd(a),
		newTagCmd(a),
	)
	return cmd
}

func newCreateCmd(a app.App) *cobra.Command {
	var name, sku, nodeType string
	var nodeCount int
	var noDeploy, wait bool

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
			if wait {
				return cmdutil.WaitForReady(context.Background(), a.Client(), client.ComputeInstancePath(ref.Project, ref.Name, name), fmt.Sprintf("Cluster %q", name))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "Name of the cluster (required)")
	cmd.Flags().StringVar(&sku, "sku", "mks-oneclick", "SKU: mks-oneclick (default), mks-k8s-ca-qc-2, mks-k8s")
	cmd.Flags().StringVar(&nodeType, "node-type", "H200", "GPU node type: H200, A40, H100, CPU")
	cmd.Flags().IntVar(&nodeCount, "nodes", 1, "Number of nodes")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "Create resource without deploying it")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for resource to be ready after deploying")
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
			var details [][2]string
			if b, err := a.Client().Get(context.Background(), client.ComputeInstancePath(ref.Project, ref.Name, args[0])); err == nil {
				var res client.CommonResource
				json.Unmarshal(b, &res)
				details = [][2]string{
					{"Status", output.ExtractStatus(res.Status)},
					{"Workspace", ref.Name},
				}
			}
			ok, err := cmdutil.ConfirmDelete(force, "Cluster", args[0], details)
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

func newTagsCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "tags <cluster>",
		Aliases: []string{"list-tags"},
		Short:   "List tags on a Kubernetes cluster",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			clusterName := args[0]
			items, err := a.Client().List(context.Background(), client.TagAssociationPath(ref.Project, ""))
			if err != nil {
				return err
			}
			var rows [][]string
			for _, raw := range items {
				var ta client.TagAssociation
				if json.Unmarshal(raw, &ta) != nil {
					continue
				}
				for _, assoc := range ta.Spec.Associations {
					if assoc.Resource == clusterName {
						rows = append(rows, []string{assoc.TagKey, assoc.TagValue, assoc.TagType, ta.Metadata.Name})
					}
				}
			}
			if len(rows) == 0 {
				output.Info(fmt.Sprintf("No tags found on cluster %q.", clusterName))
				return nil
			}
			output.Table([]string{"KEY", "VALUE", "TYPE", "ASSOCIATION"}, rows)
			return nil
		},
	}
}

func newTagCmd(a app.App) *cobra.Command {
	var tagType string
	var remove bool

	cmd := &cobra.Command{
		Use:   "tag <cluster> key=value",
		Short: "Apply or remove a tag on a Kubernetes cluster",
		Example: `  buzz k8s tag my-cluster env=production
  buzz k8s tag my-cluster team=ml --type cost
  buzz k8s tag my-cluster env=production --remove`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			clusterName := args[0]
			kv := strings.SplitN(args[1], "=", 2)
			if len(kv) != 2 {
				return fmt.Errorf("tag must be in key=value format")
			}
			tagKey, tagValue := kv[0], kv[1]

			if remove {
				// Find the association containing this tag on the cluster
				items, err := a.Client().List(context.Background(), client.TagAssociationPath(ref.Project, ""))
				if err != nil {
					return err
				}
				found := false
				for _, raw := range items {
					var ta client.TagAssociation
					if json.Unmarshal(raw, &ta) != nil {
						continue
					}
					for _, assoc := range ta.Spec.Associations {
						if assoc.Resource == clusterName && assoc.TagKey == tagKey && assoc.TagValue == tagValue {
							if err := a.Client().Delete(context.Background(), client.TagAssociationPath(ref.Project, ta.Metadata.Name)); err != nil {
								return err
							}
							output.Success(fmt.Sprintf("Removed tag %s=%s from cluster %q.", tagKey, tagValue, clusterName))
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found {
					return fmt.Errorf("tag %s=%s not found on cluster %q", tagKey, tagValue, clusterName)
				}
				return nil
			}

			// Create a TagGroup for the key/value
			tgName := fmt.Sprintf("%s-%s-%s", clusterName, tagKey, strings.ReplaceAll(tagValue, "=", "-"))
			tg := &client.TagGroup{
				APIVersion: "tags.k8smgmt.io/v3",
				Kind:       "TagGroup",
				Metadata:   client.Metadata{Name: tgName, Project: ref.Project},
				Spec: client.TagGroupSpec{
					Tags: []client.TagKV{{Key: tagKey, Value: tagValue}},
				},
			}
			if _, err := a.Client().Post(context.Background(), client.TagGroupPath(ref.Project, ""), tg); err != nil {
				// Ignore already-exists errors
				if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "exists") {
					return fmt.Errorf("create tag group: %w", err)
				}
			}

			// Create the association
			assocName := fmt.Sprintf("%s-%s-%s-assoc", clusterName, tagKey, strings.ReplaceAll(tagValue, "=", "-"))
			ta := &client.TagAssociation{
				APIVersion: "tags.k8smgmt.io/v3",
				Kind:       "ProjectTagsAssociation",
				Metadata:   client.Metadata{Name: assocName, Project: ref.Project},
				Spec: client.TagAssociationSpec{
					Associations: []client.TagAssociationEntry{
						{TagKey: tagKey, TagType: tagType, TagValue: tagValue, Resource: clusterName},
					},
				},
			}
			if _, err := a.Client().Post(context.Background(), client.TagAssociationPath(ref.Project, ""), ta); err != nil {
				return fmt.Errorf("create tag association: %w", err)
			}
			output.Success(fmt.Sprintf("Tagged cluster %q with %s=%s (type: %s).", clusterName, tagKey, tagValue, tagType))
			return nil
		},
	}
	cmd.Flags().StringVar(&tagType, "type", "k8s", "Tag type: k8s, cost, namespacelabel")
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove the tag instead of applying it")
	return cmd
}
