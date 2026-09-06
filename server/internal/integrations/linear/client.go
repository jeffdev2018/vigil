// Package linear is the Linear Bridge (K21): Multica installed as a Linear
// app, so a Linear issue assigned to the app's user becomes a Multica issue
// owned by an agent, and comments + status flow both ways from then on.
//
// client.go is the transport half — a hand-rolled GraphQL client over
// net/http. Linear's API is five queries wide for this feature, so a generated
// client would be more machinery than the feature is.
package linear

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

// DefaultEndpoint is Linear's GraphQL endpoint. Tests point Client.Endpoint at
// an httptest server instead.
const DefaultEndpoint = "https://api.linear.app/graphql"

// ErrUnauthorized means Linear rejected the token (401/403). It is the signal
// that an installation is no longer usable: the caller marks it broken and
// tells the workspace's managers, rather than retrying forever.
var ErrUnauthorized = errors.New("linear: token rejected")

// APIError carries a non-2xx HTTP status or a GraphQL `errors` array.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("linear: api error (status %d): %s", e.Status, e.Message)
	}
	return "linear: api error: " + e.Message
}

// Client talks to one Linear organization with one OAuth access token.
type Client struct {
	HTTP     *http.Client
	Endpoint string
	Token    string
}

// NewClient binds a token to the real Linear endpoint.
func NewClient(token string) *Client {
	return &Client{
		HTTP:     &http.Client{Timeout: 15 * time.Second},
		Endpoint: DefaultEndpoint,
		Token:    token,
	}
}

// API is the slice of Linear the bridge uses. *Client satisfies it; sync tests
// inject a fake so the bridge logic is exercised without HTTP.
type API interface {
	Viewer(ctx context.Context) (Viewer, error)
	Issue(ctx context.Context, id string) (Issue, error)
	CreateComment(ctx context.Context, issueID, body string) (string, error)
	UpdateIssueState(ctx context.Context, issueID, stateID string) error
	ListTeamStates(ctx context.Context, teamID string) ([]WorkflowState, error)
	CreateWebhook(ctx context.Context, url, secret string, resourceTypes []string) (string, error)
	DeleteWebhook(ctx context.Context, id string) error
}

// Viewer is the identity the access token acts as — the app user a Linear
// issue must be assigned to for the bridge to mirror it.
type Viewer struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	OrganizationID   string `json:"-"`
	OrganizationName string `json:"-"`
}

