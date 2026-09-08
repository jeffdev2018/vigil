package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Work item type catalogue API (F30 / JEF-34).
//
// Reading is open to any workspace member — every client needs the catalogue to
// render a type badge. Mutating it is owner/admin only: a type is workspace
// configuration, and the keys it mints are what property scopes reference.
//
// Deliberately NOT modelled on issue_status's category machinery: a type
// carries no platform behavior, so there is no equivalence class to inherit and
// no "effective type" to resolve. `issue.issue_type` holds the key verbatim, or
// NULL for an untyped issue.

// seededIssueTypeKeys mirrors SeedIssueTypeEntries. Kept here so key validation
// can reject reusing one without a database read, exactly as issuestatus does
// for the 7 built-in status keys.
var seededIssueTypeKeys = []string{"bug", "story", "epic", "task"}

const maxIssueTypeKeyLen = 32

var issueTypeKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,31}$`)

func isSeededIssueTypeKey(key string) bool {
	for _, k := range seededIssueTypeKeys {
		if k == key {
			return true
		}
	}
	return false
}

// validateIssueTypeKey checks a proposed custom type key against the storage
// constraint and the reserved seeded names.
func validateIssueTypeKey(key string) (string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return "", errors.New("type key is required")
	}
	if !issueTypeKeyPattern.MatchString(key) {
		return "", errors.New("type key must be 1-32 characters of lowercase letters, digits or underscore, starting with a letter or digit")
	}
	if isSeededIssueTypeKey(key) {
		return "", fmt.Errorf("%q is a built-in type key and cannot be reused", key)
	}
	return key, nil
}

// slugifyIssueTypeName reduces a display name to the ASCII key alphabet,
// returning "" when nothing survives (a name written entirely in a non-Latin
// script). Same rules as issuestatus.slugify — a name and a key are the same
// two concepts here, and two different slug algorithms in one product would
// give "Bug 修复" a different handle depending on which catalogue it landed in.
func slugifyIssueTypeName(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore && b.Len() > 0:
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	slug := strings.Trim(b.String(), "_")
	if len(slug) > maxIssueTypeKeyLen {
		slug = strings.Trim(slug[:maxIssueTypeKeyLen], "_")
	}
	return slug
}

// deriveIssueTypeKey picks the stored key for a type whose creator supplied
// only a display name. A name with no ASCII to slug falls back to "type", which
// the seeded set does not own, so it lands on "type" then "type_2" and so on.
func deriveIssueTypeKey(name string, taken map[string]bool) (string, error) {
	base := slugifyIssueTypeName(name)
	if base == "" {
		base = "type"
	}
	occupied := func(key string) bool { return taken[key] || isSeededIssueTypeKey(key) }
	if !occupied(base) {
		return validateIssueTypeKey(base)
	}
	// Bounded by the catalogue rather than a policy constant: every candidate is
	// distinct and at most len(taken)+4 keys can be occupied, so a free one has
	// to appear within that many attempts plus one.
	limit := len(taken) + len(seededIssueTypeKeys) + 2
	for n := 2; n <= limit; n++ {
		suffix := "_" + strconv.Itoa(n)
		trimmed := base
		if len(trimmed) > maxIssueTypeKeyLen-len(suffix) {
			trimmed = strings.TrimRight(trimmed[:maxIssueTypeKeyLen-len(suffix)], "_")
		}
		candidate := trimmed + suffix
		if !occupied(candidate) {
			return validateIssueTypeKey(candidate)
		}
	}
	return "", errors.New("could not derive an unused type key from that name; provide one explicitly")
}

// ensureIssueTypeCatalog idempotently seeds a workspace's 4 system types.
// Called on read (self-heal for workspaces created before F30) and on workspace
// create, exactly like the status catalogue.
func (h *Handler) ensureIssueTypeCatalog(ctx context.Context, workspaceID pgtype.UUID) error {
	return h.Queries.SeedIssueTypeEntries(ctx, workspaceID)
}

type IssueTypeResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Color       string  `json:"color"`
	Icon        string  `json:"icon"`
	IsSystem    bool    `json:"is_system"`
	Position    float64 `json:"position"`
	ArchivedAt  *string `json:"archived_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func issueTypeToResponse(t db.IssueType) IssueTypeResponse {
	return IssueTypeResponse{
		ID:          uuidToString(t.ID),
		WorkspaceID: uuidToString(t.WorkspaceID),
		Key:         t.Key,
		Name:        t.Name,
		Description: t.Description,
		Color:       t.Color,
		Icon:        t.Icon,
		IsSystem:    t.IsSystem,
		Position:    t.Position,
		ArchivedAt:  timestampToPtr(t.ArchivedAt),
		CreatedAt:   timestampToString(t.CreatedAt),
		UpdatedAt:   timestampToString(t.UpdatedAt),
	}
}

