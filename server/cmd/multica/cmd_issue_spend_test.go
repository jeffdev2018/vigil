package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

// `multica issue spend-token {request,verify}` and `issue budget-status`. The
// server owns what a spend is allowed to be; what these tests pin is the shape
// the CLI hands it — the USD → ticks conversion, the exact paths, and the fact
// that a 202 gate is printed rather than treated as a failure.

func newSpendRequestTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("amount", "", "")
	cmd.Flags().String("purpose", "", "")
	cmd.Flags().String("gate", "", "")
	cmd.Flags().Bool("wait", false, "")
	cmd.Flags().String("issue", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newSpendVerifyTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("token", "", "")
	cmd.Flags().String("amount", "", "")
	cmd.Flags().String("issue", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newBudgetStatusTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("issue", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func setSpendEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", url)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

func TestParseUsdTicks(t *testing.T) {
	ok := []struct {
		in   string
		want int64
	}{
		{"1", 10_000_000_000},
		{"2.50", 25_000_000_000},
		{"$2.50", 25_000_000_000},
		{" 0.01 ", 100_000_000},
		{".5", 5_000_000_000},
		{"0.0000000001", 1},
		{"12.3456789012", 123_456_789_012},
	}
	for _, tc := range ok {
		got, err := parseUsdTicks(tc.in, "--amount")
		if err != nil {
			t.Errorf("parseUsdTicks(%q) errored: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			// A wrong factor here is a 10x-or-worse error on real money, and
			// the server has no way to tell it from an intended amount.
			t.Errorf("parseUsdTicks(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}

	bad := []struct{ in, want string }{
		{"", "required"},
		{"0", "greater than zero"},
		{"-1", "not a USD amount"},
		{"abc", "not a USD amount"},
		{"1e10", "not a USD amount"},
		{"1.00000000001", "decimal places"},
		{"99999999999999999999", "out of range"},
	}
	for _, tc := range bad {
		_, err := parseUsdTicks(tc.in, "--amount")
		if err == nil {
			t.Errorf("parseUsdTicks(%q) accepted an unusable amount", tc.in)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("parseUsdTicks(%q) error = %q, want it to mention %q", tc.in, err, tc.want)
		}
	}
}

// spendServer answers /spend-token with a canned response and captures the body.
func spendServer(t *testing.T, taskID string, status int, resp map[string]any, body *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks/"+taskID+"/spend-token" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestRunIssueSpendTokenRequestPostsUsdAsTicks(t *testing.T) {
	const taskID = "11111111-1111-4111-8111-111111111111"
	var body map[string]any
	srv := spendServer(t, taskID, http.StatusOK, map[string]any{"token": "mst_abc", "amount_usd_ticks": 25_000_000_000}, &body)
	defer srv.Close()
	setSpendEnv(t, srv.URL)

	cmd := newSpendRequestTestCmd()
	_ = cmd.Flags().Set("amount", "2.50")
	_ = cmd.Flags().Set("purpose", "openai batch")
	if err := runIssueSpendTokenRequest(cmd, []string{taskID}); err != nil {
		t.Fatalf("runIssueSpendTokenRequest: %v", err)
	}

	if got, want := body["amount_usd_ticks"], float64(25_000_000_000); got != want {
		t.Errorf("amount_usd_ticks = %v, want %v ($2.50 at 1e10 ticks/USD)", got, want)
	}
	if body["purpose"] != "openai batch" {
		t.Errorf("purpose = %v, want the flag value passed through", body["purpose"])
	}
	if _, sent := body["gate_id"]; sent {
		t.Errorf("gate_id = %v, want it omitted on a first request", body["gate_id"])
	}
}

func TestRunIssueSpendTokenRequestPrintsOpenedGate(t *testing.T) {
	const taskID = "22222222-2222-4222-8222-222222222222"
	var body map[string]any
	// Above the threshold the server opens a gate and answers 202. That is a
	// normal outcome: the command must print it, not fail.
	srv := spendServer(t, taskID, http.StatusAccepted, map[string]any{
		"id": "gate-1", "status": "pending", "gate_type": "spend",
	}, &body)
	defer srv.Close()
	setSpendEnv(t, srv.URL)

	cmd := newSpendRequestTestCmd()
	_ = cmd.Flags().Set("amount", "500")
	_ = cmd.Flags().Set("purpose", "stripe")
	if err := runIssueSpendTokenRequest(cmd, []string{taskID}); err != nil {
		t.Fatalf("a 202 gate must not be an error: %v", err)
	}
	if got, want := body["amount_usd_ticks"], float64(5_000_000_000_000); got != want {
		t.Errorf("amount_usd_ticks = %v, want %v", got, want)
	}
}

func TestRunIssueSpendTokenRequestPassesGateID(t *testing.T) {
	const taskID = "33333333-3333-4333-8333-333333333333"
	var body map[string]any
	srv := spendServer(t, taskID, http.StatusOK, map[string]any{"token": "mst_def"}, &body)
	defer srv.Close()
	setSpendEnv(t, srv.URL)

	cmd := newSpendRequestTestCmd()
	_ = cmd.Flags().Set("amount", "500")
	_ = cmd.Flags().Set("gate", "gate-1")
	// No --purpose: the gate already carries the one a human approved.
	if err := runIssueSpendTokenRequest(cmd, []string{taskID}); err != nil {
		t.Fatalf("runIssueSpendTokenRequest: %v", err)
	}
	if body["gate_id"] != "gate-1" {
		t.Errorf("gate_id = %v, want the --gate value so the server issues against the approved gate", body["gate_id"])
	}
}

// TestRunIssueSpendTokenRequestWaitCollectsToken drives the whole two-call
// protocol: 202 gate, long-poll until a human approves, then the second POST
// that names the gate and returns the token.
func TestRunIssueSpendTokenRequestWaitCollectsToken(t *testing.T) {
	const taskID = "44444444-4444-4444-8444-444444444444"
	var (
		mu     sync.Mutex
		posts  []map[string]any
		polls  int
		gateID = "gate-9"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/tasks/"+taskID+"/spend-token" && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			posts = append(posts, body)
			n := len(posts)
			mu.Unlock()
			if n == 1 {
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(map[string]any{"id": gateID, "status": "pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "mst_waited"})
		case r.URL.Path == "/api/tasks/"+taskID+"/gates/"+gateID && r.Method == http.MethodGet:
			if r.URL.Query().Get("wait") == "" {
				t.Errorf("gate read has no wait=, so the CLI would busy-poll the server")
			}
			mu.Lock()
			polls++
			n := polls
			mu.Unlock()
			status := "pending"
			if n >= 2 {
				status = "approved"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": gateID, "status": status})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setSpendEnv(t, srv.URL)

	cmd := newSpendRequestTestCmd()
	_ = cmd.Flags().Set("amount", "500")
	_ = cmd.Flags().Set("purpose", "stripe")
	_ = cmd.Flags().Set("wait", "true")
	if err := runIssueSpendTokenRequest(cmd, []string{taskID}); err != nil {
		t.Fatalf("runIssueSpendTokenRequest --wait: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 2 {
		t.Fatalf("posted %d times, want 2 (open the gate, then collect the token)", len(posts))
	}
	if posts[1]["gate_id"] != gateID {
		t.Errorf("second post gate_id = %v, want %q — without it the server opens a second gate", posts[1]["gate_id"], gateID)
	}
	if polls < 2 {
		t.Errorf("polled %d times, want the loop to keep waiting while the gate is pending", polls)
	}
}

// A denied gate is a printed answer, not an error: the server, not the CLI's
// exit code, is what refuses the spend.
func TestRunIssueSpendTokenRequestWaitStopsOnDeniedGate(t *testing.T) {
	const taskID = "55555555-5555-4555-8555-555555555555"
	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/spend-token") && r.Method == http.MethodPost:
			mu.Lock()
			posts++
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "gate-d", "status": "pending"})
		case strings.Contains(r.URL.Path, "/gates/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "gate-d", "status": "denied"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	setSpendEnv(t, srv.URL)

	cmd := newSpendRequestTestCmd()
	_ = cmd.Flags().Set("amount", "500")
	_ = cmd.Flags().Set("purpose", "stripe")
	_ = cmd.Flags().Set("wait", "true")
	if err := runIssueSpendTokenRequest(cmd, []string{taskID}); err != nil {
		t.Fatalf("a denied gate must print, not error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 1 {
		t.Errorf("posted %d times, want 1 — a denied gate must never be re-asked for a token", posts)
	}
}

func TestRunIssueSpendTokenRequestRejectsUnusableInput(t *testing.T) {
	const taskID = "66666666-6666-4666-8666-666666666666"
	cases := []struct {
		name    string
		amount  string
		purpose string
		want    string
	}{
		{name: "no amount", purpose: "openai", want: "--amount is required"},
		{name: "amount is not a number", amount: "lots", purpose: "openai", want: "not a USD amount"},
		{name: "no purpose", amount: "2", want: "--purpose is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// No server: an unusable request must fail before anything is sent.
			setSpendEnv(t, "http://127.0.0.1:1")
			cmd := newSpendRequestTestCmd()
			_ = cmd.Flags().Set("amount", tc.amount)
			_ = cmd.Flags().Set("purpose", tc.purpose)
			err := runIssueSpendTokenRequest(cmd, []string{taskID})
			if err == nil {
				t.Fatalf("runIssueSpendTokenRequest accepted %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q so the agent can fix the call", err, tc.want)
			}
		})
	}
}

func TestRunIssueSpendTokenVerifyPostsTokenAndAmount(t *testing.T) {
	const taskID = "77777777-7777-4777-8777-777777777777"
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks/"+taskID+"/spend-token/verify" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "gate_id": "gate-1"})
	}))
	defer srv.Close()
	setSpendEnv(t, srv.URL)

	cmd := newSpendVerifyTestCmd()
	_ = cmd.Flags().Set("token", "mst_abc")
	_ = cmd.Flags().Set("amount", "1.25")
	if err := runIssueSpendTokenVerify(cmd, []string{taskID}); err != nil {
		t.Fatalf("runIssueSpendTokenVerify: %v", err)
	}
	if body["token"] != "mst_abc" {
		t.Errorf("token = %v, want the --token value", body["token"])
	}
	if got, want := body["amount_usd_ticks"], float64(12_500_000_000); got != want {
		t.Errorf("amount_usd_ticks = %v, want %v", got, want)
	}
}

func TestRunIssueSpendTokenVerifyRequiresToken(t *testing.T) {
	setSpendEnv(t, "http://127.0.0.1:1")
	cmd := newSpendVerifyTestCmd()
	_ = cmd.Flags().Set("amount", "1")
	err := runIssueSpendTokenVerify(cmd, []string{"88888888-8888-4888-8888-888888888888"})
	if err == nil || !strings.Contains(err.Error(), "--token is required") {
		t.Fatalf("error = %v, want it to name the missing --token", err)
	}
}

func TestRunIssueBudgetStatusGetsTheRunsStatus(t *testing.T) {
	const taskID = "99999999-9999-4999-8999-999999999999"
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks/"+taskID+"/budget-status" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		hit = true
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": taskID, "usage": map[string]any{}, "gates": []any{}, "events": []any{}})
	}))
	defer srv.Close()
	setSpendEnv(t, srv.URL)

	cmd := newBudgetStatusTestCmd()
	if err := runIssueBudgetStatus(cmd, []string{taskID}); err != nil {
		t.Fatalf("runIssueBudgetStatus: %v", err)
	}
	if !hit {
		t.Error("budget-status never reached GET /api/tasks/{id}/budget-status")
	}
}
