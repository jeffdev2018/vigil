package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F01 (JEF-5): the delegate names the assignee's partner and MUST NOT start a
// run. WillEnqueueRun is the single predicate that decides whether an issue
// write enqueues one, so "a delegate is inert" is a statement about this
// function: it reads issue.AssigneeType / AssigneeID and the three change
// flags, and nothing else.
//
// The test is written against the predicate rather than against the HTTP flow
// so it fails on the day someone adds a `DelegateChanged` input to
// IssueTriggerInput, which is the realistic way this promise gets broken.
// The end-to-end half — no agent_task_queue row, no task:queued — lives in
// internal/handler/issue_delegate_test.go, where a real queue exists.

func delegateOnlyIssue(delegateType string, delegateID pgtype.UUID) db.Issue {
	return db.Issue{
		ID:           pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID:  pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		Status:       "todo",
		DelegateType: pgtype.Text{String: delegateType, Valid: true},
		DelegateID:   delegateID,
	}
}

func TestWillEnqueueRunIgnoresDelegate(t *testing.T) {
	agentID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	// A nil Queries is deliberate: reaching a query at all would mean the
	// delegate had been treated as a run candidate.
	svc := &IssueService{}

	cases := map[string]IssueTriggerInput{
		// The shape the feature actually produces: a delegate is named on an
		// unassigned issue. Nothing to run, nobody to run it.
		"delegate on an unassigned issue": {
			Issue: delegateOnlyIssue("agent", agentID),
		},
		// The dangerous shape: an AGENT delegate on an unassigned issue. If
		// the predicate ever fell back to "any agent named on the issue",
		// this is the case that would silently start dispatching runs.
		"agent delegate, create": {
			Issue:    delegateOnlyIssue("agent", agentID),
			IsCreate: true,
		},
		// A delegate write leaves the issue out of backlog with no assignee
		// change; the status source must not pick it up either.
		"agent delegate, status moved out of backlog": {
			Issue:         delegateOnlyIssue("agent", agentID),
			PrevStatus:    "backlog",
			StatusChanged: true,
		},
		// Same, but with a member delegate — the branch a human partner takes.
		"member delegate, create": {
			Issue:      delegateOnlyIssue("member", agentID),
			PrevStatus: "todo",
			IsCreate:   true,
		},
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			trigger, ok := svc.WillEnqueueRun(context.Background(), in, IssueTriggerProbe{})
			if ok {
				t.Fatalf("a delegate started a run: trigger = %+v", trigger)
			}
		})
	}
}

// The mirror assertion: the delegate does not SUPPRESS a run either. An issue
// that would have started one because of its assignee still does when it also
// carries a delegate, so the feature is additive rather than a new gate.
func TestWillEnqueueRunUnaffectedByAPresentDelegate(t *testing.T) {
	assignee := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	delegate := pgtype.UUID{Bytes: [16]byte{5}, Valid: true}
	issue := db.Issue{
		ID:           pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		WorkspaceID:  pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		Status:       "todo",
		AssigneeType: pgtype.Text{String: "member", Valid: true},
		AssigneeID:   assignee,
		DelegateType: pgtype.Text{String: "member", Valid: true},
		DelegateID:   delegate,
	}
	// A member assignee reaches the member branch, which needs no query.
	// PrevStatus is a real built-in key so issuestatus.Effective short-circuits
	// too: an empty one is not built-in and would hit the catalog.
	svc := &IssueService{}
	in := IssueTriggerInput{Issue: issue, PrevStatus: "todo", AssigneeChanged: true}
	_, withDelegate := svc.WillEnqueueRun(context.Background(), in, IssueTriggerProbe{})

	issue.DelegateType = pgtype.Text{}
	issue.DelegateID = pgtype.UUID{}
	in.Issue = issue
	_, withoutDelegate := svc.WillEnqueueRun(context.Background(), in, IssueTriggerProbe{})

	if withDelegate != withoutDelegate {
		t.Fatalf("a present delegate changed the assignee's verdict: with=%v without=%v",
			withDelegate, withoutDelegate)
	}
}
