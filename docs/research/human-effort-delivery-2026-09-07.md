# Human effort seconds on delivery review — 7 septembre 2026

## Product

Optional `human_effort_seconds` on `POST /api/issues/:id/delivery/reviews`:

- Client-timed while the Accept / request-changes form is open (web/desktop)
- Stored on `issue_delivery_review` (migration **501**)
- Returned on review payloads as `human_effort_seconds`
- **Excluded** from the review idempotency `input_hash` (timer ±1s must not conflict)
- Distinct from `review_delay_seconds` (wall clock run-complete → review-created)

UI copy (en/zh/ja/ko) states the self-timer honesty.

## Limits

- Not a stopwatch of cognitive load; tab left open inflates the number
- Mobile delivery now sends the same self-timer and displays the stored value (parity with web/desktop)
- Does not by itself prove commercial review-time reduction

## Verification

- Go: `TestIssueDeliveryHumanEffortSeconds`
- Core: `issue-delivery.test.ts` parses `human_effort_seconds: 18`

## Procedure comparison

See [human-effort-procedure-2026-09-07.md](human-effort-procedure-2026-09-07.md).
