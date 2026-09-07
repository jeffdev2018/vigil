package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Property → work item type scoping (F30 / JEF-34).

func createScopedTestProperty(t *testing.T, name string) db.IssueProperty {
	t.Helper()
	prop, err := testHandler.Queries.CreateIssueProperty(context.Background(), db.CreateIssuePropertyParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Name:        name,
		Type:        "text",
		Description: "",
		Icon:        "",
		Config:      []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create property %q: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_property_type WHERE property_id = $1`, prop.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue_property WHERE id = $1`, prop.ID)
	})
	return prop
}

func setPropertyTypes(t *testing.T, propertyID string, keys []string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPut, "/api/properties/"+propertyID+"/types", map[string]any{"type_keys": keys})
	return testutil.Call(t, testHandler.SetPropertyTypes, testutil.WithURLParams(req, "id", propertyID))
}

// propertyAppliesToType is the whole applicability rule, so it gets a table
// here rather than being re-derived through a handler on every case.
func TestPropertyAppliesToType(t *testing.T) {
	cases := []struct {
		name      string
		scope     []string
		issueType string
		want      bool
	}{
		{"global property on a typed issue", nil, "bug", true},
		{"global property on an untyped issue", nil, "", true},
		{"scoped property on its own type", []string{"bug"}, "bug", true},
		{"scoped property on another type", []string{"bug"}, "story", false},
		{"scoped property on an untyped issue", []string{"bug"}, "", false},
		{"multi-scoped property on the second type", []string{"bug", "story"}, "story", true},
	}
	for _, tc := range cases {
		if got := propertyAppliesToType(tc.scope, tc.issueType); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Acceptance 4 + 5: a property scoped to `bug` is not settable on a `story`;
// an unscoped property stays settable on both.
func TestSetIssuePropertyRespectsTypeScope(t *testing.T) {
	seedTestTypeCatalog(t)
	scoped := createScopedTestProperty(t, "F30 scoped")
	global := createScopedTestProperty(t, "F30 global")
	setPropertyTypes(t, uuidToString(scoped.ID), []string{"bug"}).Want(http.StatusOK)

	bug := dbfx.Issue(t, "a bug issue", testutil.Cols{"issue_type": "bug"})
	story := dbfx.Issue(t, "a story issue", testutil.Cols{"issue_type": "story"})
	untyped := dbfx.Issue(t, "an untyped issue")

	set := func(issueID string, prop db.IssueProperty) *testutil.Response {
		propID := uuidToString(prop.ID)
		req := newRequest(http.MethodPut, "/api/issues/"+issueID+"/properties/"+propID, map[string]any{"value": "x"})
		return testutil.Call(t, testHandler.SetIssueProperty, testutil.WithURLParams(req, "id", issueID, "propertyId", propID))
	}

	set(bug, scoped).Want(http.StatusOK)
	body := set(story, scoped).Want(http.StatusConflict).Map()
	if body["code"] != "property_not_applicable" {
		t.Errorf("a scoped property on the wrong type must carry property_not_applicable, got %v", body)
	}
	// An untyped issue carries only global properties: "no type" matches no
	// type list.
	set(untyped, scoped).Want(http.StatusConflict)

	// Acceptance 5: an unscoped property is unaffected everywhere.
	set(bug, global).Want(http.StatusOK)
	set(story, global).Want(http.StatusOK)
	set(untyped, global).Want(http.StatusOK)
}

// Acceptance 6: changing an issue's type deletes no stored value.
func TestChangingIssueTypeKeepsOutOfScopeValues(t *testing.T) {
	seedTestTypeCatalog(t)
	scoped := createScopedTestProperty(t, "F30 survivor")
	propID := uuidToString(scoped.ID)
	setPropertyTypes(t, propID, []string{"bug"}).Want(http.StatusOK)

	issue := dbfx.Issue(t, "reclassified", testutil.Cols{"issue_type": "bug"})
	req := newRequest(http.MethodPut, "/api/issues/"+issue+"/properties/"+propID, map[string]any{"value": "keep me"})
	testutil.Call(t, testHandler.SetIssueProperty, testutil.WithURLParams(req, "id", issue, "propertyId", propID)).
		Want(http.StatusOK)

	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issue, map[string]any{"issue_type": "story"}), "id", issue),
	).Want(http.StatusOK)

	var out IssueResponse
	testutil.Call(t, testHandler.GetIssue, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issue, nil), "id", issue),
	).Want(http.StatusOK).JSON(&out)
	if out.Properties[propID] != "keep me" {
		t.Fatalf("an out-of-scope value must survive a type change; properties=%v", out.Properties)
	}
}

func TestSetPropertyTypesRejectsUnknownType(t *testing.T) {
	seedTestTypeCatalog(t)
	prop := createScopedTestProperty(t, "F30 bad scope")
	body := setPropertyTypes(t, uuidToString(prop.ID), []string{"not_a_type"}).
		Want(http.StatusBadRequest).Map()
	if body["code"] != "unknown_issue_type" {
		t.Errorf("expected unknown_issue_type, got %v", body)
	}
}

// An empty payload means GLOBAL; the response must say so and the rows must go.
func TestSetPropertyTypesEmptyMakesGlobal(t *testing.T) {
	seedTestTypeCatalog(t)
	prop := createScopedTestProperty(t, "F30 back to global")
	propID := uuidToString(prop.ID)
	setPropertyTypes(t, propID, []string{"bug", "story"}).Want(http.StatusOK)

	var out PropertyResponse
	setPropertyTypes(t, propID, []string{}).Want(http.StatusOK).JSON(&out)
	if len(out.TypeKeys) != 0 {
		t.Fatalf("an empty scope must read back as global, got %v", out.TypeKeys)
	}
	keys, err := testHandler.Queries.ListIssuePropertyTypeKeys(context.Background(), prop.ID)
	if err != nil {
		t.Fatalf("list keys: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("scope rows should be gone, got %v", keys)
	}
}

// Acceptance 7: the cap is per type. Twenty properties scoped to `bug` leave
// `story` room for its own, which a workspace-wide cap would not.
func TestActivePropertyCapIsPerType(t *testing.T) {
	seedTestTypeCatalog(t)
	// Take the workspace to the cap with properties scoped to `bug` only.
	for i := 0; i < maxActivePropertiesPerWorkspace; i++ {
		prop := createScopedTestProperty(t, "F30 cap "+string(rune('a'+i)))
		setPropertyTypes(t, uuidToString(prop.ID), []string{"bug"}).Want(http.StatusOK)
	}
	// A new property is created GLOBAL, so it lands in `bug`'s budget too and
	// is refused — that is the same admission the old workspace cap made.
	testutil.Call(t, testHandler.CreateProperty, newRequest(http.MethodPost, "/api/properties", map[string]any{
		"name": "F30 over cap", "type": "text",
	})).Want(http.StatusBadRequest)

	// Scoping a NEW property to `story` instead is admitted, because story's
	// budget was never touched. Created directly to get past the global create
	// check, which is the honest sequencing the UI has too (create then scope
	// is a two-step flow).
	story := createScopedTestProperty(t, "F30 story only")
	setPropertyTypes(t, uuidToString(story.ID), []string{"story"}).Want(http.StatusOK)

	// But widening it back to `bug` is refused: bug is already full.
	body := setPropertyTypes(t, uuidToString(story.ID), []string{"bug", "story"}).
		Want(http.StatusBadRequest).Map()
	if body["code"] != "property_cap_exceeded" {
		t.Errorf("expected property_cap_exceeded, got %v", body)
	}
}
