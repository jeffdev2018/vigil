package skill

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DAEMON.md (F24 / JEF-15): a declarative autopilot. The frontmatter is the
// configuration — what it is called, which agent runs it, when it fires, what
// it produces — and the body below is the skill the agent reads on every run.
//
// The frontmatter is validated STRICTLY: an unknown key is an error, not
// something to ignore. A declaration file's whole value is that what it says
// is what runs, and a silently dropped `triggerss:` typo would leave an
// operator convinced they had scheduled something they had not.
//
// Errors carry the line they occurred on so the import dialog can point at the
// offending line instead of saying "invalid document".

// DaemonTrigger is one entry of the `triggers` list.
type DaemonTrigger struct {
	Kind     string `json:"kind"`
	Cron     string `json:"cron,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	Label    string `json:"label,omitempty"`
	// Line is the document line the entry starts on, for error reporting.
	Line int `json:"line,omitempty"`
}

// DaemonBudget is the optional `budget` mapping. Both fields are nil when the
// key is absent.
type DaemonBudget struct {
	RunsPerDay *int `json:"runs_per_day,omitempty"`
	MaxMinutes *int `json:"max_minutes,omitempty"`
}

// DaemonDoc is a parsed, structurally valid DAEMON.md. Cross-field validation
// that needs the autopilot domain (cron expressions, issue title templates,
// agent existence) stays in the handler; this type carries the line numbers it
// needs to report those failures against the right line.
type DaemonDoc struct {
	Name               string          `json:"name"`
	Role               string          `json:"role"`
	Agent              string          `json:"agent"`
	Triggers           []DaemonTrigger `json:"triggers,omitempty"`
	Budget             *DaemonBudget   `json:"budget,omitempty"`
	Outputs            string          `json:"outputs"`
	IssueTitleTemplate string          `json:"issue_title_template,omitempty"`
	// Body is everything below the closing `---`, verbatim. It becomes the
	// skill content bound to the daemon's agent.
	Body string `json:"body"`
	// Lines maps a top-level frontmatter key to the document line it was
	// declared on, so a handler-side validation failure can be reported
	// against the line the operator wrote.
	Lines map[string]int `json:"-"`
}

// DaemonParseError is one line-addressed problem with a document. Line is
// 1-based and 0 when the problem has no single line (an empty document).
type DaemonParseError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

func (e DaemonParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Message)
	}
	return e.Message
}

// DaemonOutputs are the accepted `outputs` values, mapped to execution_mode by
// the handler. "issue" is the default when the key is absent.
const (
	DaemonOutputIssue   = "issue"
	DaemonOutputRunOnly = "run_only"
)

// daemonTopLevelKeys is the closed set. Anything else is refused.
var daemonTopLevelKeys = map[string]bool{
	"name":                 true,
	"role":                 true,
	"agent":                true,
	"triggers":             true,
	"budget":               true,
	"outputs":              true,
	"issue_title_template": true,
}

var daemonTriggerKeys = map[string]bool{
	"kind":     true,
	"cron":     true,
	"timezone": true,
	"label":    true,
}

var daemonBudgetKeys = map[string]bool{
	"runs_per_day": true,
	"max_minutes":  true,
}

// ParseDaemonMarkdown decodes a DAEMON.md into its typed form. It returns
// every problem it found rather than the first, so the import dialog can
// mark up the whole document in one pass. A non-empty error slice means the
// returned doc must not be written anywhere.
func ParseDaemonMarkdown(content string) (DaemonDoc, []DaemonParseError) {
	doc := DaemonDoc{Outputs: DaemonOutputIssue, Lines: map[string]int{}}

	if !strings.HasPrefix(content, "---") {
		return doc, []DaemonParseError{{Line: 1, Message: "document must start with a YAML frontmatter block delimited by ---"}}
	}
	match := frontmatterPattern.FindStringSubmatch(content)
	if match == nil {
		return doc, []DaemonParseError{{Line: 1, Message: "frontmatter block is not closed by a --- line"}}
	}
	doc.Body = strings.TrimPrefix(content[len(match[0]):], "---")
	doc.Body = strings.TrimLeft(doc.Body, "\r\n")

	// The captured block starts on document line 2 (line 1 is the opening
	// ---), and yaml.Node lines are 1-based within the block.
	const lineOffset = 1

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(match[1]), &root); err != nil {
		return doc, []DaemonParseError{{Line: yamlErrorLine(err) + lineOffset, Message: "frontmatter is not valid YAML: " + yamlErrorMessage(err)}}
	}
	if len(root.Content) == 0 {
		return doc, []DaemonParseError{{Line: 1, Message: "frontmatter is empty; name, role and agent are required"}}
	}
	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return doc, []DaemonParseError{{Line: mapping.Line + lineOffset, Message: "frontmatter must be a mapping of keys to values"}}
	}

	var errs []DaemonParseError
	seen := map[string]bool{}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode, valNode := mapping.Content[i], mapping.Content[i+1]
		key := keyNode.Value
		line := keyNode.Line + lineOffset
		if !daemonTopLevelKeys[key] {
			errs = append(errs, DaemonParseError{Line: line, Message: fmt.Sprintf("unknown key %q; allowed keys are %s", key, joinKeys(daemonTopLevelKeys))})
			continue
		}
		if seen[key] {
			errs = append(errs, DaemonParseError{Line: line, Message: fmt.Sprintf("duplicate key %q", key)})
			continue
		}
		seen[key] = true
		doc.Lines[key] = line

		switch key {
		case "name":
			doc.Name, errs = daemonScalar(valNode, key, line, errs)
		case "role":
			doc.Role, errs = daemonScalar(valNode, key, line, errs)
		case "agent":
			doc.Agent, errs = daemonScalar(valNode, key, line, errs)
		case "issue_title_template":
			doc.IssueTitleTemplate, errs = daemonScalar(valNode, key, line, errs)
		case "outputs":
			var raw string
			raw, errs = daemonScalar(valNode, key, line, errs)
			raw = strings.TrimSpace(raw)
			switch raw {
			case "":
				// keep the default
			case DaemonOutputIssue, DaemonOutputRunOnly:
				doc.Outputs = raw
			default:
				errs = append(errs, DaemonParseError{Line: valNode.Line + lineOffset, Message: fmt.Sprintf("outputs must be %q or %q, got %q", DaemonOutputIssue, DaemonOutputRunOnly, raw)})
			}
		case "triggers":
			doc.Triggers, errs = parseDaemonTriggers(valNode, lineOffset, errs)
		case "budget":
			doc.Budget, errs = parseDaemonBudget(valNode, lineOffset, errs)
		}
	}

	for _, required := range []string{"name", "role", "agent"} {
		if !seen[required] {
			errs = append(errs, DaemonParseError{Line: 1, Message: fmt.Sprintf("%s is required", required)})
		}
	}
	if seen["name"] && strings.TrimSpace(doc.Name) == "" {
		errs = append(errs, DaemonParseError{Line: doc.Lines["name"], Message: "name must not be empty"})
	}
	if seen["role"] && strings.TrimSpace(doc.Role) == "" {
		errs = append(errs, DaemonParseError{Line: doc.Lines["role"], Message: "role must not be empty"})
	}
	if seen["agent"] && strings.TrimSpace(doc.Agent) == "" {
		errs = append(errs, DaemonParseError{Line: doc.Lines["agent"], Message: "agent must not be empty"})
	}

	doc.Name = strings.TrimSpace(doc.Name)
	doc.Role = strings.TrimSpace(doc.Role)
	doc.Agent = strings.TrimSpace(doc.Agent)
	doc.IssueTitleTemplate = strings.TrimSpace(doc.IssueTitleTemplate)

	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Line < errs[j].Line })
	return doc, errs
}

func parseDaemonTriggers(node *yaml.Node, lineOffset int, errs []DaemonParseError) ([]DaemonTrigger, []DaemonParseError) {
	if node.Tag == "!!null" {
		return nil, errs
	}
	if node.Kind != yaml.SequenceNode {
		return nil, append(errs, DaemonParseError{Line: node.Line + lineOffset, Message: "triggers must be a list"})
	}
	triggers := make([]DaemonTrigger, 0, len(node.Content))
	for _, item := range node.Content {
		line := item.Line + lineOffset
		if item.Kind != yaml.MappingNode {
			errs = append(errs, DaemonParseError{Line: line, Message: "each trigger must be a mapping with a kind"})
			continue
		}
		trigger := DaemonTrigger{Line: line}
		seen := map[string]bool{}
		for i := 0; i+1 < len(item.Content); i += 2 {
			keyNode, valNode := item.Content[i], item.Content[i+1]
			key := keyNode.Value
			keyLine := keyNode.Line + lineOffset
			if !daemonTriggerKeys[key] {
				errs = append(errs, DaemonParseError{Line: keyLine, Message: fmt.Sprintf("unknown trigger key %q; allowed keys are %s", key, joinKeys(daemonTriggerKeys))})
				continue
			}
			if seen[key] {
				errs = append(errs, DaemonParseError{Line: keyLine, Message: fmt.Sprintf("duplicate trigger key %q", key)})
				continue
			}
			seen[key] = true
			var value string
			value, errs = daemonScalar(valNode, key, keyLine, errs)
			value = strings.TrimSpace(value)
			switch key {
			case "kind":
				trigger.Kind = value
			case "cron":
				trigger.Cron = value
			case "timezone":
				trigger.Timezone = value
			case "label":
				trigger.Label = value
			}
		}
		switch trigger.Kind {
		case "schedule":
			if trigger.Cron == "" {
				errs = append(errs, DaemonParseError{Line: line, Message: "a schedule trigger needs a cron expression"})
			}
		case "webhook":
			if trigger.Cron != "" {
				errs = append(errs, DaemonParseError{Line: line, Message: "cron is not valid for a webhook trigger"})
			}
			if trigger.Timezone != "" {
				errs = append(errs, DaemonParseError{Line: line, Message: "timezone is not valid for a webhook trigger"})
			}
		case "api":
			// Reserved-but-inert since the kind was retired at the API
			// boundary: nothing schedules it and no ingress route reaches it,
			// so accepting one here would declare a trigger that can never
			// fire. The manual run endpoint already works for any autopilot.
			errs = append(errs, DaemonParseError{Line: line, Message: `trigger kind "api" is retired; use "schedule" or "webhook", or run the daemon manually`})
		case "":
			errs = append(errs, DaemonParseError{Line: line, Message: "each trigger needs a kind of schedule or webhook"})
		default:
			errs = append(errs, DaemonParseError{Line: line, Message: fmt.Sprintf("invalid trigger kind %q; use schedule or webhook", trigger.Kind)})
		}
		triggers = append(triggers, trigger)
	}
	return triggers, errs
}

func parseDaemonBudget(node *yaml.Node, lineOffset int, errs []DaemonParseError) (*DaemonBudget, []DaemonParseError) {
	if node.Tag == "!!null" {
		return nil, errs
	}
	if node.Kind != yaml.MappingNode {
		return nil, append(errs, DaemonParseError{Line: node.Line + lineOffset, Message: "budget must be a mapping"})
	}
	budget := &DaemonBudget{}
	seen := map[string]bool{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		key := keyNode.Value
		keyLine := keyNode.Line + lineOffset
		if !daemonBudgetKeys[key] {
			errs = append(errs, DaemonParseError{Line: keyLine, Message: fmt.Sprintf("unknown budget key %q; allowed keys are %s", key, joinKeys(daemonBudgetKeys))})
			continue
		}
		if seen[key] {
			errs = append(errs, DaemonParseError{Line: keyLine, Message: fmt.Sprintf("duplicate budget key %q", key)})
			continue
		}
		seen[key] = true
		var n int
		if err := valNode.Decode(&n); err != nil || n <= 0 {
			errs = append(errs, DaemonParseError{Line: keyLine, Message: fmt.Sprintf("%s must be a positive whole number", key)})
			continue
		}
		switch key {
		case "runs_per_day":
			budget.RunsPerDay = &n
		case "max_minutes":
			budget.MaxMinutes = &n
		}
	}
	return budget, errs
}

// daemonScalar reads a scalar (or block scalar) value as a string. A
// structured value where a string belongs is an error rather than a JSON
// round-trip: the skill parser coerces because it must not fail, a declaration
// must fail because the operator meant something the schema cannot express.
func daemonScalar(node *yaml.Node, key string, line int, errs []DaemonParseError) (string, []DaemonParseError) {
	if node.Tag == "!!null" {
		return "", errs
	}
	if node.Kind != yaml.ScalarNode {
		return "", append(errs, DaemonParseError{Line: line, Message: fmt.Sprintf("%s must be a single value, not a list or mapping", key)})
	}
	return node.Value, errs
}

func joinKeys(set map[string]bool) string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// yamlErrorLine digs the line out of a yaml.TypeError / parse error message.
// yaml.v3 reports "yaml: line N: ..." for syntax errors; anything else is
// reported against the frontmatter's first line.
func yamlErrorLine(err error) int {
	msg := err.Error()
	const prefix = "yaml: line "
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		return 1
	}
	rest := msg[idx+len(prefix):]
	n := 0
	digits := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		digits++
	}
	if digits == 0 {
		return 1
	}
	return n
}

// yamlErrorMessage strips yaml.v3's "yaml: line N: " prefix so the line is
// not stated twice once DaemonParseError renders its own.
func yamlErrorMessage(err error) string {
	msg := err.Error()
	if idx := strings.Index(msg, ": "); idx >= 0 && strings.HasPrefix(msg, "yaml: line ") {
		if tail := strings.SplitN(msg, ": ", 3); len(tail) == 3 {
			return tail[2]
		}
	}
	return strings.TrimPrefix(msg, "yaml: ")
}

// RenderDaemonMarkdown writes a DAEMON.md for a doc. It is the fallback for
// exporting an autopilot that was never imported from one (created in the UI
// or over the API), so a workspace can pull any daemon into a file and keep it
// there. An autopilot that WAS imported exports its stored source verbatim
// instead: reconstructing from columns would drop every declared field the
// schema has no column for.
//
// Values go through the YAML encoder rather than string concatenation so a
// cron expression, a role containing a colon, or a title starting with `[`
// round-trips instead of producing a document that no longer parses.
func RenderDaemonMarkdown(doc DaemonDoc) (string, error) {
	fm := map[string]any{
		"name":  doc.Name,
		"role":  doc.Role,
		"agent": doc.Agent,
	}
	if doc.Outputs != "" {
		fm["outputs"] = doc.Outputs
	}
	if doc.IssueTitleTemplate != "" {
		fm["issue_title_template"] = doc.IssueTitleTemplate
	}
	if len(doc.Triggers) > 0 {
		triggers := make([]map[string]any, 0, len(doc.Triggers))
		for _, t := range doc.Triggers {
			entry := map[string]any{"kind": t.Kind}
			if t.Cron != "" {
				entry["cron"] = t.Cron
			}
			if t.Timezone != "" {
				entry["timezone"] = t.Timezone
			}
			if t.Label != "" {
				entry["label"] = t.Label
			}
			triggers = append(triggers, entry)
		}
		fm["triggers"] = triggers
	}
	if doc.Budget != nil {
		budget := map[string]any{}
		if doc.Budget.RunsPerDay != nil {
			budget["runs_per_day"] = *doc.Budget.RunsPerDay
		}
		if doc.Budget.MaxMinutes != nil {
			budget["max_minutes"] = *doc.Budget.MaxMinutes
		}
		if len(budget) > 0 {
			fm["budget"] = budget
		}
	}

	var block strings.Builder
	enc := yaml.NewEncoder(&block)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}

	body := doc.Body
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return "---\n" + block.String() + "---\n\n" + body, nil
}
