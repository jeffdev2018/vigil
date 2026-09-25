package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	openai "github.com/openai/openai-go/v3"
)

func TestNativeSkillsBlock(t *testing.T) {
	if got := nativeSkillsBlock(nil); got != "" {
		t.Fatalf("empty skills should yield empty block, got %q", got)
	}
	got := nativeSkillsBlock([]AgentSkillData{{
		Name:        "refund-policy",
		Description: "How to handle refunds",
		Content:     "Always verify the order id before promising a refund.",
	}})
	for _, want := range []string{"Enabled skills", "refund-policy", "Always verify the order id", "do not outrank workspace doctrine"} {
		if !strings.Contains(got, want) {
			t.Fatalf("block missing %q:\n%s", want, got)
		}
	}
}

// N17 — an agent with an enabled skill gets that skill in the system prompt;
// a disabled binding and an agent with no skills do not.
func TestNativeAgentInjectsEnabledSkills(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-owner-%d", suffix), fmt.Sprintf("native-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-ws-%d", suffix), fmt.Sprintf("native-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	if _, err := pool.Exec(ctx, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"goal_loop":{"max_continuations":0}}'::jsonb WHERE id = $1`, ws); err != nil {
		t.Fatalf("disable goal loop: %v", err)
	}
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	withSkill := fx.Agent(t, "Skilled worker", runtimeID)
	plain := fx.Agent(t, "Plain worker", runtimeID)

	skillID := fx.Insert(t, "skill", testutil.Cols{
		"workspace_id": ws,
		"name":         "helpdesk-tone",
		"description":  "Tone for customer replies",
		"content":      "UNIQUE_SKILL_MARKER_N17: reply warmly and never invent order numbers.",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   user,
	})
	fx.InsertNoID(t, "agent_skill", testutil.Cols{
		"agent_id": withSkill,
		"skill_id": skillID,
		"enabled":  true,
	}, "agent_id = $1 AND skill_id = $2", withSkill, skillID)
	disabledSkill := fx.Insert(t, "skill", testutil.Cols{
		"workspace_id": ws,
		"name":         "disabled-skill",
		"description":  "Should not appear",
		"content":      "DISABLED_SKILL_MARKER_N17",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   user,
	})
	fx.InsertNoID(t, "agent_skill", testutil.Cols{
		"agent_id": withSkill,
		"skill_id": disabledSkill,
		"enabled":  false,
	}, "agent_id = $1 AND skill_id = $2", withSkill, disabledSkill)

	issueID := fx.Issue(t, "Customer question")
	taskWith := fx.Task(t, withSkill, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
	taskPlain := fx.Task(t, plain, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})

	tasks := NewTaskService(db.New(pool), pool, nil, events.New())
	issues := NewIssueService(db.New(pool), pool, events.New(), nil, tasks)

	runOnce := func(agentID, taskID string) string {
		llm := &scriptedNativeLLM{turns: []openai.ChatCompletion{nativeTextTurn("ok")}}
		svc := NewNativeAgentService(db.New(pool), tasks, issues, llm, events.New())
		claimed, err := tasks.claimTask(ctx, util.MustParseUUID(agentID), util.MustParseUUID(runtimeID), false)
		if err != nil || claimed == nil {
			t.Fatalf("claim %s: %v (%v)", taskID, claimed, err)
		}
		svc.runTask(ctx, *claimed)
		if len(llm.first.Messages) == 0 || llm.first.Messages[0].OfSystem == nil {
			t.Fatalf("no system message on first call")
		}
		return llm.first.Messages[0].OfSystem.Content.OfString.Value
	}

	skilledPrompt := runOnce(withSkill, taskWith)
	if !strings.Contains(skilledPrompt, "UNIQUE_SKILL_MARKER_N17") {
		t.Fatalf("enabled skill missing from system prompt:\n%s", skilledPrompt)
	}
	if !strings.Contains(skilledPrompt, "Enabled skills") || !strings.Contains(skilledPrompt, "helpdesk-tone") {
		t.Fatalf("skills section incomplete:\n%s", skilledPrompt)
	}
	if strings.Contains(skilledPrompt, "DISABLED_SKILL_MARKER_N17") {
		t.Fatalf("disabled skill leaked into prompt")
	}

	plainPrompt := runOnce(plain, taskPlain)
	if strings.Contains(plainPrompt, "UNIQUE_SKILL_MARKER_N17") || strings.Contains(plainPrompt, "Enabled skills") {
		t.Fatalf("plain agent unexpectedly got skills:\n%s", plainPrompt)
	}
}

// Direct prompt builder: Tasks wired → skills; Tasks nil → unchanged base.
func TestNativeSystemPromptWithSkillsWiring(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-owner-%d", suffix), fmt.Sprintf("native-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-ws-%d", suffix), fmt.Sprintf("native-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	agentID := fx.Agent(t, "Wired worker", runtimeID)
	skillID := fx.Insert(t, "skill", testutil.Cols{
		"workspace_id": ws,
		"name":         "wiring-check",
		"description":  "",
		"content":      fmt.Sprintf("WIRE_MARKER_%d", suffix),
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   user,
	})
	fx.InsertNoID(t, "agent_skill", testutil.Cols{
		"agent_id": agentID,
		"skill_id": skillID,
		"enabled":  true,
	}, "agent_id = $1 AND skill_id = $2", agentID, skillID)
	agent, err := db.New(pool).GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	marker := fmt.Sprintf("WIRE_MARKER_%d", suffix)

	without := NewNativeAgentService(db.New(pool), nil, nil, &scriptedNativeLLM{}, events.New())
	got := without.nativeSystemPromptWithDoctrine(ctx, agent)
	if strings.Contains(got, marker) {
		t.Fatal("nil Tasks should not inject skills")
	}

	tasks := NewTaskService(db.New(pool), pool, nil, events.New())
	with := NewNativeAgentService(db.New(pool), tasks, nil, &scriptedNativeLLM{}, events.New())
	got = with.nativeSystemPromptWithDoctrine(ctx, agent)
	if !strings.Contains(got, marker) {
		t.Fatalf("wired Tasks should inject skill, prompt=\n%s", got)
	}
}
