package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Standup and retro (K34). The standup job asks the one person who can
// unblock an issue that sat blocked, or waited on a Decision Card, longer
// than the workspace threshold: one inbox question per issue, recipient and
// day. The weekly retro groups the week's runs by outcome with each agent's
// scorecard, keeps one generated copy per week, and tells the leads.

const (
	AuditStandupAsked   = "standup.asked"
	AuditRetroGenerated = "retro.generated"
	retroRegenerateMin  = time.Hour
)

// standupRecipient picks who is asked: the module owner (K33), else a member
// assignee, else the creator. Empty means the workspace leads.
func (h *Handler) standupRecipient(ctx context.Context, issue db.Issue) pgtype.UUID {
	if s, err := h.ownershipSuggestionFor(ctx, issue); err == nil && s != nil && s.OwnerUserID != "" {
		return parseUUID(s.OwnerUserID)
	}
	if issue.AssigneeType.String == "member" && issue.AssigneeID.Valid {
		return issue.AssigneeID
	}
	return issue.CreatorID
}

func (h *Handler) askStandup(ctx context.Context, ws db.ListWorkspacesForBriefingRow, issue db.Issue, reason string, hours int, dayStart time.Time) (int, error) {
	recipients := []pgtype.UUID{}
	if r := h.standupRecipient(ctx, issue); r.Valid {
		recipients = append(recipients, r)
	} else {
		leads, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, ws.ID)
		if err != nil {
			return 0, err
		}
		for _, l := range leads {
			if l.Type == "member" {
				recipients = append(recipients, l.ID)
			}
		}
	}
	prefix := h.getIssuePrefix(ctx, ws.ID)
	identifier := fmt.Sprintf("%s-%d", prefix, issue.Number)
	question := fmt.Sprintf("%s has been %s for more than %dh. What unblocks it, and who should act?", identifier, reason, hours)
	details, _ := json.Marshal(map[string]any{"issue_id": uuidToString(issue.ID), "identifier": identifier, "reason": reason, "hours": hours})
	asked := 0
	for _, rcpt := range recipients {
		n, err := h.Queries.CountStandupQuestionsSince(ctx, db.CountStandupQuestionsSinceParams{
			WorkspaceID: ws.ID, IssueID: issue.ID, RecipientID: rcpt, CreatedAt: pgtype.Timestamptz{Time: dayStart, Valid: true},
		})
		if err != nil {
			return asked, err
		}
		if n > 0 {
			continue
		}
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: ws.ID, RecipientType: "member", RecipientID: rcpt,
			Type: "standup_question", Severity: "attention", IssueID: issue.ID,
			Title: fmt.Sprintf("Still %s after %dh: %s", reason, hours, issue.Title),
			Body:  pgtype.Text{String: question, Valid: true}, ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			return asked, err
		}
		h.publish(protocol.EventInboxNew, uuidToString(ws.ID), "system", "", map[string]any{"item": inboxToResponse(item)})
		asked++
	}
	if asked > 0 {
		h.audit(ctx, ws.ID, "system", "", AuditStandupAsked, "issue", issue.ID, map[string]any{"reason": reason, "hours": hours, "recipients": asked}, nil)
	}
	return asked, nil
}

