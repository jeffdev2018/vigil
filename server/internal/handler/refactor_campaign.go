package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Sharded refactoring campaigns (K42). Execution is a fan-out (K38): one
// child issue per shard, each on its own branch, run in parallel. Merging
// is sequential: a shard whose run finished enters the queue once F10 says
// its branch is merge-ready; the queue takes the lowest position, gives its
// agent a rebase-and-merge run, and only moves to the next shard when that
// run finished. A conflict (a merge run that failed for good, or a shard
// run that failed) holds its position and files an inbox item; a human
// skips it to unblock the rest. The queue never merges two shards at once.

const (
	AuditCampaign           = "refactor_campaign"
	InboxTypeMergeConflict  = "merge_conflict_requires_human"
	campaignShardLead       = "This shard belongs to the refactoring campaign %q. Work on branch `%s`, created from `%s`, and open a pull request against `%s`. Do NOT merge it: the campaign's merge queue merges shards one at a time, in order."
	campaignMergeLead       = "Merge queue of the refactoring campaign %q: every shard before yours is merged into `%s`. Rebase branch `%s` onto the current `%s`, re-run the checks, push, and merge the pull request into `%s`. If the rebase hits a conflict you cannot resolve trivially, do not force anything: stop and fail this run with the conflicting files in your summary."
	ErrCodeCampaignActive   = "campaign_already_active"
	campaignMergeBlockerRun = "run_failed"
)

var campaignBranchUnsafe = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)

type CampaignShardRequest struct {
	Description string `json:"description"`
	AssigneeID  string `json:"assignee_id"`
	BranchName  string `json:"branch_name"`
}

type RefactorCampaignRequest struct {
	IssueID       string                 `json:"issue_id"`
	Name          string                 `json:"name"`
	TargetBranch  string                 `json:"target_branch"`
	LeaderAgentID string                 `json:"leader_agent_id"`
	Shards        []CampaignShardRequest `json:"shards"`
}

type CampaignShardResponse struct {
	ID              string         `json:"id"`
	ChildIssueID    string         `json:"child_issue_id"`
	TaskID          string         `json:"task_id"`
	TaskStatus      string         `json:"task_status"`
	RunOutcome      *string        `json:"run_outcome"`
	AssigneeAgentID string         `json:"assignee_agent_id"`
	Description     string         `json:"description"`
	BranchName      string         `json:"branch_name"`
	MergePosition   int32          `json:"merge_position"`
	MergeStatus     string         `json:"merge_status"`
	MergeTaskID     *string        `json:"merge_task_id"`
	Blockers        []MergeBlocker `json:"blockers"`
	UpdatedAt       string         `json:"updated_at"`
}

type RefactorCampaignResponse struct {
	ID            string                  `json:"id"`
	IssueID       string                  `json:"issue_id"`
	FanoutBatchID string                  `json:"fanout_batch_id"`
	Name          string                  `json:"name"`
	TargetBranch  string                  `json:"target_branch"`
	Status        string                  `json:"status"`
	Shards        []CampaignShardResponse `json:"shards"`
	CreatedAt     string                  `json:"created_at"`
	CompletedAt   *string                 `json:"completed_at"`
}

func (h *Handler) campaignToResponse(ctx context.Context, c db.RefactorCampaign) RefactorCampaignResponse {
	out := RefactorCampaignResponse{ID: uuidToString(c.ID), IssueID: uuidToString(c.IssueID), FanoutBatchID: uuidToString(c.FanoutBatchID), Name: c.Name, TargetBranch: c.TargetBranch, Status: c.Status, Shards: []CampaignShardResponse{}, CreatedAt: timestampToString(c.CreatedAt), CompletedAt: timestampToPtr(c.CompletedAt)}
	rows, _ := h.Queries.ListCampaignShards(ctx, c.ID)
	for _, s := range rows {
		blockers := []MergeBlocker{}
		_ = json.Unmarshal(s.Blockers, &blockers)
		out.Shards = append(out.Shards, CampaignShardResponse{
			ID: uuidToString(s.ID), ChildIssueID: uuidToString(s.ChildIssueID), TaskID: uuidToString(s.TaskID), TaskStatus: s.TaskStatus, RunOutcome: textToPtr(s.RunOutcome), AssigneeAgentID: uuidToString(s.AssigneeAgentID),
			Description: s.Description, BranchName: s.BranchName, MergePosition: s.MergePosition, MergeStatus: s.MergeStatus, MergeTaskID: uuidToPtr(s.MergeTaskID), Blockers: blockers, UpdatedAt: timestampToString(s.UpdatedAt),
		})
	}
	return out
}

