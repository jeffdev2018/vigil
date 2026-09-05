package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Module ownership (K33): rules suggest, humans assign.

func TestOwnershipGlobAndPathExtraction(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"packages/core/billing", "packages/core/billing/invoice.ts", true},
		{"packages/core/billing", "packages/core/billing", true},
		{"packages/core/billing", "packages/core/billings/x.ts", false},
		{"packages/core/billing/**", "packages/core/billing/deep/er/file.ts", true},
		{"packages/core/billing/**", "packages/core/other.ts", false},
		{"**/*.sql", "server/pkg/db/queries/issue.sql", true},
		{"**/*.sql", "issue.sql", true},
		{"server/*/handler/*.go", "server/internal/handler/issue.go", true},
		{"server/*/handler/*.go", "server/internal/handler/sub/issue.go", false},
		{"apps/web/app/?", "apps/web/app/x", true},
	}
	for _, c := range cases {
		re, err := compileGlob(c.pattern)
		if err != nil {
			t.Fatalf("%q: %v", c.pattern, err)
		}
		if got := re.MatchString(c.path); got != c.want {
			t.Fatalf("%q against %q = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
	for _, bad := range []string{"", "   ", "/", "src/[ab]/x"} {
		if _, err := compileGlob(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	paths := extractPaths("Touch packages/core/billing/invoice.ts and `server/internal/handler/issue.go`, see https://example.com/docs and README.md", "feature/billing-export")
	want := map[string]bool{"packages/core/billing/invoice.ts": true, "server/internal/handler/issue.go": true, "README.md": true, "feature/billing-export": true}
	for _, p := range paths {
		if !want[p] {
			t.Fatalf("unexpected path %q in %v", p, paths)
		}
		delete(want, p)
	}
	if len(want) != 0 {
		t.Fatalf("missing paths %v from %v", want, paths)
	}
}

func TestOwnershipResolutionPicksTheMostSpecificRule(t *testing.T) {
	now := time.Now()
	rule := func(pattern, label string, priority int32, age time.Duration) db.ModuleOwnership {
		m := db.ModuleOwnership{ID: dbid7(), Priority: priority, CreatedAt: pgtype.Timestamptz{Time: now.Add(-age), Valid: true}, OwnerUserID: dbid7()}
		if pattern != "" {
			m.PathPattern = pgtype.Text{String: pattern, Valid: true}
		}
		if label != "" {
			m.LabelID = parseUUID(label)
		}
		return m
	}
	labelID := uuid.NewString()
	broad := rule("packages/**", "", 0, time.Hour)
	narrow := rule("packages/core/billing/**", "", 0, time.Hour)
	byLabel := rule("", labelID, 0, time.Hour)
	urgent := rule("packages/**", "", 5, time.Hour)
	newer := rule("packages/core/billing/**", "", 0, time.Minute)
	paths := []string{"packages/core/billing/invoice.ts"}

	if m := resolveOwnership([]db.ModuleOwnership{broad, narrow}, nil, paths); m == nil || m.rule.ID != narrow.ID {
		t.Fatalf("narrow pattern must win, got %+v", m)
	}
	if m := resolveOwnership([]db.ModuleOwnership{narrow, byLabel}, []string{labelID}, paths); m == nil || m.rule.ID != byLabel.ID || m.matched != "label:"+labelID {
		t.Fatalf("a label match must beat a path match, got %+v", m)
	}
	if m := resolveOwnership([]db.ModuleOwnership{narrow, byLabel, urgent}, []string{labelID}, paths); m == nil || m.rule.ID != urgent.ID {
		t.Fatalf("priority must beat everything, got %+v", m)
	}
	if m := resolveOwnership([]db.ModuleOwnership{narrow, newer}, nil, paths); m == nil || m.rule.ID != newer.ID {
		t.Fatalf("the newest rule must win a tie, got %+v", m)
	}
	if m := resolveOwnership([]db.ModuleOwnership{narrow}, nil, []string{"apps/web/page.tsx"}); m != nil {
		t.Fatalf("no match must yield nothing, got %+v", m)
	}
}

func dbid7() pgtype.UUID { return parseUUID(uuid.NewString()) }

func createOwnershipRule(t *testing.T, body map[string]any) *testutil.Response {
	t.Helper()
	req := testutil.WithHeaders(newRequest(http.MethodPost, "/api/module-ownership", body), "X-Workspace-ID", testWorkspaceID)
	return testutil.Call(t, inboxWorkspaceHandler(testHandler.CreateModuleOwnership), req)
}

func TestModuleOwnershipRulesSuggestAndNotify(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM module_ownership WHERE workspace_id = $1`, testWorkspaceID)
	})
	owner := dbfx.User(t, "Owner", "owner-"+uuid.NewString()[:8]+"@multica.ai")
	dbfx.Member(t, testWorkspaceID, owner, "member")
	agent := dbfx.Agent(t, "billing referent", handlerTestRuntimeID(t))
	label := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": "billing-" + uuid.NewString()[:6], "color": "#123456", "resource_type": "issue"})

	// Validation: glob, owner membership, agent, label.
	createOwnershipRule(t, map[string]any{"path_pattern": "src/[ab]", "owner_user_id": owner}).Want(http.StatusBadRequest)
	createOwnershipRule(t, map[string]any{"owner_user_id": owner}).Want(http.StatusBadRequest)
	createOwnershipRule(t, map[string]any{"path_pattern": "x/**", "owner_user_id": uuid.NewString()}).Want(http.StatusNotFound)
	createOwnershipRule(t, map[string]any{"path_pattern": "x/**", "owner_user_id": owner, "referent_agent_id": uuid.NewString()}).Want(http.StatusNotFound)
	createOwnershipRule(t, map[string]any{"label_id": uuid.NewString(), "owner_user_id": owner}).Want(http.StatusNotFound)

	var created struct{ Rule ModuleOwnershipRule }
	createOwnershipRule(t, map[string]any{"path_pattern": "packages/core/billing/**", "owner_user_id": owner, "referent_agent_id": agent}).Want(http.StatusCreated).JSON(&created)
	createOwnershipRule(t, map[string]any{"label_id": label, "owner_user_id": owner, "priority": 1}).Want(http.StatusCreated)

	// Suggestion from the paths the issue names.
	issue := dbfx.Issue(t, "Fix invoice rounding", testutil.Cols{"description": "Bug in packages/core/billing/invoice.ts when rounding."})
	t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue) })
	var out struct{ Suggestion *OwnershipSuggestion }
	testutil.Call(t, testHandler.GetOwnershipSuggestion, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/ownership-suggestion", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
	if out.Suggestion == nil || out.Suggestion.RuleID != created.Rule.ID || out.Suggestion.OwnerUserID != owner || out.Suggestion.ReferentAgentID == nil || *out.Suggestion.ReferentAgentID != agent || out.Suggestion.Matched != "path:packages/core/billing/invoice.ts" {
		t.Fatalf("suggestion = %+v", out.Suggestion)
	}
	// Nothing assigned by itself.
	var assignee *string
	dbfx.QueryRow(t, `SELECT assignee_id::text FROM issue WHERE id = $1`, issue).Scan(&assignee)
	if assignee != nil {
		t.Fatal("a suggestion must not assign")
	}
	// The owner is told once, not when the issue is already theirs.
	testHandler.suggestOwnership(t.Context(), mustIssue(t, issue), "member", testUserID)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'ownership_suggested' AND recipient_id = $2`, issue, owner); n != 1 {
		t.Fatalf("owner inbox items = %d, want 1", n)
	}
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'member', assignee_id = $2 WHERE id = $1`, issue, owner)
	testHandler.suggestOwnership(t.Context(), mustIssue(t, issue), "member", testUserID)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'ownership_suggested'`, issue); n != 1 {
		t.Fatalf("inbox items after assigning to the owner = %d, want still 1", n)
	}

	// The label rule, with its higher priority, wins on a labeled issue.
	labeled := dbfx.Issue(t, "Billing export", testutil.Cols{"description": "packages/core/billing/export.ts"})
	dbfx.InsertNoID(t, "issue_to_label", testutil.Cols{"issue_id": labeled, "label_id": label}, "issue_id = $1 AND label_id = $2", labeled, label)
	testutil.Call(t, testHandler.GetOwnershipSuggestion, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+labeled+"/ownership-suggestion", nil), "id", labeled)).Want(http.StatusOK).JSON(&out)
	if out.Suggestion == nil || out.Suggestion.Matched != "label:"+label {
		t.Fatalf("labeled suggestion = %+v", out.Suggestion)
	}

	// No paths, no labels: nothing, no error.
	bare := dbfx.Issue(t, "Just a title")
	testutil.Call(t, testHandler.GetOwnershipSuggestion, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+bare+"/ownership-suggestion", nil), "id", bare)).Want(http.StatusOK).JSON(&out)
	if out.Suggestion != nil {
		t.Fatalf("bare issue suggestion = %+v, want none", out.Suggestion)
	}

	// Delete: gone, then 404; the assigned issue keeps its assignee.
	del := testutil.WithHeaders(newRequest(http.MethodDelete, "/api/module-ownership/"+created.Rule.ID, nil), "X-Workspace-ID", testWorkspaceID)
	testutil.Call(t, inboxWorkspaceHandler(testHandler.DeleteModuleOwnership), testutil.WithURLParams(del, "id", created.Rule.ID)).Want(http.StatusNoContent)
	testutil.Call(t, inboxWorkspaceHandler(testHandler.DeleteModuleOwnership), testutil.WithURLParams(del, "id", created.Rule.ID)).Want(http.StatusNotFound)
	dbfx.QueryRow(t, `SELECT assignee_id::text FROM issue WHERE id = $1`, issue).Scan(&assignee)
	if assignee == nil || *assignee != owner {
		t.Fatal("deleting a rule must not touch assignments")
	}
	var list struct{ Rules []ModuleOwnershipRule }
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListModuleOwnership), testutil.WithHeaders(newRequest(http.MethodGet, "/api/module-ownership", nil), "X-Workspace-ID", testWorkspaceID)).Want(http.StatusOK).JSON(&list)
	if len(list.Rules) != 1 || list.Rules[0].LabelID == nil {
		t.Fatalf("rules after delete = %+v, want the label rule alone", list.Rules)
	}

	// Workspace deletion purges the rules.
	ws := dbfx.Workspace(t, "Ownership purge", "ownership-purge-"+uuid.NewString())
	dbfx.Insert(t, "module_ownership", testutil.Cols{"workspace_id": ws, "path_pattern": "x/**", "owner_user_id": testUserID})
	if err := testHandler.Queries.DeleteWorkspaceIssueRoots(t.Context(), parseUUID(ws)); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM module_ownership WHERE workspace_id = $1`, ws); n != 0 {
		t.Fatalf("rules after workspace delete = %d, want 0", n)
	}
}

func mustIssue(t *testing.T, id string) db.Issue {
	t.Helper()
	issue, err := testHandler.Queries.GetIssueInWorkspace(t.Context(), db.GetIssueInWorkspaceParams{ID: parseUUID(id), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	return issue
}
