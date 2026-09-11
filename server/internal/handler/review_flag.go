package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Review flags by severity (F06 / JEF-19).
//
// A flag is one structured finding: a file, a line range of one revision of a
// linked pull request's head, a severity and — optionally — how sure the
// author is. Any agent can record one from its run through the CLI; only a
// human settles it.
//
// Two decisions are worth stating because they are what make the list usable:
//
//   - the ORDER is server-side (see queries/review_flag.sql). Severity beats
//     confidence, because a bug the author is 40% sure of still outranks a
//     warning they are certain of. A client that sorted this itself would be
//     a second, quietly diverging opinion about what a reviewer reads first.
//   - a head that moves marks flags `stale`, never deletes them. The finding
//     was true of the code it was written against, and the reviewer who has
//     to justify a merge needs that record more than a tidy list.

const (
	// EventReviewFlagChanged is published on workspace scope so an open issue
	// panel re-queries the flags it is showing.
	EventReviewFlagChanged = "review_flag:changed"

	// reviewFlagsPerTaskCap bounds one run's contribution. A run that finds
	// more than a hundred things has stopped reviewing and started linting,
	// and the reviewer would never read past the first screen anyway.
	reviewFlagsPerTaskCap = 100

	reviewFlagTitleMax = 300
	reviewFlagBodyMax  = 10000
	reviewFlagPathMax  = 1024
)

// --- wire types ------------------------------------------------------------

