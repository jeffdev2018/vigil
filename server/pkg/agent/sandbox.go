package agent

// SandboxLaunch (K10) asks the launch boundary to run the runtime CLI through
// the daemon's confinement shim instead of directly on the host.
//
// The shim is the multica binary itself (`multica sandbox-run`): it reads the
// spec file, wraps the original command in `docker run` or `bwrap` according
// to the spec's mode, and execs it. Wrapping happens at argv level so the
// cmd.Dir and cmd.Env every backend assigns after construction still apply:
// the shim inherits both and reads the working directory from its own cwd.
type SandboxLaunch struct {
	// Mode is the effective confinement mode: "container" or "sandbox". The
	// daemon never sets a SandboxLaunch for "none".
	Mode string
	// Image is the container image for mode container (informational here;
	// the shim reads it from SpecPath).
	Image string
	// AllowedHosts are the extra egress hosts the daemon's proxy allows.
	AllowedHosts []string
	// TaskID is the run this launch belongs to.
	TaskID string
	// SpecPath is the JSON spec the shim reads (written by the daemon under
	// the task temp dir before launch).
	SpecPath string
	// ShimPath is the multica binary that implements `sandbox-run`.
	ShimPath string
}

// sandboxRunSubcommand is the hidden multica subcommand implementing the shim.
const sandboxRunSubcommand = "sandbox-run"

// wrapSandboxArgv rewrites an argv[0]+args pair to run through the shim.
// A nil launch returns its inputs unchanged.
func wrapSandboxArgv(sb *SandboxLaunch, argv0 string, args []string) (string, []string) {
	if sb == nil || sb.ShimPath == "" {
		return argv0, args
	}
	wrapped := make([]string, 0, len(args)+5)
	wrapped = append(wrapped, sandboxRunSubcommand, "--spec", sb.SpecPath, "--", argv0)
	wrapped = append(wrapped, args...)
	return sb.ShimPath, wrapped
}
