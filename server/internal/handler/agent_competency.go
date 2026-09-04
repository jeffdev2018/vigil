package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Learned competency routing (K43). A rolling tally per (agent, domain)
// where the domain is the issue's first label or, failing that, the code
// area its runs touched most. It moves when an agent-assigned issue is
// accepted (+success), reopened (-success), or cancelled (+total only),
// and when a duel verdict (K39) is confirmed (wins/losses, kept apart and
// weighted twice). The score sits next to the K27/K33 signals: it never
// assigns anyone by itself.

const (
	AuditCompetency            = "competency"
	competencyDuelWeight       = 2
	competencyDefaultMinSample = 5
	competencyDomainGeneral    = "general"
)

// CompetencySettings lives under workspace.settings.competency.
type CompetencySettings struct {
	MinSample int `json:"min_sample"`
}

func competencySettings(settings []byte) CompetencySettings {
	out := CompetencySettings{MinSample: competencyDefaultMinSample}
	var s struct {
		Competency *CompetencySettings `json:"competency"`
	}
	if len(settings) > 0 && json.Unmarshal(settings, &s) == nil && s.Competency != nil && s.Competency.MinSample > 0 {
		out.MinSample = s.Competency.MinSample
	}
	return out
}

type CompetencyRow struct {
	AgentID      string  `json:"agent_id"`
	AgentName    string  `json:"agent_name,omitempty"`
	DomainKey    string  `json:"domain_key"`
	SuccessCount int32   `json:"success_count"`
	TotalCount   int32   `json:"total_count"`
	DuelWins     int32   `json:"duel_wins"`
	DuelLosses   int32   `json:"duel_losses"`
	SampleSize   int32   `json:"sample_size"`
	Score        float64 `json:"score"`
	Reliable     bool    `json:"reliable"`
	UpdatedAt    string  `json:"updated_at"`
}

// competencyScore weighs a confirmed duel twice a plain outcome.
func competencyScore(success, total, wins, losses int32) float64 {
	den := float64(total) + competencyDuelWeight*float64(wins+losses)
	if den == 0 {
		return 0
	}
	return (float64(success) + competencyDuelWeight*float64(wins)) / den
}

func competencyRow(c db.AgentDomainCompetency, name string, minSample int) CompetencyRow {
	sample := c.TotalCount + c.DuelWins + c.DuelLosses
	return CompetencyRow{
		AgentID: uuidToString(c.AgentID), AgentName: name, DomainKey: c.DomainKey, SuccessCount: c.SuccessCount, TotalCount: c.TotalCount, DuelWins: c.DuelWins, DuelLosses: c.DuelLosses,
		SampleSize: sample, Score: competencyScore(c.SuccessCount, c.TotalCount, c.DuelWins, c.DuelLosses), Reliable: int(sample) >= minSample, UpdatedAt: timestampToString(c.UpdatedAt),
	}
}

// competencyDomainKey: the first label wins; otherwise the most frequent
// top-level directory among the paths; otherwise "general".
// ponytail: top-level directory only; add an exclusion list if a config
// dir touched everywhere blurs the domains.
func competencyDomainKey(labels []string, paths []string) string {
	for _, l := range labels {
		if l = strings.TrimSpace(strings.ToLower(l)); l != "" {
			return "label:" + l
		}
	}
	counts := map[string]int{}
	for _, p := range paths {
		p = strings.TrimLeft(strings.TrimSpace(p), "./")
		if i := strings.IndexByte(p, '/'); i > 0 {
			counts[p[:i]]++
		}
	}
	best, bestN := "", 0
	for seg, n := range counts {
		if n > bestN || (n == bestN && seg < best) {
			best, bestN = seg, n
		}
	}
	if best == "" {
		return competencyDomainGeneral
	}
	return "path:" + best
}

func (h *Handler) issueDomainKey(ctx context.Context, issue db.Issue) string {
	var labels []string
	if rows, err := h.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID}); err == nil {
		for _, l := range rows {
			labels = append(labels, l.Name)
		}
	}
	paths := service.IssuePaths(issue.Title, issue.Description.String)
	if tasks, err := h.Queries.ListTasksByIssue(ctx, issue.ID); err == nil {
		for _, t := range tasks {
			paths = append(paths, jsonStrings(t.TouchedPaths)...)
		}
	}
	return competencyDomainKey(labels, paths)
}

// issueCategory resolves the workflow category of the issue's status.
func (h *Handler) issueCategory(ctx context.Context, issue db.Issue) string {
	if entry, err := issuestatus.Resolve(ctx, h.Queries, issue.WorkspaceID, issue.Status); err == nil {
		return entry.Category
	}
	return issue.Status
}

func (h *Handler) issueCancelled(ctx context.Context, issue db.Issue) bool {
	if issue.Status == "cancelled" || issue.Status == "canceled" {
		return true
	}
	entry, err := issuestatus.Resolve(ctx, h.Queries, issue.WorkspaceID, issue.Status)
	return err == nil && (entry.Category == "cancelled" || entry.Category == "canceled")
}

func (h *Handler) bumpCompetency(ctx context.Context, wsID, agentID pgtype.UUID, domain string, success, total, wins, losses int32) {
	if _, err := h.Queries.BumpAgentDomainCompetency(ctx, db.BumpAgentDomainCompetencyParams{ID: dbid.NewV7(), WorkspaceID: wsID, AgentID: agentID, DomainKey: domain, SuccessDelta: success, TotalDelta: total, WinsDelta: wins, LossesDelta: losses}); err != nil {
		slog.Warn("competency: bump failed", "agent_id", uuidToString(agentID), "domain", domain, "error", err)
	}
}

