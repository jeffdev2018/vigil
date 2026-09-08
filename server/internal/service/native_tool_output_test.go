package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A tool result belongs in task_message.output, which is where the daemon path
// writes it and where every reader — the transcript API, the UI, and the agent
// re-reading its own run — looks for it. The native runtime wrote it to
// `content`, so its rows came back with an empty `output`: present, named after
// the tool, and silent about what the tool answered.
func TestNativeToolResultLandsInOutput(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-out-%d", suffix), fmt.Sprintf("native-out-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-out-ws-%d", suffix), fmt.Sprintf("native-out-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{"provider": "native", "runtime_mode": "native", "daemon_id": "native"})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	issueID := fx.Issue(t, "Tool output")
	task := parseUUIDForTest(t, fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID}))

	svc := &NativeAgentService{Queries: db.New(pool)}
	svc.writeToolResult(ctx, task, "transition_issue", `{"held":true,"reason":"an approval rule holds this move"}`)

	var kind, tool, content, output string
	if err := pool.QueryRow(ctx,
		`SELECT type, COALESCE(tool,''), COALESCE(content,''), COALESCE(output,'')
		   FROM task_message WHERE task_id = $1 ORDER BY seq DESC LIMIT 1`,
		task,
	).Scan(&kind, &tool, &content, &output); err != nil {
		t.Fatalf("read transcript row: %v", err)
	}
	if kind != "tool_result" || tool != "transition_issue" {
		t.Fatalf("row = %s / %s, want a tool_result for transition_issue", kind, tool)
	}
	if output == "" {
		t.Fatal("output is empty: the reader learns nothing about what the tool answered")
	}
	if content != "" {
		t.Errorf("content = %q, want the payload in output only — two columns holding the same thing is how they drift", content)
	}
}

func parseUUIDForTest(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}
