# Bug-fix recipe (guided path)

Reuse Multica as it exists today: an issue with delivery criteria, an assignable
agent, a linked repo, execution log, PR evidence, and **Delivery review**.
This path does **not** invent a new orchestrator and does **not** require a
provider agent for the fixture proof.

## Stages

| Stage | What happens | Multica surface |
| --- | --- | --- |
| `reproduce` | Confirm the failing test or repro steps | Issue description + local fixture |
| `fix` | Change the code | Agent run on the issue (optional for fixture) |
| `test` | Re-run the failing check until green | Same workdir / CI on the PR |
| `open_pr` | Push a branch and open a PR that references the issue key | Runtime git credentials; link appears on the issue |
| `delivery_review` | Human Accept or Request changes with per-criterion evidence | Delivery review card |

Board `in_review` / `done` and delivery Accept remain **soft signals** — they do
not authorize merge or deploy. See [Security model — Review vs action authorization](../../apps/docs/content/docs/security-model.mdx#review-vs-action-authorization).

## Preparation controls

Shared pure helper: `deriveBugFixRecipeReadiness` in
`packages/core/issues/bugfix-recipe.ts`.

Required before starting a real Multica run:

1. Online runtime for the target agent  
2. CLI auth ready **or** not applicable for that provider  
3. Accessible linked repo  
4. Operator may invoke the agent (`private_agent_invoke` hard gate)  
5. At least one delivery criterion on the issue  
6. A human who can review delivery  

Unknown CLI auth counts as blocked. Passing readiness never starts a provider.

## Fixture proof (no provider)

```bash
node docs/development/bugfix-recipe-fixture/prove.mjs
```

Expected:

1. Reproduction fails (`add` subtracts)  
2. Fixed path passes (`FIX_APPLIED=1`)  
3. PR + delivery review stay on Multica for a real issue — not claimed by this script  

Unit tests for readiness: `packages/core/issues/bugfix-recipe.test.ts`.

## Suggested Multica issue body

```text
Title: Fixture: fix add() so 2+3 equals 5

Criteria:
- Reproduction: npm test fails on docs/development/bugfix-recipe-fixture
- Fix: add(2,3) returns 5
- Test: npm run test:fixed (or equivalent) passes
- PR: linked PR references this issue key

Repro:
cd docs/development/bugfix-recipe-fixture && npm test
```

Assign an agent only after readiness is green. After the PR lands evidence,
use **Delivery review** — not board status alone — to accept or request changes.
