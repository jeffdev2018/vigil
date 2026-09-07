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

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Systematic adversarial critic (F25 / JEF-18).
//
// K15's cross-review is a workspace/project decision: runs on a covered
// project get a second reader, and the report is a signal. This is the other
// axis and it is stronger: a team decides that ONE agent — or one squad —
// never delivers unreviewed, on any project, and that a blocking policy makes
// the issue wait for the answer.
//
// Three decisions carry the feature:
//
//   - the critic's answer is three-valued (pass / concerns / block) and only
//     `block` costs anything: the author is relaunched with the critic's
//     summary as its brief. `concerns` is the honest middle — worth reading,
//     not worth another round — and it is also where every unreadable answer
//     lands, so silence can never approve and can never relaunch.
//   - the hold releases itself. It is expressed as "there is a live critic run
//     with critic_blocking on this issue", so a critic that crashes, is
//     cancelled or finishes without answering opens the gate by finishing.
//     Nothing has to time it out, and no issue can be stranded by a dead run.
//   - a member is never blocked. The gate reads the acting agent and returns
//     early for a human: a policy about how agents work must not become a
//     policy about what people may do.
//
// The decision itself is in service/critic_gate.go; this file is its wiring.

const (
	AuditCriticVerdict        = "critic.verdict"
	AuditCriticPolicyChanged  = "critic.policy_changed"
	EventCriticVerdictCreated = "critic_verdict:created"

	// InboxTypeCriticDegraded: the policy could not be honoured (no distinct
	// critic, or the critic run could not be started). The delivery finalised.
	InboxTypeCriticDegraded = "critic_degraded"
	// InboxTypeCriticBudget: the loop stopped on max_rounds or max_cost.
	InboxTypeCriticBudget = "critic_budget"

	// criticFindingsCap bounds one verdict's findings, same argument as the
	// F06 per-run cap: past a hundred the reader stops reading.
	criticFindingsCap  = 100
	criticSummaryMax   = 4000
	criticFindingsSize = 200_000

	criticSubjectAgent = "agent"
	criticSubjectSquad = "squad"

	ErrCodeCriticRequired = "critic_required"
	ErrCodeCriticIsAuthor = "critic_is_author"
)

// criticBrief is the critic run's whole brief. It names the diff the same way
// K15 does — the critic is an independent reader of a change, not a
// continuation of the author's conversation — and then states the verdict
// contract, because a critic that ends without one is a wasted round.
const criticBrief = "Adversarial review (round %d). An agent running on %s just delivered a change on this issue. Review ONLY that change, as an independent reader: do not read, resume or continue the author's conversation, and do not modify any code.\n" +
	"What to review: %s\n%s" +
	"End your run by recording the verdict:\n" +
	"```\nmultica review verdict --issue %s --verdict pass|concerns|block --summary \"one paragraph\" [--findings-file findings.json]\n```\n" +
	"pass = nothing in the way. concerns = worth reading, not worth another round. block = the author must fix this and deliver again; only use it for something that would be wrong to ship.\n" +
	"findings.json is a JSON array of {\"severity\":\"bug\"|\"warning\"|\"info\",\"file\":\"…\",\"line\":42,\"title\":\"…\",\"note\":\"…\"}.\n" +
	"If the CLI is unavailable, end your run with a fenced block starting with ```critic_verdict containing one JSON object: {\"verdict\":\"pass\"|\"concerns\"|\"block\",\"summary\":\"…\",\"findings\":[…]}."

var criticVerdictFence = regexp.MustCompile("(?s)```critic_verdict\\s*(\\{.*?\\})\\s*```")

// --- wire types --------------------------------------------------------------

