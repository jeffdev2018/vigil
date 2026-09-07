package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// multica issue spend-token {request,verify} and issue budget-status — the
// agent-facing half of the spend guard (K05) and the run limits (K03).
//
// The server has enforced both since they landed, but nothing could reach the
// three endpoints: an agent about to spend real money had no way to ask for a
// token, so a workspace that configured a spend threshold had a threshold that
// never fired. These commands are that missing caller; the contract below is
// the handlers' (approval_gate.go, run_limit.go), not a new one.

// spendGateWaitBudget bounds `--wait`. The gate's own timeout is a workspace
// setting (30 minutes by default), which is far longer than a CLI call should
// hang, so waiting gives up first and returns the still-pending gate rather
// than an error — the caller keeps the gate id and can resume with
// `--gate <id> --wait`.
const spendGateWaitBudget = 5 * time.Minute

// spendGatePollWait is the ?wait= value passed to the gate read. The server
// long-polls up to 30s, so the loop needs no client-side sleep.
const spendGatePollWait = 25

var issueSpendTokenCmd = &cobra.Command{
	Use:   "spend-token",
	Short: "Ask for, and spend, permission to make a paid call",
}

var issueSpendTokenRequestCmd = &cobra.Command{
	Use:   "request <run-id>",
	Short: "Ask this run for permission to spend --amount",
	Long: "Ask for a spend token before making a call that costs real money.\n\n" +
		"Under the workspace's spend threshold the token is issued immediately.\n" +
		"Above it the server opens a spend gate for a human to decide and answers\n" +
		"with the gate instead of a token — that is a normal outcome, not a\n" +
		"failure. Tell them apart by the response: a token has \"token\", a gate has\n" +
		"\"id\" and \"status\": \"pending\".\n\n" +
		"With --wait the command holds until the gate is decided and, on approval,\n" +
		"collects the token for you. Without it, wait yourself and re-run with\n" +
		"--gate <id> once the gate is approved. A denied or expired gate is printed\n" +
		"with its status and exits 0: the server, not this exit code, is what stops\n" +
		"the spend — without a token `verify` refuses.\n\n" +
		"A token is valid for 5 minutes and for one spend only.",
	Args: exactArgs(1),
	RunE: runIssueSpendTokenRequest,
}

var issueSpendTokenVerifyCmd = &cobra.Command{
	Use:   "verify <run-id>",
	Short: "Spend a token: declare the charge it is covering",
	Long: "Redeem a spend token for one charge. The token is single use — the first\n" +
		"successful verify spends it — and --amount may not exceed the amount the\n" +
		"token was issued for. Call this when the money is actually about to move,\n" +
		"so the workspace's record of what was approved matches what was spent.",
	Args: exactArgs(1),
	RunE: runIssueSpendTokenVerify,
}

var issueBudgetStatusCmd = &cobra.Command{
	Use:   "budget-status <run-id>",
	Short: "Show a run's usage against its limits, and the limits it has hit",
	Long: "Report what this run has consumed (tokens, tool calls, wall clock, cost),\n" +
		"the effective run limits it is measured against, and every warn/stop event\n" +
		"already recorded for it. Read-only.",
	Args: exactArgs(1),
	RunE: runIssueBudgetStatus,
}

