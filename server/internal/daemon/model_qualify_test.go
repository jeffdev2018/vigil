package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// stubModelDiscovery replaces the daemon's listModels indirection with a
// counting fake, so a test can assert BOTH what a task resolved to and how
// many discovery rounds it took to get there. The count is the contract:
// discovery is a CLI subprocess with a 15-30s ceiling that cachedDiscovery
// does not memoize when it returns empty or fallback, so "once" and "never"
// are behaviours worth pinning, not implementation detail.
func stubModelDiscovery(t *testing.T, catalogs map[string]agent.Catalog) func() int {
	t.Helper()
	var mu sync.Mutex
	calls := 0

	orig := listModels
	listModels = func(_ context.Context, provider string, _ agent.Command) (agent.Catalog, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return catalogs[provider], nil
	}
	t.Cleanup(func() { listModels = orig })

	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

func quietTaskLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// thinkingCatalogs mirrors the shapes the reporter's gateway config produces:
// a slash-shaped model id under a custom provider, advertising a reasoning
// catalog. codex carries a service tier so the codex-specific paths are real.
func thinkingCatalogs() map[string]agent.Catalog {
	gatewayOpus := agent.Model{
		ID:       "multica-anthropic/claude/claude-opus-5",
		Provider: "multica-anthropic",
		Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{
			{Value: "high", Label: "High"},
		}},
	}
	return map[string]agent.Catalog{
		"pi":       {Models: []agent.Model{gatewayOpus}},
		"opencode": {Models: []agent.Model{gatewayOpus}},
		"omp":      {Models: []agent.Model{gatewayOpus}},
		"claude": {Models: []agent.Model{{
			ID:       "claude-opus-5",
			Provider: "anthropic",
			Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{
				{Value: "high", Label: "High"},
			}},
		}}},
		"codex": {Models: []agent.Model{{
			ID:           "gpt-5.6-sol",
			Provider:     "openai",
			ServiceTiers: []agent.ModelServiceTier{{ID: "priority", Name: "Priority"}},
			Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{
				{Value: "high", Label: "High"},
			}},
		}}},
	}
}

// TestResolveTaskModelSelectionReadsTheCatalogAtMostOnce is the production
// path the previous round left unguarded (MUL-6471 review): a task that both
// qualifies its model and validates a capability override must not pay for
// discovery twice. It also pins the other half — the tasks that must not
// reach discovery at all.
func TestResolveTaskModelSelectionReadsTheCatalogAtMostOnce(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		in        taskModelSelection
		want      taskModelSelection
		wantReads int
	}{
		{
			// The reporter's exact configuration. One read serves both the
			// selector promotion and the thinking-level check; before the fix
			// the id never matched the catalog, so the level was dropped.
			name:      "pi qualifies and validates on a single read",
			provider:  "pi",
			in:        taskModelSelection{Model: "claude/claude-opus-5", ThinkingLevel: "high"},
			want:      taskModelSelection{Model: "multica-anthropic/claude/claude-opus-5", ThinkingLevel: "high"},
			wantReads: 1,
		},
		{
			name:      "opencode qualifies and validates on a single read",
			provider:  "opencode",
			in:        taskModelSelection{Model: "claude/claude-opus-5", ThinkingLevel: "high"},
			want:      taskModelSelection{Model: "multica-anthropic/claude/claude-opus-5", ThinkingLevel: "high"},
			wantReads: 1,
		},
		{
			// pi launches an unqualified id correctly on its own, so with no
			// capability override there is nothing to look up.
			name:      "pi without a capability override never reads the catalog",
			provider:  "pi",
			in:        taskModelSelection{Model: "claude/claude-opus-5"},
			want:      taskModelSelection{Model: "claude/claude-opus-5"},
			wantReads: 0,
		},
		{
			name:      "omp inherits pi's launch contract",
			provider:  "omp",
			in:        taskModelSelection{Model: "claude/claude-opus-5"},
			want:      taskModelSelection{Model: "claude/claude-opus-5"},
			wantReads: 0,
		},
		{
			// opencode cannot launch an unqualified selector, so it reads even
			// with no capability override — the one case that must pay.
			name:      "opencode reads even without a capability override",
			provider:  "opencode",
			in:        taskModelSelection{Model: "claude/claude-opus-5"},
			want:      taskModelSelection{Model: "multica-anthropic/claude/claude-opus-5"},
			wantReads: 1,
		},
		{
			name:      "claude with no override never reads the catalog",
			provider:  "claude",
			in:        taskModelSelection{Model: "claude-opus-5"},
			want:      taskModelSelection{Model: "claude-opus-5"},
			wantReads: 0,
		},
		{
			name:      "claude with a thinking level reads exactly once",
			provider:  "claude",
			in:        taskModelSelection{Model: "claude-opus-5", ThinkingLevel: "high"},
			want:      taskModelSelection{Model: "claude-opus-5", ThinkingLevel: "high"},
			wantReads: 1,
		},
		{
			// codex validates both overrides; they share the one read.
			name:      "codex validates thinking and service tier on a single read",
			provider:  "codex",
			in:        taskModelSelection{Model: "gpt-5.6-sol", ThinkingLevel: "high", ServiceTier: "priority"},
			want:      taskModelSelection{Model: "gpt-5.6-sol", ThinkingLevel: "high", ServiceTier: "priority"},
			wantReads: 1,
		},
		{
			// codex with no explicit model fails both checks closed, and does
			// so without a catalog read — the guard predates this change and
			// must survive it.
			name:      "codex without a model fails closed without reading",
			provider:  "codex",
			in:        taskModelSelection{ThinkingLevel: "high", ServiceTier: "priority"},
			want:      taskModelSelection{},
			wantReads: 0,
		},
		{
			name:      "no pinned model and no overrides reads nothing",
			provider:  "opencode",
			in:        taskModelSelection{},
			want:      taskModelSelection{},
			wantReads: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := stubModelDiscovery(t, thinkingCatalogs())

			got := resolveTaskModelSelection(context.Background(), tt.provider, agent.Command{}, tt.in, quietTaskLog())
			if got != tt.want {
				t.Errorf("resolveTaskModelSelection(%s, %+v) = %+v, want %+v", tt.provider, tt.in, got, tt.want)
			}
			if reads() != tt.wantReads {
				t.Errorf("catalog reads = %d, want %d", reads(), tt.wantReads)
			}
		})
	}
}

