// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_PACK_CATALOGUE,
  EMPTY_PACK_PREVIEW,
  EMPTY_PACK_SEED_CATALOGUE,
  PackCatalogueSchema,
  PackDetailSchema,
  PackInstallListSchema,
  PackInstallResultSchema,
  PackPreviewSchema,
  PackSeedCatalogueSchema,
  PackUninstallResultSchema,
  packCountTotal,
  type PackCatalogue,
  type PackPreview,
  type PackSeedCatalogue,
} from "./schemas";

// The canonical place the pack contract is proven; packs-tab.test.tsx keeps
// the happy path and the wiring.

const manifest = {
  id: "helpdesk-it",
  version: "1.2.0",
  title: "IT helpdesk",
  summary: "Take tickets, triage them, answer them.",
  description: "## What you get\n\nA queue and the rules around it.",
  domain: "helpdesk",
  wave: 1,
  author: "Multica",
  license: "MIT",
  tags: ["tickets", "sla"],
  works_without_agents: true,
  metric: { label: "First response time", description: "Median hours", hint: "Watch it weekly" },
  prerequisites: [{ kind: "channel", name: "Email", optional: true, note: "Or Slack" }],
  changelog: [{ version: "1.2.0", note: "Added the SLA view" }],
};

const summary = {
  manifest,
  counts: { agents: 2, views: 3, labels: 5 },
  builtin: true,
  installed_version: "1.1.0",
  install_id: "install-1",
  upgrade_available: true,
  prerequisites: [
    { kind: "channel", name: "Email", optional: true, note: "Or Slack", status: "unknown" },
    { kind: "native_runtime", name: "Native runtime", optional: false, note: "", status: "met" },
  ],
};

const install = {
  id: "install-1",
  pack_id: "helpdesk-it",
  pack_version: "1.1.0",
  title: "IT helpdesk",
  source: "builtin",
  strategy: "skip",
  status: "installed",
  run_id: "run-1",
  report: { created: { labels: 5 } },
  manifest,
  installed_by: "user-1",
  installed_at: "2026-09-09T09:00:00Z",
  removed_at: null,
  item_count: 12,
  metric: manifest.metric,
  domain: "helpdesk",
  upgrade_to: "1.2.0",
  bundle_sha256: "abc",
};

describe("pack catalogue", () => {
  it("parses the catalogue and its install state", () => {
    const out = parseWithFallback<PackCatalogue>(
      { packs: [summary], domains: ["helpdesk", "ops"] },
      PackCatalogueSchema,
      EMPTY_PACK_CATALOGUE,
      { endpoint: "test" },
    );
    expect(out.domains).toEqual(["helpdesk", "ops"]);
    expect(out.packs[0]?.manifest.title).toBe("IT helpdesk");
    expect(out.packs[0]?.installed_version).toBe("1.1.0");
    expect(out.packs[0]?.upgrade_available).toBe(true);
    expect(out.packs[0]?.prerequisites.map((p) => p.status)).toEqual(["unknown", "met"]);
  });

  it("keeps a pack a newer server ships with an unknown domain and metric", () => {
    const out = parseWithFallback<PackCatalogue>(
      {
        packs: [{ ...summary, manifest: { ...manifest, domain: "procurement" } }],
        domains: ["procurement"],
      },
      PackCatalogueSchema,
      EMPTY_PACK_CATALOGUE,
      { endpoint: "test" },
    );
    // Domains stay `z.string()` on purpose: a pack in a domain this client
    // has never heard of must still render, with the raw key as its label.
    expect(out.packs[0]?.manifest.domain).toBe("procurement");
  });

  it("falls back on a malformed catalogue response", () => {
    const out = parseWithFallback<PackCatalogue>(
      { packs: "not-a-list" },
      PackCatalogueSchema,
      EMPTY_PACK_CATALOGUE,
      { endpoint: "test" },
    );
    expect(out).toEqual(EMPTY_PACK_CATALOGUE);
  });

  it("repairs per-field drift instead of losing the whole pack", () => {
    const out = parseWithFallback<PackCatalogue>(
      {
        packs: [{ ...summary, counts: null, installed_version: 3, prerequisites: "nope" }],
        domains: null,
      },
      PackCatalogueSchema,
      EMPTY_PACK_CATALOGUE,
      { endpoint: "test" },
    );
    expect(out.packs).toHaveLength(1);
    expect(out.packs[0]?.counts).toEqual({});
    expect(out.packs[0]?.installed_version).toBeNull();
    expect(out.packs[0]?.prerequisites).toEqual([]);
    expect(out.domains).toEqual([]);
  });
});

