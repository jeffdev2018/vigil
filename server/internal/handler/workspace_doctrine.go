package handler

// Workspace doctrine (OS plan, chantier 22): workspace.context grown up.
//
// The doctrine is the one document a workspace's owners write for every
// agent that works there. The live text stays in workspace.context — every
// daemon claim and the native runtime already read it — and this file adds
// what a governing document needs: a revision ledger, an optional
// second-reviewer flow, restore, a line diff between two revisions, and the
// reports an agent files when a task collides with a rule.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/dbid"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// doctrineMaxBytes matches the cap memoryeval already enforced on
	// workspace.context; the brief carries the doctrine in full.
	doctrineMaxBytes        = 32000
	doctrineNoteMaxRunes    = 500
	doctrineSummaryMaxRunes = 2000
	doctrinePassageMaxRunes = 4000

	DoctrineStatusActive     = "active"
	DoctrineStatusPending    = "pending"
	DoctrineStatusRejected   = "rejected"
	DoctrineStatusSuperseded = "superseded"

	DoctrineReportOpen         = "open"
	DoctrineReportAcknowledged = "acknowledged"
	DoctrineReportDismissed    = "dismissed"

	// InboxTypeDoctrineReview asks the other owners/admins to review a
	// proposed revision, and tells the author how the review went.
	InboxTypeDoctrineReview = "doctrine_review"
	// InboxTypeDoctrineReport tells the owners/admins an agent (or a member)
	// hit a rule it could not follow, or found two rules in conflict.
	InboxTypeDoctrineReport = "doctrine_report"

	AuditDoctrinePublished      = "doctrine.published"
	AuditDoctrineProposed       = "doctrine.proposed"
	AuditDoctrineApproved       = "doctrine.approved"
	AuditDoctrineRejected       = "doctrine.rejected"
	AuditDoctrineReported       = "doctrine.reported"
	AuditDoctrineReportResolved = "doctrine.report_resolved"
)

var doctrineReportKinds = map[string]bool{"conflict": true, "refusal": true, "ambiguity": true}

// DoctrineSettings is the doctrine block of workspace.settings.
type DoctrineSettings struct {
	// RequireReview holds a publication as pending until a second
	// owner/admin approves it. A workspace with a single manager
	// self-approves, otherwise turning this on would freeze the doctrine.
	RequireReview bool `json:"require_review"`
}

