package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// The `agent` trigger: a plugin hook offered to an agent as an MCP tool.
//
// This is the fourth call site, and the one that is NOT the host deciding to
// call something. An agent sees a tool, reads its description, and chooses. That
// choice is the whole reason hooks may be reached from an agent at all: the
// alternative — a hook that must run before or after every turn — is a third
// party holding the product's main loop open, which is why no such position
// exists anywhere in this design.
//
// The daemon renders these as tools but does NOT call the plugin. A tool call
// goes back to the server, which performs the signed request. The signing secret
// is derived from the deployment key and never leaves this process; equally
// important, routing through the server means the rate limit, the circuit
// breaker, the `net:` destination check and the invocation record all apply
// exactly as they do for every other trigger, rather than being reimplemented
// daemon-side where they would drift.

// PluginHookTool is one hook, described the way an agent will see it.
type PluginHookTool struct {
	InstallationID string `json:"installation_id"`
	HookKey        string `json:"hook_key"`
	// Name is what the agent sees, namespaced so two plugins contributing the
	// same hook key do not collide.
	Name string `json:"name"`
	// Description is the manifest's hook description verbatim: it is what the
	// agent reads to decide whether to call this, so the plugin author writes
	// it and the host does not paraphrase.
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// toolNameUnsafe matches everything MCP tool names should not contain. Agents
// and providers vary in what they accept; letters, digits and underscores are
// the intersection that works everywhere.
var toolNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

// PluginToolName namespaces a hook so two plugins can both contribute a hook
// called "summarize" without one shadowing the other.
//
// The naive version of this — sanitize both halves and join with an underscore
// — is not injective, and a test caught it: a plugin key uses `.` and `-`, both
// of which have to become `_`, so `a.b` and `a-b` collapse together, and
// `a.b_` + `c` collides with `a.b` + `c`. Two different plugins would then be
// offering the agent one tool name, and whichever was registered last would
// answer for both.
//
// So the prefix carries a short digest of the FULL plugin key rather than a
// lossy transliteration of it. The readable part is the last segment, which is
// what a person recognises; the digest is what makes it unique.
//
// The `__` separator is safe because a hook key cannot contain one: its pattern
// requires an alphanumeric after every underscore, so a doubled underscore is
// unrepresentable.
func PluginToolName(pluginKey, hookKey string) string {
	clean := func(value string) string {
		return strings.Trim(toolNameUnsafe.ReplaceAllString(value, "_"), "_")
	}
	segments := strings.Split(pluginKey, ".")
	readable := clean(segments[len(segments)-1])
	if readable == "" {
		readable = "plugin"
	}
	digest := sha256.Sum256([]byte(pluginKey))
	return readable + "_" + hex.EncodeToString(digest[:])[:6] + "__" + clean(hookKey)
}

// agentPluginToolBinding is the (installation, hook) pair a binding names,
// used as a set key so lookup is O(1) per candidate hook/tool instead of a
// query per candidate.
type agentPluginToolBinding struct {
	installationID string
	hookKey        string
}

// boundAgentPluginTools loads agentID's bindings as a set. Deny by default:
// an installed, enabled plugin's tools are not offered to an agent until
// someone binds them, the same opt-in shape as the agent's own MCP connection
// settings. A read failure fails closed (returns the error) rather than
// silently offering nothing — that would look identical to "no tools bound"
// and hide a real outage behind a wrong-looking but unremarkable result.
func (s *PluginService) boundAgentPluginTools(ctx context.Context, agentID pgtype.UUID) (map[agentPluginToolBinding]bool, error) {
	bindings, err := s.Queries.ListAgentPluginTools(ctx, agentID)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "list agent plugin tool bindings", Err: err}
	}
	bound := make(map[agentPluginToolBinding]bool, len(bindings))
	for _, binding := range bindings {
		bound[agentPluginToolBinding{installationID: uuidString(binding.InstallationID), hookKey: binding.HookKey}] = true
	}
	return bound, nil
}

// AgentPluginToolBound reports whether agentID is bound to installationID's
// hookKey. The handler for the agent-triggered invoke route calls this
// directly, before InvokeAgentHook, so an unbound call gets an explicit 403
// rather than the 200-with-error-body InvokeAgentHook's own failures use
// (those are hook execution outcomes an agent should read as a tool error;
// this is an authorization boundary, and looks identical to one on the wire
// only if nothing distinguishes them).
func (s *PluginService) AgentPluginToolBound(ctx context.Context, agentID pgtype.UUID, installationID, hookKey string) (bool, error) {
	bound, err := s.boundAgentPluginTools(ctx, agentID)
	if err != nil {
		return false, err
	}
	return bound[agentPluginToolBinding{installationID: installationID, hookKey: hookKey}], nil
}

