package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fleet page (OS plan, chantier 4). One list of every run in the workspace
// — queued, running, waiting on somebody, over — with what each costs and
// what it is blocked on, plus the two controls a fleet needs: cancel these,
// and the kill switch (halt the fleet and cancel everything in flight).
//
// A run has no workspace of its own; every read here goes through the
// agents the caller may see, so a private agent's runs stay private.

const (
	runsDefaultLimit = 50
	runsMaxLimit     = 200
	runsCancelMax    = 500

	RunStateActive   = "active"
	RunStateTerminal = "terminal"
	RunStateAll      = "all"

	RunBlockerGate           = "gate"
	RunBlockerDecision       = "decision"
	RunBlockerGoalQuestion   = "goal_question"
	RunBlockerTransition     = "transition"
	RunBlockerLocalDirectory = "local_directory"
	RunBlockerPaused         = "paused"
	RunBlockerDeferred       = "deferred"

	AuditRunsCancelled = "runs.cancelled"
	AuditKillSwitch    = "runs.kill_switch"
)

var (
	runActiveStatuses   = []string{"queued", "dispatched", "running", "waiting_local_directory", "paused", "deferred"}
	runTerminalStatuses = []string{"completed", "failed", "cancelled"}
)

// RunBlocker says why a run is not moving and where to settle it.
type RunBlocker struct {
	Kind       string  `json:"kind"`
	ID         string  `json:"id,omitempty"`
	DecisionID string  `json:"decision_id,omitempty"`
	Summary    string  `json:"summary"`
	Since      *string `json:"since"`
}

// RunIssueRef names the issue a run works on.
type RunIssueRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
}

// RunResponse is one fleet row: the task as the app already knows it, plus
// the names, the cost and the blocker the list needs.
type RunResponse struct {
	AgentTaskResponse
	AgentName    string       `json:"agent_name"`
	Issue        *RunIssueRef `json:"issue"`
	CostUsdTicks int64        `json:"cost_usd_ticks"`
	DurationMs   int64        `json:"duration_ms"`
	SilenceMs    int64        `json:"silence_ms"`
	BlockedOn    *RunBlocker  `json:"blocked_on"`
}

// RunsSummary heads the page: the fleet at a glance.
type RunsSummary struct {
	Active            int64           `json:"active"`
	Queued            int64           `json:"queued"`
	Running           int64           `json:"running"`
	Blocked           int64           `json:"blocked"`
	CompletedSince    int64           `json:"completed_since"`
	FailedSince       int64           `json:"failed_since"`
	CancelledSince    int64           `json:"cancelled_since"`
	CostSinceUsdTicks int64           `json:"cost_since_usd_ticks"`
	Since             string          `json:"since"`
	RunHalt           service.RunHalt `json:"run_halt"`
}

// RunsResponse is the list envelope.
type RunsResponse struct {
	Runs       []RunResponse `json:"runs"`
	NextCursor string        `json:"next_cursor,omitempty"`
	Summary    RunsSummary   `json:"summary"`
}

type runsFilter struct {
	statuses  []string
	agentID   pgtype.UUID
	issueID   pgtype.UUID
	runtimeID pgtype.UUID
	since     pgtype.Timestamptz
	cursorAt  pgtype.Timestamptz
	cursorID  pgtype.UUID
	limit     int32
}

func encodeRunsCursor(t db.AgentTaskQueue) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.CreatedAt.Time.UTC().Format(time.RFC3339Nano) + "|" + uuidToString(t.ID)))
}