// RunStandup is the scheduler's entry: every enabled workspace gets its
// questions for the issues stale beyond its threshold. Returns how many
// questions were filed.
func (h *Handler) RunStandup(ctx context.Context, now time.Time) (int, error) {
	workspaces, err := h.Queries.ListWorkspacesForBriefing(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	asked := 0
	for _, ws := range workspaces {
		cfg := service.StandupSettings(ws.Settings)
		if !cfg.Enabled {
			continue
		}
		loc := briefingLocation(service.WorkspaceTimezone(ws.Settings))
		local := now.In(loc)
		dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		cutoff := pgtype.Timestamptz{Time: now.Add(-time.Duration(cfg.BlockedHours) * time.Hour), Valid: true}
		seen := map[string]bool{}
		blocked, err := h.Queries.ListStaleBlockedIssues(ctx, db.ListStaleBlockedIssuesParams{WorkspaceID: ws.ID, UpdatedAt: cutoff})
		if err != nil {
			slog.Warn("standup: list blocked failed", "error", err, "workspace_id", uuidToString(ws.ID))
			continue
		}
		for _, issue := range blocked {
			seen[uuidToString(issue.ID)] = true
			n, err := h.askStandup(ctx, ws, issue, "blocked", cfg.BlockedHours, dayStart)
			asked += n
			if err != nil {
				slog.Warn("standup: ask failed", "error", err, "issue_id", uuidToString(issue.ID))
			}
		}
		waiting, err := h.Queries.ListStalePendingDecisionIssues(ctx, db.ListStalePendingDecisionIssuesParams{WorkspaceID: ws.ID, CreatedAt: cutoff})
		if err != nil {
			slog.Warn("standup: list waiting failed", "error", err, "workspace_id", uuidToString(ws.ID))
			continue
		}
		for _, issue := range waiting {
			if seen[uuidToString(issue.ID)] {
				continue
			}
			n, err := h.askStandup(ctx, ws, issue, "waiting for a decision", cfg.BlockedHours, dayStart)
			asked += n
			if err != nil {
				slog.Warn("standup: ask failed", "error", err, "issue_id", uuidToString(issue.ID))
			}
		}
	}
	return asked, nil
}

// ---- weekly retro ----

type RetroAgent struct {
	AgentID            string `json:"agent_id"`
	Name               string `json:"name"`
	RunsTotal          int64  `json:"runs_total"`
	RunsFailed         int64  `json:"runs_failed"`
	RunsAccepted       int64  `json:"runs_accepted"`
	RunsReopened       int64  `json:"runs_reopened"`
	RunsNoIntervention int64  `json:"runs_no_intervention"`
	CostUsdTicks       int64  `json:"cost_usd_ticks"`
}

type RetroRun struct {
	RunID      string  `json:"run_id"`
	IssueID    string  `json:"issue_id"`
	Identifier string  `json:"identifier"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	AgentID    string  `json:"agent_id"`
	Minutes    float64 `json:"minutes"`
	Error      string  `json:"error,omitempty"`
}

type SkillProposal struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

type WeeklyRetroResponse struct {
	WeekStart      string          `json:"week_start"`
	WeekEnd        string          `json:"week_end"`
	RunsTotal      int             `json:"runs_total"`
	RunsByStatus   map[string]int  `json:"runs_by_status"`
	MedianMinutes  float64         `json:"median_minutes"`
	Failed         []RetroRun      `json:"failed"`
	Agents         []RetroAgent    `json:"agents"`
	SkillProposals []SkillProposal `json:"skill_proposals"`
	Narrative      string          `json:"narrative"`
	GeneratedAt    *string         `json:"generated_at"`
}

// weekStartOf is the Monday 00:00 of the week holding t, in loc.
func weekStartOf(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	wd := (int(l.Weekday()) + 6) % 7
	return time.Date(l.Year(), l.Month(), l.Day()-wd, 0, 0, 0, 0, loc)
}

func (h *Handler) composeWeeklyRetro(ctx context.Context, wsID pgtype.UUID, weekStart time.Time) (WeeklyRetroResponse, error) {
	weekEnd := weekStart.AddDate(0, 0, 7)
	out := WeeklyRetroResponse{
		WeekStart: weekStart.Format("2006-01-02"), WeekEnd: weekEnd.AddDate(0, 0, -1).Format("2006-01-02"),
		RunsByStatus: map[string]int{}, Failed: []RetroRun{}, Agents: []RetroAgent{}, SkillProposals: []SkillProposal{},
	}
	runs, err := h.Queries.ListWorkspaceRunsBetween(ctx, db.ListWorkspaceRunsBetweenParams{
		WorkspaceID: wsID, CreatedAt: pgtype.Timestamptz{Time: weekStart, Valid: true}, CreatedAt_2: pgtype.Timestamptz{Time: weekEnd, Valid: true},
	})
	if err != nil {
		return out, fmt.Errorf("list runs: %w", err)
	}
	prefix := h.getIssuePrefix(ctx, wsID)
	titles := map[string]db.Issue{}
	var durations []float64
	for _, r := range runs {
		out.RunsTotal++
		out.RunsByStatus[r.Status]++
		minutes := 0.0
		if r.StartedAt.Valid && r.CompletedAt.Valid {
			minutes = r.CompletedAt.Time.Sub(r.StartedAt.Time).Minutes()
			durations = append(durations, minutes)
		}
		if r.Status == "failed" && len(out.Failed) < 10 {
			issue, ok := titles[uuidToString(r.IssueID)]
			if !ok && r.IssueID.Valid {
				if loaded, err := h.Queries.GetIssue(ctx, r.IssueID); err == nil {
					issue, ok = loaded, true
					titles[uuidToString(r.IssueID)] = loaded
				}
			}
			run := RetroRun{RunID: uuidToString(r.ID), IssueID: uuidToString(r.IssueID), Status: r.Status, AgentID: uuidToString(r.AgentID), Minutes: minutes, Error: truncateReason(r.Error.String)}
			if ok {
				run.Identifier = fmt.Sprintf("%s-%d", prefix, issue.Number)
				run.Title = issue.Title
			}
			out.Failed = append(out.Failed, run)
		}
	}
	if len(durations) > 0 {
		sort.Float64s(durations)
		out.MedianMinutes = durations[len(durations)/2]
	}
	names := map[string]string{}
	if rows, err := h.Queries.ListWorkspaceAgentNames(ctx, wsID); err == nil {
		for _, a := range rows {
			names[uuidToString(a.ID)] = a.Name
		}
	}
	var ws, we pgtype.Date
	_ = ws.Scan(weekStart.Format("2006-01-02"))
	_ = we.Scan(weekEnd.Format("2006-01-02"))
	cards, err := h.Queries.SumAgentScorecardsBetween(ctx, db.SumAgentScorecardsBetweenParams{WorkspaceID: wsID, Day: ws, Day_2: we})
	if err != nil {
		return out, fmt.Errorf("sum scorecards: %w", err)
	}
	for _, c := range cards {
		out.Agents = append(out.Agents, RetroAgent{
			AgentID: uuidToString(c.AgentID), Name: names[uuidToString(c.AgentID)], RunsTotal: c.RunsTotal, RunsFailed: c.RunsFailed,
			RunsAccepted: c.RunsAccepted, RunsReopened: c.RunsReopened, RunsNoIntervention: c.RunsNoIntervention, CostUsdTicks: c.CostUsdTicksTotal,
		})
	}
	sort.Slice(out.Agents, func(i, j int) bool { return out.Agents[i].RunsTotal > out.Agents[j].RunsTotal })
	// ponytail: skill proposals wait for Skill Miner (K58); the section stays
	// so the UI and the contract do not move when it lands.
	return out, nil
}

const retroNarrativePrompt = `You write the two-sentence summary of an engineering team's week from structured numbers about agent runs: what went well, what failed and the one change worth trying. Reply with JSON only: {"narrative":"<at most 60 words>"}. No invented facts.`

func (h *Handler) retroNarrative(ctx context.Context, retro WeeklyRetroResponse) string {
	if h.LLM == nil || !h.LLM.Enabled() || retro.RunsTotal == 0 {
		return ""
	}
	facts, _ := json.Marshal(map[string]any{"runs_total": retro.RunsTotal, "runs_by_status": retro.RunsByStatus, "median_minutes": retro.MedianMinutes, "failed": retro.Failed, "agents": retro.Agents})
	raw, err := h.LLM.GenerateJSON(ctx, "", retroNarrativePrompt, string(facts), 0.2, 256)
	if err != nil {
		slog.Warn("weekly retro: narrative failed", "error", err)
		return ""
	}
	var out struct {
		Narrative string `json:"narrative"`
	}
	if json.Unmarshal([]byte(raw), &out) != nil {
		return ""
	}
	return strings.TrimSpace(out.Narrative)
}

func retroFromRow(row db.WeeklyRetro) WeeklyRetroResponse {
	var out WeeklyRetroResponse
	_ = json.Unmarshal(row.Summary, &out)
	if out.RunsByStatus == nil {
		out.RunsByStatus = map[string]int{}
	}
	out.Narrative = row.Narrative
	out.GeneratedAt = timestampToPtr(row.GeneratedAt)
	return out
}

// generateWeeklyRetro composes, stores and, when notify is set, files the
// retro to the workspace leads.
func (h *Handler) generateWeeklyRetro(ctx context.Context, wsID pgtype.UUID, weekStart time.Time, actorType, actorID string, notify bool) (WeeklyRetroResponse, error) {
	retro, err := h.composeWeeklyRetro(ctx, wsID, weekStart)
	if err != nil {
		return retro, err
	}
	retro.Narrative = h.retroNarrative(ctx, retro)
	summary, _ := json.Marshal(retro)
	var ws pgtype.Date
	_ = ws.Scan(retro.WeekStart)
	row, err := h.Queries.UpsertWeeklyRetro(ctx, db.UpsertWeeklyRetroParams{ID: dbid.NewV7(), WorkspaceID: wsID, WeekStart: ws, Summary: summary, Narrative: retro.Narrative})
	if err != nil {
		return retro, fmt.Errorf("store retro: %w", err)
	}
	retro.GeneratedAt = timestampToPtr(row.GeneratedAt)
	h.audit(ctx, wsID, actorType, actorID, AuditRetroGenerated, "workspace", wsID, map[string]any{"week_start": retro.WeekStart, "runs_total": retro.RunsTotal, "notified": notify}, nil)
	if !notify {
		return retro, nil
	}
	leads, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, wsID)
	if err != nil {
		return retro, fmt.Errorf("list leads: %w", err)
	}
	details, _ := json.Marshal(map[string]any{"week_start": retro.WeekStart, "runs_total": retro.RunsTotal, "failed": len(retro.Failed)})
	title := fmt.Sprintf("Week of %s: %d runs · %d failed", retro.WeekStart, retro.RunsTotal, retro.RunsByStatus["failed"])
	for _, l := range leads {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: wsID, RecipientType: l.Type, RecipientID: l.ID, Type: "weekly_retro", Severity: "info",
			Title: title, Body: pgtype.Text{String: retro.Narrative, Valid: retro.Narrative != ""},
			ActorType: pgtype.Text{String: actorType, Valid: actorType != ""}, ActorID: parseUUIDOrZero(actorID), Details: details,
		})
		if err != nil {
			slog.Warn("weekly retro: inbox item failed", "error", err, "workspace_id", uuidToString(wsID))
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(wsID), actorType, actorID, map[string]any{"item": inboxToResponse(item)})
	}
	return retro, nil
}

