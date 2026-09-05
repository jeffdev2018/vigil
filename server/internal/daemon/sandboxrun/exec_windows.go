//go:build windows

package sandboxrun

import (
	"errors"
	"os"
	"os/exec"
)

// execProcess runs the confined process as a child (Windows has no exec) and
// exits with its status.
func execProcess(path string, argv []string, env []string) error {
	cmd := exec.Command(path, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Env = os.Stdin, os.Stdout, os.Stderr, env
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	return err
}
