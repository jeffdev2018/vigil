package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var autopilotCmd = &cobra.Command{
	Use:   "autopilot",
	Short: "Manage autopilots (scheduled/triggered agent automations)",
}

var autopilotListCmd = &cobra.Command{
	Use:   "list",
	Short: "List autopilots in the workspace",
	RunE:  runAutopilotList,
}

var autopilotGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get autopilot details (webhook credentials redacted by default)",
	Args:  exactArgs(1),
	RunE:  runAutopilotGet,
}

var autopilotCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new autopilot",
	RunE:  runAutopilotCreate,
}

var autopilotUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotUpdate,
}

var autopilotDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotDelete,
}

var autopilotTriggerCmd = &cobra.Command{
	Use:   "trigger <id>",
	Short: "Manually trigger an autopilot to run once",
	Args:  exactArgs(1),
	RunE:  runAutopilotTrigger,
}

var autopilotRunsCmd = &cobra.Command{
	Use:   "runs <id>",
	Short: "List execution history for an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotRuns,
}

var autopilotTriggerAddCmd = &cobra.Command{
	Use:   "trigger-add <autopilot-id>",
	Short: "Add a schedule or webhook trigger to an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotTriggerAdd,
}

var autopilotTriggerListCmd = &cobra.Command{
	Use:   "trigger-list <autopilot-id>",
	Short: "List an autopilot's triggers (ids for trigger-update/-delete/-rotate-url)",
	Args:  exactArgs(1),
	RunE:  runAutopilotTriggerList,
}

var autopilotTriggerUpdateCmd = &cobra.Command{
	Use:   "trigger-update <autopilot-id> <trigger-id>",
	Short: "Update an existing trigger",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerUpdate,
}

var autopilotTriggerDeleteCmd = &cobra.Command{
	Use:   "trigger-delete <autopilot-id> <trigger-id>",
	Short: "Delete a trigger",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerDelete,
}

var autopilotTriggerDryRunCmd = &cobra.Command{
	Use:   "trigger-dry-run <autopilot-id> <trigger-id>",
	Short: "Preview what a trigger would do, without firing it",
	Long: `Replay a decision without any side effect.

With --payload-file the sample event is run through a webhook trigger's whole
delivery decision — event filters, the natural-language routing rule (the
classifier really is called), pause state and run quota — and the verdict is
printed. Nothing is recorded: no delivery, no run.

Without it, a schedule trigger is previewed instead: the next five firing
instants the scheduler itself would pick, plus whatever would suppress the
dispatch.`,
	Args: exactArgs(2),
	RunE: runAutopilotTriggerDryRun,
}

var autopilotTriggerRotateURLCmd = &cobra.Command{
	Use:   "trigger-rotate-url <autopilot-id> <trigger-id>",
	Short: "Rotate the webhook URL of a webhook trigger",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerRotateURL,
}

