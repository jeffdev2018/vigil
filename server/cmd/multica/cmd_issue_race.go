package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// multica issue race — racing attempts (F11 / JEF-6). Several agents, or the
// same agent on several models, work the same issue at once; a human reads the
// diffs and keeps one. Settling cancels every other open attempt, which is what
// makes the daemon drop their branch and worktree.

var issueRaceCmd = &cobra.Command{
	Use:   "race",
	Short: "Run several attempts on one issue and keep one",
}

var issueRaceStartCmd = &cobra.Command{
	Use:   "start <issue>",
	Short: "Queue 2 to 5 concurrent attempts on an issue",
	Long: "Queue several independent attempts on the same issue.\n\n" +
		"Each --attempt is an agent, optionally with the model it should run:\n" +
		"  --attempt \"Backend Dev\" --attempt \"Backend Dev:opus\"\n" +
		"runs the same agent twice on two models. Between 2 and 5 attempts, and\n" +
		"one open race per issue: settle or abandon the current one first.\n\n" +
		"Attempts are ordinary runs, so `issue runs` shows them and they cost what\n" +
		"they cost — a race of four is four runs.",
	Args: exactArgs(1),
	RunE: runIssueRaceStart,
}

var issueRaceListCmd = &cobra.Command{
	Use:   "list <issue>",
	Short: "List the races on an issue and their attempts",
	Args:  exactArgs(1),
	RunE:  runIssueRaceList,
}

var issueRaceSettleCmd = &cobra.Command{
	Use:   "settle <issue>",
	Short: "Keep one attempt and cancel the others",
	Long: "Designate the winning run of a race. The winner is left exactly as it\n" +
		"is; every other attempt still open is cancelled, so the daemon cleans up\n" +
		"its branch and worktree. Settling twice is refused, not replayed.",
	Args: exactArgs(1),
	RunE: runIssueRaceSettle,
}

var issueRaceAbandonCmd = &cobra.Command{
	Use:   "abandon <issue>",
	Short: "Drop a race without keeping any attempt",
	Args:  exactArgs(1),
	RunE:  runIssueRaceAbandon,
}

// parseRaceAttempt splits "<agent>[:<model>]" into its two halves. The agent
// may be a UUID, which contains no colon, so the split is unambiguous.
func parseRaceAttempt(raw string) (agent, model string, err error) {
	agent, model, _ = strings.Cut(strings.TrimSpace(raw), ":")
	agent = strings.TrimSpace(agent)
	model = strings.TrimSpace(model)
	if agent == "" {
		return "", "", fmt.Errorf("--attempt %q has no agent; write <agent>[:<model>]", raw)
	}
	return agent, model, nil
}

func runIssueRaceStart(cmd *cobra.Command, args []string) error {
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
	raw, _ := cmd.Flags().GetStringArray("attempt")
	if len(raw) == 0 {
		return fmt.Errorf("at least two --attempt flags are required")
	}
	attempts := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		agentInput, model, err := parseRaceAttempt(entry)
		if err != nil {
			return err
		}
		agentID, err := resolveAgent(ctx, client, agentInput)
		if err != nil {
			return fmt.Errorf("resolve agent: %w", err)
		}
		attempts = append(attempts, map[string]any{"agent_id": agentID, "model": model})
	}
	note, _ := cmd.Flags().GetString("note")

	var out map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/run-groups", map[string]any{"attempts": attempts, "note": note}, &out); err != nil {
		return fmt.Errorf("start race: %w", err)
	}
	return printRaceGroup(cmd, ctx, client, out)
}

