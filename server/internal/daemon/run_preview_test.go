package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Run previews on the daemon side (F12).
//
// Everything executed here is written by the test: default tests never resolve
// or run a user-installed CLI, and the `run` script is user configuration, so
// the fake is the whole point rather than a shortcut.

// freeTestPort reserves and releases a port, which is what the run script then
// binds. A race is possible in principle and irrelevant in practice: nothing
// else in this suite binds.
func freeTestPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// previewServerScript writes a shell script that serves one fixed body on
// MULTICA_PORT_BASE forever, using nothing but `nc`-free shell built-ins is not
// possible, so it shells out to a tiny Go-free listener: python3 is present on
// every machine this suite runs on (macOS + the Linux CI image).
func previewServerScript(t *testing.T, body string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to stand up a fake dev server")
	}
	script := fmt.Sprintf(`exec python3 -c '
import http.server, os, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        payload = %q.encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Set-Cookie", "session=leak")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", int(os.environ["MULTICA_PORT_BASE"])), H).serve_forever()
'`, body)
	return fakeScript(t, "run.sh", script)
}

// previewTestDaemon is a Daemon wired to a fake server that records the
// preview reports instead of making HTTP calls.
type previewReport struct {
	Report  RunPreviewReport
	Stopped bool
}

func previewTestDaemon(t *testing.T) (*Daemon, *[]previewReport) {
	t.Helper()
	reports := &[]previewReport{}
	srv := newRecordingPreviewServer(t, reports)
	d := &Daemon{
		client: NewClient(srv.URL),
		logger: lifecycleTestLogger(),
	}
	return d, reports
}

func newRecordingPreviewServer(t *testing.T, reports *[]previewReport) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/preview") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodDelete {
			*reports = append(*reports, previewReport{Stopped: true})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var report RunPreviewReport
		json.NewDecoder(r.Body).Decode(&report)
		*reports = append(*reports, previewReport{Report: report})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// previewProbeBudgetForTest shortens the probe window. The product waits 90 s
// for a first compile, which is not a thing to sit through in a unit test.
func previewProbeBudgetForTest(t *testing.T, budget time.Duration) func() {
	t.Helper()
	previous := previewProbeTimeout
	previewProbeTimeout = budget
	return func() { previewProbeTimeout = previous }
}

