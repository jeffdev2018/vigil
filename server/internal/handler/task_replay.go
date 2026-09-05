package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// Run replay (K70): one ordered event stream per run, merged at read time
// from the tables that already hold the run — task messages (text, thinking,
// tool calls, steers), effects (K69), decisions, handoff packets (K17),
// checkpoints (K20), usage and the run's audit entries — with a hash chain
// over the events. When the run ends the chain head is sealed into the
// audit log (itself hash-chained, K08); a later read recomputes the chain
// and reports whether the sealed prefix still matches. Events that happen
// after the seal (an answered decision, an undone effect) append after it.
//
// The trace is the proof; the agent's narration is one event kind among
// others, never privileged.

const (
	AuditRunSealed            = "run.sealed"
	AuditRunResumedFromReplay = "run.resumed_from_replay"
	AuditRunStarted           = "run.started"
	AuditRunReplayedSafe      = "run.replayed_safe"
	replayPageMax             = 500
)

// Data classes a replay event can carry. Confidential means the server
// redacted a secret out of it; the clear value stays only in the sources.
const (
	replayClassInternal     = "internal"
	replayClassConfidential = "confidential"
)

type ReplayActor struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// ReplayEvent is one instant of the run. Data is the machine channel, Text
// the human one (dual channel); both are hashed.
type ReplayEvent struct {
	Seq   int            `json:"seq"`
	At    time.Time      `json:"at"`
	Kind  string         `json:"kind"`
	Actor ReplayActor    `json:"actor"`
	Title string         `json:"title"`
	Text  string         `json:"text,omitempty"`
	Data  map[string]any `json:"data"`
	// DataClass is internal, or confidential when a secret was redacted.
	DataClass string `json:"data_class"`
	// InPlan is set on tool calls when the run had a plan: false = drift.
	InPlan   *bool  `json:"in_plan,omitempty"`
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
	PrevHash string `json:"prev_hash"`
	Hash     string `json:"hash"`
}