// Issue is the subset of a Linear issue the mirror needs.
type Issue struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Team        struct {
		ID string `json:"id"`
	} `json:"team"`
	State struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"state"`
	Assignee *struct {
		ID string `json:"id"`
	} `json:"assignee"`
}

// WorkflowState is one column of a Linear team's board. Type is the coarse
// lifecycle bucket (`backlog`, `unstarted`, `started`, `completed`,
// `canceled`, `triage`) the status map is keyed on.
type WorkflowState struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Position float64 `json:"position"`
}

// do runs one GraphQL request and decodes `data` into out.
func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	if c.Token == "" {
		return ErrUnauthorized
	}
	payload, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.Token)
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Message: truncate(string(body), 500)}
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return &APIError{Status: resp.StatusCode, Message: "malformed response: " + truncate(string(body), 200)}
	}
	if len(envelope.Errors) > 0 {
		msg := envelope.Errors[0].Message
		// Linear answers an expired or revoked token with HTTP 200 and a
		// GraphQL error, so the auth signal has to be read here too — otherwise
		// a dead installation looks like an ordinary transient failure and is
		// retried forever instead of being reported to the humans who can fix it.
		code := strings.ToUpper(envelope.Errors[0].Extensions.Code)
		if code == "AUTHENTICATION_ERROR" || code == "FORBIDDEN" ||
			strings.Contains(strings.ToLower(msg), "authentication") {
			return ErrUnauthorized
		}
		return &APIError{Message: truncate(msg, 500)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Data, out)
}

const viewerQuery = `query { viewer { id name } organization { id name } }`

// Viewer returns the app user the token acts as, plus its organization.
func (c *Client) Viewer(ctx context.Context) (Viewer, error) {
	var data struct {
		Viewer struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"viewer"`
		Organization struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"organization"`
	}
	if err := c.do(ctx, viewerQuery, nil, &data); err != nil {
		return Viewer{}, err
	}
	return Viewer{
		ID:               data.Viewer.ID,
		Name:             data.Viewer.Name,
		OrganizationID:   data.Organization.ID,
		OrganizationName: data.Organization.Name,
	}, nil
}

const issueQuery = `query($id: String!) {
  issue(id: $id) {
    id identifier title description url
    team { id }
    state { id type name }
    assignee { id }
  }
}`

// Issue re-reads one Linear issue. Resync uses it to reconcile a link whose
// webhook deliveries were missed or refused.
func (c *Client) Issue(ctx context.Context, id string) (Issue, error) {
	var data struct {
		Issue Issue `json:"issue"`
	}
	if err := c.do(ctx, issueQuery, map[string]any{"id": id}, &data); err != nil {
		return Issue{}, err
	}
	if data.Issue.ID == "" {
		return Issue{}, &APIError{Message: "issue not found: " + id}
	}
	return data.Issue, nil
}

const createCommentMutation = `mutation($issueId: String!, $body: String!) {
  commentCreate(input: {issueId: $issueId, body: $body}) {
    success
    comment { id }
  }
}`

// CreateComment posts a comment on a Linear issue and returns its id, which
// the caller records so the webhook echo of this very comment is ignored.
func (c *Client) CreateComment(ctx context.Context, issueID, body string) (string, error) {
	var data struct {
		CommentCreate struct {
			Success bool `json:"success"`
			Comment struct {
				ID string `json:"id"`
			} `json:"comment"`
		} `json:"commentCreate"`
	}
	if err := c.do(ctx, createCommentMutation, map[string]any{"issueId": issueID, "body": body}, &data); err != nil {
		return "", err
	}
	if !data.CommentCreate.Success {
		return "", &APIError{Message: "commentCreate returned success=false"}
	}
	return data.CommentCreate.Comment.ID, nil
}

const updateIssueStateMutation = `mutation($id: String!, $stateId: String!) {
  issueUpdate(id: $id, input: {stateId: $stateId}) { success }
}`

// UpdateIssueState moves a Linear issue to a workflow state.
func (c *Client) UpdateIssueState(ctx context.Context, issueID, stateID string) error {
	var data struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	if err := c.do(ctx, updateIssueStateMutation, map[string]any{"id": issueID, "stateId": stateID}, &data); err != nil {
		return err
	}
	if !data.IssueUpdate.Success {
		return &APIError{Message: "issueUpdate returned success=false"}
	}
	return nil
}

const teamStatesQuery = `query($teamId: String!) {
  team(id: $teamId) {
    states { nodes { id name type position } }
  }
}`

// ListTeamStates returns a team's workflow states, which is how a Multica
// status key becomes a Linear state id.
func (c *Client) ListTeamStates(ctx context.Context, teamID string) ([]WorkflowState, error) {
	var data struct {
		Team struct {
			States struct {
				Nodes []WorkflowState `json:"nodes"`
			} `json:"states"`
		} `json:"team"`
	}
	if err := c.do(ctx, teamStatesQuery, map[string]any{"teamId": teamID}, &data); err != nil {
		return nil, err
	}
	return data.Team.States.Nodes, nil
}

const createWebhookMutation = `mutation($url: String!, $secret: String!, $resourceTypes: [String!]!) {
  webhookCreate(input: {url: $url, secret: $secret, resourceTypes: $resourceTypes, allPublicTeams: true}) {
    success
    webhook { id }
  }
}`

// CreateWebhook registers the org-wide webhook that feeds the bridge. The
// secret is ours, not Linear's: we generate it, seal it, and send it here, so
// signature verification never depends on reading a value back.
func (c *Client) CreateWebhook(ctx context.Context, url, secret string, resourceTypes []string) (string, error) {
	var data struct {
		WebhookCreate struct {
			Success bool `json:"success"`
			Webhook struct {
				ID string `json:"id"`
			} `json:"webhook"`
		} `json:"webhookCreate"`
	}
	vars := map[string]any{"url": url, "secret": secret, "resourceTypes": resourceTypes}
	if err := c.do(ctx, createWebhookMutation, vars, &data); err != nil {
		return "", err
	}
	if !data.WebhookCreate.Success {
		return "", &APIError{Message: "webhookCreate returned success=false"}
	}
	return data.WebhookCreate.Webhook.ID, nil
}

const deleteWebhookMutation = `mutation($id: String!) { webhookDelete(id: $id) { success } }`

// DeleteWebhook removes the webhook registered at install. Disconnect calls it
// best-effort: a webhook we can no longer authenticate against is not a reason
// to refuse the disconnect.
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return c.do(ctx, deleteWebhookMutation, map[string]any{"id": id}, nil)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
