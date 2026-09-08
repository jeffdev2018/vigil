package memoryeval

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const ConnectedProtocol = "connected_runtime_v1"

// ConnectedConfig freezes the text-only comparison contract. Runtime jobs
// exclude expected answers; authorized human reports retain the grading criteria.
type ConnectedConfig struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	Effort           string `json:"effort"`
	AgentName        string `json:"agent_name"`
	Instructions     string `json:"instructions"`
	WorkspaceContext string `json:"workspace_context"`
}

type ConnectedCase struct {
	Check    string `json:"check,omitempty"`
	ID       string `json:"id"`
	Split    string `json:"split"`
	Prompt   string `json:"prompt"`
	Expected string `json:"expected,omitempty"`
}

// ConnectedJob is the daemon-facing projection, excluding expected answers.
type ConnectedJob struct {
	ID        string          `json:"id"`
	Config    ConnectedConfig `json:"config"`
	Cases     []ConnectedCase `json:"cases"`
	Baseline  []string        `json:"baseline"`
	Candidate string          `json:"candidate"`
}

type ConnectedResult struct {
	Index      int              `json:"index"`
	Artifact   string           `json:"artifact"`
	Runtime    *RuntimeEvidence `json:"runtime"`
	DurationMS int64            `json:"duration_ms"`
	Failed     bool             `json:"failed"`
}

func (s Suite) validateConnected() error {
	if s.Connected == nil || (s.Connected.Provider != "claude" && s.Connected.Provider != "codex") || strings.TrimSpace(s.Connected.Model) == "" || len(s.Connected.Model) > 200 || len(s.Connected.Instructions) > 32000 || len(s.Connected.WorkspaceContext) > 32000 || len(s.Connected.Effort) > 32 || len(s.Connected.AgentName) > 200 {
		return errors.New("invalid connected runtime configuration")
	}
	if s.Image != "" || len(s.Worker) != 0 || len(s.Verifier) != 0 || s.TimeoutSeconds != 60 || len(s.Cases) < 2 || len(s.Cases) > 8 || len(s.TextCases) != len(s.Cases) {
		return errors.New("connected comparison requires 2–8 text cases and a 60-second response limit")
	}
	ids, prompts, splits := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, c := range s.TextCases {
		p := strings.TrimSpace(c.Prompt)
		if strings.ContainsRune(c.Prompt, 0) || strings.ContainsRune(c.Expected, 0) {
			return errors.New("case text cannot contain NUL")
		}
		if !caseID.MatchString(c.ID) || ids[c.ID] || prompts[p] || p == "" || len(c.Prompt) > 8000 || strings.TrimSpace(c.Expected) == "" || len(c.Expected) > 8000 || (c.Split != "replay" && c.Split != "holdout") || s.Cases[i] != (Case{ID: c.ID, Split: c.Split, Input: c.ID, Checks: c.ID}) {
			return errors.New("provide distinct prompts, unique IDs, expected answers and replay/holdout splits")
		}
		if err := validateAnswerCheck(c, s.CodeImage); err != nil {
			return err
		}
		ids[c.ID], prompts[p], splits[c.Split] = true, true, true
	}
	if !splits["replay"] || !splits["holdout"] {
		return errors.New("both replay and holdout are required")
	}
	return nil
}

func ConnectedComparisons(s Suite) []Comparison {
	items := make([]Comparison, len(s.TextCases))
	for i, c := range s.TextCases {
		input, _ := json.Marshal(struct {
			Config *ConnectedConfig
			Prompt string
		}{s.Connected, c.Prompt})
		items[i] = Comparison{ID: c.ID, Split: c.Split, InputHash: fmt.Sprintf("%x", sha256.Sum256(input)), ChecksHash: fmt.Sprintf("%x", sha256.Sum256(checkContract(c, s.CodeImage)))}
	}
	return items
}

// GradeConnected never accepts a runtime's own pass/fail decision.
func GradeConnected(c ConnectedCase, result ConnectedResult, code *CodeObservation, image string) (Outcome, error) {
	if result.DurationMS < 0 || result.DurationMS > 120000 || len(result.Artifact) > 64<<10 {
		return Outcome{}, errors.New("invalid runtime result bounds")
	}
	o := Outcome{Status: "error", Artifact: result.Artifact, DurationMS: result.DurationMS, Runtime: result.Runtime}
	if result.Failed {
		o.Diagnostic = "Runtime execution failed or timed out"
		o.Runtime = nil
		return o, nil
	}
	if result.Runtime.Validate() != nil {
		return Outcome{}, errors.New("invalid runtime observations")
	}
	if result.Runtime.Status != "completed" {
		o.Diagnostic = "Runtime did not complete"
		return o, nil
	}
	o.Status = "failed"
	o.Diagnostic = "Exact answer check failed (leading and trailing whitespace ignored)"
	if strings.TrimSpace(result.Artifact) == strings.TrimSpace(c.Expected) {
		o.Status = "passed"
		o.Diagnostic = "Exact answer check passed (leading and trailing whitespace ignored)"
	}
	if c.Check != "" && c.Check != "exact" {
		return gradeAnswerCheck(c, o, code, image)
	}
	return o, nil
}

func (r Report) ValidKind() bool {
	if r.Suite.WorkerProtocol == ConnectedProtocol {
		return r.Kind == "connected_memory_comparison"
	}
	return r.Kind == "offline_memory_comparison"
}
