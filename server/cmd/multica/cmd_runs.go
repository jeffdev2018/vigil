package main

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Fleet page (OS plan, chantier 4) from the terminal: every run of the
// workspace, and a cancel that takes several ids. The kill switch stays a
// human affordance in the app; it is deliberately not a command.

var runsCmd = &cobra.Command{
	Use:   "runs",
	Short: "See and stop the workspace's runs",
}

var runsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the workspace's runs, newest first",
	Long: "List every run of the workspace — queued, running, waiting on somebody, over — " +
		"with the agent, the issue, what each is blocked on and what it cost.\n\n" +
		"Defaults to runs in flight (--active). Use --terminal for finished ones, " +
		"--all for both, --agent or --issue to narrow, --cursor to page.",
	Args: exactArgs(0),
	RunE: runRunsList,
}

var runsCancelCmd = &cobra.Command{
	Use:   "cancel <run-id> [run-id...]",
	Short: "Cancel one or more runs",
	Long: "Cancel runs by id. Each id is reported on its own line: cancelled, already_over " +
		"or not_found. A run that is already over is not an error.",
	Args: cobra.MinimumNArgs(1),
	RunE: runRunsCancel,
}

func init() {
	runsListCmd.Flags().Bool("active", false, "runs in flight only (default)")
	runsListCmd.Flags().Bool("terminal", false, "finished runs only")
	runsListCmd.Flags().Bool("all", false, "every run")
	runsListCmd.Flags().String("agent", "", "agent id")
	runsListCmd.Flags().String("issue", "", "issue id")
	runsListCmd.Flags().String("cursor", "", "page cursor from a previous read")
	runsListCmd.Flags().Int("limit", 50, "rows per page (max 200)")
	runsListCmd.Flags().Bool("full-id", false, "print full run ids")
	runsListCmd.Flags().String("output", "table", "Output format: table or json")
	runsCancelCmd.Flags().String("output", "table", "Output format: table or json")
	runsCmd.AddCommand(runsListCmd)
	runsCmd.AddCommand(runsCancelCmd)
}

func runRunsList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	state := "active"
	if v, _ := cmd.Flags().GetBool("terminal"); v {
		state = "terminal"
	}
	if v, _ := cmd.Flags().GetBool("all"); v {
		state = "all"
	}
	params := []string{"state=" + state}
	if v, _ := cmd.Flags().GetString("agent"); v != "" {
		params = append(params, "agent_id="+v)
	}
	if v, _ := cmd.Flags().GetString("issue"); v != "" {
		issueRef, err := resolveIssueRef(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve issue: %w", err)
		}
		params = append(params, "issue_id="+issueRef.ID)
	}
	if v, _ := cmd.Flags().GetString("cursor"); v != "" {
		params = append(params, "cursor="+v)
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params = append(params, fmt.Sprintf("limit=%d", v))
	}
	var resp struct {
		Runs       []map[string]any `json:"runs"`
		NextCursor string           `json:"next_cursor"`
		Summary    map[string]any   `json:"summary"`
	}
	if err := client.GetJSON(ctx, "/api/runs?"+strings.Join(params, "&"), &resp); err != nil {
		return fmt.Errorf("list runs: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "AGENT", "ISSUE", "STATUS", "BLOCKED ON", "STARTED", "COST"}
	rows := make([][]string, 0, len(resp.Runs))
	for _, r := range resp.Runs {
		started := strVal(r, "started_at")
		if len(started) >= 16 {
			started = started[:16]
		}
		blocked := ""
		if b, ok := r["blocked_on"].(map[string]any); ok {
			blocked = strVal(b, "kind")
			if s := strVal(b, "summary"); s != "" {
				if utf8.RuneCountInString(s) > 40 {
					s = string([]rune(s)[:37]) + "..."
				}
				blocked += " · " + s
			}
		}
		cost := ""
		if ticks, ok := r["cost_usd_ticks"].(float64); ok && ticks > 0 {
			cost = fmt.Sprintf("$%.4f", ticks/1e10)
		}
		rows = append(rows, []string{
			displayID(strVal(r, "id"), fullID),
			strVal(r, "agent_name"),
			strVal(r, "issue_identifier"),
			strVal(r, "status"),
			blocked,
			started,
			cost,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	if halt, ok := resp.Summary["run_halt"].(map[string]any); ok && halt["halted"] == true {
		fmt.Fprintf(os.Stderr, "warning: the fleet is halted: %s\n", strVal(halt, "reason"))
	}
	if resp.NextCursor != "" {
		fmt.Fprintf(os.Stderr, "more: --cursor %s\n", resp.NextCursor)
	}
	return nil
}

func runRunsCancel(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var resp struct {
		Cancelled int              `json:"cancelled"`
		Results   []map[string]any `json:"results"`
	}
	if err := client.PostJSON(ctx, "/api/runs/cancel", map[string]any{"task_ids": args}, &resp); err != nil {
		return fmt.Errorf("cancel runs: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	for _, r := range resp.Results {
		line := strVal(r, "task_id") + "\t" + strVal(r, "outcome")
		if e := strVal(r, "error"); e != "" {
			line += "\t" + e
		}
		fmt.Fprintln(os.Stdout, line)
	}
	fmt.Fprintf(os.Stdout, "cancelled %d of %d\n", resp.Cancelled, len(args))
	return nil
}
