package handler

import (
	"context"
	"testing"
)

func TestRedisCliAuthStoreLifecycleAndTerminalIdempotence(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	store := NewRedisCliAuthStore(rdb)

	req, err := store.Create(ctx, "runtime-1", "login")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.PopPending(ctx, "runtime-1")
	if err != nil || claimed == nil || claimed.Status != CliAuthRunning {
		t.Fatalf("claim = %+v, err=%v", claimed, err)
	}
	if err := store.Progress(ctx, req.ID, "https://auth.example/device", "ABCD-EFGH"); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(ctx, req.ID, true); err != nil {
		t.Fatal(err)
	}
	// A late failure cannot overwrite the successful terminal result.
	if err := store.Fail(ctx, req.ID, "late report"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != CliAuthCompleted || got.Authenticated == nil || !*got.Authenticated {
		t.Fatalf("terminal request = %+v", got)
	}
	if got.VerificationURL != "https://auth.example/device" || got.UserCode != "ABCD-EFGH" {
		t.Fatalf("device-code progress lost: %+v", got)
	}
}
