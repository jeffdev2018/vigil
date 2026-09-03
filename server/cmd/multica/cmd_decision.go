package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// multica decision ask <issue-id> — file a Decision Card (K01) from a run.

var decisionCmd = &cobra.Command{
	Use:   "decision",
	Short: "Ask a human for a typed decision on an issue",
}

var decisionAskCmd = &cobra.Command{
	Use:   "ask <issue-id>",
	Short: "File a Decision Card: a question with options, a recommendation and an urgency",
	Long: `Files a Decision Card on the issue and returns it. The human answers from
the issue; a new run is then queued on the issue with the answer in its
handoff note. After asking, finish your turn: do not poll for the answer.

Options are repeatable: --option "id=Label" or --option "id=Label|impact".`,
	Args: exactArgs(1),
	RunE: runDecisionAsk,
}

func init() {
	decisionAskCmd.Flags().String("question", "", "The question to decide (required)")
	decisionAskCmd.Flags().StringArray("option", nil, `Option as "id=Label" or "id=Label|impact" (at least two)`)
	decisionAskCmd.Flags().String("recommend", "", "Id of the option you recommend")
	decisionAskCmd.Flags().String("urgency", "normal", "low | normal | high")
	decisionAskCmd.Flags().String("output", "json", "Output format: json")
	decisionCmd.AddCommand(decisionAskCmd)
	rootCmd.AddCommand(decisionCmd)
}

func runDecisionAsk(cmd *cobra.Command, args []string) error {
	question, _ := cmd.Flags().GetString("question")
	if strings.TrimSpace(question) == "" {
		return fmt.Errorf("--question is required")
	}
	rawOptions, _ := cmd.Flags().GetStringArray("option")
	if len(rawOptions) < 2 {
		return fmt.Errorf("at least two --option are required")
	}
	options := make([]map[string]string, 0, len(rawOptions))
	for _, raw := range rawOptions {
		id, rest, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(id) == "" || strings.TrimSpace(rest) == "" {
			return fmt.Errorf("--option %q must be id=Label or id=Label|impact", raw)
		}
		label, impact, _ := strings.Cut(rest, "|")
		o := map[string]string{"id": strings.TrimSpace(id), "label": strings.TrimSpace(label)}
		if strings.TrimSpace(impact) != "" {
			o["impact"] = strings.TrimSpace(impact)
		}
		options = append(options, o)
	}
	recommend, _ := cmd.Flags().GetString("recommend")
	urgency, _ := cmd.Flags().GetString("urgency")
	body := map[string]any{"question": question, "options": options, "urgency": urgency}
	if recommend != "" {
		body["recommended_option_id"] = recommend
	}

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
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/decisions", body, &result); err != nil {
		return fmt.Errorf("ask decision: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}