// Acceptance 1: a worktree run with a `run` script gets a ready preview on the
// port block F09 handed it.
func TestPreviewStartProbeAndStop(t *testing.T) {
	port := freeTestPort(t)
	script := previewServerScript(t, "hello from the worktree")
	d, reports := previewTestDaemon(t)
	envRoot := t.TempDir()

	task := Task{ID: "11111111-1111-1111-1111-111111111111"}
	env := map[string]string{"MULTICA_PORT_BASE": strconv.Itoa(port), "MULTICA_PORT_COUNT": "10"}
	<-d.startRunPreview(context.Background(), task, []string{script}, t.TempDir(), envRoot, env, lifecycleTestLogger())

	if len(*reports) == 0 {
		t.Fatal("no preview was reported")
	}
	got := (*reports)[0].Report
	if got.Status != "ready" {
		t.Fatalf("status = %q (error %q), want ready", got.Status, got.Error)
	}
	if got.Port != port {
		t.Errorf("port = %d, want the run's port block base %d", got.Port, port)
	}
	if got.Scheme != "relay" {
		t.Errorf("scheme = %q, want relay: this daemon advertises run-preview-v1", got.Scheme)
	}

	// The relay reads the port from the daemon's own registry, never from the
	// wire, so it must be findable by task id while the preview is live.
	if _, ok := d.previews.get(task.ID); !ok {
		t.Fatal("the live preview is not in the registry; a relayed request could not find its port")
	}

	d.stopRunPreview(task.ID, lifecycleTestLogger())
	if _, ok := d.previews.get(task.ID); ok {
		t.Error("the preview is still registered after the stop")
	}
	if !(*reports)[len(*reports)-1].Stopped {
		t.Error("the stop was never reported; every open browser tab would keep its link")
	}
	// Acceptance 5: the process group is gone, so the port is free again.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if taskPortFree(port) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("port %d is still bound after the stop; the dev server outlived its run", port)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A script that never binds anything ends as an `error` preview carrying the
// tail of its log — the only thing that tells the user what to fix. It must not
// fail the run.
func TestPreviewScriptThatNeverAnswersReportsError(t *testing.T) {
	script := fakeScript(t, "run.sh", `echo "ERR_MODULE_NOT_FOUND: vite" >&2; sleep 30`)
	d, reports := previewTestDaemon(t)
	envRoot := t.TempDir()
	port := freeTestPort(t)

	// A short probe budget: the product waits 90 s for a first compile, which
	// is not a thing to sit through in a unit test.
	restore := previewProbeBudgetForTest(t, 400*time.Millisecond)
	defer restore()

	task := Task{ID: "22222222-2222-2222-2222-222222222222"}
	<-d.startRunPreview(context.Background(), task, []string{script}, t.TempDir(), envRoot,
		map[string]string{"MULTICA_PORT_BASE": strconv.Itoa(port)}, lifecycleTestLogger())
	defer d.stopRunPreview(task.ID, lifecycleTestLogger())

	if len(*reports) == 0 {
		t.Fatal("no preview was reported")
	}
	got := (*reports)[0].Report
	if got.Status != "error" {
		t.Fatalf("status = %q, want error", got.Status)
	}
	if !strings.Contains(got.Error, "ERR_MODULE_NOT_FOUND") {
		t.Errorf("error = %q, want the tail of the run script's log so the user knows what failed", got.Error)
	}
}

// A run with no `run` script declares nothing at all. Most runs are this one.
func TestPreviewWithoutARunScriptDeclaresNothing(t *testing.T) {
	d, reports := previewTestDaemon(t)
	<-d.startRunPreview(context.Background(), Task{ID: "33333333-3333-3333-3333-333333333333"},
		nil, t.TempDir(), t.TempDir(), nil, lifecycleTestLogger())
	if len(*reports) != 0 {
		t.Fatalf("reports = %+v, want none", *reports)
	}
}

// Acceptance 2: the relay serves the local server's own response.
func TestPreviewFetchRelaysTheLocalResponse(t *testing.T) {
	port := freeTestPort(t)
	script := previewServerScript(t, "relayed body")
	d, _ := previewTestDaemon(t)

	task := Task{ID: "44444444-4444-4444-4444-444444444444"}
	<-d.startRunPreview(context.Background(), task, []string{script}, t.TempDir(), t.TempDir(),
		map[string]string{"MULTICA_PORT_BASE": strconv.Itoa(port)}, lifecycleTestLogger())
	defer d.stopRunPreview(task.ID, lifecycleTestLogger())

	raw, _ := json.Marshal(protocol.PreviewFetchRequest{TaskID: task.ID, Method: http.MethodGet, Path: "/index.html"})
	status, body, err := d.dispatchServerRPC(context.Background(), "preview.fetch", raw)
	if err != nil {
		t.Fatalf("preview.fetch: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("rpc status = %d, want 200", status)
	}
	var fetched protocol.PreviewFetchResponse
	if err := json.Unmarshal(body, &fetched); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(fetched.Body)
	if err != nil {
		t.Fatalf("body is not base64: %v", err)
	}
	if string(decoded) != "relayed body" {
		t.Errorf("body = %q, want the dev server's own response", decoded)
	}
	if fetched.Status != http.StatusOK {
		t.Errorf("upstream status = %d, want 200", fetched.Status)
	}
	// The daemon hands back everything it got; filtering is the proxy's job and
	// is asserted there. What matters here is that the header actually travels,
	// so the proxy has something to filter.
	if len(fetched.Headers) == 0 {
		t.Error("no headers came back")
	}
}

// The port comes from the daemon's registry, never the wire: a task with no
// live preview cannot be used to reach an arbitrary local service.
func TestPreviewFetchRefusesATaskWithNoLivePreview(t *testing.T) {
	d, _ := previewTestDaemon(t)
	raw, _ := json.Marshal(protocol.PreviewFetchRequest{
		TaskID: "55555555-5555-5555-5555-555555555555", Method: http.MethodGet, Path: "/",
	})
	status, _, err := d.dispatchServerRPC(context.Background(), "preview.fetch", raw)
	if err == nil {
		t.Fatal("a task with no preview must not be fetched")
	}
	if status != http.StatusGone {
		t.Errorf("status = %d, want 410", status)
	}
}

func TestPreviewUnknownServerRPCMethodIs404(t *testing.T) {
	d, _ := previewTestDaemon(t)
	status, _, err := d.dispatchServerRPC(context.Background(), "definitely.not.a.method", nil)
	if err == nil || status != http.StatusNotFound {
		t.Fatalf("status=%d err=%v, want 404 and an error", status, err)
	}
}

// Only a worktree run gets a run script, for the same reason setup and archive
// are worktree-only: binding a port in the user's own directory is a side
// effect on a checkout they did not hand over.
func TestRunScriptOnlyAppliesToWorktreeMode(t *testing.T) {
	lc := &localDirectoryLifecycle{Run: []string{"/bin/true"}}
	inPlace := &localDirectoryAssignment{Ref: localDirectoryRef{ExecutionMode: localDirectoryModeInPlace, Lifecycle: lc}}
	if got := inPlace.RunScript(); got != nil {
		t.Fatalf("in_place RunScript() = %v, want nil", got)
	}
	worktree := &localDirectoryAssignment{Ref: localDirectoryRef{ExecutionMode: localDirectoryModeWorktree, Lifecycle: lc}}
	if got := worktree.RunScript(); len(got) != 1 {
		t.Fatalf("worktree RunScript() = %v, want the configured argv", got)
	}
	var absent *localDirectoryAssignment
	if got := absent.RunScript(); got != nil {
		t.Fatalf("nil assignment RunScript() = %v, want nil", got)
	}
}

func TestPreviewLogTailIsBounded(t *testing.T) {
	envRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(envRoot, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previewLogPath(envRoot), []byte(strings.Repeat("x", lifecycleOutputLimit*3)), 0o644); err != nil {
		t.Fatal(err)
	}
	tail := previewLogTail(envRoot)
	if len(tail) > lifecycleOutputLimit+4 {
		t.Fatalf("tail is %d bytes, want it bounded near %d", len(tail), lifecycleOutputLimit)
	}
}

// runTask defers stopRunPreview right after starting the preview, and several
// preparation failures return immediately after. The stop must always find the
// preview: a registration that lands after it would leave the run script
// running, and its port bound, for the life of the daemon.
func TestPreviewStopRightAfterStartKillsTheScript(t *testing.T) {
	workDir := t.TempDir()
	script := fakeScript(t, "run.sh", `i=0; while :; do i=$((i+1)); echo $i > "$PWD/beat"; sleep 0.05; done`)
	d, _ := previewTestDaemon(t)

	for i := 0; i < 20; i++ {
		task := Task{ID: fmt.Sprintf("66666666-6666-6666-6666-%012d", i)}
		_ = os.Remove(filepath.Join(workDir, "beat"))
		d.startRunPreview(context.Background(), task, []string{script}, workDir, t.TempDir(),
			map[string]string{"MULTICA_PORT_BASE": strconv.Itoa(freeTestPort(t))}, lifecycleTestLogger())
		d.stopRunPreview(task.ID, lifecycleTestLogger())

		before, _ := os.ReadFile(filepath.Join(workDir, "beat"))
		time.Sleep(300 * time.Millisecond)
		after, _ := os.ReadFile(filepath.Join(workDir, "beat"))
		if string(before) != string(after) {
			t.Fatalf("iteration %d: the run script is still running after the stop (beat %q -> %q)", i, before, after)
		}
	}
}
