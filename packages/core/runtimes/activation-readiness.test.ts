// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  blockedRequiredStepIds,
  deriveActivationReadiness,
  minutesSinceIso,
} from "./activation-readiness";

const onlineClaude = {
  id: "rt-1",
  status: "online",
  provider: "claude",
  metadata: {
    cli_auth: { authenticated: true, checked_at: "2026-09-06T00:00:00Z" },
  },
};

describe("deriveActivationReadiness", () => {
  it("blocks until a machine is online and Mika is kicked off", () => {
    const result = deriveActivationReadiness({
      runtimes: [],
      machines: [],
      hasWorkspaceRepos: false,
      hasGithubInstallation: false,
      memberNeedsMikaSetup: true,
    });
    expect(result.readyForFirstResult).toBe(false);
    expect(result.blockedRequiredCount).toBeGreaterThan(0);
    expect(result.steps.find((s) => s.id === "machine_online")?.status).toBe(
      "blocked",
    );
    expect(result.steps.find((s) => s.id === "first_agent_ready")?.status).toBe(
      "blocked",
    );
    expect(result.steps.find((s) => s.id === "repo_linked")?.status).toBe(
      "recommended",
    );
    expect(blockedRequiredStepIds(result)).toEqual(
      expect.arrayContaining(["machine_online", "cli_present", "first_agent_ready"]),
    );
    expect(blockedRequiredStepIds(result)).not.toContain("repo_linked");
  });

  it("treats CLI auth as not applicable when no auth-capable provider is online", () => {
    const result = deriveActivationReadiness({
      runtimes: [{ id: "rt-2", status: "online", provider: "opencode" }],
      hasWorkspaceRepos: true,
      hasGithubInstallation: false,
      memberNeedsMikaSetup: false,
    });
    expect(result.steps.find((s) => s.id === "cli_authenticated")?.status).toBe(
      "not_applicable",
    );
    expect(result.readyForFirstResult).toBe(true);
  });

  it("requires CLI auth when an online Claude/Codex runtime is unauthenticated", () => {
    const result = deriveActivationReadiness({
      runtimes: [
        {
          id: "rt-3",
          status: "online",
          provider: "codex",
          metadata: { cli_auth: { authenticated: false } },
        },
      ],
      hasWorkspaceRepos: true,
      hasGithubInstallation: false,
      memberNeedsMikaSetup: false,
    });
    expect(result.steps.find((s) => s.id === "cli_authenticated")?.status).toBe(
      "blocked",
    );
    expect(result.readyForFirstResult).toBe(false);
  });

  it("is ready when runtime, auth, repo, and Mika kickoff are all set", () => {
    const result = deriveActivationReadiness({
      runtimes: [onlineClaude],
      machines: [{ mode: "local", cliVersion: "0.4.0" }],
      hasWorkspaceRepos: false,
      hasGithubInstallation: true,
      memberNeedsMikaSetup: false,
    });
    expect(result.readyForFirstResult).toBe(true);
    expect(result.blockedRequiredCount).toBe(0);
    expect(result.steps.map((s) => [s.id, s.status])).toEqual([
      ["machine_online", "ready"],
      ["cli_present", "ready"],
      ["cli_authenticated", "ready"],
      ["repo_linked", "ready"],
      ["first_agent_ready", "ready"],
    ]);
  });

  it("marks unknown CLI auth as blocking until checked", () => {
    const result = deriveActivationReadiness({
      runtimes: [{ id: "rt-4", status: "online", provider: "claude" }],
      hasWorkspaceRepos: true,
      hasGithubInstallation: false,
      memberNeedsMikaSetup: false,
    });
    expect(result.steps.find((s) => s.id === "cli_authenticated")?.status).toBe(
      "unknown",
    );
    expect(result.readyForFirstResult).toBe(false);
  });
});

describe("minutesSinceIso", () => {
  it("returns whole minutes and refuses missing or bad stamps", () => {
    const now = Date.parse("2026-09-07T01:10:00Z");
    expect(minutesSinceIso("2026-09-07T01:00:00Z", now)).toBe(10);
    expect(minutesSinceIso(null, now)).toBeNull();
    expect(minutesSinceIso("not-a-date", now)).toBeNull();
  });
});
