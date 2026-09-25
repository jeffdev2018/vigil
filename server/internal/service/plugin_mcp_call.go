package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// Rate limit, circuit breaker and invocation log for agent-triggered
// MCP-transport tool calls.
//
// An mcp hook's tool calls never reach InvokeHook: the daemon's broker
// dials the plugin's own MCP server directly (remotemcp.Connection), because
// an MCP session is stateful and proxying every tools/call through the server
// would mean holding one MCP connection per in-flight task there. That path
// bypassed checkHookRate, HookBreakerOpen and the plugin_invocation log
// entirely — an mcp-transport hook had no rate limit, no circuit breaker, and
// left no record a call ever happened, unlike an http-transport hook calling
// InvokeHook. BeginAgentMCPCall / ReportAgentMCPCallOutcome give the daemon a
// server round-trip to close that gap without moving the MCP session itself
// through the server.

// BeginAgentMCPCall gates one MCP tools/call the daemon is about to proxy.
// The returned error, when non-nil, is a *PluginError whose Kind the handler
// maps to a status: PluginErrorQuota -> 429 (rate limit), PluginErrorUnavailable
// -> 503 (breaker open), PluginErrorForbidden -> 403 (installation disabled).
// Admission itself records an "ok"-status invocation so checkHookRate's own
// count of this call is accurate for the next one — the real outcome, which is
// what the breaker cares about, is not known until the proxied call returns
// and is reported separately via ReportAgentMCPCallOutcome.
func (s *PluginService) BeginAgentMCPCall(ctx context.Context, installation db.PluginInstallation, hookKey string) error {
	if !installation.Enabled {
		return pluginErrf(PluginErrorForbidden, "this Plugin is disabled")
	}
	if s.HookBreakerOpen(ctx, installation.ID, hookKey) {
		return pluginErrf(PluginErrorUnavailable, "hook %q circuit is open", hookKey)
	}
	if err := s.checkHookRate(ctx, installation.ID, hookKey); err != nil {
		return err
	}
	s.recordInvocation(ctx, agentMCPInvocation(installation, hookKey), "ok", 1, 0, "")
	return nil
}

// ReportAgentMCPCallOutcome records how an admitted call actually finished.
// status follows the same vocabulary InvokeHook's hookFailureStatus produces
// ("ok", "refused", "timeout", "failed"); anything else is recorded as
// "failed" rather than silently treated as success, since an unrecognized
// class from the daemon is exactly the kind of signal the breaker must not
// miss.
func (s *PluginService) ReportAgentMCPCallOutcome(ctx context.Context, installation db.PluginInstallation, hookKey, status string, latencyMs int, message string) {
	switch status {
	case "ok", "refused", "timeout", "failed":
	default:
		status = "failed"
	}
	s.recordInvocation(ctx, agentMCPInvocation(installation, hookKey), status, 1, latencyMs, message)
}

func agentMCPInvocation(installation db.PluginInstallation, hookKey string) HookInvocation {
	return HookInvocation{
		ID:           pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Installation: installation,
		Hook:         plugincontract.Hook{Key: hookKey},
		Trigger:      plugincontract.TriggerAgent,
	}
}
