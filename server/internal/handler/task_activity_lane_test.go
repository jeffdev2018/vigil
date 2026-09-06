package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F03 · the run action lane. Issue changes made by a run are joined out of
// activity_log on details.task_id rather than duplicated into the message
// stream, so these tests pin two things: that every writer able to run under an
// agent stamps that key, and that the join keeps everything else out.

// ─── 1. Every CreateActivity caller is accounted for ────────────────────────

// createActivityCallSite declares what one CreateActivity call site does about
// run lineage. The lane is only as trustworthy as this list is complete, which
// is why the test below rediscovers the call sites from source instead of
// trusting the table: a new writer that nobody classified fails here rather
// than silently going missing from every run's lane.
type createActivityCallSite struct {
	file string
	// stampsTaskID is true when this site can write details.task_id.
	stampsTaskID bool
	// why documents the classification, and is printed on failure so whoever
	// broke it learns what the site was supposed to do.
	why string
	// wantSource, when set, must appear in the file: the mechanism that does
	// the stamping. Asserting on it turns "someone deleted the stamp" into a
	// failure here rather than a silently empty lane in production.
	wantSource string
}

var createActivityCallSites = map[string]createActivityCallSite{
	"cmd/server/activity_listeners.go": {
		file:         "cmd/server/activity_listeners.go",
		stampsTaskID: true,
		why: "issue created/updated + task terminal activity. Every site routes " +
			"its details through runActivityDetails, which copies acting_task_id " +
			"(set by the HTTP handlers an agent CLI can reach) into task_id.",
		wantSource: "runActivityDetails(",
	},
	"internal/handler/squad.go": {
		file:         "internal/handler/squad.go",
		stampsTaskID: true,
		why:          "squad leader evaluation, agent-only endpoint; already stamped task_id before F03.",
		wantSource:   `"task_id":  util.UUIDToString(taskUUID)`,
	},
	"internal/handler/agent_env.go": {
		file:         "internal/handler/agent_env.go",
		stampsTaskID: false,
		why: "agent env reveal/update audit. Member-authored (actor_type is the " +
			"literal \"member\") and issue_id is deliberately unset, so these rows " +
			"are never returned by ListActivitiesForIssue and can never reach a " +
			"run lane whether or not they carry a task id.",
		wantSource: `IssueID:     pgtype.UUID{}, // env access is not tied to an issue`,
	},
}

// TestCreateActivityCallersAreClassifiedForRunLineage rediscovers every
// CreateActivity caller in the server module and fails on any file the table
// above does not classify.
func TestCreateActivityCallersAreClassifiedForRunLineage(t *testing.T) {
	t.Parallel()

	root := serverModuleRoot(t)
	found := map[string]int{}
	callPattern := regexp.MustCompile(`\bCreateActivity\(`)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// pkg/db/generated is sqlc output: it DEFINES CreateActivity, it
			// does not decide what goes in details.
			if d.Name() == "generated" || d.Name() == "node_modules" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if n := len(callPattern.FindAll(body, -1)); n > 0 {
			found[filepath.ToSlash(rel)] = n
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk server module: %v", err)
	}

	if len(found) == 0 {
		t.Fatal("found no CreateActivity callers; the scan is broken, not the code")
	}

	for rel, count := range found {
		site, ok := createActivityCallSites[rel]
		if !ok {
			t.Errorf("unclassified CreateActivity caller in %s (%d call(s)).\n"+
				"Every activity written under an agent run must stamp details.task_id "+
				"or it will not appear in that run's action lane. Add the file to "+
				"createActivityCallSites saying which it is and why.", rel, count)
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if readErr != nil {
			t.Fatalf("read %s: %v", rel, readErr)
		}
		if site.wantSource != "" && !strings.Contains(string(body), site.wantSource) {
			t.Errorf("%s no longer contains %q.\nClassified as stampsTaskID=%v because: %s",
				rel, site.wantSource, site.stampsTaskID, site.why)
		}
	}

	for rel := range createActivityCallSites {
		if _, ok := found[rel]; !ok {
			t.Errorf("createActivityCallSites lists %s, but it no longer calls CreateActivity; "+
				"drop the entry so the table keeps meaning something", rel)
		}
	}
}

// serverModuleRoot resolves server/ from the test's working directory
// (server/internal/handler).
func serverModuleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve server module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("server module root %s has no go.mod: %v", root, err)
	}
	return root
}

// ─── 2. The join itself ─────────────────────────────────────────────────────

func taskActivityRequest(t *testing.T, taskID string) *http.Request {
	t.Helper()
	req := newRequest(http.MethodGet, "/api/tasks/"+taskID+"/messages", nil)
	req = withURLParam(req, "taskId", taskID)
	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      util.MustParseUUID(testUserID),
			WorkspaceID: util.MustParseUUID(testWorkspaceID),
		})
	if err != nil {
		t.Fatalf("load member row: %v", err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
}

// seedRunActivity inserts one activity_log row on the task's issue with the
// given details JSON.
func seedRunActivity(t *testing.T, issueID, action, detailsJSON string) string {
	t.Helper()
	return dbfx.Insert(t, "activity_log", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"issue_id":     issueID,
		"actor_type":   "agent",
		"actor_id":     testUserID,
		"action":       action,
		"details":      testutil.Raw(fmt.Sprintf("'%s'::jsonb", strings.ReplaceAll(detailsJSON, "'", "''"))),
	})
}