type CreateIssueTypeRequest struct {
	// Key is optional; derived from Name when omitted. Immutable once created,
	// because it is the value stored in issue.issue_type.
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
}

// UpdateIssueTypeRequest has no Key field: it is immutable, because changing it
// would strand every issue already carrying it and every property scoped to it.
type UpdateIssueTypeRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Color       *string  `json:"color"`
	Icon        *string  `json:"icon"`
	Position    *float64 `json:"position"`
}

// ListIssueTypes returns the workspace's type catalogue in display order.
func (h *Handler) ListIssueTypes(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}

	// Self-heal: a workspace created before F30 has no catalogue rows. Seeding
	// on read keeps the endpoint correct during a rolling deploy without a
	// second backfill pass, and is idempotent.
	if err := h.ensureIssueTypeCatalog(r.Context(), wsUUID); err != nil {
		slog.Warn("failed to ensure issue type catalog", append(logger.RequestAttrs(r), "error", err)...)
	}

	includeArchived := strings.EqualFold(r.URL.Query().Get("include_archived"), "true")
	entries, err := h.Queries.ListIssueTypeEntries(r.Context(), db.ListIssueTypeEntriesParams{
		WorkspaceID:     wsUUID,
		IncludeArchived: includeArchived,
	})
	if err != nil {
		slog.Warn("ListIssueTypes failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue types")
		return
	}

	resp := make([]IssueTypeResponse, len(entries))
	for i, e := range entries {
		resp[i] = issueTypeToResponse(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"types": resp,
		"total": len(resp),
	})
}

// CreateIssueType adds a custom type to the workspace catalogue.
func (h *Handler) CreateIssueType(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req CreateIssueTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		writeError(w, http.StatusBadRequest, "name must be 1-64 characters")
		return
	}
	if len([]rune(req.Description)) > 256 {
		writeError(w, http.StatusBadRequest, "description must be at most 256 characters")
		return
	}
	color, err := normalizeColor(req.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	icon, err := validatePropertyIcon(req.Icon)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var explicitKey string
	if strings.TrimSpace(req.Key) != "" {
		explicitKey, err = validateIssueTypeKey(req.Key)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	entry, badRequest, err := h.createIssueTypeEntry(r.Context(), wsUUID, db.CreateIssueTypeEntryParams{
		WorkspaceID: wsUUID,
		Key:         explicitKey,
		Name:        name,
		Description: req.Description,
		Color:       strings.ToLower(color),
		Icon:        icon,
	})
	if badRequest != "" {
		writeError(w, http.StatusBadRequest, badRequest)
		return
	}
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a type with this key or name already exists")
			return
		}
		slog.Warn("CreateIssueType failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue type")
		return
	}
	h.publishIssueTypeChanged(workspaceID, member, "created")
	writeJSON(w, http.StatusCreated, issueTypeToResponse(entry))
}

