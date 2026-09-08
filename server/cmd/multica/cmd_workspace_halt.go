package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Fleet halt (K05). Everything else in the product stops one thing: a run, an
// action, a budget. These two stop all of them, which is what someone needs
// when an agent is doing something wrong and they do not yet know which agent
// or why. A halt holds the queue rather than emptying it — the work resumes
// when it lifts.
var workspaceHaltCmd = &cobra.Command{
	Use:   "halt",
	Short: "Hold every agent in this workspace (owner/admin only)",
	Long: "Stops new runs from being dispatched and refuses the gated actions " +
		"of the runs already in flight — pushes, sensitive tool calls and " +
		"spending. Queued work stays queued and resumes when the halt lifts.\n\n" +
		"A run already in flight keeps the tools it was given at claim time; " +
		"those cannot be taken back mid-run. What it loses is every action " +
		"that asks the server first, which is every action the product treats " +
		"as consequential. Confinement is the sandbox's job, not this one.",
	Args: cobra.NoArgs,
	RunE: runWorkspaceHalt,
}

var workspaceResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Lift the hold on this workspace's agents (owner/admin only)",
	Args:  cobra.NoArgs,
	RunE:  runWorkspaceResume,
}

type runHaltPayload struct {
	Halted   bool   `json:"halted"`
	Reason   string `json:"reason,omitempty"`
	HaltedBy string `json:"halted_by,omitempty"`
	HaltedAt string `json:"halted_at,omitempty"`
}

func runWorkspaceHalt(cmd *cobra.Command, _ []string) error {
	reason, _ := cmd.Flags().GetString("reason")
	return putRunHalt(cmd, runHaltPayload{Halted: true, Reason: reason})
}

func runWorkspaceResume(cmd *cobra.Command, _ []string) error {
	return putRunHalt(cmd, runHaltPayload{Halted: false})
}

func putRunHalt(cmd *cobra.Command, body runHaltPayload) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var out runHaltPayload
	if err := client.PutJSON(ctx, "/api/run-halt", body, &out); err != nil {
		return fmt.Errorf("set the workspace halt: %w", err)
	}
	if out.Halted {
		fmt.Fprintln(cmd.OutOrStdout(), "Halted. No new run is dispatched, and a run in flight is refused every action that asks first.")
		if out.Reason != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Reason: %s\n", out.Reason)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Queued work is held, not lost. Lift it with `multica workspace resume`.")
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Resumed. Queued work is dispatched again.")
	return nil
}

func init() {
	workspaceHaltCmd.Flags().String("reason", "", "why the agents are held; shown wherever the halt refuses something")
	workspaceCmd.AddCommand(workspaceHaltCmd)
	workspaceCmd.AddCommand(workspaceResumeCmd)
}