func campaignBranchName(name string, i int, custom string) string {
	if b := strings.TrimSpace(custom); b != "" {
		return b
	}
	slug := strings.Trim(campaignBranchUnsafe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "campaign"
	}
	return fmt.Sprintf("campaign/%s/shard-%d", slug, i+1)
}

// CreateRefactorCampaign: POST /api/refactor-campaigns.
func (h *Handler) CreateRefactorCampaign(w http.ResponseWriter, r *http.Request) {
	var req RefactorCampaignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name, req.TargetBranch = strings.TrimSpace(req.Name), strings.TrimSpace(req.TargetBranch)
	if req.Name == "" || req.TargetBranch == "" {
		writeError(w, http.StatusBadRequest, "name and target_branch are required")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, req.IssueID)
	if !ok {
		return
	}
	leaderID, ok := parseUUIDOrBadRequest(w, req.LeaderAgentID, "leader_agent_id")
	if !ok {
		return
	}
	leader, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: leaderID, WorkspaceID: issue.WorkspaceID})
	if err != nil || leader.ArchivedAt.Valid {
		writeError(w, http.StatusUnprocessableEntity, "leader agent not found in this workspace")
		return
	}
	if c, err := h.Queries.GetLatestRefactorCampaignForIssue(r.Context(), issue.ID); err == nil && (c.Status == "running" || c.Status == "merging") {
		writeErrorCode(w, http.StatusConflict, ErrCodeCampaignActive, "a campaign is already running on this issue")
		return
	}
	reqs := make([]FanoutSubTaskRequest, 0, len(req.Shards))
	branches := make([]string, 0, len(req.Shards))
	for i, s := range req.Shards {
		branch := campaignBranchName(req.Name, i, s.BranchName)
		branches = append(branches, branch)
		reqs = append(reqs, FanoutSubTaskRequest{Description: strings.TrimSpace(s.Description) + "\n\n" + fmt.Sprintf(campaignShardLead, req.Name, branch, req.TargetBranch, req.TargetBranch), AssigneeID: s.AssigneeID})
	}
	subs, ok := h.parseFanoutSubTasks(w, r, issue, reqs)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	userID := parseUUID(requestUserID(r))
	batch, members, status, msg := h.launchFanout(r.Context(), issue, leader, subs, actorType, actorID, userID)
	if status != 0 {
		if msg == ErrCodeFanoutActive {
			writeErrorCode(w, status, msg, "a fan-out is already running on this issue")
			return
		}
		writeError(w, status, msg)
		return
	}
	c, err := h.Queries.CreateRefactorCampaign(r.Context(), db.CreateRefactorCampaignParams{ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, FanoutBatchID: batch.ID, Name: req.Name, TargetBranch: req.TargetBranch, StartedBy: userID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the campaign")
		return
	}
	for i, m := range members {
		if _, err := h.Queries.AddCampaignShard(r.Context(), db.AddCampaignShardParams{ID: dbid.NewV7(), RefactorCampaignID: c.ID, WorkspaceID: issue.WorkspaceID, FanoutMemberID: m.ID, ChildIssueID: m.ChildIssueID, TaskID: m.TaskID, AssigneeAgentID: m.AssigneeAgentID, Description: strings.TrimSpace(req.Shards[i].Description), BranchName: branches[i], MergePosition: int32(i)}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record a shard")
			return
		}
	}
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditCampaign, "issue", issue.ID, map[string]any{"campaign_id": uuidToString(c.ID), "target_branch": req.TargetBranch, "shards": len(members), "started": true}, nil)
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	writeJSON(w, http.StatusCreated, map[string]any{"campaign": h.campaignToResponse(r.Context(), c)})
}

// GetRefactorCampaign: GET /api/refactor-campaigns/{id}. Reading advances
// the queue first, so a shard whose checks went green since the last run
// event is picked up without a webhook hook.
// ponytail: readiness re-evaluated on read (the board polls); move to a
// sweeper if reads get expensive.
func (h *Handler) GetRefactorCampaign(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "campaign id")
	if !ok {
		return
	}
	c, err := h.Queries.GetRefactorCampaign(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "campaign not found")
		return
	}
	if _, ok := h.loadIssueForUser(w, r, uuidToString(c.IssueID)); !ok {
		return
	}
	c = h.advanceMergeQueue(r.Context(), c)
	writeJSON(w, http.StatusOK, map[string]any{"campaign": h.campaignToResponse(r.Context(), c)})
}

