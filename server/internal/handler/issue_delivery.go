package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type DeliveryRun struct {
	ID          string          `json:"id"`
	Status      string          `json:"status"`
	Result      json.RawMessage `json:"result"`
	Error       *string         `json:"error"`
	CompletedAt *string         `json:"completed_at"`
}

type DeliveryPullRequest struct {
	GitHubPullRequestResponse
	HeadSHA string `json:"head_sha"`
}

type DeliverySnapshot struct {
	Title        string                `json:"title"`
	Description  *string               `json:"description"`
	Criteria     []string              `json:"criteria"`
	Revision     int32                 `json:"revision"`
	Run          *DeliveryRun          `json:"run"`
	PullRequests []DeliveryPullRequest `json:"pull_requests"`
}

type DeliveryAssessment struct {
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

type DeliveryReview struct {
	UsageSnapshot       *DeliveryUsageSnapshot `json:"usage_snapshot"`
	ReviewDelaySeconds  *int64                 `json:"review_delay_seconds"`
	HumanEffortSeconds  *int64                 `json:"human_effort_seconds"`
	ID                  string                 `json:"id"`
	Decision            string                 `json:"decision"`
	Feedback            string                 `json:"feedback"`
	Assessments         []DeliveryAssessment   `json:"assessments"`
	Snapshot            DeliverySnapshot       `json:"snapshot"`
	SnapshotToken       string                 `json:"snapshot_token"`
	ReviewedBy          string                 `json:"reviewed_by"`
	CreatedAt           string                 `json:"created_at"`
	CorrectionTaskID    *string                `json:"correction_task_id"`
}

type IssueDeliveryResponse struct {
	Metrics db.GetIssueDeliveryMetricsRow `json:"metrics"`
	DeliverySnapshot
	SnapshotToken string          `json:"snapshot_token"`
	LatestReview  *DeliveryReview `json:"latest_review"`
	ReviewStale   bool            `json:"review_stale"`
}

func (h *Handler) deliveryCriteriaBrief(ctx context.Context, issue db.Issue) (string, error) {
	contract, err := h.Queries.GetIssueDeliveryContract(ctx, db.GetIssueDeliveryContractParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var criteria []string
	if err := json.Unmarshal(contract.Criteria, &criteria); err != nil {
		return "", err
	}
	if len(criteria) == 0 {
		return "", nil
	}
	var brief strings.Builder
	fmt.Fprintf(&brief, "\n\n### Acceptance criteria for issue %s (revision %d)\n", uuidToString(issue.ID), contract.Revision)
	brief.WriteString("These human-defined criteria apply only to this issue. Report evidence and remaining gaps for each criterion. Only a human can accept delivery; a completed run is not acceptance.\n")
	for i, criterion := range criteria {
		fmt.Fprintf(&brief, "%d. %s\n", i+1, criterion)
	}
	return brief.String(), nil
}

func (h *Handler) loadIssueForDelivery(w http.ResponseWriter, r *http.Request) (db.Issue, string, bool) {
	member, ok := h.requireWorkspaceMember(w, r, h.resolveWorkspaceID(r), "issue not found")
	if !ok {
		return db.Issue{}, "", false
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	return issue, uuidToString(member.UserID), ok
}

func deliveryReviewResponse(row db.IssueDeliveryReview) (DeliveryReview, error) {
	resp := DeliveryReview{ID: uuidToString(row.ID), Decision: row.Decision, Feedback: row.Feedback,
		SnapshotToken: row.SnapshotToken, ReviewedBy: uuidToString(row.ReviewedBy), CreatedAt: timestampToString(row.CreatedAt),
		CorrectionTaskID: uuidToPtr(row.CorrectionTaskID)}
	if err := json.Unmarshal(row.Snapshot, &resp.Snapshot); err != nil {
		return resp, err
	}
	if len(row.UsageSnapshot) > 0 {
		if err := json.Unmarshal(row.UsageSnapshot, &resp.UsageSnapshot); err != nil {
			return resp, err
		}
	}
	if resp.Snapshot.Run != nil && resp.Snapshot.Run.CompletedAt != nil {
		completed, err := time.Parse(time.RFC3339, *resp.Snapshot.Run.CompletedAt)
		if err == nil && row.CreatedAt.Valid && !row.CreatedAt.Time.Before(completed) {
			seconds := int64(row.CreatedAt.Time.Sub(completed).Seconds())
			resp.ReviewDelaySeconds = &seconds
		}
	}
	if row.HumanEffortSeconds.Valid {
		seconds := row.HumanEffortSeconds.Int64
		resp.HumanEffortSeconds = &seconds
	}
	return resp, json.Unmarshal(row.Assessments, &resp.Assessments)
}

func deliveryHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// Refresh timestamps are provenance, not a change to the evidence. An identical
// CI refresh must not revoke an acceptance. Heads, verdicts and counts still do.
func deliverySnapshotToken(snapshot DeliverySnapshot) (string, error) {
	snapshot.PullRequests = append([]DeliveryPullRequest{}, snapshot.PullRequests...)
	for i := range snapshot.PullRequests {
		snapshot.PullRequests[i].SnapshotFetchedAt = nil
		snapshot.PullRequests[i].SnapshotStale = false
	}
	return deliveryHash(snapshot)
}

func (h *Handler) issueDelivery(ctx context.Context, q *db.Queries, issue db.Issue) (IssueDeliveryResponse, error) {
	resp := IssueDeliveryResponse{DeliverySnapshot: DeliverySnapshot{Title: issue.Title,
		Description: textToPtr(issue.Description), Criteria: []string{}, PullRequests: []DeliveryPullRequest{}}}
	contract, err := q.GetIssueDeliveryContract(ctx, db.GetIssueDeliveryContractParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err == nil {
		resp.Revision = contract.Revision
		if err := json.Unmarshal(contract.Criteria, &resp.Criteria); err != nil {
			return resp, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return resp, err
	}
	task, err := q.GetLatestIssueDeliveryTask(ctx, db.GetLatestIssueDeliveryTaskParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err == nil {
		resp.Run = &DeliveryRun{ID: uuidToString(task.ID), Status: task.Status, Result: task.Result,
			Error: textToPtr(task.Error), CompletedAt: timestampToPtr(task.CompletedAt)}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return resp, err
	}
	prs, err := q.ListPullRequestsByIssue(ctx, issue.ID)
	if err != nil {
		return resp, err
	}
	for _, pr := range prs {
		if pr.WorkspaceID != issue.WorkspaceID {
			continue
		}
		view := issuePullRequestRowToResponse(pr, h.PRRefresh != nil && h.PRRefresh.Enabled())
		sort.Strings(view.FailedCheckNames)
		resp.PullRequests = append(resp.PullRequests, DeliveryPullRequest{GitHubPullRequestResponse: view, HeadSHA: pr.HeadSha})
	}
	vcsPRs, err := q.ListVCSPullRequestsByIssue(ctx, issue.ID)
	if err != nil {
		return resp, err
	}
	for _, pr := range vcsPRs {
		if pr.WorkspaceID == issue.WorkspaceID {
			resp.PullRequests = append(resp.PullRequests, DeliveryPullRequest{GitHubPullRequestResponse: vcsPullRequestRowToResponse(pr), HeadSHA: pr.HeadSha})
		}
	}
	sort.Slice(resp.PullRequests, func(i, j int) bool { return resp.PullRequests[i].ID < resp.PullRequests[j].ID })
	resp.SnapshotToken, err = deliverySnapshotToken(resp.DeliverySnapshot)
	if err != nil {
		return resp, err
	}
	reviews, err := q.ListIssueDeliveryReviews(ctx, db.ListIssueDeliveryReviewsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return resp, err
	}
	if len(reviews) > 0 {
		review, err := deliveryReviewResponse(reviews[0])
		if err != nil {
			return resp, err
		}
		resp.LatestReview = &review
		resp.ReviewStale = review.SnapshotToken != resp.SnapshotToken
	}
	resp.Metrics, err = q.GetIssueDeliveryMetrics(ctx, db.GetIssueDeliveryMetricsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return resp, err
	}
	return resp, nil
}

func (h *Handler) GetIssueDelivery(w http.ResponseWriter, r *http.Request) {
	issue, _, ok := h.loadIssueForDelivery(w, r)
	if !ok {
		return
	}
	// All proof queries observe one database snapshot, including the issue itself.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read delivery")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read delivery")
		return
	}
	q := h.Queries.WithTx(tx)
	issue, err = q.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	resp, err := h.issueDelivery(r.Context(), q, issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read delivery evidence")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateIssueDeliveryCriteria(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "delivery criteria require a human")
		return
	}
	issue, userID, ok := h.loadIssueForDelivery(w, r)
	if !ok {
		return
	}
	var req struct {
		Criteria         *[]string `json:"criteria"`
		ExpectedRevision *int32    `json:"expected_revision"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil || req.Criteria == nil || req.ExpectedRevision == nil || *req.ExpectedRevision < 0 || len(*req.Criteria) > 20 {
		writeError(w, http.StatusBadRequest, "criteria (up to 20) and expected_revision are required")
		return
	}
	criteria := make([]string, 0, len(*req.Criteria))
	for _, raw := range *req.Criteria {
		criterion := strings.TrimSpace(util.SanitizeTextForPostgres(raw))
		if criterion == "" || utf8.RuneCountInString(criterion) > 500 || strings.ContainsAny(criterion, "\r\n") {
			writeError(w, http.StatusBadRequest, "each criterion must be one non-empty line of at most 500 characters")
			return
		}
		criteria = append(criteria, criterion)
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save criteria")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if _, err = q.LockIssueForDescriptionUpdate(r.Context(), db.LockIssueForDescriptionUpdateParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID}); err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	contract, err := q.GetIssueDeliveryContract(r.Context(), db.GetIssueDeliveryContractParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to read criteria")
		return
	}
	if contract.Revision != *req.ExpectedRevision {
		writeError(w, http.StatusConflict, "criteria changed; reload before saving")
		return
	}
	encoded, err := json.Marshal(criteria)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode criteria")
		return
	}
	_, err = q.SaveIssueDeliveryContract(r.Context(), db.SaveIssueDeliveryContractParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, Criteria: encoded, UpdatedBy: parseUUID(userID)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save criteria")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit criteria")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"criteria": criteria, "revision": contract.Revision + 1})
	h.publish(protocol.EventDeliveryChanged, uuidToString(issue.WorkspaceID), "member", userID, map[string]any{"issue_id": uuidToString(issue.ID)})
}

type ReviewIssueDeliveryRequest struct {
	ReviewID           string               `json:"review_id"`
	ExpectedReviewID   *string              `json:"expected_review_id"`
	SnapshotToken      string               `json:"snapshot_token"`
	Decision           string               `json:"decision"`
	Feedback           string               `json:"feedback"`
	Assessments        []DeliveryAssessment `json:"assessments"`
	// HumanEffortSeconds is client-timed active review work. Optional; omitted
	// on older clients. Distinct from review_delay_seconds (wall clock since run end).
	HumanEffortSeconds *int64 `json:"human_effort_seconds"`
}

func (h *Handler) ReviewIssueDelivery(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "delivery acceptance requires a human")
		return
	}
	issue, userID, ok := h.loadIssueForDelivery(w, r)
	if !ok {
		return
	}
	var req ReviewIssueDeliveryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id, ok := parseUUIDOrBadRequest(w, req.ReviewID, "review_id")
	if !ok {
		return
	}
	req.Feedback = strings.TrimSpace(util.SanitizeTextForPostgres(req.Feedback))
	if req.ExpectedReviewID == nil || len(req.SnapshotToken) != 64 || len(req.Assessments) > 20 || utf8.RuneCountInString(req.Feedback) > 4000 ||
		(req.Decision != "accepted" && req.Decision != "changes_requested") || (req.Decision == "changes_requested" && req.Feedback == "") {
		writeError(w, http.StatusBadRequest, "a decision, snapshot_token, expected_review_id and correction feedback are required")
		return
	}
	for i := range req.Assessments {
		a := &req.Assessments[i]
		a.Evidence = strings.TrimSpace(util.SanitizeTextForPostgres(a.Evidence))
		if utf8.RuneCountInString(a.Evidence) > 2000 || (req.Decision == "accepted" && (!a.Passed || a.Evidence == "")) {
			writeError(w, http.StatusBadRequest, "acceptance requires a passed assessment and evidence for every criterion")
			return
		}
	}
	humanEffort := pgtype.Int8{}
	if req.HumanEffortSeconds != nil {
		if *req.HumanEffortSeconds < 0 || *req.HumanEffortSeconds > 24*60*60 {
			writeError(w, http.StatusBadRequest, "human_effort_seconds must be between 0 and 86400")
			return
		}
		humanEffort = pgtype.Int8{Int64: *req.HumanEffortSeconds, Valid: true}
	}
	// Effort is observational metadata; exclude it from the idempotency hash so a
	// ±1s timer drift on retry does not conflict a completed decision.
	hashReq := req
	hashReq.HumanEffortSeconds = nil
	inputHash, err := deliveryHash(hashReq)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid review")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save review")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	issue, err = q.LockIssueForDescriptionUpdate(r.Context(), db.LockIssueForDescriptionUpdateParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	existing, err := q.GetIssueDeliveryReview(r.Context(), db.GetIssueDeliveryReviewParams{ID: id, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err == nil {
		if existing.InputHash != inputHash || uuidToString(existing.ReviewedBy) != userID {
			writeError(w, http.StatusConflict, "review_id already used")
			return
		}
		resp, err := deliveryReviewResponse(existing)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read review")
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to read review")
		return
	}
	delivery, err := h.issueDelivery(r.Context(), q, issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read delivery evidence")
		return
	}
	latestID := ""
	if delivery.LatestReview != nil {
		latestID = delivery.LatestReview.ID
	}
	if delivery.SnapshotToken != req.SnapshotToken || latestID != *req.ExpectedReviewID {
		writeError(w, http.StatusConflict, "delivery or human review changed; reload before deciding")
		return
	}
	if delivery.Run == nil || delivery.Run.Status != "completed" || delivery.Run.CompletedAt == nil || len(delivery.Criteria) == 0 || len(req.Assessments) != len(delivery.Criteria) {
		writeError(w, http.StatusConflict, "review requires the latest completed run and an assessment for each criterion")
		return
	}
	snapshot, err := json.Marshal(delivery.DeliverySnapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode evidence")
		return
	}
	assessments, err := json.Marshal(req.Assessments)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode assessments")
		return
	}
	usageRows, err := q.ListIssueDeliveryUsage(r.Context(), db.ListIssueDeliveryUsageParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to capture delivery usage")
		return
	}
	usageSnapshot, err := json.Marshal(snapshotDeliveryUsage(usageRows, time.Now()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode delivery usage")
		return
	}
	created, err := q.CreateIssueDeliveryReview(r.Context(), db.CreateIssueDeliveryReviewParams{
		ID: id, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, TaskID: parseUUID(delivery.Run.ID), Decision: req.Decision,
		Feedback: req.Feedback, Assessments: assessments, Snapshot: snapshot, SnapshotToken: delivery.SnapshotToken,
		InputHash: inputHash, ReviewedBy: parseUUID(userID), UsageSnapshot: usageSnapshot, HumanEffortSeconds: humanEffort,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "review_id already used")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to save review")
		}
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit review")
		return
	}
	resp, err := deliveryReviewResponse(created)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read saved review")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
	h.publish(protocol.EventDeliveryChanged, uuidToString(issue.WorkspaceID), "member", userID, map[string]any{"issue_id": uuidToString(issue.ID)})
}

// StartIssueDeliveryCorrection creates at most one correction run per review.
// Recording feedback and authorizing another run are separate human actions;
// a failed launch must not discard the saved review.
func (h *Handler) StartIssueDeliveryCorrection(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "delivery correction requires a human")
		return
	}
	issue, userID, ok := h.loadIssueForDelivery(w, r)
	if !ok {
		return
	}
	var req struct {
		ReviewID string `json:"review_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	reviewID, ok := parseUUIDOrBadRequest(w, req.ReviewID, "review_id")
	if !ok {
		return
	}
	req.ReviewID = uuidToString(reviewID)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start correction")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	issue, err = q.LockIssueForDescriptionUpdate(r.Context(), db.LockIssueForDescriptionUpdateParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	review, err := q.GetIssueDeliveryReview(r.Context(), db.GetIssueDeliveryReviewParams{ID: reviewID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "review not found")
		return
	}
	if review.Decision != "changes_requested" {
		writeError(w, http.StatusConflict, "this review did not request corrections")
		return
	}
	// A retry after a lost response returns the durable receipt even after the
	// task completes or another review supersedes it. Never launch it twice.
	if review.CorrectionTaskID.Valid {
		writeJSON(w, http.StatusOK, map[string]string{"review_id": req.ReviewID, "task_id": uuidToString(review.CorrectionTaskID)})
		return
	}
	delivery, err := h.issueDelivery(r.Context(), q, issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read delivery")
		return
	}
	if delivery.LatestReview == nil || delivery.LatestReview.ID != req.ReviewID || delivery.SnapshotToken != review.SnapshotToken {
		writeError(w, http.StatusConflict, "delivery or review changed; review the current result before starting a correction")
		return
	}
	source, err := q.GetAgentTask(r.Context(), review.TaskID)
	if err != nil || source.IssueID != issue.ID || source.ChatSessionID.Valid || source.Status != "completed" {
		writeError(w, http.StatusConflict, "the reviewed run is no longer available")
		return
	}
	agent, err := q.GetAgent(r.Context(), source.AgentID)
	if err != nil || agent.WorkspaceID != issue.WorkspaceID || !h.canInvokeAgent(r.Context(), agent, "member", userID, userID, uuidToString(issue.WorkspaceID)) {
		writeError(w, http.StatusForbidden, "not allowed to invoke the agent that produced this delivery")
		return
	}
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "the delivery agent needs an available runtime before correction")
		return
	}
	// Include the immutable human review, not whatever the latest review becomes
	// by claim time. The ordinary claim path adds the current issue criteria.
	handoff := fmt.Sprintf("Human correction requested for delivery review %s, source run %s.\nResolve the feedback below and report evidence for each acceptance criterion. Do not mark the delivery accepted.\n\nFeedback:\n%s\n\nReviewed criteria:\n- %s\n\nCriterion assessments (same order):\n%s", req.ReviewID, uuidToString(source.ID), review.Feedback, strings.Join(delivery.Criteria, "\n- "), review.Assessments)
	task, err := h.TaskService.EnqueueIssueFollowupInTx(r.Context(), q, issue, source, parseUUID(userID), handoff)
	if err != nil {
		if errors.Is(err, service.ErrDuplicatePendingTask) {
			writeError(w, http.StatusConflict, "another run is already pending; review its result before starting a correction")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to queue correction; the review is saved")
		}
		return
	}
	if _, err := q.SetIssueDeliveryCorrectionTask(r.Context(), db.SetIssueDeliveryCorrectionTaskParams{ID: reviewID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, TaskID: task.ID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link correction")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit correction")
		return
	}
	h.TaskService.BroadcastTaskQueued(r.Context(), task)
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	h.publish(protocol.EventDeliveryChanged, uuidToString(issue.WorkspaceID), "member", userID, map[string]any{"issue_id": uuidToString(issue.ID)})
	writeJSON(w, http.StatusCreated, map[string]string{"review_id": req.ReviewID, "task_id": uuidToString(task.ID)})
}

// ListIssueDeliveryHistory keeps older results accessible without changing the
// current delivery. Cursor identity is scoped to the same issue and workspace.
func (h *Handler) ListIssueDeliveryHistory(w http.ResponseWriter, r *http.Request) {
	issue, _, ok := h.loadIssueForDelivery(w, r)
	if !ok {
		return
	}
	params := db.ListIssueDeliveryReviewPageParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID}
	if value := r.URL.Query().Get("before_id"); value != "" {
		id, ok := parseUUIDOrBadRequest(w, value, "before_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetIssueDeliveryReview(r.Context(), db.GetIssueDeliveryReviewParams{ID: id, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID}); err != nil {
			writeError(w, http.StatusNotFound, "review cursor not found")
			return
		}
		params.BeforeID = id
	}
	rows, err := h.Queries.ListIssueDeliveryReviewPage(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read review history")
		return
	}
	response := struct {
		Reviews      []DeliveryReview `json:"reviews"`
		NextBeforeID *string          `json:"next_before_id"`
	}{Reviews: []DeliveryReview{}}
	if len(rows) > 20 {
		rows = rows[:20]
		next := uuidToString(rows[19].ID)
		response.NextBeforeID = &next
	}
	for _, row := range rows {
		review, err := deliveryReviewResponse(row)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode review history")
			return
		}
		response.Reviews = append(response.Reviews, review)
	}
	writeJSON(w, http.StatusOK, response)
}
