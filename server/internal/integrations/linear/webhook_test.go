package linear

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"
)

// The signature is the only credential the webhook endpoint has, so every
// branch of it is exercised here rather than through the HTTP handler.

func sign(t *testing.T, body []byte, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestLinearVerifySignature(t *testing.T) {
	body := []byte(`{"action":"create"}`)
	good := sign(t, body, "s3cret")

	cases := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{"valid", body, good, "s3cret", true},
		{"wrong secret", body, good, "other", false},
		{"tampered body", []byte(`{"action":"remove"}`), good, "s3cret", false},
		{"empty signature", body, "", "s3cret", false},
		{"non-hex signature", body, "zzzz", "s3cret", false},
		// An installation whose secret failed to decrypt must reject every
		// delivery. Accepting them would turn a decryption bug into an open
		// unauthenticated write endpoint.
		{"empty secret", body, good, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifySignature(tc.body, tc.signature, []byte(tc.secret)); got != tc.want {
				t.Fatalf("VerifySignature = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLinearParseWebhookRejectsStaleAndUnsigned(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	fresh := fmt.Sprintf(`{"action":"create","type":"Issue","webhookTimestamp":%d,"data":{"id":"iss_1"}}`, now.UnixMilli())
	stale := fmt.Sprintf(`{"action":"create","type":"Issue","webhookTimestamp":%d,"data":{"id":"iss_1"}}`, now.Add(-5*time.Minute).UnixMilli())
	undated := `{"action":"create","type":"Issue","data":{"id":"iss_1"}}`

	if _, err := ParseWebhook([]byte(fresh), sign(t, []byte(fresh), "k"), []byte("k"), now); err != nil {
		t.Fatalf("fresh delivery: %v", err)
	}
	if _, err := ParseWebhook([]byte(stale), sign(t, []byte(stale), "k"), []byte("k"), now); !errors.Is(err, ErrStaleWebhook) {
		t.Fatalf("stale delivery: got %v, want ErrStaleWebhook", err)
	}
	// The timestamp lives inside the signed body, so its absence means the
	// sender is not Linear even though the bytes verify.
	if _, err := ParseWebhook([]byte(undated), sign(t, []byte(undated), "k"), []byte("k"), now); !errors.Is(err, ErrStaleWebhook) {
		t.Fatalf("undated delivery: got %v, want ErrStaleWebhook", err)
	}
	if _, err := ParseWebhook([]byte(fresh), "deadbeef", []byte("k"), now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("bad signature: got %v, want ErrBadSignature", err)
	}
}

func TestLinearWebhookPayloadShapes(t *testing.T) {
	now := time.Now()
	issueBody := fmt.Sprintf(`{
      "action":"update","type":"Issue","organizationId":"org_1","webhookTimestamp":%d,
      "data":{"id":"iss_1","identifier":"ENG-7","title":"Ship it","description":"body",
              "url":"https://linear.app/x/issue/ENG-7","team":{"id":"team_1"},
              "state":{"id":"st_1","name":"In Progress","type":"started"},
              "assignee":{"id":"user_app","name":"Multica"}}}`, now.UnixMilli())
	ev, err := ParseWebhook([]byte(issueBody), sign(t, []byte(issueBody), "k"), []byte("k"), now)
	if err != nil {
		t.Fatalf("parse issue: %v", err)
	}
	data, err := ev.IssuePayload()
	if err != nil {
		t.Fatalf("issue payload: %v", err)
	}
	// team / assignee arrive nested here and flat elsewhere; the resolvers are
	// what stop that difference from reaching the bridge.
	if got := data.ResolvedTeamID(); got != "team_1" {
		t.Fatalf("team id = %q, want team_1", got)
	}
	if got := data.ResolvedAssigneeID(); got != "user_app" {
		t.Fatalf("assignee id = %q, want user_app", got)
	}
	if got := data.StateType(); got != "started" {
		t.Fatalf("state type = %q, want started", got)
	}

	commentBody := fmt.Sprintf(`{
      "action":"create","type":"Comment","organizationId":"org_1","webhookTimestamp":%d,
      "data":{"id":"c_1","body":"looks good","issueId":"iss_1","user":{"id":"user_h","name":"Dana"}}}`, now.UnixMilli())
	ev, err = ParseWebhook([]byte(commentBody), sign(t, []byte(commentBody), "k"), []byte("k"), now)
	if err != nil {
		t.Fatalf("parse comment: %v", err)
	}
	c, err := ev.CommentPayload()
	if err != nil {
		t.Fatalf("comment payload: %v", err)
	}
	if c.ResolvedIssueID() != "iss_1" || c.ResolvedUserID() != "user_h" || c.AuthorName() != "Dana" {
		t.Fatalf("comment resolved to %+v", c)
	}
}