func doctrineSettingsOf(settings []byte) DoctrineSettings {
	var s struct {
		Doctrine *DoctrineSettings `json:"doctrine"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.Doctrine == nil {
		return DoctrineSettings{}
	}
	return *s.Doctrine
}

type DoctrineVersionResponse struct {
	ID                   string  `json:"id"`
	Revision             *int32  `json:"revision"`
	Content              string  `json:"content"`
	Status               string  `json:"status"`
	Note                 string  `json:"note"`
	AuthorID             *string `json:"author_id"`
	ReviewedBy           *string `json:"reviewed_by"`
	ReviewedAt           *string `json:"reviewed_at"`
	ReviewNote           string  `json:"review_note"`
	RestoredFromRevision *int32  `json:"restored_from_revision"`
	CreatedAt            string  `json:"created_at"`
	Bytes                int     `json:"bytes"`
}

func doctrineVersionToResponse(v db.WorkspaceDoctrineVersion) DoctrineVersionResponse {
	out := DoctrineVersionResponse{
		ID: uuidToString(v.ID), Content: v.Content, Status: v.Status, Note: v.Note,
		AuthorID: uuidToPtr(v.AuthorID), ReviewedBy: uuidToPtr(v.ReviewedBy), ReviewedAt: timestampToPtr(v.ReviewedAt),
		ReviewNote: v.ReviewNote, CreatedAt: timestampToString(v.CreatedAt), Bytes: len(v.Content),
	}
	if v.Revision.Valid {
		rev := v.Revision.Int32
		out.Revision = &rev
	}
	if v.RestoredFromRevision.Valid {
		rev := v.RestoredFromRevision.Int32
		out.RestoredFromRevision = &rev
	}
	return out
}

// DoctrineResponse is what GET /api/workspace/doctrine returns: the live
// text, its revision, the review policy, and what is waiting on a person.
type DoctrineResponse struct {
	Content         string                   `json:"content"`
	Revision        int32                    `json:"revision"`
	UpdatedAt       *string                  `json:"updated_at"`
	UpdatedBy       *string                  `json:"updated_by"`
	ByteLimit       int                      `json:"byte_limit"`
	RequireReview   bool                     `json:"require_review"`
	CanPublish      bool                     `json:"can_publish"`
	ActiveVersionID *string                  `json:"active_version_id"`
	Pending         *DoctrineVersionResponse `json:"pending"`
	OpenReports     int64                    `json:"open_reports"`
}

type DoctrineReportResponse struct {
	ID               string  `json:"id"`
	DoctrineRevision int32   `json:"doctrine_revision"`
	Kind             string  `json:"kind"`
	Summary          string  `json:"summary"`
	Passage          string  `json:"passage"`
	ReporterType     string  `json:"reporter_type"`
	ReporterID       string  `json:"reporter_id"`
	TaskID           *string `json:"task_id"`
	IssueID          *string `json:"issue_id"`
	Status           string  `json:"status"`
	ResolvedBy       *string `json:"resolved_by"`
	ResolvedAt       *string `json:"resolved_at"`
	ResolutionNote   string  `json:"resolution_note"`
	CreatedAt        string  `json:"created_at"`
}

func doctrineReportToResponse(x db.WorkspaceDoctrineReport) DoctrineReportResponse {
	return DoctrineReportResponse{
		ID: uuidToString(x.ID), DoctrineRevision: x.DoctrineRevision, Kind: x.Kind, Summary: x.Summary, Passage: x.Passage,
		ReporterType: x.ReporterType, ReporterID: uuidToString(x.ReporterID), TaskID: uuidToPtr(x.TaskID), IssueID: uuidToPtr(x.IssueID),
		Status: x.Status, ResolvedBy: uuidToPtr(x.ResolvedBy), ResolvedAt: timestampToPtr(x.ResolvedAt), ResolutionNote: x.ResolutionNote,
		CreatedAt: timestampToString(x.CreatedAt),
	}
}

func isWorkspaceManager(m db.Member) bool { return m.Role == "owner" || m.Role == "admin" }

// loadDoctrineWorkspace resolves the workspace and the caller's membership.
func (h *Handler) loadDoctrineWorkspace(w http.ResponseWriter, r *http.Request) (pgtype.UUID, db.Member, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, db.Member{}, false
	}
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return pgtype.UUID{}, db.Member{}, false
	}
	return wsUUID, member, true
}

func (h *Handler) requireDoctrineManager(w http.ResponseWriter, r *http.Request) (pgtype.UUID, db.Member, bool) {
	wsUUID, member, ok := h.loadDoctrineWorkspace(w, r)
	if !ok {
		return pgtype.UUID{}, db.Member{}, false
	}
	if !isWorkspaceManager(member) {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can change the doctrine")
		return pgtype.UUID{}, db.Member{}, false
	}
	return wsUUID, member, true
}

func (h *Handler) doctrineResponse(ctx context.Context, wsUUID pgtype.UUID, member db.Member) (DoctrineResponse, error) {
	ws, err := h.Queries.GetWorkspaceDoctrine(ctx, wsUUID)
	if err != nil {
		return DoctrineResponse{}, err
	}
	out := DoctrineResponse{
		Content: ws.Context.String, Revision: ws.DoctrineRevision, UpdatedAt: timestampToPtr(ws.DoctrineUpdatedAt), UpdatedBy: uuidToPtr(ws.DoctrineUpdatedBy),
		ByteLimit: doctrineMaxBytes, RequireReview: doctrineSettingsOf(ws.Settings).RequireReview, CanPublish: isWorkspaceManager(member),
	}
	if active, err := h.Queries.GetActiveDoctrineVersion(ctx, wsUUID); err == nil {
		id := uuidToString(active.ID)
		out.ActiveVersionID = &id
	}
	if pending, err := h.Queries.GetPendingDoctrineVersion(ctx, wsUUID); err == nil {
		v := doctrineVersionToResponse(pending)
		out.Pending = &v
	}
	if n, err := h.Queries.CountOpenDoctrineReports(ctx, wsUUID); err == nil {
		out.OpenReports = n
	}
	return out, nil
}

// GetWorkspaceDoctrine returns the live doctrine to any member.
func (h *Handler) GetWorkspaceDoctrine(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, ok := h.loadDoctrineWorkspace(w, r)
	if !ok {
		return
	}
	resp, err := h.doctrineResponse(r.Context(), wsUUID, member)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the doctrine")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// doctrinePublication is one attempt to change the doctrine, whatever door
// it came through (PUT, restore, the legacy workspace context field, CLI).
type doctrinePublication struct {
	Content          string
	Note             string
	ExpectedRevision *int32 // nil = last writer wins (legacy workspace update)
	RestoredFrom     pgtype.Int4
}

type doctrineError struct {
	status int
	msg    string
}

func (e *doctrineError) Error() string { return e.msg }

func doctrineFail(status int, msg string) error { return &doctrineError{status: status, msg: msg} }

func writeDoctrineError(w http.ResponseWriter, err error) {
	var de *doctrineError
	if errors.As(err, &de) {
		writeError(w, de.status, de.msg)
		return
	}
	writeError(w, http.StatusInternalServerError, "failed to publish the doctrine")
}

func normalizeDoctrineContent(raw string) (string, error) {
	content := strings.TrimRight(util.SanitizeTextForPostgres(strings.ReplaceAll(raw, "\r\n", "\n")), " \t\r\n")
	if len(content) > doctrineMaxBytes {
		return "", doctrineFail(http.StatusBadRequest, fmt.Sprintf("the doctrine is limited to %d bytes", doctrineMaxBytes))
	}
	return content, nil
}

func normalizeDoctrineNote(raw string, max int, field string) (string, error) {
	note := strings.TrimSpace(util.SanitizeTextForPostgres(raw))
	if utf8.RuneCountInString(note) > max {
		return "", doctrineFail(http.StatusBadRequest, fmt.Sprintf("%s is limited to %d characters", field, max))
	}
	return note, nil
}

// publishDoctrine applies one publication under the workspace lock. When the
// workspace requires a second reviewer and has one, the version is filed as
// pending and the live text does not move; otherwise it activates at once.
// Returns the version row and the HTTP status the caller should answer with
// (200 active, 202 pending).
func (h *Handler) publishDoctrine(ctx context.Context, wsUUID pgtype.UUID, author db.Member, pub doctrinePublication) (db.WorkspaceDoctrineVersion, int, error) {
	content, err := normalizeDoctrineContent(pub.Content)
	if err != nil {
		return db.WorkspaceDoctrineVersion{}, 0, err
	}
	note, err := normalizeDoctrineNote(pub.Note, doctrineNoteMaxRunes, "note")
	if err != nil {
		return db.WorkspaceDoctrineVersion{}, 0, err
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.WorkspaceDoctrineVersion{}, 0, err
	}
	defer tx.Rollback(ctx)
	q := h.Queries.WithTx(tx)
	ws, err := q.LockWorkspaceForDoctrine(ctx, wsUUID)
	if err != nil {
		return db.WorkspaceDoctrineVersion{}, 0, doctrineFail(http.StatusConflict, "workspace is unavailable")
	}
	if pub.ExpectedRevision != nil && *pub.ExpectedRevision != ws.DoctrineRevision {
		return db.WorkspaceDoctrineVersion{}, 0, doctrineFail(http.StatusConflict, "the doctrine changed; reload before publishing")
	}
	if _, err := q.GetPendingDoctrineVersion(ctx, wsUUID); err == nil {
		return db.WorkspaceDoctrineVersion{}, 0, doctrineFail(http.StatusConflict, "a proposed revision is awaiting review; approve or reject it first")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.WorkspaceDoctrineVersion{}, 0, err
	}
	if content == strings.TrimRight(ws.Context.String, " \t\r\n") {
		return db.WorkspaceDoctrineVersion{}, 0, doctrineFail(http.StatusBadRequest, "the doctrine is unchanged")
	}
	managers, err := q.ListWorkspaceManagerUserIDs(ctx, wsUUID)
	if err != nil {
		return db.WorkspaceDoctrineVersion{}, 0, err
	}
	others := make([]pgtype.UUID, 0, len(managers))
	for _, id := range managers {
		if id != author.UserID {
			others = append(others, id)
		}
	}
	if doctrineSettingsOf(ws.Settings).RequireReview && len(others) > 0 {
		version, err := q.CreateDoctrineVersion(ctx, db.CreateDoctrineVersionParams{
			WorkspaceID: wsUUID, Content: content, Status: DoctrineStatusPending, Note: note, AuthorID: author.UserID, RestoredFromRevision: pub.RestoredFrom,
		})
		if err != nil {
			return db.WorkspaceDoctrineVersion{}, 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return db.WorkspaceDoctrineVersion{}, 0, err
		}
		h.audit(ctx, wsUUID, "member", uuidToString(author.UserID), AuditDoctrineProposed, "workspace_doctrine_version", version.ID, map[string]any{"bytes": len(content), "note": note, "restored_from_revision": pub.RestoredFrom}, nil)
		h.notifyDoctrineReview(ctx, wsUUID, others, author, version, "proposed")
		h.publish(protocol.EventDoctrineChanged, uuidToString(wsUUID), "member", uuidToString(author.UserID), map[string]any{"revision": ws.DoctrineRevision, "pending_version_id": uuidToString(version.ID), "change": "proposed"})
		return version, http.StatusAccepted, nil
	}
	revision := ws.DoctrineRevision + 1
	version, err := q.CreateDoctrineVersion(ctx, db.CreateDoctrineVersionParams{
		WorkspaceID: wsUUID, Revision: pgtype.Int4{Int32: revision, Valid: true}, Content: content, Status: DoctrineStatusPending, Note: note,
		AuthorID: author.UserID, RestoredFromRevision: pub.RestoredFrom,
	})
	if err != nil {
		return db.WorkspaceDoctrineVersion{}, 0, err
	}
	version, err = h.activateDoctrineVersion(ctx, q, wsUUID, version, author.UserID, "")
	if err != nil {
		return db.WorkspaceDoctrineVersion{}, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.WorkspaceDoctrineVersion{}, 0, err
	}
	h.audit(ctx, wsUUID, "member", uuidToString(author.UserID), AuditDoctrinePublished, "workspace_doctrine_version", version.ID, map[string]any{"revision": revision, "bytes": len(content), "note": note, "restored_from_revision": pub.RestoredFrom}, nil)
	h.publish(protocol.EventDoctrineChanged, uuidToString(wsUUID), "member", uuidToString(author.UserID), map[string]any{"revision": revision, "change": "published"})
	return version, http.StatusOK, nil
}

// activateDoctrineVersion makes a pending version the live doctrine: the
// previous active version is superseded, the workspace row moves, and the
// version records who approved it. Runs inside the caller's transaction.
func (h *Handler) activateDoctrineVersion(ctx context.Context, q *db.Queries, wsUUID pgtype.UUID, version db.WorkspaceDoctrineVersion, reviewer pgtype.UUID, reviewNote string) (db.WorkspaceDoctrineVersion, error) {
	ws, err := q.LockWorkspaceForDoctrine(ctx, wsUUID)
	if err != nil {
		return db.WorkspaceDoctrineVersion{}, err
	}
	revision := ws.DoctrineRevision + 1
	if err := q.SupersedeActiveDoctrineVersion(ctx, wsUUID); err != nil {
		return db.WorkspaceDoctrineVersion{}, err
	}
	activated, err := q.ReviewDoctrineVersion(ctx, db.ReviewDoctrineVersionParams{
		ID: version.ID, WorkspaceID: wsUUID, Status: DoctrineStatusActive, Revision: pgtype.Int4{Int32: revision, Valid: true}, ReviewedBy: reviewer, ReviewNote: reviewNote,
	})
	if err != nil {
		return db.WorkspaceDoctrineVersion{}, err
	}
	if err := q.ActivateWorkspaceDoctrine(ctx, db.ActivateWorkspaceDoctrineParams{
		ID: wsUUID, Context: pgtype.Text{String: activated.Content, Valid: true}, DoctrineRevision: revision, DoctrineUpdatedBy: reviewer,
	}); err != nil {
		return db.WorkspaceDoctrineVersion{}, err
	}
	return activated, nil
}

// notifyDoctrineReview files an inbox item for the people a review concerns:
// the other managers when a revision is proposed, the author when it is
// decided.
func (h *Handler) notifyDoctrineReview(ctx context.Context, wsUUID pgtype.UUID, recipients []pgtype.UUID, actor db.Member, version db.WorkspaceDoctrineVersion, outcome string) {
	var title, body string
	switch outcome {
	case "proposed":
		title = "Doctrine revision proposed"
		body = "A new doctrine revision is waiting for your review."
	case "approved":
		title = "Doctrine revision approved"
		body = fmt.Sprintf("Your doctrine revision is now live as revision %d.", version.Revision.Int32)
	default:
		title = "Doctrine revision rejected"
		body = "Your doctrine revision was rejected."
	}
	if note := strings.TrimSpace(version.ReviewNote); note != "" && outcome != "proposed" {
		body += " Note: " + note
	} else if note := strings.TrimSpace(version.Note); note != "" && outcome == "proposed" {
		body += " Note: " + note
	}
	details, _ := json.Marshal(map[string]any{"version_id": uuidToString(version.ID), "outcome": outcome, "revision": version.Revision})
	var pushTo []pgtype.UUID
	for _, uid := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: wsUUID, RecipientType: "member", RecipientID: uid, Type: InboxTypeDoctrineReview, Severity: "attention",
			Title: title, Body: pgtype.Text{String: body, Valid: true}, ActorType: pgtype.Text{String: "member", Valid: true}, ActorID: actor.UserID, Details: details,
		})
		if err != nil {
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(wsUUID), "member", uuidToString(actor.UserID), map[string]any{"item": inboxToResponse(item)})
		pushTo = append(pushTo, uid)
	}
	if len(pushTo) > 0 {
		h.pushToUsers(ctx, wsUUID, pushTo, title, body, map[string]any{"kind": InboxTypeDoctrineReview, "version_id": uuidToString(version.ID)})
	}
}

// UpdateWorkspaceDoctrine publishes a new revision (or proposes one).
func (h *Handler) UpdateWorkspaceDoctrine(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, ok := h.requireDoctrineManager(w, r)
	if !ok {
		return
	}
	var req struct {
		Content          *string `json:"content"`
		ExpectedRevision *int32  `json:"expected_revision"`
		Note             string  `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*doctrineMaxBytes+4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == nil || req.ExpectedRevision == nil || *req.ExpectedRevision < 0 {
		writeError(w, http.StatusBadRequest, "content and expected_revision are required")
		return
	}
	version, status, err := h.publishDoctrine(r.Context(), wsUUID, member, doctrinePublication{Content: *req.Content, Note: req.Note, ExpectedRevision: req.ExpectedRevision})
	if err != nil {
		writeDoctrineError(w, err)
		return
	}
	h.writeDoctrineWithVersion(w, r, wsUUID, member, version, status)
}

func (h *Handler) writeDoctrineWithVersion(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, member db.Member, version db.WorkspaceDoctrineVersion, status int) {
	resp, err := h.doctrineResponse(r.Context(), wsUUID, member)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the doctrine")
		return
	}
	writeJSON(w, status, struct {
		Doctrine DoctrineResponse        `json:"doctrine"`
		Version  DoctrineVersionResponse `json:"version"`
	}{resp, doctrineVersionToResponse(version)})
}

