package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Property → work item type scoping (F30 / JEF-34).
//
// A property with NO scope row is GLOBAL: it applies to every type AND to
// untyped issues. Scoping it to one or more types narrows it to exactly those.
//
// An UNTYPED issue therefore carries only global properties. That is the only
// self-consistent reading — "no type" cannot match a type list — and it is what
// keeps every pre-F30 issue and every pre-F30 property behaving identically:
// nothing is scoped until someone scopes it, so nothing changes on migration.
//
// A value is never deleted by a scope change or a type change. Out-of-scope
// values stay on the issue and stay readable; the UI folds them behind "hidden
// properties (N)". Deleting them would make classifying an issue a destructive
// act, and the only way back would be retyping data the product threw away.

// propertyTypeScopes reads every scope row in the workspace as
// propertyID -> sorted type keys. Properties with no rows are absent from the
// map, which IS the global marker — callers must treat "absent" and "empty" the
// same way rather than inventing a third state.
func (h *Handler) propertyTypeScopes(ctx context.Context, workspaceID pgtype.UUID) (map[string][]string, error) {
	rows, err := h.Queries.ListIssuePropertyTypesForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(rows))
	for _, row := range rows {
		id := uuidToString(row.PropertyID)
		out[id] = append(out[id], row.TypeKey)
	}
	return out, nil
}

// propertyAppliesToType answers the applicability rule for ONE property.
// `issueType` is "" for an untyped issue.
func propertyAppliesToType(scope []string, issueType string) bool {
	if len(scope) == 0 {
		return true
	}
	if issueType == "" {
		return false
	}
	for _, key := range scope {
		if key == issueType {
			return true
		}
	}
	return false
}

// normalizeIssueTypeKeys lowercases, trims, de-duplicates and sorts a scope
// payload. Sorted so the stored set and the response are stable regardless of
// the order the caller sent, which is what lets a test compare them literally.
func normalizeIssueTypeKeys(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, key := range raw {
		k := strings.ToLower(strings.TrimSpace(key))
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type SetPropertyTypesRequest struct {
	// Empty (or absent) means GLOBAL: every scope row is dropped and the
	// property applies everywhere again.
	TypeKeys []string `json:"type_keys"`
}

// SetPropertyTypes replaces a property's type scope wholesale.
//
// Replace rather than add/remove: the scope IS a set, the settings UI edits it
// as a set, and a two-verb API would make "make this global" either a special
// case or a loop that can half-fail.
func (h *Handler) SetPropertyTypes(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.requirePropertyAdmin(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	propertyID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "property id")
	if !ok {
		return
	}
	var req SetPropertyTypesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	keys := normalizeIssueTypeKeys(req.TypeKeys)

	property, err := h.Queries.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{ID: propertyID, WorkspaceID: wsUUID})
	if err != nil {
		writeNotFoundOrInternal(w, r, err, "property not found", "failed to load property")
		return
	}

	// Validate every key against the catalogue before writing anything: a scope
	// naming a type that does not exist would be invisible in the settings UI
	// and would silently hide the property from every issue.
	if len(keys) > 0 {
		if err := h.ensureIssueTypeCatalog(r.Context(), wsUUID); err != nil {
			slog.Warn("failed to ensure issue type catalog", append(logger.RequestAttrs(r), "error", err)...)
		}
		entries, err := h.Queries.ListIssueTypeEntries(r.Context(), db.ListIssueTypeEntriesParams{
			WorkspaceID:     wsUUID,
			IncludeArchived: true,
		})
		if err != nil {
			slog.Warn("SetPropertyTypes list types failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to load issue types")
			return
		}
		known := make(map[string]struct{}, len(entries))
		for _, e := range entries {
			known[e.Key] = struct{}{}
		}
		for _, key := range keys {
			if _, found := known[key]; !found {
				writeErrorCode(w, http.StatusBadRequest, "unknown_issue_type", fmt.Sprintf("unknown issue type %q", key))
				return
			}
		}
	}

	// Cap census and write share the workspace property lock with create and
	// with definition edits, so the count a scope change is admitted against is
	// the same one a concurrent create sees.
	var capErr string
	err = h.withPropertyLock(r, []string{"props:" + workspaceID}, func(q *db.Queries) error {
		// The property is about to become global, or to join these types.
		// Either way the census has to be taken AFTER removing its current
		// rows, or the property would be counted against a type it is leaving.
		if err := q.DeleteIssuePropertyTypes(r.Context(), propertyID); err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := q.InsertIssuePropertyTypes(r.Context(), db.InsertIssuePropertyTypesParams{
				WorkspaceID: wsUUID,
				PropertyID:  propertyID,
				TypeKeys:    keys,
			}); err != nil {
				return err
			}
		}
		if property.ArchivedAt.Valid {
			// An archived property occupies no budget, so nothing to check.
			return nil
		}
		over, err := h.overflowingIssueTypes(r.Context(), q, wsUUID, keys, 0)
		if err != nil {
			return err
		}
		if len(over) > 0 {
			capErr = fmt.Sprintf(
				"a work item type cannot have more than %d active properties; %s would be over the limit",
				maxActivePropertiesPerWorkspace, strings.Join(over, ", "),
			)
			// Returning an error rolls the whole closure back, so the scope
			// change is refused rather than committed over the cap.
			return errClientRejected
		}
		return nil
	})
	if capErr != "" {
		writeErrorCode(w, http.StatusBadRequest, "property_cap_exceeded", capErr)
		return
	}
	if err != nil {
		slog.Warn("SetPropertyTypes failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update property types")
		return
	}

	resp := propertyToResponse(property, 0)
	resp.TypeKeys = keys
	h.publish(protocol.EventPropertyUpdated, workspaceID, "member", userID, map[string]any{"property": resp})
	writeJSON(w, http.StatusOK, resp)
}

