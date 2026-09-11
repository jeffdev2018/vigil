package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Agent context document drift detection (K56).
//
// Two runs, in order, both ordinary agent work:
//
//  1. the SCAN is read-only. It reads the agent context document and the
//     repository's own declarations — Makefile targets, package.json scripts,
//     the top-level tree, the named conventions — and reports the sections
//     that no longer describe the repository, in one fenced block. It changes
//     nothing.
//  2. the PULL REQUEST run applies exactly the reported patch to exactly that
//     one document on its own branch and opens a DRAFT pull request. It is the
//     agent's own VCS tooling that pushes and opens it (same shape as CI
//     auto-fix, K49), and the run reports the URL back at completion.
//
// The line neither run crosses is committing to the default branch. The agent
// context document is the file every other run reads before it acts; a system
// that could edit it unattended could redirect every agent in the workspace
// without anyone reading a diff. So the output is a draft under human review,
// always.
//
// The trigger is the repo index's newest indexed commit (K47) differing from
// the commit this repository was last checked at — "the default branch moved"
// without a webhook, a forge poll, or credentials of its own.
//
// See service/doc_drift.go for the settings shape.

const (
	InboxTypeDocDriftReport = "doc_drift_report"

	AuditDocDriftConfigured   = "doc_drift.configured"
	AuditDocDriftCheckStarted = "doc_drift.check_started"
	AuditDocDriftProposed     = "doc_drift.proposed"
	AuditDocDriftScanFailed   = "doc_drift.scan_failed"
	AuditDocDriftDismissed    = "doc_drift.dismissed"
	AuditDocDriftPROpened     = "doc_drift.pr_opened"

	// docDriftHostIssueTitle is the single housekeeping issue every drift run
	// is attached to. Never assigned, so it never runs by itself.
	docDriftHostIssueTitle = "Agent context drift check"

	// docDriftBranchPrefix is the branch the pull-request run is told to use.
	docDriftBranchPrefix = "multica/doc-drift-"
	docDriftPRTitle      = "docs: update agent context after drift"

	docDriftMaxSummary  = 2000
	docDriftMaxSections = 25
	docDriftMaxHeading  = 200
	docDriftMaxReason   = 2000
	docDriftMaxPatch    = 20000
	// docDriftMaxText bounds one stored column after merging. A proposal that
	// has accumulated more than this is already far past what a reviewer will
	// read in one sitting.
	docDriftMaxText = 60000
)

var docDriftFence = regexp.MustCompile("(?s)```doc_drift\\s*(\\{.*?\\})\\s*```")

// --- report contract -------------------------------------------------------

// DocDriftSection is one part of a document that stopped describing the repo.
type DocDriftSection struct {
	Heading string `json:"heading"`
	Reason  string `json:"reason"`
	Patch   string `json:"patch"`
}

// DocDriftProposalReport is the drift found in one document.
type DocDriftProposalReport struct {
	DocPath  string            `json:"doc_path"`
	Summary  string            `json:"summary"`
	Sections []DocDriftSection `json:"sections"`
}

// DocDriftReport is the fenced block the scan run ends with.
type DocDriftReport struct {
	Proposals []DocDriftProposalReport `json:"proposals"`
	// NoDrift explains an empty list; purely informational.
	NoDrift string `json:"no_drift"`
}

// --- responses -------------------------------------------------------------

type DocDriftProposalResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	RepoIdentifier   string  `json:"repo_identifier"`
	DocPath          string  `json:"doc_path"`
	DetectedDrift    string  `json:"detected_drift"`
	ProposedPatch    string  `json:"proposed_patch"`
	DetectedAtCommit string  `json:"detected_at_commit"`
	Status           string  `json:"status"`
	PullRequestURL   string  `json:"pull_request_url"`
	ScanTaskID       *string `json:"scan_task_id"`
	PRTaskID         *string `json:"pr_task_id"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

func docDriftToResponse(p db.DocDriftProposal) DocDriftProposalResponse {
	return DocDriftProposalResponse{
		ID: uuidToString(p.ID), WorkspaceID: uuidToString(p.WorkspaceID), RepoIdentifier: p.RepoIdentifier,
		DocPath: p.DocPath, DetectedDrift: p.DetectedDrift, ProposedPatch: p.ProposedPatch,
		DetectedAtCommit: p.DetectedAtCommit, Status: p.Status, PullRequestURL: p.PullRequestUrl,
		ScanTaskID: uuidToPtr(p.ScanTaskID), PRTaskID: uuidToPtr(p.PrTaskID),
		CreatedAt: timestampToString(p.CreatedAt), UpdatedAt: timestampToString(p.UpdatedAt),
	}
}

// docDriftRepoStatus is one indexed repository as the settings surface reports
// it: what the daemon last indexed, what this feature last checked, and
// therefore whether a check is owed. The UI needs no second call to decide
// whether "Check now" is worth pressing.
type docDriftRepoStatus struct {
	RepoIdentifier    string `json:"repo_identifier"`
	LastIndexedCommit string `json:"last_indexed_commit"`
	LastCheckedCommit string `json:"last_checked_commit"`
	Due               bool   `json:"due"`
	Scanning          bool   `json:"scanning"`
}

type docDriftSettingsResponse struct {
	service.DocDriftSettings
	Repos []docDriftRepoStatus `json:"repos"`
}

// --- configuration ---------------------------------------------------------

// GetDocDriftSettings: GET /api/doc-drift/settings.
func (h *Handler) GetDocDriftSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg := service.DocDriftFromSettings(ws.Settings)
	writeJSON(w, http.StatusOK, docDriftSettingsResponse{
		DocDriftSettings: cfg,
		Repos:            h.docDriftRepoStatuses(r.Context(), wsUUID, ws.Settings, cfg),
	})
}

// PutDocDriftSettings: PUT /api/doc-drift/settings. Admin only.
func (h *Handler) PutDocDriftSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.DocDriftSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A form that only flips `enabled` is a valid submission.
	req.Docs = service.NormalizeDocDriftDocs(req.Docs)
	if len(req.Docs) == 0 {
		req.Docs = service.DocDriftDefaults().Docs
	}
	if err := service.ValidateDocDriftSettings(req); err != nil {
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
	// LastChecked and ScanTasks are server-owned: a client that could rewrite
	// them could make every tick look like a moved branch, or clear the guard
	// that keeps one scan per repository in flight.
	previous := service.DocDriftFromSettings(ws.Settings)
	req.LastChecked = previous.LastChecked
	req.ScanTasks = previous.ScanTasks

	if err := h.saveDocDriftSettings(r.Context(), wsUUID, req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the agent context drift settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditDocDriftConfigured, "workspace", wsUUID,
		map[string]any{"enabled": req.Enabled, "agent_id": req.AgentID, "docs": req.Docs, "open_pr": req.OpenPR}, nil)
	writeJSON(w, http.StatusOK, docDriftSettingsResponse{
		DocDriftSettings: req,
		Repos:            h.docDriftRepoStatuses(r.Context(), wsUUID, ws.Settings, req),
	})
}

// saveDocDriftSettings writes the block back into the workspace settings blob.
// Merged server-side (MergeWorkspaceSettings): a read-modify-write of the
// whole settings blob lost the writes of any concurrent settings PUT on a
// different key (data_residency, drift, ...).
func (h *Handler) saveDocDriftSettings(ctx context.Context, wsID pgtype.UUID, cfg service.DocDriftSettings) error {
	raw, err := json.Marshal(map[string]any{"doc_drift": cfg})
	if err != nil {
		return err
	}
	_, err = h.Queries.MergeWorkspaceSettings(ctx, db.MergeWorkspaceSettingsParams{ID: wsID, Settings: raw})
	return err
}

// docDriftRepos lists the repositories this check considers: the ones the
// workspace opted into the repo index for, because the index's commit is the
// signal this feature runs on. A repository with no index has nothing to
// compare against and is not offered.
func docDriftRepos(settings []byte) []string {
	out := []string{}
	for repo, cfg := range service.RepoIndexSettingsFromSettings(settings) {
		if cfg.Enabled {
			out = append(out, repo)
		}
	}
	sort.Strings(out)
	return out
}

func (h *Handler) docDriftRepoStatuses(ctx context.Context, wsID pgtype.UUID, settings []byte, cfg service.DocDriftSettings) []docDriftRepoStatus {
	out := []docDriftRepoStatus{}
	indexer := h.repoIndexer()
	for _, repo := range docDriftRepos(settings) {
		status := docDriftRepoStatus{RepoIdentifier: repo, LastCheckedCommit: cfg.LastChecked[repo]}
		if stats, err := indexer.Stats(ctx, wsID, repo); err == nil {
			status.LastIndexedCommit = stats.LastIndexedCommit
		}
		status.Due = service.DocDriftDue(cfg, repo, status.LastIndexedCommit)
		status.Scanning = h.docDriftScanRunning(ctx, cfg, repo)
		out = append(out, status)
	}
	return out
}

// docDriftScanRunning answers "is a scan already in flight for this repo?".
// A recorded task that already reached a terminal state is not in flight — the
// completion hook clears the pointer, and this is the safety net for the case
// where it never landed at all.
func (h *Handler) docDriftScanRunning(ctx context.Context, cfg service.DocDriftSettings, repo string) bool {
	taskID, ok := cfg.ScanTasks[strings.TrimSpace(repo)]
	if !ok || taskID == "" {
		return false
	}
	id, err := util.ParseUUID(taskID)
	if err != nil {
		return false
	}
	task, err := h.Queries.GetAgentTask(ctx, id)
	return err == nil && taskIsActive(task.Status)
}

// --- check -----------------------------------------------------------------

type docDriftCheckRequest struct {
	RepoIdentifier string `json:"repo_identifier"`
}

// CheckDocDrift: POST /api/doc-drift/check {repo_identifier} — an admin forces
// a check for one repository now. Allowed with the schedule off (that is the
// point of a manual trigger), refused while a scan is running for that repo.
func (h *Handler) CheckDocDrift(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req docDriftCheckRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	repo := strings.TrimSpace(req.RepoIdentifier)
	if repo == "" {
		writeError(w, http.StatusBadRequest, "repo_identifier is required")
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg := service.DocDriftFromSettings(ws.Settings)
	if cfg.AgentID == "" {
		writeError(w, http.StatusBadRequest, "configure an agent before checking for drift")
		return
	}
	task, reason, err := h.startDocDriftScan(r.Context(), wsUUID, ws.Settings, cfg, repo, requestUserID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "check failed: "+err.Error())
		return
	}
	if reason != "" {
		writeError(w, http.StatusConflict, "not checked: "+reason)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"repo_identifier": repo, "task_id": uuidToString(task.ID)})
}

// ScanDocDrift is the scheduler entry point: one read-only scan per enabled
// workspace and per indexed repository whose default branch moved since the
// last check.
func (h *Handler) ScanDocDrift(ctx context.Context) (int, error) {
	workspaces, err := h.Queries.ListWorkspacesForBriefing(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	started := 0
	indexer := h.repoIndexer()
	for _, ws := range workspaces {
		cfg := service.DocDriftFromSettings(ws.Settings)
		if !cfg.Enabled || cfg.AgentID == "" {
			continue
		}
		// The settings blob is re-read per repository because starting a scan
		// writes to it; using the stale copy would lose the previous repo's
		// last_checked stamp.
		settings := ws.Settings
		for _, repo := range docDriftRepos(ws.Settings) {
			stats, err := indexer.Stats(ctx, ws.ID, repo)
			if err != nil {
				slog.Warn("doc drift: repo stats failed", "workspace_id", uuidToString(ws.ID), "repo", repo, "error", err)
				continue
			}
			if !service.DocDriftDue(cfg, repo, stats.LastIndexedCommit) {
				continue
			}
			if _, reason, err := h.startDocDriftScan(ctx, ws.ID, settings, cfg, repo, ""); err != nil {
				slog.Warn("doc drift: scan failed", "workspace_id", uuidToString(ws.ID), "repo", repo, "error", err)
			} else if reason == "" {
				started++
			}
			if fresh, err := h.Queries.GetWorkspace(ctx, ws.ID); err == nil {
				settings = fresh.Settings
				cfg = service.DocDriftFromSettings(fresh.Settings)
			}
		}
	}
	return started, nil
}

// startDocDriftScan enqueues one read-only scan run for one repository and
// stamps the commit it was started at. A non-empty reason means nothing was
// started and the caller should say so rather than error.
//
// The stamp lands at ENQUEUE, not at completion: a scan that crashes has still
// consumed its look at that commit, and re-running it on every tick until it
// succeeds would turn one broken repository into an unbounded run loop.
func (h *Handler) startDocDriftScan(ctx context.Context, wsID pgtype.UUID, settings []byte, cfg service.DocDriftSettings, repo, actorUserID string) (db.AgentTaskQueue, string, error) {
	if h.docDriftScanRunning(ctx, cfg, repo) {
		return db.AgentTaskQueue{}, "a drift check is already running for this repository", nil
	}
	agentID, err := util.ParseUUID(cfg.AgentID)
	if err != nil {
		return db.AgentTaskQueue{}, "", fmt.Errorf("invalid agent_id in settings")
	}
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || agent.WorkspaceID != wsID || agent.ArchivedAt.Valid {
		return db.AgentTaskQueue{}, "the configured agent is not an active agent of this workspace", nil
	}
	commit := ""
	if stats, err := h.repoIndexer().Stats(ctx, wsID, repo); err == nil {
		commit = stats.LastIndexedCommit
	}
	host, ok := h.docDriftHostIssue(ctx, wsID, agentID)
	if !ok {
		return db.AgentTaskQueue{}, "", fmt.Errorf("could not create the drift housekeeping issue")
	}
	task, err := h.TaskService.EnqueueCrossReviewRun(ctx, host, agentID, docDriftScanBrief(cfg, repo, commit), parseUUIDOrZero(actorUserID))
	if err != nil {
		return db.AgentTaskQueue{}, "", fmt.Errorf("enqueue scan run: %w", err)
	}
	cfg.LastChecked[repo] = commit
	cfg.ScanTasks[repo] = uuidToString(task.ID)
	if err := h.saveDocDriftSettings(ctx, wsID, cfg); err != nil {
		slog.Warn("doc drift: stamp last_checked failed", "workspace_id", uuidToString(wsID), "repo", repo, "error", err)
	}
	actorType := "system"
	if actorUserID != "" {
		actorType = "member"
	}
	h.audit(ctx, wsID, actorType, actorUserID, AuditDocDriftCheckStarted, "workspace", wsID,
		map[string]any{"repo_identifier": repo, "commit": commit, "task_id": uuidToString(task.ID), "agent_id": cfg.AgentID}, nil)
	h.publish("doc_drift:check_started", uuidToString(wsID), actorType, actorUserID,
		map[string]any{"repo_identifier": repo, "task_id": uuidToString(task.ID)})
	return task, "", nil
}

// docDriftHostIssue returns the workspace's single housekeeping issue,
// creating it on first use. Deliberately unassigned: it exists to carry the
// drift runs, not to be worked on.
func (h *Handler) docDriftHostIssue(ctx context.Context, wsID, agentID pgtype.UUID) (db.Issue, bool) {
	existing, err := h.Queries.GetDocDriftHostIssue(ctx, wsID)
	if err == nil {
		return existing, true
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("doc drift: load host issue failed", "workspace_id", uuidToString(wsID), "error", err)
		return db.Issue{}, false
	}
	status, ok := h.resolveEvalIssueStatus(ctx, wsID)
	if !ok {
		return db.Issue{}, false
	}
	res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
		WorkspaceID: wsID,
		Title:       docDriftHostIssueTitle,
		Description: strToText("Housekeeping issue for agent context drift detection (K56). Every drift scan and every draft pull request run is attached here; the proposals themselves live in Settings."),
		Status:      status,
		Priority:    "none",
		// Created by the check itself, whether scheduled or admin-triggered:
		// the scheduled path has no member actor at all.
		CreatorType:    "agent",
		CreatorID:      agentID,
		OriginType:     strToText("doc_drift"),
		AllowDuplicate: true,
	}, service.IssueCreateOpts{ActorID: uuidToString(agentID), AnalyticsAgentID: uuidToString(agentID)})
	if err != nil {
		slog.Warn("doc drift: create host issue failed", "workspace_id", uuidToString(wsID), "error", err)
		return db.Issue{}, false
	}
	return res.Issue, true
}

// docDriftScanBrief is the read-only spec: the documents, what to compare them
// against, and the output contract. The agent reports; the server decides what
// becomes a proposal.
func docDriftScanBrief(cfg service.DocDriftSettings, repo, commit string) string {
	var b strings.Builder
	b.WriteString("AGENT CONTEXT DRIFT CHECK. Your job, in one sentence: decide whether this repository's agent context document still describes this repository, and propose the smallest edit that makes it true again.\n\n")
	fmt.Fprintf(&b, "Repository: %s\n", repo)
	if commit != "" {
		fmt.Fprintf(&b, "Default branch commit: %s\n", commit)
	}
	b.WriteString("Documents to check, in order (skip the ones this repository does not have):\n")
	for _, doc := range cfg.Docs {
		b.WriteString("- " + doc + "\n")
	}
	b.WriteString("\nCOMPARE THE DOCUMENT AGAINST THE REPOSITORY, not against your expectations:\n")
	b.WriteString("- declared commands: every Makefile target and package.json script the document names must exist, and a command the document tells an agent to run must still be the one that works;\n")
	b.WriteString("- structure: the top-level directories and packages the document describes must match the tree;\n")
	b.WriteString("- named conventions: a rule, path, file or tool the document names by hand must still exist under that name.\n\n")
	b.WriteString("ALLOWED: reading the repository and read-only commands that inspect it (listing files, reading manifests, `make -n`, `git log`).\n")
	b.WriteString("FORBIDDEN: editing any file, committing, pushing, opening a pull request, or changing any issue. This run reports; a separate run proposes the change under human review.\n\n")
	b.WriteString("REPORT ONLY WHAT YOU VERIFIED. A section you did not check against the real repository is not drift. Style, wording and things you would have written differently are not drift either: report only statements that are now FALSE or that point at something which no longer exists.\n")
	b.WriteString("Keep each patch minimal and scoped to one section: a whole-document rewrite will be rejected by the reviewer, and rightly.\n\n")
	b.WriteString("OUTPUT CONTRACT. End your answer with exactly one fenced block:\n")
	b.WriteString("```doc_drift\n")
	b.WriteString(`{"proposals":[{"doc_path":"CLAUDE.md","summary":"one paragraph: what drifted","sections":[{"heading":"the document heading that drifted","reason":"what the repository actually does now, and how you checked","patch":"a unified diff, or the replacement text for that section"}]}],"no_drift":"reason when the list is empty"}`)
	b.WriteString("\n```\n")
	b.WriteString("An empty proposals list with a reason is a valid and expected result: a document that is still accurate must not be edited.\n")
	return b.String()
}