// CriticFinding shares the F06 review-flag vocabulary so a reader who knows
// one knows the other. Everything but severity and title is optional: a
// finding about the shape of a change has no line to point at.
type CriticFinding struct {
	Severity string `json:"severity"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Title    string `json:"title"`
	Note     string `json:"note,omitempty"`
}

type CriticPolicyResponse struct {
	SubjectType             string   `json:"subject_type"`
	SubjectID               string   `json:"subject_id"`
	Enabled                 bool     `json:"enabled"`
	CriticAgentID           *string  `json:"critic_agent_id"`
	RequireDistinctProvider bool     `json:"require_distinct_provider"`
	Blocking                bool     `json:"blocking"`
	MaxRounds               int      `json:"max_rounds"`
	MaxCostUsdTicks         *int64   `json:"max_cost_usd_ticks"`
	Phases                  []string `json:"phases"`
}

type CriticVerdictResponse struct {
	ID            string          `json:"id"`
	IssueID       string          `json:"issue_id"`
	SubjectTaskID string          `json:"subject_task_id"`
	CriticTaskID  *string         `json:"critic_task_id"`
	Phase         string          `json:"phase"`
	Verdict       string          `json:"verdict"`
	Reason        string          `json:"reason"`
	Summary       string          `json:"summary"`
	Findings      []CriticFinding `json:"findings"`
	Round         int             `json:"round"`
	CostUsdTicks  int64           `json:"cost_usd_ticks"`
	CreatedAt     string          `json:"created_at"`
}

func criticPolicyToResponse(row db.AgentCriticPolicy, subjectType, subjectID string) CriticPolicyResponse {
	resp := CriticPolicyResponse{
		SubjectType: subjectType, SubjectID: subjectID,
		Enabled: row.Enabled, RequireDistinctProvider: row.RequireDistinctProvider,
		Blocking: row.Blocking, MaxRounds: int(row.MaxRounds), Phases: row.Phases,
	}
	if resp.Phases == nil {
		resp.Phases = []string{}
	}
	if row.CriticAgentID.Valid {
		resp.CriticAgentID = uuidToPtr(row.CriticAgentID)
	}
	if row.MaxCostUsdTicks.Valid {
		v := row.MaxCostUsdTicks.Int64
		resp.MaxCostUsdTicks = &v
	}
	return resp
}

// defaultCriticPolicyResponse is what a subject with no row answers: the
// feature is off, and every default is the one a fresh row would carry.
func defaultCriticPolicyResponse(subjectType, subjectID string) CriticPolicyResponse {
	return CriticPolicyResponse{
		SubjectType: subjectType, SubjectID: subjectID,
		RequireDistinctProvider: true, MaxRounds: 1,
		Phases: []string{service.CriticPhaseChange},
	}
}

func criticVerdictToResponse(row db.AgentCriticVerdict) CriticVerdictResponse {
	return CriticVerdictResponse{
		ID: uuidToString(row.ID), IssueID: uuidToString(row.IssueID),
		SubjectTaskID: uuidToString(row.SubjectTaskID), CriticTaskID: uuidToPtr(row.CriticTaskID),
		Phase: row.Phase, Verdict: row.Verdict, Reason: row.Reason.String, Summary: row.Summary.String,
		Findings: decodeCriticFindings(row.Findings), Round: int(row.Round),
		CostUsdTicks: row.CostUsdTicks, CreatedAt: timestampToString(row.CreatedAt),
	}
}

// decodeCriticFindings never fails: a malformed findings blob must still
// render a verdict, because the verdict is the part that has consequences.
func decodeCriticFindings(raw []byte) []CriticFinding {
	out := []CriticFinding{}
	if len(raw) == 0 {
		return out
	}
	if json.Unmarshal(raw, &out) != nil {
		return []CriticFinding{}
	}
	if out == nil {
		return []CriticFinding{}
	}
	return out
}

// normalizeCriticFindings bounds and cleans what a critic sent. An unknown
// severity becomes "info" rather than being dropped: the finding's text is
// what the author reads, and losing it to a typo helps nobody.
func normalizeCriticFindings(in []CriticFinding) []byte {
	out := make([]CriticFinding, 0, len(in))
	for _, f := range in {
		if len(out) >= criticFindingsCap {
			break
		}
		switch f.Severity {
		case "bug", "warning", "info":
		default:
			f.Severity = "info"
		}
		f.Title = truncate(strings.TrimSpace(f.Title), reviewFlagTitleMax)
		f.Note = truncate(strings.TrimSpace(f.Note), reviewFlagBodyMax)
		f.File = truncate(strings.TrimSpace(f.File), reviewFlagPathMax)
		if f.Line < 0 {
			f.Line = 0
		}
		if f.Title == "" && f.Note == "" {
			continue
		}
		out = append(out, f)
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > criticFindingsSize {
		return []byte("[]")
	}
	return raw
}

// --- policy resolution -------------------------------------------------------

// criticPolicyFor resolves the policy that governs a run: the agent's own
// first, the squad's when the run is a squad leg and the agent has none. An
// agent that carries its own policy is stating something about itself, so it
// wins over the squad it happens to be running for.
func (h *Handler) criticPolicyFor(ctx context.Context, task db.AgentTaskQueue, workspaceID pgtype.UUID) (db.AgentCriticPolicy, bool) {
	if row, err := h.Queries.GetCriticPolicy(ctx, db.GetCriticPolicyParams{
		WorkspaceID: workspaceID, SubjectType: criticSubjectAgent, SubjectID: task.AgentID,
	}); err == nil {
		return row, true
	}
	if !task.SquadID.Valid {
		return db.AgentCriticPolicy{}, false
	}
	row, err := h.Queries.GetCriticPolicy(ctx, db.GetCriticPolicyParams{
		WorkspaceID: workspaceID, SubjectType: criticSubjectSquad, SubjectID: task.SquadID,
	})
	return row, err == nil
}

// resolveCriticAgent answers "is there a critic that can actually judge this
// run". Not the author, not archived, in this workspace, and — when the policy
// asks — not on the author's provider. A policy naming a critic that no longer
// qualifies degrades rather than failing: see DecideCritic.
func (h *Handler) resolveCriticAgent(ctx context.Context, task db.AgentTaskQueue, policy db.AgentCriticPolicy, workspaceID pgtype.UUID) (db.Agent, bool) {
	if !policy.CriticAgentID.Valid || policy.CriticAgentID == task.AgentID {
		return db.Agent{}, false
	}
	critic, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: policy.CriticAgentID, WorkspaceID: workspaceID})
	if err != nil || critic.ArchivedAt.Valid {
		return db.Agent{}, false
	}
	if !policy.RequireDistinctProvider {
		return critic, true
	}
	authorProvider := h.runProvider(ctx, task)
	criticProvider := ""
	if rt, err := h.Queries.GetAgentRuntime(ctx, critic.RuntimeID); err == nil {
		criticProvider = rt.Provider
	}
	// Two unknown providers are not "two different providers": with nothing to
	// compare, the policy's own requirement cannot be shown to hold.
	if authorProvider == "" || criticProvider == "" || authorProvider == criticProvider {
		return db.Agent{}, false
	}
	return critic, true
}

// --- the completion hook -----------------------------------------------------

// triggerCriticReview runs at an author delivery's completion. Chat runs, review
// runs, critic runs and every other judging leg are excluded: the critic reads
// a change, and a critic of a critic is the loop this bounds.
func (h *Handler) triggerCriticReview(ctx context.Context, task db.AgentTaskQueue, prURL, branch string) {
	if task.Status != "completed" || !task.IssueID.Valid || task.ChatSessionID.Valid || task.ReviewOfTaskID.Valid {
		return
	}
	if !service.CriticAuthorLeg(task.LegRole) {
		return
	}
	if _, isCritic := service.TaskCriticOf(task.Context); isCritic {
		return
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	policy, ok := h.criticPolicyFor(ctx, task, issue.WorkspaceID)
	if !ok || !service.CriticPolicyCoversPhase(policy, service.CriticPhaseChange) {
		return
	}
	rounds, err := h.Queries.CountCriticVerdictsForIssuePhase(ctx, db.CountCriticVerdictsForIssuePhaseParams{IssueID: issue.ID, Phase: service.CriticPhaseChange})
	if err != nil {
		slog.Warn("critic: count verdicts failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	round := int(rounds) + 1
	cost, err := h.Queries.SumIssueTaskCostTicks(ctx, issue.ID)
	if err != nil {
		slog.Warn("critic: sum issue cost failed", "issue_id", uuidToString(issue.ID), "error", err)
		cost = 0
	}
	critic, hasCritic := h.resolveCriticAgent(ctx, task, policy, issue.WorkspaceID)
	decision := service.DecideCritic(service.CriticPolicyFrom(policy), round, cost, hasCritic)

	switch decision.Action {
	case service.CriticFinalize:
		return
	case service.CriticPassDegraded:
		h.recordPlatformCriticVerdict(ctx, issue, task, service.CriticVerdictPass, decision.Reason, round, cost, InboxTypeCriticDegraded)
		return
	case service.CriticConcernsBudget:
		h.recordPlatformCriticVerdict(ctx, issue, task, service.CriticVerdictConcerns, decision.Reason, round, cost, InboxTypeCriticBudget)
		return
	}

	// Bounded workflows (JEF-275): the critique grows the delivery's workflow,
	// so a workflow at its ceiling stops the loop the same way a spent budget
	// does — with a `concerns` naming the round cap, not with silence.
	if ok, reason := h.TaskService.WorkflowAllowsLeg(ctx, task, service.LegRoleCritique); !ok {
		slog.Info("critic: refused by the workflow limits", "issue_id", uuidToString(issue.ID), "reason", reason)
		h.recordPlatformCriticVerdict(ctx, issue, task, service.CriticVerdictConcerns, service.CriticReasonMaxRounds, round, cost, InboxTypeCriticBudget)
		return
	}
	if err := h.startCriticRun(ctx, issue, task, critic, policy, round, prURL, branch, decision.Hold); err != nil {
		slog.Warn("critic: enqueue failed", "issue_id", uuidToString(issue.ID), "error", err)
		// A policy that could not start its critic must not leave the delivery
		// waiting for an answer that will never come.
		h.recordPlatformCriticVerdict(ctx, issue, task, service.CriticVerdictConcerns, service.CriticReasonNoVerdict, round, cost, InboxTypeCriticDegraded)
	}
}

// startCriticRun queues the critic and, for a blocking policy, parks the issue
// in review. The hold is written AFTER the enqueue succeeds: a failed enqueue
// must never leave an issue held for a run that does not exist.
func (h *Handler) startCriticRun(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, critic db.Agent, policy db.AgentCriticPolicy, round int, prURL, branch string, hold bool) error {
	provider := h.runProvider(ctx, task)
	if provider == "" {
		provider = "another provider"
	}
	ref := diffReference(prURL, branch, jsonStrings(task.TouchedPaths))
	if ref == "" {
		ref = "the change this issue's latest run delivered"
	}
	brief := fmt.Sprintf(criticBrief, round, provider, ref, h.diffBlock(ctx, issue, prURL), uuidToString(issue.ID))
	run, err := h.TaskService.EnqueueCrossReviewRun(ctx, issue, critic.ID, brief, task.OriginatorUserID)
	if err != nil {
		return err
	}
	if run, err = h.Queries.SetTaskCriticContext(ctx, db.SetTaskCriticContextParams{
		ID: run.ID, CriticOfTaskID: uuidToString(task.ID), CriticRound: int32(round),
		CriticPhase: service.CriticPhaseChange, CriticBlocking: hold,
	}); err != nil {
		return fmt.Errorf("stamp critic run: %w", err)
	}
	// Per-leg accounting (JEF-274): the critique is a leg of the delivery's
	// workflow, never a sample of the critic's own task class.
	if _, err := h.TaskService.StampLeg(ctx, run, service.LegRoleCritique, task); err != nil {
		slog.Warn("critic: stamp leg failed", "task_id", uuidToString(run.ID), "error", err)
	}
	if hold {
		h.holdIssueForCritic(ctx, issue)
	}
	h.audit(ctx, issue.WorkspaceID, "system", "", AuditCriticVerdict, "issue", issue.ID, map[string]any{
		"critic_task_id": uuidToString(run.ID), "subject_task_id": uuidToString(task.ID),
		"critic_agent_id": uuidToString(critic.ID), "round": round, "blocking": hold,
	}, nil)
	return nil
}

// holdIssueForCritic parks the delivery in the workspace's effective in_review
// status while the critic reads it. Terminal issues are left alone — a
// cancelled issue has no delivery to judge — and an issue already in review is
// already where the hold wants it.
//
// This is a SYSTEM status write (F28): it is the platform parking its own
// work, and a workspace transition rule that could refuse it would strand the
// issue between a finished run and a verdict.
func (h *Handler) holdIssueForCritic(ctx context.Context, issue db.Issue) {
	category := issuestatus.Effective(ctx, h.Queries, issue.WorkspaceID, issue.Status)
	if category == issuestatus.Done || category == issuestatus.Cancelled || category == issuestatus.InReview {
		return
	}
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: issue.ID, Status: issuestatus.InReview, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("critic: hold in review failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	resp := issueToResponse(updated, h.getIssuePrefix(ctx, issue.WorkspaceID))
	h.fillStatusCategory(ctx, issue.WorkspaceID, &resp)
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"issue": resp, "status_changed": true,
	})
}

// --- settling the critic run -------------------------------------------------

// settleCriticRun runs at a critic run's completion. A verdict posted through
// the CLI is already stored; a run that ended without one falls back to the
// fenced block, and an unreadable answer becomes `concerns` / `no_verdict` —
// never a pass, never a block.
func (h *Handler) settleCriticRun(ctx context.Context, task db.AgentTaskQueue, output string) {
	stamp, ok := service.TaskCriticOf(task.Context)
	if !ok || !task.IssueID.Valid {
		return
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	row, err := h.Queries.GetCriticVerdictByCriticTask(ctx, task.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		parsed := parseCriticVerdictFence(h.taskOutputText(ctx, task.ID, output, criticVerdictFence))
		subjectTaskID, perr := util.ParseUUID(stamp.OfTaskID)
		if perr != nil {
			subjectTaskID = task.ID
		}
		row, err = h.writeCriticVerdict(ctx, issue, criticVerdictWrite{
			SubjectTaskID: subjectTaskID, CriticTaskID: task.ID, Round: stamp.Round,
			Verdict: parsed.Verdict, Reason: parsed.Reason, Summary: parsed.Summary,
			Findings:  normalizeCriticFindings(parsed.Findings),
			ActorType: "agent", ActorID: task.AgentID,
		})
		if err != nil {
			slog.Warn("critic: fallback verdict write failed", "task_id", uuidToString(task.ID), "error", err)
			return
		}
	} else if err != nil {
		slog.Warn("critic: verdict lookup failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	h.applyCriticVerdict(ctx, issue, task, row)
}

type parsedCriticVerdict struct {
	Verdict  string
	Reason   string
	Summary  string
	Findings []CriticFinding
}

// parseCriticVerdictFence reads the ```critic_verdict block. Missing or
// malformed is `concerns` with reason no_verdict: a critic that said nothing
// readable has neither approved the change nor asked for another round.
func parseCriticVerdictFence(text string) parsedCriticVerdict {
	m := criticVerdictFence.FindStringSubmatch(text)
	if m == nil {
		return parsedCriticVerdict{Verdict: service.CriticVerdictConcerns, Reason: service.CriticReasonNoVerdict}
	}
	var body struct {
		Verdict  string          `json:"verdict"`
		Summary  string          `json:"summary"`
		Findings []CriticFinding `json:"findings"`
	}
	if json.Unmarshal([]byte(m[1]), &body) != nil {
		return parsedCriticVerdict{Verdict: service.CriticVerdictConcerns, Reason: service.CriticReasonNoVerdict}
	}
	out := parsedCriticVerdict{
		Verdict:  service.NormalizeCriticVerdict(strings.TrimSpace(body.Verdict)),
		Summary:  truncate(strings.TrimSpace(body.Summary), criticSummaryMax),
		Findings: body.Findings,
	}
	// An empty verdict field is silence too, whatever else the block contained.
	if strings.TrimSpace(body.Verdict) == "" {
		out.Reason = service.CriticReasonNoVerdict
	}
	return out
}

// applyCriticVerdict is the only place a verdict has a consequence. `pass` and
// `concerns` release the hold by doing nothing — the critic run is terminal,
// so the gate already opens — and only `block` relaunches the author.
func (h *Handler) applyCriticVerdict(ctx context.Context, issue db.Issue, criticTask db.AgentTaskQueue, row db.AgentCriticVerdict) {
	if row.Verdict != service.CriticVerdictBlock {
		return
	}
	policy, ok := h.criticPolicyForSubjectTask(ctx, issue, row.SubjectTaskID)
	if !ok {
		return
	}
	cost, err := h.Queries.SumIssueTaskCostTicks(ctx, issue.ID)
	if err != nil {
		cost = 0
	}
	// The next round has to fit the same budget the first one was measured
	// against; reusing DecideCritic keeps that rule in one place.
	next := int(row.Round) + 1
	if d := service.DecideCritic(service.CriticPolicyFrom(policy), next, cost, true); d.Action == service.CriticConcernsBudget {
		subjectTask := db.AgentTaskQueue{ID: row.SubjectTaskID, AgentID: criticTask.AgentID}
		h.recordPlatformCriticVerdict(ctx, issue, subjectTask, service.CriticVerdictConcerns, d.Reason, next, cost, InboxTypeCriticBudget)
		return
	}
	if ok, reason := h.TaskService.WorkflowAllowsLeg(ctx, criticTask, service.LegRoleRevision); !ok {
		slog.Info("critic: relaunch refused by the workflow limits", "issue_id", uuidToString(issue.ID), "reason", reason)
		subjectTask := db.AgentTaskQueue{ID: row.SubjectTaskID, AgentID: criticTask.AgentID}
		h.recordPlatformCriticVerdict(ctx, issue, subjectTask, service.CriticVerdictConcerns, service.CriticReasonMaxRounds, next, cost, InboxTypeCriticBudget)
		return
	}
	rework, err := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, issue, criticReworkNote(row), criticTask.OriginatorUserID)
	if err != nil {
		slog.Warn("critic: relaunch failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	if _, err := h.TaskService.StampLeg(ctx, rework, service.LegRoleRevision, criticTask); err != nil {
		slog.Warn("critic: stamp revision leg failed", "task_id", uuidToString(rework.ID), "error", err)
	}
	h.publish("critic_verdict:relaunch", uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"issue_id": uuidToString(issue.ID), "task_id": uuidToString(rework.ID), "round": int(row.Round),
	})
}