// GetIssueRefactorCampaign: GET /api/issues/{id}/refactor-campaign — the latest one.
func (h *Handler) GetIssueRefactorCampaign(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	c, err := h.Queries.GetLatestRefactorCampaignForIssue(r.Context(), issue.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"campaign": nil})
		return
	}
	c = h.advanceMergeQueue(r.Context(), c)
	writeJSON(w, http.StatusOK, map[string]any{"campaign": h.campaignToResponse(r.Context(), c)})
}

// SkipCampaignShard: POST /api/campaign-shards/{id}/skip — takes a shard
// out of the queue (typically a conflict) so the ones behind it proceed.
func (h *Handler) SkipCampaignShard(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "shard id")
	if !ok {
		return
	}
	shard, err := h.Queries.GetCampaignShard(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "shard not found")
		return
	}
	c, err := h.Queries.GetRefactorCampaign(r.Context(), shard.RefactorCampaignID)
	if err != nil {
		writeError(w, http.StatusNotFound, "campaign not found")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, uuidToString(c.IssueID))
	if !ok {
		return
	}
	if shard.MergeStatus == "merged" || shard.MergeStatus == "skipped" || shard.MergeStatus == "rebasing" {
		writeError(w, http.StatusConflict, "a "+shard.MergeStatus+" shard cannot be skipped")
		return
	}
	if _, err := h.Queries.SetCampaignShardMergeStatus(r.Context(), db.SetCampaignShardMergeStatusParams{ID: shard.ID, MergeStatus: "skipped", Blockers: shard.Blockers}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to skip the shard")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditCampaign, "issue", issue.ID, map[string]any{"campaign_id": uuidToString(c.ID), "shard_id": uuidToString(shard.ID), "skipped": true}, nil)
	c = h.advanceMergeQueue(r.Context(), c)
	writeJSON(w, http.StatusOK, map[string]any{"campaign": h.campaignToResponse(r.Context(), c)})
}

// onCampaignShardSettled runs when a fan-out member settled: a completed
// shard run is evaluated for the queue, a failed one becomes a conflict.
func (h *Handler) onCampaignShardSettled(ctx context.Context, member db.FanoutBatchMember, outcome string) {
	shard, err := h.Queries.GetCampaignShardForFanoutMember(ctx, member.ID)
	if err != nil || shard.MergeStatus != "pending" {
		return
	}
	c, err := h.Queries.GetRefactorCampaign(ctx, shard.RefactorCampaignID)
	if err != nil {
		return
	}
	if outcome == "failed" {
		h.campaignConflict(ctx, c, shard, []MergeBlocker{{Kind: campaignMergeBlockerRun, Label: "The shard's run failed for good"}})
		return
	}
	h.evaluateShardReadiness(ctx, shard)
	h.advanceMergeQueue(ctx, c)
}

// evaluateShardReadiness applies F10 to the shard's child issue: ready
// enters the queue, otherwise the blockers are recorded for the board.
func (h *Handler) evaluateShardReadiness(ctx context.Context, shard db.CampaignShard) db.CampaignShard {
	child, err := h.Queries.GetIssue(ctx, shard.ChildIssueID)
	if err != nil {
		return shard
	}
	readiness, err := h.mergeReadinessFor(ctx, child)
	if err != nil {
		slog.Warn("campaign: readiness failed", "shard_id", uuidToString(shard.ID), "error", err)
		return shard
	}
	status := "pending"
	if readiness.Ready {
		status = "ready"
	}
	raw, _ := json.Marshal(readiness.Blockers)
	if updated, err := h.Queries.SetCampaignShardMergeStatus(ctx, db.SetCampaignShardMergeStatusParams{ID: shard.ID, MergeStatus: status, Blockers: raw}); err == nil {
		return updated
	}
	return shard
}