func init() {
	issueSpendTokenRequestCmd.Flags().String("amount", "", "Amount to spend, in USD (\"2\", \"2.50\", \"0.0001\"). Required.")
	issueSpendTokenRequestCmd.Flags().String("purpose", "", "What the money is for, shown to whoever approves the gate. Required.")
	issueSpendTokenRequestCmd.Flags().String("gate", "", "Collect the token for an already-approved spend gate id")
	issueSpendTokenRequestCmd.Flags().Bool("wait", false, "Wait for a pending gate to be decided, then collect the token")
	issueSpendTokenRequestCmd.Flags().String("issue", "", "Issue ID/key to scope short run ID prefix resolution")
	issueSpendTokenRequestCmd.Flags().String("output", "json", "Output format: json")

	issueSpendTokenVerifyCmd.Flags().String("token", "", "The mst_ token returned by `spend-token request`. Required.")
	issueSpendTokenVerifyCmd.Flags().String("amount", "", "Amount actually being charged, in USD. Required, and may not exceed the token's amount.")
	issueSpendTokenVerifyCmd.Flags().String("issue", "", "Issue ID/key to scope short run ID prefix resolution")
	issueSpendTokenVerifyCmd.Flags().String("output", "json", "Output format: json")

	issueBudgetStatusCmd.Flags().String("issue", "", "Issue ID/key to scope short run ID prefix resolution")
	issueBudgetStatusCmd.Flags().String("output", "json", "Output format: json")

	issueSpendTokenCmd.AddCommand(issueSpendTokenRequestCmd, issueSpendTokenVerifyCmd)
	issueCmd.AddCommand(issueSpendTokenCmd)
	issueCmd.AddCommand(issueBudgetStatusCmd)
}

// parseUsdTicks turns a plain USD amount ("2", "$2.50", "0.0001") into the
// integer tick count the API takes. Parsed as text with integer arithmetic and
// never through a float: this is the amount a human approves and an agent then
// spends, so a binary rounding artefact is not an acceptable failure mode. The
// alternative — making the caller write 25000000000 by hand — puts a 1000x slip
// one keystroke away, on the one path where that slip is money.
func parseUsdTicks(raw, flag string) (int64, error) {
	s := strings.TrimPrefix(strings.TrimSpace(raw), "$")
	if s == "" {
		return 0, fmt.Errorf("%s is required, in USD (for example %s 2.50)", flag, flag)
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	// Every *_usd_ticks field in the API counts 1 USD as 1e10 ticks
	// (handler.usdLabel divides by exactly this), so ten decimal places.
	const decimals = 10
	if len(frac) > decimals {
		return 0, fmt.Errorf("%s %q has more than %d decimal places; the smallest unit is $0.0000000001", flag, raw, decimals)
	}
	digits := whole + frac + strings.Repeat("0", decimals-len(frac))
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%s %q is not a USD amount; pass a plain number like 2.50", flag, raw)
		}
	}
	ticks, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q is out of range for a USD amount", flag, raw)
	}
	if ticks <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", flag)
	}
	return ticks, nil
}

// resolveRunArg is the run resolution every command here shares: the optional
// --issue narrows a short run-id prefix, exactly as `issue run-plan set` does.
func resolveRunArg(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, arg string) (string, error) {
	issueID := ""
	if issueInput, _ := cmd.Flags().GetString("issue"); issueInput != "" {
		issueRef, err := resolveIssueRef(ctx, client, issueInput)
		if err != nil {
			return "", fmt.Errorf("resolve issue: %w", err)
		}
		issueID = issueRef.ID
	}
	taskRef, err := resolveTaskRunID(ctx, client, issueID, arg)
	if err != nil {
		return "", fmt.Errorf("resolve run: %w", err)
	}
	return taskRef.ID, nil
}

