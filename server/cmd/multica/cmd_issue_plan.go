package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// multica issue plan {get,set,report} — the plan artifact and the
// verification report (F17). `report` defaults its run id to MULTICA_TASK_ID,
// which every run exports, so a verification run needs no extra flag.

var issuePlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Read, publish and verify an issue's plan",
}

var issuePlanGetCmd = &cobra.Command{
	Use:   "get <issue-id>",
	Short: "Show the active plan and its version history",
	Args:  exactArgs(1),
	RunE:  runIssuePlanGet,
}

var issuePlanSetCmd = &cobra.Command{
	Use:   "set <issue-id>",
	Short: "Publish a new plan version from --file or --content",
	Args:  exactArgs(1),
	RunE:  runIssuePlanSet,
}

var issuePlanReportCmd = &cobra.Command{
	Use:   "report <issue-id>",
	Short: "Report verification findings for a run (JSON from --file or stdin)",
	Args:  exactArgs(1),
	RunE:  runIssuePlanReport,
}

func init() {
	issuePlanGetCmd.Flags().String("output", "json", "Output format: json")
	issuePlanSetCmd.Flags().String("file", "", "Markdown file holding the plan")
	issuePlanSetCmd.Flags().String("content", "", "Plan text (alternative to --file)")
	issuePlanSetCmd.Flags().String("steps-json", "", `Optional steps as JSON: [{"id":"s1","title":"..."}]`)
	issuePlanSetCmd.Flags().String("output", "json", "Output format: json")
	issuePlanReportCmd.Flags().String("run", "", "Verification run id (defaults to MULTICA_TASK_ID)")
	issuePlanReportCmd.Flags().String("file", "", "JSON report file ({summary, findings[]}); '-' or empty reads stdin")
	issuePlanReportCmd.Flags().String("output", "json", "Output format: json")
	issuePlanCmd.AddCommand(issuePlanGetCmd, issuePlanSetCmd, issuePlanReportCmd)
	issueCmd.AddCommand(issuePlanCmd)
}

func runIssuePlanGet(cmd *cobra.Command, args []string) error {
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
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/plan", &result); err != nil {
		return fmt.Errorf("get plan: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssuePlanSet(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	content, _ := cmd.Flags().GetString("content")
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read plan file: %w", err)
		}
		content = string(raw)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("--file or --content is required")
	}
	body := map[string]any{"content": content}
	if stepsJSON, _ := cmd.Flags().GetString("steps-json"); stepsJSON != "" {
		var steps []map[string]any
		if err := json.Unmarshal([]byte(stepsJSON), &steps); err != nil {
			return fmt.Errorf("--steps-json must be a JSON array: %w", err)
		}
		body["steps"] = steps
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
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID+"/plan", body, &result); err != nil {
		return fmt.Errorf("set plan: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssuePlanReport(cmd *cobra.Command, args []string) error {
	runID, _ := cmd.Flags().GetString("run")
	if runID == "" {
		runID = os.Getenv("MULTICA_TASK_ID")
	}
	if runID == "" {
		return fmt.Errorf("--run is required outside a run (MULTICA_TASK_ID is not set)")
	}
	file, _ := cmd.Flags().GetString("file")
	var raw []byte
	var err error
	if file == "" || file == "-" {
		raw, err = readAllStdin()
	} else {
		raw, err = os.ReadFile(file)
	}
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("report must be a JSON object with summary and findings: %w", err)
	}
	if _, ok := body["findings"]; !ok {
		return fmt.Errorf("report is missing the findings array")
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
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/plan/verifications/"+runID, body, &result); err != nil {
		return fmt.Errorf("report verification: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func readAllStdin() ([]byte, error) {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
	}
	return []byte(b.String()), nil
}
