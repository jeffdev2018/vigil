package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/insight"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F27 insights. The DSL vocabulary matrix — one case per rejected enum, one
// injection attempt per free-text field — is canonical in
// internal/insight/dsl_test.go and is NOT replayed here. This file asserts the
// wiring: status codes, workspace scoping, visibility, concurrency, the log
// row, and the agent-visibility redaction.

func insightRun(t *testing.T, query any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.RunInsight,
		newRequest("POST", "/api/insights/run", map[string]any{"query": query}))
}

func insightValue(t *testing.T, resp *testutil.Response) float64 {
	t.Helper()
	var out InsightRunResponse
	resp.Want(http.StatusOK).JSON(&out)
	if len(out.Rows) != 1 {
		t.Fatalf("rows = %v, want exactly one aggregate row", out.Rows)
	}
	value, ok := out.Rows[0]["value"].(float64)
	if !ok {
		t.Fatalf("value = %v (%T), want a number", out.Rows[0]["value"], out.Rows[0]["value"])
	}
	return value
}

// Acceptance 1: the compiled answer equals a hand-written count of the same
// question. "Blocked more than 5 days" is the v1 approximation the DSL
// actually offers — blocked status, untouched for five days — and the
// hand-written SQL below spells out the same thing.
func TestInsightRunMatchesHandWrittenSQL(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// Two stale blocked issues, one fresh blocked issue, one stale todo.
	dbfx.Issue(t, "f27 stale blocked a", testutil.Cols{
		"status":     "blocked",
		"updated_at": testutil.Raw("now() - interval '9 days'"),
	})
	dbfx.Issue(t, "f27 stale blocked b", testutil.Cols{
		"status":     "blocked",
		"updated_at": testutil.Raw("now() - interval '30 days'"),
	})
	dbfx.Issue(t, "f27 fresh blocked", testutil.Cols{"status": "blocked"})
	dbfx.Issue(t, "f27 stale todo", testutil.Cols{
		"status":     "todo",
		"updated_at": testutil.Raw("now() - interval '40 days'"),
	})

	got := insightValue(t, insightRun(t, insight.Query{
		Entity: insight.EntityIssue,
		Metric: insight.MetricCount,
		Filters: []insight.Filter{
			{Field: "status", Op: insight.OpIs, Values: []string{"blocked"}},
			{Field: "idle_days", Op: insight.OpGt, Values: []string{"5"}},
		},
	}))

	want := dbfx.Count(t, `
		SELECT count(*) FROM issue
		WHERE workspace_id = $1
		  AND status = 'blocked'
		  AND EXTRACT(EPOCH FROM (now() - updated_at)) / 86400.0 > 5
	`, testWorkspaceID)
	if int(got) != want {
		t.Fatalf("insight count = %v, hand-written SQL = %d", got, want)
	}
	if want < 2 {
		t.Fatalf("fixture did not produce the rows the comparison needs: %d", want)
	}
}

// Acceptance 2: a document naming a table or a column outside the vocabulary
// is refused before anything executes, and the refusal is logged as `invalid`
// rather than as a run.
func TestInsightRunRefusesAnUnknownEntityWithoutExecuting(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	before := insightLogCount(t, insight.OutcomeOK)

	insightRun(t, map[string]any{
		"entity": "workspace_model_key",
		"metric": "count",
	}).Want(http.StatusBadRequest)

	if after := insightLogCount(t, insight.OutcomeOK); after != before {
		t.Errorf("an `ok` log row appeared for a document that never compiled")
	}
	if insightLogCount(t, insight.OutcomeInvalid) == 0 {
		t.Error("the rejection was not recorded as `invalid`")
	}
}

func TestInsightRunRefusesAnUnknownColumn(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	insightRun(t, map[string]any{
		"entity":  "issue",
		"metric":  "count",
		"filters": []map[string]any{{"field": "title", "op": "is", "values": []string{"x"}}},
	}).Want(http.StatusBadRequest)
}

