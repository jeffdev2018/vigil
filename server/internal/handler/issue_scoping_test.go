package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Issue scoping assistant (K14): a reviewed proposal, never a created issue;
// the model's shape is normalized and a broken answer is a clean refusal.

func proposeScoping(t *testing.T, body map[string]any) *scopingCall {
	t.Helper()
	return &scopingCall{t: t, body: body}
}

type scopingCall struct {
	t    *testing.T
	body map[string]any
}

// do returns the status and the decoded body (Response embeds the recorder).
func (c *scopingCall) do() (int, map[string]any) {
	c.t.Helper()
	req := testutil.WithHeaders(newRequest(http.MethodPost, "/api/issues/scoping/propose", c.body), "X-Workspace-ID", testWorkspaceID)
	resp := testutil.Call(c.t, inboxWorkspaceHandler(testHandler.ProposeIssueScoping), req)
	return resp.Code, resp.Map()
}

func TestIssueScopingProposesAndNormalizes(t *testing.T) {
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"title":"  Add   CSV export  ","description":"## Context\nUsers want their issues out.","acceptance_criteria":["Export button on the list","","  Archived issues excluded  "],"probable_files":[{"path":"server/internal/handler/issue.go","reason":"list endpoint"},{"path":"   "}]}`))
	status, out := proposeScoping(t, map[string]any{"raw_text": "Users want to export their issues as CSV, without archived ones."}).do()
	if status != http.StatusOK {
		t.Fatalf("status = %d: %v", status, out)
	}
	p := out["proposal"].(map[string]any)
	if p["title"] != "Add CSV export" {
		t.Fatalf("title = %q", p["title"])
	}
	criteria := p["acceptance_criteria"].([]any)
	if len(criteria) != 2 || criteria[1] != "Archived issues excluded" {
		t.Fatalf("criteria = %v", criteria)
	}
	files := p["probable_files"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["path"] != "server/internal/handler/issue.go" {
		t.Fatalf("files = %v", files)
	}
	// No issue was created by proposing.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND title = 'Add CSV export'`, testWorkspaceID); n != 0 {
		t.Fatalf("issues created by a proposal = %d, want 0", n)
	}
}

func TestIssueScopingFallsBackToRawTextAndRefusesBadAnswers(t *testing.T) {
	// A model answer without title/description still yields a draft.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"acceptance_criteria":["x"]}`))
	raw := "Fix the login page\nThe button does nothing on Safari."
	status, out := proposeScoping(t, map[string]any{"raw_text": raw}).do()
	p := out["proposal"].(map[string]any)
	if status != http.StatusOK || p["title"] != "Fix the login page" || p["description"] != raw {
		t.Fatalf("fallback = %d %v", status, p)
	}

	// Not JSON at all: a clean 502 with a stable code.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `Sure! Here is your issue: ...`))
	status, out = proposeScoping(t, map[string]any{"raw_text": raw}).do()
	if status != http.StatusBadGateway || out["code"] != ErrCodeScopingMalformed {
		t.Fatalf("malformed = %d %v", status, out)
	}

	// Upstream failure.
	withStubLLM(t, stubLLMCompletion(t, http.StatusInternalServerError, ""))
	status, out = proposeScoping(t, map[string]any{"raw_text": raw}).do()
	if status != http.StatusBadGateway || out["code"] != ErrCodeLLMUnavailable {
		t.Fatalf("upstream failure = %d %v", status, out)
	}

	// Validation: empty and oversized text.
	if status, _ := proposeScoping(t, map[string]any{"raw_text": "   "}).do(); status != http.StatusBadRequest {
		t.Fatalf("empty text = %d", status)
	}
	if status, _ := proposeScoping(t, map[string]any{"raw_text": strings.Repeat("x", scopingMaxTextRunes+1)}).do(); status != http.StatusBadRequest {
		t.Fatalf("oversized text = %d", status)
	}
}

func TestIssueScopingWithoutLLMIsUnavailable(t *testing.T) {
	// testHandler.LLM is the disabled client by default.
	status, out := proposeScoping(t, map[string]any{"raw_text": "anything"}).do()
	if status != http.StatusServiceUnavailable || out["code"] != ErrCodeLLMUnavailable {
		t.Fatalf("disabled llm = %d %v", status, out)
	}
}
