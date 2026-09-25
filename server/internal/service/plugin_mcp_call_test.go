package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The MCP transport's tools/call never reaches InvokeHook — the daemon's
// broker dials the plugin's own MCP server directly — so before
// BeginAgentMCPCall / ReportAgentMCPCallOutcome existed, an mcp hook had no
// rate limit, no circuit breaker, and left no record a call ever happened.
// These pin that the same enforcement an http hook gets through InvokeHook
// now also covers the mcp path.

func recentSince(window time.Duration) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(-window), Valid: true}
}

func setupMCPCallFixture(t *testing.T) (svc *PluginService, installation db.PluginInstallation) {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("mcpcall-owner-%d", suffix), fmt.Sprintf("mcpcall-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("mcpcall-ws-%d", suffix), fmt.Sprintf("mcpcall-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")

	installationID := fx.Insert(t, "plugin_installation", testutil.Cols{
		"workspace_id":       ws,
		"plugin_key":         fmt.Sprintf("com.example.mcpcall-%d", suffix),
		"version":            "1.0.0",
		"manifest":           testutil.Raw(`'{"manifest_version":1}'::jsonb`),
		"package_version_id": testutil.Raw("gen_random_uuid()"),
		"enabled":            true,
	})

	queries := db.New(pool)
	svc = &PluginService{Queries: queries}
	installation, err := queries.GetWorkspacePluginInstallation(context.Background(), db.GetWorkspacePluginInstallationParams{
		WorkspaceID: util.MustParseUUID(ws), ID: util.MustParseUUID(installationID),
	})
	if err != nil {
		t.Fatalf("load installation: %v", err)
	}
	return svc, installation
}

func TestBeginAgentMCPCallRefusesDisabledInstallation(t *testing.T) {
	svc, installation := setupMCPCallFixture(t)
	installation.Enabled = false
	err := svc.BeginAgentMCPCall(context.Background(), installation, "toolbox")
	if err == nil {
		t.Fatal("BeginAgentMCPCall on a disabled installation must refuse")
	}
	var pluginErr *PluginError
	if !errors.As(err, &pluginErr) || pluginErr.Kind != PluginErrorForbidden {
		t.Fatalf("err = %v, want PluginErrorForbidden", err)
	}
}

func TestBeginAgentMCPCallRefusesWhenBreakerOpen(t *testing.T) {
	svc, installation := setupMCPCallFixture(t)
	ctx := context.Background()

	// Seed enough non-ok invocations to trip the breaker — the same signal
	// CountRecentPluginFailures feeds InvokeHook's own breaker check.
	for i := 0; i < hookBreakerThreshold; i++ {
		svc.recordInvocation(ctx, agentMCPInvocation(installation, "toolbox"), "failed", 1, 0, "seed failure")
	}

	err := svc.BeginAgentMCPCall(ctx, installation, "toolbox")
	if err == nil {
		t.Fatal("BeginAgentMCPCall must refuse once the breaker is open")
	}
	var pluginErr *PluginError
	if !errors.As(err, &pluginErr) || pluginErr.Kind != PluginErrorUnavailable {
		t.Fatalf("err = %v, want PluginErrorUnavailable", err)
	}
}

func TestBeginAgentMCPCallRecordsAnAdmittedInvocation(t *testing.T) {
	svc, installation := setupMCPCallFixture(t)
	ctx := context.Background()

	if err := svc.BeginAgentMCPCall(ctx, installation, "toolbox"); err != nil {
		t.Fatalf("BeginAgentMCPCall: %v", err)
	}

	count, err := svc.Queries.CountRecentPluginInvocations(ctx, db.CountRecentPluginInvocationsParams{
		InstallationID: installation.ID, HookKey: "toolbox",
		CreatedAt: recentSince(time.Minute),
	})
	if err != nil {
		t.Fatalf("count invocations: %v", err)
	}
	if count != 1 {
		t.Fatalf("recorded %d invocations for the admitted call, want 1", count)
	}
}

func TestReportAgentMCPCallOutcomeRecordsGivenStatusAndDefaultsUnknown(t *testing.T) {
	svc, installation := setupMCPCallFixture(t)
	ctx := context.Background()

	svc.ReportAgentMCPCallOutcome(ctx, installation, "toolbox", "failed", 42, "upstream exploded")
	svc.ReportAgentMCPCallOutcome(ctx, installation, "toolbox", "not-a-real-status", 0, "")

	failures, err := svc.Queries.CountRecentPluginFailures(ctx, db.CountRecentPluginFailuresParams{
		InstallationID: installation.ID, HookKey: "toolbox",
		CreatedAt: recentSince(time.Minute),
	})
	if err != nil {
		t.Fatalf("count failures: %v", err)
	}
	// Both rows must count as failures: the explicit "failed" and the
	// unrecognized class, which must never be silently read as success.
	if failures != 2 {
		t.Fatalf("recorded %d failures, want 2 (both an explicit and an unrecognized status must count)", failures)
	}
}
