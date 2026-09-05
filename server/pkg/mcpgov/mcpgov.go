// Package mcpgov is the governed MCP gateway's shared vocabulary (K77): how a
// tool is classified by risk, which approval class a binding may grant an
// agent for it, and the Rule of Two that bounds a binding set. The handler
// decides with it at save and claim time; the daemon enforces the result at
// each call. It has no database and no network so both can test it alone.
package mcpgov

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Risk classes of one tool.
const (
	RiskRead          = "read"
	RiskInternalWrite = "internal_write"
	RiskExternal      = "external_effect"
	RiskSensitive     = "sensitive_data"
	RiskUnknown       = "unknown"
)

// Approval classes a binding grants for one tool.
const (
	ClassActAlone = "act_alone"
	ClassAsk      = "ask"
	ClassNever    = "never"
	// ClassByRisk is a policy default only: derive the class from the risk.
	ClassByRisk = "by_risk"
)

// Risks lists the classes an administrator may set by hand.
var Risks = []string{RiskRead, RiskInternalWrite, RiskExternal, RiskSensitive, RiskUnknown}

// Classes lists the per-tool classes a binding may carry.
var Classes = []string{ClassActAlone, ClassAsk, ClassNever}

// Rule of Two properties, shared with the org chart (K75).
const (
	PropUntrustedInput  = "untrusted_input"
	PropSensitiveData   = "sensitive_data"
	PropExternalEffects = "external_effects"
)

// CatalogTool is one tool of a workspace MCP server as stored on the server
// row: what was discovered plus the risk an administrator may have adjusted.
type CatalogTool struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	SchemaDigest string `json:"schema_digest,omitempty"`
	Risk         string `json:"risk"`
	// RiskSource is "auto" for a pattern classification, "manual" once set by hand.
	RiskSource string `json:"risk_source"`
}

// Policy is the per-binding tool policy stored on agent_mcp_server.
type Policy struct {
	// Default applies to a tool without an explicit entry: by_risk (default),
	// ask, never. A `never` default turns the entry map into a strict allowlist.
	Default string            `json:"default,omitempty"`
	Tools   map[string]string `json:"tools,omitempty"`
}

// GatewayTool is one tool as the daemon enforces it: its risk and the class
// in force for this run.
type GatewayTool struct {
	Name  string `json:"name"`
	Risk  string `json:"risk"`
	Class string `json:"class"`
}

// GatewayServer is one MCP server the run may reach through the gateway.
type GatewayServer struct {
	Name string `json:"name"`
	// ServerID is set for a workspace server (usage is tracked against it).
	ServerID string        `json:"server_id,omitempty"`
	Default  string        `json:"default"`
	Tools    []GatewayTool `json:"tools"`
}

// Gateway is the claim payload: everything the daemon needs to decide a call
// without asking the server, plus the dial that caps tools it has never seen.
type Gateway struct {
	TrustMode string          `json:"trust_mode"`
	Servers   []GatewayServer `json:"servers"`
}

var (
	sensitiveRe = regexp.MustCompile(`(?i)(secret|token|credential|passw|api[_-]?key|private|personal|pii\b|ssn|salary|payroll|bank|iban|card|health|medical|patient|dump|export_all)`)
	externalRe  = regexp.MustCompile(`(?i)(send|post|publish|email|mail|notify|sms|tweet|message|pay|charge|refund|transfer|deploy|release|delete|remove|destroy|drop|purge|wipe|execute|exec|run|shell|invoke|trigger|dispatch|call|order|book|approve|merge|push|upload)`)
	writeRe     = regexp.MustCompile(`(?i)(create|update|write|set|add|edit|save|put|patch|move|rename|tag|label|comment|assign|insert|append|modify|archive|close|reopen|toggle)`)
	readRe      = regexp.MustCompile(`(?i)(get|list|search|read|fetch|query|describe|find|show|status|browse|lookup|view|count|check|inspect|preview|resolve|summar)`)
)

// Classify derives a risk from a tool's name and description. Sensitive data
// wins over external effects, which win over internal writes, which win over
// reads; a name that matches nothing is unknown, which the default policy
// treats like an external effect.
func Classify(name, description string) string {
	text := name + " " + description
	switch {
	case sensitiveRe.MatchString(text):
		return RiskSensitive
	case externalRe.MatchString(text):
		return RiskExternal
	case writeRe.MatchString(text):
		return RiskInternalWrite
	case readRe.MatchString(text):
		return RiskRead
	default:
		return RiskUnknown
	}
}

// ClassForRisk is the class a by_risk policy grants.
func ClassForRisk(risk string) string {
	switch risk {
	case RiskRead, RiskInternalWrite:
		return ClassActAlone
	default:
		return ClassAsk
	}
}

var trustRank = map[string]int{"observer": 0, "propose": 1, "approval": 2, "autonomous": 3}

