package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Cooperative stop (N09 + N14). A cancelled run, a halted workspace, or an
// issue that closed under the run must not keep burning turns until the
// timeout: between turns, the loop re-reads the task row, the workspace
// halt and (when the run owns an issue) the issue status, and leaves cleanly.
// The check is one indexed read per turn (the task) plus one settings read
// per interval and one issue read when applicable.

var errNativeRunStopped = errors.New("native run stopped")

// errNativeIssueTerminal: the issue closed under the run. Unlike cancel/halt,
// the task must be failed with ReasonIssueTerminal so it matches the sweeper.
var errNativeIssueTerminal = errors.New("native run: issue terminal")

type nativeStopCheck struct {
	queries    *db.Queries
	issueID    pgtype.UUID
	lastHaltAt int64
	cachedHalt bool
	cachedWhy  string
}

func (c *nativeStopCheck) shouldStop(ctx context.Context, taskID, wsID pgtype.UUID, now func() int64) (stop bool, why string, issueTerminal bool) {
	// 1. Task state: cancelled by a human beats everything.
	task, err := c.queries.GetAgentTask(ctx, taskID)
	if err == nil && task.Status == "cancelled" {
		return true, "the run was cancelled", false
	}
	// 2. Workspace halt, re-read at most once per interval.
	if now() >= c.lastHaltAt+nativeHaltCheckInterval.Milliseconds() {
		c.lastHaltAt = now()
		ws, err := c.queries.GetWorkspace(ctx, wsID)
		if err == nil {
			halt := RunHaltFromSettings(ws.Settings)
			c.cachedHalt = halt.Halted
			c.cachedWhy = halt.Message()
		} else {
			// Unreadable settings: RunHaltFromSettings fails closed, so mirror
			// that rather than guessing open.
			c.cachedHalt = true
			c.cachedWhy = "the workspace state could not be read, so the run holds"
		}
	}
	if c.cachedHalt {
		return true, c.cachedWhy, false
	}
	// 3. Issue closed under the run (N14): same terminal categories the
	// SweepTasksOnTerminalIssues sweeper uses.
	if c.issueID.Valid {
		issue, err := c.queries.GetIssue(ctx, c.issueID)
		if err == nil {
			cat := issuestatus.Effective(ctx, c.queries, wsID, issue.Status)
			if cat == issuestatus.Done || cat == issuestatus.Cancelled {
				return true, "the issue was closed while this run was still going", true
			}
		}
	}
	return false, "", false
}

// nativeIssueIsTerminal reports whether an issue status is done/cancelled for
// the workspace — used at run entry to refuse starting on a closed issue.
func nativeIssueIsTerminal(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, status string) bool {
	if q == nil {
		return false
	}
	cat := issuestatus.Effective(ctx, q, workspaceID, status)
	return cat == issuestatus.Done || cat == issuestatus.Cancelled
}
