package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Code health autopilot (K22).
//
// On a cron, a maintenance agent runs READ-ONLY over the project's repository
// and reports maintenance opportunities in one fenced block. The server keeps
// the findings it trusts, opens one well-formed issue per finding assigned to
// that same agent, and from there it is ordinary agent work: the enqueue goes
// through the usual budget admission (K03), the run through the agent's
// permission profile (K06) and blast radius (K07).
//
// Guardrails, in code:
//   - one scan at a time per workspace;
//   - findings below the configured confidence are dropped, not queued;
//   - at most max_issues_per_scan issues per scan, whatever the agent reports;
//   - a title this autopilot already opened and nobody closed in the last 30
//     days is not opened again;
//   - a finding whose run is refused by budget admission stays on the record
//     as skipped rather than silently disappearing.
//
// See service/code_health.go for the settings shape.

const (
	InboxTypeCodeHealthReport = "code_health_report"

	AuditCodeHealthConfigured    = "code_health.configured"
	AuditCodeHealthScanStarted   = "code_health.scan_started"
	AuditCodeHealthScanCompleted = "code_health.scan_completed"

	// codeHealthLabelName is the workspace label every maintenance issue
	// carries, created on first use.
	codeHealthLabelName = "maintenance"
	// codeHealthHostIssueTitle is the single housekeeping issue every scan run
	// is attached to. It is never assigned, so it never runs by itself.
	codeHealthHostIssueTitle = "Code health scan"
	codeHealthIssuePrefix    = "[Maintenance] "

	// codeHealthDedupeWindow is how far back a title counts as already opened.
	codeHealthDedupeWindow = 30 * 24 * time.Hour

	codeHealthMaxTitle    = 200
	codeHealthMaxText     = 4000
	codeHealthMaxFindings = 50
)

var codeHealthFence = regexp.MustCompile("(?s)```code_health_findings\\s*(\\{.*?\\})\\s*```")

// codeHealthKinds is the vocabulary the brief asks for. An unknown kind is
// kept but normalised, so a newer model inventing a category does not lose the
// finding.
var codeHealthKinds = map[string]bool{"debt": true, "dependency": true, "tests": true, "security": true}

