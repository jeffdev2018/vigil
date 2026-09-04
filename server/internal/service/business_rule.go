package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Business rules (K53): a predicate compiled once (by a model, or written by
// hand) and evaluated deterministically against a flat map of facts. The
// field catalog is fixed on purpose: a rule can only read what the product
// can measure, and the plain-language rendering never shows raw JSON.

const (
	AttachProjectCreate     = "project_create"
	AttachIssueSubmitReview = "issue_submit_review"
	AttachAgentRunDispatch  = "agent_run_dispatch"
)

var AttachPoints = []string{AttachProjectCreate, AttachIssueSubmitReview, AttachAgentRunDispatch}

type RuleField struct {
	Kind   string   // number | bool | string
	Label  string   // plain-language name
	Values []string // allowed values for string fields
	Attach []string // attach points where the fact exists
}

var workspaceAttach = []string{AttachProjectCreate, AttachIssueSubmitReview, AttachAgentRunDispatch}
var issueAttach = []string{AttachIssueSubmitReview, AttachAgentRunDispatch}

// RuleFields is the whole vocabulary a rule may use.
var RuleFields = map[string]RuleField{
	"workspace.project_count":         {Kind: "number", Label: "the number of projects in the workspace", Attach: workspaceAttach},
	"workspace.member_count":          {Kind: "number", Label: "the number of members in the workspace", Attach: workspaceAttach},
	"workspace.agent_count":           {Kind: "number", Label: "the number of agents in the workspace", Attach: workspaceAttach},
	"issue.title_length":              {Kind: "number", Label: "the length of the issue title", Attach: issueAttach},
	"issue.has_description":           {Kind: "bool", Label: "whether the issue has a description", Attach: issueAttach},
	"issue.acceptance_criteria_count": {Kind: "number", Label: "the number of acceptance criteria on the issue", Attach: issueAttach},
	"issue.label_count":               {Kind: "number", Label: "the number of labels on the issue", Attach: issueAttach},
	"issue.pull_request_count":        {Kind: "number", Label: "the number of pull requests linked to the issue", Attach: issueAttach},
	"issue.decision_count":            {Kind: "number", Label: "the number of decision records on the issue", Attach: issueAttach},
	"issue.priority":                  {Kind: "string", Label: "the issue priority", Values: []string{"urgent", "high", "medium", "low", "none"}, Attach: issueAttach},
	"issue.assignee_type":             {Kind: "string", Label: "who the issue is assigned to", Values: []string{"member", "agent", "none"}, Attach: issueAttach},
}

// FieldsFor lists the fields readable at an attach point, sorted.
func FieldsFor(attach string) []string {
	var out []string
	for name, f := range RuleFields {
		for _, a := range f.Attach {
			if a == attach {
				out = append(out, name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

type RuleCondition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// RulePredicate holds when every condition in All holds and, when Any is
// set, at least one of its conditions holds.
type RulePredicate struct {
	All []RuleCondition `json:"all,omitempty"`
	Any []RuleCondition `json:"any,omitempty"`
}

var opLabels = map[string]string{
	"eq": "must be", "ne": "must not be", "lt": "must be less than", "lte": "must be at most",
	"gt": "must be more than", "gte": "must be at least", "in": "must be one of",
}

// ParsePredicate validates the JSON against the field catalog for an attach
// point. Every field must exist there, every op must fit the field's kind.
func ParsePredicate(raw []byte, attach string) (RulePredicate, error) {
	var p RulePredicate
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("predicate is not valid JSON: %w", err)
	}
	if len(p.All)+len(p.Any) == 0 {
		return p, fmt.Errorf("predicate has no condition")
	}
	allowed := map[string]bool{}
	for _, f := range FieldsFor(attach) {
		allowed[f] = true
	}
	for _, c := range append(append([]RuleCondition{}, p.All...), p.Any...) {
		f, ok := RuleFields[c.Field]
		if !ok || !allowed[c.Field] {
			return p, fmt.Errorf("field %q is not available at %s", c.Field, attach)
		}
		if _, ok := opLabels[c.Op]; !ok {
			return p, fmt.Errorf("unknown operator %q", c.Op)
		}
		switch f.Kind {
		case "number":
			if _, ok := c.Value.(float64); !ok || c.Op == "in" {
				return p, fmt.Errorf("%s needs a numeric comparison", c.Field)
			}
		case "bool":
			if _, ok := c.Value.(bool); !ok || (c.Op != "eq" && c.Op != "ne") {
				return p, fmt.Errorf("%s needs eq/ne with true or false", c.Field)
			}
		case "string":
			if c.Op == "in" {
				list, ok := c.Value.([]any)
				if !ok || len(list) == 0 {
					return p, fmt.Errorf("%s needs a list of values with in", c.Field)
				}
				for _, v := range list {
					if s, ok := v.(string); !ok || !contains(f.Values, s) {
						return p, fmt.Errorf("%s accepts only %s", c.Field, strings.Join(f.Values, ", "))
					}
				}
			} else {
				s, ok := c.Value.(string)
				if !ok || (c.Op != "eq" && c.Op != "ne") || !contains(f.Values, s) {
					return p, fmt.Errorf("%s needs eq/ne with one of %s", c.Field, strings.Join(f.Values, ", "))
				}
			}
		}
	}
	return p, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (c RuleCondition) holds(facts map[string]any) bool {
	got, ok := facts[c.Field]
	if !ok {
		return false
	}
	switch v := c.Value.(type) {
	case float64:
		n, ok := toFloat(got)
		if !ok {
			return false
		}
		switch c.Op {
		case "eq":
			return n == v
		case "ne":
			return n != v
		case "lt":
			return n < v
		case "lte":
			return n <= v
		case "gt":
			return n > v
		case "gte":
			return n >= v
		}
	case bool:
		b, ok := got.(bool)
		if !ok {
			return false
		}
		return (c.Op == "eq") == (b == v)
	case string:
		s, ok := got.(string)
		if !ok {
			return false
		}
		return (c.Op == "eq") == (s == v)
	case []any:
		s, ok := got.(string)
		if !ok {
			return false
		}
		for _, item := range v {
			if item == s {
				return true
			}
		}
		return false
	}
	return false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// Evaluate returns whether the facts satisfy the rule and, when they do
// not, a sentence naming the first broken condition and the observed value.
func (p RulePredicate) Evaluate(facts map[string]any) (bool, string) {
	for _, c := range p.All {
		if !c.holds(facts) {
			return false, c.explain(facts)
		}
	}
	if len(p.Any) > 0 {
		for _, c := range p.Any {
			if c.holds(facts) {
				return true, ""
			}
		}
		return false, p.Any[0].explain(facts) + " (none of the alternatives holds)"
	}
	return true, ""
}

func (c RuleCondition) explain(facts map[string]any) string {
	return fmt.Sprintf("%s %s %s, observed %v", RuleFields[c.Field].Label, opLabels[c.Op], formatValue(c.Value), facts[c.Field])
}

func formatValue(v any) string {
	if list, ok := v.([]any); ok {
		parts := make([]string, 0, len(list))
		for _, item := range list {
			parts = append(parts, fmt.Sprint(item))
		}
		return strings.Join(parts, ", ")
	}
	if f, ok := v.(float64); ok && f == float64(int64(f)) {
		return fmt.Sprint(int64(f))
	}
	return fmt.Sprint(v)
}

// Describe renders the predicate in plain language for the preview.
func (p RulePredicate) Describe() string {
	var parts []string
	for _, c := range p.All {
		parts = append(parts, fmt.Sprintf("%s %s %s", RuleFields[c.Field].Label, opLabels[c.Op], formatValue(c.Value)))
	}
	if len(p.Any) > 0 {
		var alts []string
		for _, c := range p.Any {
			alts = append(alts, fmt.Sprintf("%s %s %s", RuleFields[c.Field].Label, opLabels[c.Op], formatValue(c.Value)))
		}
		parts = append(parts, "at least one of: "+strings.Join(alts, "; "))
	}
	return strings.Join(parts, ", and ")
}