type ReplayLink struct {
	Relation  string `json:"relation"`
	TaskID    string `json:"task_id"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
}

// ReplaySnapshot is what the run started with, recorded at StartTask.
type ReplaySnapshot struct {
	TrustMode           string `json:"trust_mode"`
	EffectMode          string `json:"effect_mode"`
	Model               string `json:"model"`
	ThinkingLevel       string `json:"thinking_level"`
	PermissionProfileID string `json:"permission_profile_id"`
	RuntimeID           string `json:"runtime_id"`
	SafeMode            bool   `json:"safe_mode"`
	PlanVersion         int32  `json:"plan_version"`
	RecordedAt          string `json:"recorded_at"`
}

type ReplayPlan struct {
	Version int32 `json:"version"`
	Steps   int   `json:"steps"`
}

type ReplayRun struct {
	ID       string `json:"id"`
	SafeMode bool   `json:"safe_mode"`
	// Snapshot is nil for runs that started before snapshots existed.
	Snapshot *ReplaySnapshot `json:"snapshot"`
	// Plan is the plan the tool calls are compared against; nil = no plan, no drift flags.
	Plan        *ReplayPlan  `json:"plan"`
	Drift       int          `json:"drift"`
	IssueID     string       `json:"issue_id"`
	AgentID     string       `json:"agent_id"`
	AgentName   string       `json:"agent_name"`
	Status      string       `json:"status"`
	TrustMode   string       `json:"trust_mode"`
	EffectMode  string       `json:"effect_mode"`
	Model       string       `json:"model,omitempty"`
	RuntimeID   string       `json:"runtime_id,omitempty"`
	CreatedAt   *time.Time   `json:"created_at"`
	StartedAt   *time.Time   `json:"started_at"`
	CompletedAt *time.Time   `json:"completed_at"`
	Links       []ReplayLink `json:"links"`
}

type ReplayCost struct {
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CostUsdTicks *int64 `json:"cost_usd_ticks"`
}

type RunReplayResponse struct {
	Run        ReplayRun     `json:"run"`
	Events     []ReplayEvent `json:"events"`
	Total      int           `json:"total"`
	NextCursor *int          `json:"next_cursor"`
	HeadHash   string        `json:"head_hash"`
	Cost       ReplayCost    `json:"cost"`
	Sealed     *ReplaySeal   `json:"sealed"`
}

// ReplaySeal is what the audit log recorded when the run ended, and whether
// today's recomputation still agrees with it.
type ReplaySeal struct {
	Events   int       `json:"events"`
	HeadHash string    `json:"head_hash"`
	SealedAt time.Time `json:"sealed_at"`
	Verified bool      `json:"verified"`
}

// runReplayTask resolves a task the caller may read.
func (h *Handler) runReplayTask(w http.ResponseWriter, r *http.Request) (db.AgentTaskQueue, pgtype.UUID, bool) {
	taskUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task_id")
	if !ok {
		return db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	wsIDStr := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsIDStr == "" || wsIDStr != h.resolveWorkspaceID(r) {
		writeError(w, http.StatusNotFound, "task not found")
		return db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	return task, parseUUID(wsIDStr), true
}

// GetTaskReplay: GET /api/tasks/{taskId}/replay?cursor=N&limit=N
func (h *Handler) GetTaskReplay(w http.ResponseWriter, r *http.Request) {
	task, wsID, ok := h.runReplayTask(w, r)
	if !ok {
		return
	}
	cursor, limit := 0, 200
	if s := r.URL.Query().Get("cursor"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		cursor = n
	}
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(n, replayPageMax)
	}
	resp, err := h.buildRunReplay(r.Context(), task, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build the replay: "+err.Error())
		return
	}
	all := resp.Events
	if cursor > len(all) {
		cursor = len(all)
	}
	end := min(cursor+limit, len(all))
	resp.Events = all[cursor:end]
	if end < len(all) {
		next := end
		resp.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, resp)
}

// ResumeTaskReplay: POST /api/tasks/{taskId}/replay/resume {seq, instruction}
// starts a new run of the issue from a point of the replay: the handoff note
// carries the trace up to that instant and the new instruction.
func (h *Handler) ResumeTaskReplay(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	task, wsID, ok := h.runReplayTask(w, r)
	if !ok {
		return
	}
	var req struct {
		Seq         int    `json:"seq"`
		Instruction string `json:"instruction"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Instruction) == "" {
		writeError(w, http.StatusBadRequest, "instruction is required")
		return
	}
	if !task.IssueID.Valid {
		writeError(w, http.StatusBadRequest, "this run has no issue to resume on")
		return
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: task.IssueID, WorkspaceID: wsID})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	replay, err := h.buildRunReplay(r.Context(), task, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build the replay: "+err.Error())
		return
	}
	if req.Seq < 0 || req.Seq >= len(replay.Events) {
		writeError(w, http.StatusBadRequest, "seq is outside the replay")
		return
	}
	note := resumeNote(replay, req.Seq, req.Instruction)
	next, err := h.TaskService.EnqueueTaskForIssueWithHandoff(r.Context(), issue, note, parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start the resumed run: "+err.Error())
		return
	}
	h.audit(r.Context(), wsID, "member", userID, AuditRunResumedFromReplay, "task", task.ID, map[string]any{
		"from_seq": req.Seq, "from_hash": replay.Events[req.Seq].Hash, "new_task_id": uuidToString(next.ID), "instruction": truncate(req.Instruction, 500),
	}, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"task_id": uuidToString(next.ID), "from_seq": req.Seq})
}

// resumeNote is the handoff the resumed run reads first: where the replay
// was stopped, the last events before it, and the new instruction.
func resumeNote(replay RunReplayResponse, seq int, instruction string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Resumed from the replay of run %s at event %d/%d (%s).\n", replay.Run.ID, seq+1, replay.Total, replay.Events[seq].Kind)
	b.WriteString("Trace up to that point (newest last):\n")
	start := max(0, seq-7)
	for _, e := range replay.Events[start : seq+1] {
		line := e.Title
		if e.Text != "" {
			line += ": " + truncate(strings.ReplaceAll(e.Text, "\n", " "), 200)
		}
		fmt.Fprintf(&b, "- #%d %s %s\n", e.Seq+1, e.Kind, line)
	}
	b.WriteString("\nNew instruction from the human:\n" + strings.TrimSpace(instruction) + "\n")
	return b.String()
}

