package sandboxrun

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// Run is the shim entry point: read the spec, build the confined argv from
// the inherited cwd and environment, then replace this process with it (or,
// where exec is unavailable, run it and exit with its code).
func Run(specPath string, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("sandbox-run: missing command after --")
	}
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("sandbox-run: read spec: %w", err)
	}
	var spec Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return fmt.Errorf("sandbox-run: parse spec: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("sandbox-run: getwd: %w", err)
	}
	argv0, args, env, err := Argv(spec, cwd, os.Environ(), argv[0], argv[1:])
	if err != nil {
		return err
	}
	resolved, err := exec.LookPath(argv0)
	if err != nil {
		return fmt.Errorf("sandbox-run: mode %s needs %s on PATH: %w", spec.Mode, argv0, err)
	}
	return execProcess(resolved, append([]string{argv0}, args...), env)
}
