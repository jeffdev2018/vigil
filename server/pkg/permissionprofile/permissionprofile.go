// Package permissionprofile holds what an agent may touch when it runs
// (K06). A profile is resolved once when a run is claimed (the run's
// override, else the agent's profile) and travels with the task to the
// daemon, which enforces what the provider can enforce and tells the model
// the rest. It is shared by the server and the daemon, so it depends on
// nothing but the blast radius globs.
//
// SCOPE, stated negatively first, because the name invites the wrong reading.
// A permission profile is NOT a sandbox and does not bound what the agent
// process can do on the machine. Multica launches a third-party CLI as a
// subprocess and does not own its loop, so a shell command the CLI decides to
// run passes through no Multica code. Confinement is SandboxSpec's job
// (internal/daemon/sandbox.go), and it is the thing that either holds or
// refuses the run.
//
// What each field actually does:
//
//   - ReadOnly: enforced for the providers that expose a flag for it —
//     Claude through the --settings deny rules below, Codex through
//     --sandbox read-only. Told to the model on every other provider.
//   - DeniedPaths: enforced on the paths Multica sees, which are the MCP
//     tool-call arguments (daemon's remoteMCPToolGate) and the push gate, plus
//     Claude's own per-tool deny rules. A file the CLI reads with its built-in
//     Read tool on another provider is not seen and not gated.
//   - HiddenSecrets: enforced on agent.CustomEnv, which is the only secret
//     channel this package owns. The daemon's own inherited environment is
//     narrowed elsewhere (pkg/agent buildEnv drops foreign model-vendor keys)
//     and a BYOK key is injected after this filter on purpose.
//   - AllowedCommands: told to the model, and nothing checks it. See
//     AllowsAnyCommand and PromptSection.
//
// AllowedCommands cannot be made a control from here, and the reason is worth
// writing down so nobody spends a week discovering it again. An allowlist has
// to be expressible as "only these", and the provider surfaces Multica drives
// are deny lists with prefix matching: they can refuse a named command, not
// admit a closed set. Chaining and wrapping defeat prefix matching anyway
// (`cd /tmp && rm x`, `sh -c 'rm x'`, `sudo rm x` all miss a rule written for
// `rm`), and Multica sees none of those strings, because the CLI builds and
// runs them itself. Enforce a real command boundary with a sandbox, or with
// blast radius rules on the effects the command produces — both of which
// Multica does see.
package permissionprofile

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/pkg/blastradius"
)

type Profile struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// ReadOnly forbids every write: files, commits, pushes.
	ReadOnly bool `json:"read_only"`
	// DeniedPaths are .gitignore-like globs the run may neither edit nor push.
	DeniedPaths []string `json:"denied_paths"`
	// AllowedCommands is what the model is told it may run; "*" means anything.
	AllowedCommands []string `json:"allowed_commands"`
	// HiddenSecrets are env key globs (case-insensitive) withheld from the run.
	HiddenSecrets []string `json:"hidden_secrets"`
	Builtin       bool     `json:"builtin,omitempty"`
}

// Defaults are seeded once per workspace, in this order.
func Defaults() []Profile {
	return []Profile{
		{Name: "read_only", Builtin: true, ReadOnly: true,
			Description:     "Reads and reports. Never writes a file, commits or pushes, and sees no workspace secret.",
			DeniedPaths:     []string{},
			AllowedCommands: []string{"git status", "git log", "git diff", "git show", "ls", "cat", "grep", "rg", "find"},
			HiddenSecrets:   []string{"*"}},
		{Name: "code", Builtin: true,
			Description:     "Edits code and opens pull requests. Env files, keys, CI workflows and deployment manifests are off limits; production secrets stay hidden.",
			DeniedPaths:     []string{".env", ".env.*", "**/*.pem", "**/*.key", ".github/workflows/**", "infra/**", "deploy/**"},
			AllowedCommands: []string{"*"},
			HiddenSecrets:   []string{"*PROD*", "*PRODUCTION*", "*DEPLOY*"}},
		{Name: "ci", Builtin: true,
			Description:     "Maintains CI: workflows and test tooling. No deployment manifests, no keys, no production secrets.",
			DeniedPaths:     []string{"infra/**", "deploy/**", "**/*.pem", "**/*.key"},
			AllowedCommands: []string{"*"},
			HiddenSecrets:   []string{"*PROD*", "*PRODUCTION*"}},
		{Name: "staging", Builtin: true,
			Description:     "Deploys to staging: infrastructure allowed, keys and production secrets excluded.",
			DeniedPaths:     []string{"**/*.pem", "**/*.key"},
			AllowedCommands: []string{"*"},
			HiddenSecrets:   []string{"*PROD*", "*PRODUCTION*"}},
		{Name: "production", Builtin: true,
			Description:     "Full access, production secrets included. Pair it with a dual-approval blast radius.",
			DeniedPaths:     []string{},
			AllowedCommands: []string{"*"},
			HiddenSecrets:   []string{}},
	}
}

// Validate refuses globs that would never compile, so a bad rule is caught
// when it is written rather than when a run silently ignores it.
func (p Profile) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	for _, g := range p.DeniedPaths {
		if _, err := blastradius.Compile(g); err != nil {
			return fmt.Errorf("denied path %q: %w", g, err)
		}
	}
	for _, g := range p.HiddenSecrets {
		if _, err := filepath.Match(g, "X"); err != nil {
			return fmt.Errorf("hidden secret %q: %w", g, err)
		}
	}
	return nil
}

