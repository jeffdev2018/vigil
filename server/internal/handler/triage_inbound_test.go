package handler

// Email intake: the token in the path is the whole credential, so the tests
// that matter are the ones about a token that should not work.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/triage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func mintEmailIntake(t *testing.T) TriageEmailSourceResponse {
	t.Helper()
	cleanupTriageSourceKind(t, triage.SourceEmail, testWorkspaceID)
	var out TriageEmailSourceResponse
	testutil.Call(t, testHandler.CreateTriageEmailSource,
		newRequest(http.MethodPost, "/api/triage/sources/email", map[string]any{}),
	).Want(http.StatusCreated).JSON(&out)
	return out
}

func postInboundEmail(t *testing.T, token string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.HandleInboundTriageEmail, testutil.WithURLParams(
		newRequest(http.MethodPost, inboundEmailPathForToken(token), body), "token", token,
	))
}

func TestEmailIntakeMintsAGatedSourceAndReturnsTheTokenOnce(t *testing.T) {
	out := mintEmailIntake(t)

	if !strings.HasPrefix(out.Token, emailTokenPrefix) {
		t.Fatalf("token = %q, want the %s prefix", out.Token, emailTokenPrefix)
	}
	if out.Mode != string(triage.ModeGate) {
		t.Fatalf("mode = %q — inbound email is the least authenticated material there is; it must be gated", out.Mode)
	}
	if !strings.HasSuffix(out.Path, out.Token) {
		t.Fatalf("path = %q, want it to carry the token", out.Path)
	}

	// Only the digest is stored: a database dump must not hand somebody a
	// working inbox.
	src, err := testHandler.Queries.GetTriageSourceByRef(context.Background(), db.GetTriageSourceByRefParams{
		WorkspaceID: parseUUID(testWorkspaceID), Kind: triage.SourceEmail, RefID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load email source: %v", err)
	}
	if src.TokenHash == "" || src.TokenHash == out.Token {
		t.Fatalf("token_hash = %q, want a digest that is not the token itself", src.TokenHash)
	}
}

func TestInboundEmailRejectsAnUnknownToken(t *testing.T) {
	mintEmailIntake(t)
	postInboundEmail(t, emailTokenPrefix+"deadbeefdeadbeefdeadbeef", map[string]any{
		"from": "reporter@example.com", "subject": "Should not land",
	}).Want(http.StatusNotFound)
}

func TestInboundEmailRejectsAnEmptyMessage(t *testing.T) {
	out := mintEmailIntake(t)
	for _, body := range []map[string]any{
		{"subject": "no sender"},
		{"from": "reporter@example.com"},
		{"from": "  ", "text": "  "},
	} {
		postInboundEmail(t, out.Token, body).Want(http.StatusBadRequest)
	}
}

func TestInboundEmailQueuesTheMessage(t *testing.T) {
	out := mintEmailIntake(t)

	postInboundEmail(t, out.Token, map[string]any{
		"from":       "reporter@example.com",
		"subject":    "Checkout returns 500 on Safari",
		"text":       "Since this morning, paying with Apple Pay fails.",
		"html":       "<p>Since this morning…</p>",
		"message_id": "<abc@example.com>",
	}).Want(http.StatusAccepted)

	items := triageItemsForSource(t, triage.SourceEmail, testWorkspaceID)
	if len(items) != 1 {
		t.Fatalf("queued items = %d, want 1", len(items))
	}
	if items[0].Title != "Checkout returns 500 on Safari" {
		t.Fatalf("title = %q, want the subject", items[0].Title)
	}
	if items[0].State != triage.StatePending || items[0].Shadow {
		t.Fatalf("item = state %q shadow %v, want a real pending queue row", items[0].State, items[0].Shadow)
	}

	var body string
	if err := testPool.QueryRow(context.Background(),
		`SELECT body_markdown FROM triage_item WHERE id = $1`, items[0].ID).Scan(&body); err != nil {
		t.Fatalf("reload body: %v", err)
	}
	if !strings.Contains(body, "reporter@example.com") || !strings.Contains(body, "Apple Pay") {
		t.Fatalf("body = %q, want the sender and the text", body)
	}
}

func TestInboundEmailFallsBackToTheFirstLineWhenThereIsNoSubject(t *testing.T) {
	out := mintEmailIntake(t)
	postInboundEmail(t, out.Token, map[string]any{
		"from": "reporter@example.com",
		"text": "Payments are down\n\nfull details follow",
	}).Want(http.StatusAccepted)

	items := triageItemsForSource(t, triage.SourceEmail, testWorkspaceID)
	if len(items) != 1 || items[0].Title != "Payments are down" {
		t.Fatalf("items = %+v, want one titled from the first line", items)
	}
}

func TestRotatingTheIntakeTokenRevokesThePreviousOne(t *testing.T) {
	first := mintEmailIntake(t)
	var second TriageEmailSourceResponse
	testutil.Call(t, testHandler.CreateTriageEmailSource,
		newRequest(http.MethodPost, "/api/triage/sources/email", map[string]any{}),
	).Want(http.StatusCreated).JSON(&second)

	if second.Token == first.Token {
		t.Fatal("rotation must mint a new token")
	}
	if second.ID != first.ID {
		t.Fatalf("rotation created a second source (%s then %s); there is one inbox per workspace", first.ID, second.ID)
	}
	postInboundEmail(t, first.Token, map[string]any{
		"from": "reporter@example.com", "subject": "After rotation",
	}).Want(http.StatusNotFound)
	postInboundEmail(t, second.Token, map[string]any{
		"from": "reporter@example.com", "subject": "After rotation",
	}).Want(http.StatusAccepted)
}

func TestBlockedIntakeRecordsTheRefusalWithoutTellingTheSender(t *testing.T) {
	out := mintEmailIntake(t)
	setTriageSourceModeForKind(t, triage.SourceEmail, testWorkspaceID, string(triage.ModeBlocked))

	postInboundEmail(t, out.Token, map[string]any{
		"from": "spammer@example.com", "subject": "Refused",
	}).Want(http.StatusAccepted)

	var dropped int
	for _, item := range triageItemsForSource(t, triage.SourceEmail, testWorkspaceID) {
		if item.State == triage.StateDropped {
			dropped++
		}
	}
	if dropped != 1 {
		t.Fatalf("dropped audit rows = %d, want 1", dropped)
	}
}
