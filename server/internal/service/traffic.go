package service

import (
	"encoding/json"
	"path"
	"strings"
)

// Traffic control (K18), pure parts: which tool calls edit files, which
// paths they name, how a run's paths compare with a human's dirty files.

// TrafficControl is the workspace setting under settings.traffic_control.
type TrafficControl struct {
	// PauseOnConflict asks the run to pause (K19 primitive) instead of only alerting.
	PauseOnConflict bool `json:"pause_on_conflict"`
}

func TrafficControlSettings(settings []byte) TrafficControl {
	var s struct {
		TC *TrafficControl `json:"traffic_control"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.TC == nil {
		return TrafficControl{}
	}
	return *s.TC
}

// HumanEditWindow is how recent a human's dirty file must be to count.
const HumanEditWindowSeconds = 5 * 60

// DirtyCheckout is what a daemon reports per local checkout it manages.
type DirtyCheckout struct {
	Root  string   `json:"root"`
	Paths []string `json:"paths"`
}

var editingToolWords = []string{"edit", "write", "patch", "create", "replace", "insert", "delete", "remove", "rename", "move"}

// IsEditingTool reports whether a tool name suggests it changes files.
func IsEditingTool(name string) bool {
	lower := strings.ToLower(name)
	for _, w := range editingToolWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// ToolInputPaths lists the path-like arguments of a tool call.
func ToolInputPaths(input []byte) []string {
	var args map[string]any
	if json.Unmarshal(input, &args) != nil {
		return nil
	}
	var out []string
	add := func(v any) {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" && !strings.Contains(s, "\n") {
			out = append(out, strings.TrimSpace(s))
		}
	}
	for _, key := range []string{"file_path", "path", "notebook_path", "filePath", "target", "old_path", "new_path"} {
		add(args[key])
	}
	for _, key := range []string{"paths", "files"} {
		if list, ok := args[key].([]any); ok {
			for _, v := range list {
				add(v)
			}
		}
	}
	return out
}

// RelativePath strips the run's work dir (or any absolute prefix ending in
// the checkout root) so paths compare like git reports them.
func RelativePath(p, workDir string) string {
	p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if workDir != "" {
		wd := path.Clean(strings.ReplaceAll(workDir, "\\", "/"))
		if rel := strings.TrimPrefix(p, wd+"/"); rel != p {
			return rel
		}
	}
	return strings.TrimPrefix(p, "./")
}

// OverlapPaths returns the run's paths a human dirty file matches: equal
// relative paths, or an absolute run path ending in the dirty path.
func OverlapPaths(runPaths, dirty []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, rp := range runPaths {
		for _, d := range dirty {
			d = strings.TrimPrefix(path.Clean(d), "./")
			if rp == d || strings.HasSuffix(rp, "/"+d) {
				if !seen[rp] {
					seen[rp] = true
					out = append(out, rp)
				}
				break
			}
		}
	}
	return out
}

// IntersectPaths returns the paths present on both sides (agent vs agent).
func IntersectPaths(a, b []string) []string {
	set := map[string]bool{}
	for _, p := range b {
		set[p] = true
	}
	var out []string
	for _, p := range a {
		if set[p] {
			out = append(out, p)
			delete(set, p)
		}
	}
	return out
}
