package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/memoryeval"
	"github.com/spf13/cobra"
)

func TestMemoryAdoptionCLI(t *testing.T) {
	memory := memoryeval.Memory{ID: "memory", AgentID: "agent", WorkspaceID: "workspace", Revision: 3, Status: "pending", Content: "Use independent checks"}
	current := memory
	var writes int
	deny := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/agents/agent/memories/memory/evaluations":
			if deny {
				http.Error(w, "human required", http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "11111111-1111-4111-8111-111111111111"})
		case r.Method == "GET" && r.URL.Path == "/api/agents/agent/memories":
			_ = json.NewEncoder(w).Encode([]memoryeval.Memory{current})
		case r.Method == "PUT" && r.URL.Path == "/api/agents/agent/memories/memory":
			if deny {
				http.Error(w, "human required", http.StatusForbidden)
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["expected_revision"] != float64(current.Revision) {
				http.Error(w, "stale", 409)
				return
			}
			if body["expected_revision"] != float64(3) || body["status"] != "active" || body["evaluation_id"] != "11111111-1111-4111-8111-111111111111" {
				t.Errorf("bad adoption body: %+v", body)
			}
			writes++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "memory", "revision": 4, "status": "active"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace")
	t.Setenv("MULTICA_TOKEN", "test-token")
	now := time.Now().UTC()
	r := memoryeval.Report{Version: 1, Kind: "offline_memory_comparison", ServerURL: srv.URL, Candidate: memory, Baseline: []memoryeval.Memory{}, CompletedAt: &now, Suite: memoryeval.Suite{Image: "sha256:" + strings.Repeat("a", 64), Worker: []string{"/worker"}, Verifier: []string{"/verifier"}, TimeoutSeconds: 5, Cases: []memoryeval.Case{{ID: "a", Split: "replay", Input: "a", Checks: "checks"}, {ID: "b", Split: "holdout", Input: "b", Checks: "checks"}}}}
	for i, c := range r.Suite.Cases {
		r.Cases = append(r.Cases, memoryeval.Comparison{ID: c.ID, Split: c.Split, InputHash: strings.Repeat(string(rune('a'+i)), 64), ChecksHash: strings.Repeat("c", 64), Baseline: memoryeval.Outcome{Status: "failed"}, Candidate: memoryeval.Outcome{Status: "passed"}})
	}
	path := filepath.Join(t.TempDir(), "report.json")
	save := func() {
		t.Helper()
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save()
	cmd := &cobra.Command{}
	cmd.Flags().Bool("reviewed", false, "")
	if err := agentMemoryAdoptCmd.RunE(cmd, []string{path}); err == nil {
		t.Fatal("missing human review accepted")
	}
	_ = cmd.Flags().Set("reviewed", "true")
	if err := agentMemoryAdoptCmd.RunE(cmd, []string{path}); err != nil {
		t.Fatal(err)
	}
	current.Revision++
	if err := agentMemoryAdoptCmd.RunE(cmd, []string{path}); err == nil {
		t.Fatal("stale memory adopted")
	}
	current = memory
	deny = true
	if err := agentMemoryAdoptCmd.RunE(cmd, []string{path}); err == nil {
		t.Fatal("server authorization bypassed")
	}
	deny = false
	r.Cases[1].Candidate.Status = "failed"
	r.Eligible = true
	save()
	if err := agentMemoryAdoptCmd.RunE(cmd, []string{path}); err == nil {
		t.Fatal("trusted forged eligible flag")
	}
	r.Cases[1].Candidate.Status = "passed"
	r.ServerURL = "https://other.invalid"
	save()
	if err := agentMemoryAdoptCmd.RunE(cmd, []string{path}); err == nil {
		t.Fatal("cross-server report adopted")
	}
	if writes != 1 {
		t.Fatalf("writes=%d", writes)
	}
}
