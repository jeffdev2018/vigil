package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The claim's serialization key, and what F11 changed about it.
//
// ClaimAgentTask refuses a task while another task of the SAME agent is active
// on the same issue / chat session / quick-create shape. Racing attempts
// (F11 / JEF-6) add one exception and one only: two rows of the SAME
// run_group do not exclude each other. Everything else — including a grouped
// run against an ungrouped one, and two runs of two different groups — is
// serialized exactly as it was.
//
// These tests were written before the rest of the feature existed and are the
// non-regression net for the one query the whole of dispatch goes through.

// claimOnce runs one claim in its own transaction and rolls it back, so a claim
// made here never leaves a dispatched row behind for the next assertion. It
// returns the claimed task id, or "" when nothing was claimable.
func claimOnce(t *testing.T, agentID, runtimeID string) string {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin claim tx: %v", err)
	}
	defer tx.Rollback(context.Background())
	claimed, err := testHandler.Queries.WithTx(tx).ClaimAgentTask(ctx, db.ClaimAgentTaskParams{
		AgentID:          parseUUID(agentID),
		RuntimeID:        parseUUID(runtimeID),
		PrepareLeaseSecs: 30,
		RuntimeStaleSecs: service.RuntimeClaimFreshnessSeconds,
	})
	if err != nil {
		return ""
	}
	return uuidToString(claimed.ID)
}

// markActive puts a task in the state the exclusion looks for, without going
// through a claim: the point of these tests is what the NEXT claim decides.
//
// 'running' rather than 'dispatched' on purpose. Both are excluded by the
// claim, but only 'dispatched' sits in the pending-slot unique index, so a
// dispatched row would make the SECOND task of these tests un-insertable and
// the assertion would never be reached. 'running' is also the real shape of
// the case: an agent executing a run while another is queued behind it.
func markActive(t *testing.T, fx *testutil.Fixture, taskID string) {
	t.Helper()
	fx.Exec(t, `UPDATE agent_task_queue SET status = 'running', dispatched_at = now(), started_at = now() WHERE id = $1`, taskID)
}

// Acceptance 1 + 2: N attempts of one group are claimable together, including
// two attempts of the same agent differing only by model_override. Without the
// F11 conjunct the second claim returns nothing, because the first attempt is
// already dispatched on the same (issue, agent).
func TestClaimAgentTask_SameRunGroupAttemptsAreConcurrent(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "claim-group-runtime")
	agentID := fx.Agent(t, "claim-group-agent", runtimeID)
	issueID := fx.Issue(t, "three attempts race here")
	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 3,
	})

	attempts := make([]string, 0, 3)
	for _, model := range []any{nil, "sonnet", "opus"} {
		attempts = append(attempts, fx.Task(t, agentID, testutil.Cols{
			"issue_id":       issueID,
			"runtime_id":     runtimeID,
			"run_group_id":   groupID,
			"model_override": model,
		}))
	}

	// Claim them one after another, leaving each dispatched, and check every
	// later attempt is still reachable.
	claimedIDs := map[string]bool{}
	for i := range attempts {
		got := claimOnce(t, agentID, runtimeID)
		if got == "" {
			t.Fatalf("attempt %d of the group was not claimable; the group's earlier attempts must not exclude it", i+1)
		}
		if claimedIDs[got] {
			t.Fatalf("attempt %d claimed task %s twice", i+1, got)
		}
		claimedIDs[got] = true
		markActive(t, fx, got)
	}
	for _, id := range attempts {
		if !claimedIDs[id] {
			t.Fatalf("attempt %s never became claimable; claimed set = %v", id, claimedIDs)
		}
	}
}

// Acceptance 3, the regression that matters most: two runs of the same agent on
// the same issue and OUTSIDE any group are still serialized. This is the rule
// F11 relaxes for groups and must not relax for anything else.
func TestClaimAgentTask_UngroupedRunsOnOneIssueStaySerialized(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "claim-serial-runtime")
	agentID := fx.Agent(t, "claim-serial-agent", runtimeID)
	issueID := fx.Issue(t, "ordinary runs still queue behind each other")

	first := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
	// A second pending row for the same (issue, agent) is only insertable
	// because the first is now 'running', which the pending-slot index does not
	// cover — the index still owns the queued/dispatched slot for ungrouped
	// rows, which migration 835 preserves and the last test here proves.
	markActive(t, fx, first)
	second := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})

	if got := claimOnce(t, agentID, runtimeID); got != "" {
		t.Fatalf("claimed %s while an ungrouped run of the same agent was active on the issue; want nothing (second=%s)", got, second)
	}
}

// A grouped attempt and an ordinary run on the same issue still exclude each
// other. This is the case a naive `active.run_group_id = atq.run_group_id`
// silently breaks: NULL = <group> is NULL, the subquery row is not selected,
// and the exclusion disappears.
func TestClaimAgentTask_GroupedAttemptWaitsForAnUngroupedRun(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "claim-mixed-runtime")
	agentID := fx.Agent(t, "claim-mixed-agent", runtimeID)
	issueID := fx.Issue(t, "a race must not run beside an ordinary run")
	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 1,
	})

	ordinary := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
	markActive(t, fx, ordinary)
	fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "run_group_id": groupID})

	if got := claimOnce(t, agentID, runtimeID); got != "" {
		t.Fatalf("claimed grouped attempt %s while an ungrouped run was active on the same issue; want nothing", got)
	}
}

