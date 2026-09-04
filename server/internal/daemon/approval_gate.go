package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Approval gates (K05), daemon side. Three interception points feed the
// same server flow (a Decision Card the run waits on):
//   - git pre-push: a hook directory the daemon owns, selected per run
//     through GIT_CONFIG_* so the repository's own hooks stay untouched;
//   - sensitive MCP tools: the remote MCP broker asks before forwarding;
//   - spend: the run requests a short-lived token from the server.
// The run never decides for itself whether to wait: the server does.

// DefaultGateSensitiveTools mirrors the server default; the server setting
// wins when it answers the gate, this only decides what is asked.
const DefaultGateSensitiveTools = `(?i)merge|delete|remove|drop|destroy|pay|charge|transfer|refund|purchase`

const gateDefaultTimeout = 30 * time.Minute

// prePushHookScript blocks a push to the watched remote until a human
// answers the gate. Any other remote (a personal fork) passes untouched.
const prePushHookScript = `#!/bin/sh
# Multica approval gate (K05): pause a push until a human approves it.
remote="$1"; url="$2"
[ -n "$MULTICA_TASK_ID" ] && [ -n "$MULTICA_TOKEN" ] && [ -n "$MULTICA_SERVER_URL" ] || exit 0
if [ -n "$MULTICA_GATE_REMOTE" ] && [ "$url" != "$MULTICA_GATE_REMOTE" ] && [ "$remote" != "$MULTICA_GATE_REMOTE" ]; then
  exit 0
fi
refs=""
while read -r local_ref local_sha remote_ref remote_sha; do
  [ -n "$remote_ref" ] && refs="$refs $remote_ref"
done
refs=$(printf '%s' "$refs" | sed 's/^ //')
timeout="${MULTICA_GATE_TIMEOUT:-1800}"
api="${MULTICA_SERVER_URL%/}/api/tasks/$MULTICA_TASK_ID"
auth="Authorization: Bearer $MULTICA_TOKEN"
ws="X-Workspace-ID: $MULTICA_WORKSPACE_ID"
body=$(printf '{"gate_type":"git_push","summary":"git push %s %s","details":{"remote":"%s","url":"%s","refs":"%s"}}' "$remote" "$refs" "$remote" "$url" "$refs")
created=$(curl -sS -f -X POST "$api/gates" -H "$auth" -H "$ws" -H 'Content-Type: application/json' -d "$body" 2>/dev/null) || { echo "multica: could not open the approval gate; push refused" >&2; exit 1; }
gate=$(printf '%s' "$created" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
[ -n "$gate" ] || { echo "multica: approval gate answer unreadable; push refused" >&2; exit 1; }
echo "multica: push to $remote $refs is waiting for a human approval (gate $gate)" >&2
start=$(date +%s)
while :; do
  now=$(date +%s)
  [ $((now - start)) -lt "$timeout" ] || { echo "multica: approval gate timed out; push refused" >&2; exit 1; }
  res=$(curl -sS -f "$api/gates/$gate?wait=25" -H "$auth" -H "$ws" 2>/dev/null) || { sleep 5; continue; }
  status=$(printf '%s' "$res" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')
  case "$status" in
    approved) echo "multica: push approved" >&2; exit 0 ;;
    denied)   echo "multica: push denied by a human; the run must not retry it" >&2; exit 1 ;;
    expired)  echo "multica: approval gate expired; push refused" >&2; exit 1 ;;
  esac
done
`

// ensureGateHooksDir writes the daemon's hook directory and returns it.
func ensureGateHooksDir(configRoot string) (string, error) {
	dir := filepath.Join(configRoot, "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "pre-push")
	if err := os.WriteFile(path, []byte(prePushHookScript), 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// gitRemoteURL is the origin of the run's checkout, empty when unknown.
func gitRemoteURL(dir string) string {
	if dir == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gateEnvironment selects the daemon hooks for every git call of the run
// and names the watched remote.
func gateEnvironment(hooksDir, remoteURL string, timeout time.Duration) map[string]string {
	return map[string]string{
		"GIT_CONFIG_COUNT":     "1",
		"GIT_CONFIG_KEY_0":     "core.hooksPath",
		"GIT_CONFIG_VALUE_0":   hooksDir,
		"MULTICA_GATE_REMOTE":  remoteURL,
		"MULTICA_GATE_TIMEOUT": strconv.Itoa(int(timeout / time.Second)),
	}
}

// approvalGateClient asks the server on behalf of one run.
type approvalGateClient struct {
	serverURL string
	token     string
	taskID    string
	http      *http.Client
}

func newApprovalGateClient(serverURL, token, taskID string) *approvalGateClient {
	return &approvalGateClient{serverURL: strings.TrimRight(serverURL, "/"), token: token, taskID: taskID, http: &http.Client{Timeout: 40 * time.Second}}
}

func (c *approvalGateClient) do(ctx context.Context, method, path string, body any) (map[string]any, int, error) {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.serverURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out, res.StatusCode, nil
}

// Ask opens a gate and waits for its outcome: approved, denied or expired.
func (c *approvalGateClient) Ask(ctx context.Context, gateType, summary string, details map[string]any, timeout time.Duration) (string, error) {
	created, code, err := c.do(ctx, http.MethodPost, "/api/tasks/"+c.taskID+"/gates", map[string]any{"gate_type": gateType, "summary": summary, "details": details})
	if err != nil {
		return "", err
	}
	if code != http.StatusCreated {
		return "", fmt.Errorf("open gate: server answered %d", code)
	}
	gateID, _ := created["id"].(string)
	if gateID == "" {
		return "", errors.New("open gate: no id in the answer")
	}
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return "expired", nil
		}
		got, code, err := c.do(ctx, http.MethodGet, "/api/tasks/"+c.taskID+"/gates/"+gateID+"?wait=25", nil)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			time.Sleep(5 * time.Second)
			continue
		}
		if code != http.StatusOK {
			return "", fmt.Errorf("poll gate: server answered %d", code)
		}
		if status, _ := got["status"].(string); status != "" && status != "pending" {
			return status, nil
		}
	}
}

// sensitiveToolMatcher compiles the pattern, falling back to the default.
func sensitiveToolMatcher(pattern string) *regexp.Regexp {
	if pattern == "" {
		pattern = DefaultGateSensitiveTools
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return regexp.MustCompile(DefaultGateSensitiveTools)
	}
	return re
}
