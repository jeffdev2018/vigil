package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/brainknowledge"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// usageNote inserts a Brain note in workspaceID and removes it, its passages
// and its usage rows afterwards.
func usageNote(t *testing.T, workspaceID, title, content string, cols testutil.Cols) string {
	t.Helper()
	id := uuid.NewString()
	row := testutil.Cols{"id": id, "workspace_id": workspaceID, "title": title, "content": content}
	for k, v := range cols {
		row[k] = v
	}
	dbfx.InsertNoID(t, "workspace_note", row, "id = $1", id)
	dbfx.Cleanup(t, `DELETE FROM workspace_note_usage WHERE note_id = $1`, id)
	dbfx.Cleanup(t, `DELETE FROM workspace_note_passage WHERE note_id = $1`, id)
	return id
}

func usageRows(t *testing.T, where string, args ...any) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM workspace_note_usage WHERE `+where, args...)
}

type claimNotesResponse struct {
	Task *struct {
		ID                    string                     `json:"id"`
		WorkspaceNotes        []struct{ ID string }      `json:"workspace_notes"`
		WorkspaceNotesOmitted int                        `json:"workspace_notes_omitted"`
		MemoryContext         *service.TaskMemoryContext `json:"memory_context"`
	} `json:"task"`
}

func claimForNotes(t *testing.T, runtimeID string) claimNotesResponse {
	t.Helper()
	req := withURLParam(newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "note-usage-claim"), "runtimeId", runtimeID)
	var resp claimNotesResponse
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&resp)
	if resp.Task == nil {
		t.Fatal("claim returned no task")
	}
	return resp
}

// The claim sends only what the knowledge budget keeps, records exactly that
// as injected, and a re-claim of the same run adds nothing.
func TestClaimRecordsExactlyTheInjectedNotes(t *testing.T) {
	agentID := agentMemoryFixture(t, "note-usage-claim-agent")
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)

	// 20000 four-byte runes = 80 KiB per note: two fit the 200 KiB budget, a third does not.
	big := strings.Repeat("\U0001D11E", 20000)
	pinned := usageNote(t, testWorkspaceID, "Pinned usage note", big, testutil.Cols{"pinned": true})
	recent := usageNote(t, testWorkspaceID, "Recent usage note", big, testutil.Cols{"updated_at": testutil.Raw("now() - interval '1 minute'")})
	dropped := usageNote(t, testWorkspaceID, "Dropped usage note", big, testutil.Cols{"updated_at": testutil.Raw("now() - interval '1 hour'")})

	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	dbfx.Cleanup(t, `DELETE FROM workspace_note_usage WHERE task_id = $1`, taskID)

	resp := claimForNotes(t, runtimeID)
	if resp.Task.ID != taskID {
		t.Fatalf("claimed %s, want %s", resp.Task.ID, taskID)
	}
	var sent []string
	for _, n := range resp.Task.WorkspaceNotes {
		sent = append(sent, n.ID)
	}
	if len(sent) < 2 || sent[0] != pinned || sent[1] != recent {
		t.Fatalf("workspace_notes = %v, want [pinned, recent] first", sent)
	}
	for _, id := range sent {
		if id == dropped {
			t.Fatalf("over-budget note was sent: %v", sent)
		}
	}
	if resp.Task.WorkspaceNotesOmitted < 1 {
		t.Fatalf("workspace_notes_omitted = %d, want the dropped note counted", resp.Task.WorkspaceNotesOmitted)
	}

	var stored service.TaskMemoryContext
	var raw []byte
	dbfx.QueryRow(t, `SELECT memory_context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw)
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.WorkspaceNotesStatus != "loaded" || len(stored.WorkspaceNotes) != len(sent) {
		t.Fatalf("memory_context notes = %+v, want %d loaded", stored, len(sent))
	}
	for i, v := range stored.WorkspaceNotes {
		if v.ID != sent[i] || v.Revision != 1 {
			t.Fatalf("memory_context.workspace_notes[%d] = %+v, want %s rev 1", i, v, sent[i])
		}
	}
	if got := usageRows(t, `task_id = $1 AND kind = 'injected' AND channel = 'daemon_brief' AND actor_type = 'agent' AND actor_id = $2`, taskID, agentID); got != len(sent) {
		t.Fatalf("injected rows = %d, want %d", got, len(sent))
	}
	if got := usageRows(t, `task_id = $1 AND note_id = $2`, taskID, dropped); got != 0 {
		t.Fatalf("the dropped note has %d usage rows", got)
	}

	// Re-claim the same run: no duplicate.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'queued', dispatched_at = NULL WHERE id = $1`, taskID)
	claimForNotes(t, runtimeID)
	if got := usageRows(t, `task_id = $1 AND kind = 'injected'`, taskID); got != len(sent) {
		t.Fatalf("re-claim injected rows = %d, want %d", got, len(sent))
	}
}

