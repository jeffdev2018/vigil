package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica fleet {status,cost,history} — the read side of JEF-12. The three
// endpoints are small, stable aggregations over the dashboard queries, served
// so an agent can answer "who is running / what did X cost / the history"
// without scraping six dashboard endpoints. The default output is JSON, and
// that is the stable contract: these commands feed an LLM skill, so the JSON
// shape mirrors the server's exactly. `--output table` exists for a human at
// a terminal, following the `issue get` convention (json default, table on
// request), and is not part of that contract.

var fleetCmd = &cobra.Command{
	Use:   "fleet",
	Short: "Read the workspace fleet: who is running, what it costs, the history",
}

var fleetStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Who is running now, plus per-agent task counts over the window",
	Long: "Per-agent live + windowed workload: running task count now, and terminal task\n" +
		"counts (failed included) over the window. ?since defaults to 30 days server-side.",
	Args: exactArgs(0),
	RunE: runFleetStatus,
}

var fleetCostCmd = &cobra.Command{
	Use:   "cost",
	Short: "Per-agent cost in USD over the window",
	Long: "Provider-reported spend per agent over the window: cost_usd_ticks (1 USD = 1e10\n" +
		"ticks), input/output tokens and task count, summed across models. ?since defaults\n" +
		"to 30 days server-side.",
	Args: exactArgs(0),
	RunE: runFleetCost,
}

var fleetHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Daily per-agent activity buckets over the window",
	Long: "One row per (UTC date, agent) with terminal task counts anchored on\n" +
		"completed_at; days with no completions produce no row. ?since defaults to\n" +
		"30 days server-side.",
	Args: exactArgs(0),
	RunE: runFleetHistory,
}

func init() {
	for _, sub := range []*cobra.Command{fleetStatusCmd, fleetCostCmd, fleetHistoryCmd} {
		sub.Flags().String("since", "", "Window start: RFC3339 or YYYY-MM-DD (default: 30 days ago)")
		sub.Flags().String("agent-id", "", "Narrow to one agent UUID")
		sub.Flags().String("output", "json", "Output format: json or table")
	}
	fleetCmd.AddCommand(fleetStatusCmd, fleetCostCmd, fleetHistoryCmd)
}

// fleetQuery builds the shared ?since=&agent_id= query string.
func fleetQuery(cmd *cobra.Command) string {
	q := url.Values{}
	if v, _ := cmd.Flags().GetString("since"); v != "" {
		q.Set("since", v)
	}
	if v, _ := cmd.Flags().GetString("agent-id"); v != "" {
		q.Set("agent_id", v)
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// fleetStatusRow mirrors handler.FleetStatusRow.
type fleetStatusRow struct {
	AgentID          string `json:"agent_id"`
	Name             string `json:"name,omitempty"`
	RunningTaskCount int32  `json:"running_task_count"`
	TaskCount        int32  `json:"task_count"`
	FailedCount      int32  `json:"failed_count"`
}

// fleetCostRow mirrors handler.FleetCostRow.
type fleetCostRow struct {
	AgentID      string `json:"agent_id"`
	CostUSDTicks int64  `json:"cost_usd_ticks"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TaskCount    int32  `json:"task_count"`
}

// fleetHistoryRow mirrors handler.FleetHistoryRow.
type fleetHistoryRow struct {
	Date        string `json:"date"`
	AgentID     string `json:"agent_id"`
	TaskCount   int32  `json:"task_count"`
	FailedCount int32  `json:"failed_count"`
}

func runFleetStatus(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var rows []fleetStatusRow
	if err := client.GetJSON(ctx, "/api/fleet/status"+fleetQuery(cmd), &rows); err != nil {
		return fmt.Errorf("fleet status: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output != "table" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{
			r.AgentID, r.Name,
			strconv.Itoa(int(r.RunningTaskCount)),
			strconv.Itoa(int(r.TaskCount)),
			strconv.Itoa(int(r.FailedCount)),
		})
	}
	cli.PrintTable(os.Stdout, []string{"AGENT", "NAME", "RUNNING", "TASKS", "FAILED"}, table)
	return nil
}

func runFleetCost(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var rows []fleetCostRow
	if err := client.GetJSON(ctx, "/api/fleet/cost"+fleetQuery(cmd), &rows); err != nil {
		return fmt.Errorf("fleet cost: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output != "table" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		cost := ""
		if r.CostUSDTicks > 0 {
			cost = fmt.Sprintf("$%.4f", float64(r.CostUSDTicks)/1e10)
		}
		table = append(table, []string{
			r.AgentID, cost,
			strconv.FormatInt(r.InputTokens, 10),
			strconv.FormatInt(r.OutputTokens, 10),
			strconv.Itoa(int(r.TaskCount)),
		})
	}
	cli.PrintTable(os.Stdout, []string{"AGENT", "COST", "INPUT TOKENS", "OUTPUT TOKENS", "TASKS"}, table)
	return nil
}

func runFleetHistory(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var rows []fleetHistoryRow
	if err := client.GetJSON(ctx, "/api/fleet/history"+fleetQuery(cmd), &rows); err != nil {
		return fmt.Errorf("fleet history: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output != "table" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{
			r.Date, r.AgentID,
			strconv.Itoa(int(r.TaskCount)),
			strconv.Itoa(int(r.FailedCount)),
		})
	}
	cli.PrintTable(os.Stdout, []string{"DATE", "AGENT", "TASKS", "FAILED"}, table)
	return nil
}
