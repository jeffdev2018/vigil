package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Decision memory (K29): extraction cites real run messages, a complex run
// blocks merge readiness until a decision exists, manual records need a
// valid source, listing is per project with an author filter.

func runMessage(t *testing.T, taskID string, seq int, typ, content string, over ...testutil.Cols) {
	t.Helper()
	cols := testutil.Cols{"task_id": taskID, "seq": seq, "type": typ, "content": content}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	dbfx.Insert(t, "task_message", cols)
}

func decisionRun(t *testing.T, label string, files ...string) (string, string, string) {
	t.Helper()
	issueID, taskID := completedAgentRun(t, label)
	projectID := dbfx.Project(t, label+" project")
	dbfx.Exec(t, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM decision_record WHERE issue_id = $1`, issueID)
	})
	runMessage(t, taskID, 1, "text", "Looking at the schema first.")
	for i, f := range files {
		runMessage(t, taskID, 2+i, "tool_use", "", testutil.Cols{"tool": "Write", "input": testutil.Raw(`'{"file_path":"` + f + `"}'::jsonb`)})
	}
	runMessage(t, taskID, 2+len(files), "text", "Decided to keep the table denormalized: one read path, no join at 3am.")
	return issueID, taskID, projectID
}

func adrRequirementOf(t *testing.T, issueID string) ADRRequirement {
	t.Helper()
	var out ADRRequirement
	testutil.Call(t, testHandler.GetIssueADRRequirement, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/adr-required", nil), "id", issueID)).Want(http.StatusOK).JSON(&out)
	return out
}

func TestADRGateSettingsAndComplexity(t *testing.T) {
	gate := service.ADRGateSettings(nil)
	if gate != service.DefaultADRGate || !gate.Requires(10, false) || !gate.Requires(1, true) || gate.Requires(9, false) {
		t.Fatalf("default gate = %+v", gate)
	}
	off := service.ADRGateSettings([]byte(`{"adr_gate":{"file_threshold":0,"require_on_migration":false}}`))
	if off.Enabled() || off.Requires(100, true) {
		t.Fatalf("off gate = %+v", off)
	}
	msgs := []db.TaskMessage{
		{Type: "tool_use", Tool: pgText("Read"), Input: []byte(`{"file_path":"a.go"}`)},
		{Type: "tool_use", Tool: pgText("Edit"), Input: []byte(`{"file_path":"a.go"}`)},
		{Type: "tool_use", Tool: pgText("Edit"), Input: []byte(`{"file_path":"a.go"}`)},
		{Type: "tool_use", Tool: pgText("apply_patch"), Input: []byte(`{"patch":"*** Update File: server/migrations/9_x.up.sql\n+x\n*** Add File: b.go"}`)},
		{Type: "text", Content: pgText("no tool")},
	}
	if files, migration := runComplexity(msgs); files != 3 || !migration {
		t.Fatalf("complexity = %d files, migration %v; want 3, true", files, migration)
	}
}

func pgText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

func TestDecisionMemoryExtractsGatesAndLists(t *testing.T) {
	rememberSettings(t)
	issueID, _, projectID := decisionRun(t, "decision memory", "server/migrations/900_x.up.sql", "server/a.go")
	issueUUID := parseUUID(issueID)

	// A migration in the run: the gate is required and unmet.
	req := adrRequirementOf(t, issueID)
	if !req.Required || req.Satisfied || req.Files != 2 || !req.Migration || req.Decisions != 0 {
		t.Fatalf("requirement = %+v, want required and unmet", req)
	}
	if kinds := blockerKinds(callMergeReadiness(t, issueID).Blockers); !containsString(kinds, blockerADRRequired) {
		t.Fatalf("blockers = %v, want %s", kinds, blockerADRRequired)
	}

	// Extraction keeps only decisions that cite a real text message.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"decisions":[
		{"source_seq":4,"title":"Keep the table denormalized","context":"One read path","decision":"No join","consequences":"Writes fan out"},
		{"source_seq":99,"title":"Ghost","context":"","decision":"invented"},
		{"source_seq":1,"title":"","context":"","decision":"untitled"}]}`))
	issue, err := testHandler.Queries.GetIssue(context.Background(), issueUUID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := testHandler.extractDecisions(context.Background(), issue); err != nil || n != 1 {
		t.Fatalf("extract = %d, %v; want 1", n, err)
	}
	if n, err := testHandler.extractDecisions(context.Background(), issue); err != nil || n != 0 {
		t.Fatalf("second extract = %d, %v; want 0 (idempotent per run)", n, err)
	}
	req = adrRequirementOf(t, issueID)
	if !req.Required || !req.Satisfied || req.Decisions != 1 {
		t.Fatalf("requirement after extraction = %+v, want satisfied", req)
	}
	if kinds := blockerKinds(callMergeReadiness(t, issueID).Blockers); containsString(kinds, blockerADRRequired) {
		t.Fatalf("blockers = %v, adr blocker must be gone", kinds)
	}

	// Manual records: a seq outside the run is refused, a real one accepted.
	post := func(body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.CreateIssueDecisions, testutil.WithURLParams(
			newRequest(http.MethodPost, "/api/issues/"+issueID+"/decisions", body), "id", issueID))
	}
	res := post(map[string]any{"decisions": []map[string]any{{"source_message_seq": 42, "title": "x", "decision": "y"}}})
	if res.Code != http.StatusUnprocessableEntity || res.Map()["code"] != "invalid_source" {
		t.Fatalf("bad seq: %d %s", res.Code, res.Text())
	}
	var created struct {
		Decisions []DecisionRecordResponse `json:"decisions"`
	}
	post(map[string]any{"decisions": []map[string]any{{"source_message_seq": 2, "title": "Wrote the migration by hand", "context": "c", "decision": "d"}}}).Want(http.StatusCreated).JSON(&created)
	if len(created.Decisions) != 1 || created.Decisions[0].AuthorType != "member" || created.Decisions[0].SourceMessageSeq != 2 {
		t.Fatalf("manual = %+v", created.Decisions)
	}

	// Project listing, newest first, filterable by author.
	list := func(query string) []DecisionRecordResponse {
		var out struct {
			Decisions []DecisionRecordResponse `json:"decisions"`
		}
		testutil.Call(t, testHandler.ListProjectDecisions, testutil.WithURLParams(
			newRequest(http.MethodGet, "/api/projects/"+projectID+"/decisions"+query, nil), "id", projectID)).Want(http.StatusOK).JSON(&out)
		return out.Decisions
	}
	all := list("")
	if len(all) != 2 || all[0].AuthorType != "member" || all[1].Title != "Keep the table denormalized" || all[1].IssueIdentifier == "" || all[1].IssueTitle == "" {
		t.Fatalf("list = %+v", all)
	}
	if agents := list("?author_type=agent"); len(agents) != 1 || agents[0].AuthorType != "agent" {
		t.Fatalf("agent filter = %+v", agents)
	}
	testutil.Call(t, testHandler.ListProjectDecisions, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/projects/"+projectID+"/decisions?author_type=robot", nil), "id", projectID)).Want(http.StatusBadRequest)

	// Both records were audited (K08).
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2`, AuditDecisionRecorded, issueID); n != 2 {
		t.Fatalf("audit entries = %d, want 2", n)
	}

	// The gate off: nothing required even with the migration.
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"adr_gate":{"file_threshold":0,"require_on_migration":false}}' WHERE id = $1`, testWorkspaceID)
	if req := adrRequirementOf(t, issueID); req.Required {
		t.Fatalf("gate off but required: %+v", req)
	}
}

func TestDecisionMemoryExtractsWhenIssueIsAccepted(t *testing.T) {
	issueID, _, _ := decisionRun(t, "decision accepted")
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"decisions":[{"source_seq":2,"title":"Denormalized","context":"c","decision":"d"}]}`))
	moveIssue(t, issueID, "done").Want(http.StatusOK)
	deadline := time.Now().Add(5 * time.Second)
	for dbfx.Count(t, `SELECT COUNT(*) FROM decision_record WHERE issue_id = $1`, issueID) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("accepting the issue must extract its run's decisions")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Leaving done does not extract again; a run without an LLM is skipped.
	prev := testHandler.LLM
	testHandler.LLM = nil
	t.Cleanup(func() { testHandler.LLM = prev })
	other, _, _ := decisionRun(t, "decision no llm")
	moveIssue(t, other, "done").Want(http.StatusOK)
	time.Sleep(200 * time.Millisecond)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM decision_record WHERE issue_id = $1`, other); n != 0 {
		t.Fatalf("no llm but %d records", n)
	}
}