// sealRunReplay records the chain head in the audit log when a run ends.
func (h *Handler) sealRunReplay(ctx context.Context, task db.AgentTaskQueue) {
	wsIDStr := h.TaskService.ResolveTaskWorkspaceID(ctx, task)
	if wsIDStr == "" {
		return
	}
	wsID := parseUUID(wsIDStr)
	replay, err := h.buildRunReplay(ctx, task, wsID)
	if err != nil {
		return
	}
	h.audit(ctx, wsID, "system", "", AuditRunSealed, "task", task.ID, map[string]any{"events": replay.Total, "head_hash": replay.HeadHash}, nil)
}

// --- build ---------------------------------------------------------------

type replayCandidate struct {
	at       time.Time
	order    int // tie-break inside one instant: source rank, then native seq
	seq      int
	kind     string
	actor    ReplayActor
	title    string
	text     string
	data     map[string]any
	source   string
	srcID    string
	redacted bool
	inPlan   *bool
}

func (h *Handler) buildRunReplay(ctx context.Context, task db.AgentTaskQueue, wsID pgtype.UUID) (RunReplayResponse, error) {
	agentActor := ReplayActor{Type: "agent", ID: uuidToString(task.AgentID)}
	run := ReplayRun{
		ID: uuidToString(task.ID), IssueID: uuidToString(task.IssueID), AgentID: uuidToString(task.AgentID), Status: task.Status,
		RuntimeID: uuidToString(task.RuntimeID), Links: []ReplayLink{},
	}
	if agent, err := h.Queries.GetAgent(ctx, task.AgentID); err == nil {
		agentActor.Name = agent.Name
		run.AgentName, run.TrustMode, run.EffectMode, run.Model = agent.Name, agent.TrustMode, agent.EffectMode, agent.Model.String
	}
	run.CreatedAt = tsPtr(task.CreatedAt)
	run.StartedAt = tsPtr(task.StartedAt)
	run.CompletedAt = tsPtr(task.CompletedAt)
	for _, l := range []struct {
		rel string
		id  pgtype.UUID
	}{
		{"parent", task.ParentTaskID}, {"delegated_from", task.DelegatedFromTaskID}, {"retry_of", task.RetryOfTaskID}, {"rerun_of", task.RerunOfTaskID},
		{"resumed_by", task.ResumedByTaskID}, {"preempted_by", task.PreemptedByTaskID}, {"review_of", task.ReviewOfTaskID}, {"escalation_for", task.EscalationForTaskID},
	} {
		if !l.id.Valid {
			continue
		}
		link := ReplayLink{Relation: l.rel, TaskID: uuidToString(l.id)}
		if other, err := h.Queries.GetAgentTask(ctx, l.id); err == nil {
			link.AgentID = uuidToString(other.AgentID)
			if a, err := h.Queries.GetAgent(ctx, other.AgentID); err == nil {
				link.AgentName = a.Name
			}
		}
		run.Links = append(run.Links, link)
	}

	run.SafeMode = task.SafeMode
	var cands []replayCandidate
	add := func(c replayCandidate) { cands = append(cands, c) }

	// The plan the calls are compared against: the version the run started
	// with when known, else the issue's active plan.
	planText := ""
	snapshot := h.replaySnapshot(ctx, wsID, task.ID)
	run.Snapshot = snapshot
	if task.IssueID.Valid {
		var plan db.IssuePlan
		var perr error
		if snapshot != nil && snapshot.PlanVersion > 0 {
			plan, perr = h.Queries.GetIssuePlanVersion(ctx, db.GetIssuePlanVersionParams{IssueID: task.IssueID, WorkspaceID: wsID, Version: snapshot.PlanVersion})
		} else {
			plan, perr = h.Queries.GetActiveIssuePlan(ctx, db.GetActiveIssuePlanParams{IssueID: task.IssueID, WorkspaceID: wsID})
		}
		if perr == nil {
			var steps []any
			_ = json.Unmarshal(plan.Steps, &steps)
			run.Plan = &ReplayPlan{Version: plan.Version, Steps: len(steps)}
			planText = plan.Content + " " + string(plan.Steps)
		}
	}

	// 1. Task messages: the transcript, steers included.
	messages, err := h.Queries.ListTaskMessages(ctx, task.ID)
	if err != nil {
		return RunReplayResponse{}, err
	}
	for _, m := range messages {
		kind, actor := replayMessageKind(m.Type, agentActor)
		data := map[string]any{"type": m.Type, "seq": m.Seq}
		if m.Tool.Valid && m.Tool.String != "" {
			data["tool"] = m.Tool.String
		}
		if len(m.Input) > 0 {
			var in any
			if json.Unmarshal(m.Input, &in) == nil {
				data["input"] = in
			}
		}
		if m.Output.Valid && m.Output.String != "" {
			data["output"] = truncate(m.Output.String, 4000)
		}
		text := ""
		redacted := false
		if m.Content.Valid {
			text, redacted = replayRedact(m.Content.String)
		}
		if in, ok := data["input"].(map[string]any); ok {
			clean := redact.InputMap(in)
			if fmt.Sprint(clean) != fmt.Sprint(in) {
				redacted = true
			}
			data["input"] = clean
		}
		if out, ok := data["output"].(string); ok {
			clean, changed := replayRedact(out)
			data["output"] = clean
			redacted = redacted || changed
		}
		title := replayMessageTitle(kind, m)
		var inPlan *bool
		if kind == "tool_use" && planText != "" {
			v := planMentions(planText, m.Tool.String)
			inPlan = &v
			if !v {
				run.Drift++
			}
		}
		add(replayCandidate{at: m.CreatedAt.Time, order: 0, seq: int(m.Seq), kind: kind, actor: actor, title: title, text: text, data: data, redacted: redacted, inPlan: inPlan, source: "task_message", srcID: uuidToString(m.ID)})
	}
	// Checkpoint marker (K20): the last message the daemon confirmed durable.
	if task.CheckpointedAt.Valid && task.LastCheckpointSeq.Valid {
		add(replayCandidate{at: task.CheckpointedAt.Time, order: 1, seq: int(task.LastCheckpointSeq.Int64), kind: "checkpoint", actor: ReplayActor{Type: "system"},
			title: fmt.Sprintf("Checkpoint at message %d", task.LastCheckpointSeq.Int64), data: map[string]any{"seq": task.LastCheckpointSeq.Int64, "attempts": task.CheckpointAttempts}, source: "agent_task_queue", srcID: uuidToString(task.ID)})
	}
	// 2. Effects (K69): what the run changed; immutable fields only, so the
	// hash survives an undo. The undo itself appends its own event.
	effects, err := h.Queries.ListAgentEffectsForTask(ctx, db.ListAgentEffectsForTaskParams{WorkspaceID: wsID, TaskID: task.ID})
	if err != nil {
		return RunReplayResponse{}, err
	}
	for _, e := range effects {
		var before, after any
		_ = json.Unmarshal(e.Before, &before)
		_ = json.Unmarshal(e.After, &after)
		data := map[string]any{"kind": e.Kind, "target_type": e.TargetType, "target_id": uuidToString(e.TargetID), "before": before, "after": after, "reversible": e.Reversible, "status": e.Status}
		add(replayCandidate{at: e.CreatedAt.Time, order: 2, kind: "effect", actor: agentActor, title: "Effect: " + e.Kind, data: data, source: "agent_effect", srcID: uuidToString(e.ID)})
		if e.ReversedAt.Valid {
			by := ReplayActor{Type: e.ReversedByType.String, ID: uuidToString(e.ReversedByID)}
			add(replayCandidate{at: e.ReversedAt.Time, order: 2, kind: "effect_reversed", actor: by, title: "Undone: " + e.Kind,
				data: map[string]any{"effect_id": uuidToString(e.ID), "kind": e.Kind, "error": e.ReverseError.String}, source: "agent_effect", srcID: uuidToString(e.ID) + ":reversed"})
		}
	}
	// 3. Decisions the run asked (K63), and their answers.
	decisions, err := h.Queries.ListIssueDecisionsForTask(ctx, task.ID)
	if err != nil {
		return RunReplayResponse{}, err
	}
	for _, d := range decisions {
		var options any
		_ = json.Unmarshal(d.Options, &options)
		add(replayCandidate{at: d.CreatedAt.Time, order: 3, kind: "decision_asked", actor: ReplayActor{Type: d.AskedByType, ID: uuidToString(d.AskedByID)},
			title: "Decision asked", text: d.Question, data: map[string]any{"decision_id": uuidToString(d.ID), "options": options, "urgency": d.Urgency}, source: "issue_decision", srcID: uuidToString(d.ID)})
		if d.RespondedAt.Valid {
			var resp any
			_ = json.Unmarshal(d.Response, &resp)
			add(replayCandidate{at: d.RespondedAt.Time, order: 3, kind: "decision_answered", actor: ReplayActor{Type: d.RespondedByType.String, ID: uuidToString(d.RespondedByID)},
				title: "Decision answered", data: map[string]any{"decision_id": uuidToString(d.ID), "response": resp}, source: "issue_decision", srcID: uuidToString(d.ID) + ":answer"})
		}
	}
	// 4. Handoff packets (K17): where the baton was passed.
	packets, err := h.Queries.ListHandoffPacketsForRun(ctx, task.ID)
	if err != nil {
		return RunReplayResponse{}, err
	}
	for _, p := range packets {
		var decisionsJ, evidence, failed any
		_ = json.Unmarshal(p.Decisions, &decisionsJ)
		_ = json.Unmarshal(p.Evidence, &evidence)
		_ = json.Unmarshal(p.FailedAttempts, &failed)
		add(replayCandidate{at: p.CreatedAt.Time, order: 4, kind: "handoff", actor: ReplayActor{Type: p.CreatedByType, ID: uuidToString(p.CreatedByID)},
			title: "Handoff packet", text: p.Objective + "\n→ " + p.NextAction.String,
			data:   map[string]any{"packet_id": uuidToString(p.ID), "objective": p.Objective, "decisions": decisionsJ, "evidence": evidence, "failed_attempts": failed, "next_action": p.NextAction.String},
			source: "handoff_packet", srcID: uuidToString(p.ID)})
	}
	// 5. Usage: the cost, stamped when the daemon reported it.
	var cost ReplayCost
	usage, err := h.Queries.GetTaskUsage(ctx, task.ID)
	if err != nil {
		return RunReplayResponse{}, err
	}
	for _, u := range usage {
		cost.InputTokens += int64(u.InputTokens)
		cost.OutputTokens += int64(u.OutputTokens)
		if u.CostUsdTicks.Valid {
			total := u.CostUsdTicks.Int64
			if cost.CostUsdTicks != nil {
				total += *cost.CostUsdTicks
			}
			cost.CostUsdTicks = &total
		}
		data := map[string]any{"provider": u.Provider, "model": u.Model, "input_tokens": u.InputTokens, "output_tokens": u.OutputTokens, "cache_read_tokens": u.CacheReadTokens, "cache_write_tokens": u.CacheWriteTokens}
		if u.CostUsdTicks.Valid {
			data["cost_usd_ticks"] = u.CostUsdTicks.Int64
		}
		add(replayCandidate{at: u.UpdatedAt.Time, order: 5, kind: "cost", actor: ReplayActor{Type: "system"}, title: fmt.Sprintf("Usage %s: %d in / %d out", u.Model, u.InputTokens, u.OutputTokens), data: data, source: "task_usage", srcID: uuidToString(u.TaskID) + ":" + u.Provider + ":" + u.Model})
	}
	// 6. The run's own audit entries (gates, seals, resumes), minus the seal
	// itself, which is the chain's witness and cannot be in the chain.
	entries, err := h.Queries.ListAuditLogEntries(ctx, db.ListAuditLogEntriesParams{WorkspaceID: wsID, EntityID: task.ID, PageSize: 200})
	if err != nil {
		return RunReplayResponse{}, err
	}
	var seal *ReplaySeal
	for _, a := range entries {
		if a.Action == AuditRunSealed {
			var d struct {
				Events   int    `json:"events"`
				HeadHash string `json:"head_hash"`
			}
			if json.Unmarshal(a.Details, &d) == nil && (seal == nil || a.OccurredAt.Time.Before(seal.SealedAt)) {
				seal = &ReplaySeal{Events: d.Events, HeadHash: d.HeadHash, SealedAt: a.OccurredAt.Time}
			}
			continue
		}
		var details any
		_ = json.Unmarshal(a.Details, &details)
		add(replayCandidate{at: a.OccurredAt.Time, order: 6, kind: "audit", actor: ReplayActor{Type: a.ActorType, ID: uuidToString(a.ActorID)}, title: a.Action,
			data: map[string]any{"action": a.Action, "details": details, "audit_hash": a.Hash}, source: "audit_log_entry", srcID: uuidToString(a.ID)})
	}

	sort.SliceStable(cands, func(i, j int) bool {
		if !cands[i].at.Equal(cands[j].at) {
			return cands[i].at.Before(cands[j].at)
		}
		if cands[i].order != cands[j].order {
			return cands[i].order < cands[j].order
		}
		return cands[i].seq < cands[j].seq
	})
	events := make([]ReplayEvent, 0, len(cands))
	prev := ""
	for i, c := range cands {
		if c.data == nil {
			c.data = map[string]any{}
		}
		class := replayClassInternal
		if c.redacted {
			class = replayClassConfidential
		}
		e := ReplayEvent{Seq: i, At: c.at.UTC(), Kind: c.kind, Actor: c.actor, Title: c.title, Text: c.text, Data: c.data, DataClass: class, InPlan: c.inPlan, Source: c.source, SourceID: c.srcID, PrevHash: prev}
		e.Hash = replayEventHash(prev, e)
		prev = e.Hash
		events = append(events, e)
	}
	if seal != nil {
		seal.Verified = seal.Events <= len(events) && seal.Events > 0 && events[seal.Events-1].Hash == seal.HeadHash
		if seal.Events == 0 {
			seal.Verified = seal.HeadHash == ""
		}
	}
	return RunReplayResponse{Run: run, Events: events, Total: len(events), HeadHash: prev, Cost: cost, Sealed: seal}, nil
}