// --- completion: scan ------------------------------------------------------

// settleDocDriftRun is the single completion hook: one issue lookup decides
// whether this run belongs to the drift check at all, and only then does the
// scan or the pull-request settlement run. Every other completed run in the
// system pays exactly that one primary-key read.
func (h *Handler) settleDocDriftRun(ctx context.Context, task db.AgentTaskQueue, output, prURL string) {
	if !h.isDocDriftRun(ctx, task) {
		return
	}
	h.storeDocDriftProposals(ctx, task, output)
	h.storeDocDriftPR(ctx, task, prURL)
}

// isDocDriftRun answers whether a run hangs off the drift housekeeping issue.
func (h *Handler) isDocDriftRun(ctx context.Context, task db.AgentTaskQueue) bool {
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	return err == nil && issue.OriginType.String == "doc_drift"
}

// storeDocDriftProposals runs at the scan run's completion: parse the block and
// store one proposal per document that drifted, merging into the proposal
// already under review for that document when there is one.
func (h *Handler) storeDocDriftProposals(ctx context.Context, task db.AgentTaskQueue, output string) {
	wsID, repo, ok := h.docDriftScanContext(ctx, task)
	if !ok {
		return // not a drift scan run
	}
	// Clear the in-flight pointer first: whatever the report says, this scan is
	// over and the repository must not stay wedged behind it.
	h.clearDocDriftScanTask(ctx, wsID, repo)

	report, ok := h.parseDocDriftReport(ctx, task, output)
	if !ok {
		h.audit(ctx, wsID, "agent", uuidToString(task.AgentID), AuditDocDriftScanFailed, "workspace", wsID,
			map[string]any{"repo_identifier": repo, "task_id": uuidToString(task.ID),
				"reason": "the scan run ended without a readable doc_drift block"}, nil)
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return
	}
	cfg := service.DocDriftFromSettings(ws.Settings)
	stored := []db.DocDriftProposal{}
	for _, reported := range report.Proposals {
		proposal, ok := h.upsertDocDriftProposal(ctx, wsID, repo, task, reported)
		if !ok {
			continue
		}
		stored = append(stored, proposal)
	}
	if len(stored) == 0 {
		return
	}
	h.audit(ctx, wsID, "agent", uuidToString(task.AgentID), AuditDocDriftProposed, "workspace", wsID,
		map[string]any{"repo_identifier": repo, "task_id": uuidToString(task.ID), "proposals": len(stored)}, nil)
	for _, proposal := range stored {
		h.publish("doc_drift:proposed", uuidToString(wsID), "agent", uuidToString(task.AgentID),
			map[string]any{"proposal": docDriftToResponse(proposal)})
		if cfg.OpenPR && proposal.Status == "draft" && !proposal.PrTaskID.Valid {
			h.startDocDriftPRRun(ctx, ws, cfg, proposal, "")
		}
	}
}