// criticPolicyForSubjectTask re-reads the policy that governed the delivery.
// It is looked up from the delivery's own run rather than from the critic's,
// because the policy belongs to the author (or its squad), never the critic.
func (h *Handler) criticPolicyForSubjectTask(ctx context.Context, issue db.Issue, subjectTaskID pgtype.UUID) (db.AgentCriticPolicy, bool) {
	subject, err := h.Queries.GetAgentTask(ctx, subjectTaskID)
	if err != nil {
		return db.AgentCriticPolicy{}, false
	}
	return h.criticPolicyFor(ctx, subject, issue.WorkspaceID)
}

// criticReworkNote is the relaunched author's brief: the critic's summary and
// every finding, in the order the critic wrote them.
func criticReworkNote(row db.AgentCriticVerdict) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The adversarial review of your last delivery blocked it (round %d). Address every point below, then deliver again.", row.Round)
	if s := strings.TrimSpace(row.Summary.String); s != "" {
		b.WriteString("\n\n## Summary\n")
		b.WriteString(s)
		b.WriteString("\n")
	}
	findings := decodeCriticFindings(row.Findings)
	if len(findings) > 0 {
		b.WriteString("\n## Findings\n")
		for _, f := range findings {
			b.WriteString("- [" + f.Severity + "] ")
			if f.File != "" {
				b.WriteString(f.File)
				if f.Line > 0 {
					fmt.Fprintf(&b, ":%d", f.Line)
				}
				b.WriteString(" — ")
			}
			b.WriteString(f.Title)
			if f.Note != "" {
				b.WriteString(": " + f.Note)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// --- writing verdicts --------------------------------------------------------

type criticVerdictWrite struct {
	SubjectTaskID pgtype.UUID
	CriticTaskID  pgtype.UUID
	Round         int
	Verdict       string
	Reason        string
	Summary       string
	Findings      []byte
	CostUsdTicks  int64
	ActorType     string
	ActorID       pgtype.UUID
}

func (h *Handler) writeCriticVerdict(ctx context.Context, issue db.Issue, w criticVerdictWrite) (db.AgentCriticVerdict, error) {
	if len(w.Findings) == 0 {
		w.Findings = []byte("[]")
	}
	round := w.Round
	if round < 1 {
		round = 1
	}
	row, err := h.Queries.CreateCriticVerdict(ctx, db.CreateCriticVerdictParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
		SubjectTaskID: w.SubjectTaskID, CriticTaskID: w.CriticTaskID, Phase: service.CriticPhaseChange,
		Verdict:  service.NormalizeCriticVerdict(w.Verdict),
		Reason:   pgtype.Text{String: w.Reason, Valid: w.Reason != ""},
		Summary:  pgtype.Text{String: w.Summary, Valid: w.Summary != ""},
		Findings: w.Findings, Round: int32(round), CostUsdTicks: w.CostUsdTicks,
		CreatedByType: pgtype.Text{String: w.ActorType, Valid: w.ActorType != ""},
		CreatedByID:   w.ActorID,
	})
	if err != nil {
		return db.AgentCriticVerdict{}, err
	}
	h.publish(EventCriticVerdictCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"issue_id": uuidToString(issue.ID),
	})
	h.audit(ctx, issue.WorkspaceID, w.ActorType, uuidToString(w.ActorID), AuditCriticVerdict, "issue", issue.ID, map[string]any{
		"verdict": row.Verdict, "reason": row.Reason.String, "round": round,
	}, nil)
	return row, nil
}

// recordPlatformCriticVerdict writes the verdict the PLATFORM produced — the
// degraded pass, the budget stop — and tells the humans why, because these are
// exactly the cases where a policy silently stopped doing what it promised.
func (h *Handler) recordPlatformCriticVerdict(ctx context.Context, issue db.Issue, task db.AgentTaskQueue, verdict, reason string, round int, cost int64, inboxType string) {
	row, err := h.writeCriticVerdict(ctx, issue, criticVerdictWrite{
		SubjectTaskID: task.ID, Round: round, Verdict: verdict, Reason: reason,
		CostUsdTicks: cost, ActorType: "system",
	})
	if err != nil {
		slog.Warn("critic: platform verdict write failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	h.fileCriticInbox(ctx, issue, row, inboxType)
}

func criticInboxTitle(reason string) string {
	switch reason {
	case service.CriticReasonNoDistinctProvider:
		return "Adversarial review skipped: no critic on another provider"
	case service.CriticReasonMaxRounds:
		return "Adversarial review stopped: round limit reached"
	case service.CriticReasonMaxCost:
		return "Adversarial review stopped: cost limit reached"
	default:
		return "Adversarial review could not be completed"
	}
}

func (h *Handler) fileCriticInbox(ctx context.Context, issue db.Issue, row db.AgentCriticVerdict, inboxType string) {
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, issue.WorkspaceID)
	if err != nil {
		slog.Warn("critic: list notification recipients failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	details, _ := json.Marshal(map[string]any{
		"verdict": row.Verdict, "reason": row.Reason.String, "round": int(row.Round),
		"cost_usd_ticks": row.CostUsdTicks, "verdict_id": uuidToString(row.ID),
	})
	title := criticInboxTitle(row.Reason.String)
	if issue.Title != "" {
		title = title + ": " + issue.Title
	}
	for _, rcpt := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, RecipientType: rcpt.Type, RecipientID: rcpt.ID,
			Type: inboxType, Severity: "attention", IssueID: issue.ID,
			Title:     truncate(title, 120),
			ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			slog.Warn("critic: inbox write failed", "issue_id", uuidToString(issue.ID), "error", err)
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(issue.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
	}
}

// --- the done gate -----------------------------------------------------------

// criticHoldBlocksDone returns the refusal reason, or "" when the move is
// allowed. It reads a live blocking critic run, not the policy: a critic that
// finished — with an answer, with a crash, or cancelled — holds nothing.
func (h *Handler) criticHoldBlocksDone(ctx context.Context, issue db.Issue, statusKey string) (string, error) {
	if issuestatus.Effective(ctx, h.Queries, issue.WorkspaceID, statusKey) != issuestatus.Done {
		return "", nil
	}
	run, err := h.Queries.GetBlockingCriticRunForIssue(ctx, issue.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load blocking critic run: %w", err)
	}
	return "adversarial review: waiting for the critic's verdict (run " + uuidToString(run.ID) + ")", nil
}

// criticHoldAllowsStatus writes the 409 and returns false when the hold
// refuses. A human is never refused: the policy governs how agents deliver,
// not what people may decide.
func (h *Handler) criticHoldAllowsStatus(w http.ResponseWriter, r *http.Request, issue db.Issue, statusKey string) bool {
	if statusKey == "" {
		return true
	}
	if _, isAgent := h.requestAgent(r); !isAgent {
		return true
	}
	reason, err := h.criticHoldBlocksDone(r.Context(), issue, statusKey)
	if err != nil {
		slog.Warn("critic hold gate failed", "error", err, "issue_id", uuidToString(issue.ID))
		writeError(w, http.StatusInternalServerError, "failed to evaluate the adversarial review hold")
		return false
	}
	if reason != "" {
		writeError(w, http.StatusConflict, reason)
		return false
	}
	return true
}

// --- HTTP --------------------------------------------------------------------

// criticSubjectFromPath validates {subjectType}/{subjectId} and checks the
// subject really is in this workspace, so a policy can never be written
// against something the caller cannot see.
func (h *Handler) criticSubjectFromPath(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (string, pgtype.UUID, bool) {
	subjectType := chi.URLParam(r, "subjectType")
	if subjectType != criticSubjectAgent && subjectType != criticSubjectSquad {
		writeError(w, http.StatusBadRequest, "subject type must be agent or squad")
		return "", pgtype.UUID{}, false
	}
	subjectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "subjectId"), "subject id")
	if !ok {
		return "", pgtype.UUID{}, false
	}
	if subjectType == criticSubjectAgent {
		if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: subjectID, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusNotFound, "agent not found")
			return "", pgtype.UUID{}, false
		}
	} else if _, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: subjectID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "squad not found")
		return "", pgtype.UUID{}, false
	}
	return subjectType, subjectID, true
}

