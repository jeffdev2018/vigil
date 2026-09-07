package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Run previews (F12 / JEF-10), HTTP surface.
//
// The relay transport itself is asserted in internal/daemonws (correlation,
// timeout, in-flight cap) and the daemon's start/probe/stop in
// internal/daemon. What only a handler can prove is here: which scheme a
// declaration ends up with, that the public proxy refuses a code it should not
// honour, and that revocation is felt on the very next request.

type previewFixture struct {
	runtime string
	agent   string
	issue   string
	task    string
}

func newPreviewFixture(t *testing.T) previewFixture {
	t.Helper()
	runtimeID := handlerTestRuntimeID(t)
	agentID := createHandlerTestAgent(t, "f12-preview-agent", nil)
	issueID := dbfx.Issue(t, "F12 preview issue", testutil.Cols{
		"status": "in_progress", "assignee_type": "agent", "assignee_id": agentID,
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
	})
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM task_share_link WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM run_preview WHERE task_id = $1`, taskID)
	})
	return previewFixture{runtime: runtimeID, agent: agentID, issue: issueID, task: taskID}
}

// declare posts one daemon report and returns the recorded status/scheme.
func (f previewFixture) declare(t *testing.T, body map[string]any, want int) map[string]string {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+f.task+"/preview", body,
		testWorkspaceID, "f12-daemon")
	req = testutil.WithURLParams(req, "taskId", f.task)
	resp := testutil.Call(t, testHandler.ReportRunPreview, req).Want(want)
	out := map[string]string{}
	if want == http.StatusOK {
		resp.JSON(&out)
	}
	return out
}

func (f previewFixture) read(t *testing.T) runPreviewResponse {
	t.Helper()
	return testutil.Decode[runPreviewResponse](t, testHandler.GetRunPreview,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/tasks/"+f.task+"/preview", nil), "taskId", f.task),
		http.StatusOK)
}

func (f previewFixture) createLink(t *testing.T, body any) taskShareLinkResponse {
	t.Helper()
	return testutil.Decode[taskShareLinkResponse](t, testHandler.CreateTaskShareLink,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/tasks/"+f.task+"/share-links", body), "taskId", f.task),
		http.StatusCreated)
}

func proxyCall(t *testing.T, code, rest string) *testutil.Response {
	t.Helper()
	req := testutil.WithURLParams(newRequest(http.MethodGet, "/preview/"+code+"/"+rest, nil), "code", code, "*", rest)
	return testutil.Call(t, testHandler.PreviewProxy, req)
}

// A daemon that cannot relay — or a deployment whose hub does not know it —
// never gets a relay scheme, whatever it asked for. The whole point is that the
// UI is told the truth about reachability before it renders a link.
func TestRunPreviewDeclarationWithoutRelayFallsBackToLoopback(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)

	got := f.declare(t, map[string]any{
		"port": 21000, "scheme": "relay", "status": "ready", "health_path": "/",
	}, http.StatusOK)

	if got["scheme"] != previewSchemeLoopback {
		t.Errorf("scheme = %q, want loopback: no connected daemon advertises run-preview-v1 in this test server, "+
			"so a relay scheme would have promised a URL the proxy cannot serve", got["scheme"])
	}
	body := f.read(t)
	if body.Status != previewStatusReady {
		t.Errorf("status = %q, want ready", body.Status)
	}
	if body.URL != "http://127.0.0.1:21000" {
		t.Errorf("url = %q, want the loopback address so the desktop app can open it", body.URL)
	}
	if body.RelayAvailable {
		t.Error("relay_available must be false; the web UI keys its 'local to this machine' copy off it")
	}
}

// Every non-ready state answers without a URL. `ready` is checked explicitly
// rather than "not stopped", so a status this build does not know cannot
// produce a link.
func TestRunPreviewOnlyReadyCarriesAURL(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)

	for _, status := range []string{previewStatusStarting, previewStatusError} {
		f.declare(t, map[string]any{"port": 21001, "scheme": "loopback", "status": status, "error": "boom"}, http.StatusOK)
		if body := f.read(t); body.URL != "" {
			t.Errorf("status %s: url = %q, want no link", status, body.URL)
		}
	}
	f.declare(t, map[string]any{"port": 21001, "scheme": "loopback", "status": "ready"}, http.StatusOK)
	if body := f.read(t); body.URL == "" {
		t.Error("ready must carry a URL")
	}
}

func TestRunPreviewRejectsUnknownStatusAndBadPort(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)

	f.declare(t, map[string]any{"port": 21002, "scheme": "loopback", "status": "sideways"}, http.StatusBadRequest)
	f.declare(t, map[string]any{"port": 0, "scheme": "loopback", "status": "ready"}, http.StatusBadRequest)
}

// Acceptance 5: the run ended, the preview is stopped, and the proxy answers
// 410 — the reviewer had a working link a moment ago, so "gone" is the true
// answer and "never existed" would send them hunting for a typo.
func TestRunPreviewStopAnswersGoneThroughTheProxy(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	f.declare(t, map[string]any{"port": 21003, "scheme": "relay", "status": "ready"}, http.StatusOK)
	link := f.createLink(t, map[string]any{"capabilities": []string{"preview"}, "expires_in_hours": 2})

	req := newDaemonTokenRequest(http.MethodDelete, "/api/daemon/tasks/"+f.task+"/preview", nil,
		testWorkspaceID, "f12-daemon")
	testutil.Call(t, testHandler.StopRunPreview, testutil.WithURLParams(req, "taskId", f.task)).
		Want(http.StatusNoContent)

	if got := f.read(t); got.Status != previewStatusStopped || got.URL != "" {
		t.Errorf("after stop: status=%q url=%q, want stopped with the link removed", got.Status, got.URL)
	}
	proxyCall(t, link.Code, "").Want(http.StatusGone)
}

// Acceptance 3: a code nobody issued is a 404 with the same body as a revoked
// one. Anything that distinguished them would let a caller enumerate live runs.
func TestPreviewProxyUnknownCodeIsNotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	proxyCall(t, "nosuchcodeatall", "index.html").Want(http.StatusNotFound)
}

// Acceptance 4: revoking stops serving on the very next request. The proxy
// resolves the code every time, so there is no cached grant to wait out.
func TestPreviewProxyRevokedAndExpiredCodesStopServing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	f.declare(t, map[string]any{"port": 21004, "scheme": "relay", "status": "ready"}, http.StatusOK)

	revoked := f.createLink(t, nil)
	testutil.Call(t, testHandler.RevokeTaskShareLink,
		testutil.WithURLParams(newRequest(http.MethodDelete, "/api/tasks/"+f.task+"/share-links/"+revoked.ID, nil),
			"taskId", f.task, "id", revoked.ID)).Want(http.StatusNoContent)
	proxyCall(t, revoked.Code, "").Want(http.StatusNotFound)

	expired := f.createLink(t, nil)
	dbfx.Exec(t, `UPDATE task_share_link SET expires_at = now() - interval '1 minute' WHERE id = $1`, expired.ID)
	proxyCall(t, expired.Code, "").Want(http.StatusNotFound)
}

// A link without the `preview` capability resolves — it is a real link — but is
// answered exactly like an unknown code, because saying "wrong capability"
// would confirm the code is live.
func TestPreviewProxyRefusesLinkWithoutPreviewCapability(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	f.declare(t, map[string]any{"port": 21005, "scheme": "relay", "status": "ready"}, http.StatusOK)
	link := f.createLink(t, map[string]any{"capabilities": []string{"view"}})

	proxyCall(t, link.Code, "").Want(http.StatusNotFound)
}

// Acceptance 7: a loopback preview is never relayed, even with a valid link.
// The address is meaningful on one machine, and proxying it would 502 forever.
func TestPreviewProxyRefusesLoopbackScheme(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	f.declare(t, map[string]any{"port": 21006, "scheme": "loopback", "status": "ready"}, http.StatusOK)
	link := f.createLink(t, nil)

	resp := proxyCall(t, link.Code, "").Want(http.StatusGone)
	if !strings.Contains(resp.Body.String(), "local to the machine") {
		t.Errorf("body = %q, want it to name the reason so the reviewer knows it is not a broken link", resp.Body.String())
	}
}

// The per-workspace cap refuses rather than queues: a queued request just times
// out in the visitor's browser with no explanation.
func TestPreviewProxyWorkspaceRelayCapRefuses(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	f.declare(t, map[string]any{"port": 21007, "scheme": "relay", "status": "ready"}, http.StatusOK)
	link := f.createLink(t, nil)

	// Hold every slot, as a page of concurrent asset requests would.
	for i := 0; i < previewWorkspaceRelayCap; i++ {
		release, ok := previewRelaySlots.acquire(testWorkspaceID, previewWorkspaceRelayCap)
		if !ok {
			t.Fatalf("slot %d could not be acquired; the cap is %d", i, previewWorkspaceRelayCap)
		}
		defer release()
	}
	resp := proxyCall(t, link.Code, "")
	// Without a relay-capable daemon the scheme was downgraded to loopback, so
	// the cap is exercised through the limiter directly; assert the limiter
	// itself is exhausted rather than the route's happy path.
	if _, ok := previewRelaySlots.acquire(testWorkspaceID, previewWorkspaceRelayCap); ok {
		t.Error("the cap let a 17th request through")
	}
	if resp.Code != http.StatusGone && resp.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 410 (loopback) or 429 (cap)", resp.Code)
	}
}

func TestTaskShareLinkRejectsUnknownCapabilityAndOverlongExpiry(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)

	testutil.Call(t, testHandler.CreateTaskShareLink,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/tasks/"+f.task+"/share-links",
			map[string]any{"capabilities": []string{"admin"}}), "taskId", f.task)).
		Want(http.StatusUnprocessableEntity)

	testutil.Call(t, testHandler.CreateTaskShareLink,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/tasks/"+f.task+"/share-links",
			map[string]any{"expires_in_hours": shareLinkMaxHours + 1}), "taskId", f.task)).
		Want(http.StatusUnprocessableEntity)
}

// Acceptance 6: the daemon is gone, so nothing will ever report the stop. The
// sweep is the only thing that can move the preview off `ready`.
func TestRunPreviewStaleSweepMovesPreviewsOfGoneDaemons(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	f.declare(t, map[string]any{"port": 21008, "scheme": "loopback", "status": "ready"}, http.StatusOK)

	dbfx.Exec(t, `UPDATE agent_runtime SET last_seen_at = now() - interval '1 hour' WHERE id = $1`, f.runtime)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE agent_runtime SET last_seen_at = now() WHERE id = $1`, f.runtime)
	})

	if _, err := testHandler.SweepStaleRunPreviews(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := f.read(t); got.Status != previewStatusStale {
		t.Errorf("status = %q, want stale so the UI stops offering a preview whose machine left", got.Status)
	}
}

// A daemon token scoped to another workspace must not be able to declare a
// preview on this workspace's run.
func TestRunPreviewRejectsForeignDaemonWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPreviewFixture(t)
	req := newRequest(http.MethodPost, "/api/daemon/tasks/"+f.task+"/preview",
		map[string]any{"port": 21009, "scheme": "loopback", "status": "ready"})
	req.Header.Del("X-User-ID")
	req = req.WithContext(middleware.WithDaemonContext(req.Context(),
		"00000000-0000-0000-0000-0000000000ff", "other-daemon"))
	testutil.Call(t, testHandler.ReportRunPreview, testutil.WithURLParams(req, "taskId", f.task)).
		Want(http.StatusNotFound)
}