// docDriftScanContext resolves the workspace and repository a scan run belongs
// to, from the in-flight pointer the enqueue recorded. A run nobody is waiting
// on returns false, which is what makes this hook a no-op for every other run.
func (h *Handler) docDriftScanContext(ctx context.Context, task db.AgentTaskQueue) (pgtype.UUID, string, bool) {
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil || issue.OriginType.String != "doc_drift" {
		return pgtype.UUID{}, "", false
	}
	ws, err := h.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return pgtype.UUID{}, "", false
	}
	cfg := service.DocDriftFromSettings(ws.Settings)
	want := uuidToString(task.ID)
	for repo, taskID := range cfg.ScanTasks {
		if taskID == want {
			return issue.WorkspaceID, repo, true
		}
	}
	return pgtype.UUID{}, "", false
}

func (h *Handler) clearDocDriftScanTask(ctx context.Context, wsID pgtype.UUID, repo string) {
	ws, err := h.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return
	}
	cfg := service.DocDriftFromSettings(ws.Settings)
	delete(cfg.ScanTasks, repo)
	if err := h.saveDocDriftSettings(ctx, wsID, cfg); err != nil {
		slog.Warn("doc drift: clear scan pointer failed", "workspace_id", uuidToString(wsID), "repo", repo, "error", err)
	}
}

