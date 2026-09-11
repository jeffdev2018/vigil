---
name: multica-finish-first
description: "Use when coordinating multiple sub-tasks, @-mentions, or agent hops: finish and validate the current Multica issue (or plan step) before opening the next fan-out."
user-invocable: false
allowed-tools: Bash(multica *), Bash(git *), Bash(gh *)
---

# Finish first

Close or clearly validate the work you are on **before** starting parallel
fan-out. This is a soft protocol (Mythos / Superpowers-inspired). Multica does
not hard-block `@` mentions or agent-messages yet — you enforce it yourself.

## Rules

1. **One active outcome.** Keep a single in-progress outcome on the current
   issue (or the current plan step / stage). Do not open a second
   implementation thread until the first has a terminal Multica status, an
   accepted delivery, or an explicit human “pause / change scope” decision.
2. **No speculative @ hops.** Do not `multica issue ask-agent` / mention another
   agent to “start in parallel” while this issue is still mid-flight, unless the
   handoff note or the human asked for a review/critic/verification run.
3. **Plan Gate stays human.** If you published structured plan steps, wait for
   Plan Gate (or Trust Dial auto-approve). Do not create those sub-issues
   yourself — see `multica-plan-verification`.
4. **Stages already serialize.** Parent assignees wake when a **whole stage**
   finishes. Prefer that barrier over inventing your own parallel swarm.
5. **Write the plan in the repo** when the project uses Git workdirs — see
   `references/workspine.md`. Chat-only plans rot.

## Done enough to fan out

Any one of:

- Issue status in a workspace terminal category the brief allows, **or**
- Delivery review accepted / correction loop closed, **or**
- Human decision answered that explicitly unlocks the next step, **or**
- Verification run reported (`multica issue plan report`) for a plan-gated
  delivery.

If none hold, comment the blocker and end the turn.

## Anti-patterns

- Opening N sub-issues “so agents can work in parallel” before Plan Gate.
- @-mentioning a second agent while the first run on this issue is still
  `running`.
- Starting a new feature branch for step 2 while step 1 has no test and no
  Multica evidence.

## Related

- Platform contracts: `multica-platform` → `references/issues.md`,
  `references/mentions.md`, `references/racing.md`.
- Plan publish / verify: `multica-plan-verification`.
- Repo file convention: `references/workspine.md` (this skill).
