package main

import (
	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/daemon/sandboxrun"
)

// sandboxRunCmd is the K10 confinement shim the daemon puts in front of every
// runtime process of a sandboxed run: `multica sandbox-run --spec <file> --
// <cmd> <args…>`. It inherits the daemon-assigned cwd and environment, wraps
// the command per the spec's mode and replaces itself with the result.
var sandboxRunCmd = &cobra.Command{
	Use:                "sandbox-run --spec <file> -- <command> [args...]",
	Short:              "Run a command inside the daemon's sandbox (internal)",
	Hidden:             true,
	DisableFlagParsing: false,
	Args:               cobra.MinimumNArgs(1),
	SilenceUsage:       true,
	RunE: func(cmd *cobra.Command, args []string) error {
		spec, _ := cmd.Flags().GetString("spec")
		return sandboxrun.Run(spec, args)
	},
}

func init() {
	sandboxRunCmd.Flags().String("spec", "", "path to the sandbox spec JSON written by the daemon")
	_ = sandboxRunCmd.MarkFlagRequired("spec")
	// Everything after `--` belongs to the wrapped command; cobra stops flag
	// parsing there, so a CLI flag like `-p` is never mistaken for ours.
	sandboxRunCmd.Flags().SetInterspersed(false)
}
