package service

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// A machine that cannot confine a run is an infrastructure failure, not the
// agent's: another host in the pool may have Docker, so the run moves rather
// than dying — and it never falls back to running unconfined on this one.
func TestSandboxUnavailableMovesTheRun(t *testing.T) {
	if !failoverReasons[string(taskfailure.ReasonSandboxUnavailable)] {
		t.Fatal("a run this machine cannot confine must be answerable by the pool")
	}
	if failoverReasons[string(taskfailure.ReasonAgentBlocked)] {
		t.Fatal("an application failure must never move runtime — the guard this map exists for")
	}
}