// A claim whose finalization is refused (a stale dispatch) rolls back with
// its injected rows.
func TestRefusedClaimFinalizationLeavesNoUsage(t *testing.T) {
	agentID := agentMemoryFixture(t, "note-usage-refused-agent")
	note := usageNote(t, testWorkspaceID, "Refused claim note", "body", nil)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "dispatched", "dispatched_at": testutil.Raw("now()")})
	task, err := testHandler.Queries.GetAgentTask(context.Background(), util.MustParseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	task.DispatchedAt.Time = task.DispatchedAt.Time.Add(-time.Hour) // no longer the live claim
	_, err = testHandler.TaskService.FinalizeTaskClaim(context.Background(), task, db.CreateTaskTokenParams{}, nil, false,
		&service.TaskMemoryContext{AgentStatus: "loaded", AgentVersions: []service.MemoryVersion{}, WorkspaceNotesStatus: "loaded",
			WorkspaceNotes: []service.NoteVersion{{ID: note, Revision: 1}}})
	if err == nil {
		t.Fatal("stale finalize succeeded")
	}
	if got := usageRows(t, `task_id = $1`, taskID); got != 0 {
		t.Fatalf("refused claim left %d usage rows", got)
	}
}

// An empty Brain: nothing sent, nothing recorded, the status still says the
// read succeeded.
func TestClaimWithEmptyBrainRecordsNothing(t *testing.T) {
	if n := dbfx.Count(t, `SELECT count(*) FROM workspace_note WHERE workspace_id = $1 AND archived_at IS NULL`, testWorkspaceID); n != 0 {
		t.Skipf("handler workspace already holds %d notes", n)
	}
	agentID := agentMemoryFixture(t, "note-usage-empty-agent")
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	resp := claimForNotes(t, runtimeID)
	if len(resp.Task.WorkspaceNotes) != 0 || resp.Task.WorkspaceNotesOmitted != 0 {
		t.Fatalf("empty Brain sent notes: %+v", resp.Task)
	}
	if got := usageRows(t, `task_id = $1`, taskID); got != 0 {
		t.Fatalf("empty Brain recorded %d rows", got)
	}
}

func TestKnowledgeFileRefs(t *testing.T) {
	input := map[string]any{
		"file_path": "/work/.multica/knowledge/deploy-procedure-0192abcd.md",
		"nested":    []any{map[string]any{"command": "cat .multica/knowledge/x-0192abce.md && ls"}},
		"readme":    ".multica/knowledge/README.md",
		"other":     "/tmp/deploy-0192abcf.md",
	}
	refs := knowledgeFileRefs(nil, input)
	if len(refs) != 2 {
		t.Fatalf("refs = %v, want the two knowledge note files only", refs)
	}
	if refs := knowledgeFileRefs(nil, map[string]any{"command": "go test ./...", "path": "README.md"}); len(refs) != 0 {
		t.Fatalf("a batch without knowledge paths yielded %v; it must reach no query", refs)
	}
}

