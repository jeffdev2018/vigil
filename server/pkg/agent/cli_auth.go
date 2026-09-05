package agent

// CLI sign-in, per provider. One table, read by the daemon (which runs the
// commands) and by the API (which decides whether to offer the buttons at
// all), so the two can never disagree about what a provider supports.
//
// An entry exists ONLY where the provider's own documentation describes a
// command that works without a TTY. That rules out most of the ~26 CLIs the
// daemon can detect:
//
//   - `opencode auth login` and `opencode auth logout` prompt for a provider
//     to pick, so under the daemon (no terminal) they would hang until the
//     ten-minute timeout rather than sign anyone in.
//   - gemini, qwen, aider, goose and the rest either authenticate from an
//     interactive session only, or take an API key from the environment and
//     have no sign-in flow at all.
//
// Those providers keep the documentation link the runtime page already shows
// (packages/views/runtimes/components/runtime-docs.ts carries the per-provider
// URL); they are not offered a button that cannot work.

// CLIAuthCommands are the arguments to pass the provider's own executable.
// A nil field means "this provider documents no such command".
type CLIAuthCommands struct {
	Login  []string
	Logout []string
	// Status must be non-interactive AND documented to exit 0 when signed in,
	// non-zero when not: the exit code is the whole answer, and the output is
	// discarded because it carries the account identity.
	//
	// This is a much higher bar than "prints whether you are signed in".
	// `cursor-agent status` prints it but documents no exit code, so trusting
	// it would report every Cursor runtime as authenticated. Absent is the
	// honest answer there — the state stays unknown.
	Status []string
}

var cliAuthCommands = map[string]CLIAuthCommands{
	// https://code.claude.com/docs/en/cli-reference
	"claude": {
		Login:  []string{"auth", "login"},
		Logout: []string{"auth", "logout"},
		Status: []string{"auth", "status"},
	},
	// https://developers.openai.com/codex/local-config
	"codex": {
		Login:  []string{"login", "--device-auth"},
		Logout: []string{"logout"},
		Status: []string{"login", "status"},
	},
	// https://cursor.com/docs/cli/reference/authentication
	// `status` exists but documents no exit code; see CLIAuthCommands.Status.
	"cursor-agent": {
		Login:  []string{"login"},
		Logout: []string{"logout"},
	},
	// https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/authenticate-copilot-cli
	// `copilot login` is an OAuth device flow, which is exactly what the
	// daemon can drive. Signing out and checking the account are in-session
	// slash commands (/logout, /user), not shell subcommands, so neither is
	// offered here. `gh auth status` belongs to a different binary.
	"copilot": {
		Login: []string{"login"},
	},
}

// CLIAuth returns the sign-in commands for a provider.
func CLIAuth(provider string) (CLIAuthCommands, bool) {
	cmds, ok := cliAuthCommands[provider]
	return cmds, ok
}

// CLIAuthAction returns the arguments for "login" or "logout". Absent means
// the provider documents no such command, and the caller must not run the
// executable with no arguments — that would start the agent CLI itself.
func CLIAuthAction(provider, action string) ([]string, bool) {
	cmds, ok := cliAuthCommands[provider]
	if !ok {
		return nil, false
	}
	var args []string
	switch action {
	case "login":
		args = cmds.Login
	case "logout":
		args = cmds.Logout
	}
	return args, len(args) > 0
}

// CLIAuthStatus returns the non-interactive status probe, when the provider
// documents one with an exit-code contract.
func CLIAuthStatus(provider string) ([]string, bool) {
	cmds, ok := cliAuthCommands[provider]
	return cmds.Status, ok && len(cmds.Status) > 0
}

// CLIAuthSupported reports whether Multica can drive any sign-in action for
// this provider. The API gates its endpoints on this, so a provider that only
// has a status probe is still "not supported" — there is nothing to press.
func CLIAuthSupported(provider string) bool {
	cmds, ok := cliAuthCommands[provider]
	return ok && (len(cmds.Login) > 0 || len(cmds.Logout) > 0)
}

// CLIAuthProviders lists every provider with at least one sign-in action, for
// documentation and tests. Order is not guaranteed.
func CLIAuthProviders() []string {
	out := make([]string, 0, len(cliAuthCommands))
	for provider := range cliAuthCommands {
		if CLIAuthSupported(provider) {
			out = append(out, provider)
		}
	}
	return out
}
