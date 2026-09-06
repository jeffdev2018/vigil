package service

import (
	"encoding/json"
	"fmt"
	"time"
)

// Code health autopilot (K22).
//
// Maintenance work is the work nobody schedules: debt, dependencies that went
// stale, code that never got a test. This is the scheduled read-only pass that
// names it — a maintenance agent reads the repository on a cron, reports what
// it found, and the server turns the findings it trusts into ordinary issues
// assigned to that agent. From there it is normal agent work: the same budget
// policies (K03), permission profiles (K06), blast radius (K07) and
// checkpoints (K20) apply, because nothing about a maintenance issue is
// special.
//
// Disabled by default, admin-only to configure. The whole configuration lives
// under workspace.settings.code_health, so there is no table for it.

const (
	// CodeHealthDefaultCron is Monday 03:00 — weekly, outside working hours.
	CodeHealthDefaultCron     = "0 3 * * 1"
	CodeHealthDefaultTimezone = "UTC"

	CodeHealthDefaultMaxIssues     = 5
	CodeHealthMinMaxIssues         = 1
	CodeHealthMaxMaxIssues         = 20
	CodeHealthDefaultMinConfidence = 70
)

// CodeHealthSettings is the workspace configuration of the autopilot.
type CodeHealthSettings struct {
	Enabled bool `json:"enabled"`
	// Cron is a standard 5-field expression, read in Timezone.
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
	// AgentID is the maintenance agent: it runs the scan and owns every issue
	// the scan opens. Required to enable.
	AgentID string `json:"agent_id"`
	// ProjectID optionally scopes the scan (and the issues it opens) to one
	// project. Empty means the whole workspace.
	ProjectID string `json:"project_id"`
	// MaxIssuesPerScan caps how much maintenance one scan may put on the
	// board, whatever the agent reports.
	MaxIssuesPerScan int `json:"max_issues_per_scan"`
	// MinConfidence drops findings the agent is not sure about, 0..100.
	MinConfidence int `json:"min_confidence"`
	// BudgetPolicyID is the dedicated maintenance budget: a budget_policy
	// scoped to the maintenance agent. Purely informational here — enqueue
	// admission already enforces whatever policy covers the agent (K03) — so
	// the field only records which policy an admin means by "the maintenance
	// budget".
	BudgetPolicyID string `json:"budget_policy_id"`
	// EnabledAt anchors the cron before the first scan ever runs, so enabling
	// on a Monday afternoon does not fire that same evening's occurrence.
	// Stamped by the settings endpoint, never by a client.
	EnabledAt time.Time `json:"enabled_at"`
}

// CodeHealthDefaults is what a workspace that never configured this reads as.
func CodeHealthDefaults() CodeHealthSettings {
	return CodeHealthSettings{
		Cron:             CodeHealthDefaultCron,
		Timezone:         CodeHealthDefaultTimezone,
		MaxIssuesPerScan: CodeHealthDefaultMaxIssues,
		MinConfidence:    CodeHealthDefaultMinConfidence,
	}
}

// CodeHealthFromSettings reads the configuration off a workspace settings
// blob. Anything missing, unparseable or out of range falls back to the
// default — in particular Enabled stays false, so a corrupt blob never turns
// the autopilot on.
func CodeHealthFromSettings(settings []byte) CodeHealthSettings {
	out := CodeHealthDefaults()
	var s struct {
		CodeHealth *CodeHealthSettings `json:"code_health"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.CodeHealth == nil {
		return out
	}
	got := *s.CodeHealth
	out.Enabled = got.Enabled
	out.AgentID = got.AgentID
	out.ProjectID = got.ProjectID
	out.BudgetPolicyID = got.BudgetPolicyID
	out.EnabledAt = got.EnabledAt
	if got.Cron != "" {
		out.Cron = got.Cron
	}
	if got.Timezone != "" {
		out.Timezone = got.Timezone
	}
	if got.MaxIssuesPerScan >= CodeHealthMinMaxIssues && got.MaxIssuesPerScan <= CodeHealthMaxMaxIssues {
		out.MaxIssuesPerScan = got.MaxIssuesPerScan
	}
	if got.MinConfidence >= 0 && got.MinConfidence <= 100 {
		out.MinConfidence = got.MinConfidence
	}
	return out
}

// ValidateCodeHealthSettings reports what is wrong with a submitted
// configuration, or nil. The agent is checked by the handler, which is the
// only layer that can resolve it against the workspace.
func ValidateCodeHealthSettings(s CodeHealthSettings) error {
	if s.MaxIssuesPerScan < CodeHealthMinMaxIssues || s.MaxIssuesPerScan > CodeHealthMaxMaxIssues {
		return fmt.Errorf("max_issues_per_scan must be between %d and %d", CodeHealthMinMaxIssues, CodeHealthMaxMaxIssues)
	}
	if s.MinConfidence < 0 || s.MinConfidence > 100 {
		return fmt.Errorf("min_confidence must be between 0 and 100")
	}
	if err := ValidateTimezone(s.Timezone); err != nil {
		return err
	}
	// Same parser the autopilot schedule triggers use, so an expression that
	// is accepted here is one the scheduler can actually enumerate.
	if _, err := NextOccurrenceAfterUTC(s.Cron, s.Timezone, time.Now()); err != nil {
		return fmt.Errorf("invalid cron expression")
	}
	if s.Enabled && s.AgentID == "" {
		return fmt.Errorf("agent_id is required to enable the code health autopilot")
	}
	return nil
}

// CodeHealthDue answers "should this workspace be scanned now?". anchor is the
// last scan (or, before the first one, when the autopilot was enabled); a zero
// anchor means the schedule has nothing to measure from and the workspace
// waits for the next configured occurrence after now.
//
// Fails CLOSED: an unparseable cron does not scan. It cannot get here through
// the settings endpoint, but a blob edited by hand can carry one.
func CodeHealthDue(s CodeHealthSettings, anchor, now time.Time) bool {
	if !s.Enabled || anchor.IsZero() {
		return false
	}
	next, err := NextOccurrenceAfterUTC(s.Cron, s.Timezone, anchor)
	if err != nil || next.IsZero() {
		return false
	}
	return !next.After(now)
}
