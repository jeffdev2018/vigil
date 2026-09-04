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

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Decision memory (K29): when an issue is accepted (enters a done status),
// the decisions its last completed run stated are extracted from the run's
// text messages and stored with the seq of the message that states them.
// A complex run (many edited files, or a migration) then needs at least one
// record before merge readiness (F10) clears.

const (
	blockerADRRequired = "adr_required"
	// decisionMaxPerRun caps what one extraction may store.
	decisionMaxPerRun = 5
	// decisionTranscriptMaxChars bounds the prompt; the end of the run,
	// where the agent sums up, is kept.
	decisionTranscriptMaxChars = 30000
)

type DecisionRecordResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	ProjectID        *string `json:"project_id"`
	IssueID          string  `json:"issue_id"`
	IssueIdentifier  string  `json:"issue_identifier,omitempty"`
	IssueTitle       string  `json:"issue_title,omitempty"`
	RunID            string  `json:"run_id"`
	SourceMessageSeq int32   `json:"source_message_seq"`
	Title            string  `json:"title"`
	Context          string  `json:"context"`
	Decision         string  `json:"decision"`
	Consequences     *string `json:"consequences"`
	AuthorType       string  `json:"author_type"`
	AuthorID         *string `json:"author_id"`
	CreatedAt        string  `json:"created_at"`
}

func decisionRecordToResponse(d db.DecisionRecord) DecisionRecordResponse {
	return DecisionRecordResponse{
		ID:               uuidToString(d.ID),
		WorkspaceID:      uuidToString(d.WorkspaceID),
		ProjectID:        uuidToPtr(d.ProjectID),
		IssueID:          uuidToString(d.IssueID),
		RunID:            uuidToString(d.RunID),
		SourceMessageSeq: d.SourceMessageSeq,
		Title:            d.Title,
		Context:          d.Context,
		Decision:         d.Decision,
		Consequences:     textToPtr(d.Consequences),
		AuthorType:       d.AuthorType,
		AuthorID:         uuidToPtr(d.AuthorID),
		CreatedAt:        timestampToString(d.CreatedAt),
	}
}

// ADRRequirement is what the gate saw on the issue's last completed run.
type ADRRequirement struct {
	Required      bool   `json:"required"`
	Satisfied     bool   `json:"satisfied"`
	Files         int    `json:"files"`
	FileThreshold int    `json:"file_threshold"`
	Migration     bool   `json:"migration"`
	Decisions     int    `json:"decisions"`
	RunID         string `json:"run_id,omitempty"`
}

var (
	// editToolRe names the tools that change files across the agent CLIs
	// (Write, Edit, MultiEdit, NotebookEdit, apply_patch, create_file,
	// str_replace_editor); read-only tools stay out of the count.
	editToolRe  = regexp.MustCompile(`(?i)write|edit|patch|create|notebook`)
	patchFileRe = regexp.MustCompile(`\*\*\* (?:Update|Add|Delete) File: (\S+)`)
	pathKeys    = []string{"file_path", "path", "filePath", "notebook_path", "target_file"}
)

// runComplexity counts the distinct files a run edited and whether one of
// them is a migration, from the tool calls in its log.
// ponytail: naive tool-name and input-key heuristic; read the PR's file
// list from the forge once vcs_pull_request carries it.
func runComplexity(msgs []db.TaskMessage) (files int, migration bool) {
	seen := map[string]bool{}
	note := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		seen[p] = true
		if strings.Contains(p, "migrations/") {
			migration = true
		}
	}
	for _, m := range msgs {
		if m.Type != "tool_use" || !editToolRe.MatchString(m.Tool.String) || len(m.Input) == 0 {
			continue
		}
		var in map[string]any
		if json.Unmarshal(m.Input, &in) != nil {
			continue
		}
		for _, k := range pathKeys {
			if s, ok := in[k].(string); ok {
				note(s)
			}
		}
		if patch, ok := in["patch"].(string); ok {
			for _, mm := range patchFileRe.FindAllStringSubmatch(patch, -1) {
				note(mm[1])
			}
		}
	}
	return len(seen), migration
}

// adrRequirement evaluates the gate for an issue. Without a completed run
// there is nothing to measure, so nothing is required.
func (h *Handler) adrRequirement(ctx context.Context, issue db.Issue) (ADRRequirement, error) {
	ws, err := h.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return ADRRequirement{}, fmt.Errorf("load workspace: %w", err)
	}
	gate := service.ADRGateSettings(ws.Settings)
	n, err := h.Queries.CountIssueDecisionRecords(ctx, issue.ID)
	if err != nil {
		return ADRRequirement{}, fmt.Errorf("count decisions: %w", err)
	}
	req := ADRRequirement{FileThreshold: gate.FileThreshold, Decisions: int(n), Satisfied: true}
	if !gate.Enabled() {
		return req, nil
	}
	run, err := h.Queries.GetLatestCompletedTaskForIssue(ctx, issue.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return req, nil
	}
	if err != nil {
		return ADRRequirement{}, fmt.Errorf("load run: %w", err)
	}
	msgs, err := h.Queries.ListTaskMessages(ctx, run.ID)
	if err != nil {
		return ADRRequirement{}, fmt.Errorf("load run messages: %w", err)
	}
	req.RunID = uuidToString(run.ID)
	req.Files, req.Migration = runComplexity(msgs)
	req.Required = gate.Requires(req.Files, req.Migration)
	req.Satisfied = !req.Required || req.Decisions > 0
	return req, nil
}

