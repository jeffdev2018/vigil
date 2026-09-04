package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
MULTICA_HOOK_STDIN=$(cat)
while read -r local_ref local_sha remote_ref remote_sha; do
  [ -n "$remote_ref" ] && refs="$refs $remote_ref"
done <<EOF_REFS
$(printf '%s\n' "$MULTICA_HOOK_STDIN")
EOF_REFS
refs=$(printf '%s' "$refs" | sed 's/^ //')
# Paths the push touches (K07 blast radius): the pushed commits' files.
paths=""
while read -r local_ref local_sha remote_ref remote_sha; do
  [ -n "$local_sha" ] || continue
  case "$remote_sha" in
    0000000000000000000000000000000000000000|"") files=$(git diff-tree --no-commit-id --name-only -r "$local_sha" 2>/dev/null) ;;
    *) files=$(git diff --name-only "$remote_sha" "$local_sha" 2>/dev/null) ;;
  esac
  for f in $files; do paths="$paths\"$f\","; done
done <<EOF_REFS
$(printf '%s\n' "$MULTICA_HOOK_STDIN")
EOF_REFS
paths="[${paths%,}]"
timeout="${MULTICA_GATE_TIMEOUT:-1800}"
api="${MULTICA_SERVER_URL%/}/api/tasks/$MULTICA_TASK_ID"
auth="Authorization: Bearer $MULTICA_TOKEN"
ws="X-Workspace-ID: $MULTICA_WORKSPACE_ID"
body=$(printf '{"gate_type":"git_push","summary":"git push %s %s","details":{"remote":"%s","url":"%s","refs":"%s","paths":%s}}' "$remote" "$refs" "$remote" "$url" "$refs" "$paths")
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

// gateParamPaths lifts path-like arguments out of a tool call so the
// server can apply the project's blast radius (K07).
func gateParamPaths(params json.RawMessage) []string {
	var in struct {
		Arguments map[string]any `json:"arguments"`
	}
	if json.Unmarshal(params, &in) != nil || in.Arguments == nil {
		return []string{}
	}
	out := []string{}
	for _, key := range []string{"path", "file_path", "filePath", "target", "paths", "files"} {
		switch v := in.Arguments[key].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				out = append(out, v)
			}
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// applyPermissionProfile (K06) withholds the profile's hidden secrets and
// appends the flags the provider can enforce (Claude deny rules, Codex
// read-only sandbox). It mutates the agent payload once so every later
// reader — env layering, launch args, the MCP gate — sees the same run.
func applyPermissionProfile(agent *AgentData, provider string, log *slog.Logger) {
	if agent == nil || agent.PermissionProfile == nil {
		return
	}
	p := agent.PermissionProfile
	var hidden []string
	agent.CustomEnv, hidden = p.FilterSecrets(agent.CustomEnv)
	if len(hidden) > 0 && log != nil {
		log.Info("permission profile: secrets withheld from this run", "profile", p.Name, "keys", hidden)
	}
	if extra := p.ProviderArgs(provider); len(extra) > 0 {
		agent.CustomArgs = append(append([]string{}, agent.CustomArgs...), extra...)
	}
}
