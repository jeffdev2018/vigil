package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/pricing"
)

// ---------------------------------------------------------------------------
// Run-group LLM judge (JEF-234 follow-up)
//
// POST /api/run-groups/{id}/judge asks the platform's internal LLM to compare
// the attempts of one race and name a winner. Synchronous, human-only (the
// route carries RequireHumanActor): one LLM call, the verdict persisted onto
// run_group.judgement, the updated group returned.
//
// Anti-hallucination rule: the judge sees only real DB facts — the issue text
// and, per attempt, the recorded runtime/model/status, the summed
// provider-reported cost, the measured duration, the stored diff stat and the
// stored unified diff (truncated). Nothing is estimated for the prompt; the
// only cost the judgement itself reports is the judge call's own token usage,
// priced through pricing.EstimateTicks when the model has a known rate and
// NULL otherwise — never a fabricated zero.
//
// Failure model: a race with fewer than two completed attempts is not
// judgeable (409 run_group_not_judgeable). An LLM error, a malformed reply,
// or a winner_task_id that is not an attempt of the race stores a
// status='failed' judgement and still answers 200 — the failure is a fact
// about the judging attempt, and re-judging overwrites it.
// ---------------------------------------------------------------------------

const (
	ErrCodeRunGroupNotJudgeable = "run_group_not_judgeable"

	// judgeDiffMaxBytes bounds each attempt's unified diff in the prompt. The
	// compare view shows the full stored patch; the judge gets enough of it to
	// judge shape and size without one oversized run crowding the others out.
	judgeDiffMaxBytes = 8 * 1024

	// judgeMaxErrorLen bounds the LLM error text persisted on a failed
	// judgement: the field feeds the UI, not a log aggregator.
	judgeMaxErrorLen = 500
)

// judgeGeneration budgets the one LLM call. Judging is deliberate comparison
// work, so temperature stays low and the completion cap fits a justification
// plus one score per attempt.
const (
	judgeTemperature         = 0.1
	judgeMaxCompletionTokens = 2048
)

// runGroupJudgeSystemPrompt is the judge's contract. It names the reply shape
// and pins the two rules the handler cannot enforce after the fact: task ids
// come only from the prompt, and every claim comes from the given facts.
const runGroupJudgeSystemPrompt = `You are the judge of a race between AI coding-agent attempts that worked independently on the same issue. Compare the attempts on the quality of their result (the diff and diff stat), and their efficiency (cost, duration). Reply with one JSON object only: {"winner_task_id":"<task_id of the best attempt>","justification":"two or three sentences","scores":[{"task_id":"<task_id>","score":0-100,"rationale":"one sentence"}]}. Score every attempt. Every task_id you mention must be one of the attempt task ids given in the input. Base the verdict only on the facts given; never invent metrics, files, or outcomes.`

// RunGroupJudgementScore is the judge's per-attempt line.
type RunGroupJudgementScore struct {
	TaskID    string  `json:"task_id"`
	Score     float64 `json:"score"`
	Rationale string  `json:"rationale"`
}

// RunGroupJudgement is both the JSONB stored on run_group.judgement and the
// wire object on RunGroupResponse. WinnerTaskID is empty on a failed
// judgement; CostUsdTicks is NULL when the judge call reported no usage or
// the model has no known rate.
type RunGroupJudgement struct {
	Status        string                   `json:"status"` // "answered" | "failed"
	WinnerTaskID  string                   `json:"winner_task_id"`
	Justification string                   `json:"justification"`
	Scores        []RunGroupJudgementScore `json:"scores"`
	Model         string                   `json:"model"`
	JudgedAt      string                   `json:"judged_at"`
	CostUsdTicks  *int64                   `json:"cost_usd_ticks"`
}

// runGroupJudgementFromDB decodes the stored JSONB. A NULL column is a nil
// judgement; a row written by a future shape the binary cannot parse degrades
// to nil rather than failing the whole group response.
func runGroupJudgementFromDB(raw []byte) *RunGroupJudgement {
	if len(raw) == 0 {
		return nil
	}
	var j RunGroupJudgement
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil
	}
	return &j
}

// judgeLLM returns the judge LLM seam, falling back to the handler's base
// client when no dedicated seam was wired (mirrors consultLLM).
func (h *Handler) judgeLLM() ConsultLLM {
	if h.JudgeLLM != nil {
		return h.JudgeLLM
	}
	return h.LLM
}

// judgeModel resolves the judge model: MULTICA_JUDGE_MODEL when set, the
// built-in fallback otherwise — the same rule consultModel applies, so a
// deployment's default-model change cannot silently reprice judging.
func (h *Handler) judgeModel() string {
	if m := strings.TrimSpace(h.cfg.JudgeModel); m != "" {
		return m
	}
	return llm.FallbackModel
}

