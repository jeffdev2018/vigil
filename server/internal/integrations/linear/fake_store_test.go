package linear

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// fakeStore is an in-memory Store. The bridge's rules are about which row it
// reads and writes, not about SQL, so exercising them here keeps every case in
// this file fast and independent of a database.
type fakeStore struct {
	mu sync.Mutex

	installs     map[string]db.LinearInstallation
	issueLinks   []db.LinearIssueLink
	commentLinks []db.LinearCommentLink
	issues       map[string]db.Issue
	comments     []db.Comment

	// counters the assertions read
	createdIssues   int
	createdComments int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		installs: map[string]db.LinearInstallation{},
		issues:   map[string]db.Issue{},
	}
}

func newUUID() pgtype.UUID { return dbid.NewV7() }

func key(u pgtype.UUID) string { return uuidKey(u) }

func uuidKey(u pgtype.UUID) string {
	b, _ := u.MarshalJSON()
	return string(b)
}

func (f *fakeStore) putInstall(inst db.LinearInstallation) {
	f.installs[key(inst.ID)] = inst
}

func (f *fakeStore) GetLinearInstallation(_ context.Context, id pgtype.UUID) (db.LinearInstallation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.installs[key(id)]
	if !ok {
		return db.LinearInstallation{}, pgx.ErrNoRows
	}
	return inst, nil
}

func (f *fakeStore) GetLinearInstallationByWorkspace(_ context.Context, wsID pgtype.UUID) (db.LinearInstallation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, inst := range f.installs {
		if inst.WorkspaceID == wsID {
			return inst, nil
		}
	}
	return db.LinearInstallation{}, pgx.ErrNoRows
}

func (f *fakeStore) SetLinearInstallationStatus(_ context.Context, arg db.SetLinearInstallationStatusParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	inst, ok := f.installs[key(arg.ID)]
	if !ok {
		return pgx.ErrNoRows
	}
	inst.Status = arg.Status
	inst.LastError = arg.LastError
	f.installs[key(arg.ID)] = inst
	return nil
}

func (f *fakeStore) GetLinearIssueLinkByRemote(_ context.Context, arg db.GetLinearIssueLinkByRemoteParams) (db.LinearIssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.issueLinks {
		if l.LinearTeamID == arg.LinearTeamID && l.LinearIssueID == arg.LinearIssueID {
			return l, nil
		}
	}
	return db.LinearIssueLink{}, pgx.ErrNoRows
}

func (f *fakeStore) GetLinearIssueLinkByRemoteIssue(_ context.Context, arg db.GetLinearIssueLinkByRemoteIssueParams) (db.LinearIssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.issueLinks {
		if l.InstallationID == arg.InstallationID && l.LinearIssueID == arg.LinearIssueID {
			return l, nil
		}
	}
	return db.LinearIssueLink{}, pgx.ErrNoRows
}

func (f *fakeStore) GetLinearIssueLinkByIssue(_ context.Context, issueID pgtype.UUID) (db.LinearIssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.issueLinks {
		if l.IssueID == issueID {
			return l, nil
		}
	}
	return db.LinearIssueLink{}, pgx.ErrNoRows
}

// CreateLinearIssueLink mirrors the real query's ON CONFLICT on
// (linear_team_id, linear_issue_id): the second delivery of one issue must not
// produce a second link.
func (f *fakeStore) CreateLinearIssueLink(_ context.Context, arg db.CreateLinearIssueLinkParams) (db.LinearIssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.issueLinks {
		if l.LinearTeamID == arg.LinearTeamID && l.LinearIssueID == arg.LinearIssueID {
			return l, nil
		}
	}
	link := db.LinearIssueLink{
		ID:                    newUUID(),
		WorkspaceID:           arg.WorkspaceID,
		InstallationID:        arg.InstallationID,
		IssueID:               arg.IssueID,
		LinearIssueID:         arg.LinearIssueID,
		LinearIssueIdentifier: arg.LinearIssueIdentifier,
		LinearTeamID:          arg.LinearTeamID,
		LinearUrl:             arg.LinearUrl,
		SyncState:             "active",
	}
	f.issueLinks = append(f.issueLinks, link)
	return link, nil
}

