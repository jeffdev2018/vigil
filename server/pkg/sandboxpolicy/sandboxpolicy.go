// Package sandboxpolicy holds the declarative sandbox policies of JEF-256 and
// the rule that merges them: workspace default (workspace.settings) < project
// policy (project_sandbox_policy) < issue override (issue.metadata), with the
// MOST-RESTRICTIVE layer winning every field. The server resolves the chain at
// claim time and folds the result into the SandboxSpec the claim already
// carries; this package is the pure, database-free half of that resolution.
package sandboxpolicy

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// Network modes, ordered by restrictiveness: unrestricted < allowlist < none.
const (
	NetworkUnrestricted = "unrestricted"
	NetworkAllowlist    = "allowlist"
	NetworkNone         = "none"
)

// MaxHosts mirrors the per-runtime sandbox host cap (K10), so a policy cannot
// store a list a runtime would refuse.
const MaxHosts = 50

// Policy is the frozen JEF-256 wire shape, shared by every layer.
type Policy struct {
	NetworkMode         string   `json:"network_mode"`
	AllowedHosts        []string `json:"allowed_hosts"`
	BlockSensitiveFiles bool     `json:"block_sensitive_files"`
}

// Default is what "no layer set anything" resolves to.
func Default() Policy {
	return Policy{NetworkMode: NetworkUnrestricted, AllowedHosts: []string{}}
}

// networkRank orders the modes so Merge can take the max.
func networkRank(mode string) int {
	switch mode {
	case NetworkAllowlist:
		return 1
	case NetworkNone:
		return 2
	default:
		return 0
	}
}

// hostRe is the same host validation the runtime sandbox settings use.
var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// ValidateHost reports whether one allowlist entry is a host name, applying
// the runtime rule byte for byte.
func ValidateHost(host string) bool {
	return hostRe.MatchString(host)
}

// Normalize lowercases, trims, drops blanks and dedupes the host list, sorted,
// so the stored and merged forms compare by membership rather than by spelling.
func Normalize(p Policy) Policy {
	return Policy{
		NetworkMode:         strings.TrimSpace(p.NetworkMode),
		AllowedHosts:        normalizeHosts(p.AllowedHosts),
		BlockSensitiveFiles: p.BlockSensitiveFiles,
	}
}

func normalizeHosts(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		host := strings.ToLower(strings.TrimSpace(raw))
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	sort.Strings(out)
	return out
}

// IsDefault reports whether the layer constrains anything beyond Default().
func (p Policy) IsDefault() bool {
	n := Normalize(p)
	return n.NetworkMode == NetworkUnrestricted && len(n.AllowedHosts) == 0 && !n.BlockSensitiveFiles
}

// Merge folds the layers into one policy, most-restrictive wins:
// network_mode takes the strictest mode any layer declares,
// block_sensitive_files is the OR, and allowed_hosts is the intersection of
// the hosts of every layer whose mode is allowlist — carried only when the
// merged mode is itself allowlist (under none there is no egress to allow;
// under unrestricted the list is meaningless). Nil layers contribute nothing.
func Merge(layers ...*Policy) Policy {
	merged := Default()
	allowlistHosts := map[string]bool{}
	allowlistSeen := false
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		p := Normalize(*layer)
		if networkRank(p.NetworkMode) > networkRank(merged.NetworkMode) {
			merged.NetworkMode = p.NetworkMode
		}
		merged.BlockSensitiveFiles = merged.BlockSensitiveFiles || p.BlockSensitiveFiles
		if p.NetworkMode == NetworkAllowlist {
			if !allowlistSeen {
				allowlistSeen = true
				for _, h := range p.AllowedHosts {
					allowlistHosts[h] = true
				}
			} else {
				for h := range allowlistHosts {
					if !contains(p.AllowedHosts, h) {
						delete(allowlistHosts, h)
					}
				}
			}
		}
	}
	if merged.NetworkMode == NetworkAllowlist && allowlistSeen {
		merged.AllowedHosts = make([]string, 0, len(allowlistHosts))
		for h := range allowlistHosts {
			merged.AllowedHosts = append(merged.AllowedHosts, h)
		}
		sort.Strings(merged.AllowedHosts)
	}
	return merged
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// IntersectHosts is the claim-time fold of the runtime's own allowlist with
// the merged policy's: both must permit a host for the run to reach it.
func IntersectHosts(a, b []string) []string {
	allowed := make(map[string]bool, len(b))
	for _, h := range normalizeHosts(b) {
		allowed[h] = true
	}
	out := make([]string, 0, len(a))
	for _, h := range normalizeHosts(a) {
		if allowed[h] {
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}

// FromSettings reads workspace.settings->'sandbox_policy'; nil when the key is
// absent or unparseable, which is what a workspace that never configured this
// layer gets.
func FromSettings(settings []byte) *Policy {
	var s struct {
		Policy *Policy `json:"sandbox_policy"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil {
		return nil
	}
	return s.Policy
}

// FromMetadata reads issue.metadata->'sandbox_policy' — the issue override.
// Same absence rules as FromSettings.
func FromMetadata(metadata []byte) *Policy {
	return FromSettings(metadata)
}