// replayEventHash chains one event onto the previous hash. Only the fields
// that never change after the fact are hashed.
func replayEventHash(prev string, e ReplayEvent) string {
	data, _ := json.Marshal(e.Data) // Go sorts map keys: canonical enough for our own reads.
	sum := sha256.Sum256([]byte(prev + "|" + e.At.UTC().Format(time.RFC3339Nano) + "|" + e.Kind + "|" + e.Source + "|" + e.SourceID + "|" + e.Actor.Type + ":" + e.Actor.ID + "|" + e.Title + "|" + e.Text + "|" + string(data)))
	return hex.EncodeToString(sum[:])
}

// replayMessageKind normalizes the two spellings the transcript holds.
func replayMessageKind(t string, agent ReplayActor) (string, ReplayActor) {
	switch strings.ReplaceAll(t, "-", "_") {
	case "text":
		return "text", agent
	case "thinking":
		return "thinking", agent
	case "tool_use":
		return "tool_use", agent
	case "tool_result":
		return "tool_result", ReplayActor{Type: "tool"}
	case "steering_instruction":
		return "steer", ReplayActor{Type: "member"}
	case "status", "log":
		return "status", ReplayActor{Type: "system"}
	case "error":
		return "error", ReplayActor{Type: "system"}
	default:
		return strings.ReplaceAll(t, "-", "_"), agent
	}
}