// adrBlocker is the merge readiness (F10) condition: nil when the gate is
// satisfied or cannot be evaluated (logged; readiness must not 500 for it).
func (h *Handler) adrBlocker(ctx context.Context, issue db.Issue) *MergeBlocker {
	req, err := h.adrRequirement(ctx, issue)
	if err != nil {
		slog.Warn("adr gate: evaluation failed", "error", err, "issue_id", uuidToString(issue.ID))
		return nil
	}
	if req.Satisfied {
		return nil
	}
	return &MergeBlocker{Kind: blockerADRRequired, Label: "Architecture decision record required", Count: req.Files}
}

const decisionExtractSystemPrompt = `You read the transcript of a coding agent's run that was accepted. Extract the technical decisions the agent actually stated: a choice between alternatives, a design or architecture commitment, a trade-off it took. Ignore routine steps, observations, and plans that were not carried out.
Reply with JSON only: {"decisions":[{"source_seq":<seq of the message that states the decision>,"title":"<short title>","context":"<why the choice came up>","decision":"<what was chosen>","consequences":"<what follows, or an empty string>"}]}.
At most 5 decisions; an empty array when none were stated. Never invent a decision that is not in the transcript, and only use seq numbers that appear in it.`

// decisionTranscript renders the run's text messages, newest kept when the
// budget runs out, and the set of seqs a decision may cite.
func decisionTranscript(msgs []db.TaskMessage) (string, map[int32]bool) {
	seqs := map[int32]bool{}
	var lines []string
	total := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Type != "text" || strings.TrimSpace(m.Content.String) == "" {
			continue
		}
		line := fmt.Sprintf("[seq %d] %s", m.Seq, strings.TrimSpace(m.Content.String))
		if total+len(line) > decisionTranscriptMaxChars && len(lines) > 0 {
			break
		}
		total += len(line)
		lines = append(lines, line)
		seqs[m.Seq] = true
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "\n\n"), seqs
}

type extractedDecision struct {
	SourceSeq    int32  `json:"source_seq"`
	Title        string `json:"title"`
	Context      string `json:"context"`
	Decision     string `json:"decision"`
	Consequences string `json:"consequences"`
}

