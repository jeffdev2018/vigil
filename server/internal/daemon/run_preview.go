package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Run previews (F12).
//
// The `run` lifecycle script is the third and only long-lived one: setup builds
// the environment and exits, archive tidies up and exits, run STAYS UP for the
// length of the task. That single difference is what this file is about — it
// cannot use runLifecycleScript, which waits for the command, and it cannot be
// left unowned, because a dev server that outlives its worktree holds a port
// and serves files from a directory that no longer exists.
//
// So: own it in a process group, probe the port it was given, tell the server
// what it found, and kill the whole tree on every exit path.
//
// The daemon never listens on anything new for this. A relayed request arrives
// down the WebSocket it already holds open, and is answered by a plain local
// HTTP call to 127.0.0.1 — the same shape as health.go's local endpoints, in
// the opposite direction.

// previewProbeTimeout bounds how long the daemon waits for the dev server to
// answer before declaring the preview failed. Generous: a first `next dev`
// compiles the app before it binds. A var so a test can shorten it — the
// alternative is a unit test that genuinely sits for a minute and a half.
var previewProbeTimeout = 90 * time.Second

const (
	// previewProbeInterval is how often the port is polled while starting.
	previewProbeInterval = 200 * time.Millisecond
	// previewProbeRequestTimeout bounds ONE probe attempt.
	previewProbeRequestTimeout = 3 * time.Second

	// previewFetchTimeout bounds one relayed request against the local dev
	// server. Above the server's own patience would only mean the daemon is
	// still waiting after the visitor was already answered 502.
	previewFetchTimeout = 30 * time.Second
	// previewRequestBodyLimit / previewResponseBodyLimit cap what crosses the
	// WebSocket in each direction. The response cap is what daemonws sizes its
	// read limit from.
	previewRequestBodyLimit  = 2 << 20
	previewResponseBodyLimit = 8 << 20

	// previewStopGrace bounds the kill of the run script's process group.
	previewStopGrace = 5 * time.Second
)

// RunScript is the argv that starts this run's dev server. Empty for every mode
// but worktree, for the same reason SetupScript is: an in-place run is the
// user's own directory, and binding a port in it is a side effect on a checkout
// they did not hand over.
func (a *localDirectoryAssignment) RunScript() []string {
	if !a.UsesWorktree() || a.Ref.Lifecycle == nil {
		return nil
	}
	return a.Ref.Lifecycle.Run
}

// runPreview is one live dev server owned by this daemon.
type runPreview struct {
	taskID string
	port   int
	cancel context.CancelFunc
	done   chan struct{}
}

// previewRegistry maps task id → live preview, so the reverse RPC can find the
// port for a relayed request and the task teardown can find the process to kill.
//
// Zero value ready: the daemon is built as a struct literal in a dozen tests,
// and a registry that needed a constructor would panic in every one that never
// touches previews.
type previewRegistry struct {
	mu   sync.Mutex
	byID map[string]*runPreview
}

func (r *previewRegistry) put(p *runPreview) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byID == nil {
		r.byID = make(map[string]*runPreview)
	}
	r.byID[p.taskID] = p
}

func (r *previewRegistry) get(taskID string) (*runPreview, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[taskID]
	return p, ok
}

func (r *previewRegistry) take(taskID string) (*runPreview, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[taskID]
	delete(r.byID, taskID)
	return p, ok
}

// previewHealthPath is where the probe looks. Configurable per resource later;
// today the app root is what every dev server answers on.
const previewHealthPath = "/"

// startRunPreview launches the run script, probes its port, and reports the
// outcome to the server. It returns once the preview has been declared — the
// script keeps running in the background until stopRunPreview.
//
// Everything here is best-effort by design: a preview that could not start is a
// missing convenience, never a failed run. The agent's work does not depend on
// the dev server coming up, so a failure is recorded as an `error` preview and
// the task proceeds.
func (d *Daemon) startRunPreview(ctx context.Context, task Task, argv []string, workDir, envRoot string, env map[string]string, log *slog.Logger) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return
	}
	port, err := previewPortFromEnv(env)
	if err != nil {
		log.Warn("run preview: no port block for this run; skipping", "error", err)
		return
	}

	scheme := "loopback"
	if d.previewRelayCapable() {
		scheme = "relay"
	}

	// context.WithoutCancel: the preview outlives the PREPARE context that
	// started it — it has to stay up for the whole run — and is torn down
	// explicitly by stopRunPreview instead.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	preview := &runPreview{taskID: task.ID, port: port, cancel: cancel, done: make(chan struct{})}
	d.previews.put(preview)

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), lifecycleEnviron(env)...)
	logFile := previewLogWriter(envRoot, log)
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	go func() {
		defer close(preview.done)
		if logFile != nil {
			defer logFile.Close()
		}
		// processtree owns the whole group: a `npm run dev` that forks a bundler
		// must not survive the cancel that ends the run.
		if err := processtree.Run(runCtx, cmd, previewStopGrace); err != nil && runCtx.Err() == nil {
			log.Warn("run preview: the run script exited", "error", err)
		}
	}()

	if err := d.probePreviewPort(runCtx, port); err != nil {
		// A cancelled probe means the RUN ended while the dev server was still
		// coming up, and stopRunPreview has already reported `stopped`. Writing
		// `error` on top of it would tell the user their script failed when it
		// simply ran out of run to serve.
		if runCtx.Err() != nil {
			return
		}
		log.Warn("run preview: the run script never answered", "port", port, "error", err)
		d.reportPreview(task.ID, port, scheme, "error", previewHealthPath, previewStartError(err, envRoot), log)
		return
	}
	log.Info("run preview: ready", "port", port, "scheme", scheme)
	d.reportPreview(task.ID, port, scheme, "ready", previewHealthPath, "", log)
}

