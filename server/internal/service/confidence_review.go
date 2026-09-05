package service

import "encoding/json"

// Run confidence review (JEF-240): when a completed run's self-assessed
// confidence falls under the workspace threshold, the linked issue goes to
// human review automatically.
//
//	"confidence_review": {"enabled": true, "threshold": 0.5}
//
// On by default: the score is always stored, and the auto-review escalation
// is the point of the feature; a workspace turns it off explicitly.
type ConfidenceReview struct {
	Enabled   bool    `json:"enabled"`
	Threshold float64 `json:"threshold"`
}

var DefaultConfidenceReview = ConfidenceReview{Enabled: true, Threshold: 0.5}

// ValidThreshold reports whether threshold is a usable cutoff.
func ValidConfidenceThreshold(threshold float64) bool {
	return threshold > 0 && threshold <= 1
}

func ConfidenceReviewSettings(settings []byte) ConfidenceReview {
	out := DefaultConfidenceReview
	if len(settings) == 0 {
		return out
	}
	var s struct {
		ConfidenceReview *ConfidenceReview `json:"confidence_review"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.ConfidenceReview == nil {
		return out
	}
	out.Enabled = s.ConfidenceReview.Enabled
	if ValidConfidenceThreshold(s.ConfidenceReview.Threshold) {
		out.Threshold = s.ConfidenceReview.Threshold
	}
	return out
}