// ReviewFlagResponse is one flag as the issue panel reads it. `confidence` is
// a pointer: "did not say" and "0% sure" are different answers and the list
// sorts them differently.
type ReviewFlagResponse struct {
	ID             string `json:"id"`
	IssueID        string `json:"issue_id"`
	PrSource       string `json:"pr_source"`
	PrID           string `json:"pr_id"`
	HeadSha        string `json:"head_sha"`
	FilePath       string `json:"file_path"`
	LineStart      int32  `json:"line_start"`
	LineEnd        int32  `json:"line_end"`
	Side           string `json:"side"`
	Severity       string `json:"severity"`
	Confidence     *int   `json:"confidence"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	AuthorAgentID  string `json:"author_agent_id"`
	AuthorUserID   string `json:"author_user_id"`
	TaskID         string `json:"task_id"`
	State          string `json:"state"`
	ResolvedByType string `json:"resolved_by_type"`
	ResolvedByID   string `json:"resolved_by_id"`
	ResolvedAt     string `json:"resolved_at"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// ReviewFlagCounts is always over OPEN flags, whatever the list filter asked
// for. The badge answers "what is still owed on this issue"; counting settled
// findings in it would make the number grow as the reviewer works.
type ReviewFlagCounts struct {
	Bug     int `json:"bug"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

type reviewFlagListResponse struct {
	Flags  []ReviewFlagResponse `json:"flags"`
	Counts ReviewFlagCounts     `json:"counts"`
}

type createReviewFlagRequest struct {
	PrID       string `json:"pr_id"`
	HeadSha    string `json:"head_sha"`
	FilePath   string `json:"file_path"`
	LineStart  *int   `json:"line_start"`
	LineEnd    *int   `json:"line_end"`
	Side       string `json:"side"`
	Severity   string `json:"severity"`
	Confidence *int   `json:"confidence"`
	Title      string `json:"title"`
	Body       string `json:"body"`
}

type setReviewFlagStateRequest struct {
	State string `json:"state"`
}

func reviewFlagToResponse(row db.ReviewFlag) ReviewFlagResponse {
	out := ReviewFlagResponse{
		ID:             uuidToString(row.ID),
		IssueID:        uuidToString(row.IssueID),
		PrSource:       row.PrSource,
		PrID:           uuidToString(row.PrID),
		HeadSha:        row.HeadSha,
		FilePath:       row.FilePath,
		LineStart:      row.LineStart,
		LineEnd:        row.LineEnd,
		Side:           row.Side,
		Severity:       row.Severity,
		Title:          row.Title,
		Body:           row.Body,
		AuthorAgentID:  uuidToString(row.AuthorAgentID),
		AuthorUserID:   uuidToString(row.AuthorUserID),
		TaskID:         uuidToString(row.TaskID),
		State:          row.State,
		ResolvedByType: row.ResolvedByType,
		ResolvedByID:   uuidToString(row.ResolvedByID),
		ResolvedAt:     timestampToString(row.ResolvedAt),
		CreatedAt:      timestampToString(row.CreatedAt),
		UpdatedAt:      timestampToString(row.UpdatedAt),
	}
	if row.Confidence.Valid {
		v := int(row.Confidence.Int16)
		out.Confidence = &v
	}
	return out
}

// --- read ------------------------------------------------------------------

// ListIssueReviewFlags: GET /api/issues/{id}/review-flags?state=open|all.
func (h *Handler) ListIssueReviewFlags(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListReviewFlagsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the review flags")
		return
	}
	// Anything that is not the explicit `all` reads as `open`: the open list
	// is the safe default for an unknown value, because it never hides a
	// finding the reviewer still owes an answer to.
	openOnly := strings.TrimSpace(r.URL.Query().Get("state")) != "all"

	out := reviewFlagListResponse{Flags: []ReviewFlagResponse{}}
	for _, row := range rows {
		if row.State == "open" {
			switch row.Severity {
			case "bug":
				out.Counts.Bug++
			case "warning":
				out.Counts.Warning++
			default:
				out.Counts.Info++
			}
		} else if openOnly {
			continue
		}
		out.Flags = append(out.Flags, reviewFlagToResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// --- create ----------------------------------------------------------------

// CreateIssueReviewFlag: POST /api/issues/{id}/review-flags.
//
// The pull request must already be linked to the issue: `loadLinkedPR` answers
// only from the issue's own link lists, so a PR id belonging to another issue
// is indistinguishable from one that does not exist and both get a 404.
func (h *Handler) CreateIssueReviewFlag(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.requireProjectWrite(w, r, issue.ProjectID) {
		return
	}
	var req createReviewFlagRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pr, ok := h.loadLinkedPR(r.Context(), issue, strings.TrimSpace(req.PrID))
	if !ok {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}

	params, ok := validateReviewFlagRequest(w, req, pr)
	if !ok {
		return
	}

	// Authorship. A run stamps the agent and the task it is executing; a human
	// stamps the user. resolveActor is the same identity the rest of the issue
	// routes use, so a flag's author matches its comments' author.
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	if actorType == "agent" {
		params.AuthorAgentID = parseUUIDOrEmpty(actorID)
		if task, ok := h.taskFromRequestHeader(r); ok {
			params.TaskID = task.ID
			// The cap is per task, not per issue: a second run on the same
			// issue gets its own hundred, and one run cannot bury the review.
			if n, err := h.Queries.CountReviewFlagsForTask(r.Context(), task.ID); err == nil && n >= reviewFlagsPerTaskCap {
				writeErrorCode(w, http.StatusUnprocessableEntity, "too_many_flags",
					"this run has already recorded the maximum number of review flags")
				return
			}
		}
	} else {
		params.AuthorUserID = parseUUIDOrEmpty(actorID)
	}

	params.ID = dbid.NewV7()
	params.WorkspaceID = issue.WorkspaceID
	params.IssueID = issue.ID
	params.PrSource = pr.source
	params.PrID = pr.id

	row, err := h.Queries.CreateReviewFlag(r.Context(), params)
	if err != nil {
		slog.Warn("review flag: create failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record the review flag")
		return
	}
	h.publishReviewFlagChanged(issue.WorkspaceID, issue.ID)
	writeJSON(w, http.StatusCreated, reviewFlagToResponse(row))
}

// validateReviewFlagRequest turns the request into insert params, answering
// 400 itself on anything it refuses. Every bound is checked here rather than
// left to the CHECK constraints: a constraint violation reaches the caller as
// a 500 with no idea which field was wrong.
func validateReviewFlagRequest(w http.ResponseWriter, req createReviewFlagRequest, pr linkedPR) (db.CreateReviewFlagParams, bool) {
	var out db.CreateReviewFlagParams

	out.FilePath = strings.TrimSpace(req.FilePath)
	if out.FilePath == "" || len(out.FilePath) > reviewFlagPathMax {
		writeError(w, http.StatusBadRequest, "file_path is required")
		return out, false
	}
	out.Title = strings.TrimSpace(req.Title)
	if out.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return out, false
	}
	if len(out.Title) > reviewFlagTitleMax {
		out.Title = util.TruncateUTF8Bytes(out.Title, reviewFlagTitleMax)
	}
	out.Body = strings.TrimSpace(req.Body)
	if len(out.Body) > reviewFlagBodyMax {
		out.Body = util.TruncateUTF8Bytes(out.Body, reviewFlagBodyMax)
	}

	switch strings.ToLower(strings.TrimSpace(req.Severity)) {
	case "bug":
		out.Severity = "bug"
	case "warning":
		out.Severity = "warning"
	case "info":
		out.Severity = "info"
	default:
		writeError(w, http.StatusBadRequest, "severity must be one of bug, warning, info")
		return out, false
	}

	switch strings.ToLower(strings.TrimSpace(req.Side)) {
	case "", "new":
		out.Side = "new"
	case "old":
		out.Side = "old"
	default:
		writeError(w, http.StatusBadRequest, "side must be old or new")
		return out, false
	}

	if req.LineStart == nil || *req.LineStart < 1 {
		writeError(w, http.StatusBadRequest, "line_start must be a positive line number")
		return out, false
	}
	end := *req.LineStart
	if req.LineEnd != nil {
		end = *req.LineEnd
	}
	if end < *req.LineStart {
		writeError(w, http.StatusBadRequest, "line_end must not be before line_start")
		return out, false
	}
	out.LineStart = int32(*req.LineStart)
	out.LineEnd = int32(end)

	if req.Confidence != nil {
		if *req.Confidence < 0 || *req.Confidence > 100 {
			writeError(w, http.StatusBadRequest, "confidence must be between 0 and 100")
			return out, false
		}
		out.Confidence = pgtype.Int2{Int16: int16(*req.Confidence), Valid: true}
	}

	// An omitted head defaults to the PR's current one: the common case is a
	// run flagging the revision it was just given, and making it repeat the
	// sha back to us only creates a way to get it wrong.
	out.HeadSha = strings.TrimSpace(req.HeadSha)
	if out.HeadSha == "" {
		out.HeadSha = pr.headSHA
	}
	return out, true
}

// parseUUIDOrEmpty is the actor-id variant: resolveActor can hand back an
// empty or malformed id (an unauthenticated caller, a header the middleware
// did not set), and an unset author is a better record than a panic.
func parseUUIDOrEmpty(s string) pgtype.UUID {
	id, err := parseUUIDChecked(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

// --- state transitions -----------------------------------------------------

// SetIssueReviewFlagState: PATCH /api/issues/{id}/review-flags/{flagId}.
//
// Humans only. An agent may record what it found; deciding that a finding is
// handled — or not worth handling — is the reviewer's call, and an agent that
// could resolve its own flags would make the list say nothing.
func (h *Handler) SetIssueReviewFlagState(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "only a human can resolve or dismiss a review flag")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.requireProjectWrite(w, r, issue.ProjectID) {
		return
	}
	flagID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "flagId"), "flag id")
	if !ok {
		return
	}
	var req setReviewFlagStateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var state string
	switch strings.ToLower(strings.TrimSpace(req.State)) {
	case "open", "resolved", "dismissed":
		state = strings.ToLower(strings.TrimSpace(req.State))
	default:
		// `stale` is deliberately absent: it is what a moving head does, not
		// something a person asserts.
		writeError(w, http.StatusBadRequest, "state must be one of open, resolved, dismissed")
		return
	}

	row, err := h.Queries.GetReviewFlag(r.Context(), flagID)
	// A flag of another issue is 404, not 403: the caller learns nothing about
	// what exists outside the issue they are allowed to read.
	if err != nil || row.IssueID != issue.ID {
		writeError(w, http.StatusNotFound, "review flag not found")
		return
	}

	params := db.SetReviewFlagStateParams{ID: row.ID, State: state}
	if state != "open" {
		params.ResolvedByType = "member"
		params.ResolvedByID = parseUUIDOrEmpty(requestUserID(r))
	}
	updated, err := h.Queries.SetReviewFlagState(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the review flag")
		return
	}
	h.publishReviewFlagChanged(issue.WorkspaceID, issue.ID)
	writeJSON(w, http.StatusOK, reviewFlagToResponse(updated))
}

// --- head moves ------------------------------------------------------------

// staleReviewFlagsForHead is the shared head-move hook, called from both
// places the server learns a pull request moved: the GitHub snapshot pipeline
// and the VCS webhook. Every open flag written against an older head becomes
// `stale` — still readable, no longer a claim about the current code.
//
// Idempotent: a re-applied snapshot for an unchanged head matches no row.
func (h *Handler) staleReviewFlagsForHead(ctx context.Context, wsID pgtype.UUID, prID pgtype.UUID, headSHA string) {
	if headSHA == "" {
		return
	}
	if err := h.Queries.StaleReviewFlagsForMovedHead(ctx, db.StaleReviewFlagsForMovedHeadParams{
		PrID: prID, HeadSha: headSHA,
	}); err != nil {
		slog.Warn("review flag: stale on head move failed", "pr_id", uuidToString(prID), "error", err)
		return
	}
	// Published without an issue id: the move can touch flags on several
	// linked issues at once, and the client invalidates the whole prefix.
	h.publishReviewFlagChanged(wsID, pgtype.UUID{})
}

func (h *Handler) publishReviewFlagChanged(wsID, issueID pgtype.UUID) {
	h.publish(EventReviewFlagChanged, uuidToString(wsID), "system", "", map[string]any{
		"issue_id": uuidToString(issueID),
	})
}