// buildRunGroupJudgePrompt assembles the judge's user prompt from DB facts
// only: the issue text, then per attempt the recorded status, the runtime
// and model it ran on, the summed provider-reported cost, the measured
// duration, the stored diff stat, and the stored unified diff truncated to
// judgeDiffMaxBytes.
func buildRunGroupJudgePrompt(issue db.Issue, attempts []db.AgentTaskQueue, metrics map[string]db.ListRunGroupAttemptMetricsForIssueRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ISSUE: %s\n%s\n", issue.Title, issue.Description.String)
	for _, task := range attempts {
		taskID := uuidToString(task.ID)
		m := metrics[taskID]
		fmt.Fprintf(&b, "\n=== ATTEMPT task_id=%s ===\nstatus=%s runtime=%s model=%s cost_usd_ticks=%d duration_seconds=%d\n",
			taskID, task.Status, m.RuntimeName, task.ModelOverride.String, m.CostUsdTicks, m.DurationSeconds)
		if len(task.DiffStat) > 0 {
			fmt.Fprintf(&b, "diff_stat=%s\n", string(task.DiffStat))
		} else {
			b.WriteString("diff_stat=none\n")
		}
		if task.DiffUnified.Valid && task.DiffUnified.String != "" {
			diff := task.DiffUnified.String
			if len(diff) > judgeDiffMaxBytes {
				diff = diff[:judgeDiffMaxBytes] + "\n… [diff truncated for judging]"
			}
			fmt.Fprintf(&b, "diff_unified:\n%s\n", diff)
		} else if len(task.DiffStat) > 0 {
			// A stat without a patch is the recorded "too large to store" case
			// (see recordTaskDiff): the judge must know a diff exists
			// even though it cannot read it.
			b.WriteString("diff_unified: produced but too large to store\n")
		} else {
			b.WriteString("diff_unified: none recorded\n")
		}
	}
	return b.String()
}

// JudgeRunGroup: POST /api/run-groups/{id}/judge.
func (h *Handler) JudgeRunGroup(w http.ResponseWriter, r *http.Request) {
	group, issue, ok := h.loadRunGroupForUser(w, r)
	if !ok {
		return
	}
	attempts, err := h.Queries.ListRunGroupTasks(r.Context(), group.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list attempts")
		return
	}
	completed := 0
	for _, task := range attempts {
		if task.Status == "completed" {
			completed++
		}
	}
	if completed < 2 {
		writeErrorCode(w, http.StatusConflict, ErrCodeRunGroupNotJudgeable, "a race is judgeable once at least two attempts have completed")
		return
	}
	judgeLLM := h.judgeLLM()
	if judgeLLM == nil || !judgeLLM.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "judging is not available: no LLM configured")
		return
	}

	model := h.judgeModel()
	metrics := h.runGroupMetricsByTask(r.Context(), issue.ID)
	raw, usage, err := judgeLLM.GenerateJSONWithUsage(
		r.Context(), model,
		runGroupJudgeSystemPrompt,
		buildRunGroupJudgePrompt(issue, attempts, metrics),
		judgeTemperature, judgeMaxCompletionTokens,
	)

	judgement := RunGroupJudgement{
		Status:   "answered",
		Scores:   []RunGroupJudgementScore{},
		Model:    model,
		JudgedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// The judge call's own cost, from its real token usage only: priced when
	// the model has a known rate, NULL otherwise — the same rule consult
	// applies, so "not reported" is never a fabricated zero.
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		if ticks := pricing.EstimateTicks(pricing.Usage{
			Model:        model,
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
		}); ticks > 0 {
			judgement.CostUsdTicks = &ticks
		}
	}

	switch {
	case err != nil:
		reason := err.Error()
		if len(reason) > judgeMaxErrorLen {
			reason = reason[:judgeMaxErrorLen]
		}
		judgement.Status = "failed"
		judgement.Justification = "judge generation failed: " + reason
	default:
		var parsed struct {
			WinnerTaskID  string                   `json:"winner_task_id"`
			Justification string                   `json:"justification"`
			Scores        []RunGroupJudgementScore `json:"scores"`
		}
		if jerr := json.Unmarshal([]byte(raw), &parsed); jerr != nil {
			judgement.Status = "failed"
			judgement.Justification = "the judge returned malformed output"
			break
		}
		if !runGroupContainsTaskID(attempts, parsed.WinnerTaskID) {
			judgement.Status = "failed"
			judgement.Justification = "the judge named a winner that is not an attempt of this race"
			break
		}
		judgement.WinnerTaskID = parsed.WinnerTaskID
		judgement.Justification = parsed.Justification
		if len(parsed.Scores) > 0 {
			judgement.Scores = parsed.Scores
		}
	}

	encoded, merr := json.Marshal(judgement)
	if merr != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode the judgement")
		return
	}
	updated, err := h.Queries.SetRunGroupJudgement(r.Context(), db.SetRunGroupJudgementParams{ID: group.ID, Judgement: encoded})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the judgement")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditRunGroup, "issue", issue.ID, map[string]any{
		"run_group_id": uuidToString(group.ID), "judged": true,
		"judgement_status": judgement.Status, "winner_task_id": judgement.WinnerTaskID,
	}, nil)
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	writeJSON(w, http.StatusOK, map[string]any{"group": runGroupToResponse(updated, attempts, metrics)})
}

// runGroupContainsTaskID is the string-id half of runGroupContainsTask, for
// validating the id the judge named against the race's actual attempts.
func runGroupContainsTaskID(attempts []db.AgentTaskQueue, taskID string) bool {
	for _, task := range attempts {
		if uuidToString(task.ID) == taskID {
			return true
		}
	}
	return false
}