// The mirror: an ordinary run does not slip past an active grouped attempt
// either. Same NULL trap, read from the other side.
func TestClaimAgentTask_UngroupedRunWaitsForAnActiveGroupedAttempt(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "claim-mixed2-runtime")
	agentID := fx.Agent(t, "claim-mixed2-agent", runtimeID)
	issueID := fx.Issue(t, "an ordinary run must not join a running race")
	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 1,
	})

	attempt := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "run_group_id": groupID})
	markActive(t, fx, attempt)
	fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})

	if got := claimOnce(t, agentID, runtimeID); got != "" {
		t.Fatalf("claimed ungrouped run %s while a grouped attempt was active on the same issue; want nothing", got)
	}
}

// Two DIFFERENT groups on one issue are serialized against each other. The
// relaxation is per group, not "grouped rows ignore each other".
func TestClaimAgentTask_TwoDifferentGroupsStaySerialized(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "claim-twogroups-runtime")
	agentID := fx.Agent(t, "claim-twogroups-agent", runtimeID)
	issueID := fx.Issue(t, "two races do not overlap")
	group := func() string {
		return fx.Insert(t, "run_group", testutil.Cols{
			"workspace_id":  testWorkspaceID,
			"issue_id":      issueID,
			"created_by":    testUserID,
			"attempt_count": 1,
		})
	}

	first := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "run_group_id": group()})
	markActive(t, fx, first)
	fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "run_group_id": group()})

	if got := claimOnce(t, agentID, runtimeID); got != "" {
		t.Fatalf("claimed attempt %s of a second group while another group was active on the issue; want nothing", got)
	}
}

// Chat serialization is untouched: a chat task has no issue, so it serializes
// on chat_session_id, and no run_group_id is involved either way.
func TestClaimAgentTask_ChatSerializationUnchangedByRunGroups(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "claim-chat-runtime")
	agentID := fx.Agent(t, "claim-chat-agent", runtimeID)
	sessionID := fx.Insert(t, "chat_session", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"creator_id":   testUserID,
		"agent_id":     agentID,
		"title":        "claim serialization",
	})

	first := fx.Task(t, agentID, testutil.Cols{"chat_session_id": sessionID, "runtime_id": runtimeID})
	markActive(t, fx, first)
	fx.Task(t, agentID, testutil.Cols{"chat_session_id": sessionID, "runtime_id": runtimeID})

	if got := claimOnce(t, agentID, runtimeID); got != "" {
		t.Fatalf("claimed chat task %s while another task of the same session was active; want nothing", got)
	}
}

// Quick-create serialization is untouched: all four source links NULL on both
// sides, run_group_id absent, exclusion holds.
func TestClaimAgentTask_QuickCreateSerializationUnchangedByRunGroups(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "claim-quick-runtime")
	agentID := fx.Agent(t, "claim-quick-agent", runtimeID)

	first := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	markActive(t, fx, first)
	fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})

	if got := claimOnce(t, agentID, runtimeID); got != "" {
		t.Fatalf("claimed quick-create task %s while another was active for the same agent; want nothing", got)
	}
}

// A private runtime still only serves its owner's agents, group or no group.
// The F11 conjunct sits inside the exclusion, not in the visibility gate, but
// this is the threat model a rewrite of the query would most easily undo.
func TestClaimAgentTask_PrivateRuntimeStillRefusesAForeignAgentsGroupedAttempt(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	otherUser := fx.User(t, "Claim Group Outsider", "claim-group-outsider@example.com")
	runtimeID := fx.Runtime(t, "claim-private-runtime", testutil.Cols{"visibility": "private", "owner_id": otherUser})
	agentID := fx.Agent(t, "claim-private-agent", runtimeID, testutil.Cols{"owner_id": testUserID})
	issueID := fx.Issue(t, "a private runtime is not opened by racing")
	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 1,
	})
	fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "run_group_id": groupID})

	if got := claimOnce(t, agentID, runtimeID); got != "" {
		t.Fatalf("claimed %s on a private runtime owned by another user; want nothing", got)
	}
}

// An offline runtime serves nothing, grouped attempts included.
func TestClaimAgentTask_OfflineRuntimeRefusesAGroupedAttempt(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtimeID := fx.Runtime(t, "claim-offline-runtime", testutil.Cols{"status": "offline"})
	agentID := fx.Agent(t, "claim-offline-agent", runtimeID)
	issueID := fx.Issue(t, "an offline runtime runs no attempt")
	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 1,
	})
	fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "run_group_id": groupID})

	if got := claimOnce(t, agentID, runtimeID); got != "" {
		t.Fatalf("claimed %s on an offline runtime; want nothing", got)
	}
}

// The pending-slot index keeps its rule for ungrouped rows (migration 835) and
// lets a group insert its attempts (migration 836). Both halves in one place:
// they are the INSERT-time counterpart of the claim's exclusion, and relaxing
// one without the other makes the feature fail earlier rather than differently.
func TestPendingTaskSlot_UngroupedStillUniqueGroupedIsNot(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	ctx := context.Background()
	runtimeID := fx.Runtime(t, "pending-slot-runtime")
	agentID := fx.Agent(t, "pending-slot-agent", runtimeID)
	issueID := fx.Issue(t, "one pending slot, except for a race")

	fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority) VALUES ($1, $2, $3, 'queued', 0)`,
		agentID, runtimeID, issueID); err == nil {
		t.Fatal("a second ungrouped pending task was inserted for the same (issue, agent); the pending-slot index no longer holds")
	}

	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 2,
	})
	fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "run_group_id": groupID})
	fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "run_group_id": groupID, "model_override": "opus"})
}
