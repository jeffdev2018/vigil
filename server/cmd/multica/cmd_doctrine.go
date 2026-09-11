package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Workspace doctrine (OS plan, chantier 22) from the terminal: read the
// governing text, publish a revision through the ledger, read the history,
// diff two revisions, review a proposal, restore an earlier one, and file or
// resolve the reports agents leave when a task collides with a rule.

const doctrinePath = "/api/workspace/doctrine"

var doctrineCmd = &cobra.Command{
	Use:   "doctrine",
	Short: "The workspace doctrine: the standing rules every agent is bound by",
	Long: "The doctrine is the one governing document a workspace's owners write for every agent. " +
		"It is injected into every brief and it outranks issue content, comments and notes.\n\n" +
		"Read it with `show`, publish a revision with `publish`, follow the ledger with `history`, " +
		"`diff` and `restore`, and answer what agents report with `reports`, `acknowledge` and `dismiss`.",
}

var doctrineShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the live doctrine and its revision",
	Args:  exactArgs(0),
	RunE:  runDoctrineShow,
}

var doctrinePublishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish a new doctrine revision from a file or stdin",
	Long: "Publish the doctrine. --expected-revision is the revision you read (from `doctrine show`); " +
		"the server refuses the write when the doctrine moved under you.\n\n" +
		"When the workspace requires a second reviewer and has one, the text is filed as a proposal " +
		"instead and another owner or admin approves it.",
	Args: exactArgs(0),
	RunE: runDoctrinePublish,
}

var doctrineHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "List the doctrine's revision ledger, newest first",
	Args:  exactArgs(0),
	RunE:  runDoctrineHistory,
}

var doctrineDiffCmd = &cobra.Command{
	Use:   "diff <version-id>",
	Short: "Compare a version with another one, or with what came before it",
	Args:  exactArgs(1),
	RunE:  runDoctrineDiff,
}

var doctrineApproveCmd = &cobra.Command{
	Use:   "approve <version-id>",
	Short: "Approve a proposed revision; it becomes the live doctrine",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDoctrineReview(cmd, args, "approve") },
}

var doctrineRejectCmd = &cobra.Command{
	Use:   "reject <version-id>",
	Short: "Reject a proposed revision; the live doctrine does not move",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDoctrineReview(cmd, args, "reject") },
}

var doctrineRestoreCmd = &cobra.Command{
	Use:   "restore <version-id>",
	Short: "Publish an earlier revision's text as a new revision",
	Args:  exactArgs(1),
	RunE:  runDoctrineRestore,
}

var doctrineReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Report a rule you cannot follow, two rules in conflict, or a rule too vague to apply",
	Long: "File a doctrine report against the revision you are running under. " +
		"It reaches the workspace owners' inbox; it does not change the doctrine.\n\n" +
		"conflict: two rules cannot both be followed. refusal: the task cannot be done without breaking a rule. " +
		"ambiguity: the rule does not say enough to act on.",
	Args: exactArgs(0),
	RunE: runDoctrineReport,
}

var doctrineReportsCmd = &cobra.Command{
	Use:   "reports",
	Short: "List doctrine reports, open ones by default",
	Args:  exactArgs(0),
	RunE:  runDoctrineReports,
}

var doctrineAcknowledgeCmd = &cobra.Command{
	Use:   "acknowledge <report-id>",
	Short: "Acknowledge a report: it is real and it is being dealt with",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDoctrineResolve(cmd, args, "acknowledge") },
}

var doctrineDismissCmd = &cobra.Command{
	Use:   "dismiss <report-id>",
	Short: "Dismiss a report: the doctrine stands as written",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runDoctrineResolve(cmd, args, "dismiss") },
}

