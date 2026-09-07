package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/memoryeval"
)

func TestMemoryRuntimeWorker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	executable := filepath.Join(t.TempDir(), "fixture-agent")
	script, err := os.ReadFile("testdata/memory-runtime/fake-agent.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, script, 0700); err != nil {
		t.Fatal(err)
	}
	f := memoryRuntimeFixture{Provider: "claude", Executable: executable, Model: "requested-fixture", ThinkingLevel: "low", Prompt: "Resolve the fixture", AgentName: "Pilot", AgentInstructions: "Return an independently checkable result", WorkspaceContext: "Fixture workspace", ProjectTitle: "Pilot project", ProjectDescription: "Frozen project context", MaxTurns: 2, TimeoutSeconds: 5}
	var outputs []memoryeval.RuntimeOutput
	for _, memories := range [][]string{nil, {"Use independent assertions"}} {
		work := t.TempDir()
		out, err := runMemoryRuntime(context.Background(), work, f, memories)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, out)
		if out.Runtime.Status != "completed" || out.Runtime.ToolCalls != 1 || out.Runtime.Usage["observed-fixture"].InputTokens != 11 || out.Runtime.RequestedModel != "requested-fixture" {
			t.Fatalf("wrong observations: %+v", out.Runtime)
		}
		brief, _ := os.ReadFile(filepath.Join(work, "CLAUDE.md"))
		if !strings.Contains(string(brief), "Frozen project context") || !strings.Contains(string(brief), "Fixture workspace") {
			t.Fatal("runtime context omitted")
		}
		args, _ := os.ReadFile(filepath.Join(work, "received-args.txt"))
		if !strings.Contains(string(args), "requested-fixture") || strings.Contains(string(args), "--resume") {
			t.Fatalf("wrong execution options: %s", args)
		}
		prompt, _ := os.ReadFile(filepath.Join(work, "received-prompt.json"))
		if !strings.Contains(string(prompt), "Resolve the fixture") || !strings.Contains(string(prompt), "run-only mode") {
			t.Fatal("production run-only prompt missing")
		}
	}
	if outputs[0].Artifact != "baseline" || outputs[1].Artifact != "fixed" || outputs[0].Runtime.PromptHash != outputs[1].Runtime.PromptHash || outputs[0].Runtime.ExecutableHash != outputs[1].Runtime.ExecutableHash || outputs[0].Runtime.BriefHash == outputs[1].Runtime.BriefHash {
		t.Fatal("comparison did not freeze execution while changing memory")
	}
	f.Provider = "unknown"
	if _, err := runMemoryRuntime(context.Background(), t.TempDir(), f, nil); err == nil {
		t.Fatal("unknown protocol accepted")
	}
	if err := memoryRuntimeWorkerCmd.RunE(memoryRuntimeWorkerCmd, []string{"missing.json"}); err == nil {
		t.Fatal("host execution allowed")
	}
}

func TestDockerMemoryRuntime(t *testing.T) {
	image := os.Getenv("MULTICA_EVAL_RUNTIME_TEST_IMAGE")
	if image == "" {
		t.Skip("requires an explicitly built local fixture image; no providers or pulls")
	}
	root := t.TempDir()
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	suite := memoryeval.Suite{Image: image, WorkerProtocol: "multica_runtime_v1", Worker: []string{"/usr/local/bin/multica", "agent", "memory", "runtime-worker", "/input/runtime.json"}, Verifier: []string{"/bin/sh", "/checks/check.sh"}, TimeoutSeconds: 15}
	for _, split := range []string{"replay", "holdout"} {
		f := memoryRuntimeFixture{Provider: "claude", Executable: "/usr/local/bin/fixture-agent", Model: "requested-fixture", Prompt: "Complete " + split, AgentName: "Pilot", MaxTurns: 2, TimeoutSeconds: 10}
		data, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(root, split, "runtime.json"), data)
		suite.Cases = append(suite.Cases, memoryeval.Case{ID: split, Split: split, Input: split, Checks: "checks"})
	}
	write(filepath.Join(root, "checks", "check.sh"), []byte("set -eu\ntest ! -e /memory.json\ntest \"$(cat /answer)\" = fixed\n"))
	report, err := memoryeval.Run(context.Background(), memoryeval.Report{Candidate: memoryeval.Memory{Content: "Use independent assertions"}, Suite: suite}, root, filepath.Join(root, "results"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Eligible {
		t.Fatalf("runtime comparison failed: %+v", report.Cases)
	}
	for _, c := range report.Cases {
		if c.Candidate.Artifact != "fixed" || c.Baseline.Artifact != "baseline" || c.Candidate.Runtime.ToolCalls != 1 || c.Candidate.Runtime.Usage["observed-fixture"].OutputTokens != 7 || c.Candidate.CostUSD != nil {
			t.Fatalf("wrong runtime evidence: %+v", c)
		}
	}
	// Protocol failures must not reach the verifier or manufacture a pass.
	suite.Worker = []string{"/bin/sh", "-c", "printf '{}'"}
	bad, err := memoryeval.Run(context.Background(), memoryeval.Report{Suite: suite}, root, filepath.Join(root, "bad-results"))
	if err != nil {
		t.Fatal(err)
	}
	if bad.Eligible || bad.Cases[0].Baseline.Status != "error" {
		t.Fatal("malformed adapter result accepted")
	}
}

func TestMemoryPilotSummaryCLI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	report := memoryeval.Report{Version: 1, Kind: "offline_memory_comparison", Suite: memoryeval.Suite{Image: "sha256:" + strings.Repeat("a", 64), Worker: []string{"/worker"}, Verifier: []string{"/verifier"}, TimeoutSeconds: 1, Cases: []memoryeval.Case{{ID: "a", Split: "replay", Input: "a", Checks: "checks"}, {ID: "b", Split: "holdout", Input: "b", Checks: "checks"}}}}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.Flags().String("reviews", "", "")
	cmd.SetOut(&out)
	if err := memoryPilotSummaryCmd.RunE(cmd, []string{path}); err != nil {
		t.Fatal(err)
	}
	var summary memoryeval.PilotSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.ReportHash) != 64 || summary.Candidate.Planned != 2 || summary.Candidate.Accepted != nil || summary.Candidate.HumanSeconds != nil {
		t.Fatal("CLI manufactured pilot observations")
	}
	if err := os.WriteFile(path, append(raw, []byte(" {}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := memoryPilotSummaryCmd.RunE(cmd, []string{path}); err == nil {
		t.Fatal("trailing report accepted")
	}
}
