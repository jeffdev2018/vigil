package daemonws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Server→daemon RPC (F12) — the mirror image of the daemon→server transport in
// hub.go.
//
// It exists for one reason: the preview relay has nowhere else to go. The
// daemon runs on a laptop behind NAT with no inbound port and no tunnel binary,
// so the only path from a browser to the dev server in a run's worktree is the
// control connection the daemon already holds open. Everything here is that
// path: correlate a request to its answer, bound how many can be in flight, and
// give up on a deadline rather than holding an HTTP request open forever.
//
// The daemon→server direction falls back to HTTP when this transport fails.
// This direction has no fallback — an unreachable daemon means no preview — so
// every failure mode is an error the caller must render, never a silent retry.

var (
	// ErrRuntimeNotConnected is returned when no live WS connection serves the
	// runtime. The caller answers 502: the preview exists, the machine holding
	// it does not answer.
	ErrRuntimeNotConnected = errors.New("daemonws: runtime has no live connection")
	// ErrServerRPCBusy is returned when the connection already has the maximum
	// number of server-initiated RPCs in flight.
	ErrServerRPCBusy = errors.New("daemonws: too many in-flight server rpc requests")
	// ErrServerRPCTimeout is returned when the daemon did not answer in time.
	ErrServerRPCTimeout = errors.New("daemonws: daemon did not answer in time")
)

// maxInFlightServerRPCPerClient bounds concurrent server→daemon RPCs on one
// connection, the same argument as maxInFlightRPCPerClient in the other
// direction: one socket must not become an unbounded queue. A preview page
// pulls a handful of assets at once, so eight is a page's worth without letting
// one reviewer monopolise the daemon.
const maxInFlightServerRPCPerClient = 8

// serverRPCReadLimit is the daemon→server read limit. It has to fit a
// preview.fetch RESPONSE: a body capped at previewBodyLimit, base64-encoded
// (4/3), plus headers and JSON envelope. Sized from the cap rather than written
// as a number so raising one cannot silently truncate the other.
const serverRPCReadLimit int64 = 12 << 20

// serverRPCPending is the reply slot for one outstanding request.
type serverRPCPending struct {
	mu      sync.Mutex
	pending map[string]chan protocol.RPCResponsePayload
	sem     chan struct{}
}

func newServerRPCPending() *serverRPCPending {
	return &serverRPCPending{
		pending: make(map[string]chan protocol.RPCResponsePayload),
		sem:     make(chan struct{}, maxInFlightServerRPCPerClient),
	}
}

// deliver routes an inbound server:rpc_response to its waiting call. Under the
// mutex so it is serialised with failAll's close+delete: a channel still in the
// map is guaranteed not yet closed.
func (p *serverRPCPending) deliver(resp protocol.RPCResponsePayload) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch, ok := p.pending[resp.RequestID]
	if !ok {
		return
	}
	select {
	case ch <- resp:
	default:
	}
}

// failAll releases every waiter when the connection tears down, so a caller
// blocked on a dead socket returns immediately instead of waiting out its
// deadline.
func (p *serverRPCPending) failAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
}

// CallRuntime issues one RPC to whichever connection currently serves
// runtimeID, and waits for the answer.
//
// Returns an HTTP-style status plus the response body. A non-nil error means
// the request never produced an answer (no connection, saturated, timeout);
// there is no partial success to interpret.
func (h *Hub) CallRuntime(ctx context.Context, runtimeID, method string, body json.RawMessage, timeout time.Duration) (int, json.RawMessage, error) {
	if h == nil {
		return 0, nil, ErrRuntimeNotConnected
	}
	c := h.clientForRuntime(runtimeID)
	if c == nil {
		return 0, nil, ErrRuntimeNotConnected
	}
	return c.callServerRPC(ctx, method, body, timeout)
}

// RuntimeCapabilities returns the X-Client-Capabilities string the connection
// serving runtimeID advertised at connect, or "" when nothing serves it. The
// preview declaration reads it to decide relay vs loopback before it writes a
// scheme it cannot honour.
func (h *Hub) RuntimeCapabilities(runtimeID string) string {
	if h == nil {
		return ""
	}
	c := h.clientForRuntime(runtimeID)
	if c == nil {
		return ""
	}
	return c.identity.Capabilities
}

// clientForRuntime picks one live connection for the runtime. Several can be
// registered during a reconnect overlap; any of them reaches the same machine,
// so the first is as good as a choice as there is.
func (h *Hub) clientForRuntime(runtimeID string) *client {
	if runtimeID == "" {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.byRuntime[runtimeID] {
		return c
	}
	return nil
}

func (c *client) callServerRPC(ctx context.Context, method string, body json.RawMessage, timeout time.Duration) (int, json.RawMessage, error) {
	select {
	case c.serverRPC.sem <- struct{}{}:
		defer func() { <-c.serverRPC.sem }()
	default:
		return 0, nil, ErrServerRPCBusy
	}

	id := uuid.NewString()
	frame, err := json.Marshal(protocol.Message{
		Type: protocol.EventServerRPCRequest,
		Payload: mustMarshalRaw(protocol.RPCRequestPayload{
			RequestID: id,
			Method:    method,
			Body:      body,
			TimeoutMs: timeout.Milliseconds(),
		}),
	})
	if err != nil {
		return 0, nil, fmt.Errorf("daemonws: marshal server rpc frame: %w", err)
	}

	ch := make(chan protocol.RPCResponsePayload, 1)
	c.serverRPC.mu.Lock()
	c.serverRPC.pending[id] = ch
	c.serverRPC.mu.Unlock()
	defer func() {
		c.serverRPC.mu.Lock()
		delete(c.serverRPC.pending, id)
		c.serverRPC.mu.Unlock()
	}()

	if !c.trySend(frame) {
		return 0, nil, ErrRuntimeNotConnected
	}

	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp, ok := <-ch:
		if !ok {
			return 0, nil, ErrRuntimeNotConnected
		}
		if resp.Status >= 200 && resp.Status < 300 {
			return resp.Status, resp.Body, nil
		}
		msg := resp.Error
		if msg == "" {
			msg = fmt.Sprintf("daemon rpc status %d", resp.Status)
		}
		status := resp.Status
		if status == 0 {
			status = http.StatusBadGateway
		}
		return status, nil, errors.New(msg)
	case <-timer.C:
		return 0, nil, ErrServerRPCTimeout
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

// handleServerRPCResponseFrame routes the daemon's answer back to the waiting
// CallRuntime.
func (c *client) handleServerRPCResponseFrame(raw json.RawMessage) {
	var resp protocol.RPCResponsePayload
	if err := json.Unmarshal(raw, &resp); err != nil {
		slog.Debug("daemon websocket server rpc response invalid payload", "error", err, "daemon_id", c.identity.DaemonID)
		return
	}
	if resp.RequestID == "" {
		return
	}
	c.serverRPC.deliver(resp)
}