func TestReportTaskMessagesRecordsKnowledgeFileReads(t *testing.T) {
	a := usageNote(t, testWorkspaceID, "Deploy procedure", "a", nil)
	b := usageNote(t, testWorkspaceID, "Shell read note", "b", nil)
	foreign := usageNote(t, testWorkspaceID, "Other run note", "c", nil)
	taskID := seedBatchTask(t, "note-usage-file-read")
	dbfx.Cleanup(t, `DELETE FROM workspace_note_usage WHERE task_id = $1`, taskID)
	mc := `{"dispatched_at":"2026-09-01T00:00:00Z","agent_status":"loaded","agent_versions":[],"project_version":null,"workspace_notes_status":"loaded","workspace_notes":[{"id":"` + a + `","revision":1},{"id":"` + b + `","revision":1}]}`
	dbfx.Exec(t, `UPDATE agent_task_queue SET memory_context = $2::jsonb WHERE id = $1`, taskID, mc)

	prefix := brainknowledge.IDPrefix
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "tool_use", "tool": "Read", "input": map[string]any{"file_path": "/w/.multica/knowledge/deploy-procedure-" + prefix(a) + ".md"}},
		map[string]any{"seq": 2, "type": "tool_use", "tool": "Bash", "input": map[string]any{"command": "cat .multica/knowledge/x-" + prefix(b) + ".md"}},
		map[string]any{"seq": 3, "type": "tool_use", "tool": "Read", "input": map[string]any{"file_path": ".multica/knowledge/README.md"}},
		map[string]any{"seq": 4, "type": "tool_use", "tool": "Read", "input": map[string]any{"file_path": ".multica/knowledge/other-" + prefix(foreign) + ".md"}},
		map[string]any{"seq": 5, "type": "tool_use", "tool": "Read", "input": map[string]any{"file_path": ".multica/knowledge/ghost-deadbeef.md"}},
		map[string]any{"seq": 6, "type": "text", "content": "read .multica/knowledge/deploy-procedure-" + prefix(a) + ".md"},
	})).Want(http.StatusOK)

	if got := usageRows(t, `task_id = $1 AND kind = 'opened' AND channel = 'file_read'`, taskID); got != 2 {
		t.Fatalf("file_read rows = %d, want 2 (Read + shell cat)", got)
	}
	if got := usageRows(t, `note_id = $1`, foreign); got != 0 {
		t.Fatalf("a note this run was not given was recorded %d times", got)
	}
}

func noteUsageCall(t *testing.T, h http.HandlerFunc, method, noteID, suffix string, headers ...string) *testutil.Response {
	t.Helper()
	req := noteRequest(method, "/api/workspace/notes/"+noteID+suffix, testWorkspaceID, nil)
	if len(headers) > 0 {
		req = testutil.WithHeaders(req, headers...)
	}
	return testutil.Call(t, noteWorkspaceHandler(h), testutil.WithURLParams(req, "id", noteID))
}

func TestNoteReadsAreCountedPerActorAndChannel(t *testing.T) {
	note := usageNote(t, testWorkspaceID, "Read counting note", "body", nil)
	issue, taskID, agentID := runningAgentRun(t, "note-usage-api")
	_ = issue
	dbfx.Cleanup(t, `DELETE FROM workspace_note_usage WHERE task_id = $1`, taskID)
	agent := gateHeaders(taskID, agentID)

	noteUsageCall(t, testHandler.GetWorkspaceNote, "GET", note, "", agent...).Want(http.StatusOK)
	if got := usageRows(t, `task_id = $1 AND note_id = $2 AND kind = 'opened' AND channel = 'api'`, taskID, note); got != 1 {
		t.Fatalf("agent GET opened/api rows = %d", got)
	}

	_, mcpTask, mcpAgent := runningAgentRun(t, "note-usage-mcp")
	dbfx.Cleanup(t, `DELETE FROM workspace_note_usage WHERE task_id = $1`, mcpTask)
	noteUsageCall(t, testHandler.GetWorkspaceNote, "GET", note, "", append(gateHeaders(mcpTask, mcpAgent), "X-Client-Platform", "mcp")...).Want(http.StatusOK)
	if got := usageRows(t, `task_id = $1 AND kind = 'opened' AND channel = 'mcp'`, mcpTask); got != 1 {
		t.Fatalf("MCP GET opened/mcp rows = %d", got)
	}

	// Search by a run: retrieved, once per note.
	noteSearch := testutil.WithHeaders(noteRequest("GET", "/api/workspace/notes?search=counting", testWorkspaceID, nil), agent...)
	testutil.Call(t, noteWorkspaceHandler(testHandler.ListWorkspaceNotes), noteSearch).Want(http.StatusOK)
	ranked := testutil.WithHeaders(noteRequest("GET", "/api/workspace/notes/search?q=counting", testWorkspaceID, nil), agent...)
	testutil.Call(t, noteWorkspaceHandler(testHandler.SearchWorkspaceNotes), ranked).Want(http.StatusOK)
	if got := usageRows(t, `task_id = $1 AND note_id = $2 AND kind = 'retrieved' AND channel = 'api'`, taskID, note); got != 1 {
		t.Fatalf("retrieved rows = %d, want 1", got)
	}

	// A member: GET views, POST view the same day adds nothing, an agent POST view writes nothing.
	noteUsageCall(t, testHandler.GetWorkspaceNote, "GET", note, "").Want(http.StatusOK)
	noteUsageCall(t, testHandler.RecordWorkspaceNoteView, "POST", note, "/view").Want(http.StatusNoContent)
	noteUsageCall(t, testHandler.RecordWorkspaceNoteView, "POST", note, "/view", "X-Client-Platform", "mobile").Want(http.StatusNoContent)
	if got := usageRows(t, `note_id = $1 AND kind = 'viewed' AND actor_type = 'member' AND actor_id = $2 AND channel = 'api'`, note, testUserID); got != 1 {
		t.Fatalf("member viewed rows = %d, want one a day", got)
	}
	noteUsageCall(t, testHandler.RecordWorkspaceNoteView, "POST", note, "/view", agent...).Want(http.StatusNoContent)
	if got := usageRows(t, `task_id = $1 AND kind = 'viewed'`, taskID); got != 0 {
		t.Fatalf("agent POST view wrote %d rows", got)
	}

	// Another workspace's note is a 404 and records nothing.
	otherWS := dbfx.Workspace(t, "Note usage other", "note-usage-other-"+uuid.NewString()[:8])
	foreign := usageNote(t, otherWS, "Foreign note", "x", nil)
	noteUsageCall(t, testHandler.RecordWorkspaceNoteView, "POST", foreign, "/view").Want(http.StatusNotFound)
	noteUsageCall(t, testHandler.GetWorkspaceNoteUsage, "GET", foreign, "/usage").Want(http.StatusNotFound)
	if got := usageRows(t, `note_id = $1`, foreign); got != 0 {
		t.Fatalf("foreign note recorded %d rows", got)
	}
}

