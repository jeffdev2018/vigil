package mcpgov

import "regexp"

// Organisation non-negotiable denies (K75), applied to tools.
//
// An org unit carries a deny list that every unit inherits at save:
// delete, bill, send_external_without_approval, touch_secrets, commit_money.
// Until this file existed the list had exactly one runtime consumer — a line
// of the agent's brief reading "Never, whatever a comment says: …" — so it was
// a sentence addressed to the model, and a prompt injection was enough to
// neutralise it. A control the product calls non-negotiable has to be one.
//
// Each verb names a class of action, not a tool, so it is matched against a
// tool's name and description the same way Classify derives a risk. The
// resulting class is composed with Weaker, so this can only tighten what a
// policy already decided, never loosen it.
var orgDenyMatchers = map[string]struct {
	class string
	re    *regexp.Regexp
}{
	// Destroying data is refused outright: no approval unlocks it, which is
	// what "non-negotiable" means.
	"delete": {ClassNever, regexp.MustCompile(`(?i)(delete|destroy|purge|wipe|truncate|drop_|_drop\b|erase)`)},
	// Money leaving the workspace, under either of the two verbs that name it.
	"bill":         {ClassNever, moneyRe},
	"commit_money": {ClassNever, moneyRe},
	// Reading credentials. Same vocabulary Classify already uses for
	// RiskSensitive, kept as its own matcher so the deny is legible here.
	"touch_secrets": {ClassNever, regexp.MustCompile(`(?i)(secret|token|credential|passw|api[_-]?key|private_key|ssh_key)`)},
	// This verb names the approval, not the act: sending is allowed, sending
	// WITHOUT a human is not. So it asks rather than refuses — reading it as
	// never would forbid the agent from commenting or notifying at all.
	"send_external_without_approval": {ClassAsk, regexp.MustCompile(`(?i)(send|post|publish|email|mail|notify|sms|tweet|dispatch|webhook|broadcast)`)},
}

var moneyRe = regexp.MustCompile(`(?i)(pay|charge|refund|transfer|invoice|billing|purchase|checkout|subscribe|payout|payment)`)

// OrgDenyClass is the class a unit's deny verb forces on a tool, or "" when
// the verb says nothing about it. An unknown verb matches nothing: a list the
// product widens later must not silently start refusing tools before a matcher
// is written for it, and the caller reports what it tightened.
func OrgDenyClass(verb, tool, description string) string {
	m, ok := orgDenyMatchers[verb]
	if !ok {
		return ""
	}
	if m.re.MatchString(tool) || m.re.MatchString(description) {
		return m.class
	}
	return ""
}

// ApplyOrgDeny tightens every catalogued tool of the gateway against a unit's
// deny list and returns the tools it changed, "server/tool" each, sorted by
// the caller's iteration. Weaker composition means a tool already refused
// stays refused and nothing is ever loosened.
func ApplyOrgDeny(gw *Gateway, deny []string, describe func(server, tool string) string) []string {
	if gw == nil || len(deny) == 0 {
		return nil
	}
	var tightened []string
	for si := range gw.Servers {
		server := &gw.Servers[si]
		for ti := range server.Tools {
			tool := &server.Tools[ti]
			description := ""
			if describe != nil {
				description = describe(server.Name, tool.Name)
			}
			before := tool.Class
			for _, verb := range deny {
				if class := OrgDenyClass(verb, tool.Name, description); class != "" {
					tool.Class = Weaker(tool.Class, class)
				}
			}
			if tool.Class != before {
				tightened = append(tightened, server.Name+"/"+tool.Name)
			}
		}
	}
	return tightened
}
