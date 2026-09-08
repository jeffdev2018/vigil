package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/memoryeval"
)

func TestConnectedMemoryDaemon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	// Tests must not inspect the developer's provider account or launch a real CLI.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "shared-codex"))
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "fake-"+provider)
			script := `#!/bin/sh
set -eu
if [ "${1:-}" = --version ]; then echo fixture-1.0; exit 0; fi
`
			if provider == "claude" {
				raw, err := os.ReadFile("../../cmd/multica/testdata/memory-runtime/fake-agent.sh")
				if err != nil {
					t.Fatal(err)
				}
				script = string(raw)
			} else {
				script += `read -r line
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}'
read -r line
read -r line
printf '%s' "$line" > received-thread.json
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"fixture-thread"}}}'
read -r line
answer=baseline
if grep -q 'Use independent assertions' AGENTS.md; then answer=fixed; fi
printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"fixture-thread","turn":{"id":"fixture-turn"}}}'
printf '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"fixture-thread","item":{"type":"agentMessage","id":"answer","text":"%s","phase":"final_answer"}}}\n' "$answer"
printf '%s\n' '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"fixture-thread","turn":{"id":"fixture-turn","status":"completed","usage":{"input_tokens":11,"output_tokens":7}}}}'
`
			}
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			job := memoryeval.ConnectedJob{ID: "job", Config: memoryeval.ConnectedConfig{Provider: provider, Model: "fixture", Instructions: "Fixture only"}, Candidate: "Use independent assertions", Cases: []memoryeval.ConnectedCase{{ID: "a", Split: "replay", Prompt: "first"}, {ID: "b", Split: "holdout", Prompt: "second"}}}
			claims := 0
			var results []memoryeval.ConnectedResult
			d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/claim") {
					claims++
					json.NewEncoder(w).Encode(job)
					return
				}
				var result memoryeval.ConnectedResult
				if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
					t.Error(err)
				}
				results = append(results, result)
				status := "running"
				if len(results) == 4 {
					status = "completed"
				}
				json.NewEncoder(w).Encode(map[string]string{"status": status})
			})
			d.cfg.Agents = map[string]AgentEntry{provider: {Path: bin}}
			d.handleMemoryEvaluation(context.Background(), Runtime{ID: "rt", Provider: provider}, "job")
			if claims != 1 || len(results) != 4 {
				t.Fatalf("claim/results=%d/%d: %+v", claims, len(results), results)
			}
			for i, r := range results {
				expected := "baseline"
				if i%2 == 1 {
					expected = "fixed"
				}
				if r.Index != i || r.Failed || r.Artifact != expected || r.Runtime == nil || r.Runtime.Provider != provider {
					t.Fatalf("bad result %d: %+v", i, r)
				}
			}
			d.memoryEvaluationActive.Lock()
			d.handleMemoryEvaluation(context.Background(), Runtime{ID: "rt", Provider: provider}, "other")
			d.memoryEvaluationActive.Unlock()
			if claims != 1 {
				t.Fatal("second evaluation claimed while worker was busy")
			}
		})
	}
}

func TestConnectedMemoryDaemonStopsAfterReportRejection(t *testing.T) {
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(409) })
	// No executable configured: a rejected claim must end before discovery.
	d.handleMemoryEvaluation(context.Background(), Runtime{ID: "rt", Provider: "claude"}, "cancelled")
}
