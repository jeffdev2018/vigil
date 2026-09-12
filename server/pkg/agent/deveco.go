package agent

// This file implements the DevEco Code backend as a fully self-contained,
// independent agent. DevEco Code (the `deveco` CLI, Huawei's HarmonyOS
// coding-agent CLI at https://gitcode.com/openharmony-sig/deveco-code) is a
// separate product maintained by a different company. It is intentionally kept
// decoupled from the OpenCode backend: it owns its own backend struct, event
// types, blocked-args table, and process lifecycle, so a change to either agent
// can never affect the other. The two share only the package-wide generic
// process helpers (configureProcessGroup, filterCustomArgs, trySend, …) that
// every backend in this package reuses.
//
// DevEco speaks the same `run --format json` protocol and emits the same NDJSON
// event stream as upstream OpenCode (it is built on the OpenCode engine), so the event schema
// below mirrors that shape. Two deliberate differences from the OpenCode
// integration:
//
//  1. No `--prompt` flag — DevEco's `run` subcommand does not expose it
//     (verified via `deveco run --help`). System context therefore reaches the
//     CLI through the per-task AGENTS.md the daemon writes, the same file-based
//     channel the daemon relies on for OpenCode in production.
//
//  2. No inline MCP injection yet — DevEco reads MCP from DEVECO_CONFIG_CONTENT
//     (its DEVECO_-prefixed mirror of OPENCODE_CONFIG_CONTENT), but plumbing
//     agent.mcp_config through it is deferred to a follow-up so this backend
//     stays self-contained. The UI hides the MCP tab for deveco in the meantime
//     (see packages/core/agents/mcp-support.ts).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// devecoTerminateGraceNanos optionally overrides, in nanoseconds, how long a
// cancelled deveco process is given to exit after SIGTERM before it (and its
// whole process group) is SIGKILLed. Zero means use the default. It is atomic
// so tests can shorten the grace without racing the cancellation goroutine that
// reads it.
var devecoTerminateGraceNanos atomic.Int64

func devecoTerminateGrace() time.Duration {
	if n := devecoTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 5 * time.Second
}

// devecoBlockedArgs are flags hardcoded by the daemon that must not be
// overridden by user-configured custom_args. DevEco's `run` subcommand exposes
// the same daemon-managed flags as OpenCode's.
var devecoBlockedArgs = map[string]blockedArgMode{
	"--format":                       blockedWithValue,  // json output format for daemon communication
	"--dir":                          blockedWithValue,  // task workdir anchor for skill / AGENTS.md discovery
	"--variant":                      blockedWithValue,  // owned by agent.thinking_level
	"--dangerously-skip-permissions": blockedStandalone, // daemon manages non-interactive permission prompts
}

// devecoBackend implements Backend by spawning `deveco run --format json` and
// reading streaming JSON events from stdout.
type devecoBackend struct {
	cfg Config
}

