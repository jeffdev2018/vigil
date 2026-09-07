package mcpgov

import "testing"

// The five non-negotiable denies every org unit inherits had one runtime
// consumer: a sentence in the agent's brief. A sentence is not a control, so
// each verb is matched against the tool catalogue here.
func TestOrgDenyClass(t *testing.T) {
	for _, tc := range []struct {
		verb, tool, want string
	}{
		// Destroying data is refused outright: no approval unlocks it.
		{"delete", "delete_file", ClassNever},
		{"delete", "purge_workspace", ClassNever},
		{"delete", "get_issue", ""},
		// Both money verbs read the same vocabulary.
		{"bill", "stripe_refund", ClassNever},
		{"commit_money", "create_payment", ClassNever},
		{"bill", "list_invoices", ClassNever},
		{"bill", "search_docs", ""},
		{"touch_secrets", "read_api_key", ClassNever},
		{"touch_secrets", "rotate_credential", ClassNever},
		{"touch_secrets", "list_files", ""},
		// This verb names the approval, not the act. Reading it as never
		// would forbid the agent from commenting or notifying at all.
		{"send_external_without_approval", "send_email", ClassAsk},
		{"send_external_without_approval", "publish_post", ClassAsk},
		{"send_external_without_approval", "read_inbox", ""},
		// A verb nobody wrote a matcher for refuses nothing: widening the
		// product's list must not silently start removing tools.
		{"invent_new_verb", "delete_everything", ""},
	} {
		if got := OrgDenyClass(tc.verb, tc.tool, ""); got != tc.want {
			t.Errorf("OrgDenyClass(%q, %q) = %q, want %q", tc.verb, tc.tool, got, tc.want)
		}
	}

	// The description is read too, for a tool whose name says nothing.
	if got := OrgDenyClass("delete", "run", "permanently destroy the index"); got != ClassNever {
		t.Errorf("description match = %q, want %q", got, ClassNever)
	}
}

// Applying the list can only tighten. A tool already refused stays refused,
// one the deny does not name keeps the class its policy gave it, and an
// act_alone tool the deny does name loses that freedom.
func TestApplyOrgDenyOnlyTightens(t *testing.T) {
	gw := &Gateway{TrustMode: "autonomous", Servers: []GatewayServer{{
		Name: "issues",
		Tools: []GatewayTool{
			{Name: "get_issue", Risk: RiskRead, Class: ClassActAlone},
			{Name: "delete_issue", Risk: RiskExternal, Class: ClassActAlone},
			{Name: "send_email", Risk: RiskExternal, Class: ClassActAlone},
			{Name: "read_api_key", Risk: RiskSensitive, Class: ClassNever},
		},
	}}}
	tightened := ApplyOrgDeny(gw, []string{"delete", "send_external_without_approval", "touch_secrets"}, nil)

	byName := map[string]string{}
	for _, tool := range gw.Servers[0].Tools {
		byName[tool.Name] = tool.Class
	}
	if byName["get_issue"] != ClassActAlone {
		t.Errorf("get_issue = %q: a tool no verb names must keep its class", byName["get_issue"])
	}
	if byName["delete_issue"] != ClassNever {
		t.Errorf("delete_issue = %q, want never", byName["delete_issue"])
	}
	if byName["send_email"] != ClassAsk {
		t.Errorf("send_email = %q, want ask — sending is allowed, sending without a human is not", byName["send_email"])
	}
	if byName["read_api_key"] != ClassNever {
		t.Errorf("read_api_key = %q: already refused, must stay refused", byName["read_api_key"])
	}
	if len(tightened) != 2 {
		t.Errorf("tightened = %v, want only the two tools whose class actually changed", tightened)
	}

	// Nothing to apply is not the same as applying nothing wrong.
	if got := ApplyOrgDeny(gw, nil, nil); got != nil {
		t.Errorf("an empty deny list must change nothing, got %v", got)
	}
	if got := ApplyOrgDeny(nil, []string{"delete"}, nil); got != nil {
		t.Errorf("no gateway must be a no-op, got %v", got)
	}
}
