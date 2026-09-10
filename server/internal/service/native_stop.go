package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Cooperative stop (N09). A cancelled run or a halted workspace must not keep
// burning turns until the timeout: between turns, the loop re-reads the task
// row and the workspace halt and leaves cleanly. The check is one indexed
// read per turn (the task) plus one settings read per run start; the halt is
// re-read only every nativeHaltCheckInterval so a fleet-wide halt costs one
// settings read per run per interval, not per turn.

var errNativeRunStopped = errors.New("native run stopped")

type nativeStopCheck struct {
	queries    *db.Queries
	lastHaltAt int64
	cachedHalt bool
	cachedWhy  string
}

func (c *nativeStopCheck) shouldStop(ctx context.Context, taskID, wsID pgtype.UUID, now func() int64) (bool, string) {
	// 1. Task state: cancelled by a human beats everything.
	task, err := c.queries.GetAgentTask(ctx, taskID)
	if err == nil && task.Status == "cancelled" {
		return true, "the run was cancelled"
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
	return c.cachedHalt, c.cachedWhy
}
