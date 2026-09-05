package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/mcpgov"
)

// Governed MCP gateway (K77): the daemon wraps every stdio/http server of the
// mcp_config, hides never tools, refuses or gates calls by class, masks secret
// shapes in results and reports each call. The fake server is this test binary
// re-executed (no user-installed CLI is ever run), guarded by an env variable.

const fakeMcpServerChildEnv = "MULTICA_TEST_FAKE_MCP_SERVER"

var fakeMcpTools = []map[string]any{
	{"name": "get_issue", "description": "Read one issue", "inputSchema": map[string]any{"type": "object"}},
	{"name": "create_issue", "description": "Create an issue", "inputSchema": map[string]any{"type": "object"}},
	{"name": "send_email", "description": "Send an email", "inputSchema": map[string]any{"type": "object"}},
	{"name": "read_api_key", "description": "Read the api key", "inputSchema": map[string]any{"type": "object"}},
}

// fakeMcpAnswer is the JSON-RPC behaviour shared by the stdio child and the
// httptest upstream; nil means "notification, no reply".
func fakeMcpAnswer(raw []byte) []byte {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &request) != nil || len(request.ID) == 0 {
		return nil
	}
	var result any
	switch request.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": mcpGatewayProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fake", "version": "1"}}
	case "tools/list":
		result = map[string]any{"tools": fakeMcpTools}
	case "tools/call":
		result = map[string]any{"content": []map[string]any{{"type": "text", "text": "called " + request.Params.Name + " token=sk-abcdefghijklmnop"}}}
	default:
		result = map[string]any{}
	}
	reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	return reply
}

// TestFakeMcpServer is the stdio child: newline-delimited JSON-RPC on
// stdin/stdout until EOF. It is a no-op in the parent process.
func TestFakeMcpServer(t *testing.T) {
	if os.Getenv(fakeMcpServerChildEnv) != "1" {
		t.Skip("fake MCP server child only")
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	for scanner.Scan() {
		if reply := fakeMcpAnswer(scanner.Bytes()); reply != nil {
			_, _ = os.Stdout.Write(append(reply, '\n'))
		}
	}
}

func fakeMcpStdioEntry() map[string]any {
	return map[string]any{
		"command": os.Args[0],
		"args":    []string{"-test.run=^TestFakeMcpServer$"},
		"env":     map[string]string{fakeMcpServerChildEnv: "1"},
	}
}

type mcpReportSink struct {
	mu       sync.Mutex
	calls    []mcpCallReport
	catalogs map[string][]mcpCatalogTool
}

func (s *mcpReportSink) deps() mcpGatewayDeps {
	s.catalogs = map[string][]mcpCatalogTool{}
	return mcpGatewayDeps{
		reportCall: func(r mcpCallReport) { s.mu.Lock(); s.calls = append(s.calls, r); s.mu.Unlock() },
		reportCatalog: func(server string, tools []mcpCatalogTool) {
			s.mu.Lock()
			s.catalogs[server] = tools
			s.mu.Unlock()
		},
	}
}

func (s *mcpReportSink) last(t *testing.T) mcpCallReport {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		t.Fatal("no call was reported")
	}
	return s.calls[len(s.calls)-1]
}