// Acceptance 3: the workspace is the compiler's, not the document's. A forged
// document cannot see another workspace's rows however it is shaped.
func TestInsightNeverReadsAnotherWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	otherUser := dbfx.User(t, "F27 Outsider", "f27-outsider@multica.ai")
	otherWS := dbfx.Workspace(t, "F27 Other", "f27-other-ws")
	dbfx.Member(t, otherWS, otherUser, "owner")
	other := testutil.New(testPool, otherWS, otherUser)
	for i := 0; i < 3; i++ {
		other.Issue(t, "outsider issue", testutil.Cols{"status": "blocked"})
	}

	mine := dbfx.Count(t,
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND status = 'blocked'`, testWorkspaceID)

	// A document that tries to name the other workspace in every field it has.
	got := insightValue(t, insightRun(t, map[string]any{
		"entity": "issue",
		"metric": "count",
		"filters": []map[string]any{
			{"field": "status", "op": "is", "values": []string{"blocked"}},
			{"field": "project", "op": "is_not", "values": []string{otherWS}},
		},
	}))
	if int(got) != mine {
		t.Fatalf("count = %v, want this workspace's %d blocked issues", got, mine)
	}

	// And the forged workspace id cannot ride in through an unknown field
	// either: the document is rejected outright.
	insightRun(t, map[string]any{
		"entity":       "issue",
		"metric":       "count",
		"workspace_id": otherWS,
	}).Want(http.StatusBadRequest)
}

// Acceptance 3, the task entity: agent_task_queue carries no workspace_id, so
// its scoping runs entirely through the agent join. This is the row that would
// leak if that join were ever relaxed.
func TestInsightTaskCountsStopAtTheWorkspaceBoundary(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	otherUser := dbfx.User(t, "F27 Task Outsider", "f27-task-outsider@multica.ai")
	otherWS := dbfx.Workspace(t, "F27 Task Other", "f27-task-other-ws")
	dbfx.Member(t, otherWS, otherUser, "owner")
	other := testutil.New(testPool, otherWS, otherUser)
	otherRuntime := other.Runtime(t, "f27-other-rt")
	otherAgent := other.Agent(t, "f27-other-agent", otherRuntime)
	other.Task(t, otherAgent, testutil.Cols{"runtime_id": otherRuntime})
	other.Task(t, otherAgent, testutil.Cols{"runtime_id": otherRuntime})

	got := insightValue(t, insightRun(t, insight.Query{
		Entity: insight.EntityTask, Metric: insight.MetricCount,
	}))
	want := dbfx.Count(t, `
		SELECT count(*) FROM agent_task_queue t
		JOIN agent a ON a.id = t.agent_id
		WHERE a.workspace_id = $1
	`, testWorkspaceID)
	if int(got) != want {
		t.Fatalf("task count = %v, want %d", got, want)
	}
}

// Acceptance 6: a timeout is its own answer. 503 (not 500) so the client can
// tell "too expensive" from "broken", and a `timeout` log row so the ceiling
// is measurable.
func TestInsightTimeoutAnswers503AndLogsATimeout(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	before := insightLogCount(t, insight.OutcomeTimeout)

	workspaceUUID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse workspace: %v", err)
	}
	userUUID, err := util.ParseUUID(testUserID)
	if err != nil {
		t.Fatalf("parse user: %v", err)
	}
	resp := testutil.Call(t, func(w http.ResponseWriter, r *http.Request) {
		testHandler.finishInsightRun(w, r, workspaceUUID, userUUID, "how many issues",
			&insight.Query{Entity: insight.EntityIssue, Metric: insight.MetricCount},
			insight.ErrTimeout, time.Now())
	}, newRequest("POST", "/api/insights/run", nil))
	resp.Want(http.StatusServiceUnavailable)

	if after := insightLogCount(t, insight.OutcomeTimeout); after != before+1 {
		t.Errorf("timeout log rows = %d, want %d", after, before+1)
	}
}

// Acceptance 7: a pinned widget reloads through /run alone. The stored
// document is executed as-is, with no translation step, which is what stops a
// pinned figure from silently changing meaning.
func TestPinnedWidgetReloadsThroughRunAlone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	created := createInsightWidget(t, map[string]any{
		"name":     "Blocked work",
		"question": "how many issues are blocked",
		"query": insight.Query{
			Entity:  insight.EntityIssue,
			Metric:  insight.MetricCount,
			Filters: []insight.Filter{{Field: "status", Op: insight.OpIs, Values: []string{"blocked"}}},
		},
	})
	if created.Question == "" {
		t.Error("a widget was stored without the question that produced it")
	}

	var stored insight.Query
	if err := json.Unmarshal(created.Query, &stored); err != nil {
		t.Fatalf("stored query is not a document: %v", err)
	}
	got := insightValue(t, insightRun(t, stored))
	want := dbfx.Count(t,
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND status = 'blocked'`, testWorkspaceID)
	if int(got) != want {
		t.Fatalf("widget refresh = %v, want %d", got, want)
	}
}