func init() {
	autopilotCmd.AddCommand(autopilotListCmd)
	autopilotCmd.AddCommand(autopilotGetCmd)
	autopilotCmd.AddCommand(autopilotCreateCmd)
	autopilotCmd.AddCommand(autopilotUpdateCmd)
	autopilotCmd.AddCommand(autopilotDeleteCmd)
	autopilotCmd.AddCommand(autopilotTriggerCmd)
	autopilotCmd.AddCommand(autopilotRunsCmd)
	autopilotCmd.AddCommand(autopilotTriggerAddCmd)
	autopilotCmd.AddCommand(autopilotTriggerListCmd)
	autopilotCmd.AddCommand(autopilotTriggerUpdateCmd)
	autopilotCmd.AddCommand(autopilotTriggerDeleteCmd)
	autopilotCmd.AddCommand(autopilotTriggerRotateURLCmd)
	autopilotCmd.AddCommand(autopilotTriggerDryRunCmd)

	// list
	autopilotListCmd.Flags().String("status", "", "Filter by status (active, paused)")
	autopilotListCmd.Flags().String("output", "table", "Output format: table or json")
	autopilotListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// get
	autopilotGetCmd.Flags().String("output", "json", "Output format: table or json")
	autopilotGetCmd.Flags().Bool("show-secrets", false, "Include live webhook credentials in JSON output (unsafe for logs)")

	// create
	autopilotCreateCmd.Flags().String("title", "", "Autopilot title (required)")
	autopilotCreateCmd.Flags().String("description", "", "Autopilot description (used as the run prompt)")
	autopilotCreateCmd.Flags().String("agent", "", "Assignee agent (name or ID) — required")
	autopilotCreateCmd.Flags().String("mode", "", "Execution mode: create_issue or run_only (required)")
	autopilotCreateCmd.Flags().String("project", "", "Project ID (optional)")
	autopilotCreateCmd.Flags().String("issue-title-template", "", "Template for issue titles (create_issue mode). Only {{date}} (UTC, YYYY-MM-DD) is interpolated; any other {{...}} token is rejected at create-time.")
	autopilotCreateCmd.Flags().StringArray("subscriber", nil, "Member subscriber to notify for issues this autopilot creates (name or user ID; repeatable)")
	autopilotCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// update
	autopilotUpdateCmd.Flags().String("title", "", "New title")
	autopilotUpdateCmd.Flags().String("description", "", "New description")
	autopilotUpdateCmd.Flags().String("agent", "", "New assignee agent (name or ID)")
	autopilotUpdateCmd.Flags().String("project", "", "New project ID (use empty string to clear)")
	autopilotUpdateCmd.Flags().String("status", "", "New status (active, paused)")
	autopilotUpdateCmd.Flags().String("mode", "", "New execution mode (create_issue or run_only)")
	autopilotUpdateCmd.Flags().String("issue-title-template", "", "New issue title template. Only {{date}} (UTC, YYYY-MM-DD) is interpolated; any other {{...}} token is rejected.")
	autopilotUpdateCmd.Flags().StringArray("subscriber", nil, "Replace subscribers with this member (name or user ID; repeatable)")
	autopilotUpdateCmd.Flags().Bool("clear-subscribers", false, "Remove all autopilot subscribers")
	autopilotUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// delete
	// (no flags)

	// trigger (manual run)
	autopilotTriggerCmd.Flags().String("output", "json", "Output format: table or json")

	// runs
	autopilotRunsCmd.Flags().Int("limit", 20, "Max number of runs to return")
	autopilotRunsCmd.Flags().Int("offset", 0, "Pagination offset")
	autopilotRunsCmd.Flags().String("output", "table", "Output format: table or json")

	// trigger-add — supports schedule and webhook
	autopilotTriggerAddCmd.Flags().String("kind", "schedule", "Trigger kind: schedule or webhook")
	autopilotTriggerAddCmd.Flags().String("cron", "", "Cron expression (required for --kind schedule)")
	autopilotTriggerAddCmd.Flags().String("timezone", "", "IANA timezone (default UTC; schedule only)")
	autopilotTriggerAddCmd.Flags().String("label", "", "Optional human-readable label")
	autopilotTriggerAddCmd.Flags().Int("window-minutes", 0, "Fire at a random minute within this many minutes after the cron time (0-1439; schedule only)")
	autopilotTriggerAddCmd.Flags().String("event-match-criteria", "", "Plain-words rule for which events should run this autopilot, judged per delivery by a model (webhook only)")
	autopilotTriggerAddCmd.Flags().String("output", "json", "Output format: table or json")

	// trigger-list
	autopilotTriggerListCmd.Flags().String("output", "table", "Output format: table or json")
	autopilotTriggerListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// trigger-rotate-url — webhook only
	autopilotTriggerRotateURLCmd.Flags().String("output", "json", "Output format: table or json")
	autopilotTriggerRotateURLCmd.Flags().BoolP("yes", "y", false, "Skip the interactive confirmation prompt")

	// trigger-update
	autopilotTriggerUpdateCmd.Flags().Bool("enabled", true, "Enable or disable the trigger")
	autopilotTriggerUpdateCmd.Flags().String("cron", "", "New cron expression")
	autopilotTriggerUpdateCmd.Flags().String("timezone", "", "New IANA timezone")
	autopilotTriggerUpdateCmd.Flags().String("label", "", "New label")
	autopilotTriggerUpdateCmd.Flags().Int("window-minutes", 0, "New firing band in minutes (0 fires exactly on the cron time; schedule only)")
	autopilotTriggerUpdateCmd.Flags().String("event-match-criteria", "", "New plain-words routing rule; pass an empty string to clear it (webhook only)")
	autopilotTriggerUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// trigger-dry-run
	autopilotTriggerDryRunCmd.Flags().String("payload-file", "", "JSON file holding the sample event body (webhook triggers; omit for a schedule preview)")
	autopilotTriggerDryRunCmd.Flags().StringArray("header", nil, "Request header the event normalizer should see, as Name=Value (repeatable; webhook only)")
	autopilotTriggerDryRunCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Autopilot commands
// ---------------------------------------------------------------------------

func runAutopilotList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := "/api/autopilots"
	if status, _ := cmd.Flags().GetString("status"); status != "" {
		path += "?" + url.Values{"status": {status}}.Encode()
	}

	var resp struct {
		Autopilots []map[string]any `json:"autopilots"`
		Total      int              `json:"total"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list autopilots: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	actors := loadActorDisplayLookup(ctx, client)
	// NEXT_RUN is what distinguishes a scheduled autopilot from one with no
	// trigger at all. The list payload has carried next_run_at all along, but
	// the table dropped it, leaving the two indistinguishable here (MUL-6680).
	headers := []string{"ID", "TITLE", "STATUS", "MODE", "ASSIGNEE", "NEXT_RUN", "LAST_RUN"}
	rows := make([][]string, 0, len(resp.Autopilots))
	for _, a := range resp.Autopilots {
		rows = append(rows, []string{
			displayID(strVal(a, "id"), fullID),
			strVal(a, "title"),
			strVal(a, "status"),
			strVal(a, "execution_mode"),
			actors.agent(strVal(a, "assignee_id")),
			relativeTimestamp(strVal(a, "next_run_at")),
			relativeTimestamp(strVal(a, "last_run_at")),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotGet(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	showSecrets, _ := cmd.Flags().GetBool("show-secrets")
	if showSecrets && output != "json" {
		return fmt.Errorf("--show-secrets requires --output json")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/autopilots/"+autopilotRef.ID, &resp); err != nil {
		return fmt.Errorf("get autopilot: %w", err)
	}

	if showSecrets {
		fmt.Fprintln(os.Stderr, "Warning: --show-secrets exposes live webhook credentials; keep this output out of logs and shared transcripts.")
	} else {
		redactAutopilotWebhookCredentials(resp)
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	ap, _ := resp["autopilot"].(map[string]any)
	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "TITLE", "STATUS", "MODE", "ASSIGNEE", "LAST_RUN"}
	rows := [][]string{{
		strVal(ap, "id"),
		strVal(ap, "title"),
		strVal(ap, "status"),
		strVal(ap, "execution_mode"),
		actors.agent(strVal(ap, "assignee_id")),
		strVal(ap, "last_run_at"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func redactAutopilotWebhookCredentials(resp map[string]any) {
	triggers, ok := resp["triggers"].([]any)
	if !ok {
		return
	}
	for _, raw := range triggers {
		trigger, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		_, hasTokenField := trigger["webhook_token"]
		_, hasPathField := trigger["webhook_path"]
		_, hasURLField := trigger["webhook_url"]
		if !hasTokenField && !hasPathField && !hasURLField {
			continue
		}

		token := strVal(trigger, "webhook_token")
		hasToken, _ := trigger["has_webhook_token"].(bool)
		hasToken = hasToken ||
			strVal(trigger, "kind") == "webhook" ||
			token != "" ||
			strVal(trigger, "webhook_path") != "" ||
			strVal(trigger, "webhook_url") != ""
		trigger["has_webhook_token"] = hasToken
		if hint := webhookTokenHint(token); hint != "" {
			trigger["webhook_token_hint"] = hint
		} else {
			trigger["webhook_token_hint"] = nil
		}
		trigger["webhook_token"] = nil
		trigger["webhook_path"] = nil
		trigger["webhook_url"] = nil
	}
}

// relativeTimestamp renders an RFC3339 timestamp as a short, fixed-width-ish
// relative string ("in 2h", "3d ago", "—" when absent or unparseable). Table
// columns use this instead of the raw timestamp so NEXT_RUN fits alongside the
// existing columns on a narrow terminal, and so "never scheduled" reads as a
// visibly different value rather than an empty cell (MUL-6680).
func relativeTimestamp(ts string) string {
	return relativeTimestampAt(ts, time.Now())
}

func relativeTimestampAt(ts string, now time.Time) string {
	trimmed := strings.TrimSpace(ts)
	if trimmed == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return "—"
	}

	d := t.Sub(now)
	suffix := " ago"
	if d >= 0 {
		suffix = ""
	} else {
		d = -d
	}

	var magnitude string
	switch {
	case d < time.Minute:
		magnitude = fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		magnitude = fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		magnitude = fmt.Sprintf("%dh", int(d.Hours()))
	default:
		magnitude = fmt.Sprintf("%dd", int(d.Hours()/24))
	}

	if suffix == "" {
		return "in " + magnitude
	}
	return magnitude + suffix
}

func webhookTokenHint(token string) string {
	if len(token) < 4 {
		return ""
	}
	return token[len(token)-4:]
}

func runAutopilotCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	agent, _ := cmd.Flags().GetString("agent")
	if agent == "" {
		return fmt.Errorf("--agent is required (agent name or ID)")
	}
	mode, _ := cmd.Flags().GetString("mode")
	if mode == "" {
		return fmt.Errorf("--mode is required (create_issue or run_only)")
	}
	if mode != "create_issue" && mode != "run_only" {
		return fmt.Errorf("--mode must be create_issue or run_only")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	agentID, err := resolveAgent(ctx, client, agent)
	if err != nil {
		return fmt.Errorf("resolve agent: %w", err)
	}

	body := map[string]any{
		"title":          title,
		"assignee_id":    agentID,
		"execution_mode": mode,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		projectRef, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve project: %w", err)
		}
		body["project_id"] = projectRef.ID
	}
	if v, _ := cmd.Flags().GetString("issue-title-template"); v != "" {
		body["issue_title_template"] = v
	}
	if subscriberRefs, _ := cmd.Flags().GetStringArray("subscriber"); len(subscriberRefs) > 0 {
		subscribers, err := resolveAutopilotSubscriberInputs(ctx, client, subscriberRefs)
		if err != nil {
			return err
		}
		body["subscribers"] = subscribers
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/autopilots", body, &result); err != nil {
		return fmt.Errorf("create autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Autopilot created: %s (%s)\n", strVal(result, "title"), strVal(result, "id"))
	return nil
}

func runAutopilotUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	body := map[string]any{}
	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		body["title"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("agent") {
		v, _ := cmd.Flags().GetString("agent")
		agentID, resolveErr := resolveAgent(ctx, client, v)
		if resolveErr != nil {
			return fmt.Errorf("resolve agent: %w", resolveErr)
		}
		body["assignee_type"] = "agent"
		body["assignee_id"] = agentID
	}
	if cmd.Flags().Changed("project") {
		v, _ := cmd.Flags().GetString("project")
		if v == "" {
			body["project_id"] = nil
		} else {
			projectRef, err := resolveProjectID(ctx, client, v)
			if err != nil {
				return fmt.Errorf("resolve project: %w", err)
			}
			body["project_id"] = projectRef.ID
		}
	}
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		body["status"] = v
	}
	if cmd.Flags().Changed("mode") {
		v, _ := cmd.Flags().GetString("mode")
		if v != "create_issue" && v != "run_only" {
			return fmt.Errorf("--mode must be create_issue or run_only")
		}
		body["execution_mode"] = v
	}
	if cmd.Flags().Changed("issue-title-template") {
		v, _ := cmd.Flags().GetString("issue-title-template")
		body["issue_title_template"] = v
	}
	clearSubscribers, _ := cmd.Flags().GetBool("clear-subscribers")
	subscriberRefs, _ := cmd.Flags().GetStringArray("subscriber")
	if clearSubscribers && len(subscriberRefs) > 0 {
		return fmt.Errorf("--subscriber and --clear-subscribers are mutually exclusive")
	}
	if clearSubscribers {
		body["subscribers"] = []map[string]string{}
	} else if cmd.Flags().Changed("subscriber") {
		subscribers, err := resolveAutopilotSubscriberInputs(ctx, client, subscriberRefs)
		if err != nil {
			return err
		}
		body["subscribers"] = subscribers
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --description, --agent, --status, --mode, etc.")
	}

	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/autopilots/"+autopilotRef.ID, body, &result); err != nil {
		return fmt.Errorf("update autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Autopilot updated: %s (%s)\n", strVal(result, "title"), strVal(result, "id"))
	return nil
}

func runAutopilotDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/autopilots/"+autopilotRef.ID); err != nil {
		return fmt.Errorf("delete autopilot: %w", err)
	}
	fmt.Printf("Autopilot %s deleted.\n", autopilotRef.Display)
	return nil
}

func runAutopilotTrigger(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(30*time.Second))
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	var run map[string]any
	if err := client.PostJSON(ctx, "/api/autopilots/"+autopilotRef.ID+"/trigger", nil, &run); err != nil {
		return fmt.Errorf("trigger autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, run)
	}
	fmt.Printf("Autopilot triggered: run %s (status: %s)\n", strVal(run, "id"), strVal(run, "status"))
	return nil
}

func runAutopilotRuns(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	params := url.Values{}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}
	path := "/api/autopilots/" + autopilotRef.ID + "/runs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp struct {
		Runs  []map[string]any `json:"runs"`
		Total int              `json:"total"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	headers := []string{"ID", "SOURCE", "STATUS", "ISSUE", "TRIGGERED_AT", "COMPLETED_AT"}
	rows := make([][]string, 0, len(resp.Runs))
	for _, r := range resp.Runs {
		rows = append(rows, []string{
			strVal(r, "id"),
			strVal(r, "source"),
			strVal(r, "status"),
			strVal(r, "issue_id"),
			strVal(r, "triggered_at"),
			strVal(r, "completed_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotTriggerList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	// The detail endpoint already returns triggers as a top-level sibling of
	// "autopilot"; this command exists so the ids that trigger-update /
	// trigger-delete / trigger-rotate-url require are discoverable without
	// knowing that envelope shape (MUL-6680).
	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/autopilots/"+autopilotRef.ID, &resp); err != nil {
		return fmt.Errorf("get autopilot: %w", err)
	}
	redactAutopilotWebhookCredentials(resp)

	triggersRaw, _ := resp["triggers"].([]any)
	triggers := make([]map[string]any, 0, len(triggersRaw))
	for _, raw := range triggersRaw {
		if t, ok := raw.(map[string]any); ok {
			triggers = append(triggers, t)
		}
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"triggers": triggers, "total": len(triggers)})
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "KIND", "ENABLED", "SCHEDULE", "NEXT_RUN", "LABEL"}
	rows := make([][]string, 0, len(triggers))
	for _, t := range triggers {
		enabled := "no"
		if b, ok := t["enabled"].(bool); ok && b {
			enabled = "yes"
		}
		schedule := strVal(t, "cron_expression")
		if tz := strVal(t, "timezone"); schedule != "" && tz != "" {
			schedule += " (" + tz + ")"
		}
		rows = append(rows, []string{
			displayID(strVal(t, "id"), fullID),
			strVal(t, "kind"),
			enabled,
			schedule,
			relativeTimestamp(strVal(t, "next_run_at")),
			strVal(t, "label"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotTriggerAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	kind, _ := cmd.Flags().GetString("kind")
	if kind == "" {
		kind = "schedule"
	}
	if kind != "schedule" && kind != "webhook" {
		return fmt.Errorf("--kind must be schedule or webhook")
	}
	cron, _ := cmd.Flags().GetString("cron")
	if kind == "schedule" && cron == "" {
		return fmt.Errorf("--cron is required for --kind schedule")
	}
	if kind == "webhook" {
		if v, _ := cmd.Flags().GetString("timezone"); v != "" {
			return fmt.Errorf("--timezone is only valid with --kind schedule")
		}
		if cron != "" {
			return fmt.Errorf("--cron is only valid with --kind schedule")
		}
		if cmd.Flags().Changed("window-minutes") {
			return fmt.Errorf("--window-minutes is only valid with --kind schedule")
		}
	}
	if kind == "schedule" && cmd.Flags().Changed("event-match-criteria") {
		return fmt.Errorf("--event-match-criteria is only valid with --kind webhook")
	}

	body := map[string]any{"kind": kind}
	if kind == "schedule" {
		body["cron_expression"] = cron
		if v, _ := cmd.Flags().GetString("timezone"); v != "" {
			body["timezone"] = v
		}
		// Sent only when asked for: the server owns the default, and a
		// hard-coded 0 here would silently overwrite it.
		if cmd.Flags().Changed("window-minutes") {
			v, _ := cmd.Flags().GetInt("window-minutes")
			body["window_minutes"] = v
		}
	}
	if kind == "webhook" && cmd.Flags().Changed("event-match-criteria") {
		v, _ := cmd.Flags().GetString("event-match-criteria")
		body["event_match_criteria"] = v
	}
	if v, _ := cmd.Flags().GetString("label"); v != "" {
		body["label"] = v
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/autopilots/"+autopilotRef.ID+"/triggers", body, &result); err != nil {
		return fmt.Errorf("create trigger: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Trigger created: %s (kind=%s)\n", strVal(result, "id"), strVal(result, "kind"))
	if kind == "webhook" {
		printWebhookURL(client, result)
	}
	return nil
}

// printWebhookURL emits the webhook URL with the priority webhook_url >
// composed-from-base. Keeps the table-output flow useful — without this the
// table renderer drops the most important new piece of information.
func printWebhookURL(client *cli.APIClient, trigger map[string]any) {
	if u := strVal(trigger, "webhook_url"); u != "" {
		fmt.Printf("Webhook URL: %s\n", u)
		return
	}
	if path := strVal(trigger, "webhook_path"); path != "" {
		base := strings.TrimRight(client.BaseURL, "/")
		fmt.Printf("Webhook URL: %s%s\n", base, path)
	}
}

func runAutopilotTriggerRotateURL(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}
	triggerRef, err := resolveAutopilotTriggerID(ctx, client, autopilotRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve trigger: %w", err)
	}

	// Confirmation: rotation invalidates the current URL immediately. The UI
	// version uses an AlertDialog; the CLI mirrors that with a y/N prompt
	// unless --yes was passed for scripted use. Style matches confirmOverwrite
	// in cmd_setup.go.
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		fmt.Fprintln(os.Stderr, "This will invalidate the current webhook URL immediately. Continue? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(os.Stderr, "Aborted.")
			return nil
		}
	}

	var result map[string]any
	path := "/api/autopilots/" + autopilotRef.ID + "/triggers/" + triggerRef.ID + "/rotate-webhook-token"
	if err := client.PostJSON(ctx, path, nil, &result); err != nil {
		return fmt.Errorf("rotate webhook url: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Webhook URL rotated for trigger %s\n", strVal(result, "id"))
	printWebhookURL(client, result)
	return nil
}

func runAutopilotTriggerUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		body["enabled"] = v
	}
	if cmd.Flags().Changed("cron") {
		v, _ := cmd.Flags().GetString("cron")
		body["cron_expression"] = v
	}
	if cmd.Flags().Changed("timezone") {
		v, _ := cmd.Flags().GetString("timezone")
		body["timezone"] = v
	}
	if cmd.Flags().Changed("label") {
		v, _ := cmd.Flags().GetString("label")
		body["label"] = v
	}
	if cmd.Flags().Changed("window-minutes") {
		v, _ := cmd.Flags().GetInt("window-minutes")
		body["window_minutes"] = v
	}
	// An empty --event-match-criteria clears the rule, so presence — not
	// emptiness — is what decides whether the field is sent.
	if cmd.Flags().Changed("event-match-criteria") {
		v, _ := cmd.Flags().GetString("event-match-criteria")
		body["event_match_criteria"] = v
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --enabled, --cron, --timezone, --label, --window-minutes, or --event-match-criteria")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}
	triggerRef, err := resolveAutopilotTriggerID(ctx, client, autopilotRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve trigger: %w", err)
	}

	var result map[string]any
	path := "/api/autopilots/" + autopilotRef.ID + "/triggers/" + triggerRef.ID
	if err := client.PatchJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("update trigger: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Trigger updated: %s\n", strVal(result, "id"))
	return nil
}

func runAutopilotTriggerDryRun(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	payloadFile, _ := cmd.Flags().GetString("payload-file")
	headerArgs, _ := cmd.Flags().GetStringArray("header")
	if payloadFile == "" && len(headerArgs) > 0 {
		return fmt.Errorf("--header is only meaningful with --payload-file")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}
	triggerRef, err := resolveAutopilotTriggerID(ctx, client, autopilotRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve trigger: %w", err)
	}
	path := "/api/autopilots/" + autopilotRef.ID + "/triggers/" + triggerRef.ID + "/dry-run"

	var result map[string]any
	if payloadFile == "" {
		// No sample event to send: this is the schedule preview.
		if err := client.GetJSON(ctx, path, &result); err != nil {
			return fmt.Errorf("dry-run trigger: %w", err)
		}
		return printScheduleDryRun(cmd, result)
	}

	raw, err := os.ReadFile(payloadFile)
	if err != nil {
		return fmt.Errorf("read payload file: %w", err)
	}
	// Validate locally so a typo in the file is reported against the file,
	// not as a 400 from a server the user then has to correlate back.
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("%s is not valid JSON: %w", payloadFile, err)
	}
	headers := map[string]string{}
	for _, h := range headerArgs {
		name, value, ok := strings.Cut(h, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("--header %q must be Name=Value", h)
		}
		headers[strings.TrimSpace(name)] = value
	}
	body := map[string]any{"payload": payload}
	if len(headers) > 0 {
		body["headers"] = headers
	}
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("dry-run trigger: %w", err)
	}
	return printWebhookDryRun(cmd, result)
}

func printWebhookDryRun(cmd *cobra.Command, result map[string]any) error {
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	verdict := "would NOT run"
	if b, ok := result["would_run"].(bool); ok && b {
		verdict = "would run"
	}
	fmt.Printf("%s (event=%s)\n", verdict, strVal(result, "event"))
	if reason := strVal(result, "reason_code"); reason != "" {
		fmt.Printf("Reason: %s\n", reason)
	}
	if explanation := strVal(result, "explanation"); explanation != "" {
		fmt.Printf("Explanation: %s\n", explanation)
	}
	if filters, ok := result["matched_filters"].([]any); ok && len(filters) > 0 {
		names := make([]string, 0, len(filters))
		for _, raw := range filters {
			if f, ok := raw.(map[string]any); ok {
				names = append(names, strVal(f, "event"))
			}
		}
		fmt.Printf("Matched filters: %s\n", strings.Join(names, ", "))
	}
	return nil
}

func printScheduleDryRun(cmd *cobra.Command, result map[string]any) error {
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	if b, ok := result["would_run"].(bool); ok && !b {
		fmt.Printf("Would NOT run: %s\n", strVal(result, "reason_code"))
	}
	runs, _ := result["next_runs"].([]any)
	if len(runs) == 0 {
		fmt.Println("This schedule never fires again.")
		return nil
	}
	headers := []string{"NEXT_RUN", "IN"}
	rows := make([][]string, 0, len(runs))
	for _, raw := range runs {
		at, _ := raw.(string)
		rows = append(rows, []string{at, relativeTimestamp(at)})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotTriggerDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	autopilotRef, err := resolveAutopilotID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve autopilot: %w", err)
	}
	triggerRef, err := resolveAutopilotTriggerID(ctx, client, autopilotRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve trigger: %w", err)
	}

	path := "/api/autopilots/" + autopilotRef.ID + "/triggers/" + triggerRef.ID
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete trigger: %w", err)
	}
	fmt.Printf("Trigger %s deleted.\n", triggerRef.ID)
	return nil
}

func resolveAutopilotSubscriberInputs(ctx context.Context, client *cli.APIClient, refs []string) ([]map[string]string, error) {
	inputs := make([]map[string]string, 0, len(refs))
	seen := map[string]struct{}{}
	memberOnly := assigneeKinds{member: true}
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return nil, fmt.Errorf("--subscriber cannot be empty")
		}
		userType, userID, err := resolveAssignee(ctx, client, ref, memberOnly)
		if err != nil {
			return nil, fmt.Errorf("resolve subscriber %q: %w", ref, err)
		}
		if userType != "member" {
			return nil, fmt.Errorf("subscriber %q resolved to %s; autopilot subscribers must be members", ref, userType)
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		inputs = append(inputs, map[string]string{
			"user_type": "member",
			"user_id":   userID,
		})
	}
	return inputs, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// uuidRegexp matches a canonical UUID (8-4-4-4-12 hex).
var uuidRegexp = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// resolveAgent accepts either a UUID or an agent name (case-insensitive substring)
// and returns the agent's UUID. Errors on no match or ambiguous match.
func resolveAgent(ctx context.Context, client *cli.APIClient, nameOrID string) (string, error) {
	if uuidRegexp.MatchString(nameOrID) {
		return nameOrID, nil
	}
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("workspace ID is required to resolve agents; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	var agents []map[string]any
	agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
	if err := client.GetJSON(ctx, agentPath, &agents); err != nil {
		return "", fmt.Errorf("fetch agents: %w", err)
	}

	nameLower := strings.ToLower(nameOrID)
	type match struct{ ID, Name string }
	var matches []match
	for _, a := range agents {
		aName := strVal(a, "name")
		if strings.Contains(strings.ToLower(aName), nameLower) {
			matches = append(matches, match{ID: strVal(a, "id"), Name: aName})
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no agent found matching %q", nameOrID)
	case 1:
		return matches[0].ID, nil
	default:
		var parts []string
		for _, m := range matches {
			parts = append(parts, fmt.Sprintf("  %q (%s)", m.Name, truncateID(m.ID)))
		}
		return "", fmt.Errorf("ambiguous agent %q; matches:\n%s", nameOrID, strings.Join(parts, "\n"))
	}
}
