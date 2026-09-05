import { describe, expect, it } from "vitest";
import type { RuntimeDevice } from "@multica/core/types";
import {
  groupRuntimesByMachine,
  machineSkippedAgents,
  providerDisplayName,
  readRuntimeCliAuthState,
  readRuntimeSkippedAgents,
  runtimeCliVersion,
  runtimeDisplayName,
} from "./runtime-display";

function runtime(overrides: Partial<RuntimeDevice> = {}): RuntimeDevice {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Codex (host)",
    custom_name: null,
    runtime_mode: "local",
    provider: "codex",
    launch_header: "",
    status: "online",
    device_info: "host · 0.9.1",
    metadata: {},
    owner_id: null,
    visibility: "private",
    last_seen_at: "2026-01-01T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("providerDisplayName", () => {
  it("uses the daemon's overrides", () => {
    expect(providerDisplayName("traecli")).toBe("Trae");
    expect(providerDisplayName("dsh")).toBe("DeepSeek Harness");
  });

  it("capitalizes anything else", () => {
    expect(providerDisplayName("codex")).toBe("Codex");
    expect(providerDisplayName("")).toBe("");
  });
});

describe("runtimeDisplayName", () => {
  it("prefers a real alias over the daemon name", () => {
    expect(runtimeDisplayName({ name: "Codex (host)", custom_name: "Laptop" }))
      .toBe("Laptop");
  });

  it("ignores an absent or whitespace-only alias", () => {
    expect(runtimeDisplayName({ name: "Codex (host)", custom_name: null }))
      .toBe("Codex (host)");
    expect(runtimeDisplayName({ name: "Codex (host)", custom_name: "  " }))
      .toBe("Codex (host)");
  });
});

describe("runtimeCliVersion", () => {
  it("reads metadata.cli_version", () => {
    expect(runtimeCliVersion({ cli_version: "0.9.1" })).toBe("0.9.1");
  });

  it("is null when absent, blank, or not a string", () => {
    expect(runtimeCliVersion({})).toBeNull();
    expect(runtimeCliVersion({ cli_version: "   " })).toBeNull();
    expect(runtimeCliVersion({ cli_version: 3 })).toBeNull();
    expect(runtimeCliVersion(undefined)).toBeNull();
  });
});

describe("readRuntimeSkippedAgents", () => {
  it("distinguishes 'never reported' from 'nothing skipped'", () => {
    expect(readRuntimeSkippedAgents({})).toBeNull();
    expect(readRuntimeSkippedAgents({ skipped_agents: null })).toBeNull();
    expect(readRuntimeSkippedAgents({ skipped_agents: {} })).toEqual([]);
  });

  it("sorts by provider and drops empty pairs", () => {
    expect(
      readRuntimeSkippedAgents({
        skipped_agents: {
          zeroclaw: "version below minimum",
          antigravity: " version undetectable ",
          "  ": "orphan reason",
          qwen: "",
        },
      }),
    ).toEqual([
      { provider: "antigravity", reason: "version undetectable" },
      { provider: "zeroclaw", reason: "version below minimum" },
    ]);
  });

  it("refuses a non-object map", () => {
    expect(readRuntimeSkippedAgents({ skipped_agents: ["a"] })).toBeNull();
  });
});

describe("machineSkippedAgents", () => {
  it("takes the snapshot from the row that reported most recently", () => {
    const stale = runtime({
      id: "rt-old",
      last_seen_at: "2026-01-01T00:00:00Z",
      metadata: { skipped_agents: { qwen: "version below minimum" } },
    });
    const fresh = runtime({
      id: "rt-new",
      last_seen_at: "2026-02-01T00:00:00Z",
      metadata: { skipped_agents: {} },
    });
    expect(machineSkippedAgents([stale, fresh])).toEqual([]);
    expect(machineSkippedAgents([fresh, stale])).toEqual([]);
  });

  it("ignores rows that never reported the field", () => {
    const never = runtime({ id: "rt-a", last_seen_at: "2026-03-01T00:00:00Z" });
    const reported = runtime({
      id: "rt-b",
      last_seen_at: "2026-01-01T00:00:00Z",
      metadata: { skipped_agents: { qwen: "version below minimum" } },
    });
    expect(machineSkippedAgents([never, reported])).toEqual([
      { provider: "qwen", reason: "version below minimum" },
    ]);
  });

  it("is empty when no row ever reported", () => {
    expect(machineSkippedAgents([runtime()])).toEqual([]);
  });
});

describe("readRuntimeCliAuthState", () => {
  it("reads a well-formed record", () => {
    expect(
      readRuntimeCliAuthState({
        cli_auth: {
          authenticated: true,
          checked_at: "2026-01-01T00:00:00Z",
          provider: "codex",
        },
      }),
    ).toEqual({
      authenticated: true,
      checked_at: "2026-01-01T00:00:00Z",
      provider: "codex",
      reason: undefined,
    });
  });

  it("never lets a malformed record read as signed in", () => {
    expect(readRuntimeCliAuthState({})).toBeNull();
    expect(readRuntimeCliAuthState({ cli_auth: { authenticated: "yes" } }))
      .toBeNull();
    expect(readRuntimeCliAuthState({ cli_auth: [] })).toBeNull();
  });
});

describe("groupRuntimesByMachine", () => {
  it("groups by daemon and counts online CLIs", () => {
    const machines = groupRuntimesByMachine([
      runtime({ id: "a", provider: "qwen", status: "offline" }),
      runtime({ id: "b", provider: "codex", status: "online" }),
    ]);
    expect(machines).toHaveLength(1);
    expect(machines[0]?.onlineCount).toBe(1);
    // Sorted by provider display name: Codex before Qwen Code.
    expect(machines[0]?.runtimes.map((r) => r.id)).toEqual(["b", "a"]);
  });

  it("gives a runtime with no daemon its own machine", () => {
    const machines = groupRuntimesByMachine([
      runtime({ id: "a" }),
      runtime({ id: "cloud", daemon_id: null, runtime_mode: "cloud" }),
    ]);
    expect(machines.map((m) => m.id).sort()).toEqual(["cloud", "daemon-1"]);
  });

  it("orders machines most-recently-seen first and picks the freshest row", () => {
    const machines = groupRuntimesByMachine([
      runtime({
        id: "old",
        daemon_id: "d-old",
        last_seen_at: "2026-01-01T00:00:00Z",
      }),
      runtime({
        id: "new",
        daemon_id: "d-new",
        last_seen_at: "2026-05-01T00:00:00Z",
        metadata: { cli_version: "1.2.3" },
      }),
    ]);
    expect(machines.map((m) => m.id)).toEqual(["d-new", "d-old"]);
    expect(machines[0]?.representativeId).toBe("new");
    expect(machines[0]?.cliVersion).toBe("1.2.3");
  });

  it("returns nothing for an empty list", () => {
    expect(groupRuntimesByMachine([])).toEqual([]);
  });
});