func runIssueRaceList(cmd *cobra.Command, args []string) error {
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
	groups, err := fetchRaceGroups(ctx, client, issueRef.ID)
	if err != nil {
		return err
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, groups)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"RACE", "RACE STATUS", "RUN", "AGENT", "MODEL", "RUN STATUS", "DIFF"}
	rows := make([][]string, 0)
	for _, g := range groups {
		winner := strVal(g, "winner_task_id")
		for _, a := range mapSlice(g["attempts"]) {
			mark := ""
			if winner != "" && strVal(a, "task_id") == winner {
				mark = " *"
			}
			rows = append(rows, []string{
				displayID(strVal(g, "id"), fullID),
				strVal(g, "status"),
				displayID(strVal(a, "task_id"), fullID) + mark,
				actors.agent(strVal(a, "agent_id")),
				strVal(a, "model"),
				strVal(a, "status"),
				raceDiffLabel(a),
			})
		}
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueRaceSettle(cmd *cobra.Command, args []string) error {
	client, ctx, cancel, groupID, err := raceGroupFromFlags(cmd, args[0])
	if err != nil {
		return err
	}
	defer cancel()

	winnerInput, _ := cmd.Flags().GetString("winner")
	if strings.TrimSpace(winnerInput) == "" {
		return fmt.Errorf("--winner is required; take the run id from `multica issue race list`")
	}
	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	winnerRef, err := resolveTaskRunID(ctx, client, issueRef.ID, winnerInput)
	if err != nil {
		return fmt.Errorf("resolve winning run: %w", err)
	}

	var out map[string]any
	path := "/api/run-groups/" + url.PathEscape(groupID) + "/settle"
	if err := client.PostJSON(ctx, path, map[string]any{"winner_task_id": winnerRef.ID}, &out); err != nil {
		return fmt.Errorf("settle race: %w", err)
	}
	return printRaceGroup(cmd, ctx, client, out)
}

func runIssueRaceAbandon(cmd *cobra.Command, args []string) error {
	client, ctx, cancel, groupID, err := raceGroupFromFlags(cmd, args[0])
	if err != nil {
		return err
	}
	defer cancel()

	var out map[string]any
	path := "/api/run-groups/" + url.PathEscape(groupID) + "/abandon"
	if err := client.PostJSON(ctx, path, map[string]any{}, &out); err != nil {
		return fmt.Errorf("abandon race: %w", err)
	}
	return printRaceGroup(cmd, ctx, client, out)
}

// raceGroupFromFlags resolves --group against the issue's races, accepting the
// short id `race list` prints as well as the full UUID.
func raceGroupFromFlags(cmd *cobra.Command, issueInput string) (*cli.APIClient, context.Context, context.CancelFunc, string, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, nil, nil, "", err
	}
	ctx, cancel := cli.APIContext(context.Background())

	group, _ := cmd.Flags().GetString("group")
	group = strings.TrimSpace(group)
	if group == "" {
		cancel()
		return nil, nil, nil, "", fmt.Errorf("--group is required; take the race id from `multica issue race list`")
	}
	if uuidRegexp.MatchString(group) {
		return client, ctx, cancel, group, nil
	}
	issueRef, err := resolveIssueRef(ctx, client, issueInput)
	if err != nil {
		cancel()
		return nil, nil, nil, "", fmt.Errorf("resolve issue: %w", err)
	}
	groups, err := fetchRaceGroups(ctx, client, issueRef.ID)
	if err != nil {
		cancel()
		return nil, nil, nil, "", err
	}
	matches := make([]string, 0, 1)
	for _, g := range groups {
		if id := strVal(g, "id"); strings.HasPrefix(id, group) {
			matches = append(matches, id)
		}
	}
	if len(matches) != 1 {
		cancel()
		return nil, nil, nil, "", fmt.Errorf("--group %q matches %d races on this issue; give the full id", group, len(matches))
	}
	return client, ctx, cancel, matches[0], nil
}

func fetchRaceGroups(ctx context.Context, client *cli.APIClient, issueID string) ([]map[string]any, error) {
	var envelope struct {
		Groups []map[string]any `json:"groups"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+issueID+"/run-groups", &envelope); err != nil {
		return nil, fmt.Errorf("list races: %w", err)
	}
	return envelope.Groups, nil
}

// raceDiffLabel says what there is to compare: the stat when it is stored, and
// whether the unified patch was dropped for size.
func raceDiffLabel(attempt map[string]any) string {
	if attempt["diff_stat"] == nil {
		return "-"
	}
	if truncated, ok := attempt["diff_truncated"].(bool); ok && truncated {
		return "stat only (too large)"
	}
	return "stat + patch"
}

func mapSlice(v any) []map[string]any {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func printRaceGroup(cmd *cobra.Command, ctx context.Context, client *cli.APIClient, envelope map[string]any) error {
	group, _ := envelope["group"].(map[string]any)
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, group)
	}
	if group == nil {
		fmt.Fprintln(os.Stdout, "no race returned")
		return nil
	}
	actors := loadActorDisplayLookup(ctx, client)
	fmt.Fprintf(os.Stdout, "Race %s is %s\n", strVal(group, "id"), strVal(group, "status"))
	winner := strVal(group, "winner_task_id")
	for _, a := range mapSlice(group["attempts"]) {
		mark := "  "
		if winner != "" && strVal(a, "task_id") == winner {
			mark = "* "
		}
		model := strVal(a, "model")
		if model == "" {
			model = "default model"
		}
		fmt.Fprintf(os.Stdout, "%s%s  %s (%s) -> %s\n", mark, strVal(a, "task_id"), actors.agent(strVal(a, "agent_id")), model, strVal(a, "status"))
	}
	return nil
}

func init() {
	issueRaceStartCmd.Flags().StringArray("attempt", nil, "An attempt, written as <agent>[:<model>] (repeatable, 2 to 5). Agent is a name or UUID; the model is optional and defaults to the agent's own.")
	issueRaceStartCmd.Flags().String("note", "", "Instruction handed to every attempt")
	issueRaceStartCmd.Flags().String("output", "table", "Output format: table or json")

	issueRaceListCmd.Flags().String("output", "table", "Output format: table or json")
	issueRaceListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	issueRaceSettleCmd.Flags().String("group", "", "Race id (full UUID, or the short id `race list` prints). Required.")
	issueRaceSettleCmd.Flags().String("winner", "", "Run id of the attempt to keep. Required.")
	issueRaceSettleCmd.Flags().String("output", "table", "Output format: table or json")

	issueRaceAbandonCmd.Flags().String("group", "", "Race id (full UUID, or the short id `race list` prints). Required.")
	issueRaceAbandonCmd.Flags().String("output", "table", "Output format: table or json")

	issueRaceCmd.AddCommand(issueRaceStartCmd, issueRaceListCmd, issueRaceSettleCmd, issueRaceAbandonCmd)
	issueCmd.AddCommand(issueRaceCmd)
}