// startGateway wraps one server named "issues" and returns its proxied URL.
func startGateway(t *testing.T, task Task, entry map[string]any, deps mcpGatewayDeps) (string, *mcpReportSink) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"issues": entry}})
	sink := &mcpReportSink{}
	sinkDeps := sink.deps()
	deps.reportCall, deps.reportCatalog = sinkDeps.reportCall, sinkDeps.reportCatalog
	wrapped, diagnostics, gateway, err := startTaskMcpGateway(ctx, ctx, task, "claude", config, deps, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("start gateway: %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	t.Cleanup(gateway.Close)
	var document struct {
		MCPServers map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(wrapped, &document); err != nil {
		t.Fatalf("wrapped config: %v\n%s", err, wrapped)
	}
	server := document.MCPServers["issues"]
	if server.Type != "http" || !strings.HasPrefix(server.URL, "http://127.0.0.1:") {
		t.Fatalf("server was not rewritten to a loopback url: %+v", server)
	}
	return server.URL, sink
}

func rpc(t *testing.T, url, id, method string, params any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	res, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("%s: decode: %v", method, err)
	}
	return out
}

func rpcErrorCode(reply map[string]any) (float64, string) {
	errObj, _ := reply["error"].(map[string]any)
	code, _ := errObj["code"].(float64)
	message, _ := errObj["message"].(string)
	return code, message
}

func toolNames(t *testing.T, reply map[string]any) []string {
	t.Helper()
	result, _ := reply["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	return names
}

func policyTask(trust string, def string, tools ...mcpgov.GatewayTool) Task {
	return Task{ID: "task-1", Agent: &AgentData{Name: "bot"}, McpGateway: &mcpgov.Gateway{
		TrustMode: trust,
		Servers:   []mcpgov.GatewayServer{{Name: "issues", ServerID: "srv-1", Default: def, Tools: tools}},
	}}
}

func TestMcpGatewayHidesNeverToolsAndReportsCatalog(t *testing.T) {
	task := policyTask("propose", "by_risk",
		mcpgov.GatewayTool{Name: "read_api_key", Risk: mcpgov.RiskSensitive, Class: mcpgov.ClassNever},
		mcpgov.GatewayTool{Name: "get_issue", Risk: mcpgov.RiskRead, Class: mcpgov.ClassActAlone},
	)
	url, sink := startGateway(t, task, fakeMcpStdioEntry(), mcpGatewayDeps{})
	// The CLI's own handshake is answered from the cached initialize.
	if init := rpc(t, url, "1", "initialize", map[string]any{"protocolVersion": "2025-03-26"}); init["result"] == nil {
		t.Fatalf("initialize through the proxy: %v", init)
	}
	names := toolNames(t, rpc(t, url, "2", "tools/list", map[string]any{}))
	if strings.Join(names, ",") != "get_issue,create_issue,send_email" {
		t.Fatalf("tools/list = %v, want read_api_key hidden", names)
	}
	catalog := sink.catalogs["issues"]
	if len(catalog) != 4 || catalog[3].Name != "read_api_key" || !strings.HasPrefix(catalog[3].SchemaDigest, "sha256:") {
		t.Fatalf("catalog = %+v", catalog)
	}
}

func TestMcpGatewayActAloneMasksSecretsInResult(t *testing.T) {
	task := policyTask("propose", "by_risk", mcpgov.GatewayTool{Name: "get_issue", Risk: mcpgov.RiskRead, Class: mcpgov.ClassActAlone})
	url, sink := startGateway(t, task, fakeMcpStdioEntry(), mcpGatewayDeps{})
	reply := rpc(t, url, "3", "tools/call", map[string]any{"name": "get_issue", "arguments": map[string]any{"id": "1"}})
	raw, _ := json.Marshal(reply)
	if strings.Contains(string(raw), "sk-abcdefghijklmnop") || !strings.Contains(string(raw), "***") {
		t.Fatalf("secret shape reached the model: %s", raw)
	}
	report := sink.last(t)
	if report.Result != "success" || report.Tool != "get_issue" || report.ServerID != "srv-1" || !report.First || strings.Join(report.Flags, ",") != "secret_masked" {
		t.Fatalf("report = %+v", report)
	}
	rpc(t, url, "4", "tools/call", map[string]any{"name": "get_issue"})
	if sink.last(t).First {
		t.Fatal("second success must not be first")
	}
}

func TestMcpGatewayRefusesNeverTool(t *testing.T) {
	task := policyTask("propose", "by_risk", mcpgov.GatewayTool{Name: "read_api_key", Risk: mcpgov.RiskSensitive, Class: mcpgov.ClassNever})
	url, sink := startGateway(t, task, fakeMcpStdioEntry(), mcpGatewayDeps{})
	code, message := rpcErrorCode(rpc(t, url, "5", "tools/call", map[string]any{"name": "read_api_key"}))
	if code != -32004 || !strings.Contains(message, "Blocked by tool policy: read_api_key") {
		t.Fatalf("refusal = %v %q", code, message)
	}
	if report := sink.last(t); report.Result != "refused" || report.Class != mcpgov.ClassNever {
		t.Fatalf("report = %+v", report)
	}
}

func TestMcpGatewayAsksTheGateAndAttributesIt(t *testing.T) {
	srv, _ := fakeGateServer(t, "denied")
	gate := newApprovalGateClient(srv.URL, "mat_test", "task-1")
	task := policyTask("propose", "by_risk", mcpgov.GatewayTool{Name: "send_email", Risk: mcpgov.RiskExternal, Class: mcpgov.ClassAsk})
	url, sink := startGateway(t, task, fakeMcpStdioEntry(), mcpGatewayDeps{gate: gate})
	code, message := rpcErrorCode(rpc(t, url, "6", "tools/call", map[string]any{"name": "send_email", "arguments": map[string]any{"to": "a@b"}}))
	if code != -32004 || !strings.Contains(message, "a human denied this tool call") {
		t.Fatalf("gated = %v %q", code, message)
	}
	if report := sink.last(t); report.Result != "gated" || report.GateID != "gate-1" {
		t.Fatalf("report = %+v", report)
	}
	// No gate client at all: ask fails closed.
	url2, sink2 := startGateway(t, task, fakeMcpStdioEntry(), mcpGatewayDeps{})
	if code, _ := rpcErrorCode(rpc(t, url2, "7", "tools/call", map[string]any{"name": "send_email"})); code != -32004 {
		t.Fatalf("unreachable gate must refuse, got %v", code)
	}
	if sink2.last(t).Result != "gated" {
		t.Fatalf("report = %+v", sink2.last(t))
	}
}

func TestMcpGatewayClassifiesLocallyWithoutClaimPolicy(t *testing.T) {
	srv, gateCalls := fakeGateServer(t, "denied")
	gate := newApprovalGateClient(srv.URL, "mat_test", "task-1")
	task := Task{ID: "task-1", Agent: &AgentData{Name: "bot", TrustMode: "propose"}}
	url, sink := startGateway(t, task, fakeMcpStdioEntry(), mcpGatewayDeps{gate: gate})
	if reply := rpc(t, url, "8", "tools/call", map[string]any{"name": "get_issue"}); reply["result"] == nil {
		t.Fatalf("get_issue must run alone: %v", reply)
	}
	if report := sink.last(t); report.Risk != mcpgov.RiskRead || report.Class != mcpgov.ClassActAlone || report.ServerID != "" {
		t.Fatalf("report = %+v", report)
	}
	if code, _ := rpcErrorCode(rpc(t, url, "9", "tools/call", map[string]any{"name": "send_email"})); code != -32004 || gateCalls.Load() == 0 {
		t.Fatalf("send_email under propose must ask (code %v, gate calls %d)", code, gateCalls.Load())
	}
	if report := sink.last(t); report.Risk != mcpgov.RiskExternal || report.Class != mcpgov.ClassAsk {
		t.Fatalf("report = %+v", report)
	}
	// Sensitive tools are asked under propose too, and the catalog is still sent.
	if _, class := mcpPolicyFor(task, "issues").classify("read_api_key", ""); class != mcpgov.ClassAsk {
		t.Fatalf("read_api_key class = %s", class)
	}
	if len(sink.catalogs["issues"]) != 4 {
		t.Fatalf("catalog = %+v", sink.catalogs)
	}
}

func TestMcpGatewayHTTPUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		reply := fakeMcpAnswer(raw)
		if reply == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Mcp-Session-Id", "s-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(reply)
	}))
	t.Cleanup(upstream.Close)
	task := policyTask("propose", "by_risk", mcpgov.GatewayTool{Name: "read_api_key", Risk: mcpgov.RiskSensitive, Class: mcpgov.ClassNever})
	url, sink := startGateway(t, task, map[string]any{"type": "streamable-http", "url": upstream.URL, "headers": map[string]string{"X-Api-Key": "k"}}, mcpGatewayDeps{})
	init, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "1", "method": "initialize", "params": map[string]any{}})
	res, err := http.Post(url, "application/json", bytes.NewReader(init))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Header.Get("Mcp-Session-Id") != "s-1" {
		t.Fatalf("session header not echoed: %v", res.Header)
	}
	if names := toolNames(t, rpc(t, url, "2", "tools/list", map[string]any{})); len(names) != 3 {
		t.Fatalf("tools/list = %v", names)
	}
	if reply := rpc(t, url, "3", "tools/call", map[string]any{"name": "create_issue"}); reply["result"] == nil {
		t.Fatalf("create_issue: %v", reply)
	}
	if report := sink.last(t); report.Result != "success" || strings.Join(report.Flags, ",") != "secret_masked" {
		t.Fatalf("report = %+v", report)
	}
}