// CodeHealthFinding is one reported maintenance opportunity, plus what the
// server did with it.
type CodeHealthFinding struct {
	Kind       string   `json:"kind"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Paths      []string `json:"paths,omitempty"`
	Confidence int      `json:"confidence"`
	Effort     string   `json:"effort"`
	Evidence   string   `json:"evidence,omitempty"`

	// IssueID is the issue this finding opened, when it opened one.
	IssueID string `json:"issue_id,omitempty"`
	// Skipped says why no issue was opened: low_confidence, duplicate, cap,
	// budget or error. Empty when the finding produced an issue.
	Skipped string `json:"skipped,omitempty"`
}

// CodeHealthReport is the fenced block the scan run ends with.
type CodeHealthReport struct {
	Findings []CodeHealthFinding `json:"findings"`
	// NothingFound explains an empty findings list; purely informational.
	NothingFound string `json:"nothing_found"`
}

type CodeHealthScanResponse struct {
	ID            string              `json:"id"`
	WorkspaceID   string              `json:"workspace_id"`
	ProjectID     *string             `json:"project_id"`
	AgentID       string              `json:"agent_id"`
	TaskID        string              `json:"task_id"`
	Status        string              `json:"status"`
	Findings      []CodeHealthFinding `json:"findings"`
	IssuesCreated int32               `json:"issues_created"`
	Error         string              `json:"error"`
	CreatedAt     string              `json:"created_at"`
	CompletedAt   *string             `json:"completed_at"`
}

func codeHealthScanToResponse(s db.CodeHealthScan) CodeHealthScanResponse {
	findings := []CodeHealthFinding{}
	if err := json.Unmarshal(s.Findings, &findings); err != nil || findings == nil {
		findings = []CodeHealthFinding{}
	}
	return CodeHealthScanResponse{
		ID: uuidToString(s.ID), WorkspaceID: uuidToString(s.WorkspaceID), ProjectID: uuidToPtr(s.ProjectID),
		AgentID: uuidToString(s.AgentID), TaskID: uuidToString(s.TaskID), Status: s.Status, Findings: findings,
		IssuesCreated: s.IssuesCreated, Error: s.Error, CreatedAt: timestampToString(s.CreatedAt),
		CompletedAt: nullableTimestamp(s.CompletedAt),
	}
}

func nullableTimestamp(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}
	out := timestampToString(ts)
	return &out
}

// --- configuration ---------------------------------------------------------

// codeHealthSettingsResponse is the setting plus the bounds a client may
// submit, so the form does not carry its own copy of them.
type codeHealthSettingsResponse struct {
	service.CodeHealthSettings
	MinIssuesAllowed int `json:"min_issues_allowed"`
	MaxIssuesAllowed int `json:"max_issues_allowed"`
}

func codeHealthSettingsBody(cfg service.CodeHealthSettings) codeHealthSettingsResponse {
	return codeHealthSettingsResponse{
		CodeHealthSettings: cfg,
		MinIssuesAllowed:   service.CodeHealthMinMaxIssues,
		MaxIssuesAllowed:   service.CodeHealthMaxMaxIssues,
	}
}

// GetCodeHealthSettings: GET /api/code-health/settings.
func (h *Handler) GetCodeHealthSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, codeHealthSettingsBody(service.CodeHealthFromSettings(ws.Settings)))
}

// PutCodeHealthSettings: PUT /api/code-health/settings. Admin only.
func (h *Handler) PutCodeHealthSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.CodeHealthSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Fill the blanks with the defaults before validating, so a form that only
	// flips `enabled` is a valid submission.
	current := service.CodeHealthDefaults()
	if req.Cron == "" {
		req.Cron = current.Cron
	}
	if req.Timezone == "" {
		req.Timezone = current.Timezone
	}
	if req.MaxIssuesPerScan == 0 {
		req.MaxIssuesPerScan = current.MaxIssuesPerScan
	}
	if err := service.ValidateCodeHealthSettings(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if req.AgentID != "" {
		agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
		if !ok {
			return
		}
		agent, err := h.Queries.GetAgent(r.Context(), agentID)
		if err != nil || agent.WorkspaceID != wsUUID || agent.ArchivedAt.Valid {
			writeError(w, http.StatusBadRequest, "agent_id must be an active agent of this workspace")
			return
		}
	}
	if req.ProjectID != "" {
		projectID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusBadRequest, "project_id must be a project of this workspace")
			return
		}
	}
	// The cron anchor is server-owned: a client cannot backdate it to make the
	// autopilot fire immediately. It is stamped when the autopilot goes from
	// off to on and preserved otherwise.
	previous := service.CodeHealthFromSettings(ws.Settings)
	req.EnabledAt = previous.EnabledAt
	if req.Enabled && (!previous.Enabled || req.EnabledAt.IsZero()) {
		req.EnabledAt = time.Now().UTC()
	}

	settings := map[string]any{}
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &settings)
	}
	settings["code_health"] = req
	raw, err := json.Marshal(settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the code health settings")
		return
	}
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the code health settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditCodeHealthConfigured, "workspace", wsUUID,
		map[string]any{"enabled": req.Enabled, "cron": req.Cron, "timezone": req.Timezone, "agent_id": req.AgentID,
			"project_id": req.ProjectID, "max_issues_per_scan": req.MaxIssuesPerScan, "min_confidence": req.MinConfidence}, nil)
	writeJSON(w, http.StatusOK, codeHealthSettingsBody(req))
}

// ListCodeHealthScans: GET /api/code-health/scans — the last 50 scans with
// their findings and the issues those findings opened.
func (h *Handler) ListCodeHealthScans(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListCodeHealthScans(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list the code health scans")
		return
	}
	out := make([]CodeHealthScanResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, codeHealthScanToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"scans": out})
}

// TriggerCodeHealthScan: POST /api/code-health/scans/trigger — an admin asks
// for a scan now. Allowed even when the schedule is off (that is the point of
// a manual trigger), refused while one is already running.
func (h *Handler) TriggerCodeHealthScan(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg := service.CodeHealthFromSettings(ws.Settings)
	if cfg.AgentID == "" {
		writeError(w, http.StatusBadRequest, "configure a maintenance agent before scanning")
		return
	}
	scan, reason, err := h.startCodeHealthScan(r.Context(), wsUUID, cfg, requestUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}
	if reason != "" {
		writeError(w, http.StatusConflict, "not scanned: "+reason)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"scan": codeHealthScanToResponse(scan)})
}

// --- scan ------------------------------------------------------------------

// ScanCodeHealth is the scheduler entry point: every workspace whose autopilot
// is enabled and whose cron came due since the last scan gets one scan.
func (h *Handler) ScanCodeHealth(ctx context.Context, now time.Time) (int, error) {
	workspaces, err := h.Queries.ListWorkspacesForBriefing(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	started := 0
	for _, ws := range workspaces {
		cfg := service.CodeHealthFromSettings(ws.Settings)
		if !cfg.Enabled || cfg.AgentID == "" {
			continue
		}
		anchor := cfg.EnabledAt
		if last, err := h.Queries.GetLastCodeHealthScanAt(ctx, ws.ID); err == nil && last.Valid {
			anchor = last.Time
		}
		if !service.CodeHealthDue(cfg, anchor, now) {
			continue
		}
		if _, reason, err := h.startCodeHealthScan(ctx, ws.ID, cfg, ""); err != nil {
			slog.Warn("code health: scan failed", "workspace_id", uuidToString(ws.ID), "error", err)
		} else if reason == "" {
			started++
		}
	}
	return started, nil
}

// startCodeHealthScan enqueues one read-only scan run. A non-empty reason
// means nothing was started and the caller should say so rather than error.
func (h *Handler) startCodeHealthScan(ctx context.Context, wsID pgtype.UUID, cfg service.CodeHealthSettings, actorUserID string) (db.CodeHealthScan, string, error) {
	if running, err := h.Queries.GetRunningCodeHealthScan(ctx, wsID); err == nil {
		// A running row whose run already reached a terminal state is an
		// orphan (the completion hook never landed). Settle it instead of
		// wedging the workspace forever.
		if prev, err := h.Queries.GetAgentTask(ctx, running.TaskID); err == nil && taskIsActive(prev.Status) {
			return db.CodeHealthScan{}, "a scan is already running", nil
		}
		h.finishCodeHealthScan(ctx, running, "failed", nil, 0, "the scan run ended without reporting findings")
	}
	agentID, err := util.ParseUUID(cfg.AgentID)
	if err != nil {
		return db.CodeHealthScan{}, "", fmt.Errorf("invalid agent_id in settings")
	}
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || agent.WorkspaceID != wsID || agent.ArchivedAt.Valid {
		return db.CodeHealthScan{}, "the maintenance agent is not an active agent of this workspace", nil
	}
	projectID := pgtype.UUID{}
	if cfg.ProjectID != "" {
		if projectID, err = util.ParseUUID(cfg.ProjectID); err != nil {
			projectID = pgtype.UUID{}
		}
	}
	host, ok := h.codeHealthHostIssue(ctx, wsID, projectID, agentID)
	if !ok {
		return db.CodeHealthScan{}, "", fmt.Errorf("could not create the code health housekeeping issue")
	}
	actor := parseUUIDOrZero(actorUserID)
	task, err := h.TaskService.EnqueueCrossReviewRun(ctx, host, agentID, codeHealthBrief(cfg), actor)
	if err != nil {
		return db.CodeHealthScan{}, "", fmt.Errorf("enqueue scan run: %w", err)
	}
	scan, err := h.Queries.CreateCodeHealthScan(ctx, db.CreateCodeHealthScanParams{
		ID: dbid.NewV7(), WorkspaceID: wsID, ProjectID: projectID, AgentID: agentID, TaskID: task.ID,
	})
	if err != nil {
		return db.CodeHealthScan{}, "", fmt.Errorf("record scan: %w", err)
	}
	actorType := "system"
	if actorUserID != "" {
		actorType = "member"
	}
	h.audit(ctx, wsID, actorType, actorUserID, AuditCodeHealthScanStarted, "workspace", wsID,
		map[string]any{"scan_id": uuidToString(scan.ID), "task_id": uuidToString(task.ID), "agent_id": cfg.AgentID}, nil)
	h.publish("code_health:scan_started", uuidToString(wsID), actorType, actorUserID,
		map[string]any{"scan": codeHealthScanToResponse(scan)})
	return scan, "", nil
}

// codeHealthHostIssue returns the workspace's single housekeeping issue,
// creating it on first use. It is deliberately unassigned: it exists to carry
// the scan runs, not to be worked on.
func (h *Handler) codeHealthHostIssue(ctx context.Context, wsID, projectID, agentID pgtype.UUID) (db.Issue, bool) {
	existing, err := h.Queries.GetCodeHealthHostIssue(ctx, wsID)
	if err == nil {
		return existing, true
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("code health: load host issue failed", "workspace_id", uuidToString(wsID), "error", err)
		return db.Issue{}, false
	}
	status, ok := h.resolveEvalIssueStatus(ctx, wsID)
	if !ok {
		return db.Issue{}, false
	}
	res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
		WorkspaceID: wsID,
		Title:       codeHealthHostIssueTitle,
		Description: strToText("Housekeeping issue for the code health autopilot (K22). Every scheduled scan run is attached here; the maintenance work it finds is opened as separate issues."),
		Status:      status,
		Priority:    "none",
		// Created by the autopilot itself, whether the scan was scheduled or
		// triggered by an admin: the scheduled path has no member actor at all.
		CreatorType:    "agent",
		CreatorID:      agentID,
		ProjectID:      projectID,
		OriginType:     strToText("code_health"),
		AllowDuplicate: true,
	}, service.IssueCreateOpts{ActorID: uuidToString(agentID), AnalyticsAgentID: uuidToString(agentID)})
	if err != nil {
		slog.Warn("code health: create host issue failed", "workspace_id", uuidToString(wsID), "error", err)
		return db.Issue{}, false
	}
	return res.Issue, true
}

// codeHealthBrief is the single-agent spec: one-sentence job, allowlist,
// denylist, the output contract. Read-only by construction — the agent reports,
// the server decides what becomes an issue.
func codeHealthBrief(cfg service.CodeHealthSettings) string {
	var b strings.Builder
	b.WriteString("CODE HEALTH SCAN. Your job, in one sentence: read this project's repository and report the maintenance work nobody has scheduled — technical debt, outdated or vulnerable dependencies, and code that has no test.\n\n")
	b.WriteString("ALLOWED: reading the repository, its dependency manifests and lockfiles, its test layout, and read-only commands that inspect them (git log, dependency audit, test listing).\n")
	b.WriteString("FORBIDDEN: doing the work, editing any file, committing, opening a pull request, changing any issue, or calling an external system that writes. This run reports; it never fixes.\n")
	b.WriteString("WORK IN SMALL VERIFIABLE STEPS: inspect one area at a time and state what you actually observed. A finding you cannot point at a file or a manifest entry for does not belong in the report.\n")
	b.WriteString("EVIDENCE, NOT IMPRESSIONS: confidence is how sure you are that the finding is real and worth doing, not how confident you feel in general. Anything you did not verify belongs below 50.\n\n")
	if cfg.ProjectID != "" {
		b.WriteString("Scope: the repository of this project only.\n\n")
	}
	fmt.Fprintf(&b, "At most %d findings will be turned into issues, keeping the highest-confidence ones at or above %d. Reporting more is fine; padding the list is not.\n\n", cfg.MaxIssuesPerScan, cfg.MinConfidence)
	b.WriteString("OUTPUT CONTRACT. End your answer with exactly one fenced block:\n")
	b.WriteString("```code_health_findings\n")
	b.WriteString(`{"findings":[{"kind":"debt|dependency|tests|security","title":"short imperative title","summary":"what and why, one paragraph","paths":["path/to/file"],"confidence":0-100,"effort":"S|M|L","evidence":"what you observed that proves it"}],"nothing_found":"reason when the list is empty"}`)
	b.WriteString("\n```\n")
	b.WriteString("Report an empty findings list with a reason rather than inventing work: a clean scan is a valid result.\n")
	return b.String()
}

