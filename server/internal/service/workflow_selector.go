package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Workflow selector (JEF-273). Above the runtime router (which picks the
// runtime a task runs on), this layer picks the WORKFLOW of each issue task:
//
//	single   — one direct run (the pre-JEF-273 behavior)
//	cascade  — start on the cheapest proven runtime; the low-confidence
//	           escalation climbs from there
//	critique — the run is force-flagged for an independent cross-review
//	           (JEF-238) even without a project gate
//
// The cardinal rule, same discipline as the runtime router (JEF-237): no data
// means no behavior change. The policy defaults to off; in auto mode a
// workflow is only eligible with workflowMinSamples terminal runs of its task
// class on record, the success estimate is the Wilson lower bound, and any
// degradation (no eligible workflow, no cheap runtime, no available reviewer)
// settles on single.
const (
	WorkflowSingle   = "single"
	WorkflowCascade  = "cascade"
	WorkflowCritique = "critique"
)

// Workflow policy modes (workspace settings key "workflow_policy").
const (
	WorkflowPolicyModeOff  = "off"
	WorkflowPolicyModeAuto = "auto"
)

// Workflow selection reasons, surfaced on the task:workflow-selected event.
const (
	workflowReasonPolicyOff        = "policy:off-default"
	workflowReasonPolicyAuto       = "policy:auto"
	workflowReasonInsufficientData = "auto:insufficient-data"
)

// Workflow selector tuning knobs, mirroring the router's discipline.
const (
	// workflowMinSamples is the sample floor for a (task_class, workflow)
	// bucket to be eligible. Below it the workflow does not exist as far as
	// the selector is concerned.
	workflowMinSamples = 5
	// workflowWilsonSlack is how far below the best Wilson lower bound a
	// workflow may sit and still be considered equally good — inside that
	// band the cheapest one wins.
	workflowWilsonSlack = 0.05
	// workflowExplorationEpsilon is the probability of skipping the chosen
	// workflow in favor of the least-sampled one, so cascade and critique can
	// accrue the samples they need to become eligible.
	workflowExplorationEpsilon = 0.10
	// workflowStatsWindow mirrors the router's 90-day lookback.
	workflowStatsWindow = 90 * 24 * time.Hour
)

// WorkflowPolicy is the workspace's workflow_policy settings value:
//
//	"workflow_policy": {"mode": "off" | "auto"}
//
// Off by default: a workspace opts into learned workflow selection
// explicitly.
type WorkflowPolicy struct {
	Mode string `json:"mode"`
}

var DefaultWorkflowPolicy = WorkflowPolicy{Mode: WorkflowPolicyModeOff}

// ValidWorkflowPolicyMode reports whether mode is a known policy mode.
func ValidWorkflowPolicyMode(mode string) bool {
	return mode == WorkflowPolicyModeOff || mode == WorkflowPolicyModeAuto
}

// WorkflowPolicySettings reads the workflow_policy key of a workspace
// settings blob; anything missing or malformed yields the default (off).
func WorkflowPolicySettings(settings []byte) WorkflowPolicy {
	out := DefaultWorkflowPolicy
	if len(settings) == 0 {
		return out
	}
	var s struct {
		WorkflowPolicy *WorkflowPolicy `json:"workflow_policy"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.WorkflowPolicy == nil {
		return out
	}
	if ValidWorkflowPolicyMode(s.WorkflowPolicy.Mode) {
		out.Mode = s.WorkflowPolicy.Mode
	}
	return out
}

// workflowOption is one workflow's track record on the requested task class.
type workflowOption struct {
	workflow  string
	samples   int
	successes int
	wilson    float64
	avgCost   *float64
}

// SelectWorkflow picks the execution workflow for one issue task:
// "single", "cascade" or "critique". It never fails and never invents data:
// policy off, no settings, a failed stats query or too little history all
// yield single. Degradations are part of the choice: cascade with no cheap
// proven runtime to start on, and critique with no independent reviewer
// available, both fall back to single.
func (s *TaskService) SelectWorkflow(ctx context.Context, agent db.Agent, issue db.Issue, taskClass string) string {
	workflow, _ := s.selectWorkflow(ctx, agent, issue, taskClass)
	return workflow
}

// selectWorkflow is SelectWorkflow plus the decision reason for the
// task:workflow-selected event.
func (s *TaskService) selectWorkflow(ctx context.Context, agent db.Agent, issue db.Issue, taskClass string) (string, string) {
	ws, err := s.Queries.GetWorkspace(ctx, agent.WorkspaceID)
	if err != nil {
		slog.Warn("workflow selector: workspace lookup failed; staying single",
			"agent_id", util.UUIDToString(agent.ID), "error", err)
		return WorkflowSingle, workflowReasonPolicyOff
	}
	if WorkflowPolicySettings(ws.Settings).Mode != WorkflowPolicyModeAuto {
		return WorkflowSingle, workflowReasonPolicyOff
	}

	stats, err := s.Queries.GetWorkflowStats(ctx, db.GetWorkflowStatsParams{
		WorkspaceID: agent.WorkspaceID,
		Since:       pgtype.Timestamptz{Time: time.Now().Add(-workflowStatsWindow), Valid: true},
	})
	if err != nil {
		slog.Warn("workflow selector: stats query failed; staying single",
			"agent_id", util.UUIDToString(agent.ID), "error", err)
		return WorkflowSingle, workflowReasonInsufficientData
	}

	chosen := chooseWorkflow(workflowOptionsFor(stats, taskClass), s.routingRand())
	if chosen == "" {
		return WorkflowSingle, workflowReasonInsufficientData
	}

	// Degradations: a workflow whose enabling condition is not actually
	// met in this workspace right now collapses to single.
	switch chosen {
	case WorkflowCascade:
		if _, ok := s.cheapCascadeCandidate(ctx, agent, taskClass); !ok {
			return WorkflowSingle, workflowReasonPolicyAuto
		}
	case WorkflowCritique:
		if !s.crossReviewerAvailable(ctx, agent) {
			return WorkflowSingle, workflowReasonPolicyAuto
		}
	}
	return chosen, workflowReasonPolicyAuto
}

// workflowOptionsFor projects the stats rows of one task class into the
// selector's option set. Only rows naming a known workflow participate — an
// unknown token in old data is ignored, not trusted.
func workflowOptionsFor(stats []db.GetWorkflowStatsRow, taskClass string) []workflowOption {
	options := []workflowOption{}
	for _, row := range stats {
		if row.TaskClass != taskClass {
			continue
		}
		switch row.Workflow {
		case WorkflowSingle, WorkflowCascade, WorkflowCritique:
		default:
			continue
		}
		opt := workflowOption{
			workflow:  row.Workflow,
			samples:   int(row.Samples),
			successes: int(row.SuccessCount),
			wilson:    wilsonLowerBound(int(row.SuccessCount), int(row.Samples)),
		}
		if row.CostSamples > 0 {
			avg := row.TotalCostUsdTicks / float64(row.CostSamples) * costTicksPerUSD
			opt.avgCost = &avg
		}
		options = append(options, opt)
	}
	return options
}

// chooseWorkflow picks among the options of one task class. Eligible means
// ≥ workflowMinSamples samples. The pick is the CHEAPEST eligible workflow
// whose Wilson lower bound is within workflowWilsonSlack of the best — cost
// is the differentiator only among equally trustworthy workflows; without
// cost data on the whole band the best Wilson lower bound wins. With
// probability epsilon the least-sampled known workflow is explored instead,
// but only once at least one workflow is eligible (same anti-cold-start rule
// as the router). "" means no eligible workflow at all.
func chooseWorkflow(options []workflowOption, rnd *rand.Rand) string {
	eligible := []workflowOption{}
	for _, o := range options {
		if o.samples >= workflowMinSamples {
			eligible = append(eligible, o)
		}
	}
	if len(eligible) == 0 {
		return ""
	}

	if rnd.Float64() < workflowExplorationEpsilon {
		least := ""
		leastSamples := -1
		for _, name := range []string{WorkflowCascade, WorkflowCritique, WorkflowSingle} {
			samples := 0
			for _, o := range options {
				if o.workflow == name {
					samples = o.samples
					break
				}
			}
			if leastSamples < 0 || samples < leastSamples {
				least, leastSamples = name, samples
			}
		}
		return least
	}

	best := eligible[0]
	for _, o := range eligible[1:] {
		if o.wilson > best.wilson {
			best = o
		}
	}
	bar := best.wilson - workflowWilsonSlack
	chosen := best
	chosenCost := best.avgCost
	for _, o := range eligible {
		if o.wilson < bar || o.avgCost == nil {
			continue
		}
		// Prefer the cheaper one; an option with cost data beats the
		// cost-unknown best-wilson default, and ties break on the higher
		// lower bound then on a stable name order.
		if chosenCost == nil ||
			*o.avgCost < *chosenCost ||
			(*o.avgCost == *chosenCost && (o.wilson > chosen.wilson ||
				(o.wilson == chosen.wilson && o.workflow < chosen.workflow))) {
			chosen, chosenCost = o, o.avgCost
		}
	}
	return chosen.workflow
}

// cheapCascadeCandidate finds the runtime a cascade workflow starts on: the
// cheapest (avg cost) routing candidate that is proven for the task class —
// ≥ routingMinScoredSamples samples, a success rate of at least 0.5, and not
// excluded by the router's floor guard. A candidate without cost data cannot
// be "the cheapest", so it never qualifies. False means the cascade cannot
// start cheap and the caller falls back to normal routing.
func (s *TaskService) cheapCascadeCandidate(ctx context.Context, agent db.Agent, taskClass string) (pgtype.UUID, bool) {
	runtimes, err := s.Queries.ListRoutingCandidateRuntimes(ctx, db.ListRoutingCandidateRuntimesParams{
		WorkspaceID:      agent.WorkspaceID,
		OwnerID:          agent.OwnerID,
		RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
	})
	if err != nil || len(runtimes) == 0 {
		return pgtype.UUID{}, false
	}
	stats, err := s.Queries.GetRoutingStats(ctx, db.GetRoutingStatsParams{
		WorkspaceID: agent.WorkspaceID,
		Since:       pgtype.Timestamptz{Time: time.Now().Add(-routingStatsWindow), Valid: true},
	})
	if err != nil {
		return pgtype.UUID{}, false
	}
	candidates := buildRoutingCandidates(agent, runtimes, stats, taskClass)
	scoreRoutingCandidates(candidates) // applies the exclusion guard
	var cheapest *routingCandidate
	for _, c := range candidates {
		if c.trace.ExcludedReason != "" ||
			c.trace.Samples < routingMinScoredSamples ||
			c.trace.SuccessRate < 0.5 ||
			c.trace.AvgCostUSD == nil {
			continue
		}
		if cheapest == nil || *c.trace.AvgCostUSD < *cheapest.trace.AvgCostUSD {
			cheapest = c
		}
	}
	if cheapest == nil {
		return pgtype.UUID{}, false
	}
	return cheapest.runtimeID, true
}

// crossReviewerAvailable replicates the failure condition of the handler's
// reviewer choice (ListCrossReviewCandidates comes back empty): no live
// workspace agent other than this one on a different (runtime, model) pair.
// A critique workflow without an independent reviewer is no critique at all.
func (s *TaskService) crossReviewerAvailable(ctx context.Context, agent db.Agent) bool {
	provider := ""
	if rt, err := s.Queries.GetAgentRuntime(ctx, agent.RuntimeID); err == nil {
		provider = rt.Provider
	}
	candidates, err := s.Queries.ListCrossReviewCandidates(ctx, db.ListCrossReviewCandidatesParams{
		WorkspaceID:     agent.WorkspaceID,
		AuthorAgentID:   agent.ID,
		AuthorRuntimeID: agent.RuntimeID,
		AuthorModel:     agent.Model.String,
		AuthorProvider:  provider,
	})
	if err != nil {
		slog.Warn("workflow selector: reviewer availability query failed; critique unavailable",
			"agent_id", util.UUIDToString(agent.ID), "error", err)
		return false
	}
	return len(candidates) > 0
}

// publishWorkflowSelected broadcasts the selector's decision for one freshly
// enqueued issue task. The reason names the policy branch that produced it
// (policy:auto, policy:off-default, auto:insufficient-data).
func (s *TaskService) publishWorkflowSelected(issue db.Issue, task db.AgentTaskQueue, workflow, reason string) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskWorkflowSelected,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  util.UUIDToString(task.ID),
			"issue_id": util.UUIDToString(issue.ID),
			"workflow": workflow,
			"reason":   reason,
		},
	})
}

// TaskWorkflow reads the workflow stamp of a task's context, "" when the row
// predates the selector or the stamp is unreadable.
func TaskWorkflow(contextJSON []byte) string {
	if len(contextJSON) == 0 {
		return ""
	}
	var c struct {
		Workflow string `json:"workflow"`
	}
	if json.Unmarshal(contextJSON, &c) != nil {
		return ""
	}
	return c.Workflow
}

// TaskForceReview reports whether the task's context stamp demands a
// cross-review regardless of any project/workspace review configuration.
func TaskForceReview(contextJSON []byte) bool {
	if len(contextJSON) == 0 {
		return false
	}
	var c struct {
		ForceReview bool `json:"force_review"`
	}
	if json.Unmarshal(contextJSON, &c) != nil {
		return false
	}
	return c.ForceReview
}