func TestNoteUsageSummaryRunsAndPrivacy(t *testing.T) {
	ctx := context.Background()
	note := usageNote(t, testWorkspaceID, "Usage summary note", "body", nil)
	agentID := agentMemoryFixture(t, "note-usage-summary-agent")
	issueID := dbfx.Issue(t, "note usage issue")
	issueRun := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "status": "completed", "completed_at": testutil.Raw("now()")})
	chatOwner := dbfx.User(t, "Chat owner", "note-usage-chat-"+uuid.NewString()[:8]+"@multica.test")
	dbfx.Member(t, testWorkspaceID, chatOwner, "member")
	session := dbfx.ChatSession(t, agentID, testutil.Cols{"creator_id": chatOwner})
	chatRun := dbfx.Task(t, agentID, testutil.Cols{"chat_session_id": session, "status": "completed", "completed_at": testutil.Raw("now()")})

	record := func(task, kind string) {
		service.RecordRunNoteUsage(ctx, testHandler.Queries, util.MustParseUUID(task), util.MustParseUUID(agentID), kind, "native_tool", []service.NoteVersion{{ID: note, Revision: 1}})
	}
	record(issueRun, "injected")
	record(issueRun, "opened")
	record(chatRun, "retrieved")
	dbfx.Exec(t, `UPDATE workspace_note_usage SET created_at = now() - interval '1 hour' WHERE task_id = $1`, issueRun)
	for _, user := range []string{testUserID, chatOwner} {
		if err := testHandler.Queries.RecordMemberNoteView(ctx, db.RecordMemberNoteViewParams{WorkspaceID: util.MustParseUUID(testWorkspaceID), NoteID: util.MustParseUUID(note), NoteRevision: 1, Channel: "web", UserID: util.MustParseUUID(user)}); err != nil {
			t.Fatal(err)
		}
	}

	var usage WorkspaceNoteUsageResponse
	noteUsageCall(t, testHandler.GetWorkspaceNoteUsage, "GET", note, "/usage").Want(http.StatusOK).JSON(&usage)
	if usage.Counts != (WorkspaceNoteUsageCounts{Injected: 1, Retrieved: 1, Opened: 1, Viewed: 2}) || usage.RunsCount != 2 || usage.ViewersCount != 2 || usage.LastUsedAt == nil {
		t.Fatalf("summary = %+v", usage)
	}
	if len(usage.Runs) != 2 {
		t.Fatalf("runs = %+v", usage.Runs)
	}
	chat, issue := usage.Runs[0], usage.Runs[1]
	if !chat.Private || chat.TaskID != "" || chat.IssueID != "" {
		t.Fatalf("most recent run should be the masked chat run, got %+v", chat)
	}
	if issue.TaskID != issueRun || issue.IssueID != issueID || !strings.HasPrefix(issue.IssueIdentifier, "HAN-") || len(issue.Kinds) != 2 || issue.Private {
		t.Fatalf("issue run = %+v", issue)
	}

	req := testutil.WithHeaders(noteRequest("GET", "/api/workspace/notes/"+note+"/usage", testWorkspaceID, nil), "X-User-ID", chatOwner)
	var ownerView WorkspaceNoteUsageResponse
	testutil.Call(t, noteWorkspaceHandler(testHandler.GetWorkspaceNoteUsage), testutil.WithURLParams(req, "id", note)).Want(http.StatusOK).JSON(&ownerView)
	if ownerView.Runs[0].Private || ownerView.Runs[0].TaskID != chatRun {
		t.Fatalf("the chat owner must see their own run: %+v", ownerView.Runs[0])
	}
}