// --- completion ------------------------------------------------------------

// storeCodeHealthFindings runs at the scan run's completion: parse the block,
// filter the findings, open one issue per kept finding, close the scan.
func (h *Handler) storeCodeHealthFindings(ctx context.Context, task db.AgentTaskQueue, output string) {
	scan, err := h.Queries.GetCodeHealthScanByTask(ctx, task.ID)
	if err != nil || scan.Status != "running" {
		return // not a scan run, or already settled: idempotent per scan
	}
	report, ok := h.parseCodeHealthReport(ctx, task, output)
	if !ok {
		h.finishCodeHealthScan(ctx, scan, "failed", nil, 0, "the scan run ended without a readable code_health_findings block")
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, scan.WorkspaceID)
	if err != nil {
		h.finishCodeHealthScan(ctx, scan, "failed", nil, 0, "the workspace could not be read")
		return
	}
	cfg := service.CodeHealthFromSettings(ws.Settings)
	kept := h.selectCodeHealthFindings(ctx, scan, cfg, report.Findings)

	labelIDs := []pgtype.UUID{}
	if labelID, ok := h.ensureCodeHealthLabel(ctx, scan.WorkspaceID); ok {
		labelIDs = append(labelIDs, labelID)
	}
	created := 0
	for i := range kept {
		if kept[i].Skipped != "" {
			continue
		}
		issueID, taskQueued := h.openCodeHealthIssue(ctx, scan, kept[i], labelIDs)
		if issueID == "" {
			kept[i].Skipped = "error"
			continue
		}
		kept[i].IssueID = issueID
		created++
		if !taskQueued {
			// The issue exists and is assigned; what did not happen is the run.
			// Enqueue admission is where the maintenance budget and the run
			// limits (K03) refuse work, so that is what this records.
			kept[i].Skipped = "budget"
		}
	}
	status := "completed"
	if created == 0 {
		status = "empty"
	}
	h.finishCodeHealthScan(ctx, scan, status, kept, int32(created), "")
	h.audit(ctx, scan.WorkspaceID, "agent", uuidToString(scan.AgentID), AuditCodeHealthScanCompleted, "workspace", scan.WorkspaceID,
		map[string]any{"scan_id": uuidToString(scan.ID), "status": status, "findings": len(kept), "issues_created": created}, nil)
	if created > 0 {
		h.notifyCodeHealthAdmins(ctx, scan, fmt.Sprintf("The code health scan opened %d maintenance issue(s).", created))
	}
}