// recordCompetencyOutcome runs on a status transition of an agent-assigned
// issue. One upsert; cheap enough to stay on the request.
func (h *Handler) recordCompetencyOutcome(ctx context.Context, prev, issue db.Issue) {
	if issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		return
	}
	wasDone, isDone := h.issueAccepted(ctx, prev), h.issueAccepted(ctx, issue)
	var success, total int32
	event := ""
	switch {
	case !wasDone && isDone:
		success, total, event = 1, 1, "accepted"
	case wasDone && !isDone:
		success, event = -1, "reopened"
	case !wasDone && h.issueCancelled(ctx, issue):
		total, event = 1, "cancelled"
	case !wasDone && h.issueCategory(ctx, prev) == "in_review" && (h.issueCategory(ctx, issue) == "in_progress" || h.issueCategory(ctx, issue) == "todo" || h.issueCategory(ctx, issue) == "backlog"):
		// A review that sends the work back is a rejected attempt.
		total, event = 1, "review_rejected"
	default:
		return
	}
	domain := h.issueDomainKey(ctx, issue)
	h.bumpCompetency(ctx, issue.WorkspaceID, issue.AssigneeID, domain, success, total, 0, 0)
	h.audit(ctx, issue.WorkspaceID, "system", "", AuditCompetency, "agent", issue.AssigneeID, map[string]any{"issue_id": uuidToString(issue.ID), "domain_key": domain, "event": event, "success_delta": success, "total_delta": total}, nil)
}

// recordDuelCompetency: the confirmed winner gains a win, the loser a
// loss, in the duel issue's domain; a tie moves nothing.
func (h *Handler) recordDuelCompetency(ctx context.Context, issue db.Issue, d db.AgentDuel) {
	winner, loser := d.AgentAID, d.AgentBID
	switch d.Winner.String {
	case "b":
		winner, loser = d.AgentBID, d.AgentAID
	case "a":
	default:
		return
	}
	domain := h.issueDomainKey(ctx, issue)
	h.bumpCompetency(ctx, d.WorkspaceID, winner, domain, 0, 0, 1, 0)
	h.bumpCompetency(ctx, d.WorkspaceID, loser, domain, 0, 0, 0, 1)
	h.audit(ctx, d.WorkspaceID, "system", "", AuditCompetency, "agent", winner, map[string]any{"issue_id": uuidToString(issue.ID), "duel_id": uuidToString(d.ID), "domain_key": domain, "event": "duel_won", "against": uuidToString(loser), "weight": competencyDuelWeight}, nil)
}

// GetAgentCompetency: GET /api/agents/{id}/competency.
func (h *Handler) GetAgentCompetency(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), agent.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	minSample := competencySettings(ws.Settings).MinSample
	rows, _ := h.Queries.ListAgentDomainCompetency(r.Context(), db.ListAgentDomainCompetencyParams{WorkspaceID: agent.WorkspaceID, AgentID: agent.ID})
	out := make([]CompetencyRow, 0, len(rows))
	for _, c := range rows {
		out = append(out, competencyRow(c, agent.Name, minSample))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": uuidToString(agent.ID), "min_sample": minSample, "rows": out})
}

// GetAssigneeSuggestion: GET /api/issues/{id}/assignee-suggestion — the
// issue's domain, every agent's history in it (best first), and the K33
// ownership suggestion side by side.
func (h *Handler) GetAssigneeSuggestion(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	minSample := competencySettings(ws.Settings).MinSample
	domain := h.issueDomainKey(r.Context(), issue)
	rows, _ := h.Queries.ListDomainCompetency(r.Context(), db.ListDomainCompetencyParams{WorkspaceID: issue.WorkspaceID, DomainKey: domain})
	candidates := make([]CompetencyRow, 0, len(rows))
	for _, c := range rows {
		candidates = append(candidates, competencyRow(db.AgentDomainCompetency{ID: c.ID, WorkspaceID: c.WorkspaceID, AgentID: c.AgentID, DomainKey: c.DomainKey, SuccessCount: c.SuccessCount, TotalCount: c.TotalCount, DuelWins: c.DuelWins, DuelLosses: c.DuelLosses, UpdatedAt: c.UpdatedAt}, c.AgentName, minSample))
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Reliable != b.Reliable {
			return a.Reliable
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.SampleSize > b.SampleSize
	})
	ownership, err := h.ownershipSuggestionFor(r.Context(), issue)
	if err != nil {
		slog.Warn("assignee suggestion: ownership failed", "error", err, "issue_id", uuidToString(issue.ID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain_key": domain, "min_sample": minSample, "candidates": candidates, "ownership": ownership})
}

// GetCompetencySettings: GET /api/competency-settings.
func (h *Handler) GetCompetencySettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, competencySettings(ws.Settings))
}

// PutCompetencySettings: PUT /api/competency-settings {min_sample}.
func (h *Handler) PutCompetencySettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req CompetencySettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MinSample < 1 || req.MinSample > 1000 {
		writeError(w, http.StatusBadRequest, "min_sample must be between 1 and 1000")
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	settings := map[string]any{}
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &settings)
	}
	settings["competency"] = req
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save competency settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditCompetency, "workspace", wsUUID, map[string]any{"min_sample": req.MinSample}, nil)
	writeJSON(w, http.StatusOK, req)
}
