package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// multica interview ask <issue-id> — Requirement Interview (K13): one to
// three multiple-choice questions before coding, asked together.

var interviewCmd = &cobra.Command{
	Use:   "interview",
	Short: "Ask a human up to three multiple-choice questions before coding",
}

var interviewAskCmd = &cobra.Command{
	Use:   "ask <issue-id>",
	Short: "File a requirement interview from a JSON file (or stdin with -)",
	Long: `Files one to three Decision Cards at once and parks the issue in
"Waiting for PM". The run resumes only once every question is answered, with
the answers in order in its handoff note. After asking, finish your turn.

The JSON is {"questions": [{"question": "...", "options": [{"id": "a", "label": "..."}, ...],
"recommended_option_id": "a"}]}; each question needs 2 to 8 options.`,
	Args: exactArgs(1),
	RunE: runInterviewAsk,
}

func init() {
	interviewAskCmd.Flags().String("file", "", "JSON file with the questions; '-' or empty reads stdin")
	interviewAskCmd.Flags().String("output", "json", "Output format: json")
	interviewCmd.AddCommand(interviewAskCmd)
	rootCmd.AddCommand(interviewCmd)
}

func runInterviewAsk(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	var raw []byte
	var err error
	if file == "" || file == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(file)
	}
	if err != nil {
		return fmt.Errorf("read questions: %w", err)
	}
	var body struct {
		Questions []map[string]any `json:"questions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("questions must be JSON {\"questions\": [...]}: %w", err)
	}
	if len(body.Questions) == 0 {
		return fmt.Errorf("at least one question is required")
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
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/interview", body, &result); err != nil {
		return fmt.Errorf("ask interview: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}
