package main

import (
	"net/http"
	"reflect"
	"runtime"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

// Task watchdog (K73): oversight is configured by people, so the agent
// under watch must not be able to loosen, remove, or control the cadence of
// its own watchdog. PUT/DELETE already carried RequireHumanActor; POST
// /scan did not, letting the exact agent under watch (authenticated with
// its own task-scoped token) force an unthrottled scan of itself.
//
// The chain is read off the real router by name rather than by response
// status: reaching the guard through a request would need to clear Auth
// first, and what this pins is the wiring, not the guard's own behaviour
// (internal/handler/actor_guards_test.go owns that).
func TestWatchdogWritesRejectMachineActors(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)

	humanOnly := map[string]bool{
		"PUT /api/issues/{id}/watchdog/":          true,
		"DELETE /api/issues/{id}/watchdog/":       true,
		"POST /api/issues/{id}/watchdog/scan":     true,
		"POST /api/watchdog-verdicts/{id}/review": true,
		// Reads stay member-readable.
		"GET /api/issues/{id}/watchdog/":         false,
		"GET /api/issues/{id}/watchdog/verdicts": false,
	}
	seen := map[string]bool{}

	if err := chi.Walk(router, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		want, tracked := humanOnly[key]
		if !tracked {
			return nil
		}
		seen[key] = true
		guarded := false
		for _, mw := range mws {
			if runtime.FuncForPC(reflect.ValueOf(mw).Pointer()).Name() == "github.com/multica-ai/multica/server/internal/handler.RequireHumanActor" {
				guarded = true
			}
		}
		if guarded != want {
			t.Errorf("%s RequireHumanActor = %v, want %v", key, guarded, want)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk router: %v", err)
	}

	for key := range humanOnly {
		if !seen[key] {
			t.Errorf("%s is not registered — update this test with the route's new shape", key)
		}
	}
}