func init() {
	// --output is per-command in this CLI (the root has no persistent one).
	for _, c := range []*cobra.Command{doctrineShowCmd, doctrinePublishCmd, doctrineHistoryCmd, doctrineDiffCmd,
		doctrineApproveCmd, doctrineRejectCmd, doctrineRestoreCmd,
		doctrineReportCmd, doctrineReportsCmd, doctrineAcknowledgeCmd, doctrineDismissCmd} {
		c.Flags().String("output", "table", "Output format: table or json")
	}
	doctrinePublishCmd.Flags().String("file", "", "read the doctrine from this file")
	doctrinePublishCmd.Flags().Bool("stdin", false, "read the doctrine from stdin")
	doctrinePublishCmd.Flags().Int("expected-revision", -1, "the revision you read (required)")
	doctrinePublishCmd.Flags().String("note", "", "why this revision changes what it changes")
	doctrineHistoryCmd.Flags().Int("limit", 20, "rows to read (max 100)")
	doctrineHistoryCmd.Flags().String("cursor", "", "page cursor from a previous read")
	doctrineHistoryCmd.Flags().Bool("full-id", false, "print full version ids")
	doctrineDiffCmd.Flags().String("against", "", "compare with this version id (default: what came before)")
	doctrineApproveCmd.Flags().String("note", "", "what the reviewer wants recorded")
	doctrineRejectCmd.Flags().String("note", "", "what the reviewer wants recorded")
	doctrineRestoreCmd.Flags().Int("expected-revision", -1, "the revision you read (required)")
	doctrineRestoreCmd.Flags().String("note", "", "why the earlier text is coming back")
	doctrineReportCmd.Flags().String("kind", "", "conflict, refusal or ambiguity")
	doctrineReportCmd.Flags().String("summary", "", "what collided, in your own words")
	doctrineReportCmd.Flags().String("passage", "", "the doctrine passage at stake, quoted")
	doctrineReportCmd.Flags().String("issue", "", "issue id or identifier the collision happened on")
	doctrineReportsCmd.Flags().String("status", "open", "open, acknowledged, dismissed or all")
	doctrineReportsCmd.Flags().Bool("full-id", false, "print full report ids")
	doctrineAcknowledgeCmd.Flags().String("note", "", "what was decided")
	doctrineDismissCmd.Flags().String("note", "", "what was decided")
	doctrineCmd.AddCommand(doctrineShowCmd, doctrinePublishCmd, doctrineHistoryCmd, doctrineDiffCmd,
		doctrineApproveCmd, doctrineRejectCmd, doctrineRestoreCmd,
		doctrineReportCmd, doctrineReportsCmd, doctrineAcknowledgeCmd, doctrineDismissCmd)
}

// doctrineInt reads a JSON number field as an int.
func doctrineInt(m map[string]any, key string) int { return int(floatVal(m, key)) }

func jsonOutput(cmd *cobra.Command) bool {
	output, _ := cmd.Flags().GetString("output")
	return output == "json"
}

// requireJSONOutput is for commands that only print JSON: the --output flag
// is still honoured in that any other value is refused instead of ignored.
func requireJSONOutput(cmd *cobra.Command) error {
	if output, _ := cmd.Flags().GetString("output"); output != "" && output != "json" {
		return fmt.Errorf("--output %q is not supported by this command (only json)", output)
	}
	return nil
}