func (h *Handler) loadDoctrineVersion(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.WorkspaceDoctrineVersion, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "version id")
	if !ok {
		return db.WorkspaceDoctrineVersion{}, false
	}
	version, err := h.Queries.GetDoctrineVersion(r.Context(), db.GetDoctrineVersionParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "doctrine version not found")
		return db.WorkspaceDoctrineVersion{}, false
	}
	return version, true
}

// GetDoctrineVersion returns one version with its content.
func (h *Handler) GetDoctrineVersion(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.loadDoctrineWorkspace(w, r)
	if !ok {
		return
	}
	version, ok := h.loadDoctrineVersion(w, r, wsUUID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Version DoctrineVersionResponse `json:"version"`
	}{doctrineVersionToResponse(version)})
}

func encodeDoctrineCursor(v db.WorkspaceDoctrineVersion) string {
	return base64.RawURLEncoding.EncodeToString([]byte(v.CreatedAt.Time.UTC().Format(time.RFC3339Nano) + "|" + uuidToString(v.ID)))
}

// ListDoctrineVersions pages the ledger newest first, every status included.
func (h *Handler) ListDoctrineVersions(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.loadDoctrineWorkspace(w, r)
	if !ok {
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	params := db.ListDoctrineVersionsParams{WorkspaceID: wsUUID, Limit: int32(limit + 1)}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		at, id, ok := decodeRunsCursor(cursor)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		params.BeforeCreatedAt, params.BeforeID = at, id
	}
	rows, err := h.Queries.ListDoctrineVersions(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the doctrine history")
		return
	}
	resp := struct {
		Versions   []DoctrineVersionResponse `json:"versions"`
		NextCursor *string                   `json:"next_cursor"`
	}{Versions: []DoctrineVersionResponse{}}
	if len(rows) > limit {
		rows = rows[:limit]
		next := encodeDoctrineCursor(rows[limit-1])
		resp.NextCursor = &next
	}
	for _, row := range rows {
		resp.Versions = append(resp.Versions, doctrineVersionToResponse(row))
	}
	writeJSON(w, http.StatusOK, resp)
}

