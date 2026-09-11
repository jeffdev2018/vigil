# ADR-lite — intent-before-tool on the Multica MCP gateway

Date: 2026-09-11  
Source pattern: [uber/ADR](https://github.com/uber/ADR) (inspiration only — no Python dep).  
Kokpit: [kokpit-product-patterns-2026-09-11.md](kokpit-product-patterns-2026-09-11.md) §2.

## Hard gates (already shipped)

Before any MCP `tools/call` reaches the upstream, `server/internal/daemon/mcp_gateway.go`
does:

| Step | Behavior |
| --- | --- |
| `classify(tool, description)` | Risk (`read` / `internal_write` / `external_effect` / `sensitive_data` / `unknown`) + class (`act_alone` / `ask` / `never`) from binding, else `mcpgov.Classify` + trust ceiling |
| `ClassNever` | Immediate refuse (`-32004`), report `result=refused` |
| `ClassAsk` | `AskWithID(..., "mcp_tool_call", …)` with **secret-scanned** params + `gateParamPaths`; refuse if gate unreachable or not approved; report `result=gated` + `gate_id` |
| Secret substitution | Run-scoped tokens expanded **after** approval |
| Result redact | `secretscan` on upstream body → `flags+=secret_masked` |
| Report | `POST /api/tasks/{id}/mcp-calls` → audit `run.mcp_tool_call` + usage touch + high-risk inbox alert |

`tools/list` drops every tool whose effective class is `never`.

### Matrix (binding default × trust dial)

Effective class for an **unlisted** tool (no per-tool override):

| Binding `default` | Trust `observer`/`propose` | Trust `approval` | Trust `autonomous` |
| --- | --- | --- | --- |
| `never` | never | never | never |
| `ask` | ask (capped by ceiling) | ask | weaker(ask, ceiling) |
| `by_risk` | ClassForRisk(risk) ∩ ceiling | same | same |

Per-tool `class` in the claim catalog always wins over `default`.

Shell / ACP coding CLIs keep their own permission surfaces (`permissionprofile`,
provider deny rules). ADR-lite does **not** claim to gate those; only the
governed MCP broker path.

## Abuse / “intent-like” fixtures (Go)

Same tool **name**, hostile **arguments** — Multica does not invent an LLM
“intent” field. Prevention is class + path blast radius + secret redaction:

| Case | Expectation | Coverage |
| --- | --- | --- |
| Tool marked `never` | Refuse even with benign args | `TestMcpGatewayRefusesNeverTool` |
| Tool marked `ask`, human denies | Refuse | `TestMcpGatewayAsksTheGateAndAttributesIt` |
| Ask with secret-shaped args | Params stored/redacted via `secretscan.JSON` before gate | `serveToolCall` + `TestMcpGatewayAbuseTable` |
| Path-like args (`../../.ssh/id_rsa`, `/etc/passwd`) | Lifted by `gateParamPaths` for blast-radius / permission profile on the **server** Ask path | `TestGateParamPathsAbuse` |
| Success body contains `sk-…` | Masked before model sees it | `TestMcpGatewayActAloneMasksSecretsInResult` |
| Unsupported provider + declared never/ask | Server dropped, not passed ungoverned | `TestMcpGatewayLeavesUnsupportedProvider…` |

### Results (2026-09-11, live `go test`)

```bash
cd server && go test ./internal/daemon/ \
  -run 'TestMcpGatewayAbuseTable|TestGateParamPathsAbuse|TestMcpGatewayRefusesNeverTool|TestMcpGatewayAsksTheGate|TestMcpGatewayActAloneMasksSecrets' \
  -count=1 -v
```

**PASS** — all 4 abuse subcases + path fixtures + never/ask/mask suites green
against the real gateway code path (in-process fake MCP server; no Uber dep).

No Uber ADR ML detector. No new Python.

## Observability surface

| Layer | Status |
| --- | --- |
| Daemon log | `MCP gateway call` structured line |
| API | `POST …/mcp-calls` persists audit + optional owner alert; `GET …/mcp-calls` lists them for the run UI |
| Run UI list of calls | Execution log button + replay panel (`tool · class · result · gate_id`); replay kind `mcp_call` |

## Non-goals

- Import Uber ADR runtime or attack corpus as a dependency.
- Soft “intent_summary” from the last agent message (nice-to-have later).
- Hard-block Git merge from Multica.
