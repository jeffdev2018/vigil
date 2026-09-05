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
	// EffectIssueUpdate is the pending shape of an issue write: the request
	// fields, replayed on approval and journaled as the kinds above.
	EffectIssueUpdate = "issue_update"
	// Deletions an agent asked for; the reverse re-creates the row under its old id.
	EffectCommentDelete = "comment_delete"
	EffectNoteDelete    = "note_delete"
	// A chat reply the run produced; when the session is bound to a channel
	// the reply reached the provider, and the reverse posts a corrective
	// message in the same thread (no provider can delete).
	EffectChatMessage = "chat_message"
)

// Effect statuses.
const (
	EffectApplied  = "applied"
	EffectPending  = "pending"
	EffectApproved = "approved"
	EffectRejected = "rejected"
)

// Agent effect modes: apply writes as they come, or hold them for approval.
const (
	EffectModeApply   = "apply"
	EffectModePreview = "preview"
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
		Status: EffectApplied, Payload: []byte("{}"),
	}); err != nil {
		slog.Warn("agent effect: record failed", "kind", p.Kind, "task_id", p.TaskID, "error", err)
	}
}

// RecordPendingAgentEffect journals a write an agent in preview mode asked
// for but that was not applied: payload is what approval will replay.
func RecordPendingAgentEffect(ctx context.Context, q *db.Queries, p AgentEffectParams, payload map[string]any) (db.AgentEffect, error) {
	raw, err := json.Marshal(orEmpty(payload))
	if err != nil {
		return db.AgentEffect{}, err
	}
	after, _ := json.Marshal(orEmpty(p.After))
	return q.CreateAgentEffect(ctx, db.CreateAgentEffectParams{
		ID: dbid.NewV7(), WorkspaceID: p.WorkspaceID, TaskID: p.TaskID, AgentID: p.AgentID, IssueID: p.IssueID,
		Kind: p.Kind, TargetType: p.TargetType, TargetID: p.TargetID, Before: []byte("{}"), After: after, Reversible: p.Reversible,
		Status: EffectPending, Payload: raw,
	})
}

// AgentPreviewsEffects reports whether the agent holds its writes for approval.
func AgentPreviewsEffects(ctx context.Context, q *db.Queries, agentID pgtype.UUID) bool {
	agent, err := q.GetAgent(ctx, agentID)
	return err == nil && agent.EffectMode == EffectModePreview
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
