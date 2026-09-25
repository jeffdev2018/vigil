package handler

import (
	"context"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// TestModelKeyActiveUniqueIndexRejectsConcurrentInserts covers the audit
// finding that createModelKey's "one active key per vendor/scope" invariant
// was enforced only by a read-then-write check in Go (ListModelKeys, then
// compare in memory), with no DB constraint behind it — so two concurrent
// creates could both pass the check before either committed. This drives two
// real concurrent INSERTs straight through h.Queries.CreateModelKey
// (bypassing the app-level pre-check entirely, to force the actual race
// window) and asserts idx_workspace_model_key_active_unique (migration 918)
// lets exactly one through.
func TestModelKeyActiveUniqueIndexRejectsConcurrentInserts(t *testing.T) {
	ctx := context.Background()
	workspaceID := testWorkspaceID
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace_model_key WHERE workspace_id = $1 AND provider = 'race-test-vendor'`, workspaceID)
	})

	newParams := func() db.CreateModelKeyParams {
		return db.CreateModelKeyParams{
			ID: dbid.NewV7(), WorkspaceID: wsUUID, Scope: "workspace", Provider: "race-test-vendor",
			Label: "race", KeyEncrypted: "sealed", KeyHint: "hint", Priority: 0, CreatedBy: parseUUID(testUserID),
		}
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, 2)
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := testHandler.Queries.CreateModelKey(ctx, newParams())
			results[i] = err
		}()
	}
	close(start)
	wg.Wait()

	var successes, conflicts int
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case isUniqueViolation(err):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent creates = %d successes, %d unique-violation conflicts; want exactly 1 and 1 (the unique index must reject the loser)", successes, conflicts)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM workspace_model_key WHERE workspace_id = $1 AND provider = 'race-test-vendor' AND active`, workspaceID); n != 1 {
		t.Fatalf("active rows for the vendor = %d, want 1", n)
	}
}
