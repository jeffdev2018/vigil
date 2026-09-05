// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";

import { SkippedAgentsSection } from "./skipped-agents-section";

// The read/merge matrix is pinned in
// packages/core/runtimes/skipped-agents.test.ts. This suite keeps the render
// path and the "nothing to say" case only.
function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (dev.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "dev.local",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: "2026-07-15T00:00:00Z",
    created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-15T00:00:00Z",
    ...overrides,
  };
}

describe("SkippedAgentsSection", () => {
  it("lists each rejected CLI with its reason", () => {
    render(
      <SkippedAgentsSection
        runtimes={[
          runtime({
            metadata: {
              skipped_agents: {
                claude: "claude 1.0.3 rejected: minimum 2.1.0",
                codex: "version could not be detected",
              },
            },
          }),
        ]}
      />,
    );

    expect(screen.getByText("claude")).toBeTruthy();
    expect(
      screen.getByText("claude 1.0.3 rejected: minimum 2.1.0"),
    ).toBeTruthy();
    expect(screen.getByText("codex")).toBeTruthy();
    expect(screen.getByText("version could not be detected")).toBeTruthy();
  });

  it("renders nothing when the machine reported no rejected CLI", () => {
    const { container } = render(
      <SkippedAgentsSection
        runtimes={[runtime({ metadata: { skipped_agents: {} } }), runtime()]}
      />,
    );
    expect(container.firstChild).toBeNull();
  });
})
