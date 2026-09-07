package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The settings file Multica passes to Claude is where the command gate is
// registered. Before the gate existed the file was written only when a runtime
// skill was disabled, so a run with a narrow allowlist and no disabled skill
// got no file and no gate.
func TestClaudeSettingsCarryTheShellHook(t *testing.T) {
	read := func(t *testing.T, path string) map[string]any {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("settings are not JSON: %v", err)
		}
		return payload
	}

	t.Run("a hook alone produces the file", func(t *testing.T) {
		root := t.TempDir()
		hook := claudeShellHookFor(root, &ClaudeShellHook{Command: "/usr/local/bin/multica"})
		if hook == nil || hook.MarkerPath != filepath.Join(root, ClaudeHookMarkerFile) {
			t.Fatalf("hook = %+v, want the marker beside the settings file", hook)
		}
		path, err := prepareClaudeSkillSettings(root, nil, nil, hook)
		if err != nil || path == "" {
			t.Fatalf("path = %q, err = %v: a run with an allowlist and no disabled skill still needs the file", path, err)
		}
		payload := read(t, path)
		if _, hasSkills := payload["skillOverrides"]; hasSkills {
			t.Error("no skill was disabled, so the file must not claim a skill policy")
		}
		hooks, ok := payload["hooks"].(map[string]any)
		if !ok {
			t.Fatalf("payload = %+v, want a hooks block", payload)
		}
		events, ok := hooks["PreToolUse"].([]any)
		if !ok || len(events) != 1 {
			t.Fatalf("PreToolUse = %+v, want one matcher group", hooks["PreToolUse"])
		}
		group := events[0].(map[string]any)
		if group["matcher"] != "Bash" {
			t.Errorf("matcher = %v, want the Bash tool and nothing else", group["matcher"])
		}
		handler := group["hooks"].([]any)[0].(map[string]any)
		if handler["type"] != "command" || handler["command"] != "/usr/local/bin/multica hook pre-tool-use" {
			t.Errorf("handler = %+v, want the daemon's own binary", handler)
		}
	})

	t.Run("no hook and no disabled skill removes the file", func(t *testing.T) {
		root := t.TempDir()
		stale := filepath.Join(root, claudeRuntimeSkillSettingsFile)
		if err := os.WriteFile(stale, []byte(`{"hooks":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		path, err := prepareClaudeSkillSettings(root, nil, nil, nil)
		if err != nil || path != "" {
			t.Fatalf("path = %q, err = %v, want none", path, err)
		}
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Error("a run that gates nothing must not inherit the previous run's settings file")
		}
	})

	t.Run("an incomplete request registers nothing", func(t *testing.T) {
		root := t.TempDir()
		// No binary to call means no gate. Registering a hook whose command
		// cannot run would be worse than none: the CLI would report a hook
		// error and keep going, and we would have claimed a control.
		if hook := claudeShellHookFor(root, &ClaudeShellHook{Command: "  "}); hook != nil {
			t.Errorf("hook = %+v, want none without a command", hook)
		}
		if hook := claudeShellHookFor(root, nil); hook != nil {
			t.Errorf("hook = %+v, want none when the run declares no allowlist", hook)
		}
		if hook := claudeShellHookFor("", &ClaudeShellHook{Command: "/bin/multica"}); hook != nil {
			t.Errorf("hook = %+v, want none without an env root to put the marker in", hook)
		}
	})
}
