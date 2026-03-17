package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	"github.com/buzzhpc/buzz-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var version = "dev"

// config holds values loaded from ~/.buzzhpc/config
type config struct {
	APIKey  string `yaml:"api_key"`
	Project string `yaml:"project"`
	BaseURL string `yaml:"base_url"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".buzzhpc", "config")
}

func loadConfig() config {
	var cfg config
	b, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	yaml.Unmarshal(b, &cfg)
	return cfg
}

func saveConfig(cfg config) error {
	dir := filepath.Dir(configPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), b, 0600)
}

type globalApp struct {
	apiKey    string
	baseURL   string
	project   string
	workspace string
	c         *client.Client

	wsOnce  sync.Once
	wsCache []client.WorkspaceRef
	wsErr   error
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
	if a.workspace != "" {
		for _, r := range a.wsCache {
			if r.Name == a.workspace {
				return []client.WorkspaceRef{r}, nil
			}
		}
		return []client.WorkspaceRef{{Name: a.workspace, Project: a.project}}, nil
	}
	return a.wsCache, nil
}

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
	ws, err := a.c.ListWorkspaces(ctx, a.project)
	if err == nil && len(ws) > 0 {
		refs := make([]client.WorkspaceRef, len(ws))
		for i, w := range ws {
			refs[i] = client.WorkspaceRef{Name: w, Project: a.project}
		}
		return refs, nil
	}

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

// isInteractive returns true if stdin is a terminal.
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func newConfigureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Configure the CLI interactively and save to ~/.buzzhpc/config",
		RunE: func(cmd *cobra.Command, args []string) error {
			existing := loadConfig()
			scanner := bufio.NewScanner(os.Stdin)

			prompt := func(label, current string) string {
				if current != "" {
					fmt.Printf("%s [%s]: ", label, current)
				} else {
					fmt.Printf("%s: ", label)
				}
				scanner.Scan()
				val := strings.TrimSpace(scanner.Text())
				if val == "" {
					return current
				}
				return val
			}

			fmt.Println("Configure the BuzzHPC CLI. Press Enter to keep existing values.")
			fmt.Println()

			cfg := config{
				APIKey:  prompt("API Key", existing.APIKey),
				Project: prompt("Project", existing.Project),
				BaseURL: prompt("Base URL (leave blank for default)", existing.BaseURL),
			}

			if cfg.APIKey == "" {
				return fmt.Errorf("API key is required")
			}

			if err := saveConfig(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			output.Success(fmt.Sprintf("Config saved to %s", configPath()))
			return nil
		},
	}
}

func main() {
	g := &globalApp{}

	root := &cobra.Command{
		Use:     "buzz",
		Short:   "BuzzHPC CLI — manage GPU cloud resources from the command line",
		Version: version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip auth for help and configure
			for c := cmd; c != nil; c = c.Parent() {
				if c.Name() == "completion" || c.Name() == "help" || c.Name() == "configure" {
					return nil
				}
			}

			// Load config file first, then env vars, then flags (flags win)
			cfg := loadConfig()
			if g.apiKey == "" {
				g.apiKey = cfg.APIKey
			}
			if g.apiKey == "" {
				g.apiKey = os.Getenv("BUZZHPC_API_KEY")
			}
			if g.apiKey == "" {
				return fmt.Errorf("API key required: run 'buzz configure', set BUZZHPC_API_KEY, or use --api-key")
			}
			if g.baseURL == "" {
				g.baseURL = cfg.BaseURL
			}
			if g.baseURL == "" {
				g.baseURL = os.Getenv("BUZZHPC_BASE_URL")
			}
			if g.project == "" {
				g.project = cfg.Project
			}
			if g.project == "" {
				g.project = os.Getenv("BUZZHPC_PROJECT")
			}
			if g.workspace == "" {
				g.workspace = os.Getenv("BUZZHPC_WORKSPACE")
			}

			g.c = client.New(g.apiKey, g.baseURL)

			if g.project == "" {
				g.project = "defaultproject"
				if _, err := g.c.ListWorkspaces(cmd.Context(), g.project); err != nil {
					// Non-interactive: fail immediately instead of hanging
					if !isInteractive() {
						return fmt.Errorf("project %q not found. Set BUZZHPC_PROJECT or use -p flag", g.project)
					}
					fmt.Printf("Project %q not found.\n", g.project)
					fmt.Print("Enter your project name (visible in the BuzzHPC console URL): ")
					scanner := bufio.NewScanner(os.Stdin)
					scanner.Scan()
					g.project = strings.TrimSpace(scanner.Text())
					if g.project == "" {
						return fmt.Errorf("project name is required")
					}
					fmt.Printf("\nTip: run 'buzz configure' to save this permanently.\n\n")
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
		newConfigureCmd(),
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
