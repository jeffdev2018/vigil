package execenv

import (
	"fmt"
	"os"
	"path/filepath"
)

// LockProviderAuthentication excludes other Multica processes using this OS
// account. Keep the file after releasing: unlinking it could split the lock.
// It does not prevent someone running the provider CLI outside Multica.
func LockProviderAuthentication(home, provider string) (func(), error) {
	if provider != "claude" && provider != "codex" {
		return nil, fmt.Errorf("unsupported authentication provider")
	}
	dir := filepath.Join(home, ".multica", "auth-locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	file, err := openLockFile(filepath.Join(dir, provider+".lock"))
	if err != nil {
		return nil, err
	}
	locked, err := lockFileExclusiveNonBlocking(file)
	if err != nil || !locked {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("another %s authentication is in progress on this machine; retry when it finishes", provider)
	}
	return func() { releaseLockFile(file) }, nil
}
