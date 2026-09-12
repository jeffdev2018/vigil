package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// `multica fleet {status,cost,history}` (JEF-12). The server owns the
// aggregation; what these tests pin is the CLI side of the contract: the
// exact paths, the ?since=/agent_id= query encoding, and JSON staying the
// default output — the shape the fleet skill parses.

func newFleetTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("since", "", "")
	cmd.Flags().String("agent-id", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func setFleetEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", url)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

// captureStdout lives in cmd_skill_test.go and is shared by these tests.

// fleetServer answers the three fleet GETs with canned rows and records the
// last query string each path saw.
func fleetServer(t *testing.T, queries map[string]*string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		record := func() {
			if dst := queries[r.URL.Path]; dst != nil {
				*dst = r.URL.RawQuery
			}
		}
		switch r.URL.Path {
		case "/api/fleet/status":
			record()
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"agent_id": "a-1", "name": "Mika",
				"running_task_count": 2, "task_count": 7, "failed_count": 1,
			}})
		case "/api/fleet/cost":
			record()
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"agent_id": "a-1", "cost_usd_ticks": 25_000_000_000,
				"input_tokens": 1000, "output_tokens": 500, "task_count": 3,
			}})
		case "/api/fleet/history":
			record()
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"date": "2026-09-09", "agent_id": "a-1", "task_count": 4, "failed_count": 0,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestFleetCommandsHitTheirEndpointsWithQuery(t *testing.T) {
	queries := map[string]*string{
		"/api/fleet/status":  new(string),
		"/api/fleet/cost":    new(string),
		"/api/fleet/history": new(string),
	}
	srv := fleetServer(t, queries)
	defer srv.Close()
	setFleetEnv(t, srv.URL)

	runs := []struct {
		path string
		run  func(*cobra.Command, []string) error
	}{
		{"/api/fleet/status", runFleetStatus},
		{"/api/fleet/cost", runFleetCost},
		{"/api/fleet/history", runFleetHistory},
	}
	for _, tc := range runs {
		cmd := newFleetTestCmd()
		_ = cmd.Flags().Set("since", "2026-09-01")
		_ = cmd.Flags().Set("agent-id", "a-1")
		if err := tc.run(cmd, nil); err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		got := *queries[tc.path]
		if got == "" {
			t.Errorf("%s was never called", tc.path)
			continue
		}
		for _, want := range []string{"since=2026-09-01", "agent_id=a-1"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s query = %q, want it to carry %q", tc.path, got, want)
			}
		}
	}
}

func TestFleetCommandsOmitEmptyFilters(t *testing.T) {
	queries := map[string]*string{"/api/fleet/status": new(string)}
	srv := fleetServer(t, queries)
	defer srv.Close()
	setFleetEnv(t, srv.URL)

	if err := runFleetStatus(newFleetTestCmd(), nil); err != nil {
		t.Fatalf("runFleetStatus: %v", err)
	}
	if got := *queries["/api/fleet/status"]; got != "" {
		t.Errorf("query = %q, want empty when no flags are set (server defaults the window)", got)
	}
}

// JSON is the stable contract these commands feed to an LLM skill: the
// default output must parse as the server's row shape, not a table.
func TestFleetStatusDefaultOutputIsTheJSONShape(t *testing.T) {
	queries := map[string]*string{}
	srv := fleetServer(t, queries)
	defer srv.Close()
	setFleetEnv(t, srv.URL)

	out, err := captureStdout(t, func() error {
		return runFleetStatus(newFleetTestCmd(), nil)
	})
	if err != nil {
		t.Fatalf("runFleetStatus: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("default output is not JSON: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["agent_id"] != "a-1" || rows[0]["running_task_count"] != float64(2) {
		t.Errorf("rows = %v, want the server's status row passed through", rows)
	}
}

func TestFleetCostTableOutput(t *testing.T) {
	queries := map[string]*string{}
	srv := fleetServer(t, queries)
	defer srv.Close()
	setFleetEnv(t, srv.URL)

	cmd := newFleetTestCmd()
	_ = cmd.Flags().Set("output", "table")
	out, err := captureStdout(t, func() error {
		return runFleetCost(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runFleetCost: %v", err)
	}
	if !strings.Contains(out, "COST") || !strings.Contains(out, "$2.5000") {
		t.Errorf("table output = %q, want a COST column rendering 25e9 ticks as $2.5000", out)
	}
}
