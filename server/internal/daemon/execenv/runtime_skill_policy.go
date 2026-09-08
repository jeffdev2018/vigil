package execenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const claudeRuntimeSkillSettingsFile = "claude-runtime-skill-settings.json"

// ClaudeHookMarkerFile is the file the PreToolUse hook touches the first time
// it runs. Whether a hook runs at all is the one thing the daemon cannot see
// from outside — a CLI whose hooks were disabled looks exactly like a run that
// used no shell command — so a run that declared an allowlist and never
// observed the hook must not be reported as having enforced one.
const ClaudeHookMarkerFile = "claude-hook-observed"

// ClaudeShellHook registers `multica hook pre-tool-use` as a PreToolUse hook
// on the Bash tool. It is the only place a command allowlist can be decided:
// the hook receives the final command string, so chaining and wrapping are
// visible to it, where a provider permission rule only ever matches a prefix
// of what the model proposed.
type ClaudeShellHook struct {
	// Command is the multica binary the hook runs. The daemon resolves its own
	// executable rather than trusting PATH, because a hook does not
	// necessarily inherit the agent's.
	Command string
	// MarkerPath is touched on every invocation. See ClaudeHookMarkerFile.
	MarkerPath string
}

// RuntimeSkillRefForEnv identifies a runtime-local skill for provider-specific
// task environment filtering. Provider and runtime are already selected by the
// task, so only the discovery root and provider-native key are needed here.
type RuntimeSkillRefForEnv struct {
	Root   string
	Key    string
	Name   string
	Plugin string
}

func cleanRuntimeSkillKey(key string) (string, bool) {
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimSpace(key)))
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(cleaned), true
}

func prepareClaudeSkillSettings(envRoot string, disabled []RuntimeSkillRefForEnv, workspaceSkills []SkillContextForEnv, hook *ClaudeShellHook) (string, error) {
	path := filepath.Join(envRoot, claudeRuntimeSkillSettingsFile)
	if len(disabled) == 0 && hook == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return "", nil
	}

	overrides := make(map[string]string)
	deny := make([]string, 0, len(disabled)*2)
	seenDeny := make(map[string]struct{}, len(disabled)*2)
	addDeny := func(rule string) {
		if _, exists := seenDeny[rule]; exists {
			return
		}
		seenDeny[rule] = struct{}{}
		deny = append(deny, rule)
	}
	for _, skill := range disabled {
		key, ok := cleanRuntimeSkillKey(skill.Key)
		if !ok {
			continue
		}
		invocationName := strings.TrimSpace(skill.Name)
		if invocationName == "" {
			invocationName = filepath.Base(filepath.FromSlash(key))
		}
		if workspaceClaimsRuntimeSkill(invocationName, workspaceSkills) {
			continue
		}
		// Claude Code's skillOverrides fully hides personal/project skills.
		// Plugin skills ignore that setting, so the permission deny below is
		// also emitted for every key and is the enforcement path for plugins.
		if skill.Root != "plugin" {
			overrides[invocationName] = "off"
		} else {
			invocationName = key
		}
		addDeny("Skill(" + invocationName + ")")
		addDeny("Skill(" + invocationName + " *)")
	}
	if len(overrides) == 0 && len(deny) == 0 && hook == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return "", nil
	}
	payload := map[string]any{}
	if len(overrides) > 0 || len(deny) > 0 {
		payload["skillOverrides"] = overrides
		payload["permissions"] = map[string]any{"deny": deny}
	}
	if hook != nil {
		// matcher "Bash" is an exact tool-name match; the handler decides from
		// tool_input.command, which is the string the CLI is about to run.
		payload["hooks"] = map[string]any{
			"PreToolUse": []any{map[string]any{
				"matcher": "Bash",
				"hooks": []any{map[string]any{
					"type":          "command",
					"command":       hook.Command + " hook pre-tool-use",
					"statusMessage": "Checking the command against this run's allowed commands",
				}},
			}},
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func ensureCodexDisabledSkillsConfig(configPath, codexHome string, disabled []RuntimeSkillRefForEnv, workspaceSkills []SkillContextForEnv) error {
	if len(disabled) == 0 {
		return nil
	}
	home := ""
	paths := make([]string, 0, len(disabled))
	seen := make(map[string]struct{}, len(disabled))
	for _, skill := range disabled {
		key, ok := cleanRuntimeSkillKey(skill.Key)
		if !ok {
			continue
		}
		var skillPath string
		switch skill.Root {
		case "provider":
			firstKeyPart := strings.SplitN(key, "/", 2)[0]
			if workspaceClaimsRuntimeSkill(firstKeyPart, workspaceSkills) {
				continue
			}
			skillPath = filepath.Join(codexHome, "skills", filepath.FromSlash(key), "SKILL.md")
		case "universal":
			if home == "" {
				var err error
				home, err = os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("resolve user home for disabled Codex skills: %w", err)
				}
			}
			skillPath = filepath.Join(home, ".agents", "skills", filepath.FromSlash(key), "SKILL.md")
		default:
			continue
		}
		if _, exists := seen[skillPath]; exists {
			continue
		}
		seen[skillPath] = struct{}{}
		paths = append(paths, skillPath)
	}
	if len(paths) == 0 {
		return nil
	}
	file, err := os.OpenFile(configPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, path := range paths {
		block := "\n[[skills.config]]\npath = " + strconv.Quote(filepath.ToSlash(path)) + "\nenabled = false\n"
		if _, err := file.WriteString(block); err != nil {
			return err
		}
	}
	return nil
}

func workspaceClaimsRuntimeSkill(name string, workspaceSkills []SkillContextForEnv) bool {
	claim := sanitizeSkillName(name)
	for _, skill := range workspaceSkills {
		if sanitizeSkillName(skill.Name) == claim {
			return true
		}
	}
	return false
}

// claudeShellHookFor completes a hook the daemon asked for with the marker
// path inside this run's environment root, or returns nil when the run
// declares no allowlist. It is separate from the daemon's own resolution so
// the marker always lands beside the settings file that registered the hook.
func claudeShellHookFor(envRoot string, requested *ClaudeShellHook) *ClaudeShellHook {
	if requested == nil || strings.TrimSpace(requested.Command) == "" || strings.TrimSpace(envRoot) == "" {
		return nil
	}
	return &ClaudeShellHook{
		Command:    requested.Command,
		MarkerPath: filepath.Join(envRoot, ClaudeHookMarkerFile),
	}
}