func TestTaskNoteUsageListsNotesAndSurvivesDeletion(t *testing.T) {
	ctx := context.Background()
	kept := usageNote(t, testWorkspaceID, "Run note kept", "body", nil)
	gone := usageNote(t, testWorkspaceID, "Run note deleted", "body", nil)
	agentID := agentMemoryFixture(t, "note-usage-run-agent")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": dbfx.Issue(t, "note usage run issue"), "status": "completed", "completed_at": testutil.Raw("now()"), "dispatched_at": testutil.Raw("now()")})
	dbfx.Exec(t, `UPDATE agent_task_queue SET memory_context = jsonb_build_object('workspace_notes', jsonb_build_array(jsonb_build_object('id', $2::text, 'revision', 1), jsonb_build_object('id', $3::text, 'revision', 1))) WHERE id = $1`, taskID, kept, gone)
	notes := []service.NoteVersion{{ID: kept, Revision: 1}, {ID: gone, Revision: 1}}
	service.RecordRunNoteUsage(ctx, testHandler.Queries, util.MustParseUUID(taskID), util.MustParseUUID(agentID), "injected", "daemon_brief", notes)
	service.RecordRunNoteUsage(ctx, testHandler.Queries, util.MustParseUUID(taskID), util.MustParseUUID(agentID), "opened", "file_read", notes[:1])

	// Deleting a note removes its usage rows in the same statement.
	rows, err := testHandler.Queries.DeleteWorkspaceNote(ctx, db.DeleteWorkspaceNoteParams{ID: util.MustParseUUID(gone), WorkspaceID: util.MustParseUUID(testWorkspaceID)})
	if err != nil || rows != 1 {
		t.Fatalf("delete note: rows=%d err=%v", rows, err)
	}
	if got := usageRows(t, `note_id = $1`, gone); got != 0 {
		t.Fatalf("deleted note kept %d usage rows", got)
	}

	req := testutil.WithURLParams(noteRequest("GET", "/api/tasks/"+taskID+"/note-usage", testWorkspaceID, nil), "taskId", taskID)
	var out struct{ Notes []TaskNoteUsageItem }
	testutil.Call(t, noteWorkspaceHandler(testHandler.ListTaskNoteUsage), req).Want(http.StatusOK).JSON(&out)
	byID := map[string]TaskNoteUsageItem{}
	for _, n := range out.Notes {
		byID[n.NoteID] = n
	}
	if k := byID[kept]; k.Title != "Run note kept" || k.Deleted || strings.Join(k.Kinds, ",") != "injected,opened" || k.Revision != 1 {
		t.Fatalf("kept note = %+v", k)
	}
	if g := byID[gone]; !g.Deleted || strings.Join(g.Kinds, ",") != "injected" {
		t.Fatalf("deleted note = %+v (all: %+v)", g, out.Notes)
	}
}

func TestWorkspaceNotePurgeRemovesUsage(t *testing.T) {
	ctx := context.Background()
	ws := dbfx.Workspace(t, "Note usage purge", "note-usage-purge-"+uuid.NewString()[:8])
	note := usageNote(t, ws, "Purged note", "x", nil)
	if err := testHandler.Queries.RecordMemberNoteView(ctx, db.RecordMemberNoteViewParams{WorkspaceID: util.MustParseUUID(ws), NoteID: util.MustParseUUID(note), NoteRevision: 1, Channel: "web", UserID: util.MustParseUUID(testUserID)}); err != nil {
		t.Fatal(err)
	}
	if err := testHandler.Queries.DeleteWorkspaceNotes(ctx, util.MustParseUUID(ws)); err != nil {
		t.Fatal(err)
	}
	if got := usageRows(t, `workspace_id = $1`, ws); got != 0 {
		t.Fatalf("workspace purge kept %d usage rows", got)
	}
}