// createIssueTypeEntry writes one catalogue row, deriving arg.Key from the
// display name when it arrives empty.
//
// Derivation READS the catalogue to pick a free key, so read and insert have to
// be one atomic step: two admins creating a non-Latin-named type at the same
// instant would otherwise both compute `type_2` and the loser would be told a
// key they never typed was taken. The EXCLUSIVE catalogue lock serializes them,
// and EVERY create takes it — including one supplying its own key, which could
// otherwise land between a derive's read and its insert.
func (h *Handler) createIssueTypeEntry(ctx context.Context, workspaceID pgtype.UUID, arg db.CreateIssueTypeEntryParams) (db.IssueType, string, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.IssueType{}, "", err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if err := qtx.LockIssueTypeCatalog(ctx, workspaceID); err != nil {
		return db.IssueType{}, "", err
	}
	// Seed inside the lock so a workspace whose catalogue has never been read
	// cannot mint a custom key that collides with a system one seeded later.
	if err := qtx.SeedIssueTypeEntries(ctx, workspaceID); err != nil {
		return db.IssueType{}, "", err
	}

	if arg.Key == "" {
		// IncludeArchived, because idx_issue_type_workspace_key is NOT partial:
		// a retired type still owns its key.
		entries, err := qtx.ListIssueTypeEntries(ctx, db.ListIssueTypeEntriesParams{
			WorkspaceID:     workspaceID,
			IncludeArchived: true,
		})
		if err != nil {
			return db.IssueType{}, "", err
		}
		taken := make(map[string]bool, len(entries))
		for _, e := range entries {
			taken[e.Key] = true
		}
		key, err := deriveIssueTypeKey(arg.Name, taken)
		if err != nil {
			return db.IssueType{}, err.Error(), nil
		}
		arg.Key = key
	}

	entry, err := qtx.CreateIssueTypeEntry(ctx, arg)
	if err != nil {
		return db.IssueType{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.IssueType{}, "", err
	}
	return entry, "", nil
}

// UpdateIssueType edits a type's presentation. System types ARE editable here,
// unlike built-in statuses: a type carries no platform behavior, so renaming
// "Story" to "Feature" changes nothing but the label. Its key stays, so every
// issue and property scope pointing at it is unaffected.
func (h *Handler) UpdateIssueType(w http.ResponseWriter, r *http.Request) {
	entry, wsUUID, member, ok := h.loadIssueTypeForAdmin(w, r)
	if !ok {
		return
	}

	var req UpdateIssueTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if entry.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "archived types cannot be modified")
		return
	}

	var name pgtype.Text
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" || len([]rune(trimmed)) > 64 {
			writeError(w, http.StatusBadRequest, "name must be 1-64 characters")
			return
		}
		name = pgtype.Text{String: trimmed, Valid: true}
	}
	var description pgtype.Text
	if req.Description != nil {
		if len([]rune(*req.Description)) > 256 {
			writeError(w, http.StatusBadRequest, "description must be at most 256 characters")
			return
		}
		description = pgtype.Text{String: *req.Description, Valid: true}
	}
	var color pgtype.Text
	if req.Color != nil {
		normalized, err := normalizeColor(*req.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		color = pgtype.Text{String: strings.ToLower(normalized), Valid: true}
	}
	var icon pgtype.Text
	if req.Icon != nil {
		validated, err := validatePropertyIcon(*req.Icon)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		icon = pgtype.Text{String: validated, Valid: true}
	}
	var position pgtype.Float8
	if req.Position != nil {
		position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}

	updated, err := h.Queries.UpdateIssueTypeEntry(r.Context(), db.UpdateIssueTypeEntryParams{
		ID:          entry.ID,
		WorkspaceID: wsUUID,
		Name:        name,
		Description: description,
		Color:       color,
		Icon:        icon,
		Position:    position,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "type is no longer editable")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a type with this name already exists")
			return
		}
		slog.Warn("UpdateIssueType failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update issue type")
		return
	}
	h.publishIssueTypeChanged(uuidToString(wsUUID), member, "updated")
	writeJSON(w, http.StatusOK, issueTypeToResponse(updated))
}

// ArchiveIssueType retires a custom type from FUTURE assignment. Issues already
// on it keep it — they still resolve their label and color through the row —
// while nothing new can be assigned to it.
//
// A system type is refused with 409: the four seeded handles are what the API
// docs and agent instructions reference.
func (h *Handler) ArchiveIssueType(w http.ResponseWriter, r *http.Request) {
	entry, wsUUID, member, ok := h.loadIssueTypeForAdmin(w, r)
	if !ok {
		return
	}
	if entry.IsSystem {
		writeErrorCode(w, http.StatusConflict, "issue_type_is_system", "built-in types cannot be archived")
		return
	}
	if entry.ArchivedAt.Valid {
		writeJSON(w, http.StatusOK, issueTypeToResponse(entry))
		return
	}

	// The EXCLUSIVE catalogue lock is what makes "no NEW issue can be assigned
	// an archived type" exact rather than approximate: an issue write naming a
	// type re-resolves it under this lock, so a write cannot interleave between
	// this archive and its own catalogue check.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Warn("ArchiveIssueType begin failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue type")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.LockIssueTypeCatalog(r.Context(), wsUUID); err != nil {
		slog.Warn("ArchiveIssueType lock failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue type")
		return
	}
	archived, err := qtx.ArchiveIssueTypeEntry(r.Context(), db.ArchiveIssueTypeEntryParams{
		ID:          entry.ID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "type is no longer archivable")
			return
		}
		slog.Warn("ArchiveIssueType failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue type")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("ArchiveIssueType commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to archive issue type")
		return
	}
	// After the commit, never before: an event announcing a change the
	// transaction then rolled back would have every other tab cache the
	// pre-archive row as truth.
	h.publishIssueTypeChanged(uuidToString(wsUUID), member, "archived")
	writeJSON(w, http.StatusOK, issueTypeToResponse(archived))
}

