package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica consult — the agent-facing half of JEF-12: a running agent task
// asks the platform's internal LLM one synchronous question mid-run. The
// server accepts only a task_token (POST /api/consult), so this command is
// meant to be called from inside a run, where the daemon injects MULTICA_TOKEN
// (mat_) plus MULTICA_TASK_ID — the X-Task-ID header the endpoint scopes on.
// Outside a run the token guard fails first (newAPIClient refuses a
// user-global token in a daemon-managed context) or the server answers
// 401/403, both surfaced as ordinary CLI errors.
//
// A REFUSED consult is not a failure of the calling run: on 429
// consult_budget_exceeded and 503 consult_llm_disabled the refusal is printed
// to stderr and the exit code is 0, so an agent script that treats consult as
// best-effort keeps going. Every other non-2xx stays an error.

var consultCmd = &cobra.Command{
	Use:   "consult \"<question>\"",
	Short: "Ask the platform's internal LLM one question from inside a run",
	Long: "Ask the platform's internal LLM one synchronous question mid-run. Prints the\n" +
		"answer to stdout; --output json prints the full response object (consult_id,\n" +
		"answer, model, cost_usd_ticks). --context <file> attaches a file's contents as\n" +
		"context for the question.\n\n" +
		"A refused consult (daily budget exhausted, or no consult LLM configured) is\n" +
		"printed to stderr and exits 0 — the refusal is an answer, not a failure of\n" +
		"your run.",
	Args: exactArgs(1),
	RunE: runConsult,
}

func init() {
	consultCmd.Flags().String("context", "", "Path to a file whose contents become the consult's context")
	consultCmd.Flags().String("output", "", "Output format: json prints the full response object (default prints only the answer)")
}

// consultRefusalCodes are the reason_code values that mean "refused, keep
// going" rather than "the call failed" (handler.consultReason*).
var consultRefusalCodes = map[string]bool{
	"consult_budget_exceeded": true,
	"consult_llm_disabled":    true,
}

func runConsult(cmd *cobra.Command, args []string) error {
	question := strings.TrimSpace(args[0])
	if question == "" {
		return fmt.Errorf("question is required")
	}
	body := map[string]any{"question": question}
	if path, _ := cmd.Flags().GetString("context"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read --context file: %w", err)
		}
		body["context"] = string(data)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var resp struct {
		ConsultID    string `json:"consult_id"`
		Answer       string `json:"answer"`
		Model        string `json:"model"`
		CostUSDTicks *int64 `json:"cost_usd_ticks"`
	}
	if err := client.PostJSON(ctx, "/api/consult", body, &resp); err != nil {
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) && isConsultRefusal(httpErr) {
			// Refusals (budget exhausted, LLM disabled) are born-terminal
			// answers the run continues from, not failures of it: stderr, exit 0.
			fmt.Fprintf(os.Stderr, "consult refused: %s\n", strings.TrimSpace(httpErr.Body))
			return nil
		}
		return fmt.Errorf("consult: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintln(os.Stdout, resp.Answer)
	return nil
}

// isConsultRefusal reports whether err is one of the consult's two typed
// refusals: 429 consult_budget_exceeded or 503 consult_llm_disabled. The
// reason_code in the body — not the status alone — decides, so an unrelated
// 429/503 (rate limiter, deploy) still surfaces as an error.
func isConsultRefusal(err *cli.HTTPError) bool {
	if err.StatusCode != http.StatusTooManyRequests && err.StatusCode != http.StatusServiceUnavailable {
		return false
	}
	var refusal struct {
		ReasonCode string `json:"reason_code"`
	}
	if json.Unmarshal([]byte(err.Body), &refusal) != nil {
		return false
	}
	return consultRefusalCodes[refusal.ReasonCode]
}