// GenerateDueWeeklyRetros is the scheduler's entry: every workspace with the
// retro enabled and no copy for the last completed week gets one.
func (h *Handler) GenerateDueWeeklyRetros(ctx context.Context, now time.Time) (int, error) {
	workspaces, err := h.Queries.ListWorkspacesForBriefing(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	generated := 0
	for _, ws := range workspaces {
		if !service.StandupSettings(ws.Settings).WeeklyRetro {
			continue
		}
		loc := briefingLocation(service.WorkspaceTimezone(ws.Settings))
		weekStart := weekStartOf(now, loc).AddDate(0, 0, -7)
		var day pgtype.Date
		_ = day.Scan(weekStart.Format("2006-01-02"))
		if _, err := h.Queries.GetWeeklyRetro(ctx, db.GetWeeklyRetroParams{WorkspaceID: ws.ID, WeekStart: day}); err == nil {
			continue
		}
		if _, err := h.generateWeeklyRetro(ctx, ws.ID, weekStart, "system", "", true); err != nil {
			slog.Warn("weekly retro: generate failed", "error", err, "workspace_id", uuidToString(ws.ID))
			continue
		}
		generated++
	}
	return generated, nil
}

// GET /api/retro/weekly?week=YYYY-MM-DD — the stored retro for that week
// (any day of it), or the latest one.
func (h *Handler) GetWeeklyRetro(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	var row db.WeeklyRetro
	var err error
	if week := strings.TrimSpace(r.URL.Query().Get("week")); week != "" {
		t, perr := time.Parse("2006-01-02", week)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "week must be YYYY-MM-DD")
			return
		}
		var day pgtype.Date
		_ = day.Scan(weekStartOf(t, time.UTC).Format("2006-01-02"))
		row, err = h.Queries.GetWeeklyRetro(r.Context(), db.GetWeeklyRetroParams{WorkspaceID: wsUUID, WeekStart: day})
	} else {
		row, err = h.Queries.GetLatestWeeklyRetro(r.Context(), wsUUID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusNotFound, "retro_not_found", "no retro generated for that week yet")
		return
	}
	if err != nil {
		slog.Warn("weekly retro: load failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the retro")
		return
	}
	writeJSON(w, http.StatusOK, retroFromRow(row))
}