// A runtime that cannot answer must not block the task: the persisted model
// may well be exactly what its CLI expects, and a stale-looking capability
// override is kept rather than silently dropped on a transient failure. The
// failed read is still only attempted once.
func TestResolveTaskModelSelectionFailsOpenOnDiscoveryError(t *testing.T) {
	var mu sync.Mutex
	calls := 0

	orig := listModels
	listModels = func(_ context.Context, _ string, _ agent.Command) (agent.Catalog, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return agent.Catalog{}, context.DeadlineExceeded
	}
	t.Cleanup(func() { listModels = orig })

	in := taskModelSelection{Model: "claude/claude-opus-5", ThinkingLevel: "high"}
	got := resolveTaskModelSelection(context.Background(), "opencode", agent.Command{}, in, quietTaskLog())
	if got != in {
		t.Errorf("resolveTaskModelSelection on discovery error = %+v, want %+v unchanged", got, in)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("catalog reads = %d, want 1 — a failed read must not be retried within the task", calls)
	}
}

// TestResolveTaskModelCascadeOrder pins the tier order JEF-12 added on top of
// the old two-tier resolution: task.model_override (the claim's
// agent_task_queue.model_override) beats agent.model, which beats the
// daemon-wide env var, and an all-empty cascade still passes "" through so
// the backend omits --model and the CLI's own default applies.
func TestResolveTaskModelCascadeOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		task     Task
		envModel string
		want     string
	}{
		{
			name:     "override beats agent model and env",
			task:     Task{ModelOverride: "claude-opus-5", Agent: &AgentData{Model: "claude-sonnet-5"}},
			envModel: "claude-haiku-4",
			want:     "claude-opus-5",
		},
		{
			name: "override beats agent model with empty env",
			task: Task{ModelOverride: "gpt-5.6", Agent: &AgentData{Model: "gpt-5-mini"}},
			want: "gpt-5.6",
		},
		{
			name:     "agent model wins when no override",
			task:     Task{Agent: &AgentData{Model: "claude-sonnet-5"}},
			envModel: "claude-haiku-4",
			want:     "claude-sonnet-5",
		},
		{
			name:     "env wins when neither override nor agent model",
			envModel: "claude-haiku-4",
			want:     "claude-haiku-4",
		},
		{
			name: "all empty passes through for the CLI default",
			want: "",
		},
		{
			name:     "nil agent with override still wins",
			task:     Task{ModelOverride: "claude-opus-5"},
			envModel: "claude-haiku-4",
			want:     "claude-opus-5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveTaskModel(tt.task, tt.envModel); got != tt.want {
				t.Errorf("resolveTaskModel(%+v, %q) = %q, want %q", tt.task, tt.envModel, got, tt.want)
			}
		})
	}
}

// TestTaskWireDecodesModelOverride pins the claim-payload contract: the
// server's claim query returns agent_task_queue.model_override, and the
// daemon must actually read it off the wire — a field that decodes nowhere
// is a cascade tier that never fires.
func TestTaskWireDecodesModelOverride(t *testing.T) {
	t.Parallel()
	var task Task
	if err := json.Unmarshal([]byte(`{"id":"t-1","model_override":"claude-opus-5","agent":{"model":"claude-sonnet-5"}}`), &task); err != nil {
		t.Fatalf("unmarshal claim payload: %v", err)
	}
	if task.ModelOverride != "claude-opus-5" {
		t.Errorf("ModelOverride = %q, want %q from the claim payload", task.ModelOverride, "claude-opus-5")
	}
	if got := resolveTaskModel(task, ""); got != "claude-opus-5" {
		t.Errorf("resolveTaskModel on the decoded claim = %q, want the override to win", got)
	}
}
