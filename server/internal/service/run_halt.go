package service

import (
	"encoding/json"
	"strings"
)

// Fleet halt (K05 / m169): one switch that stops a workspace's agents.
//
// Everything else in the product stops one thing. daemon/pause.go pauses a
// single run at a safe boundary, a run limit stops a run that went too far, an
// approval gate holds one action. None of them answer "stop, now, all of it",
// which is the question someone asks when an agent is doing something wrong
// and they do not yet know which agent or why.
//
// A halt is deliberately not a delete. Queued tasks stay queued and resume
// when it lifts: the point is to buy time to look, not to lose the work.
type RunHalt struct {
	// Halted stops new runs from being claimed and refuses the gated actions
	// of the runs already in flight.
	Halted bool `json:"halted"`
	// Reason is shown wherever the halt refuses something, so the person who
	// meets it does not have to go looking for who stopped what and why.
	Reason string `json:"reason,omitempty"`
	// HaltedBy and HaltedAt are stamped by the endpoint. They are part of the
	// record rather than decoration: a switch that stops a fleet has to say
	// who threw it.
	HaltedBy string `json:"halted_by,omitempty"`
	HaltedAt string `json:"halted_at,omitempty"`
	// FrozenCount is how many in-flight runs the halt is currently holding
	// (JEF-257). Live-computed on reads, just-frozen on the write that set the
	// halt; never stored in settings — the copy persisted there keeps it zero.
	FrozenCount int `json:"frozen_count"`
	// ResumedCount is how many frozen runs the lift that produced this
	// response resumed. Zero everywhere except a lift response.
	ResumedCount int `json:"resumed_count"`
}

// RunHaltMaxReasonRunes bounds the reason. It travels in refusals, including
// into an agent's own error text.
const RunHaltMaxReasonRunes = 280

// RunHaltFromSettings reads the halt out of workspace.settings. An unreadable
// settings blob is NOT read as "not halted": a halt that a malformed
// neighbouring key can lift is not a halt. It fails closed and reports the
// parse failure as a halt with a reason saying so.
func RunHaltFromSettings(settings []byte) RunHalt {
	if len(settings) == 0 {
		return RunHalt{}
	}
	var s struct {
		Halt *RunHalt `json:"run_halt"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		return RunHalt{Halted: true, Reason: "the workspace settings could not be read, so runs are held rather than dispatched"}
	}
	if s.Halt == nil {
		return RunHalt{}
	}
	out := *s.Halt
	out.Reason = strings.TrimSpace(out.Reason)
	return out
}

// Message is what a refusal says. It always names the halt, and adds the
// operator's reason when there is one.
func (h RunHalt) Message() string {
	const base = "this workspace's agents are halted"
	if h.Reason == "" {
		return base
	}
	return base + ": " + h.Reason
}