func (h *Handler) loadIssueTypeForAdmin(w http.ResponseWriter, r *http.Request) (db.IssueType, pgtype.UUID, db.Member, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.IssueType{}, pgtype.UUID{}, db.Member{}, false
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return db.IssueType{}, pgtype.UUID{}, db.Member{}, false
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue type id")
	if !ok {
		return db.IssueType{}, pgtype.UUID{}, db.Member{}, false
	}
	entry, err := h.Queries.GetIssueTypeEntryByID(r.Context(), db.GetIssueTypeEntryByIDParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue type not found")
			return db.IssueType{}, pgtype.UUID{}, db.Member{}, false
		}
		slog.Warn("load issue type failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load issue type")
		return db.IssueType{}, pgtype.UUID{}, db.Member{}, false
	}
	return entry, wsUUID, member, true
}

// publishIssueTypeChanged announces that the workspace catalogue moved. One
// event for every write: clients re-read the catalogue, they do not merge the
// payload — it is a handful of rows, and merging an entry out of an event means
// reconciling it against a concurrent write the client cannot see.
func (h *Handler) publishIssueTypeChanged(workspaceID string, actor db.Member, action string) {
	h.publish(protocol.EventIssueTypeChanged, workspaceID, "member", uuidToString(actor.UserID), map[string]any{
		"action": action,
	})
}

// ReorderIssueTypesRequest carries the catalogue's ACTIVE types in their new
// order. `ids` must name every one of them: positions are assigned from the
// array index, so reordering a subset would collide with the rows left out.
type ReorderIssueTypesRequest struct {
	IDs []string `json:"ids"`
}

// ReorderIssueTypes rewrites the catalogue order atomically. Everything happens
// inside ONE transaction under the catalogue lock, so a type archived between
// validation and write cannot leave a half-applied order behind.
func (h *Handler) ReorderIssueTypes(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req ReorderIssueTypesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids must not be empty")
		return
	}
	ids := make([]pgtype.UUID, 0, len(req.IDs))
	seen := make(map[string]struct{}, len(req.IDs))
	for _, raw := range req.IDs {
		if _, duplicate := seen[raw]; duplicate {
			writeError(w, http.StatusBadRequest, "duplicate ids")
			return
		}
		seen[raw] = struct{}{}
		idUUID, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue type id")
			return
		}
		ids = append(ids, idUUID)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Warn("ReorderIssueTypes begin failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue types")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.LockIssueTypeCatalog(r.Context(), wsUUID); err != nil {
		slog.Warn("ReorderIssueTypes lock failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue types")
		return
	}

	// The authoritative set, read under the lock. Comparing the payload against
	// it covers every rejection at once: an archived type, another workspace's
	// row, and — the case a per-id check misses — an active type left out.
	active, err := qtx.ListActiveCustomIssueTypeEntries(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("ReorderIssueTypes list failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue types")
		return
	}
	activeIDs := make(map[string]struct{}, len(active))
	for _, entry := range active {
		activeIDs[util.UUIDToString(entry.ID)] = struct{}{}
	}
	for _, raw := range req.IDs {
		if _, isActive := activeIDs[raw]; !isActive {
			writeError(w, http.StatusConflict, "ids must name every active type in the catalogue")
			return
		}
	}
	if len(active) != len(ids) {
		writeError(w, http.StatusConflict, "ids must name every active type in the catalogue")
		return
	}

	affected, err := qtx.ReorderIssueTypeEntries(r.Context(), db.ReorderIssueTypeEntriesParams{
		Ids:         ids,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Warn("ReorderIssueTypes failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue types")
		return
	}
	if affected != int64(len(ids)) {
		slog.Warn("ReorderIssueTypes touched an unexpected row count",
			append(logger.RequestAttrs(r), "affected", affected, "expected", len(ids))...)
		writeError(w, http.StatusConflict, "issue type catalog changed during reorder")
		return
	}

	entries, err := qtx.ListIssueTypeEntries(r.Context(), db.ListIssueTypeEntriesParams{
		WorkspaceID:     wsUUID,
		IncludeArchived: true,
	})
	if err != nil {
		slog.Warn("list issue types after reorder failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue types")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("ReorderIssueTypes commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to reorder issue types")
		return
	}
	h.publishIssueTypeChanged(workspaceID, member, "reordered")

	resp := make([]IssueTypeResponse, len(entries))
	for i, e := range entries {
		resp[i] = issueTypeToResponse(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"types": resp, "total": len(resp)})
}

