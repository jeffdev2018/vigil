package service

// Follow-ups (OS plan, vague B, réveil programmé): one vocabulary for "come
// back to this later" shared by the native tool, the REST endpoint, the CLI
// and MCP. The row is a deferred agent task; this file owns the rules every
// entry point applies: when a follow-up may fire, how long a note is, and
// how many an agent or a workspace may schedule per day.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

const (
	FollowupMinDelay     = time.Minute
	FollowupMaxDelay     = 30 * 24 * time.Hour
	FollowupNoteMaxRunes = 500
	// FollowupSummaryPrefix is how the note rides on the task row.
	FollowupSummaryPrefix = "Follow-up: "
	FollowupDefaultNote   = "Scheduled follow-up."

	followupDefaultPerAgentPerDay     = 20
	followupDefaultPerWorkspacePerDay = 200
	followupBudgetCeiling             = 1000
)

// FollowupSettings is the per-day budget, read off workspace.settings.followups.
type FollowupSettings struct {
	MaxPerAgentPerDay     int `json:"max_per_agent_per_day"`
	MaxPerWorkspacePerDay int `json:"max_per_workspace_per_day"`
}

// FollowupSettingsFrom reads the budget; missing or out of range falls back
// to the defaults, so a workspace cannot configure an unbounded wake-up loop.
func FollowupSettingsFrom(settings []byte) FollowupSettings {
	out := FollowupSettings{MaxPerAgentPerDay: followupDefaultPerAgentPerDay, MaxPerWorkspacePerDay: followupDefaultPerWorkspacePerDay}
	var s struct {
		Followups *struct {
			MaxPerAgentPerDay     *int `json:"max_per_agent_per_day"`
			MaxPerWorkspacePerDay *int `json:"max_per_workspace_per_day"`
		} `json:"followups"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.Followups == nil {
		return out
	}
	if v := s.Followups.MaxPerAgentPerDay; v != nil && *v > 0 && *v <= followupBudgetCeiling {
		out.MaxPerAgentPerDay = *v
	}
	if v := s.Followups.MaxPerWorkspacePerDay; v != nil && *v > 0 && *v <= followupBudgetCeiling {
		out.MaxPerWorkspacePerDay = *v
	}
	return out
}

// ParseFollowupWhen accepts RFC 3339 or "+<minutes>" and bounds the result
// to [now+1min, now+30d].
func ParseFollowupWhen(raw string, now time.Time) (time.Time, error) {
	when := strings.TrimSpace(raw)
	if when == "" {
		return time.Time{}, errors.New("when is required (RFC 3339 or +minutes)")
	}
	var fireAt time.Time
	if strings.HasPrefix(when, "+") {
		mins, err := strconv.Atoi(strings.TrimPrefix(when, "+"))
		if err != nil || mins < 1 {
			return time.Time{}, errors.New("offset must be +<minutes> with at least 1 minute")
		}
		fireAt = now.Add(time.Duration(mins) * time.Minute)
	} else {
		parsed, err := time.Parse(time.RFC3339, when)
		if err != nil {
			return time.Time{}, errors.New("when must be RFC 3339 (e.g. 2026-09-11T09:00:00+02:00) or +minutes")
		}
		fireAt = parsed
	}
	if fireAt.Before(now.Add(FollowupMinDelay)) {
		return time.Time{}, errors.New("the follow-up must fire at least 1 minute from now")
	}
	if fireAt.After(now.Add(FollowupMaxDelay)) {
		return time.Time{}, errors.New("the follow-up must fire within 30 days")
	}
	return fireAt, nil
}

// NormalizeFollowupNote trims, sanitizes and caps the note; empty gets the
// default so the trigger summary always reads as a follow-up.
func NormalizeFollowupNote(note string) string {
	note = strings.TrimSpace(util.SanitizeTextForPostgres(note))
	if utf8.RuneCountInString(note) > FollowupNoteMaxRunes {
		note = string([]rune(note)[:FollowupNoteMaxRunes])
	}
	if note == "" {
		note = FollowupDefaultNote
	}
	return note
}

// FollowupNoteFromSummary is the inverse of the trigger summary stamp.
func FollowupNoteFromSummary(summary string) string {
	return strings.TrimPrefix(summary, FollowupSummaryPrefix)
}

// FollowupInput is what every entry point provides to schedule one.
type FollowupInput struct {
	WorkspaceID pgtype.UUID
	IssueID     pgtype.UUID
	Agent       db.Agent
	Priority    int32
	When        string
	Note        string
	// ByType is "member" or "agent"; ByID the user or agent id.
	ByType string
	ByID   pgtype.UUID
	// Settings is the workspace budget; zero value uses the defaults.
	Settings FollowupSettings
}

// ErrFollowupBudget is returned when the per-day budget is spent; the
// message names the bound so the caller can show it.
type ErrFollowupBudget struct {
	Scope string
	Max   int
}

func (e ErrFollowupBudget) Error() string {
	return fmt.Sprintf("follow-up budget reached: at most %d per %s per day", e.Max, e.Scope)
}

// ScheduleFollowup validates, checks the budget and files the deferred task.
// The audit entry and the realtime event belong to the caller.
func ScheduleFollowup(ctx context.Context, q *db.Queries, in FollowupInput) (db.AgentTaskQueue, error) {
	now := time.Now()
	fireAt, err := ParseFollowupWhen(in.When, now)
	if err != nil {
		return db.AgentTaskQueue{}, err
	}
	note := NormalizeFollowupNote(in.Note)
	settings := in.Settings
	if settings.MaxPerAgentPerDay <= 0 || settings.MaxPerWorkspacePerDay <= 0 {
		settings = FollowupSettingsFrom(nil)
	}
	since := pgtype.Timestamptz{Time: now.Add(-24 * time.Hour), Valid: true}
	if n, err := q.CountAgentFollowupsSince(ctx, db.CountAgentFollowupsSinceParams{AgentID: in.Agent.ID, CreatedAt: since}); err == nil && int(n) >= settings.MaxPerAgentPerDay {
		return db.AgentTaskQueue{}, ErrFollowupBudget{Scope: "agent", Max: settings.MaxPerAgentPerDay}
	}
	if n, err := q.CountWorkspaceFollowupsSince(ctx, db.CountWorkspaceFollowupsSinceParams{WorkspaceID: in.WorkspaceID, CreatedAt: since}); err == nil && int(n) >= settings.MaxPerWorkspacePerDay {
		return db.AgentTaskQueue{}, ErrFollowupBudget{Scope: "workspace", Max: settings.MaxPerWorkspacePerDay}
	}
	params := db.CreateDeferredAgentTaskParams{
		ID:                   dbid.NewV7(),
		AgentID:              in.Agent.ID,
		RuntimeID:            in.Agent.RuntimeID,
		IssueID:              in.IssueID,
		Priority:             in.Priority,
		TriggerSummary:       pgtype.Text{String: FollowupSummaryPrefix + note, Valid: true},
		FireAt:               pgtype.Timestamptz{Time: fireAt, Valid: true},
		TriggerEvidenceKind:  pgtype.Text{String: "followup", Valid: true},
		TriggerEvidenceRefID: in.ByID,
	}
	if in.ByType == "member" {
		params.OriginatorUserID = in.ByID
		params.AccountableUserID = in.ByID
	}
	task, err := q.CreateDeferredAgentTask(ctx, params)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("follow-up could not be scheduled: %w", err)
	}
	return task, nil
}