func runDoctrineShow(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var resp map[string]any
	if err := client.GetJSON(cmd.Context(), doctrinePath, &resp); err != nil {
		return fmt.Errorf("read doctrine: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	content := strVal(resp, "content")
	revision := doctrineInt(resp, "revision")
	if content == "" {
		fmt.Fprintln(os.Stderr, "this workspace has no doctrine yet")
	} else {
		fmt.Fprintf(os.Stdout, "# doctrine revision %d (%d bytes of %d)\n\n%s\n", revision, len(content), doctrineInt(resp, "byte_limit"), content)
	}
	if pending, ok := resp["pending"].(map[string]any); ok {
		fmt.Fprintf(os.Stderr, "a revision is awaiting review: %s\n", strVal(pending, "id"))
	}
	if n := doctrineInt(resp, "open_reports"); n > 0 {
		fmt.Fprintf(os.Stderr, "%d open doctrine report(s): multica doctrine reports\n", n)
	}
	return nil
}

// readDoctrineText takes the new text from --file or --stdin, never both.
func readDoctrineText(cmd *cobra.Command) (string, error) {
	file, _ := cmd.Flags().GetString("file")
	fromStdin, _ := cmd.Flags().GetBool("stdin")
	switch {
	case file != "" && fromStdin:
		return "", fmt.Errorf("--file and --stdin cannot be combined")
	case file != "":
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", file, err)
		}
		return string(raw), nil
	case fromStdin:
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(raw), nil
	default:
		return "", fmt.Errorf("--file <path> or --stdin is required")
	}
}

func doctrineExpectedRevision(cmd *cobra.Command) (int, error) {
	revision, _ := cmd.Flags().GetInt("expected-revision")
	if revision < 0 {
		return 0, fmt.Errorf("--expected-revision is required; read it from `multica doctrine show`")
	}
	return revision, nil
}

// printDoctrinePublication reports whether the write went live or is held for
// a reviewer. A pending version's `status` says which happened.
func printDoctrinePublication(cmd *cobra.Command, resp map[string]any) error {
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	version, _ := resp["version"].(map[string]any)
	doctrine, _ := resp["doctrine"].(map[string]any)
	if strVal(version, "status") == "pending" {
		fmt.Fprintf(os.Stdout, "%s\tpending review\n", strVal(version, "id"))
		fmt.Fprintln(os.Stderr, "held for a second reviewer: another owner or admin runs `multica doctrine approve`")
		return nil
	}
	fmt.Fprintf(os.Stdout, "%s\tpublished as revision %d\n", strVal(version, "id"), doctrineInt(doctrine, "revision"))
	return nil
}

func runDoctrinePublish(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	content, err := readDoctrineText(cmd)
	if err != nil {
		return err
	}
	revision, err := doctrineExpectedRevision(cmd)
	if err != nil {
		return err
	}
	note, _ := cmd.Flags().GetString("note")
	var resp map[string]any
	if err := client.PutJSON(cmd.Context(), doctrinePath, map[string]any{"content": content, "expected_revision": revision, "note": note}, &resp); err != nil {
		return fmt.Errorf("publish doctrine: %w", err)
	}
	return printDoctrinePublication(cmd, resp)
}

func runDoctrineHistory(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	params := []string{}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params = append(params, "limit="+strconv.Itoa(v))
	}
	if v, _ := cmd.Flags().GetString("cursor"); v != "" {
		params = append(params, "cursor="+v)
	}
	var resp struct {
		Versions   []map[string]any `json:"versions"`
		NextCursor string           `json:"next_cursor"`
	}
	path := doctrinePath + "/versions"
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}
	if err := client.GetJSON(cmd.Context(), path, &resp); err != nil {
		return fmt.Errorf("read doctrine history: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	rows := make([][]string, 0, len(resp.Versions))
	for _, v := range resp.Versions {
		revision := ""
		if r, ok := v["revision"].(float64); ok {
			revision = strconv.Itoa(int(r))
		}
		rows = append(rows, []string{
			displayID(strVal(v, "id"), fullID), revision, strVal(v, "status"),
			strconv.Itoa(doctrineInt(v, "bytes")), shortTime(strVal(v, "created_at")), strVal(v, "note"),
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "REV", "STATUS", "BYTES", "CREATED", "NOTE"}, rows)
	if resp.NextCursor != "" {
		fmt.Fprintf(os.Stderr, "more: --cursor %s\n", resp.NextCursor)
	}
	return nil
}

func runDoctrineDiff(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	path := doctrinePath + "/versions/" + args[0] + "/diff"
	if against, _ := cmd.Flags().GetString("against"); against != "" {
		path += "?against=" + against
	}
	var resp struct {
		From    map[string]any   `json:"from"`
		To      map[string]any   `json:"to"`
		Lines   []map[string]any `json:"lines"`
		Added   int              `json:"added"`
		Removed int              `json:"removed"`
	}
	if err := client.GetJSON(cmd.Context(), path, &resp); err != nil {
		return fmt.Errorf("diff doctrine: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "--- %s\n+++ %s\n", doctrineDiffLabel(resp.From), doctrineDiffLabel(resp.To))
	for _, line := range resp.Lines {
		prefix := " "
		switch strVal(line, "kind") {
		case "add":
			prefix = "+"
		case "del":
			prefix = "-"
		}
		fmt.Fprintln(os.Stdout, prefix+strVal(line, "text"))
	}
	fmt.Fprintf(os.Stderr, "%d added, %d removed\n", resp.Added, resp.Removed)
	return nil
}

func doctrineDiffLabel(v map[string]any) string {
	if v == nil {
		return "(nothing)"
	}
	if r, ok := v["revision"].(float64); ok {
		return fmt.Sprintf("revision %d", int(r))
	}
	return strVal(v, "status") + " " + strVal(v, "id")
}

func runDoctrineReview(cmd *cobra.Command, args []string, action string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	note, _ := cmd.Flags().GetString("note")
	var resp map[string]any
	if err := client.PostJSON(cmd.Context(), doctrinePath+"/versions/"+args[0]+"/"+action, map[string]any{"note": note}, &resp); err != nil {
		return fmt.Errorf("%s doctrine version: %w", action, err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	version, _ := resp["version"].(map[string]any)
	doctrine, _ := resp["doctrine"].(map[string]any)
	if action == "approve" {
		fmt.Fprintf(os.Stdout, "%s\tlive as revision %d\n", strVal(version, "id"), doctrineInt(doctrine, "revision"))
		return nil
	}
	fmt.Fprintf(os.Stdout, "%s\trejected\n", strVal(version, "id"))
	return nil
}

func runDoctrineRestore(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	revision, err := doctrineExpectedRevision(cmd)
	if err != nil {
		return err
	}
	note, _ := cmd.Flags().GetString("note")
	var resp map[string]any
	if err := client.PostJSON(cmd.Context(), doctrinePath+"/versions/"+args[0]+"/restore", map[string]any{"expected_revision": revision, "note": note}, &resp); err != nil {
		return fmt.Errorf("restore doctrine version: %w", err)
	}
	return printDoctrinePublication(cmd, resp)
}

func runDoctrineReport(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	kind, _ := cmd.Flags().GetString("kind")
	summary, _ := cmd.Flags().GetString("summary")
	if kind != "conflict" && kind != "refusal" && kind != "ambiguity" {
		return fmt.Errorf("--kind must be conflict, refusal or ambiguity")
	}
	if strings.TrimSpace(summary) == "" {
		return fmt.Errorf("--summary is required")
	}
	body := map[string]any{"kind": kind, "summary": summary}
	if v, _ := cmd.Flags().GetString("passage"); v != "" {
		body["passage"] = v
	}
	if v, _ := cmd.Flags().GetString("issue"); v != "" {
		issueRef, err := resolveIssueRef(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve issue: %w", err)
		}
		body["issue_id"] = issueRef.ID
	}
	var resp map[string]any
	if err := client.PostJSON(ctx, doctrinePath+"/reports", body, &resp); err != nil {
		return fmt.Errorf("file doctrine report: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	report, _ := resp["report"].(map[string]any)
	fmt.Fprintf(os.Stdout, "%s\t%s\tagainst revision %d\n", strVal(report, "id"), strVal(report, "kind"), doctrineInt(report, "doctrine_revision"))
	fmt.Fprintln(os.Stderr, "filed: the workspace owners decide; the doctrine has not changed")
	return nil
}

func runDoctrineReports(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	status, _ := cmd.Flags().GetString("status")
	var resp struct {
		Reports []map[string]any `json:"reports"`
	}
	if err := client.GetJSON(cmd.Context(), doctrinePath+"/reports?status="+status, &resp); err != nil {
		return fmt.Errorf("read doctrine reports: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	rows := make([][]string, 0, len(resp.Reports))
	for _, x := range resp.Reports {
		rows = append(rows, []string{
			displayID(strVal(x, "id"), fullID), strVal(x, "kind"), strconv.Itoa(doctrineInt(x, "doctrine_revision")),
			strVal(x, "reporter_type"), strVal(x, "status"), shortTime(strVal(x, "created_at")), strVal(x, "summary"),
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "KIND", "REV", "REPORTER", "STATUS", "CREATED", "SUMMARY"}, rows)
	return nil
}

func runDoctrineResolve(cmd *cobra.Command, args []string, action string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	note, _ := cmd.Flags().GetString("note")
	var resp map[string]any
	if err := client.PostJSON(cmd.Context(), doctrinePath+"/reports/"+args[0]+"/"+action, map[string]any{"note": note}, &resp); err != nil {
		return fmt.Errorf("%s doctrine report: %w", action, err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	report, _ := resp["report"].(map[string]any)
	fmt.Fprintf(os.Stdout, "%s\t%s\n", strVal(report, "id"), strVal(report, "status"))
	return nil
}
