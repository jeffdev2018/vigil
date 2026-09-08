package daemon

import (
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Remediation keys attached to an error task_message. They name a UI action
// the client already has, not a message: the wording belongs to the locale
// files, and an installed client that predates a key renders the error without
// a button rather than guessing.
const (
	// RemediationRerun: the run can simply be tried again — a rate limit, a
	// provider hiccup, a timeout.
	RemediationRerun = "rerun"
	// RemediationRuntimeSettings: the agent is misconfigured or unauthorized.
	// Re-running changes nothing until the runtime's credentials or model are
	// fixed.
	RemediationRuntimeSettings = "runtime_settings"
	// RemediationInstallCLI: the agent CLI is missing or too old on this
	// machine.
	RemediationInstallCLI = "install_cli"
)

// remediationForReason maps the canonical failure taxonomy onto the action that
// actually helps. A reason with no entry gets no button: offering "try again"
// for a context overflow or a blocked agent would be a lie, and an unknown
// failure has no honest suggestion.
var remediationForReason = map[taskfailure.Reason]string{
	taskfailure.ReasonAgentProviderCapacityOrRateLimit: RemediationRerun,
	taskfailure.ReasonAgentProviderServerError:         RemediationRerun,
	taskfailure.ReasonAgentProviderNetwork:             RemediationRerun,
	taskfailure.ReasonAgentTimeout:                     RemediationRerun,
	taskfailure.ReasonAgentProcessFailure:              RemediationRerun,
	taskfailure.ReasonAgentEmptyOrUnparseableOutput:    RemediationRerun,

	taskfailure.ReasonAgentProviderAuthOrAccess:       RemediationRuntimeSettings,
	taskfailure.ReasonAgentProviderQuotaLimit:         RemediationRuntimeSettings,
	taskfailure.ReasonAgentMissingConfig:              RemediationRuntimeSettings,
	taskfailure.ReasonAgentModelNotFoundOrUnavailable: RemediationRuntimeSettings,

	taskfailure.ReasonAgentRuntimeMissingExecutable:  RemediationInstallCLI,
	taskfailure.ReasonAgentRuntimeVersionUnsupported: RemediationInstallCLI,
}

// errorRemediationInput builds the input map carried by an error task_message:
// the classified failure reason, plus the remediation key when one applies.
//
// The reason is always present so the client can label the error with the same
// vocabulary as the task's stored failure_reason — the classifier is the
// single source of truth for both, so the transcript and the run header can
// never disagree. remediation is omitted rather than emitted empty, so an
// absent key and an unhelpful one are the same thing on the wire.
//
// Returns nil for empty content: an error with no text cannot be classified
// into anything but the catch-all, and a bare `agent_error.unknown` tells a
// reader nothing they cannot already see.
func errorRemediationInput(content string) map[string]any {
	if content == "" {
		return nil
	}
	reason := taskfailure.Classify(content)
	input := map[string]any{"reason": reason.String()}
	if key, ok := remediationForReason[reason]; ok {
		input["remediation"] = key
	}
	return input
}