// parseDocDriftReport reads the fenced block off the run's answer, falling back
// to its streamed text messages when the final answer does not carry it.
func (h *Handler) parseDocDriftReport(ctx context.Context, task db.AgentTaskQueue, output string) (DocDriftReport, bool) {
	text := output
	if !docDriftFence.MatchString(text) {
		if msgs, err := h.Queries.ListTaskMessages(ctx, task.ID); err == nil {
			var b strings.Builder
			for _, m := range msgs {
				if m.Type == "text" && m.Content.Valid {
					b.WriteString(m.Content.String + "\n\n")
				}
			}
			if docDriftFence.MatchString(b.String()) {
				text = b.String()
			}
		}
	}
	m := docDriftFence.FindStringSubmatch(text)
	if m == nil {
		return DocDriftReport{}, false
	}
	var report DocDriftReport
	if err := json.Unmarshal([]byte(m[1]), &report); err != nil {
		return DocDriftReport{}, false
	}
	return report, true
}

// upsertDocDriftProposal stores one reported document, merging into the open
// proposal for that document when there is one. Merging rather than creating
// is what keeps review tractable: a repository that moves three times before
// anyone looks leaves one proposal carrying three rounds of drift, not three
// proposals racing each other on the same file.
func (h *Handler) upsertDocDriftProposal(ctx context.Context, wsID pgtype.UUID, repo string, task db.AgentTaskQueue, reported DocDriftProposalReport) (db.DocDriftProposal, bool) {
	docPath := strings.TrimSpace(reported.DocPath)
	// The path came from a model, so it gets the same check a client's would:
	// it names the file a later run is told to patch.
	if err := service.ValidateDocDriftPath(docPath); err != nil {
		slog.Warn("doc drift: refused reported doc_path", "workspace_id", uuidToString(wsID), "doc_path", reported.DocPath, "error", err)
		return db.DocDriftProposal{}, false
	}
	sections := normalizeDocDriftSections(reported.Sections)
	if len(sections) == 0 {
		return db.DocDriftProposal{}, false // a proposal with no section is not a proposal
	}
	drift := docDriftSummaryText(reported.Summary, sections)
	patch := docDriftPatchText(sections)
	commit := ""
	if stats, err := h.repoIndexer().Stats(ctx, wsID, repo); err == nil {
		commit = stats.LastIndexedCommit
	}

	if open, err := h.Queries.GetOpenDocDriftProposalForDoc(ctx, db.GetOpenDocDriftProposalForDocParams{
		WorkspaceID: wsID, RepoIdentifier: repo, DocPath: docPath,
	}); err == nil {
		merged, err := h.Queries.AppendDocDriftProposal(ctx, db.AppendDocDriftProposalParams{
			ID:               open.ID,
			DetectedDrift:    truncate(strings.TrimSpace(open.DetectedDrift+"\n\n"+drift), docDriftMaxText),
			ProposedPatch:    truncate(strings.TrimSpace(open.ProposedPatch+"\n\n"+patch), docDriftMaxText),
			DetectedAtCommit: commit,
			ScanTaskID:       task.ID,
		})
		if err != nil {
			slog.Warn("doc drift: merge proposal failed", "proposal_id", uuidToString(open.ID), "error", err)
			return db.DocDriftProposal{}, false
		}
		return merged, true
	}
	created, err := h.Queries.CreateDocDriftProposal(ctx, db.CreateDocDriftProposalParams{
		ID: dbid.NewV7(), WorkspaceID: wsID, RepoIdentifier: repo, DocPath: docPath,
		DetectedDrift: truncate(drift, docDriftMaxText), ProposedPatch: truncate(patch, docDriftMaxText),
		DetectedAtCommit: commit, ScanTaskID: task.ID,
	})
	if err != nil {
		slog.Warn("doc drift: create proposal failed", "workspace_id", uuidToString(wsID), "doc_path", docPath, "error", err)
		return db.DocDriftProposal{}, false
	}
	return created, true
}

