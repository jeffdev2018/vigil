package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The public preview proxy end to end (F12), against a fake daemon that answers
// the reverse RPC over a real WebSocket.
//
// The transport itself is covered in internal/daemonws; what is asserted here
// is everything the proxy adds on top: which headers cross in each direction,
// the frame headers it sets itself, and what a visitor sees when the daemon
// does not answer.

// fakeDaemonConn connects one WS client to hub as runtimeID and answers every
// server:rpc_request with answer(req). A nil answer never replies, which is how
// the timeout path is exercised.
func fakeDaemonConn(t *testing.T, hub *daemonws.Hub, runtimeID, capabilities string,
	answer func(protocol.PreviewFetchRequest) protocol.PreviewFetchResponse) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, daemonws.ClientIdentity{
			DaemonID:     "f12-fake-daemon",
			RuntimeIDs:   []string{runtimeID},
			Capabilities: capabilities,
		})
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial the fake daemon: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

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
			var rpcReq protocol.RPCRequestPayload
			if json.Unmarshal(msg.Payload, &rpcReq) != nil {
				continue
			}
			if answer == nil {
				continue
			}
			var fetch protocol.PreviewFetchRequest
			json.Unmarshal(rpcReq.Body, &fetch)
			body, _ := json.Marshal(answer(fetch))
			frame, _ := json.Marshal(protocol.Message{
				Type: protocol.EventServerRPCResponse,
				Payload: mustJSON(protocol.RPCResponsePayload{
					RequestID: rpcReq.RequestID, Status: http.StatusOK, Body: body,
				}),
			})
			if conn.WriteMessage(websocket.TextMessage, frame) != nil {
				return
			}
		}
	}()
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// useDaemonHub attaches hub to the test handler for the duration of one test.
func useDaemonHub(t *testing.T, hub *daemonws.Hub) {
	t.Helper()
	previous := testHandler.DaemonHub
	testHandler.DaemonHub = hub
	t.Cleanup(func() { testHandler.DaemonHub = previous })
}

// waitForRelay blocks until the handler agrees the runtime can relay, i.e. the
// hub has registered the fake connection.
func waitForRelay(t *testing.T, runtimeID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if testHandler.relayAvailableForRuntime(runtimeID) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the hub never registered a relay-capable connection for runtime %s", runtimeID)
}

// Acceptance 2: the relayed URL serves the local server's response, with the
// header set filtered in both directions and the frame headers set by us.
func TestPreviewProxyRelaysWithFilteredHeaders(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	hub := daemonws.NewHub()
	useDaemonHub(t, hub)

	var seen protocol.PreviewFetchRequest
	fakeDaemonConn(t, hub, f.runtime, protocol.DaemonCapabilityRunPreviewV1,
		func(req protocol.PreviewFetchRequest) protocol.PreviewFetchResponse {
			seen = req
			return protocol.PreviewFetchResponse{
				Status: http.StatusOK,
				Headers: map[string][]string{
					"Content-Type": {"text/html"},
					"Etag":         {`W/"abc"`},
					// Must NOT reach the visitor: a dev server's session cookie
					// set on the API origin is both useless and dangerous.
					"Set-Cookie": {"session=leak"},
					// Transport framing belongs to this hop, not the relayed one.
					"Transfer-Encoding": {"chunked"},
				},
				Body:      base64.StdEncoding.EncodeToString([]byte("<h1>worktree</h1>")),
				Truncated: true,
			}
		})
	waitForRelay(t, f.runtime)

	f.declare(t, map[string]any{"port": 21100, "scheme": "relay", "status": "ready"}, http.StatusOK)
	link := f.createLink(t, nil)

	req := testutil.WithURLParams(newRequest(http.MethodGet, "/preview/"+link.Code+"/app.html?q=1", nil),
		"code", link.Code, "*", "app.html")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Cookie", "multica_session=secret")
	req.Header.Set("Authorization", "Bearer nope")
	req.URL.RawQuery = "q=1"
	resp := testutil.Call(t, testHandler.PreviewProxy, req).Want(http.StatusOK)

	if got := resp.Body.String(); got != "<h1>worktree</h1>" {
		t.Errorf("body = %q, want the dev server's own response", got)
	}
	if seen.Path != "/app.html" || seen.Query != "q=1" {
		t.Errorf("daemon saw path=%q query=%q, want /app.html and q=1", seen.Path, seen.Query)
	}
	for _, banned := range []string{"Cookie", "Authorization"} {
		if _, present := seen.Headers[banned]; present {
			t.Errorf("%s reached the dev server; the share code is the only credential in this flow", banned)
		}
	}
	if _, present := seen.Headers["Accept"]; !present {
		t.Error("Accept did not reach the dev server, so content negotiation is broken through the relay")
	}
	if got := resp.Header().Get("Set-Cookie"); got != "" {
		t.Errorf("Set-Cookie = %q, want it dropped: the API origin must not carry the preview app's session", got)
	}
	if got := resp.Header().Get("Transfer-Encoding"); got != "" {
		t.Errorf("Transfer-Encoding = %q, want it dropped: framing belongs to this hop", got)
	}
	if got := resp.Header().Get("Etag"); got != `W/"abc"` {
		t.Errorf("Etag = %q, want it forwarded so conditional requests still work", got)
	}
	if got := resp.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
		t.Errorf("CSP = %q, want the proxy's own value, not the app's", got)
	}
	if got := resp.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := resp.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store: a shared origin must not serve one reviewer's build to the next", got)
	}
	if resp.Header().Get("X-Multica-Preview-Truncated") != "1" {
		t.Error("a truncated body must say so; otherwise a capped bundle looks like a corrupt one")
	}
}

