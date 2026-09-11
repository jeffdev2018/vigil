package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Agent-to-agent messaging (F19 / JEF-32).
//
// An agent could already wake another agent, by writing a comment containing
// mention markup and letting the comment pipeline enqueue the run. That is the
// protocol a squad leader is instructed to follow. What it lacked was three
// things, and this endpoint adds exactly those three and nothing else:
//
//   - A DECLARED INTENT. "@Bob can you look at this" is indistinguishable from
//     discussion. `a2a_intent` says question / review / handoff, so the thread
//     reads as a protocol and the UI can chip it.
//   - A DEPTH. Every hop lands one step further from the human who started the
//     chain; past MaxA2ADepth the message is refused.
//   - A BUDGET. A per-issue hourly rate, the only net under a loop that leaves
//     the issue and comes back through a human trigger with the depth reset.
//
// WHAT IT IS NOT:
//
//   - It is NOT a new dispatch path. It composes the same mention markup, posts
//     the same ordinary comment, and hands off to triggerTasksForComment.
//     Permission, attribution, squad routing, dedup, realtime, inbox and
//     rendering are inherited, not reimplemented — the MUL-3375 lesson about
//     four drifting copies of one trigger decision applies here more than
//     anywhere, since a private channel between agents is exactly the thing the
//     invoke gate exists to prevent.
//   - It is NOT a permission model. canInvokeAgent is called unchanged, with the
//     same arguments POST /comments passes. The calling agent is never the
//     principal: the human at the head of the chain is (originator_user_id), and
//     routing around that would be a privilege escalation dressed as a feature.
//   - It is NOT a message store. There is no message table and no private
//     channel: the carrier is the comment, so a human reading the issue sees the
//     agents' conversation by construction.
//   - It is NOT synchronous. A sender that asks a question finishes its run; the
//     answer arrives as a new comment that wakes it again, if it is woken at all.
//
// Refusal contract:
//
//	400 invalid_request     — unknown intent, empty body, malformed to_agent_id,
//	                          addressing yourself, or a non-agent caller.
//	403 invocation_not_allowed — the head-of-chain human may not invoke the
//	                          recipient, OR the recipient does not exist here.
//	                          Deliberately the same answer: distinguishing them
//	                          would enumerate private agents.
//	409                     — the calling run is already terminal.
//	429 a2a_depth_exceeded  — the resulting run would sit deeper than MaxA2ADepth.
//	429 a2a_budget_exceeded — the issue is over its A2A rate for the window.
//
// A refused message writes NO comment. The checks are ordered cheapest-first and
// caller-facts-first: depth and budget are answered from the caller's own run and
// its own issue — facts it already holds — so a runaway fan-out is cut with one
// indexed read per hop instead of a full permission resolution.

// AgentMessageRequest is the wire body of POST /issues/{id}/agent-messages.
//
// There is deliberately no `content`/`type`/`parent_id` here. This is not a
// second comment endpoint: the server composes the markup so that the intent
// and the mention it declares can never disagree.
type AgentMessageRequest struct {
	ToAgentID string `json:"to_agent_id"`
	Intent    string `json:"intent"`
	Body      string `json:"body"`
}

