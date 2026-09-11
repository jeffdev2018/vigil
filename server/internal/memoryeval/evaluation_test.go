package memoryeval

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testReport() Report {
	now := time.Now().UTC()
	suite := Suite{Image: "sha256:" + strings.Repeat("a", 64), Worker: []string{"/bin/sh", "worker.sh"}, Verifier: []string{"/bin/sh", "/checks/check.sh"}, TimeoutSeconds: 5, Cases: []Case{{ID: "a", Split: "replay", Input: "a", Checks: "checks"}, {ID: "b", Split: "holdout", Input: "b", Checks: "checks"}}}
	return Report{Version: 1, Kind: "offline_memory_comparison", Suite: suite, CompletedAt: &now, Candidate: Memory{ID: "memory", AgentID: "agent", WorkspaceID: "workspace", Revision: 1, Status: "pending", Content: "Always return uppercase"}, Baseline: []Memory{}, Cases: []Comparison{
		{ID: "a", Split: "replay", InputHash: strings.Repeat("a", 64), ChecksHash: strings.Repeat("c", 64), Baseline: Outcome{Status: "failed"}, Candidate: Outcome{Status: "passed"}},
		{ID: "b", Split: "holdout", InputHash: strings.Repeat("b", 64), ChecksHash: strings.Repeat("c", 64), Baseline: Outcome{Status: "failed"}, Candidate: Outcome{Status: "passed"}},
	}}
}

func TestAdoptionGate(t *testing.T) {
	if ok, why := testReport().Gate(); !ok {
		t.Fatal(why)
	}
	for _, mutate := range []func(*Report){
		func(r *Report) { r.Cases[1].Candidate.Status = "failed" },
		func(r *Report) { r.Cases[1].Baseline.Status = "error" },
		func(r *Report) { r.Cases[1].Baseline.Status = "passed" },
		func(r *Report) { r.Cases[0].ID = "other" },
		func(r *Report) { r.CompletedAt = nil },
		func(r *Report) { r.Suite.Cases[1].Split = "replay" },
		func(r *Report) { r.Suite.Image = "alpine:latest" },
		func(r *Report) { r.Cases[1].InputHash = r.Cases[0].InputHash },
		func(r *Report) { r.Cases[1].ChecksHash = strings.Repeat("z", 64) },
	} {
		r := testReport()
		mutate(&r)
		r.Eligible = true
		if ok, why := r.Gate(); ok {
			t.Fatalf("accepted unsafe report: %s", why)
		}
	}
}

// ReadJSON and cappedBuffer are still live (VerifyJavaScript's container
// path uses cappedBuffer; ReadJSON reads back every report). The
// snapshot()-specific assertions this test used to carry were removed along
// with Run/evaluate/snapshot/TestDockerMemoryComparison (dead code, fork-only,
// never called outside the Run() path that nothing invokes — see the memory
// eval audit finding on evaluation.go).
func TestReadJSONRejectsTrailingDataAndCappedBufferEnforcesItsCap(t *testing.T) {
	var suite Suite
	file := filepath.Join(t.TempDir(), "suite.json")
	if err := os.WriteFile(file, []byte(`{} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	if ReadJSON(file, &suite) == nil {
		t.Fatal("accepted trailing JSON")
	}
	var bounded cappedBuffer
	if _, err := io.Copy(&bounded, io.LimitReader(strings.NewReader(strings.Repeat("x", 65537)), 65537)); err != nil {
		t.Fatal(err)
	}
	if !bounded.overflow || len(bounded.String()) != 65536 {
		t.Fatal("io.Copy bypassed output cap")
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