// failCodeHealthScan settles the scan of a run that failed. Called from the
// terminal failure path, so a crashed scan does not block the next one.
func (h *Handler) failCodeHealthScan(ctx context.Context, task db.AgentTaskQueue, reason string) {
	scan, err := h.Queries.GetCodeHealthScanByTask(ctx, task.ID)
	if err != nil || scan.Status != "running" {
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "the scan run failed"
	}
	h.finishCodeHealthScan(ctx, scan, "failed", nil, 0, truncate(reason, 1000))
	h.notifyCodeHealthAdmins(ctx, scan, "The code health scan failed: "+truncate(reason, 500))
}

func (h *Handler) finishCodeHealthScan(ctx context.Context, scan db.CodeHealthScan, status string, findings []CodeHealthFinding, created int32, errText string) {
	raw, err := json.Marshal(findings)
	if err != nil || findings == nil {
		raw = []byte("[]")
	}
	updated, err := h.Queries.FinishCodeHealthScan(ctx, db.FinishCodeHealthScanParams{
		ID: scan.ID, Status: status, Findings: raw, IssuesCreated: created, Error: errText,
	})
	if err != nil {
		slog.Warn("code health: finish scan failed", "scan_id", uuidToString(scan.ID), "error", err)
		return
	}
	h.publish("code_health:scan_finished", uuidToString(scan.WorkspaceID), "agent", uuidToString(scan.AgentID),
		map[string]any{"scan": codeHealthScanToResponse(updated)})
}

