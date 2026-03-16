package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/buzzhpc/buzz-cli/internal/app"
	"github.com/buzzhpc/buzz-cli/internal/client"
	"github.com/buzzhpc/buzz-cli/internal/cmdutil"
	"github.com/buzzhpc/buzz-cli/internal/output"
	"github.com/spf13/cobra"
)

func NewCmd(a app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "vm",
		Aliases: []string{"gpu-vm", "virtual-machine"},
		Short:   "Manage GPU Virtual Machines",
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
	var noDeploy bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and deploy a GPU Virtual Machine",
		Example: `  buzz vm create --name my-vm
  buzz gpu-vm create --name my-vm --node-type H200 --gpu-count 2
  buzz vm create --name my-vm --no-deploy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			output.Info(fmt.Sprintf("Creating GPU VM %q in %s/%s...", name, ref.Project, ref.Name))
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
			if _, err := a.Client().Post(context.Background(), path, res); err != nil {
				return err
			}
			if noDeploy {
				output.Success(fmt.Sprintf("VM %q created (not deployed)", name))
				return nil
			}
			output.Info(fmt.Sprintf("Deploying VM %q...", name))
			if err := a.Client().PublishComputeInstance(context.Background(), ref.Project, ref.Name, name); err != nil {
				return fmt.Errorf("created but deploy failed: %w", err)
			}
			output.Success(fmt.Sprintf("VM %q deployed.", name))
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "Name of the VM (required)")
	cmd.Flags().StringVar(&sku, "sku", "no-gpu-vm", "SKU (default: no-gpu-vm)")
	cmd.Flags().StringVar(&nodeType, "node-type", "H200", "GPU node type: H200")
	cmd.Flags().IntVar(&gpuCount, "gpu-count", 1, "Number of GPUs")
	cmd.Flags().BoolVar(&noDeploy, "no-deploy", false, "Create resource without deploying it")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newListCmd(a app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List all GPU VMs",
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, err := a.WorkspaceRefs(cmd.Context())
			if err != nil || len(refs) == 0 {
				output.Info("No VMs found.")
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
				filtered := client.FilterBySKU(r.Items, client.VMSKUs)
				rows = append(rows, parseRows(filtered, r.Project, r.Workspace)...)
			}
			if len(rows) == 0 {
				output.Info("No VMs found.")
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
		Short:   "Get details of a GPU VM",
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
			sshCmd, privateKey := extractSSHDetails(res.Status)
			vmDetails := extractVMDetails(res.Status)
			vmDetails.Datacenter = extractVMDatacenter(res.Spec)

			memGB := ""
			if mb := vars["Guest Memory Size"]; mb != "" {
				if v, err := strconv.Atoi(mb); err == nil {
					memGB = fmt.Sprintf("%d GB", v/1024)
				} else {
					memGB = mb
				}
			}

			rows := [][]string{
				{"Name", res.Metadata.Name},
				{"Project", ref.Project},
				{"Workspace", ref.Name},
				{"Status", output.StatusColor(output.ExtractStatus(res.Status))},
				{"SKU", extractProfileName(res.Spec)},
			}
			if v := vars["GPU Model"]; v != "" {
				rows = append(rows, []string{"GPU Model", v})
			}
			if v := vars["Guest GPU Count"]; v != "" {
				rows = append(rows, []string{"GPU Count", v})
			}
			if v := vars["Guest CPU Count"]; v != "" {
				rows = append(rows, []string{"CPU Count", v})
			}
			if memGB != "" {
				rows = append(rows, []string{"Memory", memGB})
			}
			if v := vars["Guest Disk Size"]; v != "" {
				rows = append(rows, []string{"Disk (GB)", v})
			}
			if vmDetails.Datacenter != "" {
				rows = append(rows, []string{"Datacenter", vmDetails.Datacenter})
			}
			if vmDetails.OSName != "" {
				rows = append(rows, []string{"OS", vmDetails.OSName})
			}
			if vmDetails.Username != "" {
				rows = append(rows, []string{"Username", vmDetails.Username})
			}
			if vmDetails.Password != "" {
				rows = append(rows, []string{"Password", vmDetails.Password})
			}
			if vmDetails.PrivateIP != "" {
				rows = append(rows, []string{"Private IP", vmDetails.PrivateIP})
			}
			if vmDetails.PublicIP != "" {
				rows = append(rows, []string{"Public IP", vmDetails.PublicIP})
			}
			if vmDetails.Port != "" {
				rows = append(rows, []string{"SSH Port", vmDetails.Port})
			}
			if vmDetails.PortForwards != "" && vmDetails.PortForwards != "No Port forward configured" {
				rows = append(rows, []string{"Port Forwards", vmDetails.PortForwards})
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

func newDeleteCmd(a app.App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"destroy", "rm"},
		Short:   "Delete a GPU VM",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := cmdutil.RequireWorkspaceRef(cmd.Context(), a)
			if err != nil {
				return err
			}
			if !force {
				fmt.Printf("Delete VM %q? [y/N] ", args[0])
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
			output.Success(fmt.Sprintf("VM %q deleted.", args[0]))
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

type vmDetails struct {
	Hostname     string
	OSName       string
	Username     string
	Password     string
	PrivateIP    string
	PublicIP     string
	Port         string
	PortForwards string
	ServerHost   string
	Datacenter   string
}

func extractVMDetails(status json.RawMessage) vmDetails {
	var raw struct {
		Output map[string]struct {
			Tasks map[string]map[string]string `json:"tasks"`
		} `json:"output"`
	}
	var d vmDetails
	if err := json.Unmarshal(status, &raw); err != nil {
		return d
	}
	for _, res := range raw.Output {
		for _, task := range res.Tasks {
			if v, ok := task["Hostname"]; ok {
				d.Hostname = v
			}
			if v, ok := task["OSName"]; ok {
				d.OSName = v
			}
			if v, ok := task["Username"]; ok {
				d.Username = v
			}
			if v, ok := task["Password"]; ok {
				d.Password = v
			}
			if v, ok := task["Private IP"]; ok {
				d.PrivateIP = v
			}
			if v, ok := task["Public IP"]; ok {
				d.PublicIP = v
			}
			if v, ok := task["Port"]; ok {
				d.Port = v
			}
			if v, ok := task["Port Forwards"]; ok {
				d.PortForwards = v
			}
			if v, ok := task["ServerHost"]; ok {
				d.ServerHost = v
			}
		}
	}
	return d
}

func extractVMDatacenter(spec json.RawMessage) string {
	var s struct {
		Datacenter struct{ Name string `json:"name"` } `json:"datacenter"`
	}
	if err := json.Unmarshal(spec, &s); err != nil {
		var str string
		if json.Unmarshal(spec, &str) == nil {
			json.Unmarshal([]byte(str), &s)
		}
	}
	return s.Datacenter.Name
}

func extractSSHDetails(status json.RawMessage) (sshCmd, privateKeyCmd string) {
	var raw struct {
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(status, &raw); err != nil || raw.Output == nil {
		return
	}
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

// extractOutputField searches output tasks for the first value whose key contains any of the given substrings.
func extractOutputField(status json.RawMessage, keywords ...string) string {
	var raw struct {
		Output map[string]struct {
			Tasks map[string]map[string]struct {
				Value string `json:"value"`
			} `json:"tasks"`
		} `json:"output"`
	}
	if err := json.Unmarshal(status, &raw); err != nil {
		return ""
	}
	for _, res := range raw.Output {
		for _, task := range res.Tasks {
			for k, v := range task {
				lower := strings.ToLower(k)
				for _, kw := range keywords {
					if strings.Contains(lower, kw) && v.Value != "" {
						return v.Value
					}
				}
			}
		}
	}
	return ""
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