// stopRunPreview kills the process group and tells the server the preview is
// gone. Safe to call for a task that never had one.
func (d *Daemon) stopRunPreview(taskID string, log *slog.Logger) {
	preview, ok := d.previews.take(taskID)
	if !ok {
		return
	}
	preview.cancel()
	select {
	case <-preview.done:
	case <-time.After(previewStopGrace + time.Second):
		log.Warn("run preview: the run script did not stop within the grace period", "task_id", taskID)
	}
	// context.WithoutCancel is not available here — the task context is already
	// cancelled on the paths that matter (finalize, cancel), and the report is
	// the only thing that tells every open browser tab the preview is gone.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.client.DeleteRunPreview(ctx, taskID); err != nil {
		log.Warn("run preview: could not report the stop", "task_id", taskID, "error", err)
	}
}

// previewRelayCapable reports whether this daemon can serve relayed requests.
// It is the daemon's own answer, not a negotiation: the capability string it
// advertises is what the server gates on, and both come from the same list.
func (d *Daemon) previewRelayCapable() bool {
	return strings.Contains(daemonClientCapabilities(), protocol.DaemonCapabilityRunPreviewV1)
}

func previewPortFromEnv(env map[string]string) (int, error) {
	raw := env["MULTICA_PORT_BASE"]
	if raw == "" {
		return 0, errors.New("MULTICA_PORT_BASE is not set for this run")
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("MULTICA_PORT_BASE %q is not a port", raw)
	}
	return port, nil
}

// probePreviewPort polls the local port until it answers anything that proves a
// server is listening. 2xx, 3xx and 404-with-a-body all count: a single-page app
// dev server answers 404 on `/` until the router mounts, and refusing to declare
// it ready would make the feature depend on the app having a root route.
func (d *Daemon) probePreviewPort(ctx context.Context, port int) error {
	deadline := time.Now().Add(previewProbeTimeout)
	client := &http.Client{
		Timeout: previewProbeRequestTimeout,
		// A dev server that redirects to a login page is still up; following the
		// redirect would only turn one probe into two.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			fmt.Sprintf("http://127.0.0.1:%d%s", port, previewHealthPath), nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			lastErr = fmt.Errorf("the server answered %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(previewProbeInterval):
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no response")
	}
	return fmt.Errorf("nothing answered on 127.0.0.1:%d within %s: %w", port, previewProbeTimeout, lastErr)
}

func (d *Daemon) reportPreview(taskID string, port int, scheme, status, healthPath, errMsg string, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := d.client.ReportRunPreview(ctx, taskID, RunPreviewReport{
		Port: port, Scheme: scheme, Status: status, HealthPath: healthPath, Error: errMsg,
	}); err != nil {
		log.Warn("run preview: could not report the preview", "task_id", taskID, "status", status, "error", err)
	}
}

// previewStartError is what the user reads when the script never answered. The
// tail of the log is the only thing that says why, so it is what travels.
func previewStartError(err error, envRoot string) string {
	msg := err.Error()
	if envRoot == "" {
		return msg
	}
	tail := previewLogTail(envRoot)
	if tail == "" {
		return msg
	}
	return msg + "\n\n" + tail
}

// handleServerRPC answers one server→daemon RPC (F12). It runs in its own
// goroutine so a slow local fetch never stalls the read pump.
func (d *Daemon) handleServerRPC(req protocol.RPCRequestPayload) {
	timeout := previewFetchTimeout
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	status, body, err := d.dispatchServerRPC(ctx, req.Method, req.Body)
	resp := protocol.RPCResponsePayload{RequestID: req.RequestID, Status: status}
	if err != nil {
		if resp.Status < 400 {
			resp.Status = http.StatusInternalServerError
		}
		resp.Error = err.Error()
	} else {
		resp.Body = body
	}
	frame, marshalErr := json.Marshal(protocol.Message{
		Type:    protocol.EventServerRPCResponse,
		Payload: marshalRaw(resp),
	})
	if marshalErr != nil {
		d.logger.Debug("server rpc: marshal response failed", "error", marshalErr)
		return
	}
	if sendErr := d.wsRPC.send(frame); sendErr != nil {
		// The socket is gone; the server's own deadline answers the visitor.
		d.logger.Debug("server rpc: could not send response", "error", sendErr)
	}
}

