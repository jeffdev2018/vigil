package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/blastradius"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Issue router (K27). Risk comes from the project's blast radius rules
// (K07) matched against the paths an issue names; each risk level maps to a
// runtime pool (K28); repeated failures on an issue escalate it one level
// up. The decision is written on the task and never taken silently.

const (
	RiskLow    = "low"    // every named path is autonomous
	RiskNormal = "normal" // no rule matched, or paths inherit
	RiskHigh   = "high"   // a named path needs dual approval or is read-only
)

var riskOrder = []string{RiskLow, RiskNormal, RiskHigh}

// Routing is the workspace setting under settings.routing.
type Routing struct {
	Enabled bool `json:"enabled"`
	// Pools maps a risk level to a pool id; a missing level falls back to the agent's own pool.
	Pools map[string]string `json:"pools"`
	// EscalationFailures is how many consecutive failed runs of an issue
	// move it one level up. 0 disables escalation.
	EscalationFailures int `json:"escalation_failures"`
}

var DefaultRouting = Routing{Enabled: false, Pools: map[string]string{}, EscalationFailures: 2}

func RoutingSettings(settings []byte) Routing {
	out := DefaultRouting
	out.Pools = map[string]string{}
	if len(settings) == 0 {
		return out
	}
	var s struct {
		Routing *Routing `json:"routing"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Routing == nil {
		return out
	}
	out.Enabled = s.Routing.Enabled
	for _, level := range riskOrder {
		if id := strings.TrimSpace(s.Routing.Pools[level]); id != "" {
			out.Pools[level] = id
		}
	}
	if s.Routing.EscalationFailures >= 0 {
		out.EscalationFailures = s.Routing.EscalationFailures
	}
	return out
}

// RoutingDecision is stored on agent_task_queue.routing_decision.
type RoutingDecision struct {
	RiskLevel        string   `json:"risk_level"`
	MatchedPaths     []string `json:"matched_paths"`
	TargetPoolID     string   `json:"target_pool_id,omitempty"`
	TargetPoolName   string   `json:"target_pool_name,omitempty"`
	RuntimeID        string   `json:"runtime_id,omitempty"`
	Escalated        bool     `json:"escalated"`
	EscalationReason string   `json:"escalation_reason,omitempty"`
	DecidedAt        string   `json:"decided_at"`
}

// pathToken finds file-path-looking words in free text: at least one slash
// or a dotted file name, no spaces. ponytail: naive scan; the ticket rules
// out a classifier for V1.
var pathToken = regexp.MustCompile(`(?:^|[\s(\x60"'])((?:[\w.-]+/)+[\w.*-]+|[\w-]+\.[a-zA-Z]{2,5})(?:[\s)\x60"',.:;]|$)`)

// IssuePaths lists the distinct path-like tokens an issue names.
func IssuePaths(title, description string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range pathToken.FindAllStringSubmatch(title+"\n"+description, -1) {
		p := strings.TrimSuffix(strings.TrimPrefix(m[1], "./"), ".")
		if p == "" || seen[p] || strings.Count(p, ".") > 3 {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// ClassifyRisk turns the worst blast radius level among the named paths
// into a risk level. No path or no rule is normal risk.
func ClassifyRisk(rules []blastradius.Rule, paths []string) (level string, matched []string) {
	if len(rules) == 0 || len(paths) == 0 {
		return RiskNormal, nil
	}
	worst := ""
	for _, p := range paths {
		if rule, ok := blastradius.Resolve(rules, p); ok {
			matched = append(matched, p)
			if rank(rule.Level) > rank(worst) {
				worst = rule.Level
			}
		}
	}
	switch worst {
	case blastradius.LevelDualApproval, blastradius.LevelReadOnly:
		return RiskHigh, matched
	case blastradius.LevelAutonomous:
		return RiskLow, matched
	}
	return RiskNormal, matched
}

func rank(level string) int {
	switch level {
	case blastradius.LevelAutonomous:
		return 1
	case blastradius.LevelDualApproval:
		return 2
	case blastradius.LevelReadOnly:
		return 3
	}
	return 0
}

// escalate moves a risk level one step up; high stays high.
func escalate(level string) string {
	for i, l := range riskOrder {
		if l == level && i+1 < len(riskOrder) {
			return riskOrder[i+1]
		}
	}
	return level
}

// consecutiveFailures counts failed runs since the last completed one.
func consecutiveFailures(rows []db.ListRecentIssueTaskOutcomesRow) int {
	n := 0
	for _, r := range rows {
		switch r.Status {
		case "failed":
			n++
		case "completed":
			return n
		}
	}
	return n
}

// routeIssueTask (K27) decides pool and runtime for a new run of an issue.
// It returns the runtime to enqueue on and the decision to store; ok is
// false when routing is off or nothing applies, so K28's own choice stands.
func (s *TaskService) routeIssueTask(ctx context.Context, issue db.Issue, agent db.Agent) (runtimeID pgtype.UUID, decision *RoutingDecision, ok bool) {
	ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return pgtype.UUID{}, nil, false
	}
	cfg := RoutingSettings(ws.Settings)
	if !cfg.Enabled {
		return pgtype.UUID{}, nil, false
	}
	var rules []blastradius.Rule
	if issue.ProjectID.Valid {
		if rows, err := s.Queries.ListBlastRadiusRules(ctx, db.ListBlastRadiusRulesParams{WorkspaceID: issue.WorkspaceID, ProjectID: issue.ProjectID}); err == nil {
			for _, r := range rows {
				rules = append(rules, blastradius.Rule{ID: util.UUIDToString(r.ID), Pattern: r.PathPattern, Level: r.AutonomyLevel})
			}
		}
	}
	level, matched := ClassifyRisk(rules, IssuePaths(issue.Title, issue.Description.String))
	d := &RoutingDecision{RiskLevel: level, MatchedPaths: matched, DecidedAt: time.Now().UTC().Format(time.RFC3339)}
	if d.MatchedPaths == nil {
		d.MatchedPaths = []string{}
	}
	if cfg.EscalationFailures > 0 {
		if rows, err := s.Queries.ListRecentIssueTaskOutcomes(ctx, issue.ID); err == nil {
			if n := consecutiveFailures(rows); n >= cfg.EscalationFailures {
				if up := escalate(level); up != level && cfg.Pools[up] != "" && cfg.Pools[up] != cfg.Pools[level] {
					d.Escalated = true
					d.EscalationReason = fmt.Sprintf("%d consecutive failed runs on the %s pool", n, level)
					d.RiskLevel = up
				}
			}
		}
	}
	poolID := cfg.Pools[d.RiskLevel]
	if poolID == "" {
		// No pool for this level: the agent's own pool (K28) or runtime decides.
		return pgtype.UUID{}, d, true
	}
	pid, err := util.ParseUUID(poolID)
	if err != nil {
		return pgtype.UUID{}, d, true
	}
	pool, err := s.Queries.GetRuntimePool(ctx, pid)
	if err != nil || pool.WorkspaceID != issue.WorkspaceID {
		slog.Warn("issue router: configured pool missing", "pool_id", poolID, "risk", d.RiskLevel)
		return pgtype.UUID{}, d, true
	}
	d.TargetPoolID, d.TargetPoolName = util.UUIDToString(pool.ID), pool.Name
	var ids []string
	_ = json.Unmarshal(pool.RuntimeIds, &ids)
	if pool.DegradedRuntimeID.Valid {
		ids = append(ids, util.UUIDToString(pool.DegradedRuntimeID))
	}
	for _, id := range ids {
		rtID, err := util.ParseUUID(id)
		if err != nil {
			continue
		}
		rt, err := s.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{ID: rtID, WorkspaceID: issue.WorkspaceID})
		if err != nil || rt.Status != "online" {
			continue
		}
		d.RuntimeID = id
		return rt.ID, d, true
	}
	// Pool has nothing online: fall back to the agent, decision still recorded.
	return pgtype.UUID{}, d, true
}