func TestMcpGatewayLeavesUnsupportedProviderAndTransportsAlone(t *testing.T) {
	ctx := context.Background()
	config := json.RawMessage(`{"mcpServers":{"issues":{"command":"/nonexistent/mcp"}}}`)
	out, diagnostics, gateway, err := startTaskMcpGateway(ctx, ctx, Task{ID: "t"}, "gemini", config, mcpGatewayDeps{}, nil)
	if err != nil || gateway != nil || !bytes.Equal(out, config) || len(diagnostics) != 1 {
		t.Fatalf("unsupported provider: out=%s diags=%v gw=%v err=%v", out, diagnostics, gateway, err)
	}
	// An sse entry is never rewritten, even under a restrictive policy; a
	// server that cannot start is dropped when its policy holds a never.
	task := policyTask("propose", "never")
	config = json.RawMessage(`{"mcpServers":{"issues":{"command":"/nonexistent/mcp"},"events":{"type":"sse","url":"http://x/sse"},"plugin-x":{"type":"http","url":"http://127.0.0.1:1/p"}}}`)
	out, diagnostics, gateway, err = startTaskMcpGateway(ctx, ctx, task, "claude", config, mcpGatewayDeps{skip: map[string]bool{"plugin-x": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gateway.Close()
	var document struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	_ = json.Unmarshal(out, &document)
	if _, dropped := document.MCPServers["issues"]; dropped || string(document.MCPServers["events"]) != `{"type":"sse","url":"http://x/sse"}` || string(document.MCPServers["plugin-x"]) != `{"type":"http","url":"http://127.0.0.1:1/p"}` {
		t.Fatalf("out = %s (diagnostics %v)", out, diagnostics)
	}
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
}

func TestStdioMcpUpstreamFraming(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var entry mcpServerEntry
	raw, _ := json.Marshal(fakeMcpStdioEntry())
	_ = json.Unmarshal(raw, &entry)
	upstream, err := startStdioMcpUpstream(ctx, entry, Task{})
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.close()
	// A notification gets no reply and does not block the next request.
	if body, _, err := upstream.call(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{}}`), nil); err != nil || body != nil {
		t.Fatalf("notification: body=%s err=%v", body, err)
	}
	body, _, err := upstream.call(ctx, []byte(`{"jsonrpc":"2.0","id":7,"method":"ping"}`), nil)
	if err != nil || !strings.Contains(string(body), `"id":7`) {
		t.Fatalf("ping: body=%s err=%v", body, err)
	}
	// Responses are routed by id even when several calls are in flight.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("c%d", i)
			body, _, err := upstream.call(ctx, []byte(`{"jsonrpc":"2.0","id":"`+id+`","method":"ping"}`), nil)
			if err != nil || !strings.Contains(string(body), `"id":"`+id+`"`) {
				t.Errorf("call %s: body=%s err=%v", id, body, err)
			}
		}(i)
	}
	wg.Wait()
}