func (f *fakeStore) SetLinearIssueLinkState(_ context.Context, arg db.SetLinearIssueLinkStateParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, l := range f.issueLinks {
		if l.ID == arg.ID {
			f.issueLinks[i].SyncState = arg.SyncState
			f.issueLinks[i].LastError = arg.LastError
			return nil
		}
	}
	return pgx.ErrNoRows
}

// CreateLinearCommentLink mirrors ON CONFLICT (linear_comment_id) DO NOTHING.
func (f *fakeStore) CreateLinearCommentLink(_ context.Context, arg db.CreateLinearCommentLinkParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.commentLinks {
		if l.LinearCommentID == arg.LinearCommentID {
			return nil
		}
	}
	f.commentLinks = append(f.commentLinks, db.LinearCommentLink{
		ID:              newUUID(),
		InstallationID:  arg.InstallationID,
		CommentID:       arg.CommentID,
		LinearCommentID: arg.LinearCommentID,
		Direction:       arg.Direction,
	})
	return nil
}

func (f *fakeStore) GetLinearCommentLinkByComment(_ context.Context, commentID pgtype.UUID) (db.LinearCommentLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.commentLinks {
		if l.CommentID == commentID {
			return l, nil
		}
	}
	return db.LinearCommentLink{}, pgx.ErrNoRows
}

func (f *fakeStore) GetLinearCommentLinkByRemote(_ context.Context, id string) (db.LinearCommentLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.commentLinks {
		if l.LinearCommentID == id {
			return l, nil
		}
	}
	return db.LinearCommentLink{}, pgx.ErrNoRows
}

func (f *fakeStore) GetIssue(_ context.Context, id pgtype.UUID) (db.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	issue, ok := f.issues[key(id)]
	if !ok {
		return db.Issue{}, pgx.ErrNoRows
	}
	return issue, nil
}

func (f *fakeStore) UpdateIssueStatus(_ context.Context, arg db.UpdateIssueStatusParams) (db.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	issue, ok := f.issues[key(arg.ID)]
	if !ok {
		return db.Issue{}, pgx.ErrNoRows
	}
	issue.Status = arg.Status
	f.issues[key(arg.ID)] = issue
	return issue, nil
}

func (f *fakeStore) UpdateLinearMirrorIssueContent(_ context.Context, arg db.UpdateLinearMirrorIssueContentParams) (db.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	issue, ok := f.issues[key(arg.ID)]
	if !ok {
		return db.Issue{}, pgx.ErrNoRows
	}
	issue.Title = arg.Title
	issue.Description = arg.Description
	f.issues[key(arg.ID)] = issue
	return issue, nil
}

func (f *fakeStore) CreateComment(_ context.Context, arg db.CreateCommentParams) (db.CreateCommentRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdComments++
	c := db.Comment{
		ID:          arg.ID,
		IssueID:     arg.IssueID,
		WorkspaceID: arg.WorkspaceID,
		AuthorType:  arg.AuthorType,
		AuthorID:    arg.AuthorID,
		Content:     arg.Content,
		Type:        arg.Type,
	}
	f.comments = append(f.comments, c)
	return db.CreateCommentRow{
		ID: c.ID, IssueID: c.IssueID, WorkspaceID: c.WorkspaceID,
		AuthorType: c.AuthorType, AuthorID: c.AuthorID, Content: c.Content, Type: c.Type,
	}, nil
}

// fakeCreator is the host half: it records the mirror issue the bridge asked
// for, so a test can assert exactly one was created.
type fakeCreator struct {
	store *fakeStore
	err   error
	last  MirrorIssueInput
}

