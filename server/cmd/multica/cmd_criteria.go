package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// multica criteria {list|set|prove} — Outcome Contract (K12): the issue's
// acceptance criteria and the proof behind each one.

var criteriaCmd = &cobra.Command{
	Use:   "criteria",
	Short: "Read, set and prove an issue's acceptance criteria",
}

var criteriaListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List the acceptance criteria with their proof state",
	Args:  exactArgs(1),
	RunE:  runCriteriaList,
}

var criteriaSetCmd = &cobra.Command{
	Use:   "set <issue-id>",
	Short: "Replace the acceptance criteria (one --text per criterion)",
	Long: `Replaces the issue's acceptance criteria. A criterion whose text is
unchanged keeps its proof; a reworded one starts without proof.`,
	Args: exactArgs(1),
	RunE: runCriteriaSet,
}

var criteriaProveCmd = &cobra.Command{
	Use:   "prove <issue-id> <criterion-id>",
	Short: "Attach a proof to one criterion",
	Long: `Attaches a proof: --type test|file|screenshot|url with --ref naming it
(a test command, a path, an URL), or --type human_validation to ask the human
to validate it themselves. The issue cannot move to done while a criterion
lacks a satisfied proof.`,
	Args: exactArgs(2),
	RunE: runCriteriaProve,
}

func init() {
	criteriaSetCmd.Flags().StringArray("text", nil, "Criterion text (repeatable, in order)")
	criteriaProveCmd.Flags().String("type", "", "test | file | screenshot | url | human_validation (required)")
	criteriaProveCmd.Flags().String("ref", "", "What proves it: a test command, a path or an URL")
	criteriaCmd.AddCommand(criteriaListCmd, criteriaSetCmd, criteriaProveCmd)
	rootCmd.AddCommand(criteriaCmd)
}

func criteriaCall(cmd *cobra.Command, issueArg string, call func(ctx context.Context, client *cli.APIClient, issueID string, out *map[string]any) error) error {
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

func runCriteriaList(cmd *cobra.Command, args []string) error {
	return criteriaCall(cmd, args[0], func(ctx context.Context, client *cli.APIClient, id string, out *map[string]any) error {
		return client.GetJSON(ctx, "/api/issues/"+id+"/acceptance-criteria", out)
	})
}

func runCriteriaSet(cmd *cobra.Command, args []string) error {
	texts, _ := cmd.Flags().GetStringArray("text")
	criteria := make([]map[string]string, 0, len(texts))
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("--text must not be empty")
		}
		criteria = append(criteria, map[string]string{"text": strings.TrimSpace(text)})
	}
	return criteriaCall(cmd, args[0], func(ctx context.Context, client *cli.APIClient, id string, out *map[string]any) error {
		return client.PutJSON(ctx, "/api/issues/"+id+"/acceptance-criteria", map[string]any{"criteria": criteria}, out)
	})
}

func runCriteriaProve(cmd *cobra.Command, args []string) error {
	proofType, _ := cmd.Flags().GetString("type")
	ref, _ := cmd.Flags().GetString("ref")
	if strings.TrimSpace(proofType) == "" {
		return fmt.Errorf("--type is required")
	}
	body := map[string]any{"proof_type": strings.TrimSpace(proofType), "proof_ref": strings.TrimSpace(ref)}
	return criteriaCall(cmd, args[0], func(ctx context.Context, client *cli.APIClient, id string, out *map[string]any) error {
		return client.PatchJSON(ctx, "/api/issues/"+id+"/acceptance-criteria/"+args[1]+"/proof", body, out)
	})
}
