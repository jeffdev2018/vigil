package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// `multica issue ask-agent` — one agent addressing another on an issue
// (F19 / JEF-32).
//
// This is `issue comment add` with a declared intent and two circuit breakers.
// Everything the reader of the issue sees is the same: the message IS a comment,
// so the exchange stays in the thread instead of moving to a channel nobody can
// audit. What the dedicated command buys is:
//
//   - the intent (question / review / handoff) is recorded on the comment, which
//     the generic comment endpoint cannot do — there is no request field for it
//     there, deliberately, so it cannot be forged;
//   - the server composes the mention markup, so the declared intent and the
//     agent it addresses cannot disagree;
//   - the hop is counted, and refused past the depth or the per-issue rate.
//
// It is NOT a reply channel: sending finishes here. An answer arrives later as a
// new comment, which wakes the sender the ordinary way, if at all.
var issueAskAgentCmd = &cobra.Command{
	Use:   "ask-agent <issue-id>",
	Short: "Send an agent-to-agent message on an issue",
	Long: "Ask another agent for something on this issue, with a declared intent.\n\n" +
		"Intents:\n" +
		"  question  you need an answer to continue\n" +
		"  review    you want the other agent to check work you have done\n" +
		"  handoff   you are asking the other agent to take the work on\n\n" +
		"A handoff DOES NOT reassign the issue. It wakes the other agent and says\n" +
		"what you want; if they accept, they call `multica issue assign`.\n\n" +
		"The message is posted as an ordinary comment on the issue, so the person\n" +
		"who opened the issue can read the whole exchange.\n\n" +
		"Refusals you may see:\n" +
		"  403  you may not ask that agent — it is private, on someone else's\n" +
		"       allow-list, or the id names no agent in this workspace. The answer\n" +
		"       is the same for all three on purpose.\n" +
		"  409  your own run has finished; a completed run cannot send messages.\n" +
		"  429  a2a_depth_exceeded — the chain is already too far from the person\n" +
		"       who started it. Report back instead of forwarding again.\n" +
		"  429  a2a_budget_exceeded — this issue has had too many agent-to-agent\n" +
		"       messages in the last hour. Wait, or say what you need in a comment.",
	Args: exactArgs(1),
	RunE: runIssueAskAgent,
}

func runIssueAskAgent(cmd *cobra.Command, args []string) error {
	to, _ := cmd.Flags().GetString("to")
	if strings.TrimSpace(to) == "" {
		return fmt.Errorf("--to <agent-name-or-id> is required")
	}
	intent, _ := cmd.Flags().GetString("intent")
	intent = strings.TrimSpace(intent)
	switch intent {
	case "question", "review", "handoff":
	default:
		return fmt.Errorf("--intent must be one of: question, review, handoff")
	}
	body, hasBody, err := resolveTextFlag(cmd, "body")
	if err != nil {
		return err
	}
	if !hasBody || strings.TrimSpace(body) == "" {
		return fmt.Errorf("--body, --body-stdin, or --body-file is required")
	}
	if err := guardLocalPathLinks(body, "agent message body",
		"Attach the file to a comment with `multica issue comment add <issue-id> --attachment <path>` and reference it from there."); err != nil {
		return err
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
	// Resolved client-side by name for convenience only. The server still judges
	// the resulting id against the human at the head of this run's chain, so a
	// name that resolves here can still be refused there.
	toID, err := resolveAgent(ctx, client, to)
	if err != nil {
		return fmt.Errorf("resolve recipient: %w", err)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/agent-messages", map[string]any{
		"to_agent_id": toID,
		"intent":      intent,
		"body":        body,
	}, &result); err != nil {
		return fmt.Errorf("send agent message: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Sent %s to %s on issue %s.\n", intent, to, issueRef.Display)
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func init() {
	issueAskAgentCmd.Flags().String("to", "", "Recipient agent name or ID")
	issueAskAgentCmd.Flags().String("intent", "", "Why you are writing: question, review, or handoff")
	issueAskAgentCmd.Flags().String("body", "", "Message body (decodes \\n, \\r, \\t; pipe via --body-stdin for multi-line bodies)")
	issueAskAgentCmd.Flags().Bool("body-stdin", false, "Read the message body from stdin (preserves multi-line content verbatim)")
	issueAskAgentCmd.Flags().String("body-file", "", "Read the message body from a UTF-8 file. The path must be inside the current working directory unless --allow-external-file is set.")
	issueAskAgentCmd.Flags().Bool("allow-external-file", false, "Allow --body-file to read a path outside the current working directory")
	issueAskAgentCmd.Flags().String("output", "json", "Output format: table or json")
	issueCmd.AddCommand(issueAskAgentCmd)
}