// DoctrineDiffLine is one line of a two-revision comparison.
type DoctrineDiffLine struct {
	Kind string `json:"kind"` // same | add | del
	Text string `json:"text"`
}

// diffDoctrineLines is a plain LCS line diff: the doctrine is capped at
// 32 000 bytes, so the quadratic table stays small.
func diffDoctrineLines(from, to string) (lines []DoctrineDiffLine, added, removed int) {
	a := splitDoctrineLines(from)
	b := splitDoctrineLines(to)
	n, m := len(a), len(b)
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	lines = []DoctrineDiffLine{}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			lines = append(lines, DoctrineDiffLine{Kind: "same", Text: a[i]})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			lines = append(lines, DoctrineDiffLine{Kind: "del", Text: a[i]})
			removed++
			i++
		default:
			lines = append(lines, DoctrineDiffLine{Kind: "add", Text: b[j]})
			added++
			j++
		}
	}
	for ; i < n; i++ {
		lines = append(lines, DoctrineDiffLine{Kind: "del", Text: a[i]})
		removed++
	}
	for ; j < m; j++ {
		lines = append(lines, DoctrineDiffLine{Kind: "add", Text: b[j]})
		added++
	}
	return lines, added, removed
}

func splitDoctrineLines(s string) []string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// DiffDoctrineVersion compares a version with another one (`against`, a
// version id) or, by default, with the live doctrine.
func (h *Handler) DiffDoctrineVersion(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.loadDoctrineWorkspace(w, r)
	if !ok {
		return
	}
	to, ok := h.loadDoctrineVersion(w, r, wsUUID)
	if !ok {
		return
	}
	var from db.WorkspaceDoctrineVersion
	if against := r.URL.Query().Get("against"); against != "" {
		id, ok := parseUUIDOrBadRequest(w, against, "against")
		if !ok {
			return
		}
		v, err := h.Queries.GetDoctrineVersion(r.Context(), db.GetDoctrineVersionParams{ID: id, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "doctrine version not found")
			return
		}
		from = v
	} else {
		// A version compared with nothing named is compared with what came
		// before it: the live doctrine for a proposal, the previous revision
		// for an activated one.
		var err error
		if to.Status == DoctrineStatusPending || to.Status == DoctrineStatusRejected {
			from, err = h.Queries.GetActiveDoctrineVersion(r.Context(), wsUUID)
		} else if to.Revision.Valid && to.Revision.Int32 > 0 {
			from, err = h.Queries.GetDoctrineVersionByRevision(r.Context(), db.GetDoctrineVersionByRevisionParams{WorkspaceID: wsUUID, Revision: pgtype.Int4{Int32: to.Revision.Int32 - 1, Valid: true}})
		} else {
			err = pgx.ErrNoRows
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to read the doctrine history")
			return
		}
	}
	lines, added, removed := diffDoctrineLines(from.Content, to.Content)
	var fromResp *DoctrineVersionResponse
	if from.ID.Valid {
		v := doctrineVersionToResponse(from)
		v.Content = ""
		fromResp = &v
	}
	toResp := doctrineVersionToResponse(to)
	toResp.Content = ""
	writeJSON(w, http.StatusOK, struct {
		From    *DoctrineVersionResponse `json:"from"`
		To      DoctrineVersionResponse  `json:"to"`
		Lines   []DoctrineDiffLine       `json:"lines"`
		Added   int                      `json:"added"`
		Removed int                      `json:"removed"`
	}{fromResp, toResp, lines, added, removed})
}