describe("pre-workspace catalogue", () => {
  const entry = { manifest, counts: { labels: 5, views: 3 }, contents: { labels: ["Bug"] } };

  it("parses the seed catalogue: manifest, counts and contents, no install state", () => {
    const out = parseWithFallback<PackSeedCatalogue>(
      { packs: [entry], domains: ["helpdesk", "ops"] },
      PackSeedCatalogueSchema,
      EMPTY_PACK_SEED_CATALOGUE,
      { endpoint: "test" },
    );
    expect(out.packs[0]?.manifest.title).toBe("IT helpdesk");
    expect(packCountTotal(out.packs[0]?.counts ?? {})).toBe(8);
    expect(out.packs[0]?.contents.labels).toEqual(["Bug"]);
    expect(out.domains).toEqual(["helpdesk", "ops"]);
  });

  it("falls back on a malformed seed catalogue response", () => {
    const out = parseWithFallback<PackSeedCatalogue>(
      { packs: 7 },
      PackSeedCatalogueSchema,
      EMPTY_PACK_SEED_CATALOGUE,
      { endpoint: "test" },
    );
    expect(out).toEqual(EMPTY_PACK_SEED_CATALOGUE);
  });

  it("repairs a drifted entry instead of dropping the pack the picker needs", () => {
    const out = parseWithFallback<PackSeedCatalogue>(
      { packs: [{ manifest, counts: null, contents: "nope" }], domains: null },
      PackSeedCatalogueSchema,
      EMPTY_PACK_SEED_CATALOGUE,
      { endpoint: "test" },
    );
    expect(out.packs).toHaveLength(1);
    expect(out.packs[0]?.counts).toEqual({});
    expect(out.packs[0]?.contents).toEqual({});
    expect(out.domains).toEqual([]);
  });
});

describe("pack detail and preview", () => {
  it("parses contents grouped by kind and the YAML source", () => {
    const out = PackDetailSchema.parse({
      pack: summary,
      contents: { labels: ["Bug", "Question"], views: ["Open tickets"] },
      source: "pack:\n  id: helpdesk-it\n",
    });
    expect(out.contents.labels).toEqual(["Bug", "Question"]);
    expect(out.source).toContain("helpdesk-it");
  });

  it("parses a preview with collisions and the server's default strategy", () => {
    const out = parseWithFallback<PackPreview>(
      {
        pack: summary,
        contents: { labels: ["Bug"] },
        collisions: [{ kind: "label", name: "Bug", existing_id: "label-1" }],
        problems: [],
        strategies: ["rename", "merge", "skip"],
        strategy: "merge",
        installed: install,
        blocked: "",
      },
      PackPreviewSchema,
      EMPTY_PACK_PREVIEW,
      { endpoint: "test" },
    );
    expect(out.strategy).toBe("merge");
    expect(out.collisions[0]?.existing_id).toBe("label-1");
    expect(out.installed?.pack_version).toBe("1.1.0");
    expect(out.blocked).toBe("");
  });

  it("keeps the blocked reason so the UI can disable Install", () => {
    const out = PackPreviewSchema.parse({
      pack: summary,
      contents: {},
      collisions: [],
      problems: ["issue status `done` is missing"],
      strategies: ["skip"],
      strategy: "skip",
      installed: install,
      blocked: "version 1.2.0 is already installed",
    });
    expect(out.blocked).toBe("version 1.2.0 is already installed");
    expect(out.problems).toHaveLength(1);
  });

  it("defaults the strategy list when the server omits it", () => {
    const out = PackPreviewSchema.parse({ pack: summary, contents: {}, strategy: "skip" });
    expect(out.strategies).toEqual(["skip", "merge", "rename"]);
    expect(out.installed).toBeNull();
  });
});

describe("install, ledger and uninstall", () => {
  it("parses the install report with its per-row items", () => {
    const out = PackInstallResultSchema.parse({
      install,
      report: {
        created: { labels: 5, views: 3 },
        merged: { issue_statuses: 2 },
        skipped: [{ kind: "label", name: "Bug", existing_id: "label-1" }],
        secrets_pending: [],
        warnings: ["one autopilot arrived disabled"],
        items: [{ kind: "label", name: "Question", id: "label-2", action: "created" }],
      },
    });
    expect(packCountTotal(out.report.created)).toBe(8);
    expect(packCountTotal(out.report.merged)).toBe(2);
    expect(out.report.skipped).toHaveLength(1);
    expect(out.report.items[0]?.action).toBe("created");
    expect(out.report.warnings).toHaveLength(1);
  });

  it("parses the install ledger, upgrade pointer included", () => {
    const out = PackInstallListSchema.parse({ installs: [install, { ...install, id: "i2", upgrade_to: null }] });
    expect(out.installs).toHaveLength(2);
    expect(out.installs[0]?.upgrade_to).toBe("1.2.0");
    expect(out.installs[1]?.upgrade_to).toBeNull();
  });

  it("parses the uninstall report: what went, what stayed and why", () => {
    const out = PackUninstallResultSchema.parse({
      install: { ...install, status: "removed", removed_at: "2026-09-10T09:00:00Z" },
      report: {
        removed: { labels: 5, views: 3 },
        kept: [{ kind: "project", name: "Ticket queue", id: "p1", action: "created" }],
        reasons: ["project Ticket queue: content stays in the workspace"],
      },
    });
    expect(out.install.status).toBe("removed");
    expect(packCountTotal(out.report.removed)).toBe(8);
    expect(out.report.kept[0]?.kind).toBe("project");
    expect(out.report.reasons[0]).toContain("stays in the workspace");
  });
});

describe("packCountTotal", () => {
  it("sums a report's per-kind counts and copes with an empty record", () => {
    expect(packCountTotal({})).toBe(0);
    expect(packCountTotal({ a: 1, b: 2, c: 3 })).toBe(6);
  });
});