func (c *fakeCreator) CreateMirrorIssue(_ context.Context, in MirrorIssueInput) (db.Issue, error) {
	if c.err != nil {
		return db.Issue{}, c.err
	}
	c.last = in
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	c.store.createdIssues++
	issue := db.Issue{
		ID:          newUUID(),
		WorkspaceID: in.WorkspaceID,
		Title:       in.Title,
		Description: pgtype.Text{String: in.Description, Valid: true},
		Status:      in.Status,
	}
	c.store.issues[key(issue.ID)] = issue
	return issue, nil
}

// fakeAPI stands in for Linear. Every method records its calls and can be told
// to fail, which is how the 401 path is driven without a network.
type fakeAPI struct {
	mu sync.Mutex

	viewer      Viewer
	issue       Issue
	states      []WorkflowState
	comments    []struct{ IssueID, Body string }
	stateWrites []struct{ IssueID, StateID string }
	stateCalls  int

	commentErr error
	stateErr   error
	statesErr  error
	issueErr   error
}

func (a *fakeAPI) Viewer(context.Context) (Viewer, error) { return a.viewer, nil }

func (a *fakeAPI) Issue(context.Context, string) (Issue, error) {
	if a.issueErr != nil {
		return Issue{}, a.issueErr
	}
	return a.issue, nil
}

func (a *fakeAPI) CreateComment(_ context.Context, issueID, body string) (string, error) {
	if a.commentErr != nil {
		return "", a.commentErr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.comments = append(a.comments, struct{ IssueID, Body string }{issueID, body})
	return "remote_comment_1", nil
}

func (a *fakeAPI) UpdateIssueState(_ context.Context, issueID, stateID string) error {
	if a.stateErr != nil {
		return a.stateErr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stateWrites = append(a.stateWrites, struct{ IssueID, StateID string }{issueID, stateID})
	return nil
}

func (a *fakeAPI) ListTeamStates(context.Context, string) ([]WorkflowState, error) {
	if a.statesErr != nil {
		return nil, a.statesErr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stateCalls++
	return a.states, nil
}

func (a *fakeAPI) CreateWebhook(context.Context, string, string, []string) (string, error) {
	return "wh_1", nil
}

func (a *fakeAPI) DeleteWebhook(context.Context, string) error { return nil }

// newTestBridge wires the fakes together and returns the installation every
// test acts through.
func newTestBridge(t testingT) (*Bridge, *fakeStore, *fakeAPI, *fakeCreator, db.LinearInstallation) {
	t.Helper()
	store := newFakeStore()
	api := &fakeAPI{
		viewer: Viewer{ID: "user_app", OrganizationID: "org_1", OrganizationName: "Acme"},
		states: []WorkflowState{
			{ID: "st_backlog", Type: "backlog", Position: 0},
			{ID: "st_todo", Type: "unstarted", Position: 1},
			{ID: "st_doing", Type: "started", Position: 2},
			{ID: "st_done", Type: "completed", Position: 3},
			{ID: "st_cancel", Type: "canceled", Position: 4},
		},
	}
	creator := &fakeCreator{store: store}
	statusMap, _ := json.Marshal(DefaultStatusMap())
	inst := db.LinearInstallation{
		ID:                     newUUID(),
		WorkspaceID:            newUUID(),
		AgentID:                newUUID(),
		LinearOrgID:            "org_1",
		ActorUserID:            "user_app",
		AccessTokenEncrypted:   []byte("sealed"),
		WebhookSecretEncrypted: []byte("sealed-secret"),
		StatusMap:              statusMap,
		Status:                 "active",
	}
	store.putInstall(inst)

	// The decrypter is the identity here: at-rest encryption is secretbox's
	// contract and is tested there, not a rule of the bridge.
	bridge := NewBridge(store, func(b []byte) ([]byte, error) { return b, nil }, creator, nil)
	bridge.SetClientFactory(func(string) API { return api })
	return bridge, store, api, creator, inst
}

type testingT interface{ Helper() }
