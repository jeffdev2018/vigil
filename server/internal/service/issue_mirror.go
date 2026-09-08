package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Cross-repo mirror issues (K54).
//
// A mirror link says "an issue of project A carrying label L must also exist
// in project B". When the label lands on such an issue — at creation or later
// — one mirror issue is created per configured target, and a `blocks`
// dependency is written with the MIRROR as the blocker: the source cannot be
// considered finished while its mirror is open, which is the semantics the
// existing dependency machinery already reports (merge readiness, PR stack).
//
// The link is configuration and the mirror is a real issue: deleting a link
// never deletes the mirrors it produced.

const (
	// AuditMirrorCreated is the audit action for one mirror issue.
	AuditMirrorCreated = "mirror.created"

	// IssueOriginMirror stamps generated mirrors (migration 725). origin_id is
	// the source issue.
	IssueOriginMirror = "mirror"

	// mirrorLinkWalkDepth bounds the cycle walk. A workspace with a chain of
	// mirror links deeper than this has a configuration problem of its own.
	mirrorLinkWalkDepth = 20
)

var (
	// ErrMirrorLinkSameProject is a link whose source and target are one
	// project — the CHECK constraint would refuse it anyway.
	ErrMirrorLinkSameProject = errors.New("a project cannot mirror into itself")
	// ErrMirrorLinkCycle is a link that closes a loop in the mirror graph:
	// the target already reaches the source, so the pair would generate
	// mirrors of mirrors forever.
	ErrMirrorLinkCycle = errors.New("mirror link would create a cycle")
	// ErrMirrorLinkDuplicate is an identical (source, target, label) link.
	ErrMirrorLinkDuplicate = errors.New("mirror link already exists")
	// ErrMirrorLinkEmptyLabel is a link with nothing to fire on.
	ErrMirrorLinkEmptyLabel = errors.New("trigger_label is required")
)

// IssueMirrorService owns mirror links and the mirrors they produce.
type IssueMirrorService struct {
	Queries      *db.Queries
	IssueService *IssueService
}

func NewIssueMirrorService(q *db.Queries, is *IssueService) *IssueMirrorService {
	return &IssueMirrorService{Queries: q, IssueService: is}
}

// MirrorLinkParams are the resolved inputs to CreateMirrorLink. The caller
// owns parsing and access control; this method owns the graph rules.
type MirrorLinkParams struct {
	WorkspaceID     pgtype.UUID
	SourceProjectID pgtype.UUID
	TargetProjectID pgtype.UUID
	TriggerLabel    string
	CreatedBy       pgtype.UUID
}

// CreateMirrorLink refuses a self-link, a duplicate and a cycle, then writes
// the link.
func (s *IssueMirrorService) CreateMirrorLink(ctx context.Context, p MirrorLinkParams) (db.ProjectMirrorLink, error) {
	label := strings.TrimSpace(p.TriggerLabel)
	if label == "" {
		return db.ProjectMirrorLink{}, ErrMirrorLinkEmptyLabel
	}
	if p.SourceProjectID == p.TargetProjectID {
		return db.ProjectMirrorLink{}, ErrMirrorLinkSameProject
	}

	links, err := s.Queries.ListProjectMirrorLinksByWorkspace(ctx, p.WorkspaceID)
	if err != nil {
		return db.ProjectMirrorLink{}, fmt.Errorf("list mirror links: %w", err)
	}
	for _, l := range links {
		if l.SourceProjectID == p.SourceProjectID && l.TargetProjectID == p.TargetProjectID &&
			strings.EqualFold(l.TriggerLabel, label) {
			return db.ProjectMirrorLink{}, ErrMirrorLinkDuplicate
		}
	}
	if mirrorGraphReaches(links, p.TargetProjectID, p.SourceProjectID) {
		return db.ProjectMirrorLink{}, ErrMirrorLinkCycle
	}

	link, err := s.Queries.CreateProjectMirrorLink(ctx, db.CreateProjectMirrorLinkParams{
		WorkspaceID:     p.WorkspaceID,
		SourceProjectID: p.SourceProjectID,
		TargetProjectID: p.TargetProjectID,
		TriggerLabel:    label,
		CreatedBy:       p.CreatedBy,
	})
	if err != nil {
		return db.ProjectMirrorLink{}, fmt.Errorf("create mirror link: %w", err)
	}
	return link, nil
}

