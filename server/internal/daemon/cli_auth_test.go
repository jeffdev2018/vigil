package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestCliAuthCommandSupportsClaudeAndCodex(t *testing.T) {
	tests := map[string][]string{
		"codex:login":   {"login", "--device-auth"},
		"codex:logout":  {"logout"},
		"claude:login":  {"auth", "login"},
		"claude:logout": {"auth", "logout"},
	}
	for key, want := range tests {
		provider, action, _ := strings.Cut(key, ":")
		got, err := cliAuthCommand(provider, action)
		if err != nil || strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("cliAuthCommand(%s) = %v, %v", key, got, err)
		}
	}
	if _, err := cliAuthCommand("other", "login"); err == nil {
		t.Fatal("unsupported provider should fail")
	}
}

func TestHandleCliAuthReportsDeviceCodeAndNeverReportsOrLogsSecret(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses a POSIX script")
	}
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz123456"
	bin := filepath.Join(t.TempDir(), "fake-codex")
	script := "#!/bin/sh\nprintf '%s\\n' 'Open https://auth.openai.com/codex/device' 'Code: ABCD-EFGH' '" + secret + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var reports []map[string]any
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var report map[string]any
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("decode report: %v", err)
		}
		mu.Lock()
		reports = append(reports, report)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	var logs bytes.Buffer
	d.logger = slog.New(slog.NewTextHandler(&logs, nil))
	d.cfg.Agents = map[string]AgentEntry{"codex": {Path: bin}}

	d.handleCliAuth(context.Background(), Runtime{ID: "rt-1", Provider: "codex"}, PendingCliAuth{ID: "req-1", Action: "login"})

	mu.Lock()
	defer mu.Unlock()
	if len(reports) < 2 {
		t.Fatalf("reports = %#v, want progress and completion", reports)
	}
	if reports[0]["verification_url"] != "https://auth.openai.com/codex/device" || reports[0]["user_code"] != "ABCD-EFGH" {
		t.Fatalf("progress report = %#v", reports[0])
	}
	last := reports[len(reports)-1]
	if last["status"] != "completed" || last["authenticated"] != true {
		t.Fatalf("terminal report = %#v", last)
	}
	wire, _ := json.Marshal(reports)
	if strings.Contains(string(wire), secret) || strings.Contains(logs.String(), secret) {
		t.Fatal("CLI output secret crossed a report or logger boundary")
	}
}

func TestCliAuthOutputWriterHandlesSplitOutput(t *testing.T) {
	var gotURL, gotCode string
	w := &cliAuthOutputWriter{onUpdate: func(url, code string) {
		gotURL, gotCode = url, code
	}}
	_, _ = w.Write([]byte("Open https://auth.example/de"))
	_, _ = w.Write([]byte("vice\nEnter code: abcd-"))
	_, _ = w.Write([]byte("efgh\n"))
	if gotURL != "https://auth.example/device" || gotCode != "ABCD-EFGH" {
		t.Fatalf("parsed url=%q code=%q", gotURL, gotCode)
	}
}

func TestCliAuthRejectsBusyAccountBeforeResolvingOrExecutingProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var reports []map[string]any
	d, _ := localSkillReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var report map[string]any
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Error(err)
		}
		reports = append(reports, report)
		w.WriteHeader(http.StatusOK)
	})
	// There is deliberately no executable configured: the busy guard must
	// run before executable discovery, including on duplicate deliveries.
	rt := Runtime{ID: "rt-1", Provider: "codex"}
	d.cliAuthActive.Store("codex", "first")
	d.handleCliAuth(context.Background(), rt, PendingCliAuth{ID: "first", Action: "login"})
	if len(reports) != 0 {
		t.Fatal("duplicate delivery failed the active request")
	}
	d.handleCliAuth(context.Background(), rt, PendingCliAuth{ID: "second", Action: "logout"})
	if len(reports) != 1 || reports[0]["status"] != "failed" || !strings.Contains(reports[0]["error"].(string), "in progress") {
		t.Fatalf("competing request not rejected: %+v", reports)
	}
	d.cliAuthActive.Delete("codex")
	release, err := execenv.LockProviderAuthentication(home, "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	d.handleCliAuth(context.Background(), rt, PendingCliAuth{ID: "third", Action: "logout"})
	if len(reports) != 2 || !strings.Contains(reports[1]["error"].(string), "in progress") {
		t.Fatalf("independent lock ignored: %+v", reports)
	}
}
