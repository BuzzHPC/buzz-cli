package client

import (
	"context"
	"encoding/json"
	"fmt"
)

// ListProjects returns all project names accessible to this API key.
func (c *Client) ListProjects(ctx context.Context) ([]string, error) {
	b, err := c.Get(ctx, "/apis/system.k8smgmt.io/v3/projects?limit=100")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	var names []string
	for _, raw := range resp.Items {
		var p struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(raw, &p); err == nil && p.Metadata.Name != "" {
			names = append(names, p.Metadata.Name)
		}
	}
	return names, nil
}

// ListWorkspaces returns all workspace names for a given project.
func (c *Client) ListWorkspaces(ctx context.Context, project string) ([]string, error) {
	path := fmt.Sprintf("/apis/paas.envmgmt.io/v1/projects/%s/workspaces?limit=100&offset=0", project)
	b, err := c.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	var names []string
	for _, raw := range resp.Items {
		var ws struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(raw, &ws); err == nil && ws.Metadata.Name != "" {
			names = append(names, ws.Metadata.Name)
		}
	}
	return names, nil
}

// WorkspaceRef holds a workspace name and the project it belongs to.
type WorkspaceRef struct {
	Name    string
	Project string
}

// WorkspaceResult holds items from one workspace along with its name and project.
type WorkspaceResult struct {
	Workspace string
	Project   string
	Items     []json.RawMessage
}

// ListAcrossWorkspaces calls pathFn(project, workspace) for each WorkspaceRef and aggregates results.
// Results are returned in workspace order; inaccessible workspaces are silently skipped.
func (c *Client) ListAcrossWorkspaces(ctx context.Context, refs []WorkspaceRef, pathFn func(project, ws string) string) ([]WorkspaceResult, error) {
	type result struct {
		idx   int
		ref   WorkspaceRef
		items []json.RawMessage
		err   error
	}

	ch := make(chan result, len(refs))
	for i, ref := range refs {
		i, ref := i, ref
		go func() {
			items, err := c.List(ctx, pathFn(ref.Project, ref.Name))
			ch <- result{idx: i, ref: ref, items: items, err: err}
		}()
	}

	results := make([]WorkspaceResult, len(refs))
	for range refs {
		r := <-ch
		if r.err != nil {
			continue
		}
		results[r.idx] = WorkspaceResult{Workspace: r.ref.Name, Project: r.ref.Project, Items: r.items}
	}
	return results, nil
}