// Ceiling is the strongest class the agent's trust dial (K26) allows for a
// tool of this risk: an observer only reads alone, a proposing or approving
// agent writes internally alone but asks before any external or sensitive
// effect, an autonomous agent may act alone everywhere.
func Ceiling(trustMode, risk string) string {
	rank, ok := trustRank[trustMode]
	if !ok {
		rank = 1
	}
	switch {
	case rank >= 3:
		return ClassActAlone
	case rank == 0:
		if risk == RiskRead {
			return ClassActAlone
		}
		return ClassNever
	default:
		if risk == RiskRead || risk == RiskInternalWrite {
			return ClassActAlone
		}
		return ClassAsk
	}
}

var classRank = map[string]int{ClassNever: 0, ClassAsk: 1, ClassActAlone: 2}

// Weaker returns the more restrictive of two classes.
func Weaker(a, b string) string {
	if classRank[a] <= classRank[b] {
		return a
	}
	return b
}

// Requested is the class a policy names for a tool before any ceiling.
func (p Policy) Requested(tool, risk string) string {
	if c, ok := p.Tools[tool]; ok && (c == ClassNever || classRank[c] > 0) {
		return c
	}
	switch p.Default {
	case ClassNever, ClassAsk:
		return p.Default
	default:
		return ClassForRisk(risk)
	}
}

// Effective is the class in force for a tool: the policy's request, capped by
// the dial ceiling.
func (p Policy) Effective(tool, risk, trustMode string) string {
	return Weaker(p.Requested(tool, risk), Ceiling(trustMode, risk))
}

// Validate refuses a policy that names an unknown class or asks the dial for
// more than it allows on a catalogued tool.
func (p Policy) Validate(catalog []CatalogTool, trustMode string) error {
	switch p.Default {
	case "", ClassByRisk, ClassAsk, ClassNever:
	default:
		return fmt.Errorf("unknown default class %q", p.Default)
	}
	risks := map[string]string{}
	for _, t := range catalog {
		risks[t.Name] = t.Risk
	}
	for tool, class := range p.Tools {
		if classRank[class] == 0 && class != ClassNever {
			return fmt.Errorf("unknown class %q for tool %q", class, tool)
		}
		risk, ok := risks[tool]
		if !ok {
			continue
		}
		if ceiling := Ceiling(trustMode, risk); classRank[class] > classRank[ceiling] {
			return fmt.Errorf("tool %q is %s: the agent's trust dial (%s) allows at most %q", tool, risk, trustMode, ceiling)
		}
	}
	return nil
}

// Properties are the Rule of Two properties a tool contributes when it is
// reachable: a read returns untrusted content, a sensitive tool touches
// sensitive data, an external or unknown tool has effects outside.
func Properties(risk string) []string {
	switch risk {
	case RiskRead:
		return []string{PropUntrustedInput}
	case RiskSensitive:
		return []string{PropSensitiveData, PropUntrustedInput}
	case RiskExternal, RiskUnknown:
		return []string{PropExternalEffects}
	default:
		return nil
	}
}

// Binding is one server's reachable tools with their effective classes, the
// unit the Rule of Two is checked over.
type Binding struct {
	Server string
	Tools  []GatewayTool
}

// RuleOfTwo refuses a binding set that lets an agent read untrusted content,
// touch sensitive data and act outside, all at once, with any external effect
// running without a human: exclude one property or set the external tools to
// ask. Tools of class never do not count: they are unreachable.
func RuleOfTwo(bindings []Binding) error {
	props := map[string]bool{}
	var alone []string
	for _, b := range bindings {
		for _, t := range b.Tools {
			if t.Class == ClassNever {
				continue
			}
			for _, p := range Properties(t.Risk) {
				props[p] = true
			}
			if t.Class == ClassActAlone && (t.Risk == RiskExternal || t.Risk == RiskUnknown) {
				alone = append(alone, b.Server+"/"+t.Name)
			}
		}
	}
	if props[PropUntrustedInput] && props[PropSensitiveData] && props[PropExternalEffects] && len(alone) > 0 {
		sort.Strings(alone)
		return fmt.Errorf("Rule of Two: this agent would read untrusted content, touch sensitive data and act outside at once; set %s to ask, or drop one of the three", strings.Join(alone, ", "))
	}
	return nil
}

// Tighten applies the Rule of Two at claim time: when the set trips it, every
// external or unknown tool running alone is downgraded to ask so the run is
// always safe even if a catalogue changed after the bindings were saved.
func Tighten(bindings []Binding) (changed bool) {
	if RuleOfTwo(bindings) == nil {
		return false
	}
	for i := range bindings {
		for j, t := range bindings[i].Tools {
			if t.Class == ClassActAlone && (t.Risk == RiskExternal || t.Risk == RiskUnknown) {
				bindings[i].Tools[j].Class = ClassAsk
				changed = true
			}
		}
	}
	return changed
}

// HighRisk reports whether a successful call of this risk is worth an alert
// to the owner even when a human approved it.
func HighRisk(risk string) bool { return risk == RiskExternal || risk == RiskSensitive }
