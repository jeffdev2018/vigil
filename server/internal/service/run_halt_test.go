package service

import "testing"

// The halt is read on every claim and on every gate, so how it reads a
// malformed settings blob is the whole design: a switch a neighbouring key can
// lift is not a switch.
func TestRunHaltFromSettings(t *testing.T) {
	if got := RunHaltFromSettings(nil); got.Halted {
		t.Error("no settings is not a halt")
	}
	if got := RunHaltFromSettings([]byte(`{"confidence_review":{"enabled":true}}`)); got.Halted {
		t.Error("a workspace that never set a halt is not halted")
	}
	if got := RunHaltFromSettings([]byte(`{"run_halt":{"halted":false}}`)); got.Halted {
		t.Error("halted:false is not halted")
	}

	halted := RunHaltFromSettings([]byte(`{"run_halt":{"halted":true,"reason":"  an agent opened 40 PRs  ","halted_by":"u1"}}`))
	if !halted.Halted || halted.Reason != "an agent opened 40 PRs" || halted.HaltedBy != "u1" {
		t.Fatalf("halt = %+v", halted)
	}

	// Fail closed. Unreadable settings hold the fleet and say why, rather than
	// reading as "not halted" — the one reading that would let a malformed
	// unrelated key turn the switch off.
	broken := RunHaltFromSettings([]byte(`{"run_halt":`))
	if !broken.Halted || broken.Reason == "" {
		t.Fatalf("unreadable settings = %+v, want a halt that explains itself", broken)
	}
}

// The message travels into refusals, including an agent's own error text.
func TestRunHaltMessage(t *testing.T) {
	if got := (RunHalt{Halted: true}).Message(); got != "this workspace's agents are halted" {
		t.Errorf("message = %q", got)
	}
	got := (RunHalt{Halted: true, Reason: "investigating"}).Message()
	if got != "this workspace's agents are halted: investigating" {
		t.Errorf("message = %q, want the operator's reason carried through", got)
	}
}
