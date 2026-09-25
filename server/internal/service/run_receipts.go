package service

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
)

// A run's receipts are the actions the server itself carried out for it: one
// entry per tool call that reached a write, with the arguments it was given
// and whether it succeeded. The native loop keeps them for the honest-stop
// ledger (N18), which reconciles what an agent claims against what it did.
//
// Both judges — "is the goal met?" and "how much evidence does this run show?"
// — were asked those questions with only the agent's own closing prose to go
// on. A smoke test showed what that costs: an agent posted the comment it was
// asked for, the server recorded the receipt, and both judges answered "no
// evidence". One scored the run 0 and filed a review for nothing; the other
// answered missing_evidence, which ran a continuation that posted the same
// comment a second time.
//
// The receipts are the strongest evidence in the system, because they are the
// server's own record rather than a claim. They go in the state.

const (
	// runReceiptsMax bounds how many receipts are described. A run is capped
	// at 10 effectful actions, so this holds every one with room to spare.
	runReceiptsMax = 12
	// runReceiptArgsCap bounds each entry. Enough to see what was written
	// without shipping a whole comment body per receipt.
	runReceiptArgsCap = 240
)

// runReceipt is one entry as the judges see it.
type runReceipt struct {
	Tool string `json:"tool"`
	OK   bool   `json:"succeeded"`
	Args string `json:"arguments,omitempty"`
}

// receiptsFromResult reads the actions a finished run actually performed out of
// its stored result. Returns nil when the result holds none, which is the
// honest answer for a run that wrote nothing — and for one whose runtime does
// not keep receipts at all, where the judges fall back to the closing prose
// exactly as they did before.
func receiptsFromResult(result []byte) []runReceipt {
	if len(result) == 0 {
		return nil
	}
	var stored struct {
		Receipts []struct {
			Tool string `json:"tool"`
			Args string `json:"args"`
			OK   bool   `json:"ok"`
		} `json:"receipts"`
	}
	if json.Unmarshal(result, &stored) != nil || len(stored.Receipts) == 0 {
		return nil
	}
	out := make([]runReceipt, 0, min(len(stored.Receipts), runReceiptsMax))
	for _, r := range stored.Receipts {
		if len(out) >= runReceiptsMax {
			break
		}
		tool := strings.TrimSpace(r.Tool)
		if tool == "" {
			continue
		}
		out = append(out, runReceipt{
			Tool: tool,
			OK:   r.OK,
			Args: util.TruncateUTF8Bytes(strings.TrimSpace(r.Args), runReceiptArgsCap),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
