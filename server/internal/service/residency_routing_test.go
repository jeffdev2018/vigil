package service

import (
	"context"
	"math/rand"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Data residency (K46) inside the runtime router (JEF-237). The filter runs
// BEFORE scoring, so a rejected runtime cannot win on statistics: the point of
// the feature is that a better-performing machine in the wrong place still
// does not get the work.

// setResidencyPolicy writes the policy into the fixture workspace's settings.
func setResidencyPolicy(t *testing.T, fx *testutil.Fixture, wsID, policyJSON string) {
	t.Helper()
	fx.Exec(t, `UPDATE workspace SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{data_residency_policy}', $2::jsonb) WHERE id = $1`, wsID, policyJSON)
}

func TestRouteTaskExcludesRuntimesTheResidencyPolicyRejects(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	// The banned provider has the BETTER record, so a router that ignored the
	// policy would pick it. Runtime A stays "codex" (banned), runtime B moves
	// to "claude".
	dbfx.Exec(t, `UPDATE agent_runtime SET provider = 'claude' WHERE id = $1`, fx.runtimeB)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeA, TaskClassGeneral, "openai", "m-a", 20, 20)
	seedRoutingRuns(t, dbfx, fx.agentID, fx.runtimeB, TaskClassGeneral, "openai", "m-b", 20, 12)
	setResidencyPolicy(t, dbfx, fx.workspace, `{"banned_providers":["codex"],"region_allowlist":[],"require_on_prem":false}`)

	ctx := context.Background()
	svc := &TaskService{Queries: db.New(fx.pool), RoutingRand: rand.New(rand.NewSource(1))}
	agent, err := svc.Queries.GetAgent(ctx, util.MustParseUUID(fx.agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	filter := svc.compliantRuntimeFilter(ctx, util.MustParseUUID(fx.workspace))
	if filter == nil {
		t.Fatal("a workspace with a banned provider must produce a filter")
	}
	decision := svc.RouteTask(ctx, agent, "Improve onboarding flow", nil, filter)

	if decision.ChosenRuntimeID != fx.runtimeB {
		t.Errorf("chosen runtime = %s, want the compliant runtime %s (the banned one had the better record)", decision.ChosenRuntimeID, fx.runtimeB)
	}
	// The rejected runtime is still traced, with the reason it was dropped —
	// a decision nobody can audit is not a decision.
	var excluded *RoutingCandidateTrace
	for i := range decision.Candidates {
		if decision.Candidates[i].RuntimeID == fx.runtimeA {
			excluded = &decision.Candidates[i]
		}
	}
	if excluded == nil {
		t.Fatalf("the rejected runtime must still appear in the trace: %+v", decision.Candidates)
	}
	if excluded.ExcludedReason != ResidencyReasonBannedProvider {
		t.Errorf("excluded_reason = %q, want %q", excluded.ExcludedReason, ResidencyReasonBannedProvider)
	}
	if excluded.Score != nil {
		t.Error("a runtime the policy rejects must never be scored")
	}
}

func TestCompliantRuntimeFilterIsNilWithoutAPolicy(t *testing.T) {
	fx := newRoutingTestFixture(t)
	svc := &TaskService{Queries: db.New(fx.pool)}
	if f := svc.compliantRuntimeFilter(context.Background(), util.MustParseUUID(fx.workspace)); f != nil {
		t.Error("a workspace with no policy must skip the filter entirely")
	}
	// An empty policy is still no constraint: writing one must not start
	// refusing every undeclared runtime in the workspace.
	setResidencyPolicy(t, testutil.New(fx.pool, fx.workspace, fx.user), fx.workspace,
		`{"region_allowlist":[],"banned_providers":[],"require_on_prem":false}`)
	if f := svc.compliantRuntimeFilter(context.Background(), util.MustParseUUID(fx.workspace)); f != nil {
		t.Error("an empty policy must be treated as no constraint")
	}
}

func TestCompliantRuntimeFilterFailsClosedOnUndeclaredRuntimes(t *testing.T) {
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	setResidencyPolicy(t, dbfx, fx.workspace, `{"region_allowlist":["eu-west-1"],"banned_providers":[],"require_on_prem":false}`)
	// The table has no id column (its uniqueness is the concurrent index from
	// migration 702), so the generic Insert helper does not apply.
	dbfx.Exec(t, `INSERT INTO runtime_compliance_profile (runtime_id, region, on_prem) VALUES ($1, 'eu-west-1', false)`, fx.runtimeB)
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM runtime_compliance_profile WHERE runtime_id = $1`, fx.runtimeB)
	})

	ctx := context.Background()
	svc := &TaskService{Queries: db.New(fx.pool)}
	filter := svc.compliantRuntimeFilter(ctx, util.MustParseUUID(fx.workspace))
	if filter == nil {
		t.Fatal("a region allowlist must produce a filter")
	}
	undeclared, err := svc.Queries.GetAgentRuntime(ctx, util.MustParseUUID(fx.runtimeA))
	if err != nil {
		t.Fatalf("get runtime a: %v", err)
	}
	declared, err := svc.Queries.GetAgentRuntime(ctx, util.MustParseUUID(fx.runtimeB))
	if err != nil {
		t.Fatalf("get runtime b: %v", err)
	}
	if ok, reason := filter(undeclared); ok || reason != ResidencyReasonRegionNotAllowed {
		t.Errorf("an undeclared runtime must be refused, got ok=%v reason=%q", ok, reason)
	}
	if ok, reason := filter(declared); !ok {
		t.Errorf("a runtime declared in an allowed region must pass, got reason %q", reason)
	}
}