// parseCodeHealthReport reads the fenced block off the run's answer, falling
// back to its streamed text messages when the final answer does not carry it.
func (h *Handler) parseCodeHealthReport(ctx context.Context, task db.AgentTaskQueue, output string) (CodeHealthReport, bool) {
	text := output
	if !codeHealthFence.MatchString(text) {
		if msgs, err := h.Queries.ListTaskMessages(ctx, task.ID); err == nil {
			var b strings.Builder
			for _, m := range msgs {
				if m.Type == "text" && m.Content.Valid {
					b.WriteString(m.Content.String + "\n\n")
				}
			}
			if codeHealthFence.MatchString(b.String()) {
				text = b.String()
			}
		}
	}
	m := codeHealthFence.FindStringSubmatch(text)
	if m == nil {
		return CodeHealthReport{}, false
	}
	var report CodeHealthReport
	if err := json.Unmarshal([]byte(m[1]), &report); err != nil {
		return CodeHealthReport{}, false
	}
	return report, true
}

// selectCodeHealthFindings normalises the reported findings and marks the ones
// that will not become issues. Every reported finding stays on the record with
// the reason it was skipped; nothing is silently dropped.
func (h *Handler) selectCodeHealthFindings(ctx context.Context, scan db.CodeHealthScan, cfg service.CodeHealthSettings, reported []CodeHealthFinding) []CodeHealthFinding {
	seen := map[string]bool{}
	since := pgtype.Timestamptz{Time: time.Now().Add(-codeHealthDedupeWindow), Valid: true}
	if titles, err := h.Queries.ListRecentCodeHealthIssueTitles(ctx, db.ListRecentCodeHealthIssueTitlesParams{
		WorkspaceID: scan.WorkspaceID, Since: since,
	}); err == nil {
		for _, title := range titles {
			seen[codeHealthTitleKey(strings.TrimPrefix(title, codeHealthIssuePrefix))] = true
		}
	} else {
		slog.Warn("code health: dedupe lookup failed", "scan_id", uuidToString(scan.ID), "error", err)
	}
	if len(reported) > codeHealthMaxFindings {
		reported = reported[:codeHealthMaxFindings]
	}
	out := make([]CodeHealthFinding, 0, len(reported))
	room := cfg.MaxIssuesPerScan
	for _, f := range reported {
		f.Title = truncate(strings.TrimSpace(f.Title), codeHealthMaxTitle)
		f.Summary = truncate(strings.TrimSpace(f.Summary), codeHealthMaxText)
		f.Evidence = truncate(strings.TrimSpace(f.Evidence), codeHealthMaxText)
		if !codeHealthKinds[f.Kind] {
			f.Kind = "debt"
		}
		if f.Effort != "S" && f.Effort != "M" && f.Effort != "L" {
			f.Effort = "M"
		}
		if f.Title == "" {
			continue // a finding with no title is not a finding
		}
		key := codeHealthTitleKey(f.Title)
		switch {
		case f.Confidence < cfg.MinConfidence:
			f.Skipped = "low_confidence"
		case seen[key]:
			f.Skipped = "duplicate"
		case room <= 0:
			f.Skipped = "cap"
		default:
			seen[key] = true
			room--
		}
		out = append(out, f)
	}
	return out
}

