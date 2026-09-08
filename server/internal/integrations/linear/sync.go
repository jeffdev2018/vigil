package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// sync.go is the bridge itself: what a Linear event does to Multica, and what
// a Multica event does to Linear. It talks to the database and to the API
// interface only, so every rule below is exercised by sync_test.go without an
// HTTP server or a router.

// LinearStateTypes are Linear's coarse workflow-state buckets, in the order a
// reverse lookup prefers them. Several of them can map to the same Multica
// status (triage and unstarted both usually mean "todo"), so pushing a status
// back needs a deterministic winner rather than whatever map iteration gives.
var LinearStateTypes = []string{"triage", "backlog", "unstarted", "started", "completed", "canceled"}

// DefaultStatusMap is what a fresh installation starts with: Linear workflow
// state type -> Multica status key.
func DefaultStatusMap() map[string]string {
	return map[string]string{
		"triage":    issuestatus.Todo,
		"backlog":   issuestatus.Backlog,
		"unstarted": issuestatus.Todo,
		"started":   issuestatus.InProgress,
		"completed": issuestatus.Done,
		"canceled":  issuestatus.Cancelled,
	}
}

// Store is the slice of generated queries the bridge needs. *db.Queries
// satisfies it; tests inject a fake.
type Store interface {
	GetLinearIssueLinkByRemote(ctx context.Context, arg db.GetLinearIssueLinkByRemoteParams) (db.LinearIssueLink, error)
	GetLinearIssueLinkByRemoteIssue(ctx context.Context, arg db.GetLinearIssueLinkByRemoteIssueParams) (db.LinearIssueLink, error)
	GetLinearIssueLinkByIssue(ctx context.Context, issueID pgtype.UUID) (db.LinearIssueLink, error)
	CreateLinearIssueLink(ctx context.Context, arg db.CreateLinearIssueLinkParams) (db.LinearIssueLink, error)
	SetLinearIssueLinkState(ctx context.Context, arg db.SetLinearIssueLinkStateParams) error
	CreateLinearCommentLink(ctx context.Context, arg db.CreateLinearCommentLinkParams) error
	GetLinearCommentLinkByComment(ctx context.Context, commentID pgtype.UUID) (db.LinearCommentLink, error)
	GetLinearCommentLinkByRemote(ctx context.Context, linearCommentID string) (db.LinearCommentLink, error)
	GetLinearInstallation(ctx context.Context, id pgtype.UUID) (db.LinearInstallation, error)
	GetLinearInstallationByWorkspace(ctx context.Context, workspaceID pgtype.UUID) (db.LinearInstallation, error)
	SetLinearInstallationStatus(ctx context.Context, arg db.SetLinearInstallationStatusParams) error
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	UpdateIssueStatus(ctx context.Context, arg db.UpdateIssueStatusParams) (db.Issue, error)
	UpdateLinearMirrorIssueContent(ctx context.Context, arg db.UpdateLinearMirrorIssueContentParams) (db.Issue, error)
	CreateComment(ctx context.Context, arg db.CreateCommentParams) (db.CreateCommentRow, error)
}

// MirrorIssueInput is what the bridge asks the host to create. It is a narrow
// local type rather than service.IssueCreateParams so this package does not
// depend on the service layer (and so a sync test needs no issue service).
type MirrorIssueInput struct {
	WorkspaceID    pgtype.UUID
	AgentID        pgtype.UUID
	InstallationID pgtype.UUID
	CreatedBy      pgtype.UUID
	Title          string
	Description    string
	Status         string
}

// IssueCreator makes the mirrored issue. The handler implements it over
// IssueService.Create, which is what enqueues the agent's run — the bridge
// never enqueues anything itself, so a mirrored issue starts a run exactly the
// way any other agent-assigned issue does.
type IssueCreator interface {
	CreateMirrorIssue(ctx context.Context, in MirrorIssueInput) (db.Issue, error)
}

// Bridge is the both-ways sync.
type Bridge struct {
	q       Store
	decrypt func([]byte) ([]byte, error)
	issues  IssueCreator
	logger  *slog.Logger

	// newClient builds an API for one installation's token. Injectable so the
	// sync tests never reach the network.
	newClient func(token string) API

	// OnCommentCreated lets the host broadcast a comment the bridge wrote.
	// Optional: the row is already committed when it runs.
	OnCommentCreated func(ctx context.Context, issue db.Issue, comment db.Comment, issueRevision int64)
	// OnIssueChanged lets the host broadcast a mirrored issue update.
	OnIssueChanged func(ctx context.Context, issue db.Issue)
	// OnInstallationBroken is the inbox alert. Called once per transition into
	// `broken`, never on an ordinary transient failure.
	OnInstallationBroken func(ctx context.Context, inst db.LinearInstallation, reason string)

	// AppURL is the web base used for the "open in Multica" link the bridge
	// puts on a Linear run-outcome comment. Empty omits the link.
	AppURL string

	statesMu sync.Mutex
	states   map[string]cachedStates
	now      func() time.Time
}