// reviewDoctrineVersion is approve and reject: a pending version, a manager
// who is not its author (unless nobody else could review it), one decision.
func (h *Handler) reviewDoctrineVersion(w http.ResponseWriter, r *http.Request, approve bool) {
	wsUUID, member, ok := h.requireDoctrineManager(w, r)
	if !ok {
		return
	}
	version, ok := h.loadDoctrineVersion(w, r, wsUUID)
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	note, err := normalizeDoctrineNote(req.Note, doctrineNoteMaxRunes, "note")
	if err != nil {
		writeDoctrineError(w, err)
		return
	}
	if version.Status != DoctrineStatusPending {
		writeError(w, http.StatusConflict, "this version is not awaiting review")
		return
	}
	if version.AuthorID.Valid && version.AuthorID == member.UserID {
		managers, err := h.Queries.ListWorkspaceManagerUserIDs(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read the workspace managers")
			return
		}
		for _, id := range managers {
			if id != member.UserID {
				writeError(w, http.StatusForbidden, "a proposal is reviewed by another owner or admin")
				return
			}
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to review the doctrine")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	var reviewed db.WorkspaceDoctrineVersion
	if approve {
		reviewed, err = h.activateDoctrineVersion(r.Context(), q, wsUUID, version, member.UserID, note)
	} else {
		reviewed, err = q.ReviewDoctrineVersion(r.Context(), db.ReviewDoctrineVersionParams{ID: version.ID, WorkspaceID: wsUUID, Status: DoctrineStatusRejected, ReviewedBy: member.UserID, ReviewNote: note})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to review the doctrine")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to review the doctrine")
		return
	}
	action, outcome, change := AuditDoctrineRejected, "rejected", "rejected"
	if approve {
		action, outcome, change = AuditDoctrineApproved, "approved", "published"
	}
	h.audit(r.Context(), wsUUID, "member", uuidToString(member.UserID), action, "workspace_doctrine_version", reviewed.ID, map[string]any{"revision": reviewed.Revision, "note": note, "author_id": uuidToPtr(reviewed.AuthorID)}, nil)
	if reviewed.AuthorID.Valid && reviewed.AuthorID != member.UserID {
		h.notifyDoctrineReview(r.Context(), wsUUID, []pgtype.UUID{reviewed.AuthorID}, member, reviewed, outcome)
	}
	h.publish(protocol.EventDoctrineChanged, uuidToString(wsUUID), "member", uuidToString(member.UserID), map[string]any{"revision": reviewed.Revision, "version_id": uuidToString(reviewed.ID), "change": change})
	h.writeDoctrineWithVersion(w, r, wsUUID, member, reviewed, http.StatusOK)
}

func (h *Handler) ApproveDoctrineVersion(w http.ResponseWriter, r *http.Request) {
	h.reviewDoctrineVersion(w, r, true)
}

func (h *Handler) RejectDoctrineVersion(w http.ResponseWriter, r *http.Request) {
	h.reviewDoctrineVersion(w, r, false)
}

// RestoreDoctrineVersion publishes an earlier revision's text as a new
// revision, through the same review policy as any publication.
func (h *Handler) RestoreDoctrineVersion(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, ok := h.requireDoctrineManager(w, r)
	if !ok {
		return
	}
	version, ok := h.loadDoctrineVersion(w, r, wsUUID)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision *int32 `json:"expected_revision"`
		Note             string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ExpectedRevision == nil || *req.ExpectedRevision < 0 {
		writeError(w, http.StatusBadRequest, "expected_revision is required")
		return
	}
	if !version.Revision.Valid {
		writeError(w, http.StatusConflict, "only a revision that was live can be restored")
		return
	}
	restored, status, err := h.publishDoctrine(r.Context(), wsUUID, member, doctrinePublication{Content: version.Content, Note: req.Note, ExpectedRevision: req.ExpectedRevision, RestoredFrom: version.Revision})
	if err != nil {
		writeDoctrineError(w, err)
		return
	}
	h.writeDoctrineWithVersion(w, r, wsUUID, member, restored, status)
}

// CreateDoctrineReport records that a rule could not be followed, two rules
// collided, or a rule was too vague to act on. Agents file it through their
// run token (the task and its issue ride along); members can file one too.
func (h *Handler) CreateDoctrineReport(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, ok := h.loadDoctrineWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Kind    string  `json:"kind"`
		Summary string  `json:"summary"`
		Passage string  `json:"passage"`
		IssueID *string `json:"issue_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !doctrineReportKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, "kind must be conflict, refusal or ambiguity")
		return
	}
	summary, err := normalizeDoctrineNote(req.Summary, doctrineSummaryMaxRunes, "summary")
	if err != nil || summary == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("summary is required and limited to %d characters", doctrineSummaryMaxRunes))
		return
	}
	passage, err := normalizeDoctrineNote(req.Passage, doctrinePassageMaxRunes, "passage")
	if err != nil {
		writeDoctrineError(w, err)
		return
	}
	actorType, actorID := h.resolveActor(r, uuidToString(member.UserID), uuidToString(wsUUID))
	params := db.CreateDoctrineReportParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Kind: req.Kind, Summary: summary, Passage: passage, ReporterType: actorType, ReporterID: parseUUID(actorID)}
	if actorType == "agent" {
		if taskUUID, err := util.ParseUUID(r.Header.Get("X-Task-ID")); err == nil {
			params.TaskID = taskUUID
			if task, err := h.Queries.GetAgentTask(r.Context(), taskUUID); err == nil && task.IssueID.Valid {
				params.IssueID = task.IssueID
			}
		}
	}
	if req.IssueID != nil && *req.IssueID != "" {
		issueUUID, ok := parseUUIDOrBadRequest(w, *req.IssueID, "issue_id")
		if !ok {
			return
		}
		if issue, err := h.Queries.GetIssue(r.Context(), issueUUID); err != nil || issue.WorkspaceID != wsUUID {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		params.IssueID = issueUUID
	}
	ws, err := h.Queries.GetWorkspaceDoctrine(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the doctrine")
		return
	}
	params.DoctrineRevision = ws.DoctrineRevision
	report, err := h.Queries.CreateDoctrineReport(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to file the report")
		return
	}
	h.audit(r.Context(), wsUUID, actorType, actorID, AuditDoctrineReported, "workspace_doctrine_report", report.ID, map[string]any{"kind": report.Kind, "revision": report.DoctrineRevision, "task_id": uuidToPtr(report.TaskID), "issue_id": uuidToPtr(report.IssueID)}, nil)
	h.notifyDoctrineReport(r.Context(), report, actorType, actorID)
	h.publish(protocol.EventDoctrineChanged, uuidToString(wsUUID), actorType, actorID, map[string]any{"revision": ws.DoctrineRevision, "report_id": uuidToString(report.ID), "change": "reported"})
	writeJSON(w, http.StatusCreated, struct {
		Report DoctrineReportResponse `json:"report"`
	}{doctrineReportToResponse(report)})
}

func (h *Handler) notifyDoctrineReport(ctx context.Context, report db.WorkspaceDoctrineReport, actorType, actorID string) {
	managers, err := h.Queries.ListWorkspaceManagerUserIDs(ctx, report.WorkspaceID)
	if err != nil {
		return
	}
	title := "Doctrine " + report.Kind + " reported"
	body := report.Summary
	details, _ := json.Marshal(map[string]any{"report_id": uuidToString(report.ID), "kind": report.Kind, "revision": report.DoctrineRevision, "task_id": uuidToPtr(report.TaskID)})
	var pushTo []pgtype.UUID
	for _, uid := range managers {
		if actorType == "member" && uuidToString(uid) == actorID {
			continue
		}
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: report.WorkspaceID, RecipientType: "member", RecipientID: uid, Type: InboxTypeDoctrineReport, Severity: "attention",
			IssueID: report.IssueID, Title: title, Body: pgtype.Text{String: body, Valid: true}, ActorType: pgtype.Text{String: actorType, Valid: true}, ActorID: parseUUID(actorID), Details: details,
		})
		if err != nil {
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(report.WorkspaceID), actorType, actorID, map[string]any{"item": inboxToResponse(item)})
		pushTo = append(pushTo, uid)
	}
	if len(pushTo) > 0 {
		h.pushToUsers(ctx, report.WorkspaceID, pushTo, title, body, map[string]any{"kind": InboxTypeDoctrineReport, "report_id": uuidToString(report.ID)})
	}
}

// ListDoctrineReports lists reports, open ones by default (`status=all` for every status).
func (h *Handler) ListDoctrineReports(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.loadDoctrineWorkspace(w, r)
	if !ok {
		return
	}
	params := db.ListDoctrineReportsParams{WorkspaceID: wsUUID, Limit: 200}
	switch status := r.URL.Query().Get("status"); status {
	case "", DoctrineReportOpen:
		params.Status = pgtype.Text{String: DoctrineReportOpen, Valid: true}
	case DoctrineReportAcknowledged, DoctrineReportDismissed:
		params.Status = pgtype.Text{String: status, Valid: true}
	case "all":
	default:
		writeError(w, http.StatusBadRequest, "status must be open, acknowledged, dismissed or all")
		return
	}
	rows, err := h.Queries.ListDoctrineReports(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the reports")
		return
	}
	out := make([]DoctrineReportResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, doctrineReportToResponse(row))
	}
	writeJSON(w, http.StatusOK, struct {
		Reports []DoctrineReportResponse `json:"reports"`
	}{out})
}

func (h *Handler) resolveDoctrineReport(w http.ResponseWriter, r *http.Request, status string) {
	wsUUID, member, ok := h.requireDoctrineManager(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "report id")
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	note, err := normalizeDoctrineNote(req.Note, doctrineNoteMaxRunes, "note")
	if err != nil {
		writeDoctrineError(w, err)
		return
	}
	report, err := h.Queries.ResolveDoctrineReport(r.Context(), db.ResolveDoctrineReportParams{ID: id, WorkspaceID: wsUUID, Status: status, ResolvedBy: member.UserID, ResolutionNote: note})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, lookup := h.Queries.GetDoctrineReport(r.Context(), db.GetDoctrineReportParams{ID: id, WorkspaceID: wsUUID}); lookup == nil {
			writeError(w, http.StatusConflict, "this report is already resolved")
		} else {
			writeError(w, http.StatusNotFound, "report not found")
		}
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve the report")
		return
	}
	h.audit(r.Context(), wsUUID, "member", uuidToString(member.UserID), AuditDoctrineReportResolved, "workspace_doctrine_report", report.ID, map[string]any{"status": status, "note": note}, nil)
	h.publish(protocol.EventDoctrineChanged, uuidToString(wsUUID), "member", uuidToString(member.UserID), map[string]any{"revision": report.DoctrineRevision, "report_id": uuidToString(report.ID), "change": "report_" + status})
	writeJSON(w, http.StatusOK, struct {
		Report DoctrineReportResponse `json:"report"`
	}{doctrineReportToResponse(report)})
}

func (h *Handler) AcknowledgeDoctrineReport(w http.ResponseWriter, r *http.Request) {
	h.resolveDoctrineReport(w, r, DoctrineReportAcknowledged)
}

func (h *Handler) DismissDoctrineReport(w http.ResponseWriter, r *http.Request) {
	h.resolveDoctrineReport(w, r, DoctrineReportDismissed)
}
