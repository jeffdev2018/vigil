# Runtime adapter and pilot measurement — 6 September 2026

## Scope

The offline memory evaluator can now execute Multica’s shared Claude adapter, run-only prompt builder and runtime brief writer. The worker uses an explicit executable inside the existing isolated Docker container. The independent verifier receives only the final artifact. Reports retain requested model/effort, executable/prompt/brief fingerprints, tool-use events and adapter-reported usage; paired configuration mismatches and malformed envelopes prevent adoption. Server import validates and retains observations; web/desktop can inspect and export them.

The local `agent memory pilot-summary` command joins optional human reviews to the exact report bytes. It reports independent check counts, regressions, review coverage, explicit acceptance/effort and sourced human-recorded cost per accepted output. Missing measurements remain null. Human review files neither change server eligibility nor authorize adoption.

This is not the full daemon scheduler, issue/MCP integration, skill provisioning, session resumption or a provider-connected pilot. Frozen context is supplied by fixtures rather than copied from a live agent automatically. Only the Claude protocol family is covered. Requested model/effort and adapter usage keys are not provider identity attestation. The fake executable performs no inference and establishes no product-quality improvement. The fully UI-launched evaluation workflow and native mobile remain open.

## Change boundary

The worktree contains substantial earlier work. The scoped delta is `/tmp/vigil-runtime-eval-review.diff`, with 23 files listed in `/tmp/vigil-runtime-eval-files.json`. Before copies are in `/tmp/vigil-runtime-eval-before/`. The only added portion of `agent_memory_evaluation_test.go` is `TestAgentMemoryRuntimeEvaluation`. No migration, new dependency, production configuration, commit or deployment belongs to this tranche.

## Parent verification

- Native Go tests with race detector and serialized execution passed for the runtime worker, pilot summary (including CLI parsing), runtime gate and existing adoption contracts.
- A Linux arm64 CLI was built and packaged with the checked-in deterministic fake Claude executable using an already-installed Alpine base, `--network=none --pull=false`. Image: `sha256:bf5899e474431cbd385229827c4b398a6e7622b222a75a2472225ebd2da3d003`. No provider, network or host credential was available to the evaluation worker.
- `TestDockerMemoryRuntime` passed against that image: baseline/candidate runs, independent verifier isolation, parsed observations and malformed-envelope rejection. The image validates the runtime-worker path; subsequent local pilot-summary-only changes are covered by native tests.
- Server import/retention and existing evaluation tests passed with race detector against the dedicated `multica_memory_review_test_20260904` database. Built-in skill conformity passed. No migration ledger was changed.
- Core API parsing: 1 test passed. Shared views: 13 tests passed, covering comparison/adoption/history behavior and runtime observation labels.
- Global typecheck passed: 9/9 tasks, 5 cached, concurrency 1; native mobile excluded by the root script.
- Chromium rendered the actual shared component with real CSS/fonts and a mocked HTTP API. At 390 px, document width is 390 and dialog width is 358; no page errors. Import, exact JSON export, 409 adoption conflict then success, explicit deletion and empty state passed. Inspected captures: `/tmp/vigil-runtime-evaluations/runtime.png`, `phone.png`, `empty.png`; harness: `/tmp/vigil-runtime-evaluations/check.cjs`. This is not a live-backend browser E2E test.
- Scoped core/views lint, final native CLI build and `git diff --check` passed. The parent inspected the complete scoped delta and all newly added source/test files before review.

## Independent review

First fresh review `/root/memory_runtime_review`: **fix-first**. The reviewer identified an accepted-but-unreadable import: Go accepted uppercase hexadecimal runtime fingerprints while the frontend schema required lowercase. The shared runtime validator now requires canonical lowercase hex; the server import test rejects uppercase executable, prompt and brief fingerprints. Correction verification passed with serialized race-enabled tests for server import (all three uppercase fingerprints rejected), runtime worker, pilot CLI/summary and runtime gate. The parent inspected the correction, reran the native CLI build and diff check successfully, then dispatched fresh review `/root/memory_runtime_review_final` (Terra/high requested). **ship**, no findings. The fresh reviewer inspected all 23 files and relevant runtime/import/adapter/pilot paths, and verified every file against the manifest. No tests or provider commands were rerun by the reviewer. The parent verified the same 23 files remained unchanged after review.

Requested model/effort: `gpt-5.6-terra` / `high`; actual settings and token usage are unobservable in public metadata. Reviewer verified all scoped file hashes, inspected the complete delta and traced import to UI parsing, without running tests or changing files. Review is instructed read-only; no enforced read-only sandbox is claimed.

## Cost evidence

API-EQUIVALENT COST RECEIPT: unavailable. Native tools expose no observed token usage for the parent or reviewer; requested routing is not observed model/effort telemetry. No USD estimate, token savings from delegation or subscription-credit saving is asserted. Fixture token values are synthetic test data and are never used as task usage.
