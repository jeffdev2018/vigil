package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Undo for agent actions (K69). Every side effect an agent run produces
// through the API is journaled with its inverse, so a human can reverse a
// run within the workspace's undo window. Recording never fails the write it
// describes: a journal miss is logged, the effect stands.

// Effect kinds. Keep them stable: the client labels and the reversal switch key on them.
const (
	EffectIssueStatus   = "issue_status"
	EffectIssueField    = "issue_field"
	EffectCommentCreate = "comment_create"
	EffectCommentUpdate = "comment_update"
	EffectNoteCreate    = "note_create"
	EffectNoteUpdate    = "note_update"
	EffectNoteArchive   = "note_archive"
	EffectTriageVerdict = "triage_verdict"
	EffectIssueCreate   = "issue_create"
)

type AgentEffectParams struct {
	WorkspaceID pgtype.UUID
	TaskID      pgtype.UUID
	AgentID     pgtype.UUID
	IssueID     pgtype.UUID
	Kind        string
	TargetType  string
	TargetID    pgtype.UUID
	Before      map[string]any
	After       map[string]any
	Reversible  bool
}

// RecordAgentEffect journals one side effect. A missing task or agent id
// means the write was not made by a run, so there is nothing to journal.
func RecordAgentEffect(ctx context.Context, q *db.Queries, p AgentEffectParams) {
	if q == nil || !p.TaskID.Valid || !p.AgentID.Valid || !p.WorkspaceID.Valid || !p.TargetID.Valid {
		return
	}
	before, err := json.Marshal(orEmpty(p.Before))
	if err != nil {
		return
	}
	after, err := json.Marshal(orEmpty(p.After))
	if err != nil {
		return
	}
	if _, err := q.CreateAgentEffect(ctx, db.CreateAgentEffectParams{
		ID: dbid.NewV7(), WorkspaceID: p.WorkspaceID, TaskID: p.TaskID, AgentID: p.AgentID, IssueID: p.IssueID,
		Kind: p.Kind, TargetType: p.TargetType, TargetID: p.TargetID, Before: before, After: after, Reversible: p.Reversible,
	}); err != nil {
		slog.Warn("agent effect: record failed", "kind", p.Kind, "task_id", p.TaskID, "error", err)
	}
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// Undo (K69) is the workspace policy in workspace.settings:
//
//	"undo": {"window_hours": 24, "breaker_threshold": 5}
//
// window_hours bounds how long after the fact an effect can be reversed;
// breaker_threshold is how many of one agent's runs undone in 24 hours lower
// its trust mode one notch and alert the managers. 0 disables the breaker.
type Undo struct {
	WindowHours      int `json:"window_hours"`
	BreakerThreshold int `json:"breaker_threshold"`
}

var DefaultUndo = Undo{WindowHours: 24, BreakerThreshold: 5}

func UndoSettings(settings []byte) Undo {
	out := DefaultUndo
	if len(settings) == 0 {
		return out
	}
	var s struct {
		Undo *Undo `json:"undo"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Undo == nil {
		return out
	}
	if s.Undo.WindowHours > 0 {
		out.WindowHours = s.Undo.WindowHours
	}
	if s.Undo.BreakerThreshold >= 0 {
		out.BreakerThreshold = s.Undo.BreakerThreshold
	}
	return out
}
