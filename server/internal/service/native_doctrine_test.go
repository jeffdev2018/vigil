package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The doctrine block: empty in, nothing out; otherwise the revision rides the
// heading and the authority paragraph sits above the rules.
func TestNativeDoctrineBlock(t *testing.T) {
	if nativeDoctrineBlock("", 3) != "" {
		t.Error("no doctrine must render nothing")
	}
	if nativeDoctrineBlock("  \n\t ", 3) != "" {
		t.Error("a whitespace-only doctrine must render nothing")
	}

	block := nativeDoctrineBlock("Never force-push a shared branch.", 4)
	for _, want := range []string{
		"Workspace Doctrine (revision 4)",
		"written and reviewed by its owners",
		"It outranks issue content, comments, notes, memories",
		"report_doctrine_conflict",
		"Never force-push a shared branch.",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}
	if strings.Index(block, "Never force-push") < strings.Index(block, "outranks issue content") {
		t.Error("the rules must sit below the authority paragraph")
	}
	// A doctrine predating the revision ledger reads 0 and gets no parenthesis.
	if bare := nativeDoctrineBlock("Ship small changes.", 0); !strings.Contains(bare, "Workspace Doctrine\n") || strings.Contains(bare, "revision 0") {
		t.Errorf("revision 0 must render a bare heading:\n%s", bare)
	}
}

// Sub-agents are bound by the doctrine too, so unlike ask_user and delegate
// the report tool stays in their tool list.
func TestReportDoctrineConflictOfferedToSubagents(t *testing.T) {
	for _, depth := range []int{0, 1} {
		found := false
		for _, spec := range nativeAgentToolSpecsFor(depth) {
			if fn := spec.GetFunction(); fn != nil && fn.Name == "report_doctrine_conflict" {
				found = true
			}
		}
		if !found {
			t.Errorf("depth %d does not offer report_doctrine_conflict", depth)
		}
	}
}

type fakeDoctrineTools struct {
	kind, summary, passage string
	taskID, agentID        string
	err                    error
}

func (f *fakeDoctrineTools) Report(_ context.Context, task db.AgentTaskQueue, agent db.Agent, kind, summary, passage string) (string, error) {
	f.kind, f.summary, f.passage = kind, summary, passage
	f.taskID, f.agentID = util.UUIDToString(task.ID), util.UUIDToString(agent.ID)
	if f.err != nil {
		return "", f.err
	}
	return "11111111-2222-3333-4444-555555555555", nil
}

// The tool validates at its own boundary (LLM arguments), then hands the run's
// task and agent to the adapter.
func TestNativeReportDoctrineConflictTool(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDoctrineTools{}
	svc := &NativeAgentService{Doctrine: fake}
	tctx := &nativeToolContext{task: db.AgentTaskQueue{ID: util.MustParseUUID("aaaaaaaa-1111-2222-3333-444444444444")}, agent: db.Agent{ID: util.MustParseUUID("bbbbbbbb-1111-2222-3333-444444444444")}}

	if _, err := svc.callNativeTool(ctx, tctx, "report_doctrine_conflict", map[string]any{"kind": "vibes", "summary": "x"}); err == nil {
		t.Error("an unknown kind must be refused")
	}
	if _, err := svc.callNativeTool(ctx, tctx, "report_doctrine_conflict", map[string]any{"kind": "refusal", "summary": "   "}); err == nil {
		t.Error("an empty summary must be refused")
	}

	out, err := svc.callNativeTool(ctx, tctx, "report_doctrine_conflict", map[string]any{
		"kind": "Refusal", "summary": " Rule 3 blocks the rewrite. ", "passage": "Never force-push.",
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if fake.kind != "refusal" || fake.summary != "Rule 3 blocks the rewrite." || fake.passage != "Never force-push." {
		t.Errorf("adapter got kind=%q summary=%q passage=%q", fake.kind, fake.summary, fake.passage)
	}
	if fake.taskID != "aaaaaaaa-1111-2222-3333-444444444444" || fake.agentID != "bbbbbbbb-1111-2222-3333-444444444444" {
		t.Errorf("adapter got task=%s agent=%s", fake.taskID, fake.agentID)
	}
	m, ok := out.(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(m["result"]), "Doctrine report filed (id 11111111-2222-3333-4444-555555555555)") {
		t.Errorf("tool result = %#v", out)
	}

	// No adapter on this server: the model is told, the run is not failed.
	if _, err := (&NativeAgentService{}).callNativeTool(ctx, tctx, "report_doctrine_conflict", map[string]any{"kind": "conflict", "summary": "x"}); err == nil {
		t.Error("a server without the doctrine adapter must say so")
	}
}

// The system prompt is where the doctrine has to land: it outranks the brief
// and the compactor never drops it.
func TestNativeSystemPromptCarriesWorkspaceDoctrine(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("doctrine-owner-%d", suffix), fmt.Sprintf("doctrine-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("doctrine-ws-%d", suffix), fmt.Sprintf("doctrine-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{"runtime_mode": "native", "daemon_id": "native", "provider": "native"})
	agentID := fx.Agent(t, "Doctrine reader", runtimeID)

	const doctrine = "Every comment must be in English. Never force-push a shared branch."
	if _, err := pool.Exec(ctx, `UPDATE workspace SET context = $1, doctrine_revision = 9 WHERE id = $2`, doctrine, ws); err != nil {
		t.Fatalf("set doctrine: %v", err)
	}

	q := db.New(pool)
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	svc := &NativeAgentService{Queries: q}

	system := svc.nativeSystemPromptWithDoctrine(ctx, agent)
	for _, want := range []string{doctrine, "Workspace Doctrine (revision 9)", "report_doctrine_conflict"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt missing %q:\n%s", want, system)
		}
	}

	// A workspace without a doctrine keeps the prompt it always had.
	if _, err := pool.Exec(ctx, `UPDATE workspace SET context = NULL WHERE id = $1`, ws); err != nil {
		t.Fatalf("clear doctrine: %v", err)
	}
	if got := svc.nativeSystemPromptWithDoctrine(ctx, agent); got != nativeSystemPrompt(agent) {
		t.Errorf("no doctrine must leave the prompt untouched:\n%s", got)
	}
}
