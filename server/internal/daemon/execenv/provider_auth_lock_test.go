package execenv

import (
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
	if other, err := LockProviderAuthentication(home, "../escape"); err == nil {
		other()
		t.Fatal("invalid provider accepted")
	}
}