// GetCriticPolicy: GET /api/critic-policies/{subjectType}/{subjectId}.
func (h *Handler) GetCriticPolicy(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	subjectType, subjectID, ok := h.criticSubjectFromPath(w, r, wsUUID)
	if !ok {
		return
	}
	row, err := h.Queries.GetCriticPolicy(r.Context(), db.GetCriticPolicyParams{WorkspaceID: wsUUID, SubjectType: subjectType, SubjectID: subjectID})
	if err != nil {
		writeJSON(w, http.StatusOK, defaultCriticPolicyResponse(subjectType, uuidToString(subjectID)))
		return
	}
	writeJSON(w, http.StatusOK, criticPolicyToResponse(row, subjectType, uuidToString(subjectID)))
}

type criticPolicyRequest struct {
	Enabled                 bool     `json:"enabled"`
	CriticAgentID           string   `json:"critic_agent_id"`
	RequireDistinctProvider *bool    `json:"require_distinct_provider"`
	Blocking                bool     `json:"blocking"`
	MaxRounds               *int     `json:"max_rounds"`
	MaxCostUsdTicks         *int64   `json:"max_cost_usd_ticks"`
	Phases                  []string `json:"phases"`
}

// PutCriticPolicy: PUT /api/critic-policies/{subjectType}/{subjectId}.
//
// An enabled policy MUST name its critic. "Pick one for me" is exactly what
// K15's cross-review already does; F25 exists to be the explicit choice, and a
// policy that silently chose would be a second, quieter cross-review.
func (h *Handler) PutCriticPolicy(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	subjectType, subjectID, ok := h.criticSubjectFromPath(w, r, wsUUID)
	if !ok {
		return
	}
	var req criticPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var criticID pgtype.UUID
	if raw := strings.TrimSpace(req.CriticAgentID); raw != "" {
		parsed, ok := parseUUIDOrBadRequest(w, raw, "critic agent id")
		if !ok {
			return
		}
		if subjectType == criticSubjectAgent && parsed == subjectID {
			writeErrorCode(w, http.StatusUnprocessableEntity, ErrCodeCriticIsAuthor, "an agent cannot be its own critic")
			return
		}
		if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: parsed, WorkspaceID: wsUUID}); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, ErrCodeCriticRequired, "the critic agent is not in this workspace")
			return
		}
		criticID = parsed
	}
	if req.Enabled && !criticID.Valid {
		writeErrorCode(w, http.StatusUnprocessableEntity, ErrCodeCriticRequired, "an enabled critic policy must name its critic agent")
		return
	}
	requireDistinct := true
	if req.RequireDistinctProvider != nil {
		requireDistinct = *req.RequireDistinctProvider
	}
	maxRounds := 1
	if req.MaxRounds != nil {
		maxRounds = *req.MaxRounds
	}
	if maxRounds < 1 || maxRounds > 10 {
		writeError(w, http.StatusUnprocessableEntity, "max_rounds must be between 1 and 10")
		return
	}
	var maxCost pgtype.Int8
	if req.MaxCostUsdTicks != nil && *req.MaxCostUsdTicks > 0 {
		maxCost = pgtype.Int8{Int64: *req.MaxCostUsdTicks, Valid: true}
	}
	phases := req.Phases
	if len(phases) == 0 {
		phases = []string{service.CriticPhaseChange}
	}
	row, err := h.Queries.UpsertCriticPolicy(r.Context(), db.UpsertCriticPolicyParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, SubjectType: subjectType, SubjectID: subjectID,
		Enabled: req.Enabled, CriticAgentID: criticID, RequireDistinctProvider: requireDistinct,
		Blocking: req.Blocking, MaxRounds: int16(maxRounds), MaxCostUsdTicks: maxCost, Phases: phases,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the critic policy")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditCriticPolicyChanged, subjectType, subjectID, map[string]any{
		"enabled": row.Enabled, "blocking": row.Blocking, "max_rounds": int(row.MaxRounds),
		"critic_agent_id": uuidToString(row.CriticAgentID),
	}, nil)
	writeJSON(w, http.StatusOK, criticPolicyToResponse(row, subjectType, uuidToString(subjectID)))
}

