package main

// `multica autopilot draft` — an autopilot from a sentence (JEF-373).
//
// The model turns "every Monday at 9, list the open tickets" into a title, a
// 5-field cron, a timezone and the instruction the agent will follow, and the
// server validates the schedule against the same cron parser the scheduler
// uses. Draft alone writes nothing: it prints what would be created and the
// next three firing instants.
//
// `--create` files it through POST /api/autopilots/propose, which creates the
// autopilot PAUSED with its trigger disabled. Nothing runs until someone
// activates it — from the Decision Card when the proposal is attached to an
// issue, or from `multica autopilot update <id> --status active` otherwise.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var autopilotDraftCmd = &cobra.Command{
	Use:   "draft <sentence>",
	Short: "Turn a sentence into an autopilot (schedule, prompt, next runs)",
	Long: "Draft an autopilot from plain words and print it: title, cron, timezone,\n" +
		"the instruction each run follows, and the next three firing instants.\n\n" +
		"Without --create nothing is written; the draft is a preview.\n\n" +
		"With --create the autopilot is filed PAUSED, with its schedule disabled.\n" +
		"--activate files it running instead, with no card: the deciding person is you.\n\n" +
		"A person activates it — from the Decision Card when --issue attaches the\n" +
		"proposal to an issue, or by hand with `autopilot update --status active`\n" +
		"plus `autopilot trigger-update --enabled`.\n" +
		"--agent names the agent that will run it, and is required for a member.\n\n" +
		"Refusals you may see:\n" +
		"  503  no model is configured for this workspace; write the schedule\n" +
		"       yourself with `multica autopilot create`\n" +
		"  502  the model answered with a schedule the cron parser rejects",
	Args:    exactArgs(1),
	RunE:    runAutopilotDraft,
	Aliases: []string{"propose"},
}

