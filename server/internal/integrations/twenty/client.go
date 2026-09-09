// Package twenty integrates a workspace with a Twenty CRM instance (OS plan,
// chantier 2): Twenty's own MCP server mounted for the workspace's agents,
// Twenty's signed webhooks turned into triage items, an issue accepted from
// such an item linked back as a Task on the CRM record, and the workspace's
// members matched with Twenty's by email so one identity spans both.
//
// Everything here uses Twenty's public application interfaces — REST,
// GraphQL, webhooks, MCP — which its licence names as free to build on. No
// Twenty code is copied.
package twenty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	clientTimeout    = 15 * time.Second
	maxResponseBytes = 4 << 20
)

// Client is a minimal Twenty API client bound to one instance and one key.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient normalises the base URL (scheme, host, no trailing slash).
func NewClient(baseURL, apiKey string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("base_url must be an http(s) URL")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("api_key is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: clientTimeout}
	}
	return &Client{BaseURL: u.String(), APIKey: strings.TrimSpace(apiKey), HTTP: httpClient}, nil
}

// MCPURL is the instance's MCP endpoint.
func (c *Client) MCPURL() string { return c.BaseURL + "/mcp" }

// RecordURL is where a person opens a record in Twenty.
func (c *Client) RecordURL(objectSingular, id string) string {
	return c.BaseURL + "/object/" + url.PathEscape(objectSingular) + "/" + url.PathEscape(id)
}

// WorkspaceMember is what the members mapping needs.
type WorkspaceMember struct {
	ID        string `json:"id"`
	UserEmail string `json:"userEmail"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// ListWorkspaceMembers reads the instance's members; it doubles as the
// credential check on connect.
func (c *Client) ListWorkspaceMembers(ctx context.Context) ([]WorkspaceMember, error) {
	var out struct {
		Data struct {
			WorkspaceMembers []struct {
				ID        string `json:"id"`
				UserEmail string `json:"userEmail"`
				Name      struct {
					FirstName string `json:"firstName"`
					LastName  string `json:"lastName"`
				} `json:"name"`
			} `json:"workspaceMembers"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/workspaceMembers?limit=200", nil, &out); err != nil {
		return nil, err
	}
	members := make([]WorkspaceMember, 0, len(out.Data.WorkspaceMembers))
	for _, m := range out.Data.WorkspaceMembers {
		members = append(members, WorkspaceMember{ID: m.ID, UserEmail: strings.ToLower(strings.TrimSpace(m.UserEmail)), FirstName: m.Name.FirstName, LastName: m.Name.LastName})
	}
	return members, nil
}

// Webhook is a Twenty outbound webhook subscription.
type Webhook struct {
	ID         string   `json:"id"`
	TargetURL  string   `json:"targetUrl"`
	Operations []string `json:"operations"`
	Secret     string   `json:"secret"`
}

// CreateWebhook subscribes Twenty to POST the given operations to targetURL.
// Twenty mints the signing secret and returns it once.
func (c *Client) CreateWebhook(ctx context.Context, targetURL string, operations []string, description string) (Webhook, error) {
	var out struct {
		Data struct {
			CreateWebhook Webhook `json:"createWebhook"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	err := c.graphql(ctx, `mutation CreateWebhook($input: CreateWebhookInput!) { createWebhook(input: $input) { id targetUrl operations secret } }`,
		map[string]any{"input": map[string]any{"targetUrl": targetURL, "operations": operations, "description": description}}, &out)
	if err != nil {
		return Webhook{}, err
	}
	if len(out.Errors) > 0 {
		return Webhook{}, fmt.Errorf("twenty: createWebhook: %s", out.Errors[0].Message)
	}
	if out.Data.CreateWebhook.ID == "" {
		return Webhook{}, errors.New("twenty: createWebhook returned no id")
	}
	return out.Data.CreateWebhook, nil
}

// DeleteWebhook removes a subscription; a missing one is not an error.
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	var out struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.graphql(ctx, `mutation DeleteWebhook($id: UUID!) { deleteWebhook(id: $id) { id } }`, map[string]any{"id": id}, &out); err != nil {
		return err
	}
	if len(out.Errors) > 0 && !strings.Contains(strings.ToLower(out.Errors[0].Message), "not found") {
		return fmt.Errorf("twenty: deleteWebhook: %s", out.Errors[0].Message)
	}
	return nil
}

// CreateTask files a Task on the CRM and, when a record is named, links it
// through a taskTarget. Returns the task id.
func (c *Client) CreateTask(ctx context.Context, title, bodyMarkdown, targetObject, targetID string) (string, error) {
	var created struct {
		Data struct {
			CreateTask struct {
				ID string `json:"id"`
			} `json:"createTask"`
		} `json:"data"`
	}
	body := map[string]any{"title": title, "status": "TODO"}
	if bodyMarkdown != "" {
		body["bodyV2"] = map[string]any{"markdown": bodyMarkdown}
	}
	if err := c.do(ctx, http.MethodPost, "/rest/tasks", body, &created); err != nil {
		return "", err
	}
	taskID := created.Data.CreateTask.ID
	if taskID == "" {
		return "", errors.New("twenty: createTask returned no id")
	}
	if field := taskTargetField(targetObject); field != "" && targetID != "" {
		var out map[string]any
		if err := c.do(ctx, http.MethodPost, "/rest/taskTargets", map[string]any{"taskId": taskID, field: targetID}, &out); err != nil {
			return taskID, fmt.Errorf("twenty: task filed but not linked: %w", err)
		}
	}
	return taskID, nil
}

// taskTargetField maps a Twenty object to the taskTarget relation field.
func taskTargetField(objectSingular string) string {
	switch objectSingular {
	case "person":
		return "targetPersonId"
	case "company":
		return "targetCompanyId"
	case "opportunity":
		return "targetOpportunityId"
	}
	return ""
}

func (c *Client) graphql(ctx context.Context, query string, variables map[string]any, out any) error {
	return c.do(ctx, http.MethodPost, "/graphql", map[string]any{"query": query, "variables": variables}, out)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("twenty: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("twenty: %s %s: read: %w", method, path, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("twenty: the API key was refused")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("twenty: %s %s: HTTP %d: %s", method, path, resp.StatusCode, truncate(string(raw), 300))
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("twenty: %s %s: not JSON: %w", method, path, err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
