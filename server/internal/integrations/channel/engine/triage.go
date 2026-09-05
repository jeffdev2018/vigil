package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// TriageGate admits, holds, or refuses one `/issue` command from a channel.
// It is implemented by the handler layer, which owns the queue, its realtime
// events and its rules; the engine only asks. A Router built without a gate
// admits every channel directly, which is also every source's default mode.
type TriageGate interface {
	AdmitChannelIssue(ctx context.Context, in ChannelIssueAdmission) TriageDecision
}

// ChannelIssueAdmission describes the issue a channel is about to create.
// InstallationID is what the source's policy is keyed on: one triage source
// per installed channel, so a noisy support group can be gated without
// touching the engineering one on the same platform.
type ChannelIssueAdmission struct {
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	ChannelType    string
	OriginType     string
	OriginID       pgtype.UUID
	CreatorUserID  pgtype.UUID
	Title          string
	Description    string
}

// TriageDecision is what the gate answered.
type TriageDecision string

const (
	// TriageAdmit creates the issue now (the default for every source).
	TriageAdmit TriageDecision = "admit"
	// TriageHeld parked the material in the queue: no issue, and the sender
	// is told it is waiting for a human.
	TriageHeld TriageDecision = "held"
	// TriageRefused means an admin blocked this channel as an issue source.
	// The refusal is recorded in the queue and the sender is not answered:
	// silence is the configured behavior, not a bug.
	TriageRefused TriageDecision = "refused"
)
