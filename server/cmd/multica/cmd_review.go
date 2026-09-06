package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// multica review flag {add|list} — Review flags by severity (F06 / JEF-19):
// one structured finding per problem, anchored to a file and a line range of a
// linked pull request, so the reviewer reads a sorted list instead of prose.

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Record and read structured review findings on a pull request",
}

var reviewFlagCmd = &cobra.Command{
	Use:   "flag",
	Short: "Review flags: one finding, one file, one line range",
}

var reviewFlagAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Record one finding on a line range of a linked pull request",
	Long: `Records one review flag.

Severity is what a reviewer sorts by: bug (this is wrong), warning (this is
risky), info (worth knowing). Confidence is optional and only orders flags of
the same severity — omit it rather than guessing a number.

One flag per finding. --body - reads the long form from stdin.`,
	Args: cobra.NoArgs,
	RunE: runReviewFlagAdd,
}

var reviewFlagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an issue's review flags, most severe first",
	Args:  cobra.NoArgs,
	RunE:  runReviewFlagList,
}

func init() {
	reviewFlagAddCmd.Flags().String("issue", "", "Issue ID or key the pull request is linked to (required)")
	reviewFlagAddCmd.Flags().String("pr", "", "Pull request UUID from the issue's pull-requests listing (required)")
	reviewFlagAddCmd.Flags().String("sha", "", "Head commit the line range belongs to (default: the PR's current head)")
	reviewFlagAddCmd.Flags().String("file", "", "Repository-relative path of the file (required)")
	reviewFlagAddCmd.Flags().String("line", "", "Line or range in the file: 42 or 42-48 (required)")
	reviewFlagAddCmd.Flags().String("side", "new", "Diff side the range is on: new or old")
	reviewFlagAddCmd.Flags().String("severity", "", "bug | warning | info (required)")
	reviewFlagAddCmd.Flags().Int("confidence", -1, "How sure you are, 0-100. Omit when you would be guessing")
	reviewFlagAddCmd.Flags().String("title", "", "One line naming the problem (required)")
	reviewFlagAddCmd.Flags().String("body", "", "The detail. Use - to read it from stdin")

	reviewFlagListCmd.Flags().String("issue", "", "Issue ID or key (required)")
	reviewFlagListCmd.Flags().String("state", "open", "open (default) or all")

	reviewFlagCmd.AddCommand(reviewFlagAddCmd, reviewFlagListCmd)
	reviewCmd.AddCommand(reviewFlagCmd)
	rootCmd.AddCommand(reviewCmd)
}

// parseLineRange reads the --line flag. "42" is the one-line case and the
// common one; "42-48" spans a hunk. Anything else is refused here rather than
// silently narrowed to a line the finding is not about.
func parseLineRange(value string) (start, end int, err error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, 0, fmt.Errorf("--line is required (42 or 42-48)")
	}
	first, rest, hasRange := strings.Cut(trimmed, "-")
	start, err = strconv.Atoi(strings.TrimSpace(first))
	if err != nil || start < 1 {
		return 0, 0, fmt.Errorf("--line %q is not a line number (expected 42 or 42-48)", value)
	}
	if !hasRange {
		return start, start, nil
	}
	end, err = strconv.Atoi(strings.TrimSpace(rest))
	if err != nil || end < start {
		return 0, 0, fmt.Errorf("--line %q is not a valid range (expected 42-48 with the end after the start)", value)
	}
	return start, end, nil
}

// reviewFlagBody resolves --body, reading stdin for "-" so a multi-paragraph
// finding does not have to survive shell quoting.
func reviewFlagBody(value string) (string, error) {
	if strings.TrimSpace(value) != "-" {
		return value, nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin for --body: %w", err)
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

func runReviewFlagAdd(cmd *cobra.Command, _ []string) error {
	issueArg, _ := cmd.Flags().GetString("issue")
	prID, _ := cmd.Flags().GetString("pr")
	sha, _ := cmd.Flags().GetString("sha")
	file, _ := cmd.Flags().GetString("file")
	line, _ := cmd.Flags().GetString("line")
	side, _ := cmd.Flags().GetString("side")
	severity, _ := cmd.Flags().GetString("severity")
	confidence, _ := cmd.Flags().GetInt("confidence")
	title, _ := cmd.Flags().GetString("title")
	rawBody, _ := cmd.Flags().GetString("body")

	if strings.TrimSpace(prID) == "" {
		return fmt.Errorf("--pr is required (a pull request UUID from `multica issue pull-requests`)")
	}
	if strings.TrimSpace(file) == "" {
		return fmt.Errorf("--file is required")
	}
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "bug", "warning", "info":
	default:
		return fmt.Errorf("--severity must be one of bug, warning, info")
	}
	start, end, err := parseLineRange(line)
	if err != nil {
		return err
	}
	body, err := reviewFlagBody(rawBody)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"pr_id":      strings.TrimSpace(prID),
		"file_path":  strings.TrimSpace(file),
		"line_start": start,
		"line_end":   end,
		"side":       strings.ToLower(strings.TrimSpace(side)),
		"severity":   strings.ToLower(strings.TrimSpace(severity)),
		"title":      strings.TrimSpace(title),
		"body":       body,
	}
	if s := strings.TrimSpace(sha); s != "" {
		payload["head_sha"] = s
	}
	// The default is -1 rather than 0 so "did not say" stays distinguishable
	// from "0% sure", which is a real answer the server sorts differently.
	if cmd.Flags().Changed("confidence") {
		if confidence < 0 || confidence > 100 {
			return fmt.Errorf("--confidence must be between 0 and 100")
		}
		payload["confidence"] = confidence
	}

	return reviewFlagCall(cmd, issueArg, func(ctx context.Context, client *cli.APIClient, id string, out *map[string]any) error {
		return client.PostJSON(ctx, "/api/issues/"+id+"/review-flags", payload, out)
	})
}

func runReviewFlagList(cmd *cobra.Command, _ []string) error {
	issueArg, _ := cmd.Flags().GetString("issue")
	rawState, _ := cmd.Flags().GetString("state")
	state := strings.ToLower(strings.TrimSpace(rawState))
	switch state {
	case "", "open":
		state = "open"
	case "all":
	default:
		return fmt.Errorf("--state must be open or all")
	}
	return reviewFlagCall(cmd, issueArg, func(ctx context.Context, client *cli.APIClient, id string, out *map[string]any) error {
		return client.GetJSON(ctx, "/api/issues/"+id+"/review-flags?state="+url.QueryEscape(state), out)
	})
}

// reviewFlagCall resolves the issue ref once and prints the server's JSON,
// the same shape every other read-write CLI command emits.
func reviewFlagCall(cmd *cobra.Command, issueArg string, call func(ctx context.Context, client *cli.APIClient, issueID string, out *map[string]any) error) error {
	if strings.TrimSpace(issueArg) == "" {
		return fmt.Errorf("--issue is required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	issueRef, err := resolveIssueRef(ctx, client, issueArg)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	var result map[string]any
	if err := call(ctx, client, issueRef.ID, &result); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, result)
}
