package handler

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
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

func TestCliAuthStoresKeepTerminalResultsUnderConcurrentReports(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store CliAuthStore
	}{
		{"memory", NewInMemoryCliAuthStore()}, {"redis", NewRedisCliAuthStore(newRedisTestClient(t))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			req, err := tc.store.Create(ctx, "runtime-concurrent", "login")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tc.store.PopPending(ctx, req.RuntimeID); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < 6; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					var err error
					if i%2 == 0 {
						err = tc.store.Complete(ctx, req.ID, true)
					} else {
						err = tc.store.Progress(ctx, req.ID, "https://auth.example/device", "ABCD-EFGH")
					}
					if err != nil {
						t.Errorf("report: %v", err)
					}
				}(i)
			}
			close(start)
			wg.Wait()
			got, err := tc.store.Get(ctx, req.ID)
			if err != nil || got.Status != CliAuthCompleted || got.Authenticated == nil || !*got.Authenticated {
				t.Fatalf("lost completion: %+v %v", got, err)
			}
			if err := tc.store.Fail(ctx, req.ID, "late failure"); err != nil {
				t.Fatal(err)
			}
			final, _ := tc.store.Get(ctx, req.ID)
			if final.Status != CliAuthCompleted {
				t.Fatalf("terminal result changed: %+v", final)
			}
		})
	}
}

func TestMemoryCliAuthStoreReturnsIndependentSnapshotsAndHonorsExpiry(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryCliAuthStore()
	req, _ := store.Create(ctx, "runtime", "login")
	id := req.ID
	req.Status = CliAuthCompleted
	claimed, _ := store.PopPending(ctx, "runtime")
	if claimed == nil || claimed.Status != CliAuthRunning {
		t.Fatal("create exposed store data")
	}
	claimed.Status = CliAuthFailed
	if err := store.Complete(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(ctx, id)
	*got.Authenticated = false
	got.Status = CliAuthFailed
	again, _ := store.Get(ctx, id)
	if again.Status != CliAuthCompleted || !*again.Authenticated {
		t.Fatal("get exposed store data")
	}
	expired, _ := store.Create(ctx, "runtime", "logout")
	store.mu.Lock()
	store.requests[expired.ID].ExpiresAt = time.Now().Add(-time.Second)
	store.mu.Unlock()
	if err := store.Complete(ctx, expired.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Get(ctx, expired.ID)
	if got.Status != CliAuthTimeout {
		t.Fatalf("expired request completed: %+v", got)
	}
}

func TestRedisCliAuthClaimCannotResurrectTerminalRequest(t *testing.T) {
	ctx := context.Background()
	rdb := newRedisTestClient(t)
	store := NewRedisCliAuthStore(rdb)
	req, _ := store.Create(ctx, "runtime", "login")
	stale := *req
	stale.Status = CliAuthRunning
	data, err := json.Marshal(&stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Fail(ctx, req.ID, "failed before claim"); err != nil {
		t.Fatal(err)
	}
	claimed, err := claimCliAuthScript.Run(ctx, rdb, []string{cliAuthPendingKey(req.RuntimeID), cliAuthKey(req.ID)}, req.ID, data, 60).Int64()
	if err != nil || claimed != 0 {
		t.Fatalf("stale claim: %d %v", claimed, err)
	}
	got, _ := store.Get(ctx, req.ID)
	if got.Status != CliAuthFailed {
		t.Fatalf("resurrected request: %+v", got)
	}
}
