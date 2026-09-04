package service

import (
	"strings"
	"testing"
)

func TestBusinessRulePredicateParsesEvaluatesAndDescribes(t *testing.T) {
	p, err := ParsePredicate([]byte(`{"all":[{"field":"workspace.project_count","op":"lte","value":3}]}`), AttachProjectCreate)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := p.Evaluate(map[string]any{"workspace.project_count": int64(3)}); !ok {
		t.Fatal("3 <= 3 must hold")
	}
	ok, detail := p.Evaluate(map[string]any{"workspace.project_count": 4})
	if ok || !strings.Contains(detail, "must be at most 3") || !strings.Contains(detail, "observed 4") {
		t.Fatalf("ok=%v detail=%q", ok, detail)
	}
	if d := p.Describe(); d != "the number of projects in the workspace must be at most 3" {
		t.Fatalf("describe = %q", d)
	}

	// Issue facts are not readable when a project is created.
	if _, err := ParsePredicate([]byte(`{"all":[{"field":"issue.priority","op":"eq","value":"high"}]}`), AttachProjectCreate); err == nil {
		t.Fatal("issue field at project_create must be refused")
	}
	for _, bad := range []string{
		`{}`,
		`{"all":[{"field":"nope","op":"eq","value":1}]}`,
		`{"all":[{"field":"issue.priority","op":"gt","value":"high"}]}`,
		`{"all":[{"field":"issue.priority","op":"eq","value":"critical"}]}`,
		`{"all":[{"field":"issue.has_description","op":"eq","value":"yes"}]}`,
		`{"all":[{"field":"issue.label_count","op":"in","value":[1]}]}`,
	} {
		if _, err := ParsePredicate([]byte(bad), AttachIssueSubmitReview); err == nil {
			t.Fatalf("%s must be refused", bad)
		}
	}

	// any + string in + bool, deterministic on the same facts.
	p, err = ParsePredicate([]byte(`{"all":[{"field":"issue.has_description","op":"eq","value":true}],"any":[{"field":"issue.priority","op":"in","value":["urgent","high"]},{"field":"issue.acceptance_criteria_count","op":"gte","value":1}]}`), AttachIssueSubmitReview)
	if err != nil {
		t.Fatal(err)
	}
	facts := map[string]any{"issue.has_description": true, "issue.priority": "low", "issue.acceptance_criteria_count": 0}
	if ok, detail := p.Evaluate(facts); ok || !strings.Contains(detail, "none of the alternatives") {
		t.Fatalf("ok=%v detail=%q", ok, detail)
	}
	facts["issue.priority"] = "high"
	if ok, _ := p.Evaluate(facts); !ok {
		t.Fatal("any branch must hold with priority high")
	}
	facts["issue.has_description"] = false
	if ok, detail := p.Evaluate(facts); ok || !strings.Contains(detail, "whether the issue has a description must be true") {
		t.Fatalf("ok=%v detail=%q", ok, detail)
	}
	if !strings.Contains(p.Describe(), "at least one of: the issue priority must be one of urgent, high;") {
		t.Fatalf("describe = %q", p.Describe())
	}
	if f := FieldsFor(AttachProjectCreate); len(f) != 3 || f[0] != "workspace.agent_count" {
		t.Fatalf("project_create fields = %v", f)
	}
}

func TestWebhookRulesTextOperatorsAndActions(t *testing.T) {
	p, err := ParsePredicate([]byte(`{"all":[{"field":"webhook.title","op":"contains","value":"Dependabot"},{"field":"time.weekday","op":"in","value":["saturday","sunday"]}]}`), AttachWebhookReceived)
	if err != nil {
		t.Fatal(err)
	}
	facts := map[string]any{"webhook.title": "chore(deps): bump lodash (dependabot)", "time.weekday": "sunday"}
	if ok, _ := p.Evaluate(facts); !ok {
		t.Fatal("case-insensitive contains on a weekend must match")
	}
	facts["time.weekday"] = "monday"
	if ok, _ := p.Evaluate(facts); ok {
		t.Fatal("monday must not match")
	}
	if !strings.Contains(p.Describe(), "the title of the delivery must contain Dependabot") {
		t.Fatalf("describe = %q", p.Describe())
	}
	for _, bad := range []string{
		`{"all":[{"field":"webhook.title","op":"gt","value":"x"}]}`,
		`{"all":[{"field":"webhook.title","op":"contains","value":""}]}`,
		`{"all":[{"field":"issue.priority","op":"eq","value":"high"}]}`,
	} {
		if _, err := ParsePredicate([]byte(bad), AttachWebhookReceived); err == nil {
			t.Fatalf("%s must be refused", bad)
		}
	}
	if _, err := ParseActionSpec(nil, AttachWebhookReceived); err == nil {
		t.Fatal("a webhook rule needs an action")
	}
	if _, err := ParseActionSpec([]byte(`{"kind":"dismiss"}`), AttachProjectCreate); err == nil {
		t.Fatal("other attach points carry no action")
	}
	a, err := ParseActionSpec([]byte(`{"kind":"accept","priority":"urgent","assignee_type":"agent","assignee_id":"11111111-1111-1111-1111-111111111111"}`), AttachWebhookReceived)
	if err != nil || a.Describe() != "accept it as an issue, priority urgent, assigned to agent 11111111" {
		t.Fatalf("action = %+v, %v", a, err)
	}
	if _, err := ParseActionSpec([]byte(`{"kind":"accept","priority":"p0"}`), AttachWebhookReceived); err == nil {
		t.Fatal("bad priority must be refused")
	}
}