func codeHealthTitleKey(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// openCodeHealthIssue creates one maintenance issue assigned to the
// maintenance agent. The second return value says whether the assignment
// actually queued a run.
func (h *Handler) openCodeHealthIssue(ctx context.Context, scan db.CodeHealthScan, f CodeHealthFinding, labelIDs []pgtype.UUID) (string, bool) {
	status, ok := h.resolveEvalIssueStatus(ctx, scan.WorkspaceID)
	if !ok {
		slog.Warn("code health: no usable issue status", "workspace_id", uuidToString(scan.WorkspaceID))
		return "", false
	}
	res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
		WorkspaceID:    scan.WorkspaceID,
		Title:          truncate(codeHealthIssuePrefix+f.Title, codeHealthMaxTitle),
		Description:    strToText(codeHealthIssueBody(f)),
		Status:         status,
		Priority:       "none",
		AssigneeType:   strToText("agent"),
		AssigneeID:     scan.AgentID,
		CreatorType:    "agent",
		CreatorID:      scan.AgentID,
		ProjectID:      scan.ProjectID,
		OriginType:     strToText("code_health"),
		OriginID:       scan.ID,
		LabelIDs:       labelIDs,
		AllowDuplicate: true,
	}, service.IssueCreateOpts{ActorID: uuidToString(scan.AgentID), AnalyticsAgentID: uuidToString(scan.AgentID)})
	if err != nil {
		slog.Warn("code health: create maintenance issue failed", "scan_id", uuidToString(scan.ID), "error", err)
		return "", false
	}
	return uuidToString(res.Issue.ID), res.AssignedTaskID.Valid
}