type cachedStates struct {
	states  []WorkflowState
	fetched time.Time
}

// teamStatesTTL bounds how stale a cached board can be. A workflow state is
// renamed or added rarely; re-reading it on every status push would be one
// extra API round trip per issue move.
const teamStatesTTL = 10 * time.Minute

// NewBridge builds the bridge over the generated queries.
func NewBridge(q Store, decrypt func([]byte) ([]byte, error), issues IssueCreator, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{
		q:         q,
		decrypt:   decrypt,
		issues:    issues,
		logger:    logger,
		newClient: func(token string) API { return NewClient(token) },
		states:    map[string]cachedStates{},
		now:       time.Now,
	}
}

// SetClientFactory replaces the API constructor. Tests and the router (which
// pins the endpoint in a self-hosted deployment) use it.
func (b *Bridge) SetClientFactory(f func(token string) API) { b.newClient = f }

// SetClock replaces the clock the state cache ages against.
func (b *Bridge) SetClock(f func() time.Time) { b.now = f }

// client decrypts an installation's access token and returns an API bound to it.
func (b *Bridge) client(inst db.LinearInstallation) (API, error) {
	if b.decrypt == nil {
		return nil, errors.New("linear: no decrypter configured")
	}
	token, err := b.decrypt(inst.AccessTokenEncrypted)
	if err != nil {
		return nil, fmt.Errorf("linear: decrypt access token: %w", err)
	}
	return b.newClient(string(token)), nil
}

// WebhookSecret decrypts an installation's webhook secret.
func (b *Bridge) WebhookSecret(inst db.LinearInstallation) ([]byte, error) {
	if len(inst.WebhookSecretEncrypted) == 0 {
		return nil, errors.New("linear: installation has no webhook secret")
	}
	if b.decrypt == nil {
		return nil, errors.New("linear: no decrypter configured")
	}
	return b.decrypt(inst.WebhookSecretEncrypted)
}

