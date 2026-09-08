package memoryeval

import (
	"context"
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

func TestSnapshotRejectsLinksAndNestedOutput(t *testing.T) {
	root := t.TempDir()
	if _, err := snapshot(root, filepath.Join(root, "nested")); err == nil {
		t.Fatal("accepted recursive snapshot")
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot(root, filepath.Join(t.TempDir(), "snapshot")); err == nil {
		t.Fatal("followed symlink")
	}
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

// Opt-in only: pinned local image, no pulls, providers, credentials or live repo.
func TestDockerMemoryComparison(t *testing.T) {
	image := os.Getenv("MULTICA_EVAL_TEST_IMAGE")
	if image == "" {
		t.Skip("set MULTICA_EVAL_TEST_IMAGE to a local immutable Alpine-compatible image ID")
	}
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	worker := `set -eu
test ! -e /checks/check.sh
test ! -e /answer
test -z "$(ip route show default)"
if touch /input/host-write 2>/dev/null; then exit 70; fi
if grep -q 'Always return uppercase' /memory.json; then tr '[:lower:]' '[:upper:]' < task.txt; else cat task.txt; fi
`
	for _, id := range []string{"a", "b"} {
		write(filepath.Join(root, id, "worker.sh"), worker)
		write(filepath.Join(root, id, "task.txt"), "hello "+id)
	}
	write(filepath.Join(root, "checks", "check.sh"), `set -eu
test ! -e /memory.json
test "$(cat /answer)" = "$(tr '[:lower:]' '[:upper:]' < task.txt)"
`)
	r := testReport()
	r.Suite.Image = image
	output := filepath.Join(root, "results")
	got, err := Run(context.Background(), r, root, output)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Eligible {
		t.Fatalf("not eligible: %+v", got)
	}
	var stored Report
	if err := ReadJSON(filepath.Join(output, "report.json"), &stored); err != nil {
		t.Fatal(err)
	}
	if ok, why := stored.Gate(); !ok {
		t.Fatal(why)
	}
	for _, c := range stored.Cases {
		if c.Baseline.Status != "failed" || c.Candidate.Status != "passed" || c.Candidate.CostUSD != nil || c.Candidate.HumanInterventions != nil {
			t.Fatalf("wrong results: %+v", c)
		}
		if _, err := os.Stat(filepath.Join(root, c.ID, "host-write")); !os.IsNotExist(err) {
			t.Fatal("fixture changed")
		}
	}
	if _, err := Run(context.Background(), r, root, output); err == nil {
		t.Fatal("overwrote report")
	}
	// Verifier exit 2 is an infrastructure error, not a baseline failure that
	// could manufacture an improvement. A timeout must also close adoption.
	r.Suite.Verifier = []string{"/bin/sh", "-c", "exit 2"}
	result := evaluate(context.Background(), r.Suite, filepath.Join(output, "a", "input"), filepath.Join(output, "a", "checks"), filepath.Join(output, "candidate.json"), filepath.Join(output, "error.answer"))
	if result.Status != "error" {
		t.Fatalf("infrastructure error counted as quality: %+v", result)
	}
	r.Suite.Worker = []string{"/bin/sh", "-c", "sleep 30"}
	r.Suite.TimeoutSeconds = 1
	result = evaluate(context.Background(), r.Suite, filepath.Join(output, "a", "input"), filepath.Join(output, "a", "checks"), filepath.Join(output, "candidate.json"), filepath.Join(output, "timeout.answer"))
	if result.Status != "error" || !strings.Contains(result.Diagnostic, "deadline") {
		t.Fatalf("timeout: %+v", result)
	}
	r.Suite.Worker = []string{"/bin/sh", "-c", "head -c 65537 /dev/zero"}
	r.Suite.TimeoutSeconds = 5
	result = evaluate(context.Background(), r.Suite, filepath.Join(output, "a", "input"), filepath.Join(output, "a", "checks"), filepath.Join(output, "candidate.json"), filepath.Join(output, "overflow.answer"))
	if result.Status != "error" || !strings.Contains(result.Diagnostic, "exceeded") {
		t.Fatalf("output cap: status=%s bytes=%d diagnostic=%s", result.Status, len(result.Artifact), result.Diagnostic)
	}
}
