package service

import "encoding/json"

// Approval gates (K05): workspace policy in workspace.settings.
//
//	"approval_gates": {"timeout_minutes": 30, "spend_threshold_usd_ticks": 100000000000, "sensitive_tools": "merge|delete|..."}
type ApprovalGates struct {
	TimeoutMinutes         int    `json:"timeout_minutes"`
	SpendThresholdUsdTicks int64  `json:"spend_threshold_usd_ticks"`
	SensitiveTools         string `json:"sensitive_tools"`
}

// DefaultSensitiveTools names MCP tools that pause for a human by default.
const DefaultSensitiveTools = `(?i)merge|delete|remove|drop|destroy|pay|charge|transfer|refund|purchase`

var DefaultApprovalGates = ApprovalGates{TimeoutMinutes: 30, SpendThresholdUsdTicks: 100_000_000_000, SensitiveTools: DefaultSensitiveTools}

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
	return out
}
