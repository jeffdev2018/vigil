package service

import "encoding/json"

// Approval gates (K05): workspace policy in workspace.settings.
//
//	"approval_gates": {"timeout_minutes": 30, "spend_threshold_usd_ticks": 100000000000, "sensitive_tools": "merge|delete|..."}
type ApprovalGates struct {
	TimeoutMinutes         int    `json:"timeout_minutes"`
	SpendThresholdUsdTicks int64  `json:"spend_threshold_usd_ticks"`
	SensitiveTools         string `json:"sensitive_tools"`
	// Approvers says who may settle a gate (git push, tool call, spend):
	// "any_member" (the historical rule: anyone who can read the issue) or
	// "owner_admin".
	Approvers string `json:"approvers"`
}

const (
	GateApproversAnyMember  = "any_member"
	GateApproversOwnerAdmin = "owner_admin"
)

// GateApproverAllowed reports whether a member role may settle a gate under
// the policy. Agents never do; the caller checks the actor type.
func (g ApprovalGates) GateApproverAllowed(role string) bool {
	if g.Approvers == GateApproversOwnerAdmin {
		return role == "owner" || role == "admin"
	}
	return true
}

// DefaultSensitiveTools names MCP tools that pause for a human by default.
const DefaultSensitiveTools = `(?i)merge|delete|remove|drop|destroy|pay|charge|transfer|refund|purchase`

var DefaultApprovalGates = ApprovalGates{TimeoutMinutes: 30, SpendThresholdUsdTicks: 100_000_000_000, SensitiveTools: DefaultSensitiveTools, Approvers: GateApproversAnyMember}

func ApprovalGatesSettings(settings []byte) ApprovalGates {
	out := DefaultApprovalGates
	if len(settings) == 0 {
		return out
	}
	var s struct {
		Gates *ApprovalGates `json:"approval_gates"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Gates == nil {
		return out
	}
	if s.Gates.TimeoutMinutes > 0 {
		out.TimeoutMinutes = s.Gates.TimeoutMinutes
	}
	if s.Gates.SpendThresholdUsdTicks > 0 {
		out.SpendThresholdUsdTicks = s.Gates.SpendThresholdUsdTicks
	}
	if s.Gates.SensitiveTools != "" {
		out.SensitiveTools = s.Gates.SensitiveTools
	}
	if s.Gates.Approvers == GateApproversOwnerAdmin {
		out.Approvers = GateApproversOwnerAdmin
	}
	return out
}