func normalizeDocDriftSections(sections []DocDriftSection) []DocDriftSection {
	out := make([]DocDriftSection, 0, len(sections))
	for _, s := range sections {
		s.Heading = truncate(strings.TrimSpace(s.Heading), docDriftMaxHeading)
		s.Reason = truncate(strings.TrimSpace(s.Reason), docDriftMaxReason)
		s.Patch = truncate(strings.TrimSpace(s.Patch), docDriftMaxPatch)
		if s.Patch == "" {
			continue // a section with nothing to apply is not actionable
		}
		out = append(out, s)
		if len(out) >= docDriftMaxSections {
			break
		}
	}
	return out
}

// docDriftSummaryText is what a reviewer reads first: the one-paragraph
// summary, then one line per section saying what is now false.
func docDriftSummaryText(summary string, sections []DocDriftSection) string {
	var b strings.Builder
	if s := truncate(strings.TrimSpace(summary), docDriftMaxSummary); s != "" {
		b.WriteString(s + "\n\n")
	}
	for _, s := range sections {
		heading := s.Heading
		if heading == "" {
			heading = "(unnamed section)"
		}
		fmt.Fprintf(&b, "- **%s** — %s\n", heading, s.Reason)
	}
	return strings.TrimSpace(b.String())
}

// docDriftPatchText is what the pull-request run applies: the sections, each
// under its heading, in report order.
func docDriftPatchText(sections []DocDriftSection) string {
	var b strings.Builder
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if s.Heading != "" {
			b.WriteString("### " + s.Heading + "\n\n")
		}
		b.WriteString(s.Patch)
	}
	return strings.TrimSpace(b.String())
}

// --- pull request run ------------------------------------------------------

