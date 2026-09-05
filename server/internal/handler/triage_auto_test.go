package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Triage auto-ML (K61): resolved items teach the classifier; a suggestion
// carries its confidence and neighbours; nothing is applied below the
// threshold or while the workspace keeps it off; an auto-dismissed item is
// reopened in one call.

func resolveTriage(t *testing.T, id, state string) {
	t.Helper()
	if state == "accepted" {
		issue := dbfx.Issue(t, "auto accepted example")
		dbfx.Exec(t, `UPDATE triage_item SET state = 'accepted', issue_id = $2, resolved_at = now(), resolved_by_type = 'member', resolved_by_id = $3 WHERE id = $1`, id, issue, testUserID)
		return
	}
	dbfx.Exec(t, `UPDATE triage_item SET state = $2, resolution_reason = 'example', resolved_at = now(), resolved_by_type = 'member', resolved_by_id = $3 WHERE id = $1`, id, state, testUserID)
}

func suggestionFor(t *testing.T, id string) TriageSuggestion {
	t.Helper()
	var out struct {
		Suggestions map[string]TriageSuggestion `json:"suggestions"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.GetTriageSuggestions),
		testutil.WithHeaders(newRequest(http.MethodGet, "/api/triage/suggestions?ids="+id, nil), "X-Workspace-ID", testWorkspaceID)).Want(http.StatusOK).JSON(&out)
	return out.Suggestions[id]
}

func TestTriageAutoSuggestsAppliesAndReopens(t *testing.T) {
	rememberSettings(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM triage_item WHERE workspace_id = $1 AND (title LIKE 'auto %' OR title LIKE 'zzqx%')`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM triage_source WHERE workspace_id = $1 AND name = 'Sentry alerts'`, testWorkspaceID)
	})
	// Examples: dependabot bumps dismissed, checkout crashes accepted.
	for i := 0; i < 6; i++ {
		resolveTriage(t, parkDelivery(t, fmt.Sprintf("auto chore(deps): bump lodash %d by dependabot", i), `{"sender":"dependabot"}`), "dismissed")
		resolveTriage(t, parkDelivery(t, fmt.Sprintf("auto NullPointer crash in checkout %d", i), `{"level":"critical"}`), "accepted")
	}
	fresh := parkDelivery(t, "auto chore(deps): bump axios by dependabot", `{"sender":"dependabot"}`)
	s := suggestionFor(t, fresh)
	if s.Suggested != "dismiss" || s.Confidence < 0.9 || len(s.Neighbors) == 0 || s.Examples < 12 {
		t.Fatalf("suggestion = %+v, want a confident dismiss", s)
	}
	if s.Ready {
		t.Fatalf("12 examples must be under the default minimum of 20: %+v", s)
	}
	// Off, or under the minimum: never applied.
	testHandler.ApplyTriageRules(context.Background(), mustTriageItem(t, fresh))
	if state := triageState(t, fresh); state != "pending" {
		t.Fatalf("state with auto off = %s", state)
	}
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"triage_auto":{"enabled":true,"threshold":0.8,"min_examples":10}}' WHERE id = $1`, testWorkspaceID)
	if cfg := service.TriageAutoSettings([]byte(`{"triage_auto":{"enabled":true,"threshold":0.8,"min_examples":10}}`)); !cfg.Enabled || cfg.Threshold != 0.8 || cfg.MinExamples != 10 {
		t.Fatalf("settings = %+v", cfg)
	}
	testHandler.ApplyTriageRules(context.Background(), mustTriageItem(t, fresh))
	if state := triageState(t, fresh); state != "dismissed" {
		t.Fatalf("state with auto on = %s, want dismissed", state)
	}
	var reason string
	dbfx.QueryRow(t, `SELECT resolution_reason FROM triage_item WHERE id = $1`, fresh).Scan(&reason)
	if reason == "" || reason[:5] != "auto:" {
		t.Fatalf("reason = %q", reason)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2`, AuditTriageAutoDecided, fresh); n != 1 {
		t.Fatalf("audit = %d", n)
	}
	// A crash is accepted with a low-threshold auto.
	// dbfx.Issue rows do not bump the workspace counter; IssueService.Create does.
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID)
	crash := parkDelivery(t, "auto NullPointer crash in checkout again", `{"level":"critical"}`)
	cs := suggestionFor(t, crash)
	if cs.Suggested != "accept" || cs.Confidence < 0.8 {
		t.Fatalf("crash suggestion = %+v, want a confident accept", cs)
	}
	testHandler.ApplyTriageRules(context.Background(), mustTriageItem(t, crash))
	if state := triageState(t, crash); state != "accepted" {
		res := testHandler.acceptTriageItemCore(context.Background(), parseUUID(testWorkspaceID), testUserID, parseUUID(crash), triageAcceptOverrides{})
		t.Fatalf("crash state = %s, want accepted (direct accept outcome now: %s)", state, res.outcome)
	}
	// Reopen the auto-dismissed item; a pending one cannot be reopened.
	reopen := func(id string) *testutil.Response {
		return testutil.Call(t, inboxWorkspaceHandler(testHandler.ReopenTriageItem),
			testutil.WithURLParams(testutil.WithHeaders(newRequest(http.MethodPost, "/api/triage/items/"+id+"/reopen", nil), "X-Workspace-ID", testWorkspaceID), "id", id))
	}
	reopen(fresh).Want(http.StatusOK)
	if state := triageState(t, fresh); state != "pending" {
		t.Fatalf("reopened state = %s", state)
	}
	reopen(fresh).Want(http.StatusConflict)
	// An unknown-looking title has no neighbours and no suggestion.
	blank := parkDelivery(t, "zzqx wibble", `{}`)
	if s := suggestionFor(t, blank); s.Suggested != "" {
		t.Fatalf("no neighbours must give no suggestion: %+v", s)
	}
}