func runIssueSpendTokenRequest(cmd *cobra.Command, args []string) error {
	// Validate the amount before any network round trip, so a malformed
	// --amount can never reach the spend path.
	amount, _ := cmd.Flags().GetString("amount")
	ticks, err := parseUsdTicks(amount, "--amount")
	if err != nil {
		return err
	}
	gateID, _ := cmd.Flags().GetString("gate")
	purpose := strings.TrimSpace(mustString(cmd, "purpose"))
	// The server accepts an empty purpose; we do not. It is the whole body of
	// the Decision Card a human is asked to approve, and "spend $500 · " is not
	// a question anyone can answer. Not required when collecting the token for
	// a gate that already carries its purpose.
	if purpose == "" && gateID == "" {
		return fmt.Errorf("--purpose is required: it is what the person approving the spend sees")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	wait, _ := cmd.Flags().GetBool("wait")
	timeout := cli.APITimeout()
	if wait {
		timeout = cli.AtLeastAPITimeout(spendGateWaitBudget)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	runID, err := resolveRunArg(ctx, client, cmd, args[0])
	if err != nil {
		return err
	}

	result, err := postSpendToken(ctx, client, runID, ticks, purpose, gateID)
	if err != nil {
		return err
	}
	// A token means the spend is allowed; anything else is the gate, which the
	// two bodies keep disjoint (200 has "token", 202 has "id" + "status").
	if !wait || result["token"] != nil {
		return cli.PrintJSON(os.Stdout, result)
	}
	gate, err := waitForSpendGate(ctx, client, runID, strVal(result, "id"), result)
	if err != nil {
		return err
	}
	if strVal(gate, "status") != "approved" {
		// Denied, expired, or still pending when the wait budget ran out. The
		// gate id is in the payload either way, so the caller can resume.
		return cli.PrintJSON(os.Stdout, gate)
	}
	issued, err := postSpendToken(ctx, client, runID, ticks, purpose, strVal(gate, "id"))
	if err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, issued)
}

// postSpendToken performs one POST /spend-token. A 202 is not an error here:
// PostJSON only fails at >= 400, so the gate body decodes like any other.
func postSpendToken(ctx context.Context, client *cli.APIClient, runID string, ticks int64, purpose, gateID string) (map[string]any, error) {
	body := map[string]any{"amount_usd_ticks": ticks}
	if purpose != "" {
		body["purpose"] = purpose
	}
	if gateID != "" {
		body["gate_id"] = gateID
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/tasks/"+url.PathEscape(runID)+"/spend-token", body, &result); err != nil {
		return nil, fmt.Errorf("request spend token: %w", err)
	}
	return result, nil
}

// waitForSpendGate long-polls the gate until a human decides it or the wait
// budget runs out, and returns the last gate seen either way.
func waitForSpendGate(ctx context.Context, client *cli.APIClient, runID, gateID string, opened map[string]any) (map[string]any, error) {
	if gateID == "" {
		return opened, nil
	}
	path := fmt.Sprintf("/api/tasks/%s/gates/%s?wait=%d", url.PathEscape(runID), url.PathEscape(gateID), spendGatePollWait)
	gate := opened
	for {
		var next map[string]any
		if err := client.GetJSON(ctx, path, &next); err != nil {
			// The budget running out is the documented outcome, not a failure:
			// return the last gate so the caller keeps the id.
			if ctx.Err() != nil {
				return gate, nil
			}
			return nil, fmt.Errorf("wait for spend gate: %w", err)
		}
		gate = next
		if strVal(gate, "status") != "pending" {
			return gate, nil
		}
		// The server holds the request for ?wait= seconds, so this normally
		// costs nothing. It is here so a server that answers immediately
		// (an older one that ignores ?wait=) is polled once a second rather
		// than in a tight loop for the whole budget.
		select {
		case <-ctx.Done():
			return gate, nil
		case <-time.After(time.Second):
		}
	}
}

func runIssueSpendTokenVerify(cmd *cobra.Command, args []string) error {
	token := strings.TrimSpace(mustString(cmd, "token"))
	if token == "" {
		return fmt.Errorf("--token is required: pass the token `spend-token request` returned")
	}
	amount, _ := cmd.Flags().GetString("amount")
	ticks, err := parseUsdTicks(amount, "--amount")
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	runID, err := resolveRunArg(ctx, client, cmd, args[0])
	if err != nil {
		return err
	}

	var result map[string]any
	body := map[string]any{"token": token, "amount_usd_ticks": ticks}
	if err := client.PostJSON(ctx, "/api/tasks/"+url.PathEscape(runID)+"/spend-token/verify", body, &result); err != nil {
		return fmt.Errorf("verify spend token: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueBudgetStatus(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	runID, err := resolveRunArg(ctx, client, cmd, args[0])
	if err != nil {
		return err
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/tasks/"+url.PathEscape(runID)+"/budget-status", &result); err != nil {
		return fmt.Errorf("get budget status: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}
