-- Approval gates (K05): an action a run wanted to take (git push, a
-- sensitive MCP tool call, a spend) paused behind a Decision Card until a
-- human decides; the spend variant also carries the short-lived token.
CREATE TABLE approval_gate_event (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL,
    task_id             UUID NOT NULL,
    issue_id            UUID,
    gate_type           TEXT NOT NULL CHECK (gate_type IN ('git_push', 'mcp_tool_call', 'spend')),
    decision_request_id UUID,
    summary             TEXT NOT NULL DEFAULT '',
    details             JSONB NOT NULL DEFAULT '{}'::jsonb,
    resolved_action     TEXT CHECK (resolved_action IN ('approved', 'denied', 'modified')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at          TIMESTAMPTZ,
    resolved_at         TIMESTAMPTZ
);
