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

var triageCmd = &cobra.Command{
	Use:   "triage",
	Short: "Read the triage queue and suggest verdicts on its items",
}

var triageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List triage items (default: the pending queue)",
	RunE:  runTriageList,
}

var triageVerdictCmd = &cobra.Command{
	Use:   "verdict <item-id>",
	Short: "Suggest accept or dismiss on a pending triage item (agents only)",
	Long: "Record a suggested verdict on a pending triage item.\n\n" +
		"A verdict is advisory: the item stays in the queue and a human still\n" +
		"decides. Exactly one of --accept or --dismiss is required.",
	Args: exactArgs(1),
	RunE: runTriageVerdict,
}

func init() {
	triageCmd.AddCommand(triageListCmd)
	triageCmd.AddCommand(triageVerdictCmd)

	// list
	triageListCmd.Flags().Bool("pending", false, "List the pending queue (the default)")
	triageListCmd.Flags().String("state", "", "State to list: pending, accepted, dismissed, merged")
	triageListCmd.Flags().Bool("include-snoozed", false, "Include pending items parked by a snooze")
	triageListCmd.Flags().Int("limit", 50, "Maximum items to return (1-100)")
	triageListCmd.Flags().String("output", "table", "Output format: table or json")
	triageListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// verdict
	triageVerdictCmd.Flags().Bool("accept", false, "Suggest accepting the item")
	triageVerdictCmd.Flags().Bool("dismiss", false, "Suggest dismissing the item")
	triageVerdictCmd.Flags().String("reason", "", "Why — shown to the human who decides")
	triageVerdictCmd.Flags().String("output", "json", "Output format: table or json")
}

func runTriageList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	state, _ := cmd.Flags().GetString("state")
	pending, _ := cmd.Flags().GetBool("pending")
	if state == "" || pending {
		state = "pending"
	}
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 || limit > 100 {
		return fmt.Errorf("--limit must be between 1 and 100")
	}
	query := url.Values{"state": {state}, "limit": {strconv.Itoa(limit)}}
	if includeSnoozed, _ := cmd.Flags().GetBool("include-snoozed"); includeSnoozed {
		query.Set("include_snoozed", "true")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := client.GetJSON(ctx, "/api/triage/items?"+query.Encode(), &resp); err != nil {
		return fmt.Errorf("list triage items: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TITLE", "SOURCE", "SEEN", "VERDICT"}
	rows := make([][]string, 0, len(resp.Items))
	for _, item := range resp.Items {
		rows = append(rows, []string{
			displayID(strVal(item, "id"), fullID),
			strVal(item, "title"),
			strVal(item, "source_name"),
			relativeTimestamp(strVal(item, "first_seen_at")),
			strVal(item, "verdict"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTriageVerdict(cmd *cobra.Command, args []string) error {
	accept, _ := cmd.Flags().GetBool("accept")
	dismiss, _ := cmd.Flags().GetBool("dismiss")
	verdict, err := triageVerdictFromFlags(accept, dismiss)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	reason, _ := cmd.Flags().GetString("reason")
	body := map[string]any{"verdict": verdict, "reason": reason}

	var resp map[string]any
	if err := client.PostJSON(ctx, "/api/triage/items/"+url.PathEscape(args[0])+"/verdict", body, &resp); err != nil {
		return fmt.Errorf("set triage verdict: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "Suggested %s on %s\n", verdict, args[0])
	return nil
}

// triageVerdictFromFlags maps the two mutually exclusive flags onto the API's
// verdict value. Neither or both is a caller mistake, not a default.
func triageVerdictFromFlags(accept, dismiss bool) (string, error) {
	switch {
	case accept && dismiss:
		return "", fmt.Errorf("--accept and --dismiss are mutually exclusive")
	case accept:
		return "accept", nil
	case dismiss:
		return "dismiss", nil
	default:
		return "", fmt.Errorf("one of --accept or --dismiss is required")
	}
}
