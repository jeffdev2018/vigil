package service

import "encoding/json"

// Trust Dial (K26): the scorecard thresholds that make an agent eligible for
// the next mode. In workspace.settings:
//
//	"trust_dial": {"days": 30, "min_runs": 10, "min_accepted_rate": 0.8, "min_no_intervention_rate": 0.7, "max_reopen_rate": 0.1}
type TrustDial struct {
	Days                  int     `json:"days"`
	MinRuns               int     `json:"min_runs"`
	MinAcceptedRate       float64 `json:"min_accepted_rate"`
	MinNoInterventionRate float64 `json:"min_no_intervention_rate"`
	MaxReopenRate         float64 `json:"max_reopen_rate"`
}

var DefaultTrustDial = TrustDial{Days: 30, MinRuns: 10, MinAcceptedRate: 0.8, MinNoInterventionRate: 0.7, MaxReopenRate: 0.1}

func TrustDialSettings(settings []byte) TrustDial {
	out := DefaultTrustDial
	if len(settings) == 0 {
		return out
	}
	var s struct {
		Dial *TrustDial `json:"trust_dial"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Dial == nil {
		return out
	}
	d := *s.Dial
	if d.Days > 0 {
		out.Days = d.Days
	}
	if d.MinRuns > 0 {
		out.MinRuns = d.MinRuns
	}
	if d.MinAcceptedRate > 0 {
		out.MinAcceptedRate = d.MinAcceptedRate
	}
	if d.MinNoInterventionRate > 0 {
		out.MinNoInterventionRate = d.MinNoInterventionRate
	}
	if d.MaxReopenRate > 0 {
		out.MaxReopenRate = d.MaxReopenRate
	}
	return out
}
