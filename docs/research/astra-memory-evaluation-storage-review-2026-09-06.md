# Saved memory evaluations — 6 September 2026

## Scope

Extends the existing offline comparison CLI with human-manager report retention,
shared web/desktop inspection, and atomic evaluated adoption. Imported reports are
human-supplied evidence, not server-attested executions. No provider calls, new
runtime, dependency, automatic promotion, commit or deployment.

The server recomputes eligibility, checks frozen memory versions, caps imports at
2 MiB and ten reports per memory, and deduplicates canonical reports. The existing
memory writer lock protects both the active baseline and the pending candidate
during adoption. The resulting revision and adoption receipt commit together.
Deleting a referenced memory removes reports containing its text, including baseline
copies. Agent and workspace sweeps explicitly clean reports without foreign keys.

The shared Memory tab supports JSON import, paired outputs and checks, human review,
adoption, export and removal. A revision conflict preserves evidence and displays
an error outside the scrolling content. No native mobile comparison UI was added.

## Change set

- `server/internal/handler/agent_memory_evaluation.go` and its tests: new report API,
  validation, permissions, version matching, adoption check.
- `server/internal/handler/agent_memory.go`: optional `evaluation_id` on the existing
  update request, atomic validation and receipt. Other memory changes predate this lot.
- `server/cmd/server/router.go`: four evaluation routes only.
- `server/cmd/multica/cmd_agent_memory.go` and tests: `publish`, adoption publishes
  first and replaces the prior client-side baseline preflight with server validation.
  Offline execution and restore are existing work.
- Migrations 492–495, `agent_memory_evaluation.sql`, cleanup CTEs in
  `agent_memory.sql` and `workspace_delete.sql`, corresponding sqlc output/model.
- `packages/core/{agents/memory.ts,api/client.ts,api/schemas.ts,types/agent.ts,types/index.ts}`:
  report types, schema validation, API methods, scoped queries/mutations and adoption field.
- `packages/core/agents/memory-evaluations.test.ts`.
- `packages/views/agents/components/tabs/memory-evaluations-dialog.tsx`, its tests,
  and the MemoryTab entry point.
- Four `agents.json` locale sections, four agents user guides,
  `docs/development/memory-evaluation.md`, creating-agents built-in skill/source map.

The worktree includes substantial previously reviewed changes. They are retained;
the new review is limited to this change set and its existing integration points.

## Verification

- Race-enabled Go tests: report permissions (human manager, machine, non-manager,
  wrong workspace/agent), canonical import, malformed evidence, candidate/baseline
  mismatch, concurrent adoption, baseline mutation, restore freshness, deletion,
  CLI adoption and built-in skill conformance passed. Existing workspace/carrier
  deletion checks also passed; those older fixtures do not seed evaluation reports.
- 17 targeted core/views tests passed, including malformed response handling,
  human-review wiring, retry, old revisions and explicit removal.
- Core and views ESLint passed; views reports 27 existing warnings outside the new
  comparison files. Four-locale evaluation key parity checked.
- SQL generation compared against `/tmp/vigil-evaluations-generated-before`:
  only the new query file/model and the two intended cleanup queries changed.
- Chromium uses the actual shared component and CSS/fonts with a simulated HTTP
  API. This is not a browser-to-live-backend E2E test; PostgreSQL handlers are tested
  separately. Screenshots and harness: `/tmp/vigil-evaluations/`.

- Server and CLI build passed with `GOMAXPROCS=2 go build -p 1`.
- Migrations 492–495 passed down/up individually on the isolated
  `multica_memory_review_test_20260904` database; the migration ledger was not
  rewritten. Existing manually applied schema 486–491 was preserved.
- Final Chromium pass: 390 px document/viewport, 358 px dialog, Inter loaded, no
  page errors; import, deletion and both rejected/successful adoption exercised.
  Export was downloaded and compared to the full fixture report, with no field loss.
  The conflict alert is asserted in the viewport. Desktop, phone, evidence, conflict
  and empty screenshots were inspected.
- Final `pnpm typecheck --concurrency=1` passed: 9 tasks, 5 cached; native mobile
  is excluded by the repository script. `git diff --check` passed.
- Fresh read-only reviewer `/root/memory_evaluation_storage_review` returned **ship**.
  Static trace found no bypass of manager/tenant checks, snapshot verification or
  atomic adoption. Tests were not repeated by the reviewer. All 41 scoped file
  hashes match `/tmp/vigil-evaluations/review-files.json` after review.
- Parent accepts this tranche after inspecting the scoped changes, running the
  checks above and receiving the fresh ship verdict. The overall campaign remains open.

## Limits

The configured offline worker is not the complete Multica production runtime.
Reports remain removable by a manager even after adoption; this is not immutable
audit retention. Real provider efficacy, cost, human effort, server-attested execution and native
mobile parity remain open. These results do not prove uniqueness or willingness to pay.

## Routing and cost

Parent model/effort: unobservable. Requested fresh reviewer: `gpt-5.6-terra / high`,
read-only by instruction, one worker. Runtime realization and token usage are not
exposed by native metadata; requested values are not execution confirmation.

API-EQUIVALENT COST RECEIPT: unavailable — native tools expose no observed token
usage for the parent or reviewer. No USD estimate or delegation savings claimed.
Graft's text-retrieval estimate is separate from model usage and billing.

Parent graft discovery estimate: 107,463 tokens across four successful calls; one
additional symbol lookup failed. This is not observed model usage. The reviewer
reported only global historical RTK savings, which are excluded from this tally.