// startDocDriftPRRun enqueues the run that applies the proposal and opens the
// draft pull request. Best effort by design: a failure leaves the proposal in
// `draft` with its patch intact, which is still reviewable by hand.
func (h *Handler) startDocDriftPRRun(ctx context.Context, ws db.Workspace, cfg service.DocDriftSettings, proposal db.DocDriftProposal, actorUserID string) bool {
	agentID, err := util.ParseUUID(cfg.AgentID)
	if err != nil {
		return false
	}
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || agent.WorkspaceID != ws.ID || agent.ArchivedAt.Valid {
		return false
	}
	host, ok := h.docDriftHostIssue(ctx, ws.ID, agentID)
	if !ok {
		return false
	}
	// Same enqueue as the scan: the housekeeping issue is deliberately
	// unassigned, so the agent is named explicitly rather than inherited from
	// an assignee. It also forces a fresh session, which is what this run
	// wants — it applies a patch it was handed, not a conversation it had.
	task, err := h.TaskService.EnqueueCrossReviewRun(ctx, host, agentID, docDriftPRBrief(proposal), parseUUIDOrZero(actorUserID))
	if err != nil {
		slog.Warn("doc drift: enqueue pull request run failed", "proposal_id", uuidToString(proposal.ID), "error", err)
		h.notifyDocDriftAdmins(ctx, proposal, "The agent context update could not be queued as a draft pull request. The proposal is in Settings, ready to apply by hand.")
		return false
	}
	if _, err := h.Queries.SetDocDriftProposalPRTask(ctx, db.SetDocDriftProposalPRTaskParams{ID: proposal.ID, PrTaskID: task.ID}); err != nil {
		slog.Warn("doc drift: record pull request task failed", "proposal_id", uuidToString(proposal.ID), "error", err)
	}
	return true
}

// docDriftPRBrief is deliberately narrow: one file, one branch, a draft pull
// request, nothing else. The blast radius of this run is the reason the whole
// feature is safe to have.
func docDriftPRBrief(proposal db.DocDriftProposal) string {
	branch := docDriftBranchPrefix + shortSha(strings.ReplaceAll(uuidToString(proposal.ID), "-", ""))
	var b strings.Builder
	b.WriteString("AGENT CONTEXT UPDATE. Apply exactly the patch below to `" + proposal.DocPath + "` on a NEW branch `" + branch + "`, then open a DRAFT pull request titled \"" + docDriftPRTitle + "\".\n\n")
	b.WriteString("DO NOT touch any other file. DO NOT commit to the default branch. DO NOT merge, and do not take the pull request out of draft: a human reviews this change.\n")
	b.WriteString("If the patch no longer applies because the document has moved on, adjust it minimally to fit the current file and say so in the pull request body — do not rewrite the document, and do not invent new sections.\n\n")
	fmt.Fprintf(&b, "Repository: %s\nDocument: %s\nDetected at commit: %s\n\n", proposal.RepoIdentifier, proposal.DocPath, proposal.DetectedAtCommit)
	b.WriteString("WHAT DRIFTED\n\n" + proposal.DetectedDrift + "\n\n")
	b.WriteString("PATCH TO APPLY\n\n" + proposal.ProposedPatch + "\n\n")
	b.WriteString("Use the pull request body to say what drifted and how you verified it. Report the pull request URL when you finish.\n")
	return b.String()
}

// storeDocDriftPR runs at the pull-request run's completion: a URL moves the
// proposal to `opened_pr`; no URL leaves it in `draft` and tells the admins,
// because a proposal nobody can find is the same as no proposal.
func (h *Handler) storeDocDriftPR(ctx context.Context, task db.AgentTaskQueue, prURL string) {
	proposal, err := h.Queries.GetDocDriftProposalByPRTask(ctx, task.ID)
	if err != nil || proposal.Status != "draft" {
		return // not a drift pull-request run, or already settled
	}
	prURL = strings.TrimSpace(prURL)
	if prURL == "" {
		h.notifyDocDriftAdmins(ctx, proposal, "The agent context update run finished without reporting a pull request URL. The proposal is still in Settings; open the pull request by hand or retry it.")
		return
	}
	updated, err := h.Queries.SetDocDriftProposalPullRequest(ctx, db.SetDocDriftProposalPullRequestParams{
		ID: proposal.ID, PullRequestUrl: truncate(prURL, 1000),
	})
	if err != nil {
		slog.Warn("doc drift: record pull request url failed", "proposal_id", uuidToString(proposal.ID), "error", err)
		return
	}
	h.audit(ctx, updated.WorkspaceID, "agent", uuidToString(task.AgentID), AuditDocDriftPROpened, "workspace", updated.WorkspaceID,
		map[string]any{"proposal_id": uuidToString(updated.ID), "doc_path": updated.DocPath, "pull_request_url": updated.PullRequestUrl}, nil)
	h.publish("doc_drift:pr_opened", uuidToString(updated.WorkspaceID), "agent", uuidToString(task.AgentID),
		map[string]any{"proposal": docDriftToResponse(updated)})
	h.notifyDocDriftAdmins(ctx, updated, "A draft pull request updates "+updated.DocPath+" after the repository moved: "+updated.PullRequestUrl)
}

// failDocDriftRun settles a crashed drift run: a scan releases its repository,
// a pull-request run leaves its proposal in `draft` and says so.
func (h *Handler) failDocDriftRun(ctx context.Context, task db.AgentTaskQueue, reason string) {
	if !h.isDocDriftRun(ctx, task) {
		return
	}
	if wsID, repo, ok := h.docDriftScanContext(ctx, task); ok {
		h.clearDocDriftScanTask(ctx, wsID, repo)
		return
	}
	proposal, err := h.Queries.GetDocDriftProposalByPRTask(ctx, task.ID)
	if err != nil || proposal.Status != "draft" {
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "the run failed"
	}
	h.notifyDocDriftAdmins(ctx, proposal, "The agent context update run failed: "+truncate(reason, 500)+". The proposal is still in Settings.")
}