// resolveIssueTypeKey validates a caller-supplied type against the workspace
// catalogue and returns the key to store.
//
// This is the application-layer replacement for the foreign key the project
// rules forbid, so EVERY write path that touches issue.issue_type must route
// through it — a missed entrypoint is how an unresolvable handle reaches the
// column. Resolution is case- and whitespace-insensitive, and callers must
// persist what it returns rather than the raw input: writing back "  BUG "
// would store a value the column's format CHECK rejects.
//
// Returns ok=false after writing the response. An unknown key is 400, not 404:
// it arrived in a request body as a field value, not as a resource path.
func (h *Handler) resolveIssueTypeKey(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, raw string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		writeError(w, http.StatusBadRequest, "issue_type must not be blank; send null to clear it")
		return "", false
	}
	entry, err := h.Queries.GetIssueTypeEntryByKey(r.Context(), db.GetIssueTypeEntryByKeyParams{
		WorkspaceID: workspaceID,
		Key:         key,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Seed and retry once: a workspace created before F30 has no
			// catalogue until something reads it, and the four system keys must
			// work on the first write as well as the first read.
			if seedErr := h.ensureIssueTypeCatalog(r.Context(), workspaceID); seedErr == nil {
				entry, err = h.Queries.GetIssueTypeEntryByKey(r.Context(), db.GetIssueTypeEntryByKeyParams{
					WorkspaceID: workspaceID,
					Key:         key,
				})
			}
		}
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeErrorCode(w, http.StatusBadRequest, "unknown_issue_type", fmt.Sprintf("unknown issue type %q", key))
				return "", false
			}
			slog.Warn("resolve issue type failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to resolve issue type")
			return "", false
		}
	}
	if entry.ArchivedAt.Valid {
		writeErrorCode(w, http.StatusBadRequest, "archived_issue_type", fmt.Sprintf("issue type %q is archived and cannot be assigned", key))
		return "", false
	}
	return entry.Key, true
}

// issueTypeKeyOf reads an issue's type key, answering "" for an untyped issue.
// The pgtype.Text zero value and an empty string mean the same thing here, and
// callers branch on the string, so folding them once keeps every caller from
// having to remember which of the two it holds.
func issueTypeKeyOf(issue db.Issue) string {
	if !issue.IssueType.Valid {
		return ""
	}
	return issue.IssueType.String
}

// applyIssueTypeWrite resolves and persists a caller-supplied `issue_type`,
// mirroring applyIssueCycleWrite. `raw` nil or blank CLEARS the type back to
// untyped; anything else is validated against the catalogue first.
//
// Written on its own rather than through UpdateIssue for the reason
// SetIssueCycle documents: a bare narg on UpdateIssueParams would be cleared by
// every write that does not mention the type.
//
// Returns false after writing the response.
func (h *Handler) applyIssueTypeWrite(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, issue *db.Issue, raw *string) bool {
	target := pgtype.Text{}
	if raw != nil && strings.TrimSpace(*raw) != "" {
		key, ok := h.resolveIssueTypeKey(w, r, wsUUID, *raw)
		if !ok {
			return false
		}
		target = pgtype.Text{String: key, Valid: true}
	}
	if err := h.Queries.SetIssueIssueType(r.Context(), db.SetIssueIssueTypeParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
		IssueType:   target,
	}); err != nil {
		slog.Warn("set issue type failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to set issue type")
		return false
	}
	issue.IssueType = target
	return true
}
