# plan-verification source map

Evidence layer for `SKILL.md`. Every contract the skill states is traced to a
current `file:line` here (F17).

## Plan artifact

| Behavior | File:line | Drifted from |
|---|---|---|
| `multica issue plan set/get/report` commands | `server/cmd/multica/cmd_issue_plan.go` | new citation |
| `PUT /api/issues/{id}/plan` creates the next version and supersedes the previous active one | `server/internal/handler/issue_plan.go` (`SetIssuePlan`); `server/pkg/db/queries/plan.sql` (`CreateIssuePlan`, `SupersedeOtherIssuePlans`) | new citation |
| Versions unique per issue | `server/migrations/464_issue_plan_issue_version_key.up.sql` | new citation |

## Verification run

| Behavior | File:line | Drifted from |
|---|---|---|
| A completed run on an issue with an active plan queues one verification run when the workspace setting `plan_verification_gate` is on; a verification run never spawns another | `server/internal/handler/plan_verification.go` (`maybeEnqueuePlanVerification`); `plan.sql` (`PlanVerificationExistsForSource`) | new citation |
| The verification run carries the plan in its handoff note, starting with `Plan verification` | `server/internal/handler/plan_verification.go` (`planVerificationHandoffNote`) | new citation |
| `POST /api/issues/{id}/plan/verifications/{runId}` stores findings once; a repeat is a no-op 200 | `server/internal/handler/issue_plan.go` (`ReportPlanVerification`); `plan.sql` (`ReportPlanVerification`) | new citation |
| Report doubled as a `type='system'` comment and `issue:updated` | `server/internal/handler/issue_plan.go` (`ReportPlanVerification`) | new citation |
| `done`-category refused with 409 `plan_verification_critical` while the latest report on the active plan has a critical finding | `server/internal/handler/plan_verification.go` (`planVerificationBlocksDone`); called from `UpdateIssue` / `BatchUpdateIssues` next to the sub-issue guard | new citation |

## Plan Gate (K11)

| Behavior | File:line | Drifted from |
|---|---|---|
| Steps carry `after`, `assignee_type`/`assignee_id` and, once approved, `issue_id`; a cycle or unknown `after` id is a 400 at `PUT /api/issues/{id}/plan` | `server/internal/handler/issue_plan.go` (`IssuePlanStep`, `SetIssuePlan`); `server/internal/handler/plan_gate.go` (`normalizePlanSteps`, `planStages`) | new citation |
| A plan with steps published by a task token files a Decision Card (`plan_version` set); answering `approve` materializes it, `revise` or free text resumes the run without creating anything | `server/internal/handler/plan_gate.go` (`askPlanApproval`); `server/internal/handler/decision.go` (`RespondIssueDecision`) | new citation |
| `POST /api/issues/{id}/plan/{version}/materialize` (human-only, `multica issue plan approve`) creates one child per step, stage from `after`, stage 1 in `todo`, later stages in `backlog`, blocking dependencies, then writes `materialized_at`; a second approval is 409 `already_materialized`, a superseded version 409 `plan_superseded` | `server/internal/handler/plan_gate.go` (`materializePlan`, `MaterializeIssuePlan`); `server/pkg/db/queries/plan.sql` (`ClaimIssuePlanMaterialization`) | new citation |