// notifyDocDriftAdmins puts one inbox item in front of every workspace admin.
func (h *Handler) notifyDocDriftAdmins(ctx context.Context, proposal db.DocDriftProposal, body string) {
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, proposal.WorkspaceID)
	if err != nil {
		slog.Warn("doc drift: list admins failed", "workspace_id", uuidToString(proposal.WorkspaceID), "error", err)
		return
	}
	details, _ := json.Marshal(map[string]any{
		"proposal_id": uuidToString(proposal.ID), "doc_path": proposal.DocPath,
		"repo_identifier": proposal.RepoIdentifier, "status": proposal.Status,
		"pull_request_url": proposal.PullRequestUrl,
	})
	for _, rcpt := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: proposal.WorkspaceID, RecipientType: rcpt.Type, RecipientID: rcpt.ID,
			Type: InboxTypeDocDriftReport, Severity: "info", Title: "Agent context drift",
			Body:      pgtype.Text{String: truncate(body, 1000), Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			slog.Warn("doc drift: inbox failed", "workspace_id", uuidToString(proposal.WorkspaceID), "error", err)
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(proposal.WorkspaceID), "system", "",
			map[string]any{"item": inboxToResponse(item)})
	}
}

// --- proposals API ---------------------------------------------------------

// ListDocDriftProposals: GET /api/doc-drift/proposals.
func (h *Handler) ListDocDriftProposals(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListDocDriftProposals(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list the drift proposals")
		return
	}
	out := make([]DocDriftProposalResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, docDriftToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": out})
}

// loadDocDriftProposalForAdmin resolves a proposal of the caller's workspace.
func (h *Handler) loadDocDriftProposalForAdmin(w http.ResponseWriter, r *http.Request) (db.DocDriftProposal, pgtype.UUID, bool) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return db.DocDriftProposal{}, pgtype.UUID{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "proposal id")
	if !ok {
		return db.DocDriftProposal{}, pgtype.UUID{}, false
	}
	proposal, err := h.Queries.GetDocDriftProposal(r.Context(), id)
	if err != nil || proposal.WorkspaceID != wsUUID {
		writeError(w, http.StatusNotFound, "proposal not found")
		return db.DocDriftProposal{}, pgtype.UUID{}, false
	}
	return proposal, wsUUID, true
}

// DismissDocDriftProposal: POST /api/doc-drift/proposals/{id}/dismiss.
//
// Dismissing is an answer, not a mute: the open-state unique index no longer
// covers the row, so the next scan that still sees the drift proposes it again.
func (h *Handler) DismissDocDriftProposal(w http.ResponseWriter, r *http.Request) {
	proposal, wsUUID, ok := h.loadDocDriftProposalForAdmin(w, r)
	if !ok {
		return
	}
	updated, err := h.Queries.DismissDocDriftProposal(r.Context(), proposal.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "this proposal is already settled")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditDocDriftDismissed, "workspace", wsUUID,
		map[string]any{"proposal_id": uuidToString(updated.ID), "doc_path": updated.DocPath}, nil)
	h.publish("doc_drift:dismissed", uuidToString(wsUUID), "member", requestUserID(r),
		map[string]any{"proposal": docDriftToResponse(updated)})
	writeJSON(w, http.StatusOK, map[string]any{"proposal": docDriftToResponse(updated)})
}

// OpenDocDriftProposalPR: POST /api/doc-drift/proposals/{id}/open-pr — an admin
// asks for the draft pull request of a proposal that has none.
func (h *Handler) OpenDocDriftProposalPR(w http.ResponseWriter, r *http.Request) {
	proposal, wsUUID, ok := h.loadDocDriftProposalForAdmin(w, r)
	if !ok {
		return
	}
	if proposal.Status != "draft" {
		writeError(w, http.StatusConflict, "only a draft proposal can be opened as a pull request")
		return
	}
	if proposal.PrTaskID.Valid {
		if task, err := h.Queries.GetAgentTask(r.Context(), proposal.PrTaskID); err == nil && taskIsActive(task.Status) {
			writeError(w, http.StatusConflict, "a pull request run is already in flight for this proposal")
			return
		}
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg := service.DocDriftFromSettings(ws.Settings)
	if cfg.AgentID == "" {
		writeError(w, http.StatusBadRequest, "configure an agent before opening a pull request")
		return
	}
	if !h.startDocDriftPRRun(r.Context(), ws, cfg, proposal, requestUserID(r)) {
		writeError(w, http.StatusInternalServerError, "failed to queue the pull request run")
		return
	}
	refreshed, err := h.Queries.GetDocDriftProposal(r.Context(), proposal.ID)
	if err != nil {
		refreshed = proposal
	}
	writeJSON(w, http.StatusCreated, map[string]any{"proposal": docDriftToResponse(refreshed)})
}
