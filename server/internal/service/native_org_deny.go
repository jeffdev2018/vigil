package service

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/mcpgov"
)

// Organization denies on the native tool catalogue (N08). The unit's deny
// list already governs the CLI runs' MCP catalogue (#164, mcpgov.ApplyOrgDeny);
// the native loop gets the same tightening: a tool whose name or description
// a denied verb matches as NEVER is absent from the specs the model sees AND
// refused at dispatch. ASK matches (send_external_without_approval matches
// "post" in "Post a comment") stay for the trust/preview work (N10) —
// removing them would take commenting away from every org workspace today.

// nativeToolDeniedByOrg reports whether a native tool is refused outright by
// one of the unit's denied verbs.
func nativeToolDeniedByOrg(name, description string, denies []string) bool {
	for _, verb := range denies {
		if mcpgov.OrgDenyClass(verb, name, description) == mcpgov.ClassNever {
			return true
		}
	}
	return false
}

// nativeOrgDenies resolves the deny list of the org unit holding this issue:
// the latest explicit routing first, then the issue's assignee (member,
// agent or squad), then the agent itself as a unit member. Nil when the
// workspace has no org structure — the deny pipeline is inert there, exactly
// like the CLI path.
func (s *NativeAgentService) nativeOrgDenies(ctx context.Context, wsID pgtype.UUID, issue *db.Issue, agentID pgtype.UUID) []string {
	if s.Queries == nil || issue == nil {
		return nil
	}
	structure, ok := s.orgStructureFor(ctx, wsID, issue.ProjectID)
	if !ok {
		return nil
	}
	var def struct {
		Units []struct {
			ID      string   `json:"id"`
			Deny    []string `json:"deny"`
			SquadID string   `json:"squad_id"`
			Members []struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"members"`
		} `json:"units"`
	}
	if err := json.Unmarshal(structure.Definition, &def); err != nil {
		return nil
	}
	unitFor := func(match func(u int) bool) []string {
		for i := range def.Units {
			if match(i) {
				return def.Units[i].Deny
			}
		}
		return nil
	}
	if routing, err := s.Queries.GetLatestOrgRoutingForIssue(ctx, db.GetLatestOrgRoutingForIssueParams{WorkspaceID: wsID, IssueID: issue.ID}); err == nil {
		if denies := unitFor(func(i int) bool { return def.Units[i].ID == routing.UnitID }); denies != nil {
			return denies
		}
	}
	if issue.AssigneeID.Valid {
		assigneeID := util.UUIDToString(issue.AssigneeID)
		assigneeType := issue.AssigneeType.String
		if denies := unitFor(func(i int) bool {
			if def.Units[i].SquadID != "" && def.Units[i].SquadID == assigneeID {
				return true
			}
			for _, m := range def.Units[i].Members {
				if m.Type == assigneeType && m.ID == assigneeID {
					return true
				}
			}
			return false
		}); denies != nil {
			return denies
		}
	}
	agentStr := util.UUIDToString(agentID)
	return unitFor(func(i int) bool {
		for _, m := range def.Units[i].Members {
			if m.Type == "agent" && m.ID == agentStr {
				return true
			}
		}
		return false
	})
}

func (s *NativeAgentService) orgStructureFor(ctx context.Context, wsID, projectID pgtype.UUID) (db.OrgStructure, bool) {
	if projectID.Valid {
		if st, err := s.Queries.GetOrgStructureForProject(ctx, db.GetOrgStructureForProjectParams{WorkspaceID: wsID, ProjectID: projectID}); err == nil {
			return st, true
		}
	}
	st, err := s.Queries.GetOrgStructureDefault(ctx, wsID)
	return st, err == nil
}