// DeniesPath reports whether the run may not touch path.
func (p Profile) DeniesPath(path string) bool {
	path = strings.TrimPrefix(strings.TrimSpace(path), "./")
	for _, g := range p.DeniedPaths {
		if re, err := blastradius.Compile(g); err == nil && re.MatchString(path) {
			return true
		}
	}
	return false
}

// DeniedAmong returns the paths the profile refuses, in input order.
func (p Profile) DeniedAmong(paths []string) []string {
	var out []string
	for _, path := range paths {
		if p.DeniesPath(path) {
			out = append(out, path)
		}
	}
	return out
}

// HidesSecret reports whether an env key is withheld from the run.
func (p Profile) HidesSecret(key string) bool {
	upper := strings.ToUpper(key)
	for _, g := range p.HiddenSecrets {
		if ok, _ := filepath.Match(strings.ToUpper(g), upper); ok {
			return true
		}
	}
	return false
}

// HidesSecretNamed reports a key the profile withholds by a pattern that says
// something about it, as opposed to the catch-all "*".
//
// The distinction exists for one caller: the run's own model credential. A
// profile with HiddenSecrets ["*"] means "withhold the workspace's secrets",
// and the key that pays for the run is not one of those — withholding it would
// simply stop the run. A profile that writes ANTHROPIC_API_KEY, or *_API_KEY,
// is saying something about that variable, and a caller that puts it back is
// overriding an instruction rather than completing one.
func (p Profile) HidesSecretNamed(key string) bool {
	upper := strings.ToUpper(key)
	for _, g := range p.HiddenSecrets {
		if strings.TrimSpace(g) == "*" {
			continue
		}
		if ok, _ := filepath.Match(strings.ToUpper(g), upper); ok {
			return true
		}
	}
	return false
}

// FilterSecrets drops the hidden keys and names them, sorted, for the log.
func (p Profile) FilterSecrets(env map[string]string) (map[string]string, []string) {
	if len(p.HiddenSecrets) == 0 || len(env) == 0 {
		return env, nil
	}
	kept := make(map[string]string, len(env))
	var hidden []string
	for k, v := range env {
		if p.HidesSecret(k) {
			hidden = append(hidden, k)
			continue
		}
		kept[k] = v
	}
	sort.Strings(hidden)
	return kept, hidden
}

// AllowsAnyCommand is true for the "*" wildcard or an empty list.
func (p Profile) AllowsAnyCommand() bool {
	if len(p.AllowedCommands) == 0 {
		return true
	}
	for _, c := range p.AllowedCommands {
		if strings.TrimSpace(c) == "*" {
			return true
		}
	}
	return false
}

// PromptSection is the paragraph the model reads. It is the only
// enforcement of allowed commands (ponytail: advisory, providers expose no
// shell allowlist; tighten through blast radius rules when it matters).
func (p Profile) PromptSection() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Permission profile: %s\n", p.Name)
	if p.Description != "" {
		b.WriteString(p.Description + "\n")
	}
	if p.ReadOnly {
		b.WriteString("- This run is read-only: do not create, edit or delete files, do not commit and do not push. Report what you find in a comment.\n")
	}
	if len(p.DeniedPaths) > 0 {
		fmt.Fprintf(&b, "- Never read, edit or push these paths: %s.\n", strings.Join(p.DeniedPaths, ", "))
	}
	if !p.AllowsAnyCommand() {
		fmt.Fprintf(&b, "- Only run these commands: %s.\n", strings.Join(p.AllowedCommands, ", "))
	}
	if len(p.HiddenSecrets) > 0 {
		b.WriteString("- Some workspace secrets are withheld from this run on purpose; do not try to recover them.\n")
	}
	return b.String()
}

// ClaudeSettingsJSON is the `--settings` payload for Claude Code: deny
// rules hold even under bypassPermissions. Empty when nothing is denied.
func (p Profile) ClaudeSettingsJSON() string {
	var deny []string
	if p.ReadOnly {
		deny = append(deny, "Edit", "Write", "MultiEdit", "NotebookEdit", "Bash(git push:*)", "Bash(git commit:*)", "Bash(rm:*)", "Bash(mv:*)")
	}
	for _, g := range p.DeniedPaths {
		for _, tool := range []string{"Read", "Edit", "Write", "MultiEdit"} {
			deny = append(deny, tool+"("+g+")")
		}
	}
	if len(deny) == 0 {
		return ""
	}
	raw, _ := json.Marshal(map[string]any{"permissions": map[string]any{"deny": deny}})
	return string(raw)
}

// CodexArgs are the flags Codex honours; denied paths have no Codex
// equivalent and stay in the prompt.
func (p Profile) CodexArgs() []string {
	if p.ReadOnly {
		return []string{"--sandbox", "read-only"}
	}
	return nil
}

// ProviderArgs returns what to append to the agent's custom args.
func (p Profile) ProviderArgs(provider string) []string {
	switch provider {
	case "claude":
		if s := p.ClaudeSettingsJSON(); s != "" {
			return []string{"--settings", s}
		}
	case "codex":
		return p.CodexArgs()
	}
	return nil
}