func issueIDForTask(t *testing.T, taskID string) string {
	t.Helper()
	var issueID string
	dbfx.QueryRow(t, `SELECT issue_id::text FROM agent_task_queue WHERE id = $1`, taskID).Scan(&issueID)
	return issueID
}

func TestListTaskMessagesByUserReturnsMessagesAndRunActions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "run-action-lane")
	issueID := issueIDForTask(t, taskID)
	otherTaskID := seedBatchTask(t, "run-action-other")

	// This run's status change, with the before/after pair the lane renders.
	seedRunActivity(t, issueID, "status_changed",
		fmt.Sprintf(`{"from":"todo","to":"in_progress","task_id":"%s"}`, taskID))
	// This run's assignee change — the polymorphic shape.
	seedRunActivity(t, issueID, "assignee_changed",
		fmt.Sprintf(`{"from_type":"member","from_id":"%s","to_type":"agent","to_id":"%s","task_id":"%s"}`,
			testUserID, testUserID, taskID))
	// A human edit on the same issue: no task_id, must never appear.
	seedRunActivity(t, issueID, "title_changed", `{"from":"Old","to":"New"}`)
	// Another run's change on the same issue.
	seedRunActivity(t, issueID, "priority_changed",
		fmt.Sprintf(`{"from":"low","to":"high","task_id":"%s"}`, otherTaskID))
	// A row whose details are not an object at all: must be skipped, not panic.
	seedRunActivity(t, issueID, "description_updated", `"not-an-object"`)

	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "text", "content": "working"},
		map[string]any{"seq": 2, "type": "response", "content": "done"},
	})).Want(http.StatusOK)

	var got TaskActivityResponse
	testutil.Call(t, testHandler.ListTaskMessagesByUser, taskActivityRequest(t, taskID)).
		Want(http.StatusOK).JSON(&got)

	if len(got.Messages) != 2 {
		t.Fatalf("messages = %d, want 2 (%+v)", len(got.Messages), got.Messages)
	}
	// The response type must survive ingest untouched — the batch endpoint has
	// no type allow-list, and widening one silently would strand the new kind.
	if got.Messages[1].Type != "response" {
		t.Errorf("second message type = %q, want %q; ReportTaskMessages must not filter types",
			got.Messages[1].Type, "response")
	}

	if len(got.Actions) != 2 {
		t.Fatalf("actions = %d, want 2 (this run's status + assignee change only): %+v",
			len(got.Actions), got.Actions)
	}
	status := got.Actions[0]
	if status.Kind != "action" || status.Action != "status_changed" {
		t.Errorf("first action = %+v, want kind=action action=status_changed", status)
	}
	if status.Before != "todo" || status.After != "in_progress" {
		t.Errorf("status before/after = %q → %q, want todo → in_progress", status.Before, status.After)
	}
	if status.At == "" {
		t.Error("status action has no timestamp; the lane orders on it")
	}
	assignee := got.Actions[1]
	if assignee.Before != "member:"+testUserID || assignee.After != "agent:"+testUserID {
		t.Errorf("assignee before/after = %q → %q, want member:%s → agent:%s",
			assignee.Before, assignee.After, testUserID, testUserID)
	}

	for _, a := range got.Actions {
		if a.Action == "title_changed" {
			t.Error("a human activity (no task_id) reached the run lane")
		}
		if a.Action == "priority_changed" {
			t.Error("another run's activity reached this run's lane")
		}
	}
}

func TestListTaskMessagesByUserRunWithoutActionsReturnsEmptyList(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "run-action-empty")

	var got TaskActivityResponse
	testutil.Call(t, testHandler.ListTaskMessagesByUser, taskActivityRequest(t, taskID)).
		Want(http.StatusOK).JSON(&got)

	// Empty, never null: the client distinguishes "changed nothing" (no lane)
	// from "failed to parse" (fallback), and a JSON null collapses the two.
	if got.Actions == nil {
		t.Fatal("actions is null; must be an empty array so the client can tell it apart from a parse failure")
	}
	if len(got.Actions) != 0 {
		t.Fatalf("actions = %+v, want empty", got.Actions)
	}
	if got.Messages == nil {
		t.Fatal("messages is null; must be an empty array")
	}
}

// TestRunActionBeforeAfter is the canonical matrix for the details→before/after
// mapping; the DB-backed tests above only cover the two shapes end to end.
func TestRunActionBeforeAfter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		details       map[string]any
		before, after string
	}{
		{"scalar change", map[string]any{"from": "todo", "to": "done"}, "todo", "done"},
		{"scalar set from empty", map[string]any{"from": "", "to": "2026-01-01"}, "", "2026-01-01"},
		{"scalar cleared", map[string]any{"from": "2026-01-01", "to": ""}, "2026-01-01", ""},
		{"assignee both sides", map[string]any{
			"from_type": "member", "from_id": "u1", "to_type": "agent", "to_id": "a1",
		}, "member:u1", "agent:a1"},
		{"assignee newly set", map[string]any{"to_type": "agent", "to_id": "a1"}, "", "agent:a1"},
		{"no pair at all", map[string]any{}, "", ""},
		// details is free-form JSONB: a non-string must not be coerced into a
		// plausible-looking label.
		{"non-string value", map[string]any{"from": 3, "to": true}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, after := runActionBeforeAfter(tc.details)
			if before != tc.before || after != tc.after {
				t.Errorf("runActionBeforeAfter(%v) = %q → %q, want %q → %q",
					tc.details, before, after, tc.before, tc.after)
			}
		})
	}
}