// ListCriticVerdicts: GET /api/issues/{id}/critic-verdicts — round order.
func (h *Handler) ListCriticVerdicts(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListCriticVerdictsForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list critic verdicts")
		return
	}
	out := make([]CriticVerdictResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, criticVerdictToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"verdicts": out})
}

type criticVerdictRequest struct {
	Verdict  string          `json:"verdict"`
	Summary  string          `json:"summary"`
	Findings []CriticFinding `json:"findings"`
}

// CreateCriticVerdict: POST /api/issues/{id}/critic-verdicts.
//
// Authorization is the run's own task token, like the run plan: the verdict is
// what the CRITIC concluded, so neither a human nor another agent may write
// one in its name. The row is recorded here; the consequence — relaunching the
// author on `block` — is applied when the critic run completes, so there is
// exactly one place where a verdict acts.
func (h *Handler) CreateCriticVerdict(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "only the critic run itself can record its verdict")
		return
	}
	callerTaskID, err := util.ParseUUID(strings.TrimSpace(r.Header.Get("X-Task-ID")))
	if err != nil {
		writeError(w, http.StatusForbidden, "only the critic run itself can record its verdict")
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), callerTaskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	// The issue was already loaded through the membership loader, so pinning
	// the run to it is the whole workspace check: a run of another workspace
	// cannot be on this issue.
	stamp, isCritic := service.TaskCriticOf(task.Context)
	if !isCritic || task.IssueID != issue.ID {
		writeError(w, http.StatusForbidden, "this run is not the critic of this issue")
		return
	}
	if _, err := h.Queries.GetCriticVerdictByCriticTask(r.Context(), task.ID); err == nil {
		writeError(w, http.StatusConflict, "this critic run already recorded its verdict")
		return
	}
	var req criticVerdictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	subjectTaskID, perr := util.ParseUUID(stamp.OfTaskID)
	if perr != nil {
		subjectTaskID = task.ID
	}
	row, err := h.writeCriticVerdict(r.Context(), issue, criticVerdictWrite{
		SubjectTaskID: subjectTaskID, CriticTaskID: task.ID, Round: stamp.Round,
		Verdict:   service.NormalizeCriticVerdict(strings.TrimSpace(req.Verdict)),
		Summary:   truncate(strings.TrimSpace(req.Summary), criticSummaryMax),
		Findings:  normalizeCriticFindings(req.Findings),
		ActorType: "agent", ActorID: task.AgentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the verdict")
		return
	}
	writeJSON(w, http.StatusCreated, criticVerdictToResponse(row))
}