func runAutopilotDraft(cmd *cobra.Command, args []string) error {
	text := strings.TrimSpace(args[0])
	if text == "" {
		return fmt.Errorf("say what should happen and when, e.g. multica autopilot draft \"every Monday at 9, list the open tickets\"")
	}
	create, _ := cmd.Flags().GetBool("create")
	activate, _ := cmd.Flags().GetBool("activate")
	// Activating is a decision, so it implies filing: there is nothing to
	// turn on otherwise.
	create = create || activate
	agent, _ := cmd.Flags().GetString("agent")
	if create && strings.TrimSpace(agent) == "" {
		return fmt.Errorf("--agent <name-or-id> is required with --create: an autopilot runs as an agent")
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

	timezone, _ := cmd.Flags().GetString("timezone")
	output, _ := cmd.Flags().GetString("output")

	if !create {
		var resp struct {
			Draft map[string]any `json:"draft"`
		}
		if err := client.PostJSON(ctx, "/api/autopilots/draft", map[string]any{"text": text, "timezone": timezone}, &resp); err != nil {
			return autopilotDraftError("draft autopilot", err)
		}
		if output == "json" {
			return cli.PrintJSON(os.Stdout, resp)
		}
		printAutopilotDraft(resp.Draft)
		fmt.Fprintln(os.Stdout, "\nNothing was created. Re-run with --create --agent <name> to file it paused.")
		return nil
	}

	agentID, err := resolveAgent(ctx, client, agent)
	if err != nil {
		return fmt.Errorf("resolve agent: %w", err)
	}
	body := map[string]any{"text": text, "timezone": timezone, "assignee_id": agentID, "activate": activate}
	if project, _ := cmd.Flags().GetString("project"); strings.TrimSpace(project) != "" {
		body["project_id"] = project
	}
	if issue, _ := cmd.Flags().GetString("issue"); strings.TrimSpace(issue) != "" {
		issueRef, err := resolveIssueRef(ctx, client, issue)
		if err != nil {
			return fmt.Errorf("resolve issue: %w", err)
		}
		body["issue_id"] = issueRef.ID
	}

	var resp struct {
		Autopilot  map[string]any `json:"autopilot"`
		DecisionID *string        `json:"decision_id"`
		NextRuns   []string       `json:"next_runs"`
	}
	if err := client.PostJSON(ctx, "/api/autopilots/propose", body, &resp); err != nil {
		return autopilotDraftError("propose autopilot", err)
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "Proposed autopilot %s — %s (%s)\n",
		strVal(resp.Autopilot, "id"), strVal(resp.Autopilot, "title"), strVal(resp.Autopilot, "status"))
	if len(resp.NextRuns) > 0 {
		fmt.Fprintf(os.Stdout, "Next runs: %s\n", strings.Join(resp.NextRuns, ", "))
	}
	if activate {
		fmt.Fprintln(os.Stdout, "It is active and will run on that schedule.")
		return nil
	}
	if resp.DecisionID != nil && *resp.DecisionID != "" {
		fmt.Fprintf(os.Stdout, "Decision Card %s asks someone to activate or discard it.\n", *resp.DecisionID)
		return nil
	}
	// The proposal's trigger is created disabled, so status alone does not
	// start it: without a card to answer, both halves are enabled by hand.
	id := strVal(resp.Autopilot, "id")
	fmt.Fprintf(os.Stdout, "It is paused and will not run. Activate it with:\n"+
		"  multica autopilot update %s --status active\n"+
		"  multica autopilot trigger-update %s <trigger-id> --enabled   # trigger-list shows the id\n", id, id)
	return nil
}

func printAutopilotDraft(d map[string]any) {
	fmt.Fprintf(os.Stdout, "Title:    %s\n", strVal(d, "title"))
	fmt.Fprintf(os.Stdout, "Schedule: %s (%s)\n", strVal(d, "cron_expression"), strVal(d, "timezone"))
	fmt.Fprintf(os.Stdout, "Mode:     %s\n", strVal(d, "execution_mode"))
	if tmpl := strVal(d, "issue_title_template"); tmpl != "" {
		fmt.Fprintf(os.Stdout, "Issue:    %s\n", tmpl)
	}
	if runs, ok := d["next_runs"].([]any); ok && len(runs) > 0 {
		parts := make([]string, 0, len(runs))
		for _, r := range runs {
			if s, ok := r.(string); ok {
				parts = append(parts, s)
			}
		}
		fmt.Fprintf(os.Stdout, "Next:     %s\n", strings.Join(parts, ", "))
	}
	if reason := strVal(d, "reason"); reason != "" {
		fmt.Fprintf(os.Stdout, "Read as:  %s\n", reason)
	}
	if model := strVal(d, "model"); model != "" {
		fmt.Fprintf(os.Stdout, "Model:    %s\n", model)
	}
	fmt.Fprintf(os.Stdout, "\nPrompt:\n%s\n", strVal(d, "description"))
}

// autopilotDraftError turns the two answers that are configuration, not
// failure, into sentences a caller can act on.
func autopilotDraftError(what string, err error) error {
	var httpErr *cli.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusServiceUnavailable:
			return fmt.Errorf("no model configured for this workspace: drafting a schedule needs an assist-layer LLM. Write it yourself with `multica autopilot create --title ... --cron ...`")
		case http.StatusBadGateway:
			return fmt.Errorf("the model did not produce a usable schedule: %s", serverErrorMessage(httpErr))
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

func init() {
	autopilotCmd.AddCommand(autopilotDraftCmd)

	autopilotDraftCmd.Flags().String("timezone", "", "IANA timezone for the schedule (default: the one the sentence names, else UTC)")
	autopilotDraftCmd.Flags().Bool("create", false, "File the drafted autopilot, paused, instead of only printing it")
	autopilotDraftCmd.Flags().Bool("activate", false, "File it running instead of paused, with no Decision Card (implies --create; members only)")
	autopilotDraftCmd.Flags().String("agent", "", "Agent name or ID that will run it (required with --create)")
	autopilotDraftCmd.Flags().String("project", "", "Project ID the autopilot belongs to")
	autopilotDraftCmd.Flags().String("issue", "", "Issue to file the activation Decision Card on")
	autopilotDraftCmd.Flags().String("output", "table", "Output format: table or json")
}