// POST /api/retro/weekly/regenerate (owner/admin) — body {"week": "YYYY-MM-DD"}
// optional, defaults to the last completed week; at most once an hour.
func (h *Handler) RegenerateWeeklyRetro(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	var req struct {
		Week string `json:"week"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	loc := briefingLocation(service.WorkspaceTimezone(ws.Settings))
	weekStart := weekStartOf(time.Now(), loc).AddDate(0, 0, -7)
	if req.Week != "" {
		t, perr := time.Parse("2006-01-02", req.Week)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "week must be YYYY-MM-DD")
			return
		}
		weekStart = weekStartOf(t, loc)
	}
	var day pgtype.Date
	_ = day.Scan(weekStart.Format("2006-01-02"))
	if row, err := h.Queries.GetWeeklyRetro(r.Context(), db.GetWeeklyRetroParams{WorkspaceID: wsUUID, WeekStart: day}); err == nil && time.Since(row.GeneratedAt.Time) < retroRegenerateMin {
		writeErrorCode(w, http.StatusTooManyRequests, "retro_rate_limited", "a retro can be regenerated once an hour")
		return
	}
	retro, err := h.generateWeeklyRetro(r.Context(), wsUUID, weekStart, "member", userID, false)
	if err != nil {
		slog.Warn("weekly retro: regenerate failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to generate the retro")
		return
	}
	writeJSON(w, http.StatusOK, retro)
}
