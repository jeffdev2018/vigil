package main

import (
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

// Every triage write is human-only: agents may suggest verdicts, humans
// decide. PATCH /api/triage/sources/{id} — the gate / direct / blocked kill
// switch — shipped without the guard its neighbours carry, so a task token
// could silently reopen a blocked source or re-gate a working one.
//
// The chain is read off the real router by name rather than by response
// status: reaching the guard through a request would need to clear Auth
// first, and what this pins is the wiring, not the guard's own behaviour
// (internal/handler/actor_guards_test.go owns that).
func TestTriageWritesRejectMachineActors(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)

	humanOnly := map[string]bool{
		"POST /api/triage/items/batch-accept": true,
		"POST /api/triage/items/{id}/accept":  true,
		"POST /api/triage/items/{id}/dismiss": true,
		"POST /api/triage/items/{id}/reopen":  true,
		"PATCH /api/triage/sources/{id}":      true,
		// Reads stay member-readable.
		"GET /api/triage/stats":       false,
		"GET /api/triage/items":       false,
		"GET /api/triage/suggestions": false,
	}
	seen := map[string]bool{}

	if err := chi.Walk(router, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		want, tracked := humanOnly[key]
		if !tracked {
			if strings.HasPrefix(route, "/api/triage/") {
				t.Errorf("%s is a triage route this test does not know about — decide whether it is human-only and list it", key)
			}
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