// codeHealthIssueBody is the issue description: what, where, how sure, how big.
func codeHealthIssueBody(f CodeHealthFinding) string {
	var b strings.Builder
	b.WriteString(f.Summary)
	b.WriteString("\n\n---\n\n")
	fmt.Fprintf(&b, "- Kind: %s\n- Effort: %s\n- Confidence: %d%%\n", f.Kind, f.Effort, f.Confidence)
	if len(f.Paths) > 0 {
		paths := f.Paths
		if len(paths) > 20 {
			paths = paths[:20]
		}
		b.WriteString("- Paths: ")
		for i, p := range paths {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("`" + truncate(p, 200) + "`")
		}
		b.WriteString("\n")
	}
	if f.Evidence != "" {
		b.WriteString("\n**Evidence**\n\n" + f.Evidence + "\n")
	}
	b.WriteString("\n_Opened by the code health autopilot. Work in small verifiable steps and prove the change with a test or a run._\n")
	return b.String()
}

// ensureCodeHealthLabel returns the workspace's "maintenance" issue label,
// creating it on first use.
func (h *Handler) ensureCodeHealthLabel(ctx context.Context, wsID pgtype.UUID) (pgtype.UUID, bool) {
	labels, err := h.Queries.ListLabels(ctx, db.ListLabelsParams{WorkspaceID: wsID, ResourceType: "issue"})
	if err != nil {
		slog.Warn("code health: list labels failed", "workspace_id", uuidToString(wsID), "error", err)
		return pgtype.UUID{}, false
	}
	for _, l := range labels {
		if strings.EqualFold(l.Name, codeHealthLabelName) {
			return l.ID, true
		}
	}
	created, err := h.Queries.CreateLabel(ctx, db.CreateLabelParams{
		WorkspaceID: wsID, ResourceType: "issue", Name: codeHealthLabelName,
		Description: "Maintenance work found by the code health autopilot.", Color: "#8b8b8b",
	})
	if err != nil {
		slog.Warn("code health: create label failed", "workspace_id", uuidToString(wsID), "error", err)
		return pgtype.UUID{}, false
	}
	return created.ID, true
}

// notifyCodeHealthAdmins puts one inbox item in front of every workspace
// admin. Informational: the issues are already on the board.
func (h *Handler) notifyCodeHealthAdmins(ctx context.Context, scan db.CodeHealthScan, body string) {
	userIDs, err := h.Queries.ListWorkspaceManagerUserIDs(ctx, scan.WorkspaceID)
	if err != nil {
		slog.Warn("code health: list admins failed", "workspace_id", uuidToString(scan.WorkspaceID), "error", err)
		return
	}
	details, _ := json.Marshal(map[string]any{"scan_id": uuidToString(scan.ID), "status": scan.Status})
	for _, userID := range userIDs {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: scan.WorkspaceID, RecipientType: "member", RecipientID: userID,
			Type: InboxTypeCodeHealthReport, Severity: "info", Title: "Code health scan",
			Body:      pgtype.Text{String: truncate(body, 1000), Valid: true},
			ActorType: pgtype.Text{String: "agent", Valid: true}, ActorID: scan.AgentID, Details: details,
		})
		if err != nil {
			slog.Warn("code health: inbox failed", "workspace_id", uuidToString(scan.WorkspaceID), "error", err)
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(scan.WorkspaceID), "agent", uuidToString(scan.AgentID),
			map[string]any{"item": inboxToResponse(item)})
	}
}