// SendAgentMessage posts one agent-to-agent message on an issue.
func (h *Handler) SendAgentMessage(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// Same write gate as POST /comments: this writes a comment on that issue,
	// so a viewer-role project must refuse it for the same reason.
	if !h.requireProjectWrite(w, r, issue.ProjectID) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)

	var req AgentMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Intent = strings.TrimSpace(req.Intent)
	if !service.IsValidA2AIntent(req.Intent) {
		writeError(w, http.StatusBadRequest, "intent must be one of: "+strings.Join(service.A2AIntents, ", "))
		return
	}
	body := strings.TrimSpace(sanitizeNullBytes(req.Body))
	if body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	toAgentID, ok := parseUUIDOrBadRequest(w, req.ToAgentID, "to_agent_id")
	if !ok {
		return
	}

	// The sender is an AGENT, identified by the server-stamped task token
	// headers. A member is refused rather than quietly accepted: a2a_depth > 0
	// is what the per-issue budget counts, so letting a human mint a depth-1 run
	// would charge ordinary human traffic to the agent-loop breaker and break
	// "a human-triggered run always has depth 0". A member wanting the same run
	// posts the mention through /comments, which is what this endpoint composes
	// anyway — they just do not get the intent marker.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "agent" {
		writeError(w, http.StatusBadRequest, "agent-messages is for agent callers; post a comment to mention an agent")
		return
	}
	callerTask, hasTask := h.taskFromRequestHeader(r)
	if !hasTask {
		writeError(w, http.StatusBadRequest, "agent-messages requires the calling run (X-Task-ID)")
		return
	}
	// A finished run lends nothing. invokeOriginatorFromRequest already returns
	// "" for a terminal task, which denies a PRIVATE recipient — but a
	// workspace-public recipient still admits a workspace-internal agent
	// principal, so the gate alone would let a completed run keep sending. Say
	// no here, where the caller's own run state is the whole question, and say
	// it in plain words: nothing about the recipient is revealed by telling a
	// caller its own run is over.
	if isTerminalTaskStatus(callerTask.Status) {
		writeError(w, http.StatusConflict, "the calling run is no longer active; agent messages can only be sent from a live run")
		return
	}
	if uuidToString(callerTask.AgentID) == uuidToString(toAgentID) {
		writeError(w, http.StatusBadRequest, "an agent cannot send an agent message to itself")
		return
	}

	// --- Circuit breakers -------------------------------------------------
	// Shared with the mention-link hand-off path (a2a_breaker.go) so a future
	// change to depth/window/threshold logic cannot land in one path and miss
	// the other, which is exactly how this endpoint went unguarded before.
	if reason := h.a2aBreakerBlocked(r.Context(), issue.ID, callerTask.A2aDepth+1); reason != "" {
		h.writeDispatchBlocked(w, http.StatusTooManyRequests, reason)
		return
	}

	// --- Permission -------------------------------------------------------
	// One gate, the same one POST /comments uses, judged by the human at the
	// head of the chain. A recipient that does not exist in this workspace and a
	// recipient this human may not invoke get the IDENTICAL answer: telling them
	// apart would let any agent enumerate private agents by id.
	recipient, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          toAgentID,
		WorkspaceID: issue.WorkspaceID,
	})
	originatorUserID := h.invokeOriginatorFromRequest(r, actorType, actorID)
	if err != nil || !h.canInvokeAgent(r.Context(), recipient, actorType, actorID, originatorUserID, workspaceID) {
		slog.Info("a2a message refused: recipient not invocable", append(logger.RequestAttrs(r),
			"issue_id", uuidToString(issue.ID),
			"from_agent_id", uuidToString(callerTask.AgentID),
			"to_agent_id", uuidToString(toAgentID),
		)...)
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}

	// --- Write ------------------------------------------------------------
	// The mention markup is composed here, never taken from the caller, so the
	// declared intent and the agent it addresses cannot disagree.
	content := sanitizeNullBytes(fmt.Sprintf("[@%s](mention://agent/%s)\n\n%s", recipient.Name, uuidToString(recipient.ID), body))
	created, err := h.Queries.CreateComment(r.Context(), db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  actorType,
		AuthorID:    parseUUID(actorID),
		Content:     content,
		// Deliberately an ordinary comment type. The intent rides on a2a_intent,
		// which no client can set — `type` is client-supplied on POST /comments,
		// so a dedicated type value would be forgeable, and adding one to
		// comment_type_check would have cost an ACCESS EXCLUSIVE rebuild on one
		// of the hottest tables in the product (migration 828).
		Type:      "comment",
		A2aIntent: pgtype.Text{String: req.Intent, Valid: true},
		// The sending run. This is what the enqueue reads back to compute the
		// recipient run's a2a_depth, and what keeps the originator chain intact
		// across the hop.
		SourceTaskID: callerTask.ID,
	})
	if err != nil {
		slog.Warn("a2a message comment create failed", append(logger.RequestAttrs(r),
			"error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to send agent message")
		return
	}
	comment := created.Comment()

	resp := commentToResponse(comment, nil, nil)
	resp.IssueRevision = created.IssueRevision
	// Why search (K55) and the undo ledger (K69) treat this like any other
	// agent-written comment, because that is what it is.
	h.indexWhy(r.Context(), comment.WorkspaceID, whySourceComment, comment.ID, comment.IssueID, comment.Content)
	h.recordEffect(r, comment.WorkspaceID, comment.IssueID, service.EffectCommentCreate, "comment", comment.ID,
		map[string]any{}, map[string]any{"type": comment.Type, "excerpt": truncate(comment.Content, 200)}, true)
	h.publish(protocol.EventCommentCreated, workspaceID, actorType, actorID, map[string]any{
		"comment":             resp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
		"issue_revision":      created.IssueRevision,
	})

	// The comment is saved; a blocked trigger must not fail the request. The
	// per-target outcome is reported the same way POST /comments reports it, so
	// the CLI reuses one result handler.
	resp.TriggerOutcomes = h.triggerTasksForComment(r.Context(), issue, comment, nil, actorType, actorID, originatorUserID, nil)

	slog.Info("a2a message sent", append(logger.RequestAttrs(r),
		"issue_id", uuidToString(issue.ID),
		"comment_id", uuidToString(comment.ID),
		"from_agent_id", uuidToString(callerTask.AgentID),
		"to_agent_id", uuidToString(recipient.ID),
		"intent", req.Intent,
		"depth", callerTask.A2aDepth+1,
	)...)
	writeJSON(w, http.StatusCreated, resp)
}
