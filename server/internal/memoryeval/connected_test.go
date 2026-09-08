package memoryeval

import (
	"strings"
	"testing"
	"time"
)

func TestConnectedGradingGate(t *testing.T) {
	s := Suite{WorkerProtocol: ConnectedProtocol, TimeoutSeconds: 60, Connected: &ConnectedConfig{Provider: "codex", Model: "fixture"}, TextCases: []ConnectedCase{{ID: "a", Split: "replay", Prompt: "first", Expected: "answer"}, {ID: "b", Split: "holdout", Prompt: "second", Expected: "answer"}}}
	for _, c := range s.TextCases {
		s.Cases = append(s.Cases, Case{ID: c.ID, Split: c.Split, Input: c.ID, Checks: c.ID})
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	runtime := &RuntimeEvidence{Provider: "codex", RequestedModel: "fixture", Status: "completed", ExecutableHash: strings.Repeat("a", 64), PromptHash: strings.Repeat("b", 64), BriefHash: strings.Repeat("c", 64)}
	now := time.Now()
	r := Report{Version: 1, Kind: "connected_memory_comparison", Suite: s, CompletedAt: &now, Cases: ConnectedComparisons(s)}
	for i := range r.Cases {
		r.Cases[i].Baseline, _ = GradeConnected(ConnectedCase{Expected: "answer"}, ConnectedResult{Artifact: "wrong", Runtime: runtime}, nil, "")
		r.Cases[i].Candidate, _ = GradeConnected(ConnectedCase{Expected: "answer"}, ConnectedResult{Artifact: " answer\n", Runtime: runtime}, nil, "")
	}
	if ok, why := r.Gate(); !ok {
		t.Fatal(why)
	}
	r.Cases[0].Candidate.Artifact = "forged wrong output"
	if ok, _ := r.Gate(); ok {
		t.Fatal("trusted runtime pass instead of server answer check")
	}
	r.Cases[0].Candidate.Artifact = "answer"
	r.Suite.TextCases[0].Expected = "different"
	if ok, _ := r.Gate(); ok {
		t.Fatal("changed answer accepted with old check hash")
	}
	if got, _ := GradeConnected(ConnectedCase{Expected: "answer"}, ConnectedResult{Artifact: "answer", Failed: true}, nil, ""); got.Status != "error" {
		t.Fatal("failed execution graded as passed")
	}
	if _, err := GradeConnected(ConnectedCase{Expected: "answer"}, ConnectedResult{Artifact: "answer"}, nil, ""); err == nil {
		t.Fatal("missing runtime evidence accepted")
	}
	s.TextCases[1].Prompt = "first"
	if s.Validate() == nil {
		t.Fatal("duplicate validation prompt accepted")
	}
}
