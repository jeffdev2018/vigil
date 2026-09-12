package main

// `multica issue followup` / `followups` / `followup-cancel` — the CLI half
// of "réveil programmé" (JEF-373). A follow-up is a deferred run of the
// issue's agent: it fires once, at a chosen instant, carrying a note that
// says what to do then. The same rows show on the issue, in the runs list
// (blocked on "deferred") and in `multica calendar agenda`.
//
// The server owns every rule (the [now+1min, now+30d] window, the 500-rune
// note, the per-agent and per-workspace daily budgets), so this file only
// resolves the issue and the agent by name and renders the answer. The one
// refusal worth catching is 429: a spent budget is a product answer the
// caller has to read, not a stack trace.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var issueFollowupCmd = &cobra.Command{
	Use:   "followup <issue-id>",
	Short: "Wake this issue's agent later, with a note",
	Long: "Schedule a deferred wake-up of the issue's agent.\n\n" +
		"--when takes an RFC 3339 instant (2026-09-11T09:00:00+02:00) or an offset\n" +
		"in minutes from now (+90). It must land between 1 minute and 30 days away.\n\n" +
		"--agent is required unless the issue is already assigned to an agent.\n\n" +
		"Refusals you may see:\n" +
		"  400  the instant is outside the window, or the issue has no agent\n" +
		"  429  the daily follow-up budget of this agent or workspace is spent",
	Args: exactArgs(1),
	RunE: runIssueFollowup,
}

var issueFollowupsCmd = &cobra.Command{
	Use:   "followups <issue-id>",
	Short: "List the follow-ups waiting to fire on an issue",
	Args:  exactArgs(1),
	RunE:  runIssueFollowups,
}

var issueFollowupCancelCmd = &cobra.Command{
	Use:   "followup-cancel <issue-id> <followup-id>",
	Short: "Cancel a follow-up before it fires",
	Long: "Cancel a scheduled follow-up. `multica issue followups <issue-id>` lists the ids.\n\n" +
		"A follow-up that already fired or was cancelled answers 409: there is\n" +
		"nothing left to take back.",
	Args: exactArgs(2),
	RunE: runIssueFollowupCancel,
}

func runIssueFollowup(cmd *cobra.Command, args []string) error {
	when, _ := cmd.Flags().GetString("when")
	if strings.TrimSpace(when) == "" {
		return fmt.Errorf("--when is required (RFC 3339, e.g. 2026-09-11T09:00:00+02:00, or +90 for minutes from now)")
	}
	note, _ := cmd.Flags().GetString("note")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{"when": when}
	if note != "" {
		body["note"] = note
	}
	if agent, _ := cmd.Flags().GetString("agent"); strings.TrimSpace(agent) != "" {
		agentID, err := resolveAgent(ctx, client, agent)
		if err != nil {
			return fmt.Errorf("resolve agent: %w", err)
		}
		body["agent_id"] = agentID
	}

	var resp struct {
		Followup map[string]any `json:"followup"`
	}
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/followups", body, &resp); err != nil {
		// A spent budget is the answer, not a fault: print what the server
		// said instead of burying it inside a wrapped transport error.
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("%s — cancel a pending follow-up, or raise the workspace's followups budget in settings", serverErrorMessage(httpErr))
		}
		return fmt.Errorf("schedule follow-up: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "Scheduled %s to wake %s on %s at %s.\n",
		strVal(resp.Followup, "id"),
		strVal(resp.Followup, "agent_name"),
		issueRef.Display,
		strVal(resp.Followup, "fires_at"),
	)
	if n := strVal(resp.Followup, "note"); n != "" {
		fmt.Fprintf(os.Stdout, "Note: %s\n", n)
	}
	return nil
}

func runIssueFollowups(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var resp struct {
		Followups []map[string]any `json:"followups"`
		Budget    struct {
			MaxPerAgentPerDay     int `json:"max_per_agent_per_day"`
			MaxPerWorkspacePerDay int `json:"max_per_workspace_per_day"`
		} `json:"budget"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/followups", &resp); err != nil {
		return fmt.Errorf("list follow-ups: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	if len(resp.Followups) == 0 {
		fmt.Fprintf(os.Stdout, "No follow-up waiting on %s.\n", issueRef.Display)
		return nil
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "FIRES_AT", "AGENT", "NOTE", "SCHEDULED_BY"}
	rows := make([][]string, 0, len(resp.Followups))
	for _, f := range resp.Followups {
		by := actors.actor(strVal(f, "scheduled_by_type"), strVal(f, "scheduled_by_id"))
		if by == "" {
			by = strVal(f, "scheduled_by_type")
		}
		rows = append(rows, []string{
			displayID(strVal(f, "id"), fullID),
			strVal(f, "fires_at"),
			strVal(f, "agent_name"),
			strVal(f, "note"),
			by,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	fmt.Fprintf(os.Stdout, "\nBudget: %d per agent per day, %d per workspace per day.\n",
		resp.Budget.MaxPerAgentPerDay, resp.Budget.MaxPerWorkspacePerDay)
	return nil
}

func runIssueFollowupCancel(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	if err := client.DeleteJSON(ctx, "/api/issues/"+issueRef.ID+"/followups/"+url.PathEscape(args[1])); err != nil {
		return fmt.Errorf("cancel follow-up: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Cancelled follow-up %s on %s.\n", args[1], issueRef.Display)
	return nil
}

// serverErrorMessage pulls the sentence out of a Multica error body
// (`{"error": "..."}`), falling back to the raw body for anything else.
func serverErrorMessage(err *cli.HTTPError) string {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(err.Body), &payload) == nil && payload.Error != "" {
		return payload.Error
	}
	return strings.TrimSpace(err.Body)
}

func init() {
	issueCmd.AddCommand(issueFollowupCmd)
	issueCmd.AddCommand(issueFollowupsCmd)
	issueCmd.AddCommand(issueFollowupCancelCmd)

	issueFollowupCmd.Flags().String("when", "", "When to fire: RFC 3339 (2026-09-11T09:00:00+02:00) or +minutes from now (+90)")
	issueFollowupCmd.Flags().String("note", "", "What the follow-up run should do (shown as its trigger, 500 characters max)")
	issueFollowupCmd.Flags().String("agent", "", "Agent name or ID to wake (required unless the issue is assigned to an agent)")
	issueFollowupCmd.Flags().String("output", "table", "Output format: table or json")

	issueFollowupsCmd.Flags().String("output", "table", "Output format: table or json")
	issueFollowupsCmd.Flags().Bool("full-id", false, "Show full UUIDs instead of short ids")
}
