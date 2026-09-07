package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/memoryeval"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/spf13/cobra"
)

var agentMemoryCmd = &cobra.Command{Use: "memory", Short: "Compare a candidate memory offline, then adopt or restore it as a human"}

func memorySnapshot(ctx context.Context, client *cli.APIClient, agent, id string) (memoryeval.Memory, []memoryeval.Memory, error) {
	var memories []memoryeval.Memory
	if err := client.GetJSON(ctx, "/api/agents/"+url.PathEscape(agent)+"/memories", &memories); err != nil {
		return memoryeval.Memory{}, nil, err
	}
	var candidate memoryeval.Memory
	baseline := []memoryeval.Memory{}
	for _, m := range memories {
		if m.ID == id {
			candidate = m
			continue
		}
		if m.Status == "active" && !m.Expired && (m.ExpiresAt == nil || m.ExpiresAt.After(time.Now())) {
			baseline = append(baseline, m)
		}
	}
	if candidate.ID == "" || candidate.AgentID == "" || candidate.WorkspaceID == "" || candidate.Revision < 1 || candidate.Content == "" {
		return candidate, nil, errors.New("candidate memory not found or malformed")
	}
	if candidate.Status != "pending" || candidate.Expired || (candidate.ExpiresAt != nil && !candidate.ExpiresAt.After(time.Now())) {
		return candidate, nil, errors.New("candidate must be pending and unexpired")
	}
	sort.Slice(baseline, func(i, j int) bool { return baseline[i].ID < baseline[j].ID })
	return candidate, baseline, nil
}

var agentMemoryEvaluateCmd = &cobra.Command{
	Use: "evaluate <agent-id> <memory-id>", Short: "Run sequential offline baseline/candidate cases and retain a private report", Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		suitePath, _ := cmd.Flags().GetString("suite")
		output, _ := cmd.Flags().GetString("output")
		var suite memoryeval.Suite
		if err := memoryeval.ReadJSON(suitePath, &suite); err != nil {
			return err
		}
		if err := suite.Validate(); err != nil {
			return err
		}
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		apiCtx, cancel := cli.APIContext(cmd.Context())
		candidate, baseline, err := memorySnapshot(apiCtx, client, args[0], args[1])
		cancel()
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()
		report, err := memoryeval.Run(ctx, memoryeval.Report{ServerURL: strings.TrimRight(client.BaseURL, "/"), Candidate: candidate, Baseline: baseline, Suite: suite}, filepath.Dir(suitePath), output)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Report: %s\nEligible for human adoption: %t\n%s\nCost and human interventions: unavailable (not instrumented).\n", filepath.Join(output, "report.json"), report.Eligible, report.Reason)
		if !report.Eligible {
			return errors.New("comparison did not pass the adoption gate; inspect the report")
		}
		return nil
	},
}

var agentMemoryAdoptCmd = &cobra.Command{
	Use: "adopt <report.json>", Short: "Adopt the exact compared revision after human review (server enforces human access)", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if yes, _ := cmd.Flags().GetBool("reviewed"); !yes {
			return errors.New("review the report, checks, artifacts and limitations, then pass --reviewed")
		}
		var report memoryeval.Report
		if err := memoryeval.ReadJSON(args[0], &report); err != nil {
			return err
		}
		if ok, reason := report.Gate(); !ok {
			return fmt.Errorf("cannot adopt: %s", reason)
		}
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(cmd.Context())
		defer cancel()
		evaluationID, err := publishMemoryEvaluation(ctx, client, report)
		if err != nil {
			return err
		}
		candidate := report.Candidate
		var result map[string]any
		if err := client.PutJSON(ctx, "/api/agents/"+url.PathEscape(candidate.AgentID)+"/memories/"+url.PathEscape(candidate.ID), map[string]any{"status": "active", "expected_revision": candidate.Revision, "evaluation_id": evaluationID}, &result); err != nil {
			return err
		}
		return cli.PrintJSON(cmd.OutOrStdout(), result)
	},
}

func publishMemoryEvaluation(ctx context.Context, client *cli.APIClient, report memoryeval.Report) (string, error) {
	if strings.TrimRight(client.BaseURL, "/") != report.ServerURL || client.WorkspaceID != report.Candidate.WorkspaceID {
		return "", errors.New("report belongs to a different server or workspace")
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := client.PostJSON(ctx, "/api/agents/"+url.PathEscape(report.Candidate.AgentID)+"/memories/"+url.PathEscape(report.Candidate.ID)+"/evaluations", report, &result); err != nil {
		return "", err
	}
	if _, err := util.ParseUUID(result.ID); err != nil {
		return "", errors.New("invalid saved evaluation response")
	}
	return result.ID, nil
}

var agentMemoryPublishCmd = &cobra.Command{
	Use: "publish <report.json>", Short: "Save an offline report for human review in the agent's Memory tab", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var report memoryeval.Report
		if err := memoryeval.ReadJSON(args[0], &report); err != nil {
			return err
		}
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(cmd.Context())
		defer cancel()
		id, err := publishMemoryEvaluation(ctx, client, report)
		if err != nil {
			return err
		}
		return cli.PrintJSON(cmd.OutOrStdout(), map[string]string{"id": id})
	},
}

var agentMemoryRestoreCmd = &cobra.Command{
	Use: "restore <agent-id> <memory-id>", Short: "Restore a historical memory version with a revision conflict check", Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		revision, _ := cmd.Flags().GetInt32("revision")
		expected, _ := cmd.Flags().GetInt32("expected-revision")
		if revision < 1 || expected < 1 {
			return errors.New("revision and expected-revision must be positive")
		}
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(cmd.Context())
		defer cancel()
		var result map[string]any
		if err := client.PutJSON(ctx, "/api/agents/"+url.PathEscape(args[0])+"/memories/"+url.PathEscape(args[1]), map[string]any{"restore_revision": revision, "expected_revision": expected}, &result); err != nil {
			return err
		}
		return cli.PrintJSON(cmd.OutOrStdout(), result)
	},
}

func init() {
	agentCmd.AddCommand(agentMemoryCmd)
	agentMemoryCmd.AddCommand(agentMemoryEvaluateCmd, agentMemoryAdoptCmd, agentMemoryRestoreCmd, agentMemoryPublishCmd)
	agentMemoryEvaluateCmd.Flags().String("suite", "", "Human-owned offline suite JSON (see docs/development/memory-evaluation.md)")
	agentMemoryEvaluateCmd.Flags().String("output", "", "New private report directory; must not exist")
	_ = agentMemoryEvaluateCmd.MarkFlagRequired("suite")
	_ = agentMemoryEvaluateCmd.MarkFlagRequired("output")
	agentMemoryAdoptCmd.Flags().Bool("reviewed", false, "Confirm human review of this local, unsigned report and its executable checks")
	agentMemoryRestoreCmd.Flags().Int32("revision", 0, "Historical revision to restore (the report records the pre-adoption revision)")
	agentMemoryRestoreCmd.Flags().Int32("expected-revision", 0, "Current memory revision; stale updates are rejected")
	_ = agentMemoryRestoreCmd.MarkFlagRequired("revision")
	_ = agentMemoryRestoreCmd.MarkFlagRequired("expected-revision")
}
