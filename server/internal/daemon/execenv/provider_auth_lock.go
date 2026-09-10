package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// providerLockName keeps the provider usable as a single filename segment.
// The provider reaches this package from a runtime row, so a value like
// "../escape" would otherwise place the lock outside the directory and, worse,
// let two different providers collide on one file. An allowlist of the
// providers that exist today would be the other way to close that hole, but it
// silently stops locking every provider added afterwards — which is the failure
// nobody notices, because a missing lock looks exactly like a working one until
// two sign-ins overlap.
func providerLockName(provider string) (string, bool) {
	if provider == "" || len(provider) > 64 {
		return "", false
	}
	for _, r := range provider {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return "", false
		}
	}
	return strings.ToLower(provider), true
}

// LockProviderAuthentication excludes other Multica processes using this OS
// account. Keep the file after releasing: unlinking it could split the lock.
// It does not prevent someone running the provider CLI outside Multica.
func LockProviderAuthentication(home, provider string) (func(), error) {
	name, ok := providerLockName(provider)
	if !ok {
		return nil, fmt.Errorf("unsupported authentication provider")
	}
	dir := filepath.Join(home, ".multica", "auth-locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	file, err := openLockFile(filepath.Join(dir, name+".lock"))
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
