package service

// Brain embeddings (OS plan, vague B): one vector per live note, beside the
// note, replaced when its content changes. Ranked search fuses the lexical
// rank with the vector rank when the query and the stored vector come from
// the same model; without an embedding provider the Brain ranks lexically
// and nothing here runs.

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
)

// noteEmbeddingTextCap bounds what is embedded: the title and the head of
// the body carry the topic; a 20 000-rune note does not need every line.
const noteEmbeddingTextCap = 6000

// NoteEmbedder is the interface the handler and the native tools call after
// a note write. It is best-effort: a failure is logged, never surfaced.
type NoteEmbedder interface {
	EmbedNoteAsync(noteID pgtype.UUID)
	QueryEmbedding(ctx context.Context, text string) (literal, model string, ok bool)
	Enabled() bool
}

// BrainEmbedder embeds notes through the workspace LLM's embeddings model.
type BrainEmbedder struct {
	Queries *db.Queries
	LLM     *llm.Client
	// Timeout bounds one embedding call.
	Timeout time.Duration
}

func NewBrainEmbedder(q *db.Queries, client *llm.Client) *BrainEmbedder {
	return &BrainEmbedder{Queries: q, LLM: client, Timeout: 30 * time.Second}
}

// Enabled reports whether an embeddings model is configured.
func (b *BrainEmbedder) Enabled() bool {
	return b != nil && b.LLM != nil && b.LLM.EmbeddingsEnabled()
}

// NoteContentHash is the identity of what a stored vector was computed from.
func NoteContentHash(title, content string) string {
	sum := md5.Sum([]byte(title + "\n" + content))
	return hex.EncodeToString(sum[:])
}

func noteEmbeddingText(title, content string) string {
	text := strings.TrimSpace(title) + "\n\n" + strings.TrimSpace(content)
	if len(text) > noteEmbeddingTextCap {
		text = text[:noteEmbeddingTextCap]
	}
	return text
}

// EmbedNote computes and stores the vector of one note. A note that no
// longer exists or is archived drops its vector.
func (b *BrainEmbedder) EmbedNote(ctx context.Context, noteID pgtype.UUID) error {
	if !b.Enabled() {
		return nil
	}
	note, err := b.Queries.GetWorkspaceNoteByID(ctx, noteID)
	if err != nil {
		_ = b.Queries.DeleteWorkspaceNoteEmbedding(ctx, noteID)
		return nil
	}
	if note.ArchivedAt.Valid {
		return b.Queries.DeleteWorkspaceNoteEmbedding(ctx, noteID)
	}
	return b.embedRow(ctx, note.ID, note.WorkspaceID, note.Title, note.Content)
}

func (b *BrainEmbedder) embedRow(ctx context.Context, id, wsID pgtype.UUID, title, content string) error {
	ctx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	vectors, err := b.LLM.Embed(ctx, []string{noteEmbeddingText(title, content)})
	if err != nil {
		return err
	}
	if len(vectors) != 1 {
		return errors.New("brain embedding: provider returned no vector")
	}
	return b.Queries.UpsertWorkspaceNoteEmbedding(ctx, db.UpsertWorkspaceNoteEmbeddingParams{
		NoteID: id, WorkspaceID: wsID, Embedding: llm.VectorLiteral(vectors[0]), EmbeddingModel: b.LLM.EmbeddingModel(), ContentHash: NoteContentHash(title, content),
	})
}

// EmbedNoteAsync embeds in the background: a note write never waits for a
// provider round trip. The backfill job catches whatever this misses.
func (b *BrainEmbedder) EmbedNoteAsync(noteID pgtype.UUID) {
	if !b.Enabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), b.Timeout+5*time.Second)
		defer cancel()
		if err := b.EmbedNote(ctx, noteID); err != nil {
			slog.Warn("brain embedding failed", "note_id", util.UUIDToString(noteID), "error", err)
		}
	}()
}

// QueryEmbedding embeds a search query. ok is false when embeddings are off
// or the provider failed: the caller ranks lexically then.
func (b *BrainEmbedder) QueryEmbedding(ctx context.Context, text string) (string, string, bool) {
	if !b.Enabled() || strings.TrimSpace(text) == "" {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	vectors, err := b.LLM.Embed(ctx, []string{noteEmbeddingText("", text)})
	if err != nil || len(vectors) != 1 {
		if err != nil {
			slog.Warn("brain query embedding failed", "error", err)
		}
		return "", "", false
	}
	return llm.VectorLiteral(vectors[0]), b.LLM.EmbeddingModel(), true
}

// Backfill embeds the notes whose vector is missing, stale or from another
// model, newest first, up to limit. Returns how many were embedded.
func (b *BrainEmbedder) Backfill(ctx context.Context, limit int32) (int, error) {
	if !b.Enabled() {
		return 0, nil
	}
	rows, err := b.Queries.ListWorkspaceNotesNeedingEmbedding(ctx, db.ListWorkspaceNotesNeedingEmbeddingParams{Limit: limit, EmbeddingModel: b.LLM.EmbeddingModel()})
	if err != nil {
		return 0, err
	}
	done := 0
	for _, row := range rows {
		if err := b.embedRow(ctx, row.ID, row.WorkspaceID, row.Title, row.Content); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
