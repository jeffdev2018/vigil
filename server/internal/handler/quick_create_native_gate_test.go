package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
)

// The quick-create CLI version gate exists because an older DAEMON cannot be
// trusted with the flow. The native runtime has no daemon and no CLI: it runs
// the flow in this binary. Version-checking it produced
// daemon_version_unsupported with an empty current_version — the gate
// answering a question nobody asked of it, and the reason a native agent could
// not be quick-created at all.
func TestQuickCreateVersionGateExemptsTheNativeRuntime(t *testing.T) {
	ctx := context.Background()

	// A CLI runtime that advertises no version is still refused: that is the
	// case the gate is for, and the exemption must not widen to it.
	cli := dbfx.Runtime(t, "gate cli "+uuid.NewString()[:6], testutil.Cols{"provider": "claude"})
	status, body := testHandler.checkQuickCreateDaemonVersionAtLeast(ctx, "test", parseUUID(cli), agentpkg.MinQuickCreateCLIVersion)
	if status == 0 {
		t.Fatal("a CLI runtime with no advertised version must still be refused")
	}
	if body["code"] != "daemon_version_unsupported" {
		t.Fatalf("refusal code = %v, want daemon_version_unsupported", body["code"])
	}

	native := dbfx.Runtime(t, "gate native "+uuid.NewString()[:6], testutil.Cols{
		"provider": "native", "runtime_mode": "native", "daemon_id": "native",
	})
	if status, body := testHandler.checkQuickCreateDaemonVersionAtLeast(ctx, "test", parseUUID(native), agentpkg.MinQuickCreateCLIVersion); status != 0 {
		t.Fatalf("the native runtime was refused %d %v — it has no daemon to be out of date", status, body)
	}
}
