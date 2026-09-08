package daemonws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Server→daemon RPC (F12). The direction the preview relay depends on: unlike
// the daemon→server transport there is no HTTP fallback, so every failure mode
// has to surface as a distinguishable error the proxy can turn into a status.

// serveServerRPC answers server:rpc_request frames on conn until it is closed,
// standing in for the daemon's own handler.
func serveServerRPC(t *testing.T, conn *websocket.Conn, answer func(protocol.RPCRequestPayload) protocol.RPCResponsePayload) {
	t.Helper()
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg protocol.Message
			if json.Unmarshal(raw, &msg) != nil || msg.Type != protocol.EventServerRPCRequest {
				continue
			}
			var req protocol.RPCRequestPayload
			if json.Unmarshal(msg.Payload, &req) != nil {
				continue
			}
			resp := answer(req)
			resp.RequestID = req.RequestID
			frame, _ := json.Marshal(protocol.Message{
				Type:    protocol.EventServerRPCResponse,
				Payload: mustMarshalRaw(resp),
			})
			if conn.WriteMessage(websocket.TextMessage, frame) != nil {
				return
			}
		}
	}()
}

func TestServerRPCRoundTripCorrelatesByRequestID(t *testing.T) {
	hub := NewHub()
	conn := dialRPCTestConn(t, hub, ClientIdentity{DaemonID: "d1", RuntimeIDs: []string{"rt-1"}})

	var seenIDs sync.Map
	serveServerRPC(t, conn, func(req protocol.RPCRequestPayload) protocol.RPCResponsePayload {
		seenIDs.Store(req.RequestID, req.Method)
		// Answer out of order so a transport that matched by arrival rather
		// than by id would hand one caller the other's body.
		time.Sleep(time.Duration(len(req.Body)) * time.Millisecond)
		return protocol.RPCResponsePayload{Status: http.StatusOK, Body: req.Body}
	})
	waitForRuntimeConn(t, hub, "rt-1")

	var wg sync.WaitGroup
	for i, body := range []string{`{"n":"aaaaaaaaaa"}`, `{"n":"b"}`} {
		wg.Add(1)
		go func(i int, body string) {
			defer wg.Done()
			status, resp, err := hub.CallRuntime(context.Background(), "rt-1", "preview.fetch",
				json.RawMessage(body), 3*time.Second)
			if err != nil {
				t.Errorf("call %d: %v", i, err)
				return
			}
			if status != http.StatusOK || string(resp) != body {
				t.Errorf("call %d: status=%d body=%s, want 200 and its OWN body %s", i, status, resp, body)
			}
		}(i, body)
	}
	wg.Wait()
}

func TestServerRPCTimesOutWhenTheDaemonNeverAnswers(t *testing.T) {
	hub := NewHub()
	conn := dialRPCTestConn(t, hub, ClientIdentity{DaemonID: "d1", RuntimeIDs: []string{"rt-silent"}})
	// Read and discard: a daemon that is connected but does not implement the
	// method looks exactly like this.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	waitForRuntimeConn(t, hub, "rt-silent")

	_, _, err := hub.CallRuntime(context.Background(), "rt-silent", "preview.fetch", nil, 150*time.Millisecond)
	if !errors.Is(err, ErrServerRPCTimeout) {
		t.Fatalf("err = %v, want ErrServerRPCTimeout so the proxy can answer 502 instead of hanging", err)
	}
}

func TestServerRPCWithoutAConnectionFailsFast(t *testing.T) {
	hub := NewHub()
	_, _, err := hub.CallRuntime(context.Background(), "rt-absent", "preview.fetch", nil, time.Second)
	if !errors.Is(err, ErrRuntimeNotConnected) {
		t.Fatalf("err = %v, want ErrRuntimeNotConnected", err)
	}
}

func TestServerRPCInFlightCapRefusesRatherThanQueues(t *testing.T) {
	hub := NewHub()
	conn := dialRPCTestConn(t, hub, ClientIdentity{DaemonID: "d1", RuntimeIDs: []string{"rt-busy"}})
	// Read and never answer: the callers stay in flight holding their slots.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	waitForRuntimeConn(t, hub, "rt-busy")
	c := hub.clientForRuntime("rt-busy")

	for i := 0; i < maxInFlightServerRPCPerClient; i++ {
		go hub.CallRuntime(context.Background(), "rt-busy", "preview.fetch", nil, 10*time.Second)
	}
	// Wait for the slots to be genuinely held before probing. A probe that
	// raced the holders would steal the slot it means to find occupied, and
	// then never observe the cap at all.
	deadline := time.Now().Add(3 * time.Second)
	for len(c.serverRPC.sem) < maxInFlightServerRPCPerClient {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d slots were taken", len(c.serverRPC.sem), maxInFlightServerRPCPerClient)
		}
		time.Sleep(5 * time.Millisecond)
	}

	_, _, err := hub.CallRuntime(context.Background(), "rt-busy", "preview.fetch", nil, 2*time.Second)
	if !errors.Is(err, ErrServerRPCBusy) {
		t.Fatalf("err = %v, want ErrServerRPCBusy — the cap must REFUSE, not queue behind eight requests "+
			"until the visitor's browser gives up", err)
	}
}

// A disconnect must release every waiter immediately. Otherwise a reviewer
// whose laptop closed mid-request waits out the full relay deadline for an
// answer that can never arrive.
func TestServerRPCDisconnectReleasesWaiters(t *testing.T) {
	hub := NewHub()
	conn := dialRPCTestConn(t, hub, ClientIdentity{DaemonID: "d1", RuntimeIDs: []string{"rt-drop"}})
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	waitForRuntimeConn(t, hub, "rt-drop")

	done := make(chan error, 1)
	go func() {
		_, _, err := hub.CallRuntime(context.Background(), "rt-drop", "preview.fetch", nil, 10*time.Second)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	conn.Close()

	select {
	case err := <-done:
		if !errors.Is(err, ErrRuntimeNotConnected) {
			t.Fatalf("err = %v, want ErrRuntimeNotConnected", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the caller was still waiting after the connection dropped")
	}
}

func TestRuntimeCapabilitiesReportsWhatTheDaemonAdvertised(t *testing.T) {
	hub := NewHub()
	dialRPCTestConn(t, hub, ClientIdentity{
		DaemonID:     "d1",
		RuntimeIDs:   []string{"rt-caps"},
		Capabilities: "rpc-v1," + protocol.DaemonCapabilityRunPreviewV1,
	})
	waitForRuntimeConn(t, hub, "rt-caps")

	if got := hub.RuntimeCapabilities("rt-caps"); got == "" {
		t.Fatal("capabilities are empty; the relay decision would always fall back to loopback")
	}
	if hub.RuntimeCapabilities("rt-nothing") != "" {
		t.Error("an unconnected runtime must report no capabilities")
	}
}

// waitForRuntimeConn blocks until the hub has registered a connection for
// runtimeID. Registration happens on the server goroutine after the upgrade, so
// a test that called immediately would race it.
func waitForRuntimeConn(t *testing.T, hub *Hub, runtimeID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.clientForRuntime(runtimeID) != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no connection registered for runtime %s", runtimeID)
}
