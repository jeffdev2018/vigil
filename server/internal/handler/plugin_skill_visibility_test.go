package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Disabling a plugin installation must cut every capability it granted, not
// just the ones an admin remembered to revoke by hand. Its skills are one of
// those capabilities: agent_skill.enabled stays TRUE (disabling a plugin is a
// different action from un-assigning a skill), so ListAgentSkills /
// ListAgentSkillsByIDs must themselves exclude a skill whose owning
// installation is disabled — a human-authored skill (no owning installation)
// is unaffected.
func TestListAgentSkillsExcludesDisabledPluginInstallation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	queries := db.New(testPool)

	runtimeID := dbfx.Runtime(t, "plugin-skill-vis runtime")
	agentID := dbfx.Agent(t, "plugin-skill-vis agent", runtimeID)

	installationID := dbfx.Insert(t, "plugin_installation", testutil.Cols{
		"workspace_id":       testWorkspaceID,
		"plugin_key":         "com.example.skill-visibility-test",
		"version":            "1.0.0",
		"manifest":           testutil.Raw(`'{"manifest_version":1}'::jsonb`),
		"package_version_id": testutil.Raw("gen_random_uuid()"),
		"enabled":            false,
	})

	pluginSkillID := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id":           testWorkspaceID,
		"name":                   "plugin-skill-vis-plugin-skill",
		"description":            "Owned by a disabled installation",
		"content":                "plugin skill content",
		"config":                 testutil.Raw("'{}'::jsonb"),
		"created_by":             testUserID,
		"plugin_installation_id": installationID,
	})
	dbfx.InsertNoID(t, "agent_skill", testutil.Cols{
		"agent_id": agentID,
		"skill_id": pluginSkillID,
		"enabled":  true,
	}, "skill_id = $1", pluginSkillID)

	humanSkillID := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "plugin-skill-vis-human-skill",
		"description":  "Human authored, unaffected",
		"content":      "human skill content",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})
	dbfx.InsertNoID(t, "agent_skill", testutil.Cols{
		"agent_id": agentID,
		"skill_id": humanSkillID,
		"enabled":  true,
	}, "skill_id = $1", humanSkillID)

	assertVisible := func(t *testing.T, wantHuman, wantPlugin bool) {
		t.Helper()
		skills, err := queries.ListAgentSkills(ctx, parseUUID(agentID))
		if err != nil {
			t.Fatalf("ListAgentSkills: %v", err)
		}
		gotHuman, gotPlugin := false, false
		for _, s := range skills {
			if parseUUID(humanSkillID) == s.ID {
				gotHuman = true
			}
			if parseUUID(pluginSkillID) == s.ID {
				gotPlugin = true
			}
		}
		if gotHuman != wantHuman {
			t.Fatalf("ListAgentSkills human skill present = %v, want %v", gotHuman, wantHuman)
		}
		if gotPlugin != wantPlugin {
			t.Fatalf("ListAgentSkills plugin skill present = %v, want %v", gotPlugin, wantPlugin)
		}

		scoped, err := queries.ListAgentSkillsByIDs(ctx, db.ListAgentSkillsByIDsParams{
			AgentID:  parseUUID(agentID),
			SkillIds: []pgtype.UUID{parseUUID(humanSkillID), parseUUID(pluginSkillID)},
		})
		if err != nil {
			t.Fatalf("ListAgentSkillsByIDs: %v", err)
		}
		wantCount := 0
		if wantHuman {
			wantCount++
		}
		if wantPlugin {
			wantCount++
		}
		if len(scoped) != wantCount {
			t.Fatalf("ListAgentSkillsByIDs returned %d skills, want %d", len(scoped), wantCount)
		}
	}

	assertVisible(t, true, false)

	dbfx.Exec(t, "UPDATE plugin_installation SET enabled = TRUE WHERE id = $1", installationID)
	assertVisible(t, true, true)
}
