package linear

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Webhook signature + payload parsing.
//
// Linear signs the raw request body with HMAC-SHA256 under the secret we gave
// it at webhookCreate and sends the hex digest in `Linear-Signature`. The
// payload repeats its own `webhookTimestamp`, which is inside the signed body:
// checking it is what stops a captured delivery from being replayed later.

// SignatureHeader is the header Linear signs its deliveries with.
const SignatureHeader = "Linear-Signature"

// MaxWebhookAge is how stale a delivery may be. Linear's own guidance is to
// reject anything older than a minute.
const MaxWebhookAge = 60 * time.Second

var (
	// ErrBadSignature means the body did not verify under the installation's
	// secret. The caller answers 401 and stores nothing.
	ErrBadSignature = errors.New("linear: webhook signature mismatch")
	// ErrStaleWebhook means the payload's own timestamp is outside
	// MaxWebhookAge — a replay of a delivery that did verify once.
	ErrStaleWebhook = errors.New("linear: webhook timestamp too old")
)

// WebhookEvent is the delivery envelope. `data` stays raw so one parse serves
// both Issue and Comment payloads.
type WebhookEvent struct {
	Action           string          `json:"action"` // create | update | remove
	Type             string          `json:"type"`   // Issue | Comment
	CreatedAt        string          `json:"createdAt"`
	Data             json.RawMessage `json:"data"`
	UpdatedFrom      json.RawMessage `json:"updatedFrom"`
	URL              string          `json:"url"`
	WebhookTimestamp int64           `json:"webhookTimestamp"` // unix millis
	WebhookID        string          `json:"webhookId"`
	OrganizationID   string          `json:"organizationId"`
}

// IssueData is the `data` of an Issue delivery.
type IssueData struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	TeamID      string `json:"teamId"`
	AssigneeID  string `json:"assigneeId"`
	UpdatedAt   string `json:"updatedAt"`
	Team        *struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	} `json:"team"`
	State *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"state"`
	Assignee *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"assignee"`
}

// ResolvedTeamID prefers the nested team object and falls back to the flat
// field, because Linear sends one or the other depending on the event.
func (d IssueData) ResolvedTeamID() string {
	if d.TeamID != "" {
		return d.TeamID
	}
	if d.Team != nil {
		return d.Team.ID
	}
	return ""
}

// ResolvedAssigneeID does the same for the assignee, which is what decides
// whether an issue is ours to mirror.
func (d IssueData) ResolvedAssigneeID() string {
	if d.AssigneeID != "" {
		return d.AssigneeID
	}
	if d.Assignee != nil {
		return d.Assignee.ID
	}
	return ""
}

// StateType is the coarse lifecycle bucket the status map is keyed on.
func (d IssueData) StateType() string {
	if d.State == nil {
		return ""
	}
	return d.State.Type
}

// CommentData is the `data` of a Comment delivery.
type CommentData struct {
	ID      string `json:"id"`
	Body    string `json:"body"`
	IssueID string `json:"issueId"`
	UserID  string `json:"userId"`
	Issue   *struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
		TeamID     string `json:"teamId"`
	} `json:"issue"`
	User *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"user"`
}

// ResolvedIssueID prefers the flat field and falls back to the nested issue.
func (d CommentData) ResolvedIssueID() string {
	if d.IssueID != "" {
		return d.IssueID
	}
	if d.Issue != nil {
		return d.Issue.ID
	}
	return ""
}

// ResolvedUserID identifies the Linear author. When it is the app's own user,
// the comment is one we pushed and must not be mirrored back.
func (d CommentData) ResolvedUserID() string {
	if d.UserID != "" {
		return d.UserID
	}
	if d.User != nil {
		return d.User.ID
	}
	return ""
}

// AuthorName is what the mirrored Multica comment is prefixed with, so a
// reader can tell whose words they are.
func (d CommentData) AuthorName() string {
	if d.User != nil && d.User.Name != "" {
		return d.User.Name
	}
	return "Linear"
}

// VerifySignature checks the hex HMAC-SHA256 of body under secret. It is
// constant-time, and an empty secret never verifies — an installation whose
// secret failed to decrypt must reject deliveries, not accept every one.
func VerifySignature(body []byte, signature string, secret []byte) bool {
	if len(secret) == 0 || signature == "" {
		return false
	}
	want, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

// ParseWebhook verifies the signature, rejects a stale delivery and returns
// the parsed envelope. `now` is injected so the freshness check is testable.
func ParseWebhook(body []byte, signature string, secret []byte, now time.Time) (WebhookEvent, error) {
	if !VerifySignature(body, signature, secret) {
		return WebhookEvent{}, ErrBadSignature
	}
	var ev WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return WebhookEvent{}, err
	}
	// A delivery with no timestamp is not trusted to be fresh: the field is
	// inside the signed body, so its absence means the sender is not Linear.
	if ev.WebhookTimestamp == 0 {
		return WebhookEvent{}, ErrStaleWebhook
	}
	sent := time.UnixMilli(ev.WebhookTimestamp)
	if age := now.Sub(sent); age > MaxWebhookAge || age < -MaxWebhookAge {
		return WebhookEvent{}, ErrStaleWebhook
	}
	return ev, nil
}

// IssuePayload decodes an Issue delivery's data.
func (e WebhookEvent) IssuePayload() (IssueData, error) {
	var d IssueData
	err := json.Unmarshal(e.Data, &d)
	return d, err
}

// CommentPayload decodes a Comment delivery's data.
func (e WebhookEvent) CommentPayload() (CommentData, error) {
	var d CommentData
	err := json.Unmarshal(e.Data, &d)
	return d, err
}