// advanceMergeQueue moves the campaign forward: nothing while a merge run
// is in flight; the head shard (lowest unmerged, unskipped position) gets
// its merge run when ready, holds the queue when in conflict; no head left
// completes the campaign.
func (h *Handler) advanceMergeQueue(ctx context.Context, c db.RefactorCampaign) db.RefactorCampaign {
	if c.Status == "completed" || c.Status == "failed" {
		return c
	}
	shards, err := h.Queries.ListCampaignShards(ctx, c.ID)
	if err != nil {
		return c
	}
	var head *db.ListCampaignShardsRow
	for i := range shards {
		switch shards[i].MergeStatus {
		case "rebasing":
			return c // one shard at a time
		case "merged", "skipped":
			continue
		}
		if head == nil {
			head = &shards[i]
		}
	}
	if head == nil {
		if updated, err := h.Queries.SetRefactorCampaignStatus(ctx, db.SetRefactorCampaignStatusParams{ID: c.ID, Status: "completed"}); err == nil {
			c = updated
		}
		h.audit(ctx, c.WorkspaceID, "system", "", AuditCampaign, "issue", c.IssueID, map[string]any{"campaign_id": uuidToString(c.ID), "status": "completed"}, nil)
		h.publishCampaignProgress(ctx, c)
		return c
	}
	if head.MergeStatus == "conflict" {
		return c
	}
	shard := db.CampaignShard{ID: head.ID, RefactorCampaignID: head.RefactorCampaignID, WorkspaceID: head.WorkspaceID, FanoutMemberID: head.FanoutMemberID, ChildIssueID: head.ChildIssueID, TaskID: head.TaskID, AssigneeAgentID: head.AssigneeAgentID, Description: head.Description, BranchName: head.BranchName, MergePosition: head.MergePosition, MergeStatus: head.MergeStatus, MergeTaskID: head.MergeTaskID, Blockers: head.Blockers, UpdatedAt: head.UpdatedAt}
	if shard.MergeStatus == "pending" {
		if !head.RunOutcome.Valid {
			return c // still executing
		}
		if shard = h.evaluateShardReadiness(ctx, shard); shard.MergeStatus != "ready" {
			h.publishCampaignProgress(ctx, c)
			return c
		}
	}
	child, err := h.Queries.GetIssue(ctx, shard.ChildIssueID)
	if err != nil {
		return c
	}
	// Merge through the platform first (K42 debt); an agent run only when
	// the platform cannot do it.
	switch outcome, detail := h.mergeShardViaAPI(ctx, child); outcome {
	case mergeOutcomeMerged:
		if _, err := h.Queries.SetCampaignShardMergeStatus(ctx, db.SetCampaignShardMergeStatusParams{ID: shard.ID, MergeStatus: "merged", Blockers: []byte("[]")}); err != nil {
			return c
		}
		if c.Status != "merging" {
			if updated, err := h.Queries.SetRefactorCampaignStatus(ctx, db.SetRefactorCampaignStatusParams{ID: c.ID, Status: "merging"}); err == nil {
				c = updated
			}
		}
		h.audit(ctx, c.WorkspaceID, "system", "", AuditCampaign, "issue", c.IssueID, map[string]any{"campaign_id": uuidToString(c.ID), "shard_id": uuidToString(shard.ID), "merged": true, "via": "api"}, nil)
		h.publishCampaignProgress(ctx, c)
		return h.advanceMergeQueue(ctx, c)
	case mergeOutcomeConflict:
		h.campaignConflict(ctx, c, shard, []MergeBlocker{{Kind: blockerMergeConflict, Label: "The platform refused to merge: " + detail}})
		return c
	}
	note := fmt.Sprintf(campaignMergeLead, c.Name, c.TargetBranch, shard.BranchName, c.TargetBranch, c.TargetBranch)
	task, err := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, child, note, c.StartedBy)
	if err != nil {
		slog.Warn("campaign: merge run enqueue failed", "shard_id", uuidToString(shard.ID), "error", err)
		return c
	}
	// Per-leg accounting (JEF-274): the shard's merge run belongs to the
	// campaign, not to another run, so it is its own root.
	if _, serr := h.TaskService.StampLeg(ctx, task, service.LegRoleShard, db.AgentTaskQueue{}); serr != nil {
		slog.Warn("campaign: stamp leg failed", "task_id", uuidToString(task.ID), "error", serr)
	}
	if _, err := h.Queries.SetCampaignShardMergeStatus(ctx, db.SetCampaignShardMergeStatusParams{ID: shard.ID, MergeStatus: "rebasing", MergeTaskID: task.ID, Blockers: []byte("[]")}); err != nil {
		return c
	}
	if c.Status != "merging" {
		if updated, err := h.Queries.SetRefactorCampaignStatus(ctx, db.SetRefactorCampaignStatusParams{ID: c.ID, Status: "merging"}); err == nil {
			c = updated
		}
	}
	h.audit(ctx, c.WorkspaceID, "system", "", AuditCampaign, "issue", c.IssueID, map[string]any{"campaign_id": uuidToString(c.ID), "shard_id": uuidToString(shard.ID), "merge_task_id": uuidToString(task.ID), "rebasing": true}, nil)
	h.publishCampaignProgress(ctx, c)
	return c
}

