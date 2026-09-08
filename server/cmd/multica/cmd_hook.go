package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/pkg/permissionprofile"
)

// hookCmd holds the callbacks a third-party CLI invokes mid-run. They are not
// for people: the daemon registers them in the CLI's own configuration and the
// CLI calls them with an event on stdin.
var hookCmd = &cobra.Command{
	Use:    "hook",
	Short:  "Callbacks a runtime CLI invokes during a run (internal)",
	Hidden: true,
}

// Environment the daemon sets for the hook. Passing the list rather than
// calling back to the server is deliberate: a PreToolUse hook that times out
// does NOT block the tool call, so a network round-trip on this path would be
// a fail-open on every slow moment.
const (
	envHookAllowedCommands = "MULTICA_HOOK_ALLOWED_COMMANDS"
	envHookProfileName     = "MULTICA_HOOK_PROFILE"
	envHookObservedMarker  = "MULTICA_HOOK_OBSERVED_FILE"
)

// hookExitBlock is the exit code that blocks a tool call. It is the only code
// that blocks through the code alone, so a policy hook has to use it: exiting
// 1 leaves the call running and only prints an error.
const hookExitBlock = 2

type preToolUseEvent struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

var hookPreToolUseCmd = &cobra.Command{
	Use:          "pre-tool-use",
	Short:        "Decide whether a runtime CLI may run the command it proposed (internal)",
	Hidden:       true,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		raw, err := io.ReadAll(cmd.InOrStdin())
		// Every path records that the hook ran, a read failure included.
		// Whether it runs at all is the one thing the daemon cannot see from
		// outside, and a control nobody can confirm fired is not one you may
		// report as enforcing.
		markHookObserved()
		if err != nil {
			raw = nil
		}
		block, reason := preToolUseDecision(raw, splitHookList(os.Getenv(envHookAllowedCommands)), os.Getenv(envHookProfileName))
		if !block {
			return nil
		}
		fmt.Fprintln(cmd.ErrOrStderr(), reason)
		// The only exit code that blocks a tool call through the code alone.
		// Exiting 1 would leave the command running and merely print an error,
		// which is the failure mode this hook exists to remove.
		os.Exit(hookExitBlock)
		return nil
	},
}

// preToolUseDecision is the whole policy, kept out of the cobra plumbing so it
// is testable without a process. Every unreadable input blocks: a hook that
// cannot tell what the CLI is about to run must not let it run.
func preToolUseDecision(raw []byte, allow []string, profileName string) (block bool, reason string) {
	if len(allow) == 0 {
		return false, "" // nothing declared: this run is not command-restricted
	}
	var event preToolUseEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return true, "the hook could not read the tool call, so it refused it"
	}
	if !strings.EqualFold(event.ToolName, "Bash") {
		return false, ""
	}
	profile := permissionprofile.Profile{Name: profileName, AllowedCommands: allow}
	ok, refused := profile.AllowsCommand(event.ToolInput.Command)
	if ok {
		return false, ""
	}
	return true, fmt.Sprintf("%q is not in this run's allowed commands (%s). Allowed: %s.",
		refused, hookProfileLabel(profileName), strings.Join(allow, ", "))
}

func hookProfileLabel(name string) string {
	if strings.TrimSpace(name) == "" {
		return "permission profile"
	}
	return "permission profile " + name
}

// markHookObserved writes the marker the daemon reads to tell "this run had no
// shell command" from "the hook never fired". A CLI whose hooks were disabled
// looks exactly like a quiet run from outside, and the difference is the whole
// value of the control.
func markHookObserved() {
	path := strings.TrimSpace(os.Getenv(envHookObservedMarker))
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_ = f.Close()
}

func splitHookList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, "\n") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func init() {
	hookCmd.AddCommand(hookPreToolUseCmd)
}
