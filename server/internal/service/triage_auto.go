package service

import "encoding/json"

// Triage auto-ML (K61): when the queue may classify a delivery by itself.
//
//	"triage_auto": {"enabled": false, "threshold": 0.9, "min_examples": 20}
//
// Off by default: suggestions always show, auto-application needs a human
// to turn it on.
type TriageAuto struct {
	Enabled     bool    `json:"enabled"`
	Threshold   float64 `json:"threshold"`
	MinExamples int     `json:"min_examples"`
}

var DefaultTriageAuto = TriageAuto{Enabled: false, Threshold: 0.9, MinExamples: 20}

func TriageAutoSettings(settings []byte) TriageAuto {
	out := DefaultTriageAuto
	if len(settings) == 0 {
		return out
	}
	var s struct {
		Auto *TriageAuto `json:"triage_auto"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Auto == nil {
		return out
	}
	out.Enabled = s.Auto.Enabled
	if s.Auto.Threshold > 0 && s.Auto.Threshold <= 1 {
		out.Threshold = s.Auto.Threshold
	}
	if s.Auto.MinExamples > 0 {
		out.MinExamples = s.Auto.MinExamples
	}
	return out
}
