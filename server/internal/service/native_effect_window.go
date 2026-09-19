package service

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Rule of Two, temporal (N11): the per-run effectful cap stops one confused
// loop, but not a frenzy of short runs that together thrash the workspace.
// A sliding window per agent refuses further state-changing tools once the
// count in the window is spent; the refusal asks the model to wrap up, same
// as the per-run cap.
//
// Defaults match the plan (20 effects / 10 min). Workspace settings may
// raise or lower them under `native_effect_window` (N07-shaped config).

const (
	nativeEffectWindowMaxDefault = 20
	nativeEffectWindowDefault    = 10 * time.Minute
	nativeEffectWindowMaxCap     = 200
	nativeEffectWindowMinSeconds = 60
	nativeEffectWindowMaxSeconds = 24 * 60 * 60
)

// NativeEffectWindow is the sliding-window policy for one workspace.
type NativeEffectWindow struct {
	Max    int           `json:"max"`
	Window time.Duration `json:"-"`
}

// NativeEffectWindowFromSettings reads settings.native_effect_window.
// Missing or partial config keeps the defaults; zero max disables the window.
func NativeEffectWindowFromSettings(settings []byte) NativeEffectWindow {
	out := NativeEffectWindow{Max: nativeEffectWindowMaxDefault, Window: nativeEffectWindowDefault}
	if len(settings) == 0 {
		return out
	}
	var s struct {
		W *struct {
			Max           *int `json:"max"`
			WindowSeconds *int `json:"window_seconds"`
		} `json:"native_effect_window"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.W == nil {
		return out
	}
	if s.W.Max != nil {
		m := *s.W.Max
		if m < 0 {
			m = 0
		}
		if m > nativeEffectWindowMaxCap {
			m = nativeEffectWindowMaxCap
		}
		out.Max = m
	}
	if s.W.WindowSeconds != nil {
		sec := *s.W.WindowSeconds
		if sec < nativeEffectWindowMinSeconds {
			sec = nativeEffectWindowMinSeconds
		}
		if sec > nativeEffectWindowMaxSeconds {
			sec = nativeEffectWindowMaxSeconds
		}
		out.Window = time.Duration(sec) * time.Second
	}
	return out
}

type agentEffectTimes struct {
	mu sync.Mutex
	at []time.Time
}

// observeAgentEffect records one state-changing tool call for the agent and
// returns a refusal reason when the sliding window is spent. An empty reason
// means the call may proceed. max<=0 disables the window.
func (s *NativeAgentService) observeAgentEffect(agentID string, cfg NativeEffectWindow, now time.Time) string {
	if s == nil || cfg.Max <= 0 || agentID == "" {
		return ""
	}
	window := cfg.Window
	if window <= 0 {
		window = nativeEffectWindowDefault
	}
	v, _ := s.agentEffects.LoadOrStore(agentID, &agentEffectTimes{})
	log := v.(*agentEffectTimes)
	log.mu.Lock()
	defer log.mu.Unlock()

	cutoff := now.Add(-window)
	kept := log.at[:0]
	for _, t := range log.at {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	log.at = kept
	if len(log.at) >= cfg.Max {
		return fmt.Sprintf("the agent's effectful-action window (%d state-changing calls in %s) is spent; stop changing the workspace and give your final answer", cfg.Max, window)
	}
	log.at = append(log.at, now)
	return ""
}