// AgentHookTools lists the agent-tool hooks agentID may call.
//
// Disabled installations are skipped, which is what makes disabling a plugin
// take effect on the next task rather than only in the UI. An uninstalled one
// has no row at all. A hook that is offered by an enabled installation but was
// never bound to this agent is skipped too — deny by default; installing a
// plugin grants nothing to any agent until an admin explicitly binds it.
func (s *PluginService) AgentHookTools(ctx context.Context, workspaceID, agentID pgtype.UUID) ([]PluginHookTool, error) {
	installations, err := s.Queries.ListWorkspacePluginInstallations(ctx, workspaceID)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "list plugin installations", Err: err}
	}
	bound, err := s.boundAgentPluginTools(ctx, agentID)
	if err != nil {
		return nil, err
	}

	tools := make([]PluginHookTool, 0)
	for _, installation := range installations {
		if !installation.Enabled {
			continue
		}
		manifest, err := ParseInstallationManifest(installation)
		if err != nil {
			// One unreadable manifest must not hide every other plugin's tools.
			continue
		}
		for _, hook := range manifest.Contributes.Hooks {
			if !HookIsAgentTool(hook) {
				continue
			}
			if !bound[agentPluginToolBinding{installationID: uuidString(installation.ID), hookKey: hook.Key}] {
				continue
			}
			tools = append(tools, PluginHookTool{
				InstallationID: uuidString(installation.ID),
				HookKey:        hook.Key,
				Name:           PluginToolName(manifest.Key, hook.Key),
				Description:    hook.Description,
				InputSchema:    hook.InputSchema,
			})
		}
	}
	// Stable order so a task's tool list does not reshuffle between claims for
	// no reason, which shows up as cache churn in provider-side prompt caching.
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// AvailableAgentPluginTool is one hook a workspace admin could bind to an
// agent, with whether that agent already is.
type AvailableAgentPluginTool struct {
	InstallationID string `json:"installation_id"`
	PluginKey      string `json:"plugin_key"`
	HookKey        string `json:"hook_key"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Transport      string `json:"transport"`
	Bound          bool   `json:"bound"`
}

// AvailableAgentPluginTools lists every agent-trigger hook an enabled
// installation in the workspace declares, across both transports (unlike
// AgentHookTools, which only lists what is both bound AND directly callable
// as an http tool). This is the admin's picker, not the agent's tool list:
// admins bind a hook here before AgentHookTools or AgentMCPConnections can
// ever surface it to the agent, so this has to show mcp-transport hooks too
// even though those are wired through the mcp connection builder rather than
// through a tool call this route would make.
func (s *PluginService) AvailableAgentPluginTools(ctx context.Context, workspaceID, agentID pgtype.UUID) ([]AvailableAgentPluginTool, error) {
	installations, err := s.Queries.ListWorkspacePluginInstallations(ctx, workspaceID)
	if err != nil {
		return nil, &PluginError{Kind: PluginErrorUnavailable, Message: "list plugin installations", Err: err}
	}
	bound, err := s.boundAgentPluginTools(ctx, agentID)
	if err != nil {
		return nil, err
	}

	tools := make([]AvailableAgentPluginTool, 0)
	for _, installation := range installations {
		if !installation.Enabled {
			continue
		}
		manifest, err := ParseInstallationManifest(installation)
		if err != nil {
			continue
		}
		for _, hook := range manifest.Contributes.Hooks {
			if !HookAllowsTrigger(hook, plugincontract.TriggerAgent) {
				continue
			}
			tools = append(tools, AvailableAgentPluginTool{
				InstallationID: uuidString(installation.ID),
				PluginKey:      manifest.Key,
				HookKey:        hook.Key,
				Name:           PluginToolName(manifest.Key, hook.Key),
				Description:    hook.Description,
				Transport:      hook.Transport.Type,
				Bound:          bound[agentPluginToolBinding{installationID: uuidString(installation.ID), hookKey: hook.Key}],
			})
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// InvokeAgentHook runs one agent-triggered hook.
//
// The actor is the AGENT, not the person who filed the issue and not the
// plugin: an agent chose to call this, so the writes it produces are the
// agent's, exactly as they would be if the agent had written them directly.
// author_type already has a value for that, which is why this trigger needs no
// new one.
func (s *PluginService) InvokeAgentHook(ctx context.Context, installationID, hookKey string, agentID pgtype.UUID, input json.RawMessage) (HookResult, error) {
	caller, err := s.AuthorizePluginAction(ctx, installationID, pgtype.UUID{}, "")
	if err != nil {
		return HookResult{}, err
	}
	hook, err := FindHook(caller.Installation, hookKey)
	if err != nil {
		return HookResult{}, err
	}
	// The deny-by-default binding check lives in the handler
	// (InvokeAgentPluginHook), not here: it needs to answer with a hard 403,
	// distinct from every failure below which the daemon deliberately turns
	// into a 200 tool-error body for the agent to read. Keeping one check in
	// one place also means InvokeAgentHook's agentID stays what HookActor
	// always meant it as — whoever the resulting writes are attributed to —
	// without this function also being the enforcement point.
	return s.InvokeHook(ctx, HookInvocation{
		Installation: caller.Installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerAgent,
		Actor:        HookActor{Type: "agent", ID: agentID},
		Input:        rawInputOrNil(input),
	}, 1)
}

func rawInputOrNil(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// HookIsAgentTool reports whether a hook can be offered to the agent as a
// tool: it declares the agent trigger AND InvokeHook can actually run it.
// InvokeHook refuses every transport but HTTP, and the manifest validator does
// not forbid an agent trigger on an MCP hook, so a tool the agent can see must
// be filtered on the same rule — or every call fails with "not supported yet".
func HookIsAgentTool(hook plugincontract.Hook) bool {
	return HookAllowsTrigger(hook, plugincontract.TriggerAgent) && hook.Transport.Type == plugincontract.TransportHTTP
}
