package service

import "encoding/json"

// Decision SLA (K35): a workspace policy, stored in workspace.settings like
// the plan verification gate, that gives every Decision Card a deadline and
// names who hears about it first when the deadline passes.
//
//	"decision_sla": {"deadline_minutes": 120, "substitute_user_id": "<uuid>"}
type DecisionSLA struct {
	DeadlineMinutes  int    `json:"deadline_minutes"`
	SubstituteUserID string `json:"substitute_user_id"`
}

// DecisionSLASettings reads the policy; ok is false when there is none or
// the deadline is not a positive number of minutes.
func DecisionSLASettings(settings []byte) (DecisionSLA, bool) {
	if len(settings) == 0 {
		return DecisionSLA{}, false
	}
	var s struct {
		SLA *DecisionSLA `json:"decision_sla"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.SLA == nil || s.SLA.DeadlineMinutes <= 0 {
		return DecisionSLA{}, false
	}
	return *s.SLA, true
}
