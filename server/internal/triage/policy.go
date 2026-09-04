// Package triage decides how inbound, externally-authored issue material is
// admitted to a workspace.
//
// M1 runs in shadow mode: webhook-originated autopilot issues are created
// exactly as before, and a shadow triage_item records what a gating queue
// would have held. That measures volume, title collapse, and the silent-loss
// population (deliveries that produced no issue) before any routing changes.
package triage

// Mode is a triage_source's admission setting.
type Mode string

const (
	// ModeGate holds inbound material in the queue until it is accepted.
	ModeGate Mode = "gate"
	// ModeDirect creates the issue immediately (today's behavior).
	ModeDirect Mode = "direct"
	// ModeBlocked refuses inbound material from this source.
	ModeBlocked Mode = "blocked"
)

// Route is the admission decision for one inbound item.
type Route string

const (
	// RouteQueue parks the item as a pending triage_item, no issue yet.
	RouteQueue Route = "queue"
	// RouteDirect admits the item straight into the workspace.
	RouteDirect Route = "direct"
	// RouteDrop refuses the item and records why.
	RouteDrop Route = "drop"
)

// Decide maps a source mode to a routing decision. Unknown or empty modes
// fail open to direct so a misconfigured source can never silently lose
// inbound work.
func Decide(mode string) Route {
	switch Mode(mode) {
	case ModeGate:
		return RouteQueue
	case ModeBlocked:
		return RouteDrop
	default:
		return RouteDirect
	}
}

// Source kinds name the object ref_id points at. Kinds are deliberately
// per-entry-point: one autopilot can be both a webhook source and a schedule
// source, and the two are gated independently.
const (
	SourceAutopilotWebhook  = "autopilot_webhook"
	SourceAutopilotSchedule = "autopilot_schedule"
	SourceChannel           = "channel"
	SourceAgentCreate       = "agent_create"
	SourceQuickCreate       = "quick_create"
	// SourceMeeting: action items extracted from a recorded meeting
	// (ref_id = meeting.id). Always queued, never dispatched directly.
	SourceMeeting = "meeting"
)

// Item states. pending is the only state that occupies the queue; dropped is
// the audit state for inbound material that produced no issue and no queue
// entry (issue limit reached, recent duplicate, source blocked).
const (
	StatePending    = "pending"
	StateAccepted   = "accepted"
	StateDismissed  = "dismissed"
	StateMerged     = "merged"
	StateSuperseded = "superseded"
	StateExpired    = "expired"
	StateDropped    = "dropped"
)