func (b *devecoBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = "deveco"
	}
	resolved, err := exec.LookPath(execPath)
	if err != nil {
		return nil, fmt.Errorf("deveco executable not found at %q: %w", execPath, err)
	}
	if runtime.GOOS == "windows" {
		if native := resolveDevecoNativeFromShim(resolved, os.Stat); native != "" {
			b.cfg.Logger.Info("deveco resolved to native binary to avoid .cmd shim argv truncation", "shim", resolved, "native", native)
			resolved = native
		}
	}
	execPath = resolved

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := []string{"run", "--format", "json", "--dangerously-skip-permissions"}
	// Anchor DevEco's project discovery (AGENTS.md walk-up + .deveco/skills/
	// project config scan) at the task workdir, mirroring the OpenCode anchor:
	// without --dir, DevEco falls back to PWD (inherited from the daemon) or
	// process.cwd(), which in self-host deployments can resolve to the user's
	// shell working directory and silently bypass the per-task workdir. PWD is
	// also overridden below for the same reason.
	if opts.Cwd != "" {
		args = append(args, "--dir", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--variant", opts.ThinkingLevel)
	}
	// DevEco's `run` subcommand has no --prompt flag, so SystemPrompt is
	// intentionally not forwarded; system context is delivered via the
	// per-task AGENTS.md the daemon writes for deveco.
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("deveco does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, devecoBlockedArgs, b.cfg.Logger)...)
	// The task prompt is delivered on stdin, never argv, exactly as the OpenCode
	// engine DevEco is built on expects: `run` merges its variadic positional
	// with whatever is piped in, so passing no positional makes the piped text
	// the whole message. On argv a prompt carrying the workspace context clears
	// the Windows CreateProcess cap (32,767 characters) even through the native
	// binary, and the process never starts (#6538).

	cmd := b.cfg.commandAt(execPath).exec(runCtx, args...)
	hideAgentWindow(cmd)
	// Take over context cancellation: drive a graceful, group-wide
	// SIGTERM→SIGKILL from the cancellation goroutine below and close the
	// stdout read end only after the tree has been signalled. Returning nil
	// here keeps os/exec from racing us with its own kill; WaitDelay is the
	// hard backstop.
	cmd.Cancel = func() error { return nil }
	b.cfg.logAgentCommandWithPrompt(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "run")), len(prompt))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg)
	// Override PWD so the child DevEco process resolves its discovery root to
	// the task workdir. cmd.Dir alone is not enough: DevEco reads PWD
	// (inherited from the parent daemon) before falling back to process.cwd()
	// when computing the directory it walks for AGENTS.md / .deveco/skills.
	if opts.Cwd != "" {
		env = append(env, "PWD="+opts.Cwd)
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("deveco stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("deveco stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[deveco:stderr] ")

	if err := startOwnedProcessTree(cmd, b.cfg.Logger); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start deveco: %w", err)
	}

	b.cfg.Logger.Info("deveco started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	// procDone closes once cmd.Wait() returns, letting the cancellation handler
	// skip a process that already exited and avoid signalling a dead pid.
	procDone := make(chan struct{})

	// Write the prompt from its own goroutine so a prompt larger than the pipe
	// buffer cannot deadlock against the stdout reader below. Closing stdin is
	// what ends the prompt (the engine reads it to EOF), so close on every path.
	writeErrCh := make(chan error, 1)
	go func() {
		_, err := io.WriteString(stdin, prompt)
		closeStdin()
		writeErrCh <- err
	}()

	// On cancellation / timeout, terminate deveco (and the tool subprocesses it
	// spawned) BEFORE unblocking the scanner. Closing the stdout read end
	// immediately would leave deveco writing into a closed pipe (EPIPE), which
	// can spin the orphaned process at 100% CPU. Instead SIGTERM the whole
	// process group, give it a grace period to exit cleanly, then SIGKILL it.
	// Only then is it safe to close the stdout read end as a last-resort
	// unblock for a scanner that a wedged descendant still keeps open.
	go func() {
		select {
		case <-procDone:
			return // finished on its own; nothing to terminate
		case <-runCtx.Done():
		}
		// Release a prompt write still blocked on a full stdin pipe.
		closeStdin()
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGTERM)
			select {
			case <-procDone: // exited within the grace window
			case <-time.After(devecoTerminateGrace()):
				signalProcessGroup(cmd, syscall.SIGKILL)
			}
		}
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processEvents(stdout, msgCh)

		exitErr := cmd.Wait()
		close(procDone)
		releaseProcessGroup(cmd)
		duration := time.Since(startTime)

		// Wait closed the process pipes, so the prompt writer has returned.
		writeErr := <-writeErrCh

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("deveco timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("deveco exited with error: %v", exitErr)
		} else if exitErr != nil && scanResult.noTerminalSignal {
			// Keep the terminal-signal diagnosis and add the exit detail.
			scanResult.errMsg = fmt.Sprintf("%s; deveco exited with error: %v", scanResult.errMsg, exitErr)
		} else if writeErr != nil && !scanResult.sawTerminalSignal {
			// A failed prompt write is benign only once the run is proven to
			// have finished (the engine reads stdin to EOF before any work).
			if scanResult.errMsg == "" {
				scanResult.errMsg = fmt.Sprintf("deveco prompt write failed: %v", writeErr)
			} else {
				scanResult.errMsg = fmt.Sprintf("%s; deveco prompt write failed: %v", scanResult.errMsg, writeErr)
			}
			scanResult.status = "failed"
		}

		b.cfg.Logger.Info("deveco finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		// Build usage map. DevEco doesn't report model per-step, so attribute
		// all usage to the configured model (or "unknown").
		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

// resolveDevecoNativeFromShim returns the path to the native DevEco executable
// bundled inside the npm package, given the path to the npm `deveco.cmd` shim
// that PATH lookup found on Windows. Returns "" if the shim doesn't end in
// `.cmd` or no candidate native binary is present, in which case the caller
// keeps the original shim path.
//
// This mirrors the OpenCode fix (resolveOpenCodeNativeFromShim) — DevEco is
// npm-distributed the same way, so it inherits the same defect: Windows batch
// argument forwarding via `%*` drops everything after the first newline, so a
// multi-line positional prompt (daemon system context + user message) is
// truncated before the shim hands off to the JS entrypoint. Spawning the native
// binary skips the cmd.exe layer entirely. The two resolvers are kept separate
// (not shared) so DevEco's independent backend can't be broken by an OpenCode
// change and vice versa; the divergence in package layout below is exactly why.
//
// DevEco's npm layout differs from OpenCode's (verified against the real
// `@deveco/deveco-code` 0.1.2 tarball):
//
//   - The package is scoped (`@deveco/deveco-code`), so it lives under
//     `node_modules\@deveco\deveco-code\...` rather than a flat name.
//   - The native binary is named `deveco.exe`, and each platform sub-package
//     (`@deveco/deveco-code-windows-x64{,-baseline}`) exposes it at `bin\deveco.exe`.
//   - `postinstall.mjs` copies the CPU-variant-selected binary into the main
//     package's own `bin\deveco.exe`, so that copy is the most reliable target
//     (it already reflects the AVX2/baseline decision) and is tried first.
//   - There is no `windows-arm64` package (DevEco ships Windows x64 only), so
//     the candidate list omits it.
//
// statFn is injected so this is testable on non-Windows hosts.
func resolveDevecoNativeFromShim(shimPath string, statFn func(string) (os.FileInfo, error)) string {
	if !strings.EqualFold(filepath.Ext(shimPath), ".cmd") {
		return ""
	}
	prefix := filepath.Dir(shimPath)
	scope := filepath.Join(prefix, "node_modules", "@deveco")
	candidates := []string{
		// postinstall copies the selected native binary here; most reliable.
		filepath.Join(scope, "deveco-code", "bin", "deveco.exe"),
		// Fall back to the platform sub-packages directly if the copy is absent.
		filepath.Join(scope, "deveco-code-windows-x64", "bin", "deveco.exe"),
		filepath.Join(scope, "deveco-code-windows-x64-baseline", "bin", "deveco.exe"),
	}
	for _, candidate := range candidates {
		if _, err := statFn(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// ── Event handlers ──

// devecoEventResult holds the accumulated state from processing the event stream.
type devecoEventResult struct {
	status           string
	errMsg           string
	output           string
	sessionID        string
	usage            TokenUsage // accumulated token usage across all steps
	noTerminalSignal bool       // guard fired: the stream ended without evidence the run finished
	// sawTerminalSignal is positive evidence the run finished: a step_finish
	// closed the last step with no continuation pending and something to show
	// for it. Not the negation of noTerminalSignal.
	sawTerminalSignal bool
}

// processEvents reads JSON lines from r, dispatches events to ch, and returns
// the accumulated result. This is the core scanner loop, extracted for testability.
func (b *devecoBackend) processEvents(r io.Reader, ch chan<- Message) devecoEventResult {
	var output strings.Builder
	var sessionID string
	var usage TokenUsage
	var unparsedOutput strings.Builder
	parsedEvents := 0
	finalStatus := "completed"
	var finalError string

	// Terminal-signal guard, ported from the CodeArts backend (same OpenCode
	// protocol family; kept separate so the two backends stay decoupled). The
	// stream has no terminal result event, so a run is only proven finished
	// when its last step closed, required no continuation, and produced
	// something (text, a tool call, or reported usage). See codearts.go's
	// processEvents for the full rationale of each signal.
	openStep := false
	stepHasContinuationTool := false
	awaitingContinuation := false
	sawStepFinish := false
	stepProducedOutput := false
	lastStepVoid := false

	scanner := newAgentStreamScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event devecoEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			if unparsedOutput.Len() < 4096 {
				if unparsedOutput.Len() > 0 {
					unparsedOutput.WriteByte('\n')
				}
				if remaining := 4096 - unparsedOutput.Len(); len(line) > remaining {
					line = line[:remaining]
				}
				unparsedOutput.WriteString(line)
			}
			continue
		}
		parsedEvents++

		if event.SessionID != "" {
			sessionID = event.SessionID
		}

		switch event.Type {
		case "text":
			b.handleTextEvent(event, ch, &output)
			if event.Part.Text != "" {
				stepProducedOutput = true
			}
		case "tool_use":
			b.handleToolUseEvent(event, ch)
			stepProducedOutput = true
			if event.Part.Metadata == nil || !event.Part.Metadata.ProviderExecuted {
				stepHasContinuationTool = true
			}
		case "error":
			b.handleErrorEvent(event, ch, &finalStatus, &finalError)
		case "step_start":
			openStep = true
			stepHasContinuationTool = false
			awaitingContinuation = false
			stepProducedOutput = false
			trySend(ch, Message{Type: MessageStatus, Status: "running"})
		case "step_finish":
			openStep = false
			sawStepFinish = true
			awaitingContinuation = event.Part.Reason == "tool-calls" ||
				(event.Part.Reason != "" && stepHasContinuationTool)
			stepHasContinuationTool = false
			// Accumulate token usage from step_finish events.
			if t := event.Part.Tokens; t != nil {
				usage.InputTokens += t.Input
				usage.OutputTokens += t.Output
				if t.Cache != nil {
					usage.CacheReadTokens += t.Cache.Read
					usage.CacheWriteTokens += t.Cache.Write
				}
			}
			if devecoStepReportedUsage(&event.Part) {
				stepProducedOutput = true
			}
			lastStepVoid = !stepProducedOutput
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		b.cfg.Logger.Warn("deveco stdout scanner error", "error", scanErr)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	noTerminalSignal := false
	if finalStatus == "completed" && parsedEvents == 0 {
		finalStatus = "failed"
		finalError = "deveco returned no parseable JSON events"
		if detail := sanitizeCLIOutput(unparsedOutput.String()); detail != "" {
			finalError += ": " + detail
		}
		noTerminalSignal = true
	}
	if finalStatus == "completed" {
		switch {
		case openStep:
			finalStatus = "failed"
			finalError = "deveco stream ended without a terminal signal (step still open at EOF)"
			noTerminalSignal = true
		case awaitingContinuation:
			finalStatus = "failed"
			finalError = "deveco stream ended without a terminal signal (last step required a continuation that never started)"
			noTerminalSignal = true
		case lastStepVoid:
			finalStatus = "failed"
			finalError = "deveco stream ended on an empty step (no text, no tool call, no reported usage) — the provider produced nothing"
			noTerminalSignal = true
		}
	}

	return devecoEventResult{
		status:            finalStatus,
		errMsg:            finalError,
		output:            output.String(),
		sessionID:         sessionID,
		usage:             usage,
		noTerminalSignal:  noTerminalSignal,
		sawTerminalSignal: sawStepFinish && !noTerminalSignal,
	}
}

// devecoStepReportedUsage reports whether a step_finish part carries any
// evidence that the provider round-trip happened: cost, or any token counter
// (reasoning and total included, read as evidence only, never billed).
func devecoStepReportedUsage(part *devecoEventPart) bool {
	if part.Cost > 0 {
		return true
	}
	t := part.Tokens
	if t == nil {
		return false
	}
	if t.Input > 0 || t.Output > 0 || t.Reasoning > 0 || t.Total > 0 {
		return true
	}
	return t.Cache != nil && (t.Cache.Read > 0 || t.Cache.Write > 0)
}

func (b *devecoBackend) handleTextEvent(event devecoEvent, ch chan<- Message, output *strings.Builder) {
	text := event.Part.Text
	if text != "" {
		output.WriteString(text)
		trySend(ch, Message{Type: MessageText, Content: text})
	}
}

// handleToolUseEvent processes "tool_use" events. A single tool_use event
// contains both the call and result in part.state when the tool reaches a
// terminal state (state.status is "completed" or "error").
func (b *devecoBackend) handleToolUseEvent(event devecoEvent, ch chan<- Message) {
	var input map[string]any
	if event.Part.State != nil && event.Part.State.Input != nil {
		_ = json.Unmarshal(event.Part.State.Input, &input)
	}

	trySend(ch, Message{
		Type:   MessageToolUse,
		Tool:   event.Part.Tool,
		CallID: event.Part.CallID,
		Input:  input,
	})

	// Pair every terminal tool-use with a tool-result. The daemon uses this
	// pair to track in-flight tools, so dropping error results would leave its
	// counter permanently elevated and suppress the normal idle watchdog.
	state := event.Part.State
	if state != nil && (state.Status == "completed" || state.Status == "error") {
		outputStr := extractDevecoToolOutput(state.Output)
		if state.Status == "error" && state.Error != "" {
			outputStr = state.Error
		}
		trySend(ch, Message{
			Type:   MessageToolResult,
			Tool:   event.Part.Tool,
			CallID: event.Part.CallID,
			Output: outputStr,
		})
	}
}

// handleErrorEvent processes "error" events. DevEco can exit with RC=0 even on
// errors (e.g. invalid model), so error events are the reliable failure signal.
func (b *devecoBackend) handleErrorEvent(event devecoEvent, ch chan<- Message, finalStatus, finalError *string) {
	errMsg := ""
	if event.Error != nil {
		errMsg = event.Error.Message()
	}
	if errMsg == "" {
		errMsg = "unknown deveco error"
	}

	b.cfg.Logger.Warn("deveco error event", "error", errMsg)
	trySend(ch, Message{Type: MessageError, Content: errMsg})

	*finalStatus = "failed"
	*finalError = errMsg
}

// extractDevecoToolOutput converts the tool state output (which may be a string
// or a structured object) into a string.
func extractDevecoToolOutput(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	data, _ := json.Marshal(output)
	return string(data)
}

// ── JSON types for `deveco run --format json` stdout events ──
//
// DevEco emits the same NDJSON event schema as upstream OpenCode. Event types
// observed in real output:
//
//	"step_start"  — agent step begins
//	"text"        — text output from agent (part.text)
//	"tool_use"    — tool invocation with call and result (part.tool, part.callID, part.state)
//	"error"       — error from deveco (error.name, error.data.message)
//	"step_finish" — agent step completes (includes token usage)
type devecoEvent struct {
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp,omitempty"`
	SessionID string          `json:"sessionID,omitempty"`
	Part      devecoEventPart `json:"part"`
	Error     *devecoError    `json:"error,omitempty"`
}

// devecoEventPart represents the part field in a deveco event.
type devecoEventPart struct {
	ID        string `json:"id,omitempty"`
	MessageID string `json:"messageID,omitempty"`
	SessionID string `json:"sessionID,omitempty"`
	Type      string `json:"type,omitempty"`

	// Text events
	Text string `json:"text,omitempty"`

	// Tool use events
	Tool   string           `json:"tool,omitempty"`
	CallID string           `json:"callID,omitempty"`
	State  *devecoToolState `json:"state,omitempty"`
	// Provider-executed tools need no continuation step.
	Metadata *devecoPartMetadata `json:"metadata,omitempty"`

	// step_finish token usage
	Tokens *devecoTokens `json:"tokens,omitempty"`
	// step_finish cost, read only as round-trip evidence.
	Cost float64 `json:"cost,omitempty"`
	// step_finish reason ("stop", "tool-calls", …); absent on older versions.
	Reason string `json:"reason,omitempty"`
}

type devecoPartMetadata struct {
	ProviderExecuted bool `json:"providerExecuted,omitempty"`
}

// devecoTokens represents token usage in a step_finish event.
type devecoTokens struct {
	Input     int64              `json:"input"`
	Output    int64              `json:"output"`
	Reasoning int64              `json:"reasoning,omitempty"`
	Total     int64              `json:"total,omitempty"`
	Cache     *devecoCacheTokens `json:"cache,omitempty"`
}

type devecoCacheTokens struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// devecoToolState represents the state of a tool invocation.
type devecoToolState struct {
	Status string          `json:"status,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output any             `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// devecoError represents an error event from deveco.
type devecoError struct {
	Name string         `json:"name,omitempty"`
	Data *devecoErrData `json:"data,omitempty"`
}

// Message returns the human-readable error message.
func (e *devecoError) Message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type devecoErrData struct {
	Message string `json:"message,omitempty"`
}
