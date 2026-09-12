package service

// Brain embeddings (OS plan, vague B; per passage since JEF-412): one vector
// per note passage, kept while the passage text is unchanged. Ranked search
// fuses the lexical rank with the nearest-passage rank when the query and the
// stored vectors come from the same model; without an embedding provider the
// Brain ranks lexically and only the lexical indexing here runs.

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
)

const (
	// noteEmbeddingTextCap bounds one embedded text in bytes; a passage is
	// at most 1 200 runes, so only a long title or heading ever reaches it.
	noteEmbeddingTextCap = 6000
	// brainEmbedBatch is how many passages go to the provider in one call.
	brainEmbedBatch = 64
	// brainBackfillIndexLimit bounds the lexical catch-up and the orphan
	// sweep of one backfill tick.
	brainBackfillIndexLimit = 1000
	// brainFloorRetry is how long a failed calibration is remembered, so a
	// provider outage costs one call per 10 minutes, not one per search.
	brainFloorRetry = 10 * time.Minute
)

// NoteEmbedder is the interface the handler and the native tools call. It is
// best-effort: a failure is logged, never surfaced.
type NoteEmbedder interface {
	EmbedNoteAsync(noteID pgtype.UUID)
	QueryEmbedding(ctx context.Context, text string) (literal, model string, ok bool)
	// VectorFloor is the cosine similarity from which a passage only the
	// vector leg finds is admitted into a person's search. ok is false when
	// the model is not calibrated (provider down, or its related and
	// unrelated pairs overlap).
	VectorFloor(ctx context.Context) (float64, bool)
	Enabled() bool
}

