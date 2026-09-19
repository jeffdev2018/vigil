package service

import "encoding/json"

// MCP server (OS plan, chantier 1): Vigil exposed as one MCP server that
// external agents (Claude Code, Codex, Cursor, any MCP client) discover and
// call. The server, not the client, decides what each call may do: deny,
// ask, allow. These settings live under workspace.settings.mcp_server.

const (
	MCPSurfaceCompound = "compound"
	MCPSurfaceGranular = "granular"

	MCPDecisionAllow = "allow"
	MCPDecisionAsk   = "ask"
	MCPDecisionDeny  = "deny"
)

// MCPServerSettings is what a workspace admin can set.
type MCPServerSettings struct {
	// Enabled turns the endpoint on for the workspace. Off, every call is
	// refused with a clear message; nothing else changes.
	Enabled bool `json:"enabled"`
	// DefaultSurface is the tool surface a client gets when it asks for
	// none: compound (a dozen tools with an action argument, cheap in
	// context) or granular (one tool per operation).
	DefaultSurface string `json:"default_surface"`
	// Tools overrides the decision per granular tool name: allow, ask or
	// deny. An override can only tighten what the caller's own ceiling
	// grants — it never lets an observer agent act alone.
	Tools map[string]string `json:"tools"`
}

func defaultMCPServerSettings() MCPServerSettings {
	return MCPServerSettings{Enabled: true, DefaultSurface: MCPSurfaceCompound, Tools: map[string]string{}}
}

// MCPServerSettingsFrom reads the settings off a workspace settings blob.
// Missing or unparseable falls back to the defaults; an unknown surface or
// decision is dropped rather than trusted.
func MCPServerSettingsFrom(settings []byte) MCPServerSettings {
	out := defaultMCPServerSettings()
	var s struct {
		MCPServer *struct {
			Enabled        *bool             `json:"enabled"`
			DefaultSurface string            `json:"default_surface"`
			Tools          map[string]string `json:"tools"`
		} `json:"mcp_server"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.MCPServer == nil {
		return out
	}
	if s.MCPServer.Enabled != nil {
		out.Enabled = *s.MCPServer.Enabled
	}
	if s.MCPServer.DefaultSurface == MCPSurfaceGranular {
		out.DefaultSurface = MCPSurfaceGranular
	}
	for tool, decision := range s.MCPServer.Tools {
		if ValidMCPDecision(decision) {
			out.Tools[tool] = decision
		}
	}
	return out
}

// ValidMCPDecision reports whether a per-tool override is in the vocabulary.
func ValidMCPDecision(d string) bool {
	return d == MCPDecisionAllow || d == MCPDecisionAsk || d == MCPDecisionDeny
}

// ValidMCPServerSettings reports whether a submitted setting can be stored.
func ValidMCPServerSettings(s MCPServerSettings) bool {
	if s.DefaultSurface != MCPSurfaceCompound && s.DefaultSurface != MCPSurfaceGranular {
		return false
	}
	for _, d := range s.Tools {
		if !ValidMCPDecision(d) {
			return false
		}
	}
	return true
}
