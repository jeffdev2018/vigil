package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Voice-dictated issue draft (K36): a draft is returned, never an issue, and
// a broken or absent LLM degrades to the deterministic draft rather than a
// refusal — dictation happens away from a keyboard.

func postVoiceTranscript(t *testing.T, body map[string]any) *testutil.Response {
	t.Helper()
	req := testutil.WithHeaders(newRequest(http.MethodPost, "/api/issues/from-voice-transcript", body), "X-Workspace-ID", testWorkspaceID)
	return testutil.Call(t, inboxWorkspaceHandler(testHandler.ProposeIssueFromVoiceTranscript), req)
}

// seedVoiceLabel adds one issue-scoped workspace label and returns its name.
func seedVoiceLabel(t *testing.T, name string) string {
	t.Helper()
	dbfx.Insert(t, "issue_label", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"resource_type": "issue",
		"name":          name,
		"color":         "#3b82f6",
	})
	return name
}

func TestIssueFromVoiceRejectsShortTranscript(t *testing.T) {
	// Fewer than 8 non-space characters is a mis-tap, not a command. The
	// stub LLM proves the refusal happens before any model call: a call
	// would return this title.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"title":"the model was called","description":"x"}`))
	for _, transcript := range []string{"", "   ", "fix it", "f i x   i t"} {
		out := postVoiceTranscript(t, map[string]any{"transcript": transcript}).Want(http.StatusBadRequest).Map()
		if out["code"] != ErrCodeTranscriptTooShort {
			t.Fatalf("transcript %q: code = %v, want %s", transcript, out["code"], ErrCodeTranscriptTooShort)
		}
		if out["title"] != nil {
			t.Fatalf("transcript %q: a rejected draft must not carry model output: %v", transcript, out)
		}
	}
}

func TestIssueFromVoiceFallsBackWithoutLLM(t *testing.T) {
	// testHandler.LLM is the disabled client by default — the fallback path.
	label := seedVoiceLabel(t, "voice-fallback-"+uuid.NewString()[:8])
	transcript := "The export button is broken on Safari. It does nothing when you click it, tag it " + label + "."

	var draft VoiceIssueDraft
	postVoiceTranscript(t, map[string]any{"transcript": transcript}).Want(http.StatusOK).JSON(&draft)

	if draft.Title != "The export button is broken on Safari" {
		t.Fatalf("title = %q, want the first sentence", draft.Title)
	}
	if draft.Description != transcript {
		t.Fatalf("description = %q, want the whole transcript", draft.Description)
	}
	if len(draft.SuggestedLabels) != 1 || draft.SuggestedLabels[0] != label {
		t.Fatalf("suggested_labels = %v, want the label named out loud (%s)", draft.SuggestedLabels, label)
	}
	// Drafting creates nothing.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, draft.Title); n != 0 {
		t.Fatalf("issues created by a draft = %d, want 0", n)
	}
}

func TestIssueFromVoiceDropsLabelsTheWorkspaceDoesNotHave(t *testing.T) {
	known := seedVoiceLabel(t, "voice-known-"+uuid.NewString()[:8])
	// The model answers with one real label (in the wrong casing), one it
	// invented, and a duplicate.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK,
		`{"title":"  Fix   the export  ","description":"Broken on Safari.","suggested_labels":["`+strings.ToUpper(known)+`","totally-invented","`+known+`"]}`))

	var draft VoiceIssueDraft
	postVoiceTranscript(t, map[string]any{"transcript": "the export button does nothing on Safari"}).Want(http.StatusOK).JSON(&draft)

	if draft.Title != "Fix the export" {
		t.Fatalf("title = %q, want the collapsed model title", draft.Title)
	}
	if len(draft.SuggestedLabels) != 1 || draft.SuggestedLabels[0] != known {
		t.Fatalf("suggested_labels = %v, want only %q in the workspace's own casing", draft.SuggestedLabels, known)
	}
}

func TestIssueFromVoiceDegradesWhenTheModelFails(t *testing.T) {
	transcript := "Rate limiting is missing on the login endpoint. Add it before the release."

	// Not JSON at all, and an upstream failure: both yield the deterministic
	// draft rather than an error, unlike the scoping assistant (K14).
	for _, stub := range []*struct {
		name   string
		status int
		body   string
	}{
		{"malformed answer", http.StatusOK, "Sure! Here is your issue: ..."},
		{"upstream failure", http.StatusInternalServerError, ""},
	} {
		withStubLLM(t, stubLLMCompletion(t, stub.status, stub.body))
		var draft VoiceIssueDraft
		postVoiceTranscript(t, map[string]any{"transcript": transcript}).Want(http.StatusOK).JSON(&draft)
		if draft.Title != "Rate limiting is missing on the login endpoint" || draft.Description != transcript {
			t.Fatalf("%s: draft = %+v, want the deterministic fallback", stub.name, draft)
		}
	}
}

func TestVoiceDraftTitleCapsOnAWordBoundary(t *testing.T) {
	// A dictation with no sentence terminator still yields a short title.
	long := strings.Repeat("word ", 40)
	title := voiceDraftTitle(long)
	if len([]rune(title)) > voiceTitleMaxRunes {
		t.Fatalf("title is %d runes, want at most %d: %q", len([]rune(title)), voiceTitleMaxRunes, title)
	}
	if strings.HasSuffix(title, " ") || !strings.HasSuffix(title, "word") {
		t.Fatalf("title = %q, want a cut on a word boundary", title)
	}
}

// TestCreateIssueAcceptsVoiceMobileOriginWithoutOriginID locks the one
// human-settable origin (K36, migration 738): the mobile draft screen stamps
// provenance with no origin_id, because a transcript is not a stored row. A
// supplied origin_id under that label is rejected rather than ignored.
func TestCreateIssueAcceptsVoiceMobileOriginWithoutOriginID(t *testing.T) {
	var created IssueResponse
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Dictated on the phone (K36)",
		"origin_type": IssueOriginVoiceMobile,
	})).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, created.ID)

	var originType, originID string
	dbfx.QueryRow(t, `SELECT COALESCE(origin_type, ''), COALESCE(origin_id::text, '') FROM issue WHERE id = $1`, created.ID).Scan(&originType, &originID)
	if originType != IssueOriginVoiceMobile || originID != "" {
		t.Fatalf("origin = (%q, %q), want (%q, no id)", originType, originID, IssueOriginVoiceMobile)
	}

	// origin_id under this label, and an origin nobody allow-lists, are 400s.
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Dictated with a smuggled id",
		"origin_type": IssueOriginVoiceMobile,
		"origin_id":   uuid.NewString(),
	})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Minted origin",
		"origin_type": "autopilot",
		"origin_id":   uuid.NewString(),
	})).Want(http.StatusBadRequest)
}
