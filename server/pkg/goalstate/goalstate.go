// Package goalstate is the wire shape of an issue's goal-loop state — what a
// resuming run (native or CLI) is told about the goal it works toward, and
// what the clients render. The server builds it from the issue_goal row; the
// daemon renders it into the prompt with Render, so both runtimes read the
// same words.
package goalstate

import (
	"fmt"
	"strings"
)

// Question is a typed question an agent asked the team. Kind is "text" or
// "choice"; Options is set for choice. Answer is empty while it waits.
type Question struct {
	Kind       string   `json:"kind"`
	Prompt     string   `json:"prompt"`
	Options    []string `json:"options,omitempty"`
	RunID      string   `json:"run_id"`
	AskedAt    string   `json:"asked_at"`
	Answer     string   `json:"answer,omitempty"`
	AnsweredBy string   `json:"answered_by,omitempty"`
	// AnsweredByName is the display name behind AnsweredBy, for the cards.
	AnsweredByName string `json:"answered_by_name,omitempty"`
	AnsweredAt     string `json:"answered_at,omitempty"`
}

// State is the goal-loop state of one issue.
type State struct {
	ID               string    `json:"id"`
	IssueID          string    `json:"issue_id"`
	Goal             string    `json:"goal"`
	Status           string    `json:"status"`
	Continuation     int       `json:"continuation"`
	MaxContinuations int       `json:"max_continuations"`
	NoProgress       int       `json:"no_progress"`
	LastOutcome      string    `json:"last_outcome"`
	LastBlocker      string    `json:"last_blocker,omitempty"`
	LastReason       string    `json:"last_reason,omitempty"`
	NextStep         string    `json:"next_step,omitempty"`
	Evidence         []string  `json:"evidence"`
	Question         *Question `json:"question,omitempty"`
	LastRunID        string    `json:"last_run_id,omitempty"`
	// ChainRootTaskID is the workflow root of the last run: the legs endpoint
	// keyed on it totals what the whole chain cost.
	ChainRootTaskID string `json:"chain_root_task_id,omitempty"`
	DoneRequestID   string `json:"done_request_id,omitempty"`
	SetByType       string `json:"set_by_type"`
	UpdatedAt       string `json:"updated_at"`
}

// Render is the prompt block a run reads. Empty when there is nothing a
// fresh run would not already know from the issue itself.
func Render(s *State) string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Goal state for this issue (kept by the server between runs; read it before working, do not redo what the evidence already covers):\n")
	if strings.TrimSpace(s.Goal) != "" {
		fmt.Fprintf(&b, "- Goal (definition of done): %s\n", strings.TrimSpace(s.Goal))
	} else {
		b.WriteString("- Goal: the issue's title, description and acceptance criteria.\n")
	}
	fmt.Fprintf(&b, "- Status: %s; this is continuation %d of at most %d.\n", s.Status, s.Continuation, s.MaxContinuations)
	if s.LastBlocker != "" || s.LastReason != "" {
		fmt.Fprintf(&b, "- Why the previous run was not enough (%s): %s\n", s.LastBlocker, s.LastReason)
	}
	if s.NextStep != "" {
		fmt.Fprintf(&b, "- Next step the judge suggested: %s\n", s.NextStep)
	}
	if len(s.Evidence) > 0 {
		b.WriteString("- Evidence gathered so far (oldest first):\n")
		for _, e := range s.Evidence {
			fmt.Fprintf(&b, "  - %s\n", e)
		}
	}
	if s.Question != nil {
		if s.Question.Answer != "" {
			fmt.Fprintf(&b, "- You asked: %s\n  The team answered: %s\n", s.Question.Prompt, s.Question.Answer)
		} else {
			fmt.Fprintf(&b, "- A question to the team is still waiting for an answer: %s\n", s.Question.Prompt)
		}
	}
	b.WriteString("When the goal is met, say so with the evidence. When you need a decision only a human can make, ask it (ask_user for native runs, POST /api/issues/{id}/goal/question for CLI runs) and stop.\n")
	return b.String()
}
