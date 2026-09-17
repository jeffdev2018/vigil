package service

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Brain note usage (JEF-413): which runs received, retrieved or opened a note.
// Recording is best-effort everywhere: a failure is logged, never returned to
// a run or a response.

// NoteVersionsOf is the (id, revision) of each note, for RecordRunNoteUsage.
func NoteVersionsOf(notes ...db.WorkspaceNote) []NoteVersion {
	out := make([]NoteVersion, 0, len(notes))
	for _, n := range notes {
		out = append(out, NoteVersion{ID: util.UUIDToString(n.ID), Revision: n.Revision})
	}
	return out
}

// RecordRunNoteUsage records one kind of use of notes by a run through one
// channel. The note must belong to the agent's workspace (enforced in SQL);
// repeated uses by the same run insert nothing.
func RecordRunNoteUsage(ctx context.Context, q *db.Queries, taskID, agentID pgtype.UUID, kind, channel string, notes []NoteVersion) {
	if err := recordRunNoteUsage(ctx, q, taskID, agentID, kind, channel, notes); err != nil {
		slog.Warn("brain: record note usage failed",
			"task_id", util.UUIDToString(taskID), "kind", kind, "channel", channel, "error", err)
	}
}

func recordRunNoteUsage(ctx context.Context, q *db.Queries, taskID, agentID pgtype.UUID, kind, channel string, notes []NoteVersion) error {
	if len(notes) == 0 || !taskID.Valid || !agentID.Valid {
		return nil
	}
	params := db.RecordRunNoteUsageParams{Kind: kind, Channel: channel, TaskID: taskID, AgentID: agentID}
	for _, n := range notes {
		id, err := util.ParseUUID(n.ID)
		if err != nil {
			continue
		}
		params.NoteIds = append(params.NoteIds, id)
		params.NoteRevisions = append(params.NoteRevisions, n.Revision)
	}
	if len(params.NoteIds) == 0 {
		return nil
	}
	return q.RecordRunNoteUsage(ctx, params)
}

// recordInjectedNotes records a claim's injected notes inside the claim
// transaction, so a claim that fails to finalize leaves no row. The insert
// runs behind a savepoint: if it fails, only the savepoint rolls back and the
// claim still commits.
func recordInjectedNotes(ctx context.Context, tx pgx.Tx, qtx *db.Queries, task db.AgentTaskQueue, mc *TaskMemoryContext) {
	if mc == nil || len(mc.WorkspaceNotes) == 0 {
		return
	}
	if tx == nil {
		RecordRunNoteUsage(ctx, qtx, task.ID, task.AgentID, "injected", "daemon_brief", mc.WorkspaceNotes)
		return
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		slog.Warn("brain: record injected notes: savepoint failed", "task_id", util.UUIDToString(task.ID), "error", err)
		return
	}
	if err := recordRunNoteUsage(ctx, qtx.WithTx(sp), task.ID, task.AgentID, "injected", "daemon_brief", mc.WorkspaceNotes); err != nil {
		slog.Warn("brain: record injected notes failed", "task_id", util.UUIDToString(task.ID), "error", err)
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			slog.Warn("brain: record injected notes: rollback to savepoint failed", "task_id", util.UUIDToString(task.ID), "error", rbErr)
		}
		return
	}
	if err := sp.Commit(ctx); err != nil {
		slog.Warn("brain: record injected notes: release savepoint failed", "task_id", util.UUIDToString(task.ID), "error", err)
	}
}

// recordNoteUsage records a native tool's use of notes against its run.
func (s *NativeAgentService) recordNoteUsage(ctx context.Context, tctx *nativeToolContext, kind string, notes ...db.WorkspaceNote) {
	RecordRunNoteUsage(ctx, s.Queries, tctx.task.ID, tctx.agent.ID, kind, "native_tool", NoteVersionsOf(notes...))
}