// The relayed URL is what the read endpoint hands out once a link exists.
func TestRunPreviewRelaySchemeProducesAShareURL(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	hub := daemonws.NewHub()
	useDaemonHub(t, hub)
	fakeDaemonConn(t, hub, f.runtime, protocol.DaemonCapabilityRunPreviewV1, nil)
	waitForRelay(t, f.runtime)

	got := f.declare(t, map[string]any{"port": 21101, "scheme": "relay", "status": "ready"}, http.StatusOK)
	if got["scheme"] != previewSchemeRelay {
		t.Fatalf("scheme = %q, want relay", got["scheme"])
	}
	if body := f.read(t); body.URL != "" {
		t.Errorf("url = %q, want none before a link exists — the member has not shared it yet", body.URL)
	}
	link := f.createLink(t, nil)
	body := f.read(t)
	if !strings.Contains(body.URL, link.Code) {
		t.Errorf("url = %q, want the share link's code", body.URL)
	}
	if body.ExpiresAt == nil {
		t.Error("a relayed URL must carry its expiry; a link with no visible end is one nobody revokes")
	}
	if !body.RelayAvailable {
		t.Error("relay_available must be true when a relay-capable daemon is connected")
	}
}

// A daemon that never answers must produce a 502, not a hung request.
func TestPreviewProxyDaemonSilenceIs502(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	hub := daemonws.NewHub()
	useDaemonHub(t, hub)
	fakeDaemonConn(t, hub, f.runtime, protocol.DaemonCapabilityRunPreviewV1, nil)
	waitForRelay(t, f.runtime)

	f.declare(t, map[string]any{"port": 21102, "scheme": "relay", "status": "ready"}, http.StatusOK)
	link := f.createLink(t, nil)

	// A request context that expires stands in for the 30 s relay deadline
	// without making the test wait it out.
	req := testutil.WithURLParams(newRequest(http.MethodGet, "/preview/"+link.Code+"/", nil), "code", link.Code, "*", "")
	ctx, cancel := context.WithTimeout(req.Context(), 200*time.Millisecond)
	defer cancel()
	testutil.Call(t, testHandler.PreviewProxy, req.WithContext(ctx)).Want(http.StatusBadGateway)
}

// previewTargetPath and filterHeaders are the two pure pieces of the proxy;
// their matrix lives here rather than being re-run through the route above.
func TestPreviewProxyPathAndHeaderHelpers(t *testing.T) {
	for _, tc := range []struct{ rest, want string }{
		{"", "/"},
		{"index.html", "/index.html"},
		{"/assets/app.js", "/assets/app.js"},
		{"a/b/c", "/a/b/c"},
	} {
		req := testutil.WithURLParams(httptest.NewRequest(http.MethodGet, "/preview/x/", nil), "*", tc.rest)
		if got := previewTargetPath(req); got != tc.want {
			t.Errorf("previewTargetPath(%q) = %q, want %q", tc.rest, got, tc.want)
		}
	}

	in := http.Header{
		"Accept":          {"text/html"},
		"Cookie":          {"a=b"},
		"Authorization":   {"Bearer x"},
		"Range":           {"bytes=0-1"},
		"X-Forwarded-For": {"1.2.3.4"},
	}
	out := filterHeaders(in, previewRequestHeaderAllowList)
	if _, ok := out["Accept"]; !ok {
		t.Error("Accept must pass")
	}
	if _, ok := out["Range"]; !ok {
		t.Error("Range must pass, or video and large assets break through the relay")
	}
	for _, banned := range []string{"Cookie", "Authorization", "X-Forwarded-For"} {
		if _, ok := out[banned]; ok {
			t.Errorf("%s must not pass", banned)
		}
	}
	if filterHeaders(http.Header{"Cookie": {"a=b"}}, previewRequestHeaderAllowList) != nil {
		t.Error("a header set with nothing allowed must come back nil, not an empty map")
	}
}
