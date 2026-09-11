package handler

import (
	"sync"
	"testing"
	"time"
)

// A panic in background work must be swallowed by the recover, not crash
// the test binary (which stands in for the server process here).
func TestGoBackgroundRecoversFromPanic(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	goBackground("test job", func() {
		defer wg.Done()
		panic("boom")
	})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background job did not finish")
	}
}
