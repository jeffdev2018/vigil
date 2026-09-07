package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/memoryeval"
	"github.com/spf13/cobra"
)

type memoryRuntimeFixture = daemon.MemoryRuntimeFixture

func runMemoryRuntime(ctx context.Context, workDir string, fixture memoryRuntimeFixture, memories []string) (memoryeval.RuntimeOutput, error) {
	return daemon.RunMemoryRuntime(ctx, workDir, fixture, memories)
}

var memoryRuntimeWorkerCmd = &cobra.Command{
	Use: "runtime-worker <runtime.json>", Short: "Container entry point for offline evaluation through Multica's Claude adapter", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Accidental host invocation must fail before executable lookup or account access.
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if _, err := os.Stat("/.dockerenv"); err != nil || cwd != "/work" || os.Getuid() != 65534 {
			return errors.New("runtime-worker must run inside the evaluation container")
		}
		var fixture memoryRuntimeFixture
		if err := memoryeval.ReadJSON(args[0], &fixture); err != nil {
			return err
		}
		var memories []string
		if err := memoryeval.ReadJSON("/memory.json", &memories); err != nil {
			return err
		}
		result, err := runMemoryRuntime(cmd.Context(), cwd, fixture, memories)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	},
}

func init() { agentMemoryCmd.AddCommand(memoryRuntimeWorkerCmd) }