// StatusMap reads an installation's Linear-type -> Multica-key map, falling
// back to the default for anything it does not define. A partial or corrupt
// map must not silently stop the bridge syncing status at all.
func StatusMap(inst db.LinearInstallation) map[string]string {
	out := DefaultStatusMap()
	if len(inst.StatusMap) == 0 {
		return out
	}
	var stored map[string]string
	if err := json.Unmarshal(inst.StatusMap, &stored); err != nil {
		return out
	}
	for k, v := range stored {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

// reverseStatusMap turns Multica key -> Linear state type, resolving the
// many-to-one collisions by LinearStateTypes order so the answer is stable.
func reverseStatusMap(m map[string]string) map[string]string {
	out := map[string]string{}
	for _, linearType := range LinearStateTypes {
		key, ok := m[linearType]
		if !ok || key == "" {
			continue
		}
		if _, taken := out[key]; taken {
			continue
		}
		out[key] = linearType
	}
	return out
}

// ---------------------------------------------------------------------------
// Inbound: Linear -> Multica
// ---------------------------------------------------------------------------

// HandleIssueEvent applies one Issue delivery.
//
// Assignment to the app's own Linear user is the whole trigger: an issue
// assigned to it is mirrored, an issue un-assigned from it pauses. Linear
// stays the source of truth for title, description and status of a mirrored
// issue, so an update overwrites those three and nothing else.
func (b *Bridge) HandleIssueEvent(ctx context.Context, inst db.LinearInstallation, ev WebhookEvent) error {
	data, err := ev.IssuePayload()
	if err != nil {
		return fmt.Errorf("linear: decode issue payload: %w", err)
	}
	if data.ID == "" {
		return errors.New("linear: issue payload carried no id")
	}
	teamID := data.ResolvedTeamID()
	link, err := b.q.GetLinearIssueLinkByRemote(ctx, db.GetLinearIssueLinkByRemoteParams{
		LinearTeamID: teamID, LinearIssueID: data.ID,
	})
	linked := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	// A deleted Linear issue stops syncing; the Multica issue and its history
	// stay, because a run may already have happened against it.
	if ev.Action == "remove" {
		if linked {
			return b.q.SetLinearIssueLinkState(ctx, db.SetLinearIssueLinkStateParams{
				ID: link.ID, SyncState: "paused", LastError: "issue removed in Linear",
			})
		}
		return nil
	}

	assigned := inst.ActorUserID != "" && data.ResolvedAssigneeID() == inst.ActorUserID
	if !linked {
		if !assigned {
			return nil
		}
		return b.mirrorNewIssue(ctx, inst, data, teamID)
	}

	if !assigned {
		// Handing the issue to a human in Linear stops the mirror rather than
		// deleting it: reassigning it back resumes the same Multica issue.
		return b.q.SetLinearIssueLinkState(ctx, db.SetLinearIssueLinkStateParams{
			ID: link.ID, SyncState: "paused", LastError: "unassigned in Linear",
		})
	}
	return b.applyRemoteIssue(ctx, inst, link, data)
}

func (b *Bridge) mirrorNewIssue(ctx context.Context, inst db.LinearInstallation, data IssueData, teamID string) error {
	if b.issues == nil {
		return errors.New("linear: no issue creator configured")
	}
	issue, err := b.issues.CreateMirrorIssue(ctx, MirrorIssueInput{
		WorkspaceID:    inst.WorkspaceID,
		AgentID:        inst.AgentID,
		InstallationID: inst.ID,
		CreatedBy:      inst.InstalledBy,
		Title:          mirrorTitle(data),
		Description:    mirrorDescription(data),
		Status:         StatusMap(inst)[data.StateType()],
	})
	if err != nil {
		return fmt.Errorf("linear: create mirror issue: %w", err)
	}
	_, err = b.q.CreateLinearIssueLink(ctx, db.CreateLinearIssueLinkParams{
		WorkspaceID:           inst.WorkspaceID,
		InstallationID:        inst.ID,
		IssueID:               issue.ID,
		LinearIssueID:         data.ID,
		LinearIssueIdentifier: data.Identifier,
		LinearTeamID:          teamID,
		LinearUrl:             data.URL,
	})
	return err
}

// applyRemoteIssue overwrites the three fields Linear owns on an existing
// mirror. Each write is independent so a rejected status (a key the workspace
// retired) does not also drop a legitimate title change.
func (b *Bridge) applyRemoteIssue(ctx context.Context, inst db.LinearInstallation, link db.LinearIssueLink, data IssueData) error {
	var updated db.Issue
	var firstErr error

	if title := mirrorTitle(data); title != "" {
		issue, err := b.q.UpdateLinearMirrorIssueContent(ctx, db.UpdateLinearMirrorIssueContentParams{
			ID:          link.IssueID,
			WorkspaceID: link.WorkspaceID,
			Title:       title,
			Description: pgtype.Text{String: mirrorDescription(data), Valid: true},
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			firstErr = err
		} else if err == nil {
			updated = issue
		}
	}
	if status := StatusMap(inst)[data.StateType()]; status != "" {
		issue, err := b.q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID: link.IssueID, Status: status, WorkspaceID: link.WorkspaceID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			if firstErr == nil {
				firstErr = err
			}
		} else if err == nil {
			updated = issue
		}
	}
	if firstErr != nil {
		return firstErr
	}
	if updated.ID.Valid && b.OnIssueChanged != nil {
		b.OnIssueChanged(ctx, updated)
	}
	// Reaching here means the link works again, so a previously broken or
	// paused one recovers without anyone pressing Resync.
	return b.q.SetLinearIssueLinkState(ctx, db.SetLinearIssueLinkStateParams{
		ID: link.ID, SyncState: "active", LastError: "",
	})
}

// HandleCommentEvent mirrors a Linear comment onto the Multica issue.
//
// Two guards keep this from looping: a comment authored by the app's own user
// is one we pushed, and a linear_comment_id we already have a link for is a
// redelivery.
func (b *Bridge) HandleCommentEvent(ctx context.Context, inst db.LinearInstallation, ev WebhookEvent) error {
	if ev.Action != "create" {
		return nil
	}
	data, err := ev.CommentPayload()
	if err != nil {
		return fmt.Errorf("linear: decode comment payload: %w", err)
	}
	if data.ID == "" || strings.TrimSpace(data.Body) == "" {
		return nil
	}
	if inst.ActorUserID != "" && data.ResolvedUserID() == inst.ActorUserID {
		return nil
	}
	if _, err := b.q.GetLinearCommentLinkByRemote(ctx, data.ID); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	issueID := data.ResolvedIssueID()
	if issueID == "" {
		return nil
	}
	link, err := b.q.GetLinearIssueLinkByRemoteIssue(ctx, db.GetLinearIssueLinkByRemoteIssueParams{
		InstallationID: inst.ID, LinearIssueID: issueID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if link.SyncState == "paused" {
		return nil
	}

	issue, err := b.q.GetIssue(ctx, link.IssueID)
	if err != nil {
		return err
	}
	// author_type='system': a Linear person's words are not a Multica member's,
	// so they are attributed to the bridge and the author is named in the body.
	created, err := b.q.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     "**" + data.AuthorName() + "** (Linear):\n\n" + data.Body,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		return fmt.Errorf("linear: mirror comment: %w", err)
	}
	comment := created.Comment()
	if err := b.q.CreateLinearCommentLink(ctx, db.CreateLinearCommentLinkParams{
		InstallationID:  inst.ID,
		CommentID:       comment.ID,
		LinearCommentID: data.ID,
		Direction:       "in",
	}); err != nil {
		return fmt.Errorf("linear: record inbound comment link: %w", err)
	}
	if b.OnCommentCreated != nil {
		b.OnCommentCreated(ctx, issue, comment, created.IssueRevision)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Outbound: Multica -> Linear
// ---------------------------------------------------------------------------

// LinkForIssue finds the active link for a Multica issue, or reports that
// there is none. Every outbound push starts here, so an issue that has nothing
// to do with Linear costs one indexed lookup.
func (b *Bridge) LinkForIssue(ctx context.Context, issueID pgtype.UUID) (db.LinearIssueLink, db.LinearInstallation, bool) {
	link, err := b.q.GetLinearIssueLinkByIssue(ctx, issueID)
	if err != nil || link.SyncState == "paused" {
		return db.LinearIssueLink{}, db.LinearInstallation{}, false
	}
	inst, err := b.q.GetLinearInstallation(ctx, link.InstallationID)
	if err != nil || inst.Status == "revoked" {
		return db.LinearIssueLink{}, db.LinearInstallation{}, false
	}
	return link, inst, true
}

// PushComment posts a Multica comment on the linked Linear issue.
//
// A comment that is itself an inbound mirror is skipped — that is the other
// half of the loop guard, and it is checked here rather than at the call site
// so every future caller inherits it.
func (b *Bridge) PushComment(ctx context.Context, issueID, commentID pgtype.UUID, body string) error {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	// The link lookup goes first: this runs on every comment in the workspace,
	// and almost none of them are on a mirrored issue, so the cheapest way to
	// say "nothing to do" should be the first thing tried.
	link, inst, ok := b.LinkForIssue(ctx, issueID)
	if !ok {
		return nil
	}
	if commentID.Valid {
		if _, err := b.q.GetLinearCommentLinkByComment(ctx, commentID); err == nil {
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	client, err := b.client(inst)
	if err != nil {
		return err
	}
	remoteID, err := client.CreateComment(ctx, link.LinearIssueID, body)
	if err != nil {
		return b.recordPushFailure(ctx, inst, link, err)
	}
	if !commentID.Valid {
		return nil
	}
	return b.q.CreateLinearCommentLink(ctx, db.CreateLinearCommentLinkParams{
		InstallationID:  inst.ID,
		CommentID:       commentID,
		LinearCommentID: remoteID,
		Direction:       "out",
	})
}

// PushStatus moves the linked Linear issue to the state the Multica status
// maps back to. A status with no Linear equivalent, or a team with no state of
// that type, is a no-op rather than an error: the workspace is free to define
// statuses Linear has no column for.
func (b *Bridge) PushStatus(ctx context.Context, issueID pgtype.UUID, status string) error {
	link, inst, ok := b.LinkForIssue(ctx, issueID)
	if !ok {
		return nil
	}
	wanted := reverseStatusMap(StatusMap(inst))[status]
	if wanted == "" {
		return nil
	}
	client, err := b.client(inst)
	if err != nil {
		return err
	}
	states, err := b.teamStates(ctx, client, link.LinearTeamID)
	if err != nil {
		return b.recordPushFailure(ctx, inst, link, err)
	}
	stateID := pickState(states, wanted)
	if stateID == "" {
		return nil
	}
	if err := client.UpdateIssueState(ctx, link.LinearIssueID, stateID); err != nil {
		return b.recordPushFailure(ctx, inst, link, err)
	}
	return nil
}

// PushRunOutcome posts the "run completed / failed" note on the Linear issue,
// which is how someone who only reads Linear learns the agent finished.
func (b *Bridge) PushRunOutcome(ctx context.Context, issueID pgtype.UUID, completed bool, costUSD string) error {
	link, inst, ok := b.LinkForIssue(ctx, issueID)
	if !ok {
		return nil
	}
	verb := "failed"
	if completed {
		verb = "completed"
	}
	body := "Multica run " + verb
	if strings.TrimSpace(costUSD) != "" {
		body += " · " + costUSD
	}
	if b.AppURL != "" {
		body += "\n\n[Open in Multica](" + strings.TrimRight(b.AppURL, "/") + "/issues/" + util.UUIDToString(issueID) + ")"
	}
	client, err := b.client(inst)
	if err != nil {
		return err
	}
	if _, err := client.CreateComment(ctx, link.LinearIssueID, body); err != nil {
		return b.recordPushFailure(ctx, inst, link, err)
	}
	return nil
}

// Resync re-reads the Linear issue and reconciles the mirror, clearing a
// `broken` link when it works. This is the manual recovery for deliveries that
// were missed while the installation was down.
func (b *Bridge) Resync(ctx context.Context, link db.LinearIssueLink) error {
	inst, err := b.q.GetLinearInstallation(ctx, link.InstallationID)
	if err != nil {
		return err
	}
	client, err := b.client(inst)
	if err != nil {
		return err
	}
	remote, err := client.Issue(ctx, link.LinearIssueID)
	if err != nil {
		return b.recordPushFailure(ctx, inst, link, err)
	}
	data := IssueData{
		ID: remote.ID, Identifier: remote.Identifier, Title: remote.Title,
		Description: remote.Description, URL: remote.URL, TeamID: remote.Team.ID,
	}
	data.State = &struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}{ID: remote.State.ID, Name: remote.State.Name, Type: remote.State.Type}
	if remote.Assignee != nil {
		data.AssigneeID = remote.Assignee.ID
	}
	return b.applyRemoteIssue(ctx, inst, link, data)
}

// recordPushFailure writes the error onto the link and, when Linear rejected
// the token, marks the whole installation broken and alerts the humans who can
// reconnect it. Any other error is transient and leaves the installation alone.
func (b *Bridge) recordPushFailure(ctx context.Context, inst db.LinearInstallation, link db.LinearIssueLink, cause error) error {
	msg := truncate(cause.Error(), 500)
	state := "active"
	if errors.Is(cause, ErrUnauthorized) {
		state = "broken"
	}
	if err := b.q.SetLinearIssueLinkState(ctx, db.SetLinearIssueLinkStateParams{
		ID: link.ID, SyncState: state, LastError: msg,
	}); err != nil {
		b.logger.WarnContext(ctx, "linear: record link error failed", "error", err)
	}
	if state != "broken" {
		return cause
	}
	if inst.Status != "broken" {
		if err := b.q.SetLinearInstallationStatus(ctx, db.SetLinearInstallationStatusParams{
			ID: inst.ID, Status: "broken", LastError: msg,
		}); err != nil {
			b.logger.WarnContext(ctx, "linear: mark installation broken failed", "error", err)
		}
		if b.OnInstallationBroken != nil {
			b.OnInstallationBroken(ctx, inst, msg)
		}
	}
	return cause
}

// teamStates reads a team's workflow states through a short-lived cache.
func (b *Bridge) teamStates(ctx context.Context, client API, teamID string) ([]WorkflowState, error) {
	if teamID == "" {
		return nil, errors.New("linear: link carries no team id")
	}
	b.statesMu.Lock()
	entry, ok := b.states[teamID]
	b.statesMu.Unlock()
	if ok && b.now().Sub(entry.fetched) < teamStatesTTL {
		return entry.states, nil
	}
	states, err := client.ListTeamStates(ctx, teamID)
	if err != nil {
		return nil, err
	}
	b.statesMu.Lock()
	b.states[teamID] = cachedStates{states: states, fetched: b.now()}
	b.statesMu.Unlock()
	return states, nil
}

// pickState returns the lowest-positioned state of the wanted type, which is
// the leftmost column of that kind on the team's board.
func pickState(states []WorkflowState, wanted string) string {
	best := ""
	bestPos := 0.0
	for _, s := range states {
		if !strings.EqualFold(s.Type, wanted) {
			continue
		}
		if best == "" || s.Position < bestPos {
			best, bestPos = s.ID, s.Position
		}
	}
	return best
}

func mirrorTitle(d IssueData) string {
	title := strings.TrimSpace(d.Title)
	if title == "" {
		return ""
	}
	return truncate(title, 500)
}

// mirrorDescription keeps a link back to the Linear issue at the bottom, so
// the agent reading the issue can reach the original.
func mirrorDescription(d IssueData) string {
	body := strings.TrimSpace(d.Description)
	if d.URL == "" {
		return body
	}
	ref := "[" + orDefault(d.Identifier, "Linear issue") + "](" + d.URL + ")"
	if body == "" {
		return ref
	}
	return body + "\n\n---\n" + ref
}

func orDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
