package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The snapshot pipeline's onApplied callback returns nothing, so a read
// failure inside it is invisible unless it is logged: the auto-fix, the
// walkthrough, the stale flags and the realtime refresh are all skipped.
func TestPRSnapshotAppliedLogsReadFailures(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	broken := *testHandler
	broken.Queries = db.New(failQueryDBTX{DBTX: testPool, failOn: "-- name: GetGitHubPullRequestByID :one", err: errors.New("injected read failure")})
	broken.broadcastPRSnapshotApplied(context.Background(), parseUUID(uuid.NewString()))
	if !strings.Contains(logs.String(), "injected read failure") {
		t.Fatalf("a failed pull request read left no trace: %q", logs.String())
	}
}