// overflowingIssueTypes returns the type keys whose ACTIVE applicable-property
// count now exceeds the cap.
//
// The cap is per TYPE, not per workspace: scoping is what makes 20 properties
// survivable, so counting them all against one workspace budget would defeat
// the feature. Global properties count for every type, because every type
// carries them.
//
// `keys` empty means the property is global (just became one, or is being
// created as one), so EVERY type's budget moved and all of them have to be
// re-counted. That is a handful of rows against a handful of types — the
// settings page writes this once per edit.
//
// `additional` is how many properties the caller is about to add that the
// census cannot see yet: 1 for a create (the row does not exist), 0 for a scope
// change (the rows are already written inside the caller's transaction).
func (h *Handler) overflowingIssueTypes(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, keys []string, additional int64) ([]string, error) {
	check := keys
	if len(check) == 0 {
		entries, err := q.ListIssueTypeEntries(ctx, db.ListIssueTypeEntriesParams{
			WorkspaceID:     workspaceID,
			IncludeArchived: false,
		})
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			check = append(check, e.Key)
		}
		// A workspace with no types at all still has the untyped bucket, which
		// only global properties reach. Counting the globals against it is what
		// keeps the cap meaningful before anyone creates a type.
		if len(check) == 0 {
			total, err := q.CountActiveGlobalProperties(ctx, workspaceID)
			if err != nil {
				return nil, err
			}
			if total+additional > maxActivePropertiesPerWorkspace {
				return []string{"untyped issues"}, nil
			}
			return nil, nil
		}
	}
	var over []string
	for _, key := range check {
		count, err := q.CountActivePropertiesApplicableToType(ctx, db.CountActivePropertiesApplicableToTypeParams{
			WorkspaceID: workspaceID,
			TypeKey:     key,
		})
		if err != nil {
			return nil, err
		}
		if count+additional > maxActivePropertiesPerWorkspace {
			over = append(over, key)
		}
	}
	return over, nil
}

// writeNotFoundOrInternal is the two-line load-failure shape this file needs
// three times; it exists so a pgx.ErrNoRows check cannot be forgotten on one of
// them.
func writeNotFoundOrInternal(w http.ResponseWriter, r *http.Request, err error, notFoundMsg, internalMsg string) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, notFoundMsg)
		return
	}
	slog.Warn(internalMsg, append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, internalMsg)
}
