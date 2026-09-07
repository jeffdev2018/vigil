// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  daemonErrorsByLine,
  daemonScheduleCrons,
  FALLBACK_DAEMON_IMPORT_PREVIEW,
  parseDaemonImportPreview,
  parseDaemonImportResult,
} from "./markdown";

// The server owns DAEMON.md validation; what is tested here is that a drifted
// or malformed response degrades safely instead of blanking the dialog or —
// worse — presenting an invalid document as importable.

describe("parseDaemonImportPreview", () => {
  it("keeps a well-formed preview intact", () => {
    const preview = parseDaemonImportPreview(
      {
        valid: true,
        errors: [],
        frontmatter: {
          name: "Nightly triage",
          role: "Sort the queue.",
          agent: "Nova",
          outputs: "issue",
          triggers: [{ kind: "schedule", cron: "0 9 * * *", timezone: "UTC", label: "Morning" }],
          budget: { runs_per_day: 3 },
        },
        body: "# Nightly triage\n",
        warnings: ["budget is recorded but not enforced"],
        agent_id: "agent-1",
        agent_candidates: [],
        digest: "abc",
        unchanged: false,
      },
      "test",
    );

    expect(preview.valid).toBe(true);
    expect(preview.frontmatter?.name).toBe("Nightly triage");
    expect(preview.frontmatter?.triggers[0]?.cron).toBe("0 9 * * *");
    expect(preview.frontmatter?.budget?.runs_per_day).toBe(3);
    expect(preview.warnings).toHaveLength(1);
  });

  it("tolerates a trigger kind this build has never heard of", () => {
    // `kind` is z.string() on purpose: a newer server growing a third kind must
    // still render its preview, with the UI falling through to a generic row.
    const preview = parseDaemonImportPreview(
      {
        valid: true,
        errors: [],
        frontmatter: { name: "n", role: "r", agent: "a", triggers: [{ kind: "carrier_pigeon" }] },
        body: "",
        digest: "d",
      },
      "test",
    );
    expect(preview.valid).toBe(true);
    expect(preview.frontmatter?.triggers[0]?.kind).toBe("carrier_pigeon");
  });

  it("degrades a malformed response to NOT valid", () => {
    // The dangerous failure is the other direction: a preview that cannot be
    // read must never look importable, or the dialog offers a write the server
    // is about to reject.
    for (const malformed of [null, undefined, "nope", 42, { valid: "yes" }, { errors: "boom" }]) {
      const preview = parseDaemonImportPreview(malformed, "test");
      expect(preview.valid).toBe(false);
    }
  });

  it("falls back to a preview that carries no frontmatter", () => {
    const preview = parseDaemonImportPreview({ valid: "yes" }, "test");
    expect(preview).toEqual(FALLBACK_DAEMON_IMPORT_PREVIEW);
    expect(preview.frontmatter).toBeUndefined();
  });

  it("defaults the missing halves of a partial preview", () => {
    const preview = parseDaemonImportPreview({ valid: true, digest: "d" }, "test");
    expect(preview.errors).toEqual([]);
    expect(preview.body).toBe("");
    expect(preview.warnings).toEqual([]);
    expect(preview.agent_candidates).toEqual([]);
    expect(preview.unchanged).toBe(false);
  });
});

describe("parseDaemonImportResult", () => {
  it("keeps a well-formed result intact", () => {
    const result = parseDaemonImportResult(
      { status: "created", autopilot: { id: "ap-1", title: "x" }, triggers: [{ kind: "schedule" }], skill_id: "s-1", digest: "d" },
      "test",
    );
    expect(result.status).toBe("created");
    expect(result.autopilot.id).toBe("ap-1");
    expect(result.skill_id).toBe("s-1");
  });

  it("degrades a malformed result to an empty status", () => {
    // An empty status is what the caller branches on: no success toast for a
    // write it could not confirm.
    expect(parseDaemonImportResult({ status: 7 }, "test").status).toBe("");
    expect(parseDaemonImportResult(null, "test").autopilot.id).toBe("");
  });
});

describe("daemonErrorsByLine", () => {
  it("groups several problems on one line", () => {
    const byLine = daemonErrorsByLine([
      { line: 5, message: "unknown key" },
      { line: 5, message: "duplicate key" },
      { line: 9, message: "invalid trigger kind" },
    ]);
    expect(byLine.get(5)).toHaveLength(2);
    expect(byLine.get(9)).toHaveLength(1);
  });

  it("keeps whole-document problems under line 0 rather than dropping them", () => {
    // "name is required" has no single line to point at, and losing it would
    // leave the dialog reporting no reason for a refused import.
    const byLine = daemonErrorsByLine([{ line: 0, message: "name is required" }]);
    expect(byLine.get(0)).toHaveLength(1);
  });
});

describe("daemonScheduleCrons", () => {
  it("returns only schedule triggers that actually carry a cron", () => {
    expect(
      daemonScheduleCrons([
        { kind: "schedule", cron: "0 9 * * *", timezone: "Europe/Paris", label: "Morning" },
        { kind: "webhook", label: "CI" },
        { kind: "schedule" },
      ]),
    ).toEqual([{ cron: "0 9 * * *", timezone: "Europe/Paris", label: "Morning" }]);
  });

  it("defaults a missing timezone to UTC, matching the server", () => {
    expect(daemonScheduleCrons([{ kind: "schedule", cron: "* * * * *" }])[0]?.timezone).toBe("UTC");
  });

  it("never previews a webhook entry that carries a cron", () => {
    // A cron on a webhook is server drift, not a schedule. Previewing "next
    // runs" for it would promise a clock that does not exist.
    expect(daemonScheduleCrons([{ kind: "webhook", cron: "0 9 * * *" }])).toEqual([]);
  });

  it("handles an absent trigger list", () => {
    expect(daemonScheduleCrons(undefined)).toEqual([]);
    expect(daemonScheduleCrons([])).toEqual([]);
  });
});
