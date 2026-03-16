package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/buzzhpc/buzz-cli/cmd/devpod"
	"github.com/buzzhpc/buzz-cli/cmd/inference"
	"github.com/buzzhpc/buzz-cli/cmd/kubernetes"
	"github.com/buzzhpc/buzz-cli/cmd/notebook"
	"github.com/buzzhpc/buzz-cli/cmd/objectstorage"
	"github.com/buzzhpc/buzz-cli/cmd/sharedfs"
	"github.com/buzzhpc/buzz-cli/cmd/vm"
	"github.com/buzzhpc/buzz-cli/internal/client"
	"github.com/spf13/cobra"
)

var version = "dev"

type globalApp struct {
	apiKey    string
	baseURL   string
	project   string
	workspace string
	c         *client.Client

	wsOnce    sync.Once
	wsCache   []client.WorkspaceRef // workspace name + owning project
	wsErr     error
}

func (a *globalApp) Client() *client.Client { return a.c }
func (a *globalApp) Project() string        { return a.project }
func (a *globalApp) Workspace() string      { return a.workspace }

func (a *globalApp) WorkspaceRefs(ctx context.Context) ([]client.WorkspaceRef, error) {
	a.wsOnce.Do(func() {
		refs, err := a.resolveWorkspaces(ctx)
		a.wsCache, a.wsErr = refs, err
	})
	if a.wsErr != nil {
		return nil, a.wsErr
	}
	// If --workspace is set, filter to just that workspace (preserving its resolved project)
	if a.workspace != "" {
		for _, r := range a.wsCache {
			if r.Name == a.workspace {
				return []client.WorkspaceRef{r}, nil
			}
		}
		// Not found in resolved refs — fall back to set project
		return []client.WorkspaceRef{{Name: a.workspace, Project: a.project}}, nil
	}
	return a.wsCache, nil
}

// Workspaces implements app.App — returns only the workspace names.
func (a *globalApp) Workspaces(ctx context.Context) ([]string, error) {
	refs, err := a.WorkspaceRefs(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return names, nil
}

func (a *globalApp) resolveWorkspaces(ctx context.Context) ([]client.WorkspaceRef, error) {
	// Try the set project first
	ws, err := a.c.ListWorkspaces(ctx, a.project)
	if err == nil && len(ws) > 0 {
		refs := make([]client.WorkspaceRef, len(ws))
		for i, w := range ws {
			refs[i] = client.WorkspaceRef{Name: w, Project: a.project}
		}
		return refs, nil
	}

	// No workspaces in set project — scan all accessible projects
	projects, err := a.c.ListProjects(ctx)
	if err != nil || len(projects) == 0 {
		return nil, err
	}
	seen := make(map[string]bool)
	var refs []client.WorkspaceRef
	for _, proj := range projects {
		wsList, err := a.c.ListWorkspaces(ctx, proj)
		if err != nil {
			continue
		}
		for _, w := range wsList {
			if !seen[w] {
				seen[w] = true
				refs = append(refs, client.WorkspaceRef{Name: w, Project: proj})
			}
		}
	}
	return refs, nil
}

func main() {
	g := &globalApp{}

	root := &cobra.Command{
		Use:     "buzz",
		Short:   "BuzzHPC CLI — manage GPU cloud resources from the command line",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip API key check for completion and help commands
			for c := cmd; c != nil; c = c.Parent() {
				if c.Name() == "completion" || c.Name() == "help" {
					return nil
				}
			}

			if g.apiKey == "" {
				g.apiKey = os.Getenv("BUZZHPC_API_KEY")
			}
			if g.apiKey == "" {
				return fmt.Errorf("API key required: set BUZZHPC_API_KEY or use --api-key")
			}
			if g.baseURL == "" {
				g.baseURL = os.Getenv("BUZZHPC_BASE_URL")
			}
			if g.project == "" {
				g.project = os.Getenv("BUZZHPC_PROJECT")
			}
			if g.workspace == "" {
				g.workspace = os.Getenv("BUZZHPC_WORKSPACE")
			}

			g.c = client.New(g.apiKey, g.baseURL)

			// Default to "defaultproject", prompt if it doesn't work
			if g.project == "" {
				g.project = "defaultproject"
				if _, err := g.c.ListWorkspaces(cmd.Context(), g.project); err != nil {
					fmt.Printf("Project %q not found.\n", g.project)
					fmt.Print("Enter your project name (visible in the BuzzHPC console URL): ")
					scanner := bufio.NewScanner(os.Stdin)
					scanner.Scan()
					g.project = strings.TrimSpace(scanner.Text())
					if g.project == "" {
						return fmt.Errorf("project name is required")
					}
					fmt.Printf("\nTip: set permanently with:\n  export BUZZHPC_PROJECT=%s\n\n", g.project)
				}
			}

			return nil
		},
	}

	root.PersistentFlags().StringVar(&g.apiKey, "api-key", "", "BuzzHPC API key (or set BUZZHPC_API_KEY)")
	root.PersistentFlags().StringVar(&g.baseURL, "base-url", "", "API base URL (or set BUZZHPC_BASE_URL)")
	root.PersistentFlags().StringVarP(&g.project, "project", "p", "", "Project name (or set BUZZHPC_PROJECT)")
	root.PersistentFlags().StringVarP(&g.workspace, "workspace", "w", "", "Workspace name (or set BUZZHPC_WORKSPACE)")

	root.AddCommand(
		devpod.NewCmd(g),
		kubernetes.NewCmd(g),
		vm.NewCmd(g),
		notebook.NewCmd(g),
		inference.NewCmd(g),
		objectstorage.NewCmd(g),
		sharedfs.NewCmd(g),
	)
	root.CompletionOptions.HiddenDefaultCmd = true

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