func replayMessageTitle(kind string, m db.TaskMessage) string {
	switch kind {
	case "tool_use":
		return "Tool call: " + m.Tool.String
	case "tool_result":
		return "Tool result: " + m.Tool.String
	case "steer":
		return "Steer from a human"
	case "thinking":
		return "Thinking"
	case "text":
		return "Agent says"
	case "error":
		return "Error"
	default:
		return kind
	}
}

func tsPtr(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time.UTC()
	return &t
}

// recordRunSnapshot audits what the run starts with (K70): trust mode,
// effect mode, model, permission profile, runtime, safe mode, plan version.
func (h *Handler) recordRunSnapshot(ctx context.Context, task db.AgentTaskQueue, wsIDStr string) {
	wsID, err := util.ParseUUID(wsIDStr)
	if err != nil {
		return
	}
	snap := ReplaySnapshot{RuntimeID: uuidToString(task.RuntimeID), SafeMode: task.SafeMode, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if agent, err := h.Queries.GetAgent(ctx, task.AgentID); err == nil {
		snap.TrustMode, snap.EffectMode, snap.Model, snap.ThinkingLevel = agent.TrustMode, agent.EffectMode, agent.Model.String, agent.ThinkingLevel.String
		snap.PermissionProfileID = uuidToString(agent.PermissionProfileID)
	}
	if task.IssueID.Valid {
		if plan, err := h.Queries.GetActiveIssuePlan(ctx, db.GetActiveIssuePlanParams{IssueID: task.IssueID, WorkspaceID: wsID}); err == nil {
			snap.PlanVersion = plan.Version
		}
	}
	h.audit(ctx, wsID, "system", "", AuditRunStarted, "task", task.ID, snap, nil)
}

// SimulateTaskReplay: POST /api/tasks/{taskId}/replay/simulate — a new run
// of the issue in safe mode: every Multica write is held for approval and
// the handoff tells the agent to describe external actions instead of
// performing them. The held payloads are the proof of what it would do.
func (h *Handler) SimulateTaskReplay(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	task, wsID, ok := h.runReplayTask(w, r)
	if !ok {
		return
	}
	if !task.IssueID.Valid {
		writeError(w, http.StatusBadRequest, "this run has no issue to replay on")
		return
	}
	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: task.IssueID, WorkspaceID: wsID})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	replay, err := h.buildRunReplay(r.Context(), task, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build the replay: "+err.Error())
		return
	}
	note := safeReplayNote(replay)
	next, err := h.TaskService.EnqueueTaskForIssueWithHandoff(r.Context(), issue, note, parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start the safe replay: "+err.Error())
		return
	}
	if err := h.Queries.SetTaskSafeMode(r.Context(), next.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to flag the safe replay")
		return
	}
	h.audit(r.Context(), wsID, "member", userID, AuditRunReplayedSafe, "task", task.ID, map[string]any{"new_task_id": uuidToString(next.ID), "events": replay.Total}, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"task_id": uuidToString(next.ID), "safe_mode": true})
}

