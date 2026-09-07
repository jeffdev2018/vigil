# Comparing a memory before adoption

`multica agent memory evaluate` runs a human-owned offline suite against a pending
agent memory. Each case runs twice: with the frozen active memories, then with the
candidate added. A separate container grades each output. No production run or
provider is invoked. This CLI workflow complements the existing memory review UI.

```bash
multica agent memory evaluate AGENT_ID MEMORY_ID --suite suite.json --output comparison-001
multica agent memory publish comparison-001/report.json
multica agent memory adopt comparison-001/report.json --reviewed
# Restore the pre-adoption revision recorded in report.json; use the current revision:
multica agent memory restore AGENT_ID MEMORY_ID --revision 3 --expected-revision 4
```

Evaluation only reads the API and retains local evidence. `publish` saves a report to
the workspace; the human manager can also import its JSON in **Agent → Memory →
Evaluations**, inspect paired cases and outputs, export or remove a report, and adopt
the compared revision after explicit review. `adopt` publishes first and then uses
the saved evaluation ID with the existing human-only memory update endpoint.
Identical imports return the same receipt. Retain the local directory as well: fixture
files are not uploaded, only the report containing fingerprints and outputs.
A network failure after adoption can leave the caller uncertain; inspect the memory
and its version history before retrying. Adoption is intentionally not automatic.

## Suite contract

Provide a trusted, locally installed image containing `/bin/sh`, `cp`, and your
offline worker dependencies. Resolve its immutable ID with
`docker image inspect IMAGE --format '{{.Id}}'` and place that full `sha256:…` value
in `image`. No image is pulled by this command. Do not bake credentials into the image.

```json
{
  "image": "sha256:REPLACE_WITH_FULL_LOCAL_IMAGE_ID",
  "worker": ["/bin/sh", "worker.sh"],
  "verifier": ["/bin/sh", "/checks/check.sh"],
  "timeout_seconds": 30,
  "cases": [
    {"id": "corrected-case", "split": "replay", "input": "cases/corrected", "checks": "checks/corrected"},
    {"id": "unseen-case", "split": "holdout", "input": "cases/unseen", "checks": "checks/unseen"}
  ]
}
```

Paths are relative to the suite JSON. Input directories are explicitly prepared
fixtures, not the current repository or the agent's home directory. Only regular
files and directories are copied; symlinks and special files are rejected. All
inputs and checks are copied and fingerprinted before the first execution. Duplicate
input fingerprints are rejected, even across differently named cases. Choose holdout
tasks before editing the candidate and keep them out of its correction examples.
Different bytes alone do not prove that cases are independent or representative.

The worker starts in a writable `/work` copied from that case's `/input`. It reads
`/memory.json`, a JSON array of memory contents, and writes its result to stdout.
It can return text or a patch. Logs belong on stderr. The verifier starts in a fresh
`/work` copied from the original input, reads the worker's stdout at `/answer`, and
runs the human-owned checks at `/checks`. It never receives `/memory.json`. The worker
never receives `/checks`. For coding cases the verifier must apply the returned patch
and run its own independent tests; tests supplied by the worker are not evidence.

Worker exit 0 means it produced an artifact; every other exit is an execution error.
Verifier exit 0 means passed, **1 means an assertion failed**, and every other exit
means an execution error. A verifier that catches every failure and exits 1 can
misclassify broken infrastructure as a baseline failure; review the checks accordingly.

