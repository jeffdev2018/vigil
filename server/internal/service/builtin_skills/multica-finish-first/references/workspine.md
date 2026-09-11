# Workspine-lite — plans and proofs in the repo

Steal the *convention*, not the Workspine framework: durable plans and check
results live as files in the workdir / Git tree, not only in chat.

## Layout

Prefer one of these (pick what the project already uses; do not invent a third):

```text
.multica/plans/<issue-identifier>.md
docs/agent-plan/<issue-identifier>.md
```

Example: issue `DEV-30` → `.multica/plans/DEV-30.md`.

## What to write

```markdown
# DEV-30 — <short title>

## Goal
…

## Steps
1. …
2. …

## Proofs
- [ ] `make test` / relevant command
- [ ] Linked PR or branch
- [ ] Multica plan version (if published): vN

## Status
draft | waiting-gate | in-progress | verifying | done
```

## Protocol

1. **Before code**, write or update the file (create `.multica/plans/` if needed).
2. Publish the Multica plan when the issue uses Plan Gate:
   `multica issue plan set <id> --file .multica/plans/<id>.md --steps-json '…'`.
3. In the issue comment that ends a meaningful turn, **cite the path** (and
   Multica plan version if any). Do not paste the whole file unless asked.
4. On verification runs, diff the file + `multica issue plan get` against what
   landed; report with `multica issue plan report`.

## Non-goals

- Do not fork or install Workspine.
- Do not treat the markdown file as a substitute for Multica delivery review
  or Plan Gate approval.
- Do not commit secrets into `.multica/` or `docs/agent-plan/`.
