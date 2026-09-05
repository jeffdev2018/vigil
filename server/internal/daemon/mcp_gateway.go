package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/mcpgov"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
	"github.com/multica-ai/multica/server/pkg/secretscan"
)

// Governed MCP gateway (K77), daemon side. Every server of the effective
// mcp_config is rewritten to a local HTTP endpoint the daemon owns; each
// tools/call is classified (server policy, else pattern + trust dial), gated
// or refused, its secrets substituted on the way out and masked on the way
// back, and reported to the server for attribution. The K05 remote broker
// keeps serving plugin connections; the gateway leaves those entries alone.

const mcpGatewayProtocolVersion = "2025-03-26"

// mcpCallReport is one attributed call: the wire body of
// POST /api/tasks/{id}/mcp-calls.
type mcpCallReport struct {
	Server     string   `json:"server"`
	ServerID   string   `json:"server_id"`
	Tool       string   `json:"tool"`
	Risk       string   `json:"risk"`
	Class      string   `json:"class"`
	Result     string   `json:"result"`
	GateID     string   `json:"gate_id"`
	DurationMs int64    `json:"duration_ms"`
	First      bool     `json:"first"`
	Flags      []string `json:"flags"`
}

// mcpCatalogTool is one discovered tool: an item of
// POST /api/tasks/{id}/mcp-catalog.
type mcpCatalogTool struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	SchemaDigest string `json:"schema_digest"`
}

// mcpGatewayDeps is what the gateway borrows from the run; every field may
// be nil, in which case the gateway fails closed (no gate: ask refuses) or
// stays quiet (no reporter).
type mcpGatewayDeps struct {
	gate          *approvalGateClient
	resolveSecret runSecretResolver
	reportCall    func(mcpCallReport)
	reportCatalog func(server string, tools []mcpCatalogTool)
	// skip names entries already brokered elsewhere (plugin connections).
	skip map[string]bool
}

type mcpGateway struct {
	servers   []*http.Server
	listeners []net.Listener
	upstreams []mcpUpstream
	once      sync.Once
}

func (g *mcpGateway) Close() {
	if g == nil {
		return
	}
	g.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		for _, server := range g.servers {
			_ = server.Shutdown(ctx)
		}
		for _, listener := range g.listeners {
			_ = listener.Close()
		}
		for _, upstream := range g.upstreams {
			upstream.close()
		}
	})
}

// mcpUpstream is one wrapped server: a child process on stdio or a remote
// HTTP endpoint. call forwards a raw JSON-RPC body and returns the reply body
// (nil for a notification) with any session headers to echo back.
type mcpUpstream interface {
	call(ctx context.Context, raw []byte, headers http.Header) ([]byte, http.Header, error)
	close()
}

// mcpServerPolicy decides the class of a tool for one server.
type mcpServerPolicy struct {
	name     string
	serverID string
	def      string
	trust    string
	tools    map[string]mcpgov.GatewayTool
	// listed is true when the claim named this server; a nil McpGateway
	// leaves every server unlisted and classified locally.
	listed bool
}

func mcpPolicyFor(task Task, name string) mcpServerPolicy {
	policy := mcpServerPolicy{name: name, trust: "propose", tools: map[string]mcpgov.GatewayTool{}}
	if task.Agent != nil && task.Agent.TrustMode != "" {
		policy.trust = task.Agent.TrustMode
	}
	if task.McpGateway == nil {
		return policy
	}
	if task.McpGateway.TrustMode != "" {
		policy.trust = task.McpGateway.TrustMode
	}
	for _, server := range task.McpGateway.Servers {
		if server.Name != name {
			continue
		}
		policy.listed, policy.serverID, policy.def = true, server.ServerID, server.Default
		for _, tool := range server.Tools {
			policy.tools[tool.Name] = tool
		}
	}
	return policy
}

// classify returns the risk and the class in force for a tool.
func (p mcpServerPolicy) classify(tool, description string) (risk, class string) {
	if known, ok := p.tools[tool]; ok {
		return known.Risk, known.Class
	}
	risk = mcpgov.Classify(tool, description)
	switch p.def {
	case mcpgov.ClassNever:
		return risk, mcpgov.ClassNever
	case mcpgov.ClassAsk:
		return risk, mcpgov.Weaker(mcpgov.ClassAsk, mcpgov.Ceiling(p.trust, risk))
	}
	return risk, mcpgov.Weaker(mcpgov.ClassForRisk(risk), mcpgov.Ceiling(p.trust, risk))
}