The runtime uses separate Docker containers, no network, read-only root and bind
mounts, an unprivileged UID, dropped capabilities, and no new privileges. It mounts
only explicit fixture snapshots and that stage's inputs. Each stage is limited to
one CPU, 512 MiB RAM without additional swap, 64 processes, and 1–300 seconds. Stages
run sequentially. Writable scratch is bounded; stdout/stderr are each capped at
64 KiB. There are 2–16 cases with at most 32 MiB per fixture directory. These controls
use [Docker's run options](https://docs.docker.com/reference/cli/docker/container/run/)
and [resource limits](https://docs.docker.com/engine/containers/resource_constraints/).
Docker must enforce them; this is not a hardened boundary against a compromised
Docker daemon, a hostile local account, or an untrusted container image.

## Reading the report

The report keeps the exact memory revision, baseline revisions/content, suite commands,
image ID, input/check hashes, each artifact, verifier diagnostics, status and elapsed
time. Completed reports qualify for human adoption only when every candidate case
passes and at least one baseline failure becomes a pass in **each** split. Baseline
infrastructure failures, timeouts and truncated output cannot manufacture improvements.
Partial or failed reports remain on disk and are not eligible. The command returns a
nonzero exit code when the gate does not pass.

Elapsed times include container startup and grading. Cost and human interventions
remain `null`: this runner does not observe provider usage or the human effort spent
preparing the candidate and checks. Null never means free or zero effort. A single
paired run is not a statistical estimate; repeat on fresh cases for a noisy worker.

The server recomputes the gate rather than trusting the imported `eligible` flag.
Imports validate the candidate and baseline against saved memory versions. Adoption
checks the pending candidate, expiry and complete active memory baseline while holding
the same agent lock as all memory writers, then atomically updates the memory and the
report's adopted-revision receipt. Concurrent context changes reject adoption. The
ordinary manual memory approval remains available separately.

Reports are human-supplied evidence, not server attestation. Human authorization
remains the trust boundary; do not adopt an untrusted or edited report. Reports can be
read, imported or removed only by human agent managers, including workspace admins.
Imports are limited to 2 MiB and 10 reports per memory; remove a report to make room.
Removing a report does not roll back the memory. Deleting any referenced memory also
removes reports containing its candidate or baseline text, and agent/workspace cleanup
removes their reports. This does not delete copies previously exported to disk.

API: `GET/POST /api/agents/{id}/memories/{memoryId}/evaluations`,
`GET/DELETE .../evaluations/{evaluationId}`. Lists return compact summaries, details
include the report. Evaluated adoption uses `PUT .../memories/{memoryId}` with
`status: "active"`, `expected_revision` and `evaluation_id`, without other changes.

This proves behavior of the configured offline worker on these fixtures. It does
not reproduce Multica's complete production prompt, project memory, tools, model
settings, or runtime. Connected text comparisons use the separate path below.
Measured provider cost, human effort, and server-attested execution remain future
work. The comparison UI is shared by web and desktop; native mobile is not
implemented in this tranche.

## Launching connected comparisons from web/desktop

On a pending memory, open **Evaluations → Run a connected comparison**. Bind the
agent to an online, updated Claude or Codex runtime and select an explicit model
in its agent settings. Starting requires both memory-manager access and runtime
owner/admin access. The UI displays the provider/model/effort and requests one
replay prompt and one distinct validation prompt, each with an expected answer.
The button starts four sequential runs. The API permits 2–8 cases. The server checks the selected criterion: exact text (outer whitespace ignored),
structured JSON, or JavaScript function tests. JSON ignores object key order but
retains array order, types and required fields. Numeric comparison is exact; values
longer than 128 characters or with exponents outside ±4096 use lexical equality. Expected
answers are never included in daemon jobs. Authorized human managers can inspect
and export them with the full report so they can review the grading criteria;
importing that report retains those checks. Machine credentials cannot read these
human report endpoints.

This is a connected **response artifact** comparison. It uses the actual CLI adapter,
run-only prompt and runtime brief with frozen agent instructions, workspace context
and active/candidate memories. It does not supply project files, custom agent env,
custom CLI args, agent skills or authenticated Multica MCP tools. The runtime's
connected account and local permissions remain in use; its inherited configuration
is not an OS security sandbox or a fully frozen image. Custom launch-prefix
profiles are rejected. Do not interpret this mode as a repository/code benchmark;
JavaScript function checks cover returned functions only; use the offline Docker
suite for repository patches, builds and broader executable fixtures.

Credentials stay on the runtime machine. Codex gets a disposable home using the
same authentication link/config-copy machinery as ordinary runs; its session and
managed MCP changes are local to the run. No credential is sent to the web client
or stored in evaluation reports. Provider-reported token usage remains distinct
from observed billing and identity attestation. A user launching a comparison may
incur provider charges. Tests in this repository use only fake executables.

The queue and results are durable in `agent_memory_evaluation`. A request UUID
recovers a launch after an uncertain HTTP failure. A runtime claims once and runs
one evaluation at a time per daemon; a crash or lost claim response never causes an
automatic paid replay. Each response has a 60-second deadline, the worker an
18-minute deadline, and the server request expires after 20 minutes. Duplicate,
identical result reports are acknowledged; changed or out-of-order reports fail.
The UI polls active comparisons every two seconds and retains partial results.
Cancellation prevents later runs; the current response may continue up to its
60-second bound (plus subprocess cleanup). Expired/failed/cancelled executions
cannot authorize adoption. A model/runtime/effort change after preview requires a
refresh before starting. Adoption still requires explicit human review and the
existing atomic memory-version/context check.

For JavaScript checks, the server operator sets `MULTICA_MEMORY_CODE_IMAGE` to
a trusted, already installed immutable Node image ID (`sha256:…`). No image is
pulled. Only then does the UI offer this check. Changing the configured image
invalidates the launch preview; each report freezes its own image. Ask the agent
for plain CommonJS code, for example `module.exports = (amount) => amount * 2`.
The expected answer contains 1–16 independent tests:

```json
[{"args":[2],"expected":4},{"args":[3],"expected":6}]
```

The server uses the existing restricted Docker runner, one CPU, 512 MiB, no network
and a 15-second deadline including container startup. Only code, a fixed runner
and argument vectors are mounted; expected outputs remain on the server. Syntax
errors and thrown exceptions fail tests; container failures, invalid output and
timeouts are execution errors and block adoption. Retained code observations bind
the artifact, checks and image; duplicate callbacks and report reads never execute
code again. Current daemons allow 30 seconds for the report callback, including
container cleanup. These are bounded function tests, not a security proof or a
whole-repository benchmark. Reports imported by humans remain unsigned evidence.

New server routes: `GET .../evaluations/runtime`, `POST .../evaluations/run`, and
`POST .../evaluations/{evaluationId}/cancel`. Daemons advertise
`memory-evaluation-v1`, receive a heartbeat hint, then use runtime-authenticated
`.../memory-evaluations/{evaluationId}/claim` and `/report`. A daemon token must
match the assigned runtime’s daemon ID; legacy user tokens require runtime
owner/admin access. No automatic promotion
or paid retry is added. Connected reports can be exported/imported and summarized
with the existing `pilot-summary` command; the report kind distinguishes them from
`offline_memory_comparison`.

## Executable integration check

### Multica runtime adapter and a measured pilot

Set `worker_protocol` to `multica_runtime_v1` and `worker` to
`["/usr/local/bin/multica", "agent", "memory", "runtime-worker", "/input/runtime.json"]`.
The pinned image must contain the Linux Multica binary and an explicit Claude-compatible
executable. Each input fixture contains `runtime.json`, for example:

```json
{
  "provider": "claude",
  "executable": "/usr/local/bin/fixture-agent",
  "model": "requested-fixture",
  "thinking_level": "low",
  "prompt": "Complete this case using the files in the working directory.",
  "agent_name": "Memory pilot",
  "agent_instructions": "Return a result the independent checks can inspect.",
  "workspace_context": "Frozen workspace instructions",
  "project_title": "Pilot",
  "project_description": "Frozen project context",
  "max_turns": 2,
  "timeout_seconds": 10
}
```

The adapter reuses Multica's run-only prompt builder, runtime brief writer and
Claude backend. It starts a fresh session in the worker container; only its memory
input varies between the two runs. It retains hashes of executable, prompt and
brief, requested model/effort, tool-use event count and adapter-reported usage.
The gate refuses a pair whose executable, prompt, requested model or effort differ.
The verifier receives the final artifact, not the structured telemetry envelope.
Runtime observations are inspectable beneath each outcome in web/desktop and survive
export/import. A malformed envelope or unsuccessful runtime cannot become a pass.

This is the shared runtime **adapter and run-only prompt path**, not the full daemon
scheduler, issue API, authenticated MCP tools, skill provisioning or session reuse.
The fixture supplies context explicitly; it is not automatically synchronized from
the live agent configuration. The image is part of the trust boundary. No network,
host credentials or provider accounts are mounted. Only Claude's protocol family is
supported by this first adapter contract. The checked-in fake executable is not an
LLM; passing its test says nothing about real model quality. Requested model/effort
are not observed provider identity; usage keys are those exposed by the adapter,
which may fall back to the requested model when upstream omits its identity.

For the pilot, freeze representative replay and holdout cases with independent
checks before changing the memory. Keep the image, configuration and fixtures fixed;
vary only the memory. Run `evaluate`, then `publish`; inspect paired evidence and
adopt through **Memory → Evaluations** only after human review. Use memory history
or `restore` to undo adoption. Existing manual approval remains a separate action.

`multica agent memory pilot-summary report.json` outputs check counts and a
`report_hash`. Unknown human effort, acceptance and cost remain `null`. Record actual
human review in a separate file and pass it with `--reviews reviews.json`:

```json
{
  "report_hash": "COPY_FROM_PILOT_SUMMARY",
  "reviewer": "human reviewer identifier",
  "runs": [
    {"case_id": "corrected-case", "variant": "baseline", "accepted": false,
     "human_seconds": 60, "cost_usd": null, "cost_source": ""},
    {"case_id": "corrected-case", "variant": "candidate", "accepted": true,
     "human_seconds": 45, "cost_usd": null, "cost_source": ""}
  ]
}
```

These numbers illustrate the format; replace them with actual observations. Record
each case and variant, including failures; allocate preparation and correction effort
consistently. `accepted` and `human_seconds` must be explicit. Acceptance requires
passing independent checks. Costs are human-recorded amounts with a required source
(for example a usage receipt), not automatic token-price estimates or attestation.
The summary reports coverage; totals remain unknown until every planned run has a
review, and costs remain unknown until every run has a sourced amount. Cost per
accepted output includes failed runs and is null when none is accepted. Changing
report bytes invalidates the human-review binding. This local pilot file does not
change report eligibility or authorize adoption.

The fixture container recipe lives in `server/cmd/multica/testdata/memory-runtime/`.
Copy a Linux/target-architecture `multica` binary beside its Dockerfile, build using
an already-installed Alpine-compatible `BASE` with `--network=none --pull=false`,
then select the immutable image ID. To exercise the real adapter with the fake CLI:

```bash
GOMAXPROCS=2 MULTICA_EVAL_RUNTIME_TEST_IMAGE=sha256:FULL_LOCAL_FIXTURE_IMAGE_ID \
  go test -p 1 -parallel 1 -race ./cmd/multica -run 'TestDockerMemoryRuntime|TestMemoryRuntimeWorker' -count=1
```

The Docker suite remains offline. Connected text comparisons use the separate
runtime-owned path described above; networking and account permissions are those
of the selected runtime. CLI/adapter internal retry behavior remains provider-specific
and bounded by the response timeout; the queue never replays a claimed job.

The checked-in test uses a deterministic shell worker to exercise isolation,
comparison, adoption gating, timeout and output limits. It is a plumbing test, not
evidence that an LLM became better. It never launches an installed agent CLI.

```bash
# From server/, after selecting an already-installed Alpine-compatible image:
GOMAXPROCS=2 MULTICA_EVAL_TEST_IMAGE=sha256:FULL_LOCAL_IMAGE_ID \
  go test -p 1 -parallel 1 -race ./internal/memoryeval -count=1 -v
GOMAXPROCS=2 go test -p 1 -parallel 1 ./cmd/multica -run TestMemoryAdoptionCLI -count=1
```
