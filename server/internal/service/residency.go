package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Data residency routing (K46).
//
// A workspace under a residency or sovereignty obligation cannot let its work
// land on whichever machine happens to be online. The policy declares where
// runs may execute — allowed regions, banned providers, on-prem only — and
// every routing decision filters its candidates through it before scoring.
//
// The declaration side is deliberately unverified: nothing proves a machine
// really sits in eu-west-1, and the settings UI says so. What the policy
// guarantees is that a run is never dispatched to a runtime the workspace has
// not declared compliant — which is the part software can actually enforce.
//
// It fails CLOSED. A runtime with no declaration is non-compliant the moment
// the policy is restrictive, so turning the policy on never silently keeps
// dispatching to machines nobody has vouched for.

// Compliance rejection reasons, in the vocabulary the routing trace and the
// audit entry record.
const (
	ResidencyReasonBannedProvider   = "banned_provider"
	ResidencyReasonNotOnPrem        = "not_on_prem"
	ResidencyReasonRegionNotAllowed = "region_not_allowed"
)

// Bounds on the policy lists. Twenty regions or providers is far past any real
// declaration; the cap exists so a settings PUT cannot store an unbounded blob.
const dataResidencyMaxListLen = 20

// DataResidencyPolicy lives under workspace.settings.data_residency_policy.
// Every field is empty by default, and an empty policy constrains nothing.
type DataResidencyPolicy struct {
	// RegionAllowlist names the regions a run may execute in. Empty means any
	// region. Values are lowercase, trimmed and deduplicated.
	RegionAllowlist []string `json:"region_allowlist"`
	// BannedProviders names runtime providers that may never take work,
	// whatever they declare. Lowercase, trimmed, deduplicated.
	BannedProviders []string `json:"banned_providers"`
	// RequireOnPrem restricts execution to runtimes declared on-prem. A cloud
	// runtime can never satisfy it.
	RequireOnPrem bool `json:"require_on_prem"`
}