func decodeRunsCursor(s string) (pgtype.Timestamptz, pgtype.UUID, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	u, ok := decisionUUID(id)
	if !ok {
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	return pgtype.Timestamptz{Time: ts, Valid: true}, u, true
}

func parseRunsFilter(w http.ResponseWriter, r *http.Request) (runsFilter, bool) {
	q := r.URL.Query()
	f := runsFilter{limit: runsDefaultLimit}
	switch state := strings.TrimSpace(q.Get("state")); state {
	case "", RunStateAll:
	case RunStateActive:
		f.statuses = runActiveStatuses
	case RunStateTerminal:
		f.statuses = runTerminalStatuses
	default:
		writeError(w, http.StatusBadRequest, "state must be active, terminal or all")
		return f, false
	}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		f.statuses = nil
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !runStatusKnown(s) {
				writeError(w, http.StatusBadRequest, "unknown status: "+s)
				return f, false
			}
			f.statuses = append(f.statuses, s)
		}
	}
	for _, spec := range []struct {
		key string
		dst *pgtype.UUID
	}{{"agent_id", &f.agentID}, {"issue_id", &f.issueID}, {"runtime_id", &f.runtimeID}} {
		if raw := strings.TrimSpace(q.Get(spec.key)); raw != "" {
			u, ok := parseUUIDOrBadRequest(w, raw, spec.key)
			if !ok {
				return f, false
			}
			*spec.dst = u
		}
	}
	if raw := strings.TrimSpace(q.Get("since")); raw != "" {
		ts, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since must be RFC 3339")
			return f, false
		}
		f.since = pgtype.Timestamptz{Time: ts, Valid: true}
	}
	if raw := strings.TrimSpace(q.Get("cursor")); raw != "" {
		at, id, ok := decodeRunsCursor(raw)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return f, false
		}
		f.cursorAt, f.cursorID = at, id
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return f, false
		}
		if n > runsMaxLimit {
			n = runsMaxLimit
		}
		f.limit = int32(n)
	}
	return f, true
}

func runStatusKnown(s string) bool {
	for _, k := range runActiveStatuses {
		if k == s {
			return true
		}
	}
	for _, k := range runTerminalStatuses {
		if k == s {
			return true
		}
	}
	return false
}

// runsScope resolves the workspace, the member and the agents they may see.
func (h *Handler) runsScope(w http.ResponseWriter, r *http.Request, roles ...string) (pgtype.UUID, db.Member, []pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return pgtype.UUID{}, db.Member{}, nil, false
	}
	if len(roles) > 0 && !roleAllowed(member.Role, roles...) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return pgtype.UUID{}, db.Member{}, nil, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, db.Member{}, nil, false
	}
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, "member", requestUserID(r), member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return pgtype.UUID{}, db.Member{}, nil, false
	}
	ids := make([]pgtype.UUID, 0, len(allowed))
	for id := range allowed {
		if u, ok := decisionUUID(id); ok {
			ids = append(ids, u)
		}
	}
	return wsUUID, member, ids, true
}