func (d *Daemon) dispatchServerRPC(ctx context.Context, method string, body json.RawMessage) (int, json.RawMessage, error) {
	switch method {
	case "preview.fetch":
		return d.rpcPreviewFetch(ctx, body)
	default:
		return http.StatusNotFound, nil, fmt.Errorf("unknown server rpc method %q", method)
	}
}

// rpcPreviewFetch performs one plain local HTTP call against the run's dev
// server and hands back what it got.
//
// It resolves the port from the daemon's OWN registry rather than trusting the
// request: the server knows which task, this side knows which port, and a port
// taken from the wire would let a compromised server reach any local service.
func (d *Daemon) rpcPreviewFetch(ctx context.Context, raw json.RawMessage) (int, json.RawMessage, error) {
	var req protocol.PreviewFetchRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return http.StatusBadRequest, nil, fmt.Errorf("preview.fetch: invalid body: %w", err)
	}
	preview, ok := d.previews.get(req.TaskID)
	if !ok {
		return http.StatusGone, nil, errors.New("preview.fetch: this run has no live preview on this daemon")
	}

	var bodyReader io.Reader
	if req.Body != "" {
		decoded, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return http.StatusBadRequest, nil, fmt.Errorf("preview.fetch: body is not base64: %w", err)
		}
		if len(decoded) > previewRequestBodyLimit {
			return http.StatusRequestEntityTooLarge, nil, errors.New("preview.fetch: request body over the cap")
		}
		bodyReader = strings.NewReader(string(decoded))
	}

	path := req.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := fmt.Sprintf("http://127.0.0.1:%d%s", preview.port, path)
	if req.Query != "" {
		target += "?" + req.Query
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return http.StatusBadRequest, nil, fmt.Errorf("preview.fetch: %w", err)
	}
	for name, values := range req.Headers {
		for _, v := range values {
			httpReq.Header.Add(name, v)
		}
	}
	httpReq.Host = fmt.Sprintf("127.0.0.1:%d", preview.port)

	client := &http.Client{
		Timeout: previewFetchTimeout,
		// No redirect following: a 302 belongs to the visitor's browser, which
		// is the only party that knows what the relayed URL looks like from
		// outside. Following it here would resolve it against 127.0.0.1.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return http.StatusBadGateway, nil, fmt.Errorf("preview.fetch: %w", err)
	}
	defer resp.Body.Close()

	// LimitReader at cap+1 so a body exactly at the cap is not reported
	// truncated, and one byte over is.
	payload, err := io.ReadAll(io.LimitReader(resp.Body, previewResponseBodyLimit+1))
	if err != nil {
		return http.StatusBadGateway, nil, fmt.Errorf("preview.fetch: read response: %w", err)
	}
	truncated := false
	if len(payload) > previewResponseBodyLimit {
		payload = payload[:previewResponseBodyLimit]
		truncated = true
	}

	out, err := json.Marshal(protocol.PreviewFetchResponse{
		Status:    resp.StatusCode,
		Headers:   resp.Header,
		Body:      base64.StdEncoding.EncodeToString(payload),
		Truncated: truncated,
	})
	if err != nil {
		return http.StatusInternalServerError, nil, err
	}
	return http.StatusOK, out, nil
}

// previewLogWriter opens <envRoot>/logs/lifecycle-run.log, the same place
// runLifecycleScript writes its transcripts, so the three lifecycle scripts are
// read from one directory.
func previewLogWriter(envRoot string, log *slog.Logger) *os.File {
	if envRoot == "" {
		return nil
	}
	dir := envRoot + string(os.PathSeparator) + "logs"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Debug("run preview: could not create the log directory", "error", err)
		return nil
	}
	f, err := os.OpenFile(previewLogPath(envRoot), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		log.Debug("run preview: could not open the log file", "error", err)
		return nil
	}
	return f
}

func previewLogPath(envRoot string) string {
	return envRoot + string(os.PathSeparator) + "logs" + string(os.PathSeparator) + "lifecycle-run.log"
}

// previewLogTail is the last lifecycleOutputLimit bytes of the run script's
// output — the tail, for the same reason formatLifecycleOutput takes it: a dev
// server prints its banner first and its reason last.
func previewLogTail(envRoot string) string {
	data, err := os.ReadFile(previewLogPath(envRoot))
	if err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) > lifecycleOutputLimit {
		trimmed = "…" + trimmed[len(trimmed)-lifecycleOutputLimit:]
	}
	return trimmed
}
