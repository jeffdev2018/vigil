package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var issueDecisionCmd = &cobra.Command{
	Use: "decision", Short: "Request a durable human decision or read its response",
	Long: "Request a decision addressed to a workspace member. The recipient answers in Inbox > Decisions. Saving an answer and starting a follow-up are separate human actions.",
}

var issueDecisionRequestCmd = &cobra.Command{
	Use: "request <issue>", Short: "Request a human decision for an issue run", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		question, _, err := resolveTextFlag(cmd, "question")
		if err != nil {
			return err
		}
		if question == "" {
			return fmt.Errorf("provide --question or --question-file")
		}
		contextText, _, err := resolveTextFlag(cmd, "context")
		if err != nil {
			return err
		}
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		issue, err := resolveIssueRef(ctx, client, args[0])
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		source, _ := cmd.Flags().GetString("source-run")
		recipient, _ := cmd.Flags().GetString("recipient")
		options, _ := cmd.Flags().GetStringArray("option")
		var result map[string]any
		if err := client.PostJSON(ctx, "/api/issues/"+issue.ID+"/decisions", map[string]any{
			"id": id, "source_task_id": source, "recipient_id": recipient, "question": question, "context": contextText, "options": options,
		}, &result); err != nil {
			return fmt.Errorf("request decision: %w", err)
		}
		return cli.PrintJSON(os.Stdout, result)
	},
}

var issueDecisionGetCmd = &cobra.Command{
	Use: "get <issue> <decision-id>", Short: "Read a decision as its recipient or source run", Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		issue, err := resolveIssueRef(ctx, client, args[0])
		if err != nil {
			return err
		}
		var result map[string]any
		if err := client.GetJSON(ctx, "/api/issues/"+issue.ID+"/decisions/"+url.PathEscape(args[1]), &result); err != nil {
			return fmt.Errorf("get decision: %w", err)
		}
		return cli.PrintJSON(os.Stdout, result)
	},
}

func init() {
	issueCmd.AddCommand(issueDecisionCmd)
	issueDecisionCmd.AddCommand(issueDecisionRequestCmd, issueDecisionGetCmd)
	flags := issueDecisionRequestCmd.Flags()
	flags.String("id", "", "Stable request UUID; reuse the same ID and content on retry")
	flags.String("source-run", "", "Source issue run UUID (must be your own run with a task credential)")
	flags.String("recipient", "", "Human recipient user UUID (your own user when using a human credential)")
	flags.StringArray("option", nil, "Suggested answer (repeatable, maximum 8)")
	for _, name := range []string{"question", "context"} {
		flags.String(name, "", "Decision "+name)
		flags.String(name+"-file", "", "Read "+name+" from a UTF-8 file")
		flags.Bool(name+"-stdin", false, "Read "+name+" from stdin")
	}
	for _, name := range []string{"id", "source-run", "recipient"} {
		_ = issueDecisionRequestCmd.MarkFlagRequired(name)
	}
}
