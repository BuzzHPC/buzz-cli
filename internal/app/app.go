package app

import (
	"context"

	"github.com/buzzhpc/buzz-cli/internal/client"
)

// App is the interface all commands use to access shared state.
type App interface {
	Client() *client.Client
	Project() string
	// Workspace returns the explicitly set workspace, or "" if none.
	Workspace() string
	// Workspaces returns workspace names across all accessible projects.
	Workspaces(ctx context.Context) ([]string, error)
	// WorkspaceRefs returns workspace+project pairs across all accessible projects.
	WorkspaceRefs(ctx context.Context) ([]client.WorkspaceRef, error)
}
