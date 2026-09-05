package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Issue scoping assistant (K14): a few raw sentences become a proposed issue
// (title, description, acceptance criteria, probable files) that a human
// reviews and edits before anything is created. The server has no view of
// the repository, so probable files are the model's guesses and are labeled
// as such; the client keeps them editable.

const (
	ErrCodeLLMUnavailable   = "llm_unavailable"
	ErrCodeScopingMalformed = "scoping_malformed"

	scopingMaxTextRunes   = 8000
	scopingMaxCriteria    = 10
	scopingMaxFiles       = 10
	scopingMaxTitleRunes  = 160
	scopingRequestTimeout = 45 * time.Second
)

type ScopingFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

type ScopingProposal struct {
	Title              string        `json:"title"`
	Description        string        `json:"description"`
	AcceptanceCriteria []string      `json:"acceptance_criteria"`
	ProbableFiles      []ScopingFile `json:"probable_files"`
}

const scopingSystemPrompt = `You turn a short, informal request into a well-scoped software issue.
Answer with JSON only, exactly this shape:
{"title":"...","description":"...","acceptance_criteria":["..."],"probable_files":[{"path":"...","reason":"..."}]}
Rules:
- title: one line, at most 12 words, imperative, no trailing period.
- description: markdown; keep every fact from the request, add a short Context section and an Out of scope section only when the request implies one. Do not invent requirements.
- acceptance_criteria: 2 to 6 short, testable statements a reviewer can check; each must follow from the request.
- probable_files: 0 to 6 entries. Only when the request names or clearly implies a place in the code; each path is a plausible repository path and the reason says why. Leave the array empty rather than guessing.
- Write in the same language as the request.`

// normalizeScopingProposal drops empties, caps lengths, and fills a missing
// title or description from the raw text so the reviewer always has a draft.
func normalizeScopingProposal(p ScopingProposal, rawText string) ScopingProposal {
	p.Title = strings.TrimSpace(strings.Join(strings.Fields(p.Title), " "))
	if p.Title == "" {
		line := strings.TrimSpace(strings.SplitN(rawText, "\n", 2)[0])
		p.Title = strings.Join(strings.Fields(line), " ")
	}
	if r := []rune(p.Title); len(r) > scopingMaxTitleRunes {
		p.Title = strings.TrimSpace(string(r[:scopingMaxTitleRunes]))
	}
	p.Description = strings.TrimSpace(p.Description)
	if p.Description == "" {
		p.Description = strings.TrimSpace(rawText)
	}
	criteria := make([]string, 0, len(p.AcceptanceCriteria))
	for _, c := range p.AcceptanceCriteria {
		if c = strings.TrimSpace(c); c != "" && len(criteria) < scopingMaxCriteria {
			criteria = append(criteria, c)
		}
	}
	p.AcceptanceCriteria = criteria
	files := make([]ScopingFile, 0, len(p.ProbableFiles))
	for _, f := range p.ProbableFiles {
		f.Path = strings.TrimSpace(f.Path)
		f.Reason = strings.TrimSpace(f.Reason)
		if f.Path != "" && len(files) < scopingMaxFiles {
			files = append(files, f)
		}
	}
	p.ProbableFiles = files
	return p
}

// ProposeIssueScoping — POST /api/issues/scoping/propose. Nothing is created:
// the proposal is returned for review.
func (h *Handler) ProposeIssueScoping(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	var req struct {
		RawText   string `json:"raw_text"`
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.RawText = strings.TrimSpace(req.RawText)
	if req.RawText == "" || utf8.RuneCountInString(req.RawText) > scopingMaxTextRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("raw_text is required (at most %d characters)", scopingMaxTextRunes))
		return
	}
	if h.LLM == nil || !h.LLM.Enabled() {
		writeErrorCode(w, http.StatusServiceUnavailable, ErrCodeLLMUnavailable, "the scoping assistant needs the server LLM to be configured")
		return
	}

	var userPrompt strings.Builder
	if req.ProjectID != "" {
		if pid, err := util.ParseUUID(req.ProjectID); err == nil {
			if project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: pid, WorkspaceID: wsUUID}); err == nil {
				fmt.Fprintf(&userPrompt, "Project: %s\n", project.Title)
				if project.Description.Valid && strings.TrimSpace(project.Description.String) != "" {
					fmt.Fprintf(&userPrompt, "Project description: %s\n", strings.TrimSpace(project.Description.String))
				}
				userPrompt.WriteString("\n")
			}
		}
	}
	userPrompt.WriteString("Request:\n")
	userPrompt.WriteString(req.RawText)

	ctx, cancel := context.WithTimeout(r.Context(), scopingRequestTimeout)
	defer cancel()
	raw, err := h.LLM.GenerateJSON(ctx, "", scopingSystemPrompt, userPrompt.String(), 0.2, 2048)
	if err != nil {
		slog.Warn("issue scoping: llm call failed", append(logger.RequestAttrs(r), "error", err)...)
		writeErrorCode(w, http.StatusBadGateway, ErrCodeLLMUnavailable, "the scoping assistant could not reach the model; write the issue by hand")
		return
	}
	var proposal ScopingProposal
	if err := json.Unmarshal([]byte(raw), &proposal); err != nil {
		slog.Warn("issue scoping: malformed model answer", append(logger.RequestAttrs(r), "error", err)...)
		writeErrorCode(w, http.StatusBadGateway, ErrCodeScopingMalformed, "the model answered in an unexpected shape; try again or write the issue by hand")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposal": normalizeScopingProposal(proposal, req.RawText)})
}
