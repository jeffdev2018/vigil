package service

import "encoding/json"

// Run confidence review (JEF-240): when a completed run's self-assessed
// confidence falls under the workspace threshold, the linked issue goes to
// human review automatically.
//
//	"confidence_review": {"enabled": true, "threshold": 0.5, "max_escalations": 2}
//
// On by default: the score is always stored, and the auto-review escalation
// is the point of the feature; a workspace turns it off explicitly.
//
// Cascade (JEF-272): before human review, a below-threshold run escalates to
// a stronger runtime, up to MaxEscalations times per issue (0 disables the
// cascade and keeps the JEF-240 direct-to-review behavior).
type ConfidenceReview struct {
	Enabled        bool    `json:"enabled"`
	Threshold      float64 `json:"threshold"`
	MaxEscalations int     `json:"max_escalations"`
}

var DefaultConfidenceReview = ConfidenceReview{Enabled: true, Threshold: 0.5, MaxEscalations: 2}

// ValidConfidenceThreshold reports whether threshold is a usable cutoff.
func ValidConfidenceThreshold(threshold float64) bool {
	return threshold > 0 && threshold <= 1
}

// MaxConfidenceEscalations bounds the cascade length; ValidMaxEscalations
// accepts the inclusive 0-3 range.
const MaxConfidenceEscalations = 3

func ValidMaxEscalations(n int) bool {
	return n >= 0 && n <= MaxConfidenceEscalations
}

func ConfidenceReviewSettings(settings []byte) ConfidenceReview {
	out := DefaultConfidenceReview
	if len(settings) == 0 {
		return out
	}
	// MaxEscalations goes through a pointer so an omitted key keeps the
	// default instead of reading as an explicit 0 (cascade off).
	var s struct {
		ConfidenceReview *struct {
			Enabled        bool    `json:"enabled"`
			Threshold      float64 `json:"threshold"`
			MaxEscalations *int    `json:"max_escalations"`
		} `json:"confidence_review"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.ConfidenceReview == nil {
		return out
	}
	out.Enabled = s.ConfidenceReview.Enabled
	if ValidConfidenceThreshold(s.ConfidenceReview.Threshold) {
		out.Threshold = s.ConfidenceReview.Threshold
	}
	if s.ConfidenceReview.MaxEscalations != nil && ValidMaxEscalations(*s.ConfidenceReview.MaxEscalations) {
		out.MaxEscalations = *s.ConfidenceReview.MaxEscalations
	}
	return out
}
