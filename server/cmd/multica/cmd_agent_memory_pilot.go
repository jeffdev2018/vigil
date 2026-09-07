package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/memoryeval"
	"github.com/spf13/cobra"
)

var memoryPilotSummaryCmd = &cobra.Command{
	Use: "pilot-summary <report.json>", Short: "Summarize independent checks and optional fingerprint-bound human effort/cost records", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var report memoryeval.Report
		file, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
		if err != nil {
			return err
		}
		if len(raw) > 64<<20 {
			return errors.New("report exceeds 64 MiB")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&report); err != nil {
			return err
		}
		if decoder.Decode(new(any)) != io.EOF {
			return errors.New("expected one report")
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(raw))
		var reviews *memoryeval.PilotReviews
		if path, _ := cmd.Flags().GetString("reviews"); path != "" {
			reviews = &memoryeval.PilotReviews{}
			if err := memoryeval.ReadJSON(path, reviews); err != nil {
				return err
			}
		}
		summary, err := memoryeval.SummarizePilot(report, hash, reviews)
		if err != nil {
			return err
		}
		return cli.PrintJSON(cmd.OutOrStdout(), summary)
	},
}

func init() {
	agentMemoryCmd.AddCommand(memoryPilotSummaryCmd)
	memoryPilotSummaryCmd.Flags().String("reviews", "", "Human review JSON bound to this report hash; omitted values remain unknown")
}
