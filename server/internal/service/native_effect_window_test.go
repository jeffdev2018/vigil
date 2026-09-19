package service

import (
	"testing"
	"time"
)

func TestNativeEffectWindowFromSettings(t *testing.T) {
	def := NativeEffectWindowFromSettings(nil)
	if def.Max != nativeEffectWindowMaxDefault || def.Window != nativeEffectWindowDefault {
		t.Fatalf("defaults = %+v", def)
	}
	got := NativeEffectWindowFromSettings([]byte(`{"native_effect_window":{"max":5,"window_seconds":120}}`))
	if got.Max != 5 || got.Window != 2*time.Minute {
		t.Fatalf("parsed = %+v", got)
	}
	off := NativeEffectWindowFromSettings([]byte(`{"native_effect_window":{"max":0}}`))
	if off.Max != 0 {
		t.Fatalf("max 0 should disable, got %d", off.Max)
	}
}

// Two runs that together exceed the window: the second is refused; after the
// window elapses, effects are accepted again.
func TestNativeAgentEffectWindow(t *testing.T) {
	svc := NewNativeAgentService(nil, nil, nil, &scriptedNativeLLM{}, nil)
	cfg := NativeEffectWindow{Max: 3, Window: time.Minute}
	now := time.Unix(1_700_000_000, 0)
	agent := "agent-a"

	for i := 0; i < 3; i++ {
		if reason := svc.observeAgentEffect(agent, cfg, now.Add(time.Duration(i)*time.Second)); reason != "" {
			t.Fatalf("effect %d refused early: %s", i, reason)
		}
	}
	if reason := svc.observeAgentEffect(agent, cfg, now.Add(3*time.Second)); reason == "" {
		t.Fatal("expected window refusal on the 4th effect")
	}
	// A different agent is unaffected.
	if reason := svc.observeAgentEffect("agent-b", cfg, now.Add(3*time.Second)); reason != "" {
		t.Fatalf("peer agent refused: %s", reason)
	}
	// After the window, the oldest effects fall out.
	if reason := svc.observeAgentEffect(agent, cfg, now.Add(cfg.Window+time.Second)); reason != "" {
		t.Fatalf("after window reset refused: %s", reason)
	}
}