// Acceptance 8: a workspace widget is visible to other members, a private one
// is not.
func TestInsightWidgetVisibility(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	private := createInsightWidget(t, map[string]any{
		"name":  "My private chart",
		"query": insight.Query{Entity: insight.EntityIssue, Metric: insight.MetricCount},
	})
	shared := createInsightWidget(t, map[string]any{
		"name":       "Team chart",
		"visibility": "workspace",
		"query":      insight.Query{Entity: insight.EntityIssue, Metric: insight.MetricCount},
	})
	if shared.Visibility != "workspace" {
		t.Fatalf("visibility = %q, want workspace", shared.Visibility)
	}

	otherUser := dbfx.User(t, "F27 Teammate", "f27-teammate@multica.ai")
	dbfx.Member(t, testWorkspaceID, otherUser, "member")

	var visible []InsightWidgetResponse
	testutil.Call(t, testHandler.ListInsightWidgets,
		newRequestAs(otherUser, "GET", "/api/insights/widgets", nil),
	).Want(http.StatusOK).JSON(&visible)

	seen := make(map[string]bool, len(visible))
	for _, wgt := range visible {
		seen[wgt.ID] = true
	}
	if !seen[shared.ID] {
		t.Error("a workspace widget was invisible to another member")
	}
	if seen[private.ID] {
		t.Error("a private widget leaked to another member")
	}
}

// A plain member cannot publish a chart to the whole workspace.
func TestInsightWidgetSharingIsAdminOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	member := dbfx.User(t, "F27 Plain", "f27-plain@multica.ai")
	dbfx.Member(t, testWorkspaceID, member, "member")

	testutil.Call(t, testHandler.CreateInsightWidget,
		newRequestAs(member, "POST", "/api/insights/widgets", map[string]any{
			"name":       "Everyone sees this",
			"visibility": "workspace",
			"query":      insight.Query{Entity: insight.EntityIssue, Metric: insight.MetricCount},
		}),
	).Want(http.StatusForbidden)
}

// Acceptance 9: two concurrent PATCHes; the second fails on expected_revision
// instead of overwriting the first.
func TestInsightWidgetConcurrentPatchConflicts(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	created := createInsightWidget(t, map[string]any{
		"name":  "Racy chart",
		"query": insight.Query{Entity: insight.EntityIssue, Metric: insight.MetricCount},
	})

	patch := func(name string, revision int) *testutil.Response {
		req := withURLParam(
			newRequest("PATCH", "/api/insights/widgets/"+created.ID, map[string]any{
				"name":              name,
				"expected_revision": revision,
			}), "id", created.ID)
		return testutil.Call(t, testHandler.UpdateInsightWidget, req)
	}

	var first InsightWidgetResponse
	patch("first writer", created.Revision).Want(http.StatusOK).JSON(&first)
	if first.Revision != created.Revision+1 {
		t.Fatalf("revision = %d, want %d", first.Revision, created.Revision+1)
	}
	// The second writer still holds the revision it read.
	patch("second writer", created.Revision).Want(http.StatusConflict)

	var latest InsightWidgetResponse
	testutil.Call(t, testHandler.ListInsightWidgets,
		newRequest("GET", "/api/insights/widgets", nil)).Want(http.StatusOK)
	getInsightWidgetByID(t, created.ID, &latest)
	if latest.Name != "first writer" {
		t.Errorf("name = %q; the losing writer overwrote the winner", latest.Name)
	}
	// Omitting expected_revision is not "I do not care", it is a 400.
	req := withURLParam(newRequest("PATCH", "/api/insights/widgets/"+created.ID,
		map[string]any{"name": "no precondition"}), "id", created.ID)
	testutil.Call(t, testHandler.UpdateInsightWidget, req).Want(http.StatusBadRequest)
}

// Acceptance 11: an agent the requester cannot see never appears in an
// assignee grouping. It folds into one `restricted` bucket so the total still
// adds up without naming the agent.
func TestInsightAssigneeGroupingHidesInvisibleAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	owner := dbfx.User(t, "F27 Agent Owner", "f27-agent-owner@multica.ai")
	dbfx.Member(t, testWorkspaceID, owner, "member")
	viewer := dbfx.User(t, "F27 Viewer", "f27-viewer@multica.ai")
	dbfx.Member(t, testWorkspaceID, viewer, "member")

	ownerFixture := testutil.New(testPool, testWorkspaceID, owner)
	runtime := ownerFixture.Runtime(t, "f27-private-rt")
	privateAgent := ownerFixture.Agent(t, "f27-private-agent", runtime, testutil.Cols{
		"owner_id":        owner,
		"visibility":      "private",
		"permission_mode": "private",
	})
	dbfx.Issue(t, "f27 assigned to a private agent", testutil.Cols{
		"assignee_type": "agent",
		"assignee_id":   privateAgent,
	})

	query := insight.Query{
		Entity:  insight.EntityIssue,
		Metric:  insight.MetricCount,
		GroupBy: []string{"assignee"},
		Limit:   100,
	}
	var out InsightRunResponse
	testutil.Call(t, testHandler.RunInsight,
		newRequestAs(viewer, "POST", "/api/insights/run", map[string]any{"query": query}),
	).Want(http.StatusOK).JSON(&out)

	for _, row := range out.Rows {
		if key, _ := row["assignee"].(string); key == "agent:"+privateAgent {
			t.Fatalf("a private agent appeared in another member's grouping: %v", out.Rows)
		}
	}

	// The owner still sees it — the bucket is a visibility boundary, not a
	// blanket redaction.
	var ownerOut InsightRunResponse
	testutil.Call(t, testHandler.RunInsight,
		newRequestAs(owner, "POST", "/api/insights/run", map[string]any{"query": query}),
	).Want(http.StatusOK).JSON(&ownerOut)
	found := false
	for _, row := range ownerOut.Rows {
		if key, _ := row["assignee"].(string); key == "agent:"+privateAgent {
			found = true
		}
	}
	if !found {
		t.Errorf("the agent's own owner could not see it in the grouping: %v", ownerOut.Rows)
	}
}

