package main

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
)

// middleware.Revocations is a package global that constructing a router
// assigns, so every router in a process shares whichever one was built last.
// A router built with no pool used to install a checker closed over db.New(nil),
// and every authenticated request afterwards — including through a router that
// DID have a pool — panicked into a recovered 500. That is what made the whole
// cmd/server package fail while each of its tests passed alone.
//
// The rule this pins: a router with nothing to consult installs no checker.
// handler/scim.go already treats a nil Revocations as a supported state.
func TestRouterWithoutPoolInstallsNoRevocationChecker(t *testing.T) {
	previous := middleware.Revocations
	t.Cleanup(func() { middleware.Revocations = previous })
	middleware.Revocations = nil

	NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)

	if middleware.Revocations == nil {
		return // nothing installed, which is the point
	}
	// If a future change installs one anyway, it must at least answer without
	// panicking: a checker that cannot be called is worse than none.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a router with no pool installed a revocation checker that panics: %v", r)
		}
	}()
	middleware.Revocations.RefusesTokenIssuedAt(ctx, "00000000-0000-0000-0000-000000000001", time.Now())
}
