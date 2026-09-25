package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Note citations (JEF-417 / B06). An agent that relies on a workspace Brain
// note is asked (runtime_config_sections.go, brain_inject.go) to cite it
// inline as a Markdown link [title](mention://note/<uuid>). After a run
// completes, this pass scans its final output and the comments it posted for
// that link shape and records each valid citation as a 'cited' use — the same
// ledger injected/retrieved/opened/viewed already write to, one row per
// (task, note), idempotent on event redelivery via the table's unique index.

const noteCitationExtractionTimeout = 30 * time.Second

// citationLinkPattern matches a Markdown link to a Brain note:
// [any text](mention://note/<uuid>). The id group only accepts the canonical
// 8-4-4-4-12 hex shape, so a malformed id never reaches the caller — no
// separate validation step is needed for shape, only for existence.
var citationLinkPattern = regexp.MustCompile(
	`\[[^\]\n]*\]\(mention://note/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\)`)

// ParseCitedNoteIDs returns the distinct note ids cited as
// [text](mention://note/<uuid>) links in text, lowercased, in first-seen
// order. Links inside a fenced code block (``` or ~~~) are ignored: a run
// that quotes the citation syntax as an example must not record a use.
func ParseCitedNoteIDs(text string) []string {
	text = stripFencedCodeBlocks(text)
	var ids []string
	seen := make(map[string]bool)
	for _, m := range citationLinkPattern.FindAllStringSubmatch(text, -1) {
		id := strings.ToLower(m[1])
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// stripFencedCodeBlocks blanks out every fenced code block so citations
// merely shown as an example are not parsed as real ones. Line-oriented,
// matching how Markdown fences are actually written; a fence marker must
// start its own line (after leading whitespace) to open or close a block.
func stripFencedCodeBlocks(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inFence := false
	var marker string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if strings.HasPrefix(trimmed, "```") {
				inFence, marker = true, "```"
				continue
			}
			if strings.HasPrefix(trimmed, "~~~") {
				inFence, marker = true, "~~~"
				continue
			}
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, marker) {
			inFence = false
		}
	}
	return strings.Join(out, "\n")
}

// SubscribeNoteCitationExtraction wires citation recording onto the bus,
// alongside SubscribeAgentMemoryExtraction. Runs detached from the
// publisher's goroutine; a failure or panic here costs the completed task
// nothing (Bus.Publish already recovers listener panics; the worker recovers
// its own).
func (s *TaskService) SubscribeNoteCitationExtraction(bus *events.Bus) {
	if bus == nil {
		return
	}
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if status, _ := payload["status"].(string); status != "completed" {
			return
		}
		taskIDRaw, _ := payload["task_id"].(string)
		taskID, err := util.ParseUUID(taskIDRaw)
		if err != nil {
			return
		}
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("note citation extraction panicked", "task_id", taskIDRaw, "panic", rec)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), noteCitationExtractionTimeout)
			defer cancel()
			if err := s.ExtractNoteCitationsForTask(ctx, taskID); err != nil {
				slog.Warn("note citation extraction failed", "task_id", taskIDRaw, "error", err)
			}
		}()
	})
}

// ExtractNoteCitationsForTask runs one extraction pass for a completed task:
// parse citation links out of its final output and the comments it posted,
// resolve each against the task's own workspace, and record the live ones as
// 'cited' uses. Synchronous and safe to call directly from tests; the async
// admission path is SubscribeNoteCitationExtraction.
func (s *TaskService) ExtractNoteCitationsForTask(ctx context.Context, taskID pgtype.UUID) error {
	task, err := s.Queries.GetAgentTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The task row vanished between the event and the pass — nothing
			// left to extract from.
			return nil
		}
		return fmt.Errorf("load completed task: %w", err)
	}
	if task.Status != "completed" {
		return nil
	}

	wsIDStr, err := s.ResolveTaskWorkspaceIDChecked(ctx, task)
	if err != nil {
		return fmt.Errorf("resolve task workspace: %w", err)
	}
	if wsIDStr == "" {
		return nil
	}
	workspaceID, err := util.ParseUUID(wsIDStr)
	if err != nil {
		return nil
	}

	var payload protocol.TaskCompletedPayload
	if err := json.Unmarshal(task.Result, &payload); err != nil {
		return fmt.Errorf("decode task result: %w", err)
	}
	text := payload.Output

	comments, err := s.Queries.ListCommentsBySourceTask(ctx, db.ListCommentsBySourceTaskParams{
		TaskID: taskID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return fmt.Errorf("list run comments: %w", err)
	}
	for _, c := range comments {
		text += "\n" + c
	}

	ids := ParseCitedNoteIDs(text)
	if len(ids) == 0 {
		return nil
	}

	cited := make([]NoteVersion, 0, len(ids))
	for _, idStr := range ids {
		noteID, err := util.ParseUUID(idStr)
		if err != nil {
			// Unreachable: citationLinkPattern only ever captures the
			// canonical hex shape util.ParseUUID accepts.
			continue
		}
		note, err := s.Queries.GetWorkspaceNote(ctx, db.GetWorkspaceNoteParams{ID: noteID, WorkspaceID: workspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("note citation: cited note does not exist in this run's workspace",
					"task_id", util.UUIDToString(taskID), "note_id", idStr)
				continue
			}
			return fmt.Errorf("load cited note %s: %w", idStr, err)
		}
		if note.ArchivedAt.Valid {
			slog.Warn("note citation: cited note is archived",
				"task_id", util.UUIDToString(taskID), "note_id", idStr)
			continue
		}
		cited = append(cited, NoteVersion{ID: idStr, Revision: note.Revision})
	}
	if len(cited) == 0 {
		return nil
	}
	RecordRunNoteUsage(ctx, s.Queries, taskID, task.AgentID, "cited", "citation", cited)
	return nil
}
