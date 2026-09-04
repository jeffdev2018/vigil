package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const cliAuthProcessTimeout = 10 * time.Minute

var (
	cliAuthURLPattern  = regexp.MustCompile(`https?://[^\s<>"']+`)
	cliAuthCodePattern = regexp.MustCompile(`(?i)\b[A-Z0-9]{4}(?:-[A-Z0-9]{4})+\b`)
)

func (d *Daemon) resolveRuntimeCommand(ctx context.Context, rt Runtime) (string, []string, error) {
	if customSpec, isCustom := d.customProfileLaunchForRuntime(rt.ID); isCustom {
		return customSpec.path, agent.FilterLaunchPrefix(rt.Provider, customSpec.fixedArgs, d.logger), nil
	}
	entry, ok := d.agents()[rt.Provider]
	if !ok {
		return "", nil, fmt.Errorf("no agent configured for provider %q", rt.Provider)
	}
	entry, _ = d.resolveAgentEntry(ctx, rt.Provider, entry)
	if entry.Path == "" {
		return "", nil, fmt.Errorf("no executable found for provider %q", rt.Provider)
	}
	return entry.Path, nil, nil
}

func cliAuthCommand(provider, action string) ([]string, error) {
	switch provider + ":" + action {
	case "codex:login":
		return []string{"login", "--device-auth"}, nil
	case "codex:logout":
		return []string{"logout"}, nil
	case "claude:login":
		return []string{"auth", "login"}, nil
	case "claude:logout":
		return []string{"auth", "logout"}, nil
	default:
		return nil, fmt.Errorf("CLI authentication is not supported for provider %q", provider)
	}
}

type cliAuthOutputWriter struct {
	mu       sync.Mutex
	buffer   string
	url      string
	code     string
	onUpdate func(string, string)
}

func (w *cliAuthOutputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buffer += string(p)
	if len(w.buffer) > 64<<10 {
		w.buffer = w.buffer[len(w.buffer)-(64<<10):]
	}
	oldURL, oldCode := w.url, w.code
	if matches := cliAuthURLPattern.FindAllString(w.buffer, -1); len(matches) > 0 {
		w.url = strings.TrimRight(matches[len(matches)-1], ".,;:)]}")
	}
	if codes := cliAuthCodePattern.FindAllString(w.buffer, -1); len(codes) > 0 {
		w.code = strings.ToUpper(codes[len(codes)-1])
	}
	url, code := w.url, w.code
	changed := (url != oldURL || code != oldCode) && (url != "" || code != "")
	w.mu.Unlock()
	if changed && w.onUpdate != nil {
		w.onUpdate(url, code)
	}
	return len(p), nil
}

func (d *Daemon) handleCliAuth(ctx context.Context, rt Runtime, pending PendingCliAuth) {
	d.logger.Info("CLI authentication requested", "runtime_id", rt.ID, "request_id", pending.ID, "provider", rt.Provider, "action", pending.Action)

	execPath, fixedArgs, err := d.resolveRuntimeCommand(ctx, rt)
	if err != nil {
		d.reportCliAuthFailure(ctx, rt, pending.ID, err)
		return
	}
	actionArgs, err := cliAuthCommand(rt.Provider, pending.Action)
	if err != nil {
		d.reportCliAuthFailure(ctx, rt, pending.ID, err)
		return
	}

	authCtx, cancel := context.WithTimeout(ctx, cliAuthProcessTimeout)
	defer cancel()
	writer := &cliAuthOutputWriter{onUpdate: func(verificationURL, userCode string) {
		d.reportCliAuthResult(authCtx, rt, pending.ID, map[string]any{
			"status":           "running",
			"verification_url": verificationURL,
			"user_code":        userCode,
		})
	}}
	cmd := exec.CommandContext(authCtx, execPath, append(fixedArgs, actionArgs...)...)
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := processtree.Run(authCtx, cmd, time.Second); err != nil {
		if authCtx.Err() != nil {
			err = fmt.Errorf("authentication request expired before the CLI completed it")
		}
		d.reportCliAuthFailure(context.WithoutCancel(ctx), rt, pending.ID, err)
		return
	}

	authenticated := pending.Action == "login"
	d.reportCliAuthResult(context.WithoutCancel(ctx), rt, pending.ID, map[string]any{
		"status":        "completed",
		"authenticated": authenticated,
	})
}

func (d *Daemon) reportCliAuthFailure(ctx context.Context, rt Runtime, requestID string, err error) {
	message := "CLI authentication failed"
	if err != nil {
		message = redact.Text(err.Error())
	}
	d.reportCliAuthResult(ctx, rt, requestID, map[string]any{"status": "failed", "error": message})
}