// mirrorGraphReaches walks source -> target edges from `from` and reports
// whether `want` is reachable. `from == want` counts: the caller asks it about
// a proposed target and source, and target == source is already refused by
// ErrMirrorLinkSameProject, so reaching the source at depth 0 can only mean
// the pair closes a loop.
//
// The visited set makes the walk terminate on a graph that is already cyclic
// (rows written before this guard existed, or by a direct SQL edit); the depth
// bound is a second stop for a pathologically wide graph.
func mirrorGraphReaches(links []db.ProjectMirrorLink, from, want pgtype.UUID) bool {
	if from == want {
		return true
	}
	visited := map[pgtype.UUID]bool{from: true}
	frontier := []pgtype.UUID{from}
	for depth := 0; depth < mirrorLinkWalkDepth && len(frontier) > 0; depth++ {
		var next []pgtype.UUID
		for _, node := range frontier {
			for _, l := range links {
				if l.SourceProjectID != node || visited[l.TargetProjectID] {
					continue
				}
				if l.TargetProjectID == want {
					return true
				}
				visited[l.TargetProjectID] = true
				next = append(next, l.TargetProjectID)
			}
		}
		frontier = next
	}
	return false
}

// MirrorResult is one mirror created (or already present) for a source issue.
type MirrorResult struct {
	Link        db.ProjectMirrorLink
	MirrorIssue db.Issue
	Record      db.IssueMirror
	// Created is false when the mirror already existed for this (source, link)
	// pair — the re-attach case.
	Created bool
}

// MirrorIssueForLabel creates the mirrors a label attach on `issue` implies.
//
// It is idempotent per (source issue, link): a re-attached label finds the
// existing issue_mirror row and creates nothing. An issue with no project, or
// a project with no link for this label, is a no-op.
//
// A failure on one link is logged and skipped rather than aborting the rest:
// this runs on the side of an already-committed label attach, and refusing the
// remaining targets would leave a partially mirrored issue with no signal.
func (s *IssueMirrorService) MirrorIssueForLabel(ctx context.Context, issue db.Issue, labelName string) ([]MirrorResult, error) {
	if !issue.ProjectID.Valid || strings.TrimSpace(labelName) == "" {
		return nil, nil
	}
	links, err := s.Queries.ListMirrorLinksForLabel(ctx, db.ListMirrorLinksForLabelParams{
		WorkspaceID:     issue.WorkspaceID,
		SourceProjectID: issue.ProjectID,
		TriggerLabel:    labelName,
	})
	if err != nil {
		return nil, fmt.Errorf("list mirror links for label: %w", err)
	}

	var out []MirrorResult
	for _, link := range links {
		res, err := s.mirrorOnce(ctx, issue, link)
		if err != nil {
			slog.Warn("mirror issue for label failed",
				"issue_id", util.UUIDToString(issue.ID),
				"link_id", util.UUIDToString(link.ID),
				"error", err)
			continue
		}
		out = append(out, res)
	}
	return out, nil
}