func safeReplayNote(replay RunReplayResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "SAFE REPLAY of run %s (%d events). This is a dry run: do NOT perform external side effects (no messages sent outside Multica, no payments, no emails, no deploys); describe each intended external action and its exact payload instead. Every write you make in Multica is held for human approval and shown as a preview.\n", replay.Run.ID, replay.Total)
	b.WriteString("Original tool calls, in order:\n")
	n := 0
	for _, e := range replay.Events {
		if e.Kind != "tool_use" {
			continue
		}
		n++
		if n > 40 {
			b.WriteString("- …\n")
			break
		}
		fmt.Fprintf(&b, "- #%d %s\n", e.Seq+1, e.Title)
	}
	return b.String()
}

// replayRedact returns the redacted text and whether anything was removed.
func replayRedact(s string) (string, bool) {
	out := redact.Text(s)
	return out, out != s
}

// planMentions reports whether the plan names the tool (drift check).
func planMentions(plan string, tool string) bool {
	tool = strings.ToLower(strings.TrimSpace(tool))
	if tool == "" {
		return true
	}
	plan = strings.ToLower(plan)
	if strings.Contains(plan, tool) {
		return true
	}
	// "read_file" is mentioned as "read file" or "read the file" just as well.
	words := strings.FieldsFunc(tool, func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	if len(words) < 2 {
		return false
	}
	for _, w := range words {
		if len(w) >= 3 && !strings.Contains(plan, w) {
			return false
		}
	}
	return true
}

// replaySnapshot reads the run.started audit entry, if the run has one.
func (h *Handler) replaySnapshot(ctx context.Context, wsID, taskID pgtype.UUID) *ReplaySnapshot {
	entries, err := h.Queries.ListAuditLogEntries(ctx, db.ListAuditLogEntriesParams{WorkspaceID: wsID, EntityID: taskID, Action: pgtype.Text{String: AuditRunStarted, Valid: true}, PageSize: 1})
	if err != nil || len(entries) == 0 {
		return nil
	}
	var snap ReplaySnapshot
	if json.Unmarshal(entries[0].Details, &snap) != nil {
		return nil
	}
	return &snap
}