// DataResidencyPolicyFromSettings reads the policy off a workspace settings
// blob. Anything missing or unparseable is the empty policy — no constraint —
// which is also what a workspace that never configured this gets.
func DataResidencyPolicyFromSettings(settings []byte) DataResidencyPolicy {
	out := DataResidencyPolicy{RegionAllowlist: []string{}, BannedProviders: []string{}}
	var s struct {
		Policy *DataResidencyPolicy `json:"data_residency_policy"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.Policy == nil {
		return out
	}
	return NormalizeDataResidencyPolicy(*s.Policy)
}

// NormalizeDataResidencyPolicy trims, lowercases, deduplicates and sorts both
// lists. Storing the normalized form is what makes the comparison in
// RuntimeCompliant a plain string match rather than a per-check fold.
func NormalizeDataResidencyPolicy(p DataResidencyPolicy) DataResidencyPolicy {
	return DataResidencyPolicy{
		RegionAllowlist: normalizeTokenList(p.RegionAllowlist),
		BannedProviders: normalizeTokenList(p.BannedProviders),
		RequireOnPrem:   p.RequireOnPrem,
	}
}

func normalizeTokenList(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		token := strings.ToLower(strings.TrimSpace(v))
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	sort.Strings(out)
	return out
}

// ValidDataResidencyPolicy reports whether a submitted policy is storable. The
// tokens are free-form on purpose — a region name is whatever the operator's
// cloud calls it, and a provider may be a custom runtime profile — so the only
// rules are "non-empty after trimming" (enforced by normalization dropping
// blanks) and the length cap.
func ValidDataResidencyPolicy(p DataResidencyPolicy) bool {
	return len(p.RegionAllowlist) <= dataResidencyMaxListLen &&
		len(p.BannedProviders) <= dataResidencyMaxListLen
}

// Restrictive reports whether the policy constrains anything at all. A
// non-restrictive policy skips every lookup on the enqueue path, so a
// workspace that never configured residency pays nothing for the feature.
func (p DataResidencyPolicy) Restrictive() bool {
	return len(p.RegionAllowlist) > 0 || len(p.BannedProviders) > 0 || p.RequireOnPrem
}

// DataResidencyPolicyRange is what the settings endpoint tells a client about
// the accepted values, so the form does not carry its own copy.
func DataResidencyPolicyRange() int { return dataResidencyMaxListLen }

// RuntimeCompliant answers "may this runtime take work under this policy?".
// profile is the runtime's declaration, nil when it has none.
//
// The order of the checks is the order of severity: a banned provider is a
// decision about the vendor and holds whatever the machine declares; on-prem
// is a property of the machine; the region is the finest grain. A runtime with
// no declaration fails every restrictive check — the fail-closed rule.
func RuntimeCompliant(policy DataResidencyPolicy, runtime db.AgentRuntime, profile *db.RuntimeComplianceProfile) (bool, string) {
	if !policy.Restrictive() {
		return true, ""
	}
	provider := strings.ToLower(strings.TrimSpace(runtime.Provider))
	for _, banned := range policy.BannedProviders {
		if provider == banned {
			return false, ResidencyReasonBannedProvider
		}
	}
	if policy.RequireOnPrem {
		// A cloud runtime is by definition not on the workspace's own
		// hardware, so its declaration cannot make it on-prem.
		if runtime.RuntimeMode == "cloud" || profile == nil || !profile.OnPrem {
			return false, ResidencyReasonNotOnPrem
		}
	}
	if len(policy.RegionAllowlist) > 0 {
		if profile == nil {
			return false, ResidencyReasonRegionNotAllowed
		}
		region := strings.ToLower(strings.TrimSpace(profile.Region))
		allowed := false
		for _, r := range policy.RegionAllowlist {
			if region == r {
				allowed = true
				break
			}
		}
		if !allowed {
			return false, ResidencyReasonRegionNotAllowed
		}
	}
	return true, ""
}

// runtimeComplianceFilter answers the same question for one enqueue, with the
// policy and every declaration in the workspace already loaded. A nil filter
// means "no constraint" — every caller treats it that way, so the compliant
// path costs one settings read and nothing else.
type runtimeComplianceFilter func(rt db.AgentRuntime) (bool, string)

// compliantRuntimeFilter builds the per-enqueue gate: the workspace's policy
// read once, and the declarations of its runtimes in one query. Returns nil
// when the workspace declares no policy, which is the common case.
//
// Fails OPEN on a query error: a residency policy that cannot be READ must not
// stop every run in the workspace. It fails CLOSED on the data it does read —
// an undeclared runtime is refused — which is the distinction that matters.
func (s *TaskService) compliantRuntimeFilter(ctx context.Context, wsID pgtype.UUID) runtimeComplianceFilter {
	if !wsID.Valid {
		return nil
	}
	ws, err := s.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		slog.Warn("data residency: workspace load failed; policy not applied", "workspace_id", util.UUIDToString(wsID), "error", err)
		return nil
	}
	policy := DataResidencyPolicyFromSettings(ws.Settings)
	if !policy.Restrictive() {
		return nil
	}
	rows, err := s.Queries.ListWorkspaceRuntimeComplianceProfiles(ctx, wsID)
	if err != nil {
		slog.Warn("data residency: profile load failed; policy not applied", "workspace_id", util.UUIDToString(wsID), "error", err)
		return nil
	}
	profiles := make(map[[16]byte]*db.RuntimeComplianceProfile, len(rows))
	for i := range rows {
		profiles[rows[i].RuntimeID.Bytes] = &rows[i]
	}
	return func(rt db.AgentRuntime) (bool, string) {
		return RuntimeCompliant(policy, rt, profiles[rt.ID.Bytes])
	}
}

// residencyAllows is the nil-safe call every routing path makes.
func residencyAllows(filter runtimeComplianceFilter, rt db.AgentRuntime) (bool, string) {
	if filter == nil {
		return true, ""
	}
	return filter(rt)
}

// RoutingProblemResidencyNoCompliantRuntime is the fatal problem raised when
// the workspace's residency policy leaves the agent nowhere to run. Unlike an
// offline machine it does not resolve on its own: either the policy changes or
// a compliant runtime is declared.
const RoutingProblemResidencyNoCompliantRuntime = "residency_policy_no_compliant_runtime"

// residencyProblem reports whether the residency policy blocks every runtime
// this agent's work could reach.
//
// The bound runtime is the default dispatch target, so a compliant one ends
// the question. Otherwise the run can still be steered: the agent's pool (K28)
// is checked first, and an auto-routed agent (JEF-237) may additionally land on
// any claim-eligible runtime in the workspace. Only when none of those holds a
// compliant runtime is the trigger refused.
func (s *TaskService) residencyProblem(ctx context.Context, agent db.Agent, rt db.AgentRuntime, filter runtimeComplianceFilter) *RoutingProblem {
	if filter == nil {
		return nil
	}
	ok, boundReason := filter(rt)
	if ok {
		return nil
	}
	rejected := []map[string]string{{"runtime_id": util.UUIDToString(rt.ID), "name": rt.Name, "reason": boundReason}}

	if target, _ := s.poolFailoverTarget(ctx, agent, db.AgentTaskQueue{RuntimeID: agent.RuntimeID}, "data_residency", filter); target.OK {
		return nil
	}
	if agent.RuntimeRouting == RoutingModeAuto {
		candidates, err := s.Queries.ListRoutingCandidateRuntimes(ctx, db.ListRoutingCandidateRuntimesParams{
			WorkspaceID:      agent.WorkspaceID,
			OwnerID:          agent.OwnerID,
			RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
		})
		if err != nil {
			// Cannot prove there is no alternative: fail open on the lookup,
			// the per-candidate filter still refuses non-compliant targets.
			slog.Warn("data residency: candidate lookup failed", "agent_id", util.UUIDToString(agent.ID), "error", err)
			return nil
		}
		for _, candidate := range candidates {
			if candidate.ID == rt.ID {
				continue
			}
			ok, reason := filter(candidate)
			if ok {
				return nil
			}
			rejected = append(rejected, map[string]string{"runtime_id": util.UUIDToString(candidate.ID), "name": candidate.Name, "reason": reason})
		}
	}
	return &RoutingProblem{
		Code:    RoutingProblemResidencyNoCompliantRuntime,
		Message: residencyBlockedMessage(agent, rejected),
		Fatal:   true,
		Details: map[string]any{
			"policy":   s.residencyPolicyForDetails(ctx, agent.WorkspaceID),
			"rejected": rejected,
		},
	}
}

// residencyPolicyForDetails re-reads the policy for the audit record. It runs
// only on the refusal path, so the extra read costs nothing in the common case
// and keeps the filter closure free of a captured copy.
func (s *TaskService) residencyPolicyForDetails(ctx context.Context, wsID pgtype.UUID) DataResidencyPolicy {
	ws, err := s.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return DataResidencyPolicy{RegionAllowlist: []string{}, BannedProviders: []string{}}
	}
	return DataResidencyPolicyFromSettings(ws.Settings)
}

func residencyBlockedMessage(agent db.Agent, rejected []map[string]string) string {
	parts := make([]string, 0, len(rejected))
	for _, r := range rejected {
		name := r["name"]
		if name == "" {
			name = r["runtime_id"]
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", name, r["reason"]))
	}
	return fmt.Sprintf(
		"%s has nowhere compliant to run: this workspace's data residency policy rejects %s.",
		agentLabel(agent), strings.Join(parts, ", "))
}

// RuntimeAllowedForClaim is the defensive re-check on the claim path. The
// enqueue-time filter is the single control point — a task is only ever queued
// on a compliant runtime — but a policy tightened after the enqueue would
// otherwise still dispatch everything already in the queue.
//
// It costs one workspace read on a claim that is already doing a dozen, and
// stops there for the overwhelmingly common case of no policy at all. Only a
// restrictive policy pays for the runtime's declaration.
func (s *TaskService) RuntimeAllowedForClaim(ctx context.Context, wsID pgtype.UUID, rt db.AgentRuntime) (bool, string) {
	if !wsID.Valid {
		return true, ""
	}
	ws, err := s.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		// Fails OPEN, like the enqueue-time filter: a policy that cannot be
		// read must not stop every claim in the workspace.
		return true, ""
	}
	policy := DataResidencyPolicyFromSettings(ws.Settings)
	if !policy.Restrictive() {
		return true, ""
	}
	var profile *db.RuntimeComplianceProfile
	if row, err := s.Queries.GetRuntimeComplianceProfile(ctx, rt.ID); err == nil {
		profile = &row
	}
	return RuntimeCompliant(policy, rt, profile)
}
