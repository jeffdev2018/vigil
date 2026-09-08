package memoryeval

import (
	"errors"
	"math"
)

// PilotReviews is a separate human record, bound to the exact report bytes.
// It does not change executable checks or authorize memory adoption.
type PilotReviews struct {
	ReportHash string           `json:"report_hash"`
	Reviewer   string           `json:"reviewer"`
	Runs       []PilotRunReview `json:"runs"`
}
type PilotRunReview struct {
	CaseID       string   `json:"case_id"`
	Variant      string   `json:"variant"`
	Accepted     *bool    `json:"accepted"`
	HumanSeconds *int64   `json:"human_seconds"`
	CostUSD      *float64 `json:"cost_usd"`
	CostSource   string   `json:"cost_source"`
}
type PilotVariantSummary struct {
	Planned         int      `json:"planned"`
	Passed          int      `json:"passed"`
	ExecutionErrors int      `json:"execution_errors"`
	Reviewed        int      `json:"reviewed"`
	UsageReported   int      `json:"usage_reported"`
	Accepted        *int     `json:"accepted"`
	HumanSeconds    *int64   `json:"human_seconds"`
	CostUSD         *float64 `json:"human_recorded_cost_usd"`
	CostPerAccepted *float64 `json:"human_recorded_cost_per_accepted_usd"`
}
type PilotSummary struct {
	Baseline    PilotVariantSummary `json:"baseline"`
	Candidate   PilotVariantSummary `json:"candidate"`
	Regressions int                 `json:"regressions"`
	ReportHash  string              `json:"report_hash"`
	Reviewer    string              `json:"reviewer"`
}

func SummarizePilot(report Report, hash string, reviews *PilotReviews) (PilotSummary, error) {
	out := PilotSummary{ReportHash: hash}
	if report.Version != 1 || !report.ValidKind() {
		return out, errors.New("unsupported comparison report")
	}
	if err := report.Suite.Validate(); err != nil {
		return out, err
	}
	if len(report.Cases) > len(report.Suite.Cases) {
		return out, errors.New("too many results")
	}
	byID := map[string]Comparison{}
	for i, c := range report.Cases {
		if c.ID != report.Suite.Cases[i].ID || c.Split != report.Suite.Cases[i].Split {
			return out, errors.New("case order differs from suite")
		}
		byID[c.ID] = c
		for _, o := range []Outcome{c.Baseline, c.Candidate} {
			switch o.Status {
			case "", "passed", "failed", "error":
			default:
				return out, errors.New("invalid execution status")
			}
			if o.Runtime != nil && o.Runtime.Validate() != nil {
				return out, errors.New("invalid runtime observations")
			}
		}
		if c.Baseline.Status == "passed" && c.Candidate.Status == "failed" {
			out.Regressions++
		}
	}
	indexed := map[string]PilotRunReview{}
	if reviews != nil {
		if reviews.ReportHash != hash || reviews.Reviewer == "" {
			return out, errors.New("human reviews must name a reviewer and match the report fingerprint")
		}
		out.Reviewer = reviews.Reviewer
		for _, r := range reviews.Runs {
			key := r.CaseID + ":" + r.Variant
			c, exists := byID[r.CaseID]
			if _, duplicate := indexed[key]; duplicate || !exists || (r.Variant != "baseline" && r.Variant != "candidate") || r.Accepted == nil || r.HumanSeconds == nil || *r.HumanSeconds < 0 || *r.HumanSeconds > 604800 {
				return out, errors.New("invalid or duplicate human review")
			}
			o := c.Baseline
			if r.Variant == "candidate" {
				o = c.Candidate
			}
			if o.Status == "" || (*r.Accepted && o.Status != "passed") {
				return out, errors.New("only independently passing outputs may be accepted")
			}
			if r.CostUSD != nil && (math.IsNaN(*r.CostUSD) || math.IsInf(*r.CostUSD, 0) || *r.CostUSD < 0 || r.CostSource == "") {
				return out, errors.New("cost requires a finite nonnegative amount and a source")
			}
			indexed[key] = r
		}
	}
	for _, variant := range []string{"baseline", "candidate"} {
		s := PilotVariantSummary{Planned: len(report.Suite.Cases)}
		accepted, seconds, cost, costs := 0, int64(0), 0.0, 0
		for _, c := range report.Cases {
			o := c.Baseline
			if variant == "candidate" {
				o = c.Candidate
			}
			if o.Status == "passed" {
				s.Passed++
			}
			if o.Status == "error" {
				s.ExecutionErrors++
			}
			if o.Runtime != nil && len(o.Runtime.Usage) > 0 {
				s.UsageReported++
			}
			if r, ok := indexed[c.ID+":"+variant]; ok {
				s.Reviewed++
				seconds += *r.HumanSeconds
				if *r.Accepted {
					accepted++
				}
				if r.CostUSD != nil {
					cost += *r.CostUSD
					costs++
				}
			}
		}
		if math.IsInf(cost, 0) {
			return out, errors.New("cost total overflow")
		}
		if s.Reviewed == s.Planned {
			s.Accepted = &accepted
			s.HumanSeconds = &seconds
		}
		if costs == s.Planned {
			s.CostUSD = &cost
			if accepted > 0 && s.Reviewed == s.Planned {
				value := cost / float64(accepted)
				s.CostPerAccepted = &value
			}
		}
		if variant == "baseline" {
			out.Baseline = s
		} else {
			out.Candidate = s
		}
	}
	return out, nil
}
