package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/mcpgov"
)

// orgNonNegotiableDeny is merged into every unit at save and was, until the
// claim started applying it, consumed by exactly one thing: a line of the
// agent's brief. The failure mode this pins is drift — a verb added to or
// renamed in the product's list with no matcher behind it would go back to
// being a sentence, silently, and nothing else in the build would notice.
func TestEveryNonNegotiableDenyIsEnforceable(t *testing.T) {
	// One tool per verb that the verb must refuse. A verb with no entry here
	// is a verb nobody decided the meaning of.
	probes := map[string]string{
		"delete":                         "delete_repository",
		"bill":                           "create_invoice",
		"send_external_without_approval": "send_email",
		"touch_secrets":                  "read_api_key",
		"commit_money":                   "stripe_payment",
	}
	if len(probes) != len(orgNonNegotiableDeny) {
		t.Fatalf("%d verbs in the product's list, %d probes: add the probe with the verb", len(orgNonNegotiableDeny), len(probes))
	}
	for _, verb := range orgNonNegotiableDeny {
		tool, ok := probes[verb]
		if !ok {
			t.Errorf("verb %q has no probe: nobody stated what it refuses", verb)
			continue
		}
		class := mcpgov.OrgDenyClass(verb, tool, "")
		if class == "" {
			t.Errorf("verb %q refuses nothing: it is back to being a sentence in the brief", verb)
			continue
		}
		if class == mcpgov.ClassActAlone {
			t.Errorf("verb %q resolves to act_alone, which is not a denial", verb)
		}
	}
}

// And the claim's own composition: a unit's deny reaches the gateway payload
// the daemon enforces, rather than only the paragraph the model reads.
func TestOrgDenyReachesTheClaimGateway(t *testing.T) {
	gw := &mcpgov.Gateway{TrustMode: "autonomous", Servers: []mcpgov.GatewayServer{{
		Name:  "billing",
		Tools: []mcpgov.GatewayTool{{Name: "issue_refund", Risk: mcpgov.RiskExternal, Class: mcpgov.ClassActAlone}},
	}}}
	if tightened := mcpgov.ApplyOrgDeny(gw, orgNonNegotiableDeny, nil); len(tightened) != 1 {
		t.Fatalf("tightened = %v, want the refund tool", tightened)
	}
	if got := gw.Servers[0].Tools[0].Class; got != mcpgov.ClassNever {
		t.Fatalf("issue_refund = %q, want never under a list that names commit_money", got)
	}
}