func (s *IssueMirrorService) mirrorOnce(ctx context.Context, issue db.Issue, link db.ProjectMirrorLink) (MirrorResult, error) {
	existing, err := s.Queries.ListIssueMirrorsForSourceAndLink(ctx, db.ListIssueMirrorsForSourceAndLinkParams{
		SourceIssueID: issue.ID,
		LinkID:        link.ID,
	})
	if err != nil {
		return MirrorResult{}, fmt.Errorf("check existing mirror: %w", err)
	}
	if len(existing) > 0 {
		mirror, err := s.Queries.GetIssue(ctx, existing[0].MirrorIssueID)
		if err != nil {
			return MirrorResult{}, fmt.Errorf("load existing mirror issue: %w", err)
		}
		return MirrorResult{Link: link, MirrorIssue: mirror, Record: existing[0], Created: false}, nil
	}

	prefix := s.issuePrefix(ctx, issue.WorkspaceID)
	sourceProject := s.projectTitle(ctx, issue.WorkspaceID, issue.ProjectID)
	identifier := prefix + "-" + strconv.Itoa(int(issue.Number))

	created, err := s.IssueService.Create(ctx, IssueCreateParams{
		WorkspaceID: issue.WorkspaceID,
		Title:       "[Mirror of " + identifier + "] " + issue.Title,
		Description: pgtype.Text{String: mirrorDescription(identifier, sourceProject, issue.Description), Valid: true},
		Status:      "todo",
		Priority:    issue.Priority,
		// Deliberately unassigned: the target project decides who picks it up.
		CreatorType: issue.CreatorType,
		CreatorID:   issue.CreatorID,
		ProjectID:   link.TargetProjectID,
		OriginType:  pgtype.Text{String: IssueOriginMirror, Valid: true},
		OriginID:    issue.ID,
		// A mirror is intentionally a near-duplicate of its source.
		AllowDuplicate: true,
	}, IssueCreateOpts{})
	if err != nil {
		return MirrorResult{}, fmt.Errorf("create mirror issue: %w", err)
	}

	// The mirror BLOCKS the source: (issue_id = mirror, depends_on = source).
	if _, err := s.Queries.CreateIssueDependency(ctx, db.CreateIssueDependencyParams{
		IssueID:          created.Issue.ID,
		DependsOnIssueID: issue.ID,
		Type:             "blocks",
	}); err != nil {
		return MirrorResult{}, fmt.Errorf("create mirror dependency: %w", err)
	}

	record, err := s.Queries.CreateIssueMirror(ctx, db.CreateIssueMirrorParams{
		WorkspaceID:   issue.WorkspaceID,
		SourceIssueID: issue.ID,
		MirrorIssueID: created.Issue.ID,
		LinkID:        link.ID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return MirrorResult{}, fmt.Errorf("record issue mirror: %w", err)
		}
		// uq_issue_mirror_pair fired: a concurrent attach recorded the same
		// pair first. The issue and dependency above are that writer's too.
	}

	s.auditMirrorCreated(ctx, issue, created.Issue, link)
	return MirrorResult{Link: link, MirrorIssue: created.Issue, Record: record, Created: true}, nil
}

// mirrorDescription is the generated body: a provenance line, then the source
// description verbatim.
func mirrorDescription(identifier, sourceProject string, sourceDescription pgtype.Text) string {
	origin := "Generated from " + identifier
	if sourceProject != "" {
		origin += " (" + sourceProject + ")"
	}
	return origin + "\n\n" + sourceDescription.String
}

func (s *IssueMirrorService) issuePrefix(ctx context.Context, workspaceID pgtype.UUID) string {
	ws, err := s.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return ""
	}
	if ws.IssuePrefix != "" {
		return ws.IssuePrefix
	}
	return "ISSUE"
}

func (s *IssueMirrorService) projectTitle(ctx context.Context, workspaceID, projectID pgtype.UUID) string {
	if !projectID.Valid {
		return ""
	}
	p, err := s.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil {
		return ""
	}
	return p.Title
}

func (s *IssueMirrorService) auditMirrorCreated(ctx context.Context, source, mirror db.Issue, link db.ProjectMirrorLink) {
	details, err := json.Marshal(map[string]any{
		"source_issue_id":   util.UUIDToString(source.ID),
		"mirror_issue_id":   util.UUIDToString(mirror.ID),
		"link_id":           util.UUIDToString(link.ID),
		"target_project_id": util.UUIDToString(link.TargetProjectID),
		"trigger_label":     link.TriggerLabel,
	})
	if err != nil {
		details = []byte("{}")
	}
	if _, err := s.Queries.CreateAuditLogEntry(ctx, db.CreateAuditLogEntryParams{
		WorkspaceID: source.WorkspaceID, ActorType: "system", Action: AuditMirrorCreated,
		EntityType: "issue", EntityID: mirror.ID, Details: details,
	}); err != nil {
		slog.Warn("mirror audit write failed", "mirror_issue_id", util.UUIDToString(mirror.ID), "error", err)
	}
}