// restrictive reports whether passing this server through unguarded would
// lose a never or ask decision.
func (p mcpServerPolicy) restrictive() bool {
	if !p.listed || p.def == mcpgov.ClassNever || p.def == mcpgov.ClassAsk {
		return true
	}
	for _, tool := range p.tools {
		if tool.Class != mcpgov.ClassActAlone {
			return true
		}
	}
	return false
}

// startTaskMcpGateway rewrites every stdio/http server of mcpConfig to a
// local governed endpoint. A server that cannot be wrapped is passed through
// untouched when its policy lets everything run alone, and dropped otherwise:
// a broken gateway must not block a run, but never/ask must still hold.
func startTaskMcpGateway(setupCtx, lifetimeCtx context.Context, task Task, provider string, mcpConfig json.RawMessage, deps mcpGatewayDeps, logger *slog.Logger) (json.RawMessage, []string, *mcpGateway, error) {
	trimmed := bytes.TrimSpace(mcpConfig)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return mcpConfig, nil, nil, nil
	}
	if !providerSupportsRemoteMCPBroker(provider) {
		return mcpConfig, []string{"MCP gateway is incompatible with provider " + provider + "; tools run ungoverned"}, nil, nil
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &document); err != nil {
		return nil, nil, nil, fmt.Errorf("parse mcp_config: %w", err)
	}
	container := "mcpServers"
	if _, ok := document[container]; !ok {
		if _, ok := document["mcp"]; !ok {
			return mcpConfig, nil, nil, nil
		}
		container = "mcp"
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(document[container], &servers); err != nil {
		return nil, nil, nil, fmt.Errorf("parse mcp_config %s: %w", container, err)
	}
	gateway := &mcpGateway{}
	diagnostics := make([]string, 0)
	for name, raw := range servers {
		if deps.skip[name] {
			continue
		}
		var entry mcpServerEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			diagnostics = append(diagnostics, "MCP server "+name+" has an unreadable entry; left ungoverned")
			continue
		}
		policy := mcpPolicyFor(task, name)
		upstream, err := entry.open(lifetimeCtx, task)
		if err == nil {
			err = gateway.serve(setupCtx, lifetimeCtx, task.ID, policy, upstream, deps, logger, servers)
		}
		if err != nil {
			if policy.restrictive() && !errors.Is(err, errMcpTransportUnproxied) {
				delete(servers, name)
				diagnostics = append(diagnostics, "MCP server "+name+" dropped: "+err.Error())
			} else {
				diagnostics = append(diagnostics, "MCP server "+name+" left ungoverned: "+err.Error())
			}
		}
	}
	if len(gateway.servers) == 0 {
		gateway.Close()
		if len(diagnostics) == 0 {
			return mcpConfig, nil, nil, nil
		}
	} else {
		go func() {
			<-lifetimeCtx.Done()
			gateway.Close()
		}()
	}
	document[container], _ = json.Marshal(servers)
	out, err := json.Marshal(document)
	if err != nil {
		gateway.Close()
		return nil, diagnostics, nil, err
	}
	return out, diagnostics, gateway, nil
}

// serve discovers the upstream's catalogue, then publishes it behind a
// loopback listener and rewrites the entry to point there.
func (g *mcpGateway) serve(setupCtx, lifetimeCtx context.Context, taskID string, policy mcpServerPolicy, upstream mcpUpstream, deps mcpGatewayDeps, logger *slog.Logger, servers map[string]json.RawMessage) error {
	catalog, err := mcpDiscoverTools(setupCtx, upstream)
	if err != nil {
		upstream.close()
		return err
	}
	if deps.reportCatalog != nil && (policy.serverID != "" || !policy.listed) {
		deps.reportCatalog(policy.name, catalog)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		upstream.close()
		return fmt.Errorf("listen for MCP gateway: %w", err)
	}
	token, err := randomBrokerToken()
	if err != nil {
		_ = listener.Close()
		upstream.close()
		return err
	}
	descriptions := make(map[string]string, len(catalog))
	for _, tool := range catalog {
		descriptions[tool.Name] = tool.Description
	}
	proxy := &mcpGatewayProxy{
		taskID: taskID, policy: policy, upstream: upstream, deps: deps, path: "/" + token,
		descriptions: descriptions, seen: map[string]bool{},
		semaphore: make(chan struct{}, remoteMCPMaxConcurrency), logger: logger,
	}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second}
	g.servers = append(g.servers, server)
	g.listeners = append(g.listeners, listener)
	g.upstreams = append(g.upstreams, upstream)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && logger != nil {
			logger.Warn("MCP gateway stopped unexpectedly", "task_id", taskID, "server", policy.name, "error", serveErr)
		}
	}()
	servers[policy.name], _ = json.Marshal(map[string]string{"type": "http", "url": "http://" + listener.Addr().String() + proxy.path})
	return nil
}

