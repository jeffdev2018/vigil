// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "../types";
import {
  machineSkippedAgents,
  readRuntimeSkippedAgents,
} from "./skipped-agents";

function runtime(over: Partial<AgentRuntime>): AgentRuntime {
  return {
    id: "r1",
    workspace_id: "w1",
    daemon_id: "d1",
    name: "claude (Mac)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "claude",
    status: "online",
    device_info: "Mac",
    metadata: {},
    owner_id: "u1",
    visibility: "private",
    last_seen_at: "2026-01-01T00:00:00.000Z",
    created_at: "2026-01-01T00:00:00.000Z",
    updated_at: "2026-01-01T00:00:00.000Z",
    ...over,
  };
}

describe("readRuntimeSkippedAgents", () => {
  it("returns null when the runtime predates the field", () => {
    expect(readRuntimeSkippedAgents({ version: "2.1.0" })).toBeNull();
    expect(readRuntimeSkippedAgents(undefined)).toBeNull();
  });

  it("returns an empty list when the daemon reported nothing skipped", () => {
    expect(readRuntimeSkippedAgents({ skipped_agents: {} })).toEqual([]);
  });

  it("reads and sorts the reported providers", () => {
    expect(
      readRuntimeSkippedAgents({
        skipped_agents: {
          codex: "version could not be detected",
          claude: "claude 1.0.3 is below the minimum supported version 2.1.0",
        },
      }),
    ).toEqual([
      {
        provider: "claude",
        reason: "claude 1.0.3 is below the minimum supported version 2.1.0",
      },
      { provider: "codex", reason: "version could not be detected" },
    ]);
  });

  it("drops blank providers and blank reasons", () => {
    expect(
      readRuntimeSkippedAgents({
        skipped_agents: { claude: "   ", "  ": "orphan reason", codex: " ok " },
      }),
    ).toEqual([{ provider: "codex", reason: "ok" }]);
  });

  // Malformed response: a backend that ships a different shape must degrade to
  // "nothing to show", never crash the runtimes page.
  it("falls back to an empty list on a malformed map", () => {
    expect(readRuntimeSkippedAgents({ skipped_agents: { claude: 42 } })).toEqual(
      [],
    );
    expect(
      readRuntimeSkippedAgents({ skipped_agents: ["claude"] }),
    ).toBeNull();
    expect(readRuntimeSkippedAgents({ skipped_agents: "claude" })).toBeNull();
  });
});

describe("machineSkippedAgents", () => {
  it("returns [] when no runtime reported the field", () => {
    expect(machineSkippedAgents([runtime({ metadata: {} })])).toEqual([]);
  });

  it("takes the snapshot of the runtime that reported most recently", () => {
    const stale = runtime({
      id: "stale",
      last_seen_at: "2026-01-01T00:00:00.000Z",
      metadata: { skipped_agents: { claude: "below minimum 2.1.0" } },
    });
    const fresh = runtime({
      id: "fresh",
      last_seen_at: "2026-02-01T00:00:00.000Z",
      metadata: { skipped_agents: {} },
    });
    expect(machineSkippedAgents([stale, fresh])).toEqual([]);
    expect(machineSkippedAgents([fresh, stale])).toEqual([]);
  });

  it("ignores rows that never reported the field", () => {
    const legacy = runtime({
      id: "legacy",
      last_seen_at: "2026-03-01T00:00:00.000Z",
      metadata: { version: "2.1.0" },
    });
    const reporting = runtime({
      id: "reporting",
      last_seen_at: "2026-02-01T00:00:00.000Z",
      metadata: { skipped_agents: { codex: "not executable" } },
    });
    expect(machineSkippedAgents([legacy, reporting])).toEqual([
      { provider: "codex", reason: "not executable" },
    ]);
  });
});
