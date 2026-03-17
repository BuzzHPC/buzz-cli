package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultBaseURL = "https://console.buzzhpc.ai"

var ErrNotFound = errors.New("not found")

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func New(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// CommonResource is the Kubernetes-style envelope used by all BuzzHPC resources.
type CommonResource struct {
	APIVersion string          `json:"apiVersion,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Metadata   Metadata        `json:"metadata"`
	Spec       json.RawMessage `json:"spec,omitempty"`
	Status     json.RawMessage `json:"status,omitempty"`
}

type Metadata struct {
	Name        string            `json:"name"`
	Project     string            `json:"project,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ListResponse struct {
	Items    []json.RawMessage `json:"items"`
	Metadata struct {
		Count int `json:"count"`
		Limit int `json:"limit"`
	} `json:"metadata"`
}

type apiError struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	Internal string `json:"internal"`
	External string `json:"external"`
}

func (e *apiError) Error() string {
	if e.External != "" {
		return e.External
	}
	if e.Internal != "" {
		return e.Internal
	}
	return e.Message
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	var lastErr error
	for attempt := 0; attempt <= 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)

		if resp.StatusCode == 404 {
			return nil, ErrNotFound
		}

		if resp.StatusCode == 502 || resp.StatusCode == 503 {
			lastErr = fmt.Errorf("server error %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode >= 400 {
			var ae apiError
			if json.Unmarshal(b, &ae) == nil {
				msg := ae.Internal
				if msg == "" {
					msg = ae.External
				}
				if strings.Contains(strings.ToLower(msg), "not found") {
					return nil, ErrNotFound
				}
				return nil, &ae
			}
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
		}

		// Check for HTTP 200 with error body
		var ae apiError
		if json.Unmarshal(b, &ae) == nil && ae.Code != 0 {
			msg := ae.Internal
			if msg == "" {
				msg = ae.External
			}
			if strings.Contains(strings.ToLower(msg), "not found") {
				return nil, ErrNotFound
			}
			if msg != "" {
				return nil, &ae
			}
		}

		return b, nil
	}
	return nil, lastErr
}

func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

func (c *Client) Post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	return c.do(ctx, http.MethodPost, path, body)
}

func (c *Client) Delete(ctx context.Context, path string) error {
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

// PublishComputeInstance triggers deployment of a compute instance.
func (c *Client) PublishComputeInstance(ctx context.Context, project, workspace, name string) error {
	path := fmt.Sprintf("/apis/paas.envmgmt.io/v1/projects/%s/workspaces/%s/computeinstances/%s/publish", project, workspace, name)
	_, err := c.do(ctx, http.MethodPost, path, nil)
	return err
}

// PublishService triggers deployment of a service.
func (c *Client) PublishService(ctx context.Context, project, workspace, name string) error {
	path := fmt.Sprintf("/apis/paas.envmgmt.io/v1/projects/%s/workspaces/%s/services/%s/publish", project, workspace, name)
	_, err := c.do(ctx, http.MethodPost, path, nil)
	return err
}

func (c *Client) List(ctx context.Context, path string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	limit := 50
	offset := 0
	for {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		url := fmt.Sprintf("%s%slimit=%d&offset=%d", path, sep, limit, offset)
		b, err := c.Get(ctx, url)
		if err != nil {
			return nil, err
		}
		var lr ListResponse
		if err := json.Unmarshal(b, &lr); err != nil {
			return nil, err
		}
		all = append(all, lr.Items...)
		// Use the server's actual page size (metadata.limit) for pagination,
		// since the API may cap the limit lower than requested.
		pageSize := lr.Metadata.Limit
		if pageSize <= 0 {
			pageSize = limit
		}
		if len(lr.Items) < pageSize {
			break
		}
		offset += pageSize
	}
	return all, nil
}

// ComputeInstancePath returns the collection or item path for compute instances.
func ComputeInstancePath(project, workspace, name string) string {
	base := fmt.Sprintf("/apis/paas.envmgmt.io/v1/projects/%s/workspaces/%s/computeinstances", project, workspace)
	if name != "" {
		return base + "/" + name
	}
	return base
}

// ServicePath returns the collection or item path for services.
func ServicePath(project, workspace, name string) string {
	base := fmt.Sprintf("/apis/paas.envmgmt.io/v1/projects/%s/workspaces/%s/services", project, workspace)
	if name != "" {
		return base + "/" + name
	}
	return base
}

// TagGroupPath returns the path for tag groups.
func TagGroupPath(project, name string) string {
	base := fmt.Sprintf("/apis/tags.k8smgmt.io/v3/projects/%s/taggroups", project)
	if name != "" {
		return base + "/" + name
	}
	return base
}

// TagAssociationPath returns the path for project tag associations.
func TagAssociationPath(project, name string) string {
	base := fmt.Sprintf("/apis/tags.k8smgmt.io/v3/projects/%s/projecttagsassociations", project)
	if name != "" {
		return base + "/" + name
	}
	return base
}

// TagGroup represents a named set of key/value tags.
type TagGroup struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   Metadata    `json:"metadata"`
	Spec       TagGroupSpec `json:"spec"`
}

type TagGroupSpec struct {
	Tags []TagKV `json:"tags"`
}

type TagKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TagAssociation associates tags with resources.
type TagAssociation struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   Metadata           `json:"metadata"`
	Spec       TagAssociationSpec `json:"spec"`
}

type TagAssociationSpec struct {
	Associations []TagAssociationEntry `json:"associations"`
}

type TagAssociationEntry struct {
	TagKey   string `json:"tagKey"`
	TagType  string `json:"tagType"`
	TagValue string `json:"tagValue"`
	Resource string `json:"resource"`
}

// TagAssociationList is the list response for tag associations.
type TagAssociationList struct {
	Items []TagAssociation `json:"items"`
}

// TagGroupList is the list response for tag groups.
type TagGroupList struct {
	Items []TagGroup `json:"items"`
}
