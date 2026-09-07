// @vitest-environment node
/**
 * Derives a readable "ready for first useful result" checklist from signals
 * the product already has: online runtime, discovered CLI, provider auth,
 * linked repos, and Mika kickoff. Pure — no I/O — so UI and tests share one
 * definition without rebuilding onboarding.
 */

import { readRuntimeCliAuthState } from "./cli-auth";

export type ActivationStepId =
  | "machine_online"
  | "cli_present"
  | "cli_authenticated"
  | "repo_linked"
  | "first_agent_ready";

/** Required steps must be ready before first-result is considered unblocked. */
export type ActivationStepKind = "required" | "recommended";

export type ActivationStepStatus =
  | "ready"
  | "blocked"
  | "recommended"
  | "unknown"
  | "not_applicable";

export interface ActivationStep {
  id: ActivationStepId;
  kind: ActivationStepKind;
  status: ActivationStepStatus;
}

export interface ActivationRuntimeInput {
  id: string;
  status: string;
  provider: string;
  metadata?: Record<string, unknown>;
}

export interface ActivationMachineInput {
  /** Local machines may report a Multica CLI version even before a runtime row is online. */
  mode: string;
  cliVersion?: string | null;
}

export interface ActivationReadinessInput {
  runtimes: readonly ActivationRuntimeInput[];
  machines?: readonly ActivationMachineInput[];
  hasWorkspaceRepos: boolean;
  hasGithubInstallation: boolean;
  /** True when this member still needs Start with Mika / kickoff. */
  memberNeedsMikaSetup: boolean;
}

export interface ActivationReadiness {
  steps: ActivationStep[];
  /** All required steps are ready (or not applicable). */
  readyForFirstResult: boolean;
  blockedRequiredCount: number;
}

const AUTH_CAPABLE_PROVIDERS = new Set(["claude", "codex"]);

function isOnline(runtime: ActivationRuntimeInput): boolean {
  return runtime.status === "online";
}

function cliAuthStatus(
  runtimes: readonly ActivationRuntimeInput[],
): ActivationStepStatus {
  const capable = runtimes.filter(
    (runtime) =>
      isOnline(runtime) && AUTH_CAPABLE_PROVIDERS.has(runtime.provider),
  );
  if (capable.length === 0) return "not_applicable";

  let sawUnknown = false;
  let sawUnauthenticated = false;
  for (const runtime of capable) {
    const state = readRuntimeCliAuthState(runtime.metadata);
    if (state?.authenticated === true) return "ready";
    if (state?.authenticated === false) sawUnauthenticated = true;
    else sawUnknown = true;
  }
  if (sawUnauthenticated) return "blocked";
  if (sawUnknown) return "unknown";
  return "blocked";
}

/**
 * Build the activation checklist. Order matches the path a new member walks:
 * machine → CLI → auth → repo → first agent conversation.
 */
export function deriveActivationReadiness(
  input: ActivationReadinessInput,
): ActivationReadiness {
  const online = input.runtimes.filter(isOnline);
  const anyRuntime = input.runtimes.length > 0;
  const localCliPresent = (input.machines ?? []).some(
    (machine) =>
      machine.mode === "local" &&
      typeof machine.cliVersion === "string" &&
      machine.cliVersion.trim().length > 0,
  );

  const machineStatus: ActivationStepStatus =
    online.length > 0 ? "ready" : "blocked";
  const cliStatus: ActivationStepStatus =
    online.length > 0 || (anyRuntime && localCliPresent)
      ? "ready"
      : anyRuntime
        ? "blocked"
        : localCliPresent
          ? "ready"
          : "blocked";
  const authStatus = cliAuthStatus(input.runtimes);
  const repoReady = input.hasWorkspaceRepos || input.hasGithubInstallation;
  const agentStatus: ActivationStepStatus = input.memberNeedsMikaSetup
    ? "blocked"
    : "ready";

  const steps: ActivationStep[] = [
    { id: "machine_online", kind: "required", status: machineStatus },
    { id: "cli_present", kind: "required", status: cliStatus },
    {
      id: "cli_authenticated",
      kind: "required",
      status: authStatus,
    },
    {
      id: "repo_linked",
      kind: "recommended",
      status: repoReady ? "ready" : "recommended",
    },
    { id: "first_agent_ready", kind: "required", status: agentStatus },
  ];

  const blockedRequiredCount = steps.filter(
    (step) =>
      step.kind === "required" &&
      (step.status === "blocked" || step.status === "unknown"),
  ).length;

  const readyForFirstResult = steps.every(
    (step) =>
      step.kind === "recommended" ||
      step.status === "ready" ||
      step.status === "not_applicable",
  );

  return { steps, readyForFirstResult, blockedRequiredCount };
}

/** Required steps that still block a first useful result. */
export function blockedRequiredStepIds(
  readiness: ActivationReadiness,
): ActivationStepId[] {
  return readiness.steps
    .filter(
      (step) =>
        step.kind === "required" &&
        (step.status === "blocked" || step.status === "unknown"),
    )
    .map((step) => step.id);
}

/**
 * Whole minutes since an ISO timestamp. Null when the stamp is missing or
 * unparseable — callers must not invent a zero delay.
 */
export function minutesSinceIso(
  iso: string | null | undefined,
  nowMs = Date.now(),
): number | null {
  if (typeof iso !== "string" || iso.trim().length === 0) return null;
  const parsed = Date.parse(iso);
  if (!Number.isFinite(parsed)) return null;
  return Math.max(0, Math.floor((nowMs - parsed) / 60_000));
}
