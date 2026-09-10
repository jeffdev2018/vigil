package execenv

import (
	"strings"
	"testing"
)

func TestProviderAuthLockExcludesIndependentHandlesAndReleases(t *testing.T) {
	home := t.TempDir()
	release, err := LockProviderAuthentication(home, "codex")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if release != nil {
			release()
		}
	})
	if other, err := LockProviderAuthentication(home, "codex"); err == nil {
		other()
		t.Fatal("same account acquired twice")
	}
	claude, err := LockProviderAuthentication(home, "claude")
	if err != nil {
		t.Fatal(err)
	}
	claude()
	release()
	release = nil
	next, err := LockProviderAuthentication(home, "codex")
	if err != nil {
		t.Fatal(err)
	}
	next()
	// A provider name reaches this from a runtime row, so anything that would
	// leave the lock directory, or collide two providers on one file, has to be
	// refused rather than sanitised into something that silently locks the
	// wrong thing.
	for _, bad := range []string{"../escape", "", "a/b", "claude codex", strings.Repeat("x", 65)} {
		if other, err := LockProviderAuthentication(home, bad); err == nil {
			other()
			t.Fatalf("invalid provider accepted: %q", bad)
		}
	}
	// Any provider the daemon can authenticate must lock, not just the two that
	// existed when this was written: an unlocked provider looks exactly like a
	// locked one until two sign-ins overlap.
	copilot, err := LockProviderAuthentication(home, "copilot")
	if err != nil {
		t.Fatalf("a supported provider must lock: %v", err)
	}
	if other, err := LockProviderAuthentication(home, "COPILOT"); err == nil {
		other()
		t.Fatal("case variants must share one lock file")
	}
	copilot()
}