// extractDecisions stores the decisions of the issue's last completed run.
// Idempotent per run: a run that already has records is left alone. Returns
// how many records were created; zero without an LLM, a run, or text.
func (h *Handler) extractDecisions(ctx context.Context, issue db.Issue) (int, error) {
	if h.LLM == nil || !h.LLM.Enabled() {
		return 0, nil
	}
	run, err := h.Queries.GetLatestCompletedTaskForIssue(ctx, issue.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load run: %w", err)
	}
	if n, err := h.Queries.CountRunDecisionRecords(ctx, run.ID); err != nil {
		return 0, fmt.Errorf("count run decisions: %w", err)
	} else if n > 0 {
		return 0, nil
	}
	msgs, err := h.Queries.ListTaskMessages(ctx, run.ID)
	if err != nil {
		return 0, fmt.Errorf("load run messages: %w", err)
	}
	transcript, seqs := decisionTranscript(msgs)
	if transcript == "" {
		return 0, nil
	}
	raw, err := h.LLM.GenerateJSON(ctx, "", decisionExtractSystemPrompt, transcript, 0.1, 2048)
	if err != nil {
		return 0, fmt.Errorf("llm: %w", err)
	}
	var out struct {
		Decisions []extractedDecision `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return 0, fmt.Errorf("malformed extraction: %w", err)
	}
	created := 0
	for _, d := range out.Decisions {
		if created >= decisionMaxPerRun {
			break
		}
		if !seqs[d.SourceSeq] || strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.Decision) == "" {
			continue
		}
		if _, err := h.Queries.CreateDecisionRecord(ctx, db.CreateDecisionRecordParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, ProjectID: issue.ProjectID, IssueID: issue.ID, RunID: run.ID,
			SourceMessageSeq: d.SourceSeq, Title: strings.TrimSpace(d.Title), Context: strings.TrimSpace(d.Context),
			Decision: strings.TrimSpace(d.Decision), Consequences: optionalText(d.Consequences),
			AuthorType: "agent", AuthorID: run.AgentID,
		}); err != nil {
			return created, fmt.Errorf("store decision: %w", err)
		}
		created++
	}
	if created > 0 {
		h.audit(ctx, issue.WorkspaceID, "agent", uuidToString(run.AgentID), AuditDecisionRecorded, "issue", issue.ID,
			map[string]any{"count": created, "run_id": uuidToString(run.ID), "source": "extraction"}, nil)
	}
	return created, nil
}

func optionalText(s string) pgtype.Text {
	s = strings.TrimSpace(s)
	return pgtype.Text{String: s, Valid: s != ""}
}

// issueAccepted tells whether the issue now sits in a done-category status.
func (h *Handler) issueAccepted(ctx context.Context, issue db.Issue) bool {
	if issue.Status == "done" {
		return true
	}
	entry, err := issuestatus.Resolve(ctx, h.Queries, issue.WorkspaceID, issue.Status)
	return err == nil && entry.Category == "done"
}

// extractDecisionsAsync runs the extraction off the request: the status
// change must not wait for a model call.
func (h *Handler) extractDecisionsAsync(issue db.Issue) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if _, err := h.extractDecisions(ctx, issue); err != nil {
			slog.Warn("decision extraction failed", "error", err, "issue_id", uuidToString(issue.ID))
		}
	}()
}

// GET /api/projects/{id}/decisions?author_type=agent|member
func (h *Handler) ListProjectDecisions(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	authorType := pgtype.Text{}
	switch at := r.URL.Query().Get("author_type"); at {
	case "":
	case "agent", "member":
		authorType = pgtype.Text{String: at, Valid: true}
	default:
		writeError(w, http.StatusBadRequest, "author_type must be agent or member")
		return
	}
	rows, err := h.Queries.ListProjectDecisionRecords(r.Context(), db.ListProjectDecisionRecordsParams{WorkspaceID: wsUUID, ProjectID: projectID, AuthorType: authorType})
	if err != nil {
		slog.Warn("list project decisions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list decisions")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	out := make([]DecisionRecordResponse, 0, len(rows))
	for _, row := range rows {
		resp := decisionRecordToResponse(row.DecisionRecord)
		resp.IssueIdentifier = fmt.Sprintf("%s-%d", prefix, row.IssueNumber)
		resp.IssueTitle = row.IssueTitle
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"decisions": out})
}

// GET /api/issues/{id}/adr-required
func (h *Handler) GetIssueADRRequirement(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	req, err := h.adrRequirement(r.Context(), issue)
	if err != nil {
		slog.Warn("adr requirement failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to evaluate the ADR gate")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

type createDecisionsRequest struct {
	RunID     string `json:"run_id"`
	Decisions []struct {
		SourceMessageSeq int32  `json:"source_message_seq"`
		Title            string `json:"title"`
		Context          string `json:"context"`
		Decision         string `json:"decision"`
		Consequences     string `json:"consequences"`
	} `json:"decisions"`
}

// POST /api/issues/{id}/decision-records records decisions by hand (the
// /decisions path belongs to Decision Cards, K01) (a human, or an
// agent through its task token). Each must cite a message of the run; the
// run defaults to the issue's last completed one.
func (h *Handler) CreateIssueDecisions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req createDecisionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Decisions) == 0 || len(req.Decisions) > decisionMaxPerRun {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("decisions must hold 1 to %d items", decisionMaxPerRun))
		return
	}
	var run db.AgentTaskQueue
	var err error
	if req.RunID != "" {
		runID, ok := parseUUIDOrBadRequest(w, req.RunID, "run id")
		if !ok {
			return
		}
		run, err = h.Queries.GetIssueTask(r.Context(), db.GetIssueTaskParams{ID: runID, IssueID: issue.ID})
	} else {
		run, err = h.Queries.GetLatestCompletedTaskForIssue(r.Context(), issue.ID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_source", "no run on this issue to cite")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load run")
		return
	}
	msgs, err := h.Queries.ListTaskMessages(r.Context(), run.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load run messages")
		return
	}
	seqs := map[int32]bool{}
	for _, m := range msgs {
		seqs[m.Seq] = true
	}
	for _, d := range req.Decisions {
		if strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.Decision) == "" {
			writeError(w, http.StatusBadRequest, "title and decision are required")
			return
		}
		if !seqs[d.SourceMessageSeq] {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_source", fmt.Sprintf("message seq %d is not in run %s", d.SourceMessageSeq, uuidToString(run.ID)))
			return
		}
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	authorID := pgtype.UUID{}
	if err := authorID.Scan(actorID); err != nil {
		authorID = pgtype.UUID{}
	}
	out := make([]DecisionRecordResponse, 0, len(req.Decisions))
	for _, d := range req.Decisions {
		rec, err := h.Queries.CreateDecisionRecord(r.Context(), db.CreateDecisionRecordParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, ProjectID: issue.ProjectID, IssueID: issue.ID, RunID: run.ID,
			SourceMessageSeq: d.SourceMessageSeq, Title: strings.TrimSpace(d.Title), Context: strings.TrimSpace(d.Context),
			Decision: strings.TrimSpace(d.Decision), Consequences: optionalText(d.Consequences),
			AuthorType: actorType, AuthorID: authorID,
		})
		if err != nil {
			slog.Warn("create decision failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to store decision")
			return
		}
		out = append(out, decisionRecordToResponse(rec))
	}
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditDecisionRecorded, "issue", issue.ID,
		map[string]any{"count": len(out), "run_id": uuidToString(run.ID), "source": "manual"}, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"decisions": out})
}