// BrainEmbedder indexes notes into passages and embeds those through the
// workspace LLM's embeddings model. LLM may be nil: indexing stays lexical.
type BrainEmbedder struct {
	Queries *db.Queries
	LLM     *llm.Client
	// Timeout bounds one embedding call.
	Timeout time.Duration

	floorMu      sync.Mutex
	floorModel   string
	floor        float64
	floorOK      bool
	floorRetryAt time.Time // set when the last calibration failed
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

func passageEmbeddingText(title, heading, body string) string {
	return util.TruncateUTF8Bytes(strings.TrimSpace(title)+"\n"+heading+"\n\n"+body, noteEmbeddingTextCap)
}

// IndexNote re-cuts one note into passages and, when a provider is
// configured and the note is live, embeds its passages that have no vector
// from the current model, in one call. A note that no longer exists is left
// to the orphan sweep.
func (b *BrainEmbedder) IndexNote(ctx context.Context, noteID pgtype.UUID) error {
	note, err := b.Queries.GetWorkspaceNoteByID(ctx, noteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := replaceNotePassages(ctx, b.Queries, note.ID, note.WorkspaceID, note.Revision, note.Title, note.Content); err != nil {
		return err
	}
	if !b.Enabled() || note.ArchivedAt.Valid {
		return nil
	}
	rows, err := b.Queries.ListPassagesNeedingEmbedding(ctx, db.ListPassagesNeedingEmbeddingParams{
		NoteID: note.ID, EmbeddingModel: b.LLM.EmbeddingModel(), RowLimit: brainPassageMaxCount,
	})
	if err != nil {
		return err
	}
	return b.embedPassages(ctx, rows)
}

func (b *BrainEmbedder) embedPassages(ctx context.Context, rows []db.ListPassagesNeedingEmbeddingRow) error {
	if len(rows) == 0 {
		return nil
	}
	texts := make([]string, len(rows))
	for i, r := range rows {
		texts[i] = passageEmbeddingText(r.Title, r.Heading, r.Body)
	}
	callCtx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	vectors, err := b.LLM.Embed(callCtx, texts)
	if err != nil {
		return err
	}
	if len(vectors) != len(rows) {
		return fmt.Errorf("brain embedding: provider returned %d vectors for %d passages", len(vectors), len(rows))
	}
	model := b.LLM.EmbeddingModel()
	for i, r := range rows {
		if err := b.Queries.SetPassageEmbedding(ctx, db.SetPassageEmbeddingParams{
			NoteID: r.NoteID, Ordinal: r.Ordinal, ContentHash: r.ContentHash,
			Embedding: llm.VectorLiteral(vectors[i]), EmbeddingModel: model,
		}); err != nil {
			return err
		}
	}
	return nil
}

// EmbedNoteAsync indexes and embeds in the background: a note write never
// waits for a provider round trip. Without a provider it does nothing: the
// next search indexes the note lexically, synchronously. The backfill job
// catches whatever this misses.
func (b *BrainEmbedder) EmbedNoteAsync(noteID pgtype.UUID) {
	if !b.Enabled() {
		return
	}
	go func() {
		// Panic containment: a bare goroutine takes the process down.
		defer func() {
			if r := recover(); r != nil {
				slog.Error("brain embedding panicked", "note_id", util.UUIDToString(noteID), "panic", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), b.Timeout+5*time.Second)
		defer cancel()
		if err := b.IndexNote(ctx, noteID); err != nil {
			slog.Warn("brain embedding failed", "note_id", util.UUIDToString(noteID), "error", err)
		}
	}()
}

// QueryEmbedding embeds a search query. ok is false when embeddings are off
// or the provider failed: the caller ranks lexically then.
func (b *BrainEmbedder) QueryEmbedding(ctx context.Context, text string) (string, string, bool) {
	text = strings.TrimSpace(text)
	if !b.Enabled() || text == "" {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	vectors, err := b.LLM.Embed(ctx, []string{util.TruncateUTF8Bytes(text, noteEmbeddingTextCap)})
	if err != nil || len(vectors) != 1 {
		if err != nil {
			slog.Warn("brain query embedding failed", "error", err)
		}
		return "", "", false
	}
	return llm.VectorLiteral(vectors[0]), b.LLM.EmbeddingModel(), true
}

// Backfill is the scheduler's catch-up, in three steps: passages for the
// notes whose index is missing or stale (every workspace, even without a
// provider), passages whose note is gone, then, with a provider, up to limit
// passages without a vector from the current model. Returns how many notes
// were indexed, passages swept and passages embedded.
func (b *BrainEmbedder) Backfill(ctx context.Context, limit int32) (int, error) {
	done, err := indexPendingNotePassages(ctx, b.Queries, pgtype.UUID{}, brainBackfillIndexLimit)
	if err != nil {
		return done, err
	}
	orphans, err := b.Queries.DeleteOrphanNotePassages(ctx, brainBackfillIndexLimit)
	if err != nil {
		return done, err
	}
	done += int(orphans)
	if !b.Enabled() {
		return done, nil
	}
	rows, err := b.Queries.ListPassagesNeedingEmbedding(ctx, db.ListPassagesNeedingEmbeddingParams{EmbeddingModel: b.LLM.EmbeddingModel(), RowLimit: limit})
	if err != nil {
		return done, err
	}
	var firstErr error
	for start := 0; start < len(rows); start += brainEmbedBatch {
		batch := rows[start:min(start+brainEmbedBatch, len(rows))]
		if err := b.embedPassages(ctx, batch); err != nil {
			// One batch that never embeds must not block every older passage
			// on every tick: log it, keep going, report the first failure.
			slog.Warn("brain embedding backfill: batch failed", "passages", len(batch), "error", err)
			if firstErr == nil {
				firstErr = err
			}
			if ctx.Err() != nil {
				return done, ctx.Err()
			}
			continue
		}
		done += len(batch)
	}
	return done, firstErr
}

// VectorFloor calibrates the model once: twelve question/answer pairs on
// unrelated topics, embedded in one call, give the similarity of a real
// answer and of an unrelated passage; the floor sits between the two. A
// failed call is remembered for brainFloorRetry.
func (b *BrainEmbedder) VectorFloor(ctx context.Context) (float64, bool) {
	if !b.Enabled() {
		return 0, false
	}
	model := b.LLM.EmbeddingModel()
	b.floorMu.Lock()
	defer b.floorMu.Unlock()
	if b.floorModel == model && (b.floorRetryAt.IsZero() || time.Now().Before(b.floorRetryAt)) {
		return b.floor, b.floorOK
	}
	b.floorModel, b.floor, b.floorOK, b.floorRetryAt = model, 0, false, time.Time{}

	texts := make([]string, 0, 2*len(brainFloorCalibration))
	for _, pair := range brainFloorCalibration {
		texts = append(texts, pair[0], pair[1])
	}
	// The calibration outlives the search that triggered it.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), b.Timeout)
	defer cancel()
	vectors, err := b.LLM.Embed(callCtx, texts)
	if err == nil && len(vectors) != len(texts) {
		err = fmt.Errorf("provider returned %d vectors for %d texts", len(vectors), len(texts))
	}
	if err != nil {
		slog.Warn("brain vector floor calibration failed", "model", model, "error", err)
		b.floorRetryAt = time.Now().Add(brainFloorRetry)
		return 0, false
	}
	var related, unrelated []float64
	for i := range brainFloorCalibration {
		for j := range brainFloorCalibration {
			sim := cosineSimilarity(vectors[2*i], vectors[2*j+1])
			if i == j {
				related = append(related, sim)
			} else {
				unrelated = append(unrelated, sim)
			}
		}
	}
	b.floor, b.floorOK = calibrateVectorFloor(related, unrelated)
	if !b.floorOK {
		slog.Warn("brain vector floor: related and unrelated pairs overlap; semantic-only results stay off", "model", model)
	}
	return b.floor, b.floorOK
}

func cosineSimilarity(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range min(len(a), len(b)) {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// percentile interpolates linearly between the closest ranks.
func percentile(values []float64, p float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

// calibrateVectorFloor is the midpoint between the 95th percentile of
// unrelated similarities and the 10th percentile of related ones, when the
// second is above the first; otherwise the model cannot tell them apart and
// there is no floor.
func calibrateVectorFloor(related, unrelated []float64) (float64, bool) {
	if len(related) == 0 || len(unrelated) == 0 {
		return 0, false
	}
	high := percentile(unrelated, 0.95)
	low := percentile(related, 0.10)
	if low <= high {
		return 0, false
	}
	return (high + low) / 2, true
}

// brainFloorCalibration pairs a question with the passage that answers it,
// in French, English and Chinese, on topics unrelated to each other.
var brainFloorCalibration = [][2]string{
	{"How long should shaped sourdough proof before baking?", "Let the shaped loaf rise in the fridge overnight, about twelve hours, then bake it straight from cold in a very hot oven."},
	{"Quand faut-il tailler les rosiers ?", "On taille les rosiers à la fin de l'hiver, quand les bourgeons gonflent, en coupant juste au-dessus d'un œil tourné vers l'extérieur."},
	{"猫每天应该喂几次？", "成年猫一般每天喂两次，定时定量，同时要一直准备干净的饮用水。"},
	{"What tyre pressure does a road bike need?", "Road bike tyres usually run between 80 and 100 psi, lower for heavier riders on wider tyres and for wet roads."},
	{"Comment détartrer une bouilloire ?", "Remplissez-la d'un mélange d'eau et de vinaigre blanc, portez à ébullition, laissez agir une heure puis rincez plusieurs fois."},
	{"怎么更换汽车雨刮片？", "把雨刮臂抬起，按下卡扣取下旧刮片，再把新刮片对准卡槽推进去，听到咔哒一声就装好了。"},
	{"Why does a violin go out of tune so often?", "Changes in temperature and humidity make the strings and the wooden body expand or contract, so the pitch drifts."},
	{"Combien de temps un passeport français est-il valable ?", "Le passeport d'un adulte est valable dix ans, celui d'un mineur cinq ans."},
	{"绿茶用多少度的水泡比较好？", "绿茶适合用八十度左右的水冲泡，水太烫会把茶叶烫熟，失去鲜爽的味道。"},
	{"How do I get rid of aphids on tomato plants?", "Spray the leaves with soapy water or release ladybirds, which eat aphids, and check the undersides of the leaves every few days."},
	{"Combien de temps met un amateur pour courir un marathon ?", "Un coureur amateur boucle généralement les 42,195 kilomètres en quatre à cinq heures."},
	{"月食是怎么形成的？", "当地球运行到太阳和月亮中间，地球的影子挡住照向月亮的阳光时，就会发生月食。"},
}