// updateCampaignMergeRun is called when a run reaches a terminal status:
// a finished merge run merges its shard, one that failed for good is a
// conflict. Either way the queue moves on (a conflict holds it).
func (h *Handler) updateCampaignMergeRun(ctx context.Context, task db.AgentTaskQueue) {
	root := task.ID
	if task.RetryOfTaskID.Valid {
		root = task.RetryOfTaskID
	} else if task.ParentTaskID.Valid {
		root = task.ParentTaskID
	}
	shard, err := h.Queries.GetRebasingCampaignShardForTask(ctx, db.GetRebasingCampaignShardForTaskParams{TaskID: task.ID, RootTaskID: root})
	if err != nil {
		return
	}
	c, err := h.Queries.GetRefactorCampaign(ctx, shard.RefactorCampaignID)
	if err != nil {
		return
	}
	switch task.Status {
	case "completed":
		if _, err := h.Queries.SetCampaignShardMergeStatus(ctx, db.SetCampaignShardMergeStatusParams{ID: shard.ID, MergeStatus: "merged", Blockers: []byte("[]")}); err != nil {
			return
		}
		h.audit(ctx, c.WorkspaceID, "system", "", AuditCampaign, "issue", c.IssueID, map[string]any{"campaign_id": uuidToString(c.ID), "shard_id": uuidToString(shard.ID), "merged": true}, nil)
	case "failed", "cancelled":
		if more, err := h.Queries.HasRunnableSuccessorForTask(ctx, task.ID); err == nil && more {
			return // a retry is coming
		}
		h.campaignConflict(ctx, c, shard, []MergeBlocker{{Kind: blockerMergeConflict, Label: "The rebase-and-merge run failed: " + task.Error.String}})
		return
	default:
		return
	}
	h.advanceMergeQueue(ctx, c)
}

// campaignConflict parks the shard and asks a human (inbox) to resolve or skip it.
func (h *Handler) campaignConflict(ctx context.Context, c db.RefactorCampaign, shard db.CampaignShard, blockers []MergeBlocker) {
	raw, _ := json.Marshal(blockers)
	if _, err := h.Queries.SetCampaignShardMergeStatus(ctx, db.SetCampaignShardMergeStatusParams{ID: shard.ID, MergeStatus: "conflict", Blockers: raw}); err != nil {
		return
	}
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, c.WorkspaceID)
	if err != nil {
		slog.Warn("campaign: list recipients failed", "error", err)
	}
	details, _ := json.Marshal(map[string]any{"campaign_id": uuidToString(c.ID), "shard_id": uuidToString(shard.ID), "branch_name": shard.BranchName, "blockers": blockers})
	for _, rcpt := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: c.WorkspaceID, RecipientType: rcpt.Type, RecipientID: rcpt.ID, Type: InboxTypeMergeConflict, Severity: "action_required",
			IssueID: shard.ChildIssueID, Title: fmt.Sprintf("Merge conflict on shard %d of %q", shard.MergePosition+1, c.Name),
			Body:      pgtype.Text{String: "Resolve the conflict on branch " + shard.BranchName + " or skip the shard so the queue moves on.", Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(c.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
	}
	h.audit(ctx, c.WorkspaceID, "system", "", AuditCampaign, "issue", c.IssueID, map[string]any{"campaign_id": uuidToString(c.ID), "shard_id": uuidToString(shard.ID), "conflict": true}, nil)
	h.publishCampaignProgress(ctx, c)
}

func (h *Handler) publishCampaignProgress(ctx context.Context, c db.RefactorCampaign) {
	h.publish("campaign:merge-progress", uuidToString(c.WorkspaceID), "system", "", map[string]any{"issue_id": uuidToString(c.IssueID), "campaign_id": uuidToString(c.ID), "status": c.Status})
}

// AdvanceCampaignMergeQueues is the scheduler's entry (K42): every active
// campaign gets its queue re-evaluated. Returns how many campaigns moved
// out of the running/merging states or started a merge.
func (h *Handler) AdvanceCampaignMergeQueues(ctx context.Context) (int, error) {
	campaigns, err := h.Queries.ListActiveRefactorCampaigns(ctx)
	if err != nil {
		return 0, fmt.Errorf("list campaigns: %w", err)
	}
	moved := 0
	for _, c := range campaigns {
		before, _ := h.Queries.ListCampaignShards(ctx, c.ID)
		after := h.advanceMergeQueue(ctx, c)
		shards, _ := h.Queries.ListCampaignShards(ctx, c.ID)
		if after.Status != c.Status || len(shards) != len(before) {
			moved++
			continue
		}
		for i := range shards {
			if i < len(before) && shards[i].MergeStatus != before[i].MergeStatus {
				moved++
				break
			}
		}
	}
	return moved, nil
}
