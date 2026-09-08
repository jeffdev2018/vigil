package memoryeval

import (
	"encoding/hex"
	"errors"
)

// RuntimeEvidence is reported by the configured executable through Multica's
// adapter. It is not server attestation or proof that a requested model ran.
type RuntimeEvidence struct {
	Provider        string                  `json:"provider"`
	RequestedModel  string                  `json:"requested_model"`
	RequestedEffort string                  `json:"requested_effort"`
	ExecutableHash  string                  `json:"executable_hash"`
	PromptHash      string                  `json:"prompt_hash"`
	BriefHash       string                  `json:"brief_hash"`
	Status          string                  `json:"status"`
	ToolCalls       int                     `json:"tool_calls"`
	Usage           map[string]RuntimeUsage `json:"usage"`
}

type RuntimeUsage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

type RuntimeOutput struct {
	Artifact string           `json:"artifact"`
	Runtime  *RuntimeEvidence `json:"runtime"`
}

func (r *RuntimeEvidence) Validate() error {
	if r == nil || (r.Provider != "claude" && r.Provider != "codex") || r.RequestedModel == "" || r.ToolCalls < 0 || len(r.Usage) > 32 {
		return errors.New("invalid runtime observations")
	}
	for _, h := range []string{r.ExecutableHash, r.PromptHash, r.BriefHash} {
		if b, err := hex.DecodeString(h); err != nil || len(b) != 32 || hex.EncodeToString(b) != h {
			return errors.New("invalid runtime fingerprint")
		}
	}
	switch r.Status {
	case "completed", "failed", "aborted", "timeout", "cancelled":
	default:
		return errors.New("invalid runtime status")
	}
	for model, u := range r.Usage {
		if model == "" || u.InputTokens < 0 || u.OutputTokens < 0 || u.CacheReadTokens < 0 || u.CacheWriteTokens < 0 {
			return errors.New("invalid reported usage")
		}
	}
	return nil
}
