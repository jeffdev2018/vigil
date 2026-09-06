// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  DOC_DRIFT_DEFAULT_SETTINGS,
  DocDriftProposalListSchema,
  DocDriftSettingsSchema,
  driftExcerpt,
  isOpenProposal,
  proposalTone,
  shortDriftCommit,
  type DocDriftProposal,
} from "./schemas";
import { parseWithFallback } from "../api/schema";

// Canonical layer for the agent context drift contract (K56). The component
// suite (packages/views/settings/components/doc-drift-tab.test.tsx) keeps the
// wiring and the happy path; the parsing matrix lives here.

const proposal = (over: Partial<DocDriftProposal> = {}): DocDriftProposal => ({
  id: "p1",
  workspace_id: "w1",
  repo_identifier: "git@example.com:team/app.git",
  doc_path: "CLAUDE.md",
  detected_drift: "Two commands moved.\n\n- **Commands** — `make serve` is gone",
  proposed_patch: "### Commands\n\n-make serve\n+make server",
  detected_at_commit: "abc1234def",
  status: "draft",
  pull_request_url: "",
  scan_task_id: "t1",
  pr_task_id: null,
  created_at: "2026-01-02T03:04:05Z",
  updated_at: "2026-01-02T03:04:05Z",
  ...over,
});

describe("DocDriftProposalListSchema", () => {
  it("parses a well-formed response", () => {
    const parsed = parseWithFallback(
      { proposals: [proposal()] },
      DocDriftProposalListSchema,
      { proposals: [] as DocDriftProposal[] },
      { endpoint: "test" },
    );
    expect(parsed.proposals).toHaveLength(1);
    expect(parsed.proposals[0]?.doc_path).toBe("CLAUDE.md");
  });

  it("keeps an unknown status rather than dropping the proposal", () => {
    // An installed desktop client can outlive a backend enum; losing the row
    // would hide a proposal that is real, which is worse than an odd label.
    const parsed = parseWithFallback(
      { proposals: [proposal({ status: "superseded" as DocDriftProposal["status"] })] },
      DocDriftProposalListSchema,
      { proposals: [] as DocDriftProposal[] },
      { endpoint: "test" },
    );
    expect(parsed.proposals[0]?.status).toBe("superseded");
  });

  it("falls back rather than throwing on a malformed response", () => {
    const parsed = parseWithFallback(
      { proposals: "not an array" },
      DocDriftProposalListSchema,
      { proposals: [] },
      { endpoint: "test" },
    );
    expect(parsed.proposals).toEqual([]);
  });
});

describe("DocDriftSettingsSchema", () => {
  it("parses settings with their repository status", () => {
    const parsed = parseWithFallback(
      {
        enabled: true,
        agent_id: "a1",
        docs: ["CLAUDE.md"],
        open_pr: false,
        last_checked: { "git@example.com:team/app.git": "abc" },
        scan_tasks: {},
        repos: [{ repo_identifier: "git@example.com:team/app.git", last_indexed_commit: "def", last_checked_commit: "abc", due: true, scanning: false }],
      },
      DocDriftSettingsSchema,
      DOC_DRIFT_DEFAULT_SETTINGS,
      { endpoint: "test" },
    );
    expect(parsed.enabled).toBe(true);
    expect(parsed.repos[0]?.due).toBe(true);
    expect(parsed.last_checked["git@example.com:team/app.git"]).toBe("abc");
  });

  it("falls back to a disabled configuration on a malformed response", () => {
    const parsed = parseWithFallback(
      { enabled: "yes please" },
      DocDriftSettingsSchema,
      DOC_DRIFT_DEFAULT_SETTINGS,
      { endpoint: "test" },
    );
    expect(parsed.enabled).toBe(false);
    expect(parsed.repos).toEqual([]);
  });
});

describe("helpers", () => {
  it("treats draft and opened_pr as the states that block a new detection", () => {
    expect(isOpenProposal(proposal({ status: "draft" }))).toBe(true);
    expect(isOpenProposal(proposal({ status: "opened_pr" }))).toBe(true);
    expect(isOpenProposal(proposal({ status: "dismissed" }))).toBe(false);
    expect(isOpenProposal(proposal({ status: "merged" }))).toBe(false);
  });

  it("never paints a proposal as an error", () => {
    expect(proposalTone("draft")).toBe("warning");
    expect(proposalTone("opened_pr")).toBe("warning");
    expect(proposalTone("merged")).toBe("success");
    expect(proposalTone("dismissed")).toBe("muted");
    expect(proposalTone("something new")).toBe("muted");
  });

  it("excerpts the first line of the drift summary", () => {
    expect(driftExcerpt(proposal())).toBe("Two commands moved.");
    expect(driftExcerpt(proposal({ detected_drift: "" }))).toBe("");
    expect(driftExcerpt(proposal({ detected_drift: "x".repeat(300) }), 10)).toHaveLength(10);
  });

  it("shortens a commit and leaves an empty one alone", () => {
    expect(shortDriftCommit("abc1234def")).toBe("abc1234");
    expect(shortDriftCommit("")).toBe("");
  });
});