// A widget pinned with a document the DSL does not accept would never load, so
// it is refused at creation rather than stored as a broken card.
func TestInsightWidgetRejectsAnInvalidDocument(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	testutil.Call(t, testHandler.CreateInsightWidget,
		newRequest("POST", "/api/insights/widgets", map[string]any{
			"name":  "Broken",
			"query": map[string]any{"entity": "issue", "metric": "median"},
		}),
	).Want(http.StatusBadRequest)
}

func TestInsightWidgetDeleteRemovesIt(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	created := createInsightWidget(t, map[string]any{
		"name":  "Temporary",
		"query": insight.Query{Entity: insight.EntityIssue, Metric: insight.MetricCount},
	})
	req := withURLParam(newRequest("DELETE", "/api/insights/widgets/"+created.ID, nil), "id", created.ID)
	testutil.Call(t, testHandler.DeleteInsightWidget, req).Want(http.StatusNoContent)

	if n := dbfx.Count(t, `SELECT count(*) FROM insight_widget WHERE id = $1`, created.ID); n != 0 {
		t.Errorf("widget rows after delete = %d, want 0", n)
	}
}

// The path segment is a pure UUID, so a human-readable id is a 400, not a
// lookup.
func TestInsightWidgetRejectsANonUUIDPathSegment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	req := withURLParam(newRequest("DELETE", "/api/insights/widgets/MUL-12", nil), "id", "MUL-12")
	testutil.Call(t, testHandler.DeleteInsightWidget, req).Want(http.StatusBadRequest)
}

// Malformed bodies must be 400s, never panics.
func TestInsightEndpointsRejectMalformedBodies(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		name    string
		handler http.HandlerFunc
		path    string
		body    any
	}{
		{"ask with a broken body", testHandler.AskInsight, "/api/insights/ask", "{"},
		{"ask with no question", testHandler.AskInsight, "/api/insights/ask", map[string]any{"question": "  "}},
		{"run with a broken body", testHandler.RunInsight, "/api/insights/run", "{"},
		{"run with no query", testHandler.RunInsight, "/api/insights/run", map[string]any{}},
		{"create with a broken body", testHandler.CreateInsightWidget, "/api/insights/widgets", "]"},
		{"create with no name", testHandler.CreateInsightWidget, "/api/insights/widgets", map[string]any{
			"query": insight.Query{Entity: insight.EntityIssue, Metric: insight.MetricCount},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := testutil.WithHeaders(
				testutil.JSONRequest("POST", tc.path, tc.body),
				"X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID,
			)
			testutil.Call(t, tc.handler, req).Want(http.StatusBadRequest)
		})
	}
}

// ---- helpers --------------------------------------------------------------

func createInsightWidget(t *testing.T, body map[string]any) InsightWidgetResponse {
	t.Helper()
	var created InsightWidgetResponse
	testutil.Call(t, testHandler.CreateInsightWidget,
		newRequest("POST", "/api/insights/widgets", body),
	).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM insight_widget WHERE id = $1`, created.ID)
	return created
}

func getInsightWidgetByID(t *testing.T, id string, dest *InsightWidgetResponse) {
	t.Helper()
	widgetID, err := util.ParseUUID(id)
	if err != nil {
		t.Fatalf("parse widget id: %v", err)
	}
	workspaceID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatalf("parse workspace id: %v", err)
	}
	row, err := testHandler.Queries.GetInsightWidget(context.Background(), db.GetInsightWidgetParams{
		ID:          widgetID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("reload widget: %v", err)
	}
	*dest = insightWidgetToResponse(row)
}

func insightLogCount(t *testing.T, outcome string) int {
	t.Helper()
	return dbfx.Count(t,
		`SELECT count(*) FROM insight_query_log WHERE workspace_id = $1 AND outcome = $2`,
		testWorkspaceID, outcome)
}