// ListRuns: GET /api/runs?state=&status=&agent_id=&issue_id=&runtime_id=&since=&cursor=&limit=
func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, agentIDs, ok := h.runsScope(w, r)
	if !ok {
		return
	}
	f, ok := parseRunsFilter(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if f.agentID.Valid {
		keep := false
		for _, id := range agentIDs {
			if id == f.agentID {
				keep = true
			}
		}
		if !keep {
			agentIDs = nil
		} else {
			agentIDs = []pgtype.UUID{f.agentID}
		}
	}
	var statuses []string
	if len(f.statuses) > 0 {
		statuses = f.statuses
	}
	rows, err := h.Queries.ListWorkspaceRuns(ctx, db.ListWorkspaceRunsParams{
		WorkspaceID: wsUUID, AgentIds: agentIDs, Statuses: statuses, IssueID: f.issueID, RuntimeID: f.runtimeID, Since: f.since,
		CursorAt: f.cursorAt, CursorID: f.cursorID, Lim: f.limit + 1,
	})
	if err != nil {
		slog.Warn("runs: list failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}
	next := ""
	if len(rows) > int(f.limit) {
		rows = rows[:f.limit]
		next = encodeRunsCursor(rows[len(rows)-1])
	}
	runs := h.buildRunRows(ctx, wsUUID, rows)
	summary := h.runsSummary(ctx, wsUUID, agentIDs)
	writeJSON(w, http.StatusOK, RunsResponse{Runs: runs, NextCursor: next, Summary: summary})
}

// buildRunRows turns task rows into fleet rows: names, issue refs, usage,
// blockers — each resolved once for the whole page, never per row.
func (h *Handler) buildRunRows(ctx context.Context, wsUUID pgtype.UUID, rows []db.AgentTaskQueue) []RunResponse {
	wsID := uuidToString(wsUUID)
	prefix := h.getIssuePrefix(ctx, wsUUID)
	agentNames := map[string]string{}
	if agents, err := h.Queries.ListAllAgentsAnyKind(ctx, wsUUID); err == nil {
		for _, a := range agents {
			agentNames[uuidToString(a.ID)] = a.Name
		}
	}
	taskIDs := make([]pgtype.UUID, 0, len(rows))
	issueIDs := make([]pgtype.UUID, 0, len(rows))
	seenIssue := map[string]bool{}
	for _, t := range rows {
		taskIDs = append(taskIDs, t.ID)
		if t.IssueID.Valid && !seenIssue[uuidToString(t.IssueID)] {
			seenIssue[uuidToString(t.IssueID)] = true
			issueIDs = append(issueIDs, t.IssueID)
		}
	}
	issues := map[string]RunIssueRef{}
	for _, id := range issueIDs {
		if issue, err := h.Queries.GetIssue(ctx, id); err == nil && issue.WorkspaceID == wsUUID {
			issues[uuidToString(id)] = RunIssueRef{ID: uuidToString(id), Identifier: prefix + "-" + strconv.Itoa(int(issue.Number)), Title: issue.Title, Status: issue.Status}
		}
	}
	usage := map[string][]TaskUsageData{}
	if len(taskIDs) > 0 {
		if urows, err := h.Queries.ListTaskUsageForTasks(ctx, taskIDs); err == nil {
			for _, u := range urows {
				appendTaskUsage(usage, u.TaskID, u.Provider, u.Model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens, u.CostUsdTicks)
			}
		}
	}
	blockers := h.runBlockers(ctx, wsUUID, rows, issueIDs)
	now := time.Now()
	out := make([]RunResponse, 0, len(rows))
	for _, t := range rows {
		resp := taskToResponse(t, wsID)
		row := RunResponse{AgentTaskResponse: resp, AgentName: agentNames[resp.AgentID]}
		if ref, ok := issues[resp.IssueID]; ok {
			ref := ref
			row.Issue = &ref
			row.IssueIdentifier = ref.Identifier
		}
		if u, ok := usage[resp.ID]; ok {
			row.Usage = u
			for _, part := range u {
				if part.CostUsdTicks != nil {
					row.CostUsdTicks += *part.CostUsdTicks
				}
			}
		}
		if t.StartedAt.Valid {
			end := now
			if t.CompletedAt.Valid {
				end = t.CompletedAt.Time
			}
			row.DurationMs = end.Sub(t.StartedAt.Time).Milliseconds()
		}
		if t.Status == "running" && t.LastActivityAt.Valid {
			row.SilenceMs = now.Sub(t.LastActivityAt.Time).Milliseconds()
		}
		if b, ok := blockers[resp.ID]; ok {
			b := b
			row.BlockedOn = &b
		}
		out = append(out, row)
	}
	return out
}

// runBlockers answers "why is this run stuck" for a page of runs, from the
// six places that answer lives: a gate on the run, a card the run asked, a
// goal question or a held transition on its issue, a local-directory wait,
// a pause. The first found wins, in that order.
func (h *Handler) runBlockers(ctx context.Context, wsUUID pgtype.UUID, rows []db.AgentTaskQueue, issueIDs []pgtype.UUID) map[string]RunBlocker {
	out := map[string]RunBlocker{}
	active := map[string]db.AgentTaskQueue{}
	activeIDs := make([]pgtype.UUID, 0, len(rows))
	for _, t := range rows {
		if runStatusActive(t.Status) {
			active[uuidToString(t.ID)] = t
			activeIDs = append(activeIDs, t.ID)
		}
	}
	if len(activeIDs) == 0 {
		return out
	}
	set := func(taskID string, b RunBlocker) {
		if _, taken := out[taskID]; !taken {
			out[taskID] = b
		}
	}
	if gates, err := h.Queries.ListPendingApprovalGatesForTasks(ctx, activeIDs); err == nil {
		for _, g := range gates {
			set(uuidToString(g.TaskID), RunBlocker{Kind: RunBlockerGate, ID: uuidToString(g.ID), DecisionID: uuidToString(g.DecisionRequestID), Summary: g.GateType + " · " + g.Summary, Since: timestampToPtr(g.CreatedAt)})
		}
	}
	if cards, err := h.Queries.ListPendingIssueDecisionsForTasks(ctx, activeIDs); err == nil {
		for _, d := range cards {
			set(uuidToString(d.TaskID), RunBlocker{Kind: RunBlockerDecision, ID: uuidToString(d.ID), DecisionID: uuidToString(d.ID), Summary: d.Question, Since: timestampToPtr(d.CreatedAt)})
		}
	}
	if len(issueIDs) > 0 {
		byIssue := map[string][]string{}
		for id, t := range active {
			if t.IssueID.Valid {
				byIssue[uuidToString(t.IssueID)] = append(byIssue[uuidToString(t.IssueID)], id)
			}
		}
		if goals, err := h.Queries.ListWaitingIssueGoalsForIssues(ctx, db.ListWaitingIssueGoalsForIssuesParams{WorkspaceID: wsUUID, IssueIds: issueIDs}); err == nil {
			for _, g := range goals {
				q := service.GoalQuestionOf(g.Question)
				if q == nil || q.Answer != "" {
					continue
				}
				for _, taskID := range byIssue[uuidToString(g.IssueID)] {
					set(taskID, RunBlocker{Kind: RunBlockerGoalQuestion, ID: uuidToString(g.ID), Summary: q.Prompt, Since: optionalPtr(q.AskedAt)})
				}
			}
		}
		if reqs, err := h.Queries.ListPendingIssueTransitionRequestsForIssues(ctx, db.ListPendingIssueTransitionRequestsForIssuesParams{WorkspaceID: wsUUID, IssueIds: issueIDs}); err == nil {
			for _, req := range reqs {
				for _, taskID := range byIssue[uuidToString(req.IssueID)] {
					set(taskID, RunBlocker{Kind: RunBlockerTransition, ID: uuidToString(req.ID), Summary: req.FromStatus + " → " + req.ToStatus, Since: timestampToPtr(req.CreatedAt)})
				}
			}
		}
	}
	for id, t := range active {
		switch t.Status {
		case "waiting_local_directory":
			set(id, RunBlocker{Kind: RunBlockerLocalDirectory, Summary: t.WaitReason.String, Since: timestampToPtr(t.DispatchedAt)})
		case "paused":
			set(id, RunBlocker{Kind: RunBlockerPaused, Summary: "paused", Since: timestampToPtr(t.PauseRequestedAt)})
		case "deferred":
			// A follow-up carries its note; other deferrals keep the bare word.
			summary := "deferred"
			if t.TriggerEvidenceKind.String == "followup" && t.TriggerSummary.Valid {
				summary = t.TriggerSummary.String
			}
			set(id, RunBlocker{Kind: RunBlockerDeferred, Summary: summary, Since: timestampToPtr(t.CreatedAt)})
		}
	}
	return out
}

func optionalPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func runStatusActive(s string) bool {
	for _, k := range runActiveStatuses {
		if k == s {
			return true
		}
	}
	return false
}

// runsSummary counts the fleet since the start of the day (UTC) and reads
// the halt. Blocked = active runs with a blocker, found the same way rows are.
func (h *Handler) runsSummary(ctx context.Context, wsUUID pgtype.UUID, agentIDs []pgtype.UUID) RunsSummary {
	since := time.Now().UTC().Truncate(24 * time.Hour)
	s := RunsSummary{Since: since.Format(time.RFC3339)}
	if len(agentIDs) == 0 {
		agentIDs = []pgtype.UUID{}
	}
	if counts, err := h.Queries.CountWorkspaceRunsByStatus(ctx, db.CountWorkspaceRunsByStatusParams{WorkspaceID: wsUUID, AgentIds: agentIDs, Since: pgtype.Timestamptz{Time: since, Valid: true}}); err == nil {
		for _, c := range counts {
			switch c.Status {
			case "queued", "dispatched", "deferred":
				s.Queued += c.N
				s.Active += c.N
			case "running", "waiting_local_directory", "paused":
				s.Running += c.N
				s.Active += c.N
			case "completed":
				s.CompletedSince = c.N
			case "failed":
				s.FailedSince = c.N
			case "cancelled":
				s.CancelledSince = c.N
			}
		}
	}
	if cost, err := h.Queries.SumWorkspaceRunCostSince(ctx, db.SumWorkspaceRunCostSinceParams{WorkspaceID: wsUUID, AgentIds: agentIDs, Since: pgtype.Timestamptz{Time: since, Valid: true}}); err == nil {
		s.CostSinceUsdTicks = cost
	}
	if activeRows, err := h.Queries.ListWorkspaceRuns(ctx, db.ListWorkspaceRunsParams{WorkspaceID: wsUUID, AgentIds: agentIDs, Statuses: runActiveStatuses, Lim: runsMaxLimit}); err == nil {
		issueIDs := make([]pgtype.UUID, 0, len(activeRows))
		seen := map[string]bool{}
		for _, t := range activeRows {
			if t.IssueID.Valid && !seen[uuidToString(t.IssueID)] {
				seen[uuidToString(t.IssueID)] = true
				issueIDs = append(issueIDs, t.IssueID)
			}
		}
		s.Blocked = int64(len(h.runBlockers(ctx, wsUUID, activeRows, issueIDs)))
	}
	if ws, err := h.Queries.GetWorkspace(ctx, wsUUID); err == nil {
		s.RunHalt = service.RunHaltFromSettings(ws.Settings)
	}
	return s
}

// RunCancelOutcome is one row of a bulk cancel.
type RunCancelOutcome struct {
	TaskID  string `json:"task_id"`
	Outcome string `json:"outcome"` // cancelled | already_over | not_found | error
	Error   string `json:"error,omitempty"`
}

// CancelRuns: POST /api/runs/cancel {task_ids: [...]} — cancels each run the
// caller may see, one by one, and reports every outcome. Never stops at the
// first failure: a fleet cancel that gives up on the third run is worse
// than one that names the run it could not stop.
func (h *Handler) CancelRuns(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, agentIDs, ok := h.runsScope(w, r)
	if !ok {
		return
	}
	var req struct {
		TaskIDs []string `json:"task_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil || len(req.TaskIDs) == 0 {
		writeError(w, http.StatusBadRequest, "task_ids is required")
		return
	}
	if len(req.TaskIDs) > runsCancelMax {
		writeError(w, http.StatusBadRequest, "at most 500 runs per call")
		return
	}
	ids := make([]pgtype.UUID, 0, len(req.TaskIDs))
	for _, raw := range req.TaskIDs {
		u, ok := parseUUIDOrBadRequest(w, raw, "task_ids")
		if !ok {
			return
		}
		ids = append(ids, u)
	}
	results := h.cancelRuns(r.Context(), wsUUID, agentIDs, ids)
	cancelled := 0
	for _, res := range results {
		if res.Outcome == "cancelled" {
			cancelled++
		}
	}
	h.audit(r.Context(), wsUUID, "member", uuidToString(member.UserID), AuditRunsCancelled, "workspace", wsUUID, map[string]any{"requested": len(ids), "cancelled": cancelled}, nil)
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "cancelled": cancelled})
}

func (h *Handler) cancelRuns(ctx context.Context, wsUUID pgtype.UUID, agentIDs []pgtype.UUID, ids []pgtype.UUID) []RunCancelOutcome {
	allowed := map[string]bool{}
	for _, id := range agentIDs {
		allowed[uuidToString(id)] = true
	}
	results := make([]RunCancelOutcome, 0, len(ids))
	for _, id := range ids {
		res := RunCancelOutcome{TaskID: uuidToString(id)}
		task, err := h.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
		if err != nil || !allowed[uuidToString(task.AgentID)] {
			res.Outcome = "not_found"
			results = append(results, res)
			continue
		}
		if !runStatusActive(task.Status) {
			res.Outcome = "already_over"
			results = append(results, res)
			continue
		}
		if h.TaskService == nil {
			res.Outcome, res.Error = "error", "task service unavailable"
			results = append(results, res)
			continue
		}
		if _, err := h.TaskService.CancelTaskWithResult(ctx, id, service.CancelTaskOptions{}); err != nil {
			if errors.Is(err, service.ErrTaskNoLongerQueued) {
				res.Outcome = "already_over"
			} else {
				res.Outcome, res.Error = "error", err.Error()
			}
			results = append(results, res)
			continue
		}
		res.Outcome = "cancelled"
		results = append(results, res)
	}
	return results
}

// KillSwitch: POST /api/runs/kill-switch {reason} — owner or admin. Halts the
// fleet (no run is claimed, every gated action is refused) AND cancels every
// run that is not over, in that order, so nothing slips in between. Lifting
// the halt afterwards is the ordinary PUT /api/run-halt.
func (h *Handler) KillSwitch(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, agentIDs, ok := h.runsScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(req.Reason) > service.RunHaltMaxReasonRunes {
		writeError(w, http.StatusBadRequest, "reason is too long")
		return
	}
	userID := uuidToString(member.UserID)
	halt, err := h.writeRunHalt(r.Context(), wsUUID, true, req.Reason, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to halt the fleet")
		return
	}
	ids, err := h.Queries.ListActiveWorkspaceTaskIDs(r.Context(), wsUUID)
	if err != nil {
		// The halt is in place, but nothing was cancelled: say so rather than
		// answering "cancelled: 0" as if the fleet had been idle.
		slog.Error("kill switch: list active runs failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "the fleet is halted but its active runs could not be listed; retry to cancel them")
		return
	}
	// An owner's kill switch stops every run, private agents included: the
	// scope check keeps the list honest for members, an owner sees it all.
	_ = agentIDs
	all := make([]pgtype.UUID, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if !seen[uuidToString(id)] {
			seen[uuidToString(id)] = true
			all = append(all, id)
		}
	}
	results := h.cancelRunsUnscoped(r.Context(), wsUUID, all)
	cancelled := 0
	for _, res := range results {
		if res.Outcome == "cancelled" {
			cancelled++
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Outcome < results[j].Outcome })
	h.audit(r.Context(), wsUUID, "member", userID, AuditKillSwitch, "workspace", wsUUID, map[string]any{"reason": req.Reason, "requested": len(all), "cancelled": cancelled}, nil)
	h.publish(protocol.EventRunHaltChanged, uuidToString(wsUUID), "member", userID, map[string]any{"run_halt": halt, "cancelled": cancelled, "frozen": halt.FrozenCount, "resumed": halt.ResumedCount})
	writeJSON(w, http.StatusOK, map[string]any{"run_halt": halt, "cancelled": cancelled, "results": results})
}

func (h *Handler) cancelRunsUnscoped(ctx context.Context, wsUUID pgtype.UUID, ids []pgtype.UUID) []RunCancelOutcome {
	agents, err := h.Queries.ListAllAgentsAnyKind(ctx, wsUUID)
	if err != nil {
		return nil
	}
	all := make([]pgtype.UUID, 0, len(agents))
	for _, a := range agents {
		all = append(all, a.ID)
	}
	return h.cancelRuns(ctx, wsUUID, all, ids)
}
