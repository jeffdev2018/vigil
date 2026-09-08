//go:build unix

package sandboxrun

import "syscall"

// execProcess replaces the shim with the confined process so the daemon's
// process-group signals and exit status apply to it directly.
func execProcess(path string, argv []string, env []string) error {
	return syscall.Exec(path, argv, env)
}
