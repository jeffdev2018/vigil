package memoryeval

import "testing"

func TestPilotSummary(t *testing.T) {
	r := testReport()
	s, err := SummarizePilot(r, "frozen", nil)
	if err != nil || s.Candidate.Passed != 2 || s.Candidate.Accepted != nil || s.Candidate.HumanSeconds != nil || s.Candidate.CostUSD != nil {
		t.Fatalf("unknown became zero: %+v %v", s, err)
	}
	yes, no, seconds, cost := true, false, int64(30), 0.5
	reviews := PilotReviews{ReportHash: "frozen", Reviewer: "human"}
	for _, c := range r.Cases {
		reviews.Runs = append(reviews.Runs, PilotRunReview{CaseID: c.ID, Variant: "baseline", Accepted: &no, HumanSeconds: &seconds, CostUSD: &cost, CostSource: "fixture receipt"}, PilotRunReview{CaseID: c.ID, Variant: "candidate", Accepted: &yes, HumanSeconds: &seconds, CostUSD: &cost, CostSource: "fixture receipt"})
	}
	s, err = SummarizePilot(r, "frozen", &reviews)
	if err != nil || *s.Candidate.Accepted != 2 || *s.Candidate.HumanSeconds != 60 || *s.Candidate.CostPerAccepted != 0.5 || s.Baseline.CostPerAccepted != nil {
		t.Fatalf("wrong pilot totals: %+v %v", s, err)
	}
	if _, err = SummarizePilot(r, "changed", &reviews); err == nil {
		t.Fatal("stale human review accepted")
	}
	reviews.Runs[0].HumanSeconds = nil
	if _, err = SummarizePilot(r, "frozen", &reviews); err == nil {
		t.Fatal("missing effort became zero")
	}
	reviews.Runs[0].HumanSeconds = &seconds
	reviews.Runs[0].Accepted = &yes
	if _, err = SummarizePilot(r, "frozen", &reviews); err == nil {
		t.Fatal("failing output accepted")
	}
	reviews.Runs[0].Accepted = &no
	reviews.Runs[0].CostSource = ""
	if _, err = SummarizePilot(r, "frozen", &reviews); err == nil {
		t.Fatal("unsourced cost accepted")
	}
}

func TestRuntimeGate(t *testing.T) {
	r := testReport()
	r.Suite.WorkerProtocol = "multica_runtime_v1"
	if ok, _ := r.Gate(); ok {
		t.Fatal("missing observations accepted")
	}
	for i := range r.Cases {
		a := &RuntimeEvidence{Provider: "claude", RequestedModel: "fixture", ExecutableHash: r.Cases[i].InputHash, PromptHash: r.Cases[i].InputHash, BriefHash: r.Cases[i].InputHash, Status: "completed"}
		b := *a
		r.Cases[i].Baseline.Runtime = a
		r.Cases[i].Candidate.Runtime = &b
	}
	if ok, why := r.Gate(); !ok {
		t.Fatal(why)
	}
	r.Cases[1].Candidate.Runtime.RequestedModel = "other"
	if ok, _ := r.Gate(); ok {
		t.Fatal("different model accepted")
	}
}
