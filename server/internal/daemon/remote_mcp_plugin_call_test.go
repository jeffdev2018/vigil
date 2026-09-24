package daemon

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

// The broker proxies a Plugin-contributed mcp hook's tools/call straight to
// the plugin's own MCP server, so the server is never otherwise on this path
// and never sees InvokeHook's rate limit, circuit breaker or invocation log.
// beginPluginCall / reportPluginCall are how the daemon gives it back; these
// pin that the broker actually calls them, only for plugin contributions, and
// refuses locally when the server does.

func pluginCallProxy(t *testing.T, upstream *httptest.Server, begin remoteMCPCallBeginner, report remoteMCPCallReporter) *remoteMCPProxy {
	t.Helper()
	endpoint, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}
	return &remoteMCPProxy{
		endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
		connection: remotemcp.Connection{
			ContributionID: remotemcp.PluginContributionPrefix + "install-1:toolbox",
			ApprovedTools:  []remotemcp.Tool{{Name: "allowed"}},
		},
		beginPluginCall:  begin,
		reportPluginCall: report,
	}
}

func TestRemoteMCPProxyRefusesPluginCallLocallyWhenServerRefuses(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalled = true }))
	defer upstream.Close()

	reportCalled := false
	proxy := pluginCallProxy(t, upstream,
		func(context.Context, string) error { return errors.New("hook rate limit exceeded") },
		func(context.Context, string, string, int, string) { reportCalled = true },
	)

	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)

	if upstreamCalled {
		t.Fatal("the plugin's own MCP server must not be dialed once the server refused the call")
	}
	if !strings.Contains(response.Body.String(), "Plugin call refused") {
		t.Fatalf("response = %s, want a plugin-call-refused error", response.Body.String())
	}
	// The call was never admitted, so there is nothing to report the outcome
	// of — reporting here would log an outcome for a call that never ran.
	if reportCalled {
		t.Fatal("reportPluginCall must not run for a call the server refused to begin")
	}
}

func TestRemoteMCPProxyReportsPluginCallOutcomeAfterSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	defer upstream.Close()

	var mu sync.Mutex
	var gotContribution, gotStatus string
	done := make(chan struct{})
	proxy := pluginCallProxy(t, upstream,
		func(context.Context, string) error { return nil },
		func(_ context.Context, contributionID, status string, _ int, _ string) {
			mu.Lock()
			gotContribution, gotStatus = contributionID, status
			mu.Unlock()
			close(done)
		},
	)

	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reportPluginCall was never called after a successful tools/call")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotContribution != remotemcp.PluginContributionPrefix+"install-1:toolbox" {
		t.Fatalf("reported contribution = %q", gotContribution)
	}
	if gotStatus != "ok" {
		t.Fatalf("reported status = %q, want ok", gotStatus)
	}
}

// A workspace's own Remote MCP connection (no plugin: prefix) must not go
// through the plugin gate at all — that telemetry is keyed to a plugin
// installation, and this connection has none.
func TestRemoteMCPProxySkipsPluginGateForNonPluginContribution(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)

	beginCalled, reportCalled := false, false
	proxy := &remoteMCPProxy{
		endpoint: endpoint, client: upstream.Client(), path: "/capability",
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency),
		connection: remotemcp.Connection{
			ContributionID: "workspace-connection-1", // no plugin: prefix
			ApprovedTools:  []remotemcp.Tool{{Name: "allowed"}},
		},
		beginPluginCall:  func(context.Context, string) error { beginCalled = true; return nil },
		reportPluginCall: func(context.Context, string, string, int, string) { reportCalled = true },
	}

	request := httptest.NewRequest(http.MethodPost, "/capability", bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"allowed","arguments":{}}}`))
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	// Give the (would-be) async report goroutine a moment, so a false pass
	// isn't just "the goroutine hadn't run yet".
	time.Sleep(50 * time.Millisecond)
	if beginCalled || reportCalled {
		t.Fatalf("beginCalled=%v reportCalled=%v, want both false for a non-plugin contribution", beginCalled, reportCalled)
	}
}
