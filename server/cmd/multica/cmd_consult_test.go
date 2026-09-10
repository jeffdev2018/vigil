package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// `multica consult` (JEF-12). The server owns the consult; what these tests
// pin is the agent-tool contract: the POST body shape, --context file
// loading, the answer on stdout by default, the full object under --output
// json, and — the one that keeps a run alive — a refused consult (429/503
// with a consult reason_code) printing to stderr and exiting 0.

func newConsultTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("context", "", "")
	cmd.Flags().String("output", "", "")
	return cmd
}

// consultServer answers POST /api/consult with the given status/body and
// captures the request body.
func consultServer(t *testing.T, status int, resp map[string]any, reqBody *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/consult" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if reqBody != nil {
			if err := json.NewDecoder(r.Body).Decode(reqBody); err != nil {
				t.Errorf("decode body: %v", err)
			}
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestRunConsultPostsQuestionAndPrintsAnswer(t *testing.T) {
	var body map[string]any
	srv := consultServer(t, http.StatusOK, map[string]any{
		"consult_id": "c-1", "answer": "use the index on agent_id",
		"model": "gpt-5-mini", "cost_usd_ticks": 100,
	}, &body)
	defer srv.Close()
	setFleetEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runConsult(newConsultTestCmd(), []string{"which index covers this query?"})
	})
	if err != nil {
		t.Fatalf("runConsult: %v", err)
	}
	if body["question"] != "which index covers this query?" {
		t.Errorf("question = %v, want the positional arg", body["question"])
	}
	if _, sent := body["context"]; sent {
		t.Errorf("context = %v, want it omitted without --context", body["context"])
	}
	if strings.TrimSpace(out) != "use the index on agent_id" {
		t.Errorf("stdout = %q, want just the answer", out)
	}
}

func TestRunConsultContextFileAndJSONOutput(t *testing.T) {
	ctx := filepath.Join(t.TempDir(), "ctx.md")
	if err := os.WriteFile(ctx, []byte("table has 2M rows\n"), 0o600); err != nil {
		t.Fatalf("write context file: %v", err)
	}
	var body map[string]any
	srv := consultServer(t, http.StatusOK, map[string]any{
		"consult_id": "c-2", "answer": "partition by day", "model": "gpt-5-mini",
	}, &body)
	defer srv.Close()
	setFleetEnv(t, srv.URL)

	cmd := newConsultTestCmd()
	_ = cmd.Flags().Set("context", ctx)
	_ = cmd.Flags().Set("output", "json")
	out, err := captureStdout(t, func() error {
		return runConsult(cmd, []string{"how do I speed this up?"})
	})
	if err != nil {
		t.Fatalf("runConsult: %v", err)
	}
	if body["context"] != "table has 2M rows\n" {
		t.Errorf("context = %q, want the file's contents", body["context"])
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("--output json did not print the response object: %v\n%s", err, out)
	}
	if resp["consult_id"] != "c-2" || resp["answer"] != "partition by day" {
		t.Errorf("response = %v, want the full consult object", resp)
	}
}

// The contract that makes consult safe to call from an agent script: a
// refusal is an answer, not a failure of the run.
func TestRunConsultRefusalsExitZero(t *testing.T) {
	cases := []struct {
		name   string
		status int
		resp   map[string]any
	}{
		{"budget exceeded", http.StatusTooManyRequests, map[string]any{
			"error":       "daily consult budget exhausted for this task",
			"reason_code": "consult_budget_exceeded", "consult_id": "c-3",
		}},
		{"llm disabled", http.StatusServiceUnavailable, map[string]any{
			"error":       "consult is not available: no LLM configured",
			"reason_code": "consult_llm_disabled", "consult_id": "c-4",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := consultServer(t, tc.status, tc.resp, nil)
			defer srv.Close()
			setFleetEnv(t, srv.URL)

			stderr := captureStderr(t)
			defer stderr.restore()
			if err := runConsult(newConsultTestCmd(), []string{"still there?"}); err != nil {
				t.Fatalf("a refused consult must exit 0, got: %v", err)
			}
			stderr.restore()
			if got := stderr.out.String(); !strings.Contains(got, "consult refused") || !strings.Contains(got, tc.resp["reason_code"].(string)) {
				t.Errorf("stderr = %q, want the refusal with its reason_code", got)
			}
		})
	}
}

// A 429/503 that is NOT a consult refusal (rate limiter, deploy) must stay an
// error — only the typed reason_codes are born-terminal answers.
func TestRunConsultNonRefusalErrorStaysAnError(t *testing.T) {
	srv := consultServer(t, http.StatusTooManyRequests, map[string]any{"error": "rate limited"}, nil)
	defer srv.Close()
	setFleetEnv(t, srv.URL)

	if err := runConsult(newConsultTestCmd(), []string{"ping"}); err == nil {
		t.Fatal("a 429 without a consult reason_code must surface as an error")
	}
}

func TestRunConsultAuthFailureSurfaces(t *testing.T) {
	srv := consultServer(t, http.StatusForbidden, map[string]any{
		"error": "consult is only available from within an agent task",
	}, nil)
	defer srv.Close()
	setFleetEnv(t, srv.URL)

	err := runConsult(newConsultTestCmd(), []string{"ping"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("error = %v, want the server's 403 to surface", err)
	}
}

func TestRunConsultMissingContextFileFailsBeforePosting(t *testing.T) {
	srv := consultServer(t, http.StatusOK, map[string]any{"answer": "unreachable"}, nil)
	defer srv.Close()
	setFleetEnv(t, srv.URL)

	cmd := newConsultTestCmd()
	_ = cmd.Flags().Set("context", filepath.Join(t.TempDir(), "does-not-exist.md"))
	err := runConsult(cmd, []string{"ping"})
	if err == nil || !strings.Contains(err.Error(), "--context") {
		t.Fatalf("error = %v, want it to name --context", err)
	}
}