// errMcpTransportUnproxied marks an entry the gateway cannot speak to (sse,
// unknown); it is always left untouched for the CLI.
var errMcpTransportUnproxied = errors.New("transport is not proxied")

// mcpServerEntry is the subset of one mcp_config entry the gateway reads.
type mcpServerEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Cwd     string            `json:"cwd"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

func (e mcpServerEntry) open(lifetimeCtx context.Context, task Task) (mcpUpstream, error) {
	switch {
	case e.Command != "":
		return startStdioMcpUpstream(lifetimeCtx, e, task)
	case e.URL != "":
		switch strings.ToLower(e.Type) {
		case "", "http", "streamable-http", "streamable_http":
			headers := make(http.Header, len(e.Headers))
			for key, value := range e.Headers {
				headers.Set(key, value)
			}
			return &httpMcpUpstream{url: e.URL, headers: headers, client: &http.Client{Timeout: 90 * time.Second}}, nil
		}
		return nil, fmt.Errorf("%w: %q", errMcpTransportUnproxied, e.Type)
	}
	return nil, errors.New("entry has neither command nor url")
}

// mcpDiscoverTools runs the gateway's own initialize / initialized /
// tools/list handshake against the upstream and returns the catalogue.
func mcpDiscoverTools(ctx context.Context, upstream mcpUpstream) ([]mcpCatalogTool, error) {
	initialize, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "gw-init", "method": "initialize",
		"params": map[string]any{
			"protocolVersion": mcpGatewayProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "multica-gateway", "version": "1"},
		},
	})
	headers := http.Header{}
	_, replied, err := upstream.call(ctx, initialize, headers)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if session := replied.Get("Mcp-Session-Id"); session != "" {
		headers.Set("Mcp-Session-Id", session)
	}
	headers.Set("Mcp-Protocol-Version", mcpGatewayProtocolVersion)
	if _, _, err := upstream.call(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), headers); err != nil {
		return nil, fmt.Errorf("initialized: %w", err)
	}
	body, _, err := upstream.call(ctx, []byte(`{"jsonrpc":"2.0","id":"gw-list","method":"tools/list","params":{}}`), headers)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	var response struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	if response.Error != nil {
		return nil, errors.New("tools/list: " + response.Error.Message)
	}
	catalog := make([]mcpCatalogTool, 0, len(response.Result.Tools))
	for _, tool := range response.Result.Tools {
		schema := tool.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		canonical, err := canonicalRemoteMCPJSON(schema)
		if err != nil {
			return nil, fmt.Errorf("tool %q input schema: %w", tool.Name, err)
		}
		catalog = append(catalog, mcpCatalogTool{Name: tool.Name, Description: tool.Description, SchemaDigest: remotemcp.DigestBytes(canonical)})
	}
	return catalog, nil
}

type mcpGatewayProxy struct {
	taskID       string
	policy       mcpServerPolicy
	upstream     mcpUpstream
	deps         mcpGatewayDeps
	path         string
	descriptions map[string]string
	mu           sync.Mutex
	seen         map[string]bool
	semaphore    chan struct{}
	logger       *slog.Logger
}

func (proxy *mcpGatewayProxy) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path != proxy.path || request.Method != http.MethodPost {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	select {
	case proxy.semaphore <- struct{}{}:
		defer func() { <-proxy.semaphore }()
	default:
		writeRemoteMCPError(w, nil, -32003, "MCP gateway concurrency limit exceeded")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, remoteMCPMaxRequestBytes+1))
	if err != nil || len(raw) > remoteMCPMaxRequestBytes {
		writeRemoteMCPError(w, nil, -32600, "MCP request is invalid")
		return
	}
	var rpcRequest remoteMCPRequest
	if err := json.Unmarshal(raw, &rpcRequest); err != nil || rpcRequest.JSONRPC != "2.0" {
		writeRemoteMCPError(w, rpcRequest.ID, -32600, "MCP request is invalid")
		return
	}
	if !allowedRemoteMCPMethod(rpcRequest.Method) {
		writeRemoteMCPError(w, rpcRequest.ID, -32601, "Method is not available through the MCP gateway")
		return
	}
	if rpcRequest.Method == "tools/call" {
		proxy.serveToolCall(w, request, raw, rpcRequest)
		return
	}
	body, headers, err := proxy.upstream.call(request.Context(), raw, request.Header)
	if err != nil {
		writeRemoteMCPError(w, rpcRequest.ID, -32000, "MCP server is unavailable")
		return
	}
	if rpcRequest.Method == "tools/list" {
		if body, err = proxy.filterToolsList(body); err != nil {
			writeRemoteMCPError(w, rpcRequest.ID, -32000, "MCP server returned an invalid tools list")
			return
		}
	}
	writeMcpGatewayReply(w, body, headers)
}

func (proxy *mcpGatewayProxy) serveToolCall(w http.ResponseWriter, request *http.Request, raw []byte, rpcRequest remoteMCPRequest) {
	started := time.Now()
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rpcRequest.Params, &params); err != nil || params.Name == "" {
		writeRemoteMCPError(w, rpcRequest.ID, -32602, "MCP tool call names no tool")
		return
	}
	risk, class := proxy.policy.classify(params.Name, proxy.descriptions[params.Name])
	report := mcpCallReport{Server: proxy.policy.name, ServerID: proxy.policy.serverID, Tool: params.Name, Risk: risk, Class: class, Flags: []string{}}
	defer func() {
		report.DurationMs = time.Since(started).Milliseconds()
		if report.Result == "success" {
			proxy.mu.Lock()
			report.First = !proxy.seen[params.Name]
			proxy.seen[params.Name] = true
			proxy.mu.Unlock()
		}
		if proxy.logger != nil {
			proxy.logger.Info("MCP gateway call", "task_id", proxy.taskID, "server", report.Server, "tool", report.Tool, "class", class, "result", report.Result, "duration_ms", report.DurationMs)
		}
		if proxy.deps.reportCall != nil {
			proxy.deps.reportCall(report)
		}
	}()
	switch class {
	case mcpgov.ClassNever:
		report.Result = "refused"
		writeRemoteMCPError(w, rpcRequest.ID, -32004, "Blocked by tool policy: "+params.Name+" is not allowed for this agent")
		return
	case mcpgov.ClassAsk:
		report.Result = "gated"
		if proxy.deps.gate == nil {
			writeRemoteMCPError(w, rpcRequest.ID, -32004, "Blocked by approval gate: the approval gate could not be reached")
			return
		}
		details := map[string]any{
			"server": proxy.policy.name, "tool": params.Name, "risk": risk, "class": class,
			"params": secretscan.JSON(rpcRequest.Params), "paths": gateParamPaths(rpcRequest.Params),
		}
		status, gateID, err := proxy.deps.gate.AskWithID(request.Context(), "mcp_tool_call", "MCP tool "+proxy.policy.name+"/"+params.Name, details, gateDefaultTimeout)
		report.GateID = gateID
		if err != nil {
			if proxy.logger != nil {
				proxy.logger.Warn("approval gate: ask failed, tool refused", "tool", params.Name, "error", err)
			}
			writeRemoteMCPError(w, rpcRequest.ID, -32004, "Blocked by approval gate: the approval gate could not be reached")
			return
		}
		if status != "approved" {
			writeRemoteMCPError(w, rpcRequest.ID, -32004, "Blocked by approval gate: a human "+status+" this tool call")
			return
		}
	}
	// Run-scoped secrets (K09): tokens become values here, for this call only.
	substituted, err := substituteRunSecrets(request.Context(), raw, proxy.deps.resolveSecret)
	if err != nil {
		report.Result = "secret_refused"
		writeRemoteMCPError(w, rpcRequest.ID, -32005, "Run secret refused: "+err.Error())
		return
	}
	body, headers, err := proxy.upstream.call(request.Context(), substituted, request.Header)
	if err != nil {
		report.Result = "remote_error"
		writeRemoteMCPError(w, rpcRequest.ID, -32000, "MCP server is unavailable")
		return
	}
	var reply struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &reply) != nil {
		report.Result = "remote_error"
		writeRemoteMCPError(w, rpcRequest.ID, -32000, "MCP server returned an invalid response")
		return
	}
	if len(reply.Error) > 0 {
		report.Result = "remote_error"
	} else {
		report.Result = "success"
	}
	// Tool output is evidence, not authority: a secret shape never reaches the model.
	if secretscan.Found(string(body)) {
		body = secretscan.JSON(body)
		report.Flags = append(report.Flags, "secret_masked")
	}
	writeMcpGatewayReply(w, body, headers)
}

// filterToolsList drops every tool whose effective class is never.
func (proxy *mcpGatewayProxy) filterToolsList(raw []byte) ([]byte, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	if _, failed := response["error"]; failed {
		return raw, nil
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(response["result"], &result); err != nil {
		return nil, err
	}
	var tools []json.RawMessage
	if err := json.Unmarshal(result["tools"], &tools); err != nil {
		return nil, err
	}
	kept := make([]json.RawMessage, 0, len(tools))
	for _, tool := range tools {
		var meta struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(tool, &meta); err != nil {
			return nil, err
		}
		if _, class := proxy.policy.classify(meta.Name, meta.Description); class == mcpgov.ClassNever {
			continue
		}
		kept = append(kept, tool)
	}
	result["tools"], _ = json.Marshal(kept)
	response["result"], _ = json.Marshal(result)
	return json.Marshal(response)
}

// writeMcpGatewayReply answers the CLI: 202 for a notification, JSON otherwise.
func writeMcpGatewayReply(w http.ResponseWriter, body []byte, headers http.Header) {
	for _, header := range []string{"Mcp-Session-Id", "Mcp-Protocol-Version"} {
		if value := headers.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	if body == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// httpMcpUpstream forwards to a Streamable HTTP server; SSE replies are
// reduced to their data frame so the gateway can read them.
type httpMcpUpstream struct {
	url     string
	headers http.Header
	client  *http.Client
}

func (u *httpMcpUpstream) call(ctx context.Context, raw []byte, headers http.Header) ([]byte, http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.url, bytes.NewReader(raw))
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	for key, values := range u.headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	for _, header := range []string{"Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID"} {
		if value := headers.Get(header); value != "" {
			request.Header.Set(header, value)
		}
	}
	response, err := u.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, remotemcp.MaxResponseBytes+1))
	if err != nil || len(body) > remotemcp.MaxResponseBytes {
		return nil, nil, errors.New("MCP response exceeded the allowed limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("MCP server answered %d", response.StatusCode)
	}
	if response.StatusCode == http.StatusAccepted || len(bytes.TrimSpace(body)) == 0 {
		return nil, response.Header, nil
	}
	body, err = decodeRemoteMCPSSEData(response.Header.Get("Content-Type"), body)
	if err != nil {
		return nil, nil, err
	}
	return body, response.Header, nil
}

func (u *httpMcpUpstream) close() {}

// stdioMcpUpstream owns a child process speaking newline-delimited JSON-RPC.
// The gateway initializes it once; the CLI's own initialize is answered from
// the cached result and its initialized notification swallowed.
type stdioMcpUpstream struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan []byte
	initRes json.RawMessage
	done    chan struct{}
}

func startStdioMcpUpstream(lifetimeCtx context.Context, entry mcpServerEntry, task Task) (*stdioMcpUpstream, error) {
	cmd := exec.CommandContext(lifetimeCtx, entry.Command, entry.Args...)
	cmd.Dir = entry.Cwd
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if key, value, ok := strings.Cut(kv, "="); ok {
			env[key] = value
		}
	}
	if task.Agent != nil {
		for key, value := range task.Agent.CustomEnv {
			env[key] = value
		}
	}
	for key, value := range entry.Env {
		env[key] = value
	}
	cmd.Env = make([]string, 0, len(env))
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", entry.Command, err)
	}
	u := &stdioMcpUpstream{cmd: cmd, stdin: stdin, pending: map[string]chan []byte{}, done: make(chan struct{})}
	go u.read(stdout)
	return u, nil
}

// read routes each response line to its waiting caller by id; anything
// else (server notifications, requests, noise) is dropped.
func (u *stdioMcpUpstream) read(stdout io.Reader) {
	defer close(u.done)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), remotemcp.MaxResponseBytes+1)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(line, &message) != nil || len(message.ID) == 0 || message.Method != "" {
			continue
		}
		u.mu.Lock()
		waiting := u.pending[string(message.ID)]
		delete(u.pending, string(message.ID))
		u.mu.Unlock()
		if waiting != nil {
			waiting <- append([]byte(nil), line...)
		}
	}
}

func (u *stdioMcpUpstream) write(raw []byte) error {
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	if _, err := u.stdin.Write(append(bytes.TrimSpace(raw), '\n')); err != nil {
		return fmt.Errorf("write to MCP server: %w", err)
	}
	return nil
}

func (u *stdioMcpUpstream) call(ctx context.Context, raw []byte, _ http.Header) ([]byte, http.Header, error) {
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, nil, err
	}
	if len(message.ID) == 0 {
		if message.Method == "notifications/initialized" && u.initRes != nil {
			return nil, nil, nil
		}
		return nil, nil, u.write(raw)
	}
	if message.Method == "initialize" && u.initRes != nil {
		reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": u.initRes})
		return reply, nil, nil
	}
	waiting := make(chan []byte, 1)
	u.mu.Lock()
	u.pending[string(message.ID)] = waiting
	u.mu.Unlock()
	if err := u.write(raw); err != nil {
		u.forget(string(message.ID))
		return nil, nil, err
	}
	select {
	case reply := <-waiting:
		if message.Method == "initialize" {
			var parsed struct {
				Result json.RawMessage `json:"result"`
			}
			if json.Unmarshal(reply, &parsed) == nil && len(parsed.Result) > 0 {
				u.initRes = parsed.Result
			}
		}
		return reply, nil, nil
	case <-u.done:
		u.forget(string(message.ID))
		return nil, nil, errors.New("MCP server exited")
	case <-ctx.Done():
		u.forget(string(message.ID))
		return nil, nil, ctx.Err()
	}
}

func (u *stdioMcpUpstream) forget(id string) {
	u.mu.Lock()
	delete(u.pending, id)
	u.mu.Unlock()
}

func (u *stdioMcpUpstream) close() {
	_ = u.stdin.Close()
	if u.cmd.Process != nil {
		_ = u.cmd.Process.Kill()
	}
	_ = u.cmd.Wait()
}

// mcpGatewayDepsFor wires the gateway to this run: the K05 gate client for
// ask tools, the K09 secret resolver, and fire-and-forget reporters for call
// attribution and catalogue discovery. A run without a task-scoped token gets
// no gate (ask fails closed) and no reporters. brokered names the entries the
// K05 remote broker already serves, which the gateway leaves alone.
func (d *Daemon) mcpGatewayDepsFor(task Task, brokered json.RawMessage, log *slog.Logger) mcpGatewayDeps {
	deps := mcpGatewayDeps{resolveSecret: d.runSecretResolver(task), skip: map[string]bool{}}
	var overlay struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(brokered, &overlay) == nil {
		for name := range overlay.MCPServers {
			deps.skip[name] = true
		}
	}
	token, err := taskScopedAuthToken(task)
	if err != nil {
		return deps
	}
	client := newApprovalGateClient(d.cfg.ServerBaseURL, token, task.ID)
	deps.gate = client
	post := func(path string, body any) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, code, err := client.do(ctx, http.MethodPost, "/api/tasks/"+task.ID+path, body)
			if err == nil && code >= 300 {
				err = fmt.Errorf("server answered %d", code)
			}
			if err != nil && log != nil {
				log.Warn("MCP gateway report failed", "path", path, "error", err)
			}
		}()
	}
	deps.reportCall = func(report mcpCallReport) { post("/mcp-calls", report) }
	deps.reportCatalog = func(server string, tools []mcpCatalogTool) {
		post("/mcp-catalog", map[string]any{"server": server, "tools": tools})
	}
	return deps
}
