package cmdutil

import (
	"context"
	"fmt"
	"strings"

	"github.com/buzzhpc/buzz-cli/internal/app"
	"github.com/buzzhpc/buzz-cli/internal/client"
)

// RequireWorkspaceRef returns the workspace+project ref for single-resource operations.
// If --workspace was set, finds its project from the workspace refs.
// If only one workspace exists, uses it silently.
// If multiple workspaces exist, requires the user to specify one with --workspace.
func RequireWorkspaceRef(ctx context.Context, a app.App) (client.WorkspaceRef, error) {
	refs, err := a.WorkspaceRefs(ctx)
	if err != nil {
		return client.WorkspaceRef{}, fmt.Errorf("could not list workspaces: %w", err)
	}
	if len(refs) == 0 {
		return client.WorkspaceRef{}, fmt.Errorf("no workspaces found; use --workspace to specify one")
	}

	// If --workspace was specified, find the matching ref
	if a.Workspace() != "" {
		for _, r := range refs {
			if r.Name == a.Workspace() {
				return r, nil
			}
		}
		// Not found in discovered refs — fall back to using the set project
		return client.WorkspaceRef{Name: a.Workspace(), Project: a.Project()}, nil
	}

	if len(refs) == 1 {
		return refs[0], nil
	}

	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return client.WorkspaceRef{}, fmt.Errorf("multiple workspaces found, use --workspace to specify one: %s", strings.Join(names, ", "))
}
