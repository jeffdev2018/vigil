package mcpgov

import (
	"strings"
	"testing"
)

func TestClassifyAndCeilings(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{"get_issue": RiskRead, "list_files": RiskRead, "create_ticket": RiskInternalWrite, "send_email": RiskExternal, "delete_repo": RiskExternal, "read_api_key": RiskSensitive, "frobnicate": RiskUnknown} {
		if got := Classify(name, ""); got != want {
			t.Fatalf("%s: %s, want %s", name, got, want)
		}
	}
	if Classify("do_it", "sends a message to Slack") != RiskExternal {
		t.Fatal("the description counts")
	}
	if Ceiling("observer", RiskRead) != ClassActAlone || Ceiling("observer", RiskInternalWrite) != ClassNever || Ceiling("propose", RiskExternal) != ClassAsk || Ceiling("approval", RiskInternalWrite) != ClassActAlone || Ceiling("autonomous", RiskSensitive) != ClassActAlone {
		t.Fatal("ceilings follow the trust dial")
	}
	p := Policy{Default: ClassByRisk, Tools: map[string]string{"send_email": ClassNever}}
	if p.Effective("send_email", RiskExternal, "autonomous") != ClassNever || p.Effective("get_issue", RiskRead, "observer") != ClassActAlone || p.Effective("delete_repo", RiskExternal, "propose") != ClassAsk || p.Effective("delete_repo", RiskExternal, "autonomous") != ClassAsk {
		t.Fatal("effective = requested capped by the ceiling")
	}
	strict := Policy{Default: ClassNever, Tools: map[string]string{"get_issue": ClassActAlone}}
	if strict.Effective("unknown_tool", RiskRead, "autonomous") != ClassNever || strict.Effective("get_issue", RiskRead, "autonomous") != ClassActAlone {
		t.Fatal("a never default is a strict allowlist")
	}
	catalog := []CatalogTool{{Name: "send_email", Risk: RiskExternal}}
	if err := (Policy{Tools: map[string]string{"send_email": ClassActAlone}}).Validate(catalog, "propose"); err == nil || !strings.Contains(err.Error(), "trust dial") {
		t.Fatalf("ceiling refused at save: %v", err)
	}
	if err := (Policy{Default: "sometimes"}).Validate(nil, "autonomous"); err == nil {
		t.Fatal("unknown default refused")
	}
	if err := (Policy{Tools: map[string]string{"send_email": ClassActAlone}}).Validate(catalog, "autonomous"); err != nil {
		t.Fatal(err)
	}
}

func TestRuleOfTwo(t *testing.T) {
	t.Parallel()
	set := []Binding{{Server: "crm", Tools: []GatewayTool{{Name: "get_contact", Risk: RiskRead, Class: ClassActAlone}, {Name: "read_ssn", Risk: RiskSensitive, Class: ClassActAlone}}}, {Server: "mail", Tools: []GatewayTool{{Name: "send_email", Risk: RiskExternal, Class: ClassActAlone}}}}
	if err := RuleOfTwo(set); err == nil || !strings.Contains(err.Error(), "mail/send_email") {
		t.Fatalf("three properties with an external tool alone is refused: %v", err)
	}
	set[1].Tools[0].Class = ClassAsk
	if err := RuleOfTwo(set); err != nil {
		t.Fatal("ask on the external effect satisfies the rule")
	}
	set[1].Tools[0].Class = ClassActAlone
	set[0].Tools[1].Class = ClassNever
	if err := RuleOfTwo(set); err != nil {
		t.Fatal("an unreachable tool does not count")
	}
	set[0].Tools[1].Class = ClassActAlone
	if !Tighten(set) || set[1].Tools[0].Class != ClassAsk || Tighten(set) {
		t.Fatal("tighten downgrades external tools to ask once")
	}
}
