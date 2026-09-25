// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  DoctrineDiffSchema,
  DoctrinePublishResponseSchema,
  DoctrineReportsResponseSchema,
  DoctrineSchema,
  DoctrineVersionsResponseSchema,
  EMPTY_DOCTRINE,
  EMPTY_DOCTRINE_DIFF,
  canPublishDoctrine,
  doctrineByteLength,
  normalizeDoctrineContent,
  type Doctrine,
} from "./schemas";

// The canonical place the doctrine contract and the publish precondition are
// proven; doctrine-tab.test.tsx keeps the happy path and the wiring.

const version = {
  id: "v1",
  revision: 3,
  content: "Ship small.",
  status: "active",
  note: "clarified the review rule",
  author_id: "u1",
  reviewed_by: "u2",
  reviewed_at: "2026-09-09T10:00:00Z",
  review_note: "",
  restored_from_revision: null,
  created_at: "2026-09-09T09:00:00Z",
  bytes: 11,
};

const doctrine = {
  content: "Ship small.",
  revision: 3,
  updated_at: "2026-09-09T10:00:00Z",
  updated_by: "u1",
  byte_limit: 32000,
  require_review: true,
  can_publish: true,
  active_version_id: "v1",
  pending: null,
  open_reports: 2,
};

describe("DoctrineSchema", () => {
  it("parses the live doctrine with a pending proposal", () => {
    const parsed = DoctrineSchema.parse({
      ...doctrine,
      pending: { ...version, id: "v2", revision: null, status: "pending" },
    });
    expect(parsed.revision).toBe(3);
    expect(parsed.pending?.revision).toBeNull();
    expect(parsed.pending?.status).toBe("pending");
    expect(parsed.open_reports).toBe(2);
  });

  it("keeps a status a newer server invented", () => {
    const parsed = DoctrineSchema.parse({
      ...doctrine,
      pending: { ...version, status: "escalated" },
    });
    expect(parsed.pending?.status).toBe("escalated");
  });

  it("falls back to the empty doctrine on garbage", () => {
    expect(
      parseWithFallback<Doctrine>("nope", DoctrineSchema, EMPTY_DOCTRINE, { endpoint: "test" }),
    ).toEqual(EMPTY_DOCTRINE);
    // A field-level miss degrades that field only: the editor still renders,
    // read-only, rather than white-screening on a drifted response.
    const parsed = DoctrineSchema.parse({ content: 12, revision: "three", can_publish: "yes", pending: "x" });
    expect(parsed.content).toBe("");
    expect(parsed.revision).toBe(0);
    expect(parsed.can_publish).toBe(false);
    expect(parsed.pending).toBeNull();
    expect(parsed.byte_limit).toBe(32000);
  });
});

describe("version, publish, diff and report envelopes", () => {
  it("parses a publish answer", () => {
    const parsed = DoctrinePublishResponseSchema.parse({ doctrine, version });
    expect(parsed.doctrine.revision).toBe(3);
    expect(parsed.version.id).toBe("v1");
  });

  it("pages the ledger and tolerates a missing cursor", () => {
    const parsed = DoctrineVersionsResponseSchema.parse({ versions: [version], next_cursor: "abc" });
    expect(parsed.versions).toHaveLength(1);
    expect(parsed.next_cursor).toBe("abc");
    expect(DoctrineVersionsResponseSchema.parse({ versions: "x" })).toMatchObject({
      versions: [],
      next_cursor: null,
    });
  });

  it("parses a diff and keeps an unknown line kind", () => {
    const parsed = DoctrineDiffSchema.parse({
      from: { ...version, content: "" },
      to: { ...version, id: "v2", content: "" },
      lines: [
        { kind: "same", text: "Ship small." },
        { kind: "add", text: "Review before you push." },
        { kind: "del", text: "Old rule." },
        { kind: "moved", text: "?" },
      ],
      added: 1,
      removed: 1,
    });
    expect(parsed.lines.map((l) => l.kind)).toEqual(["same", "add", "del", "moved"]);
    expect(parsed.added).toBe(1);
    expect(
      parseWithFallback("nope", DoctrineDiffSchema, EMPTY_DOCTRINE_DIFF, { endpoint: "test" }),
    ).toEqual(EMPTY_DOCTRINE_DIFF);
  });

  it("parses reports and keeps an unknown kind", () => {
    const parsed = DoctrineReportsResponseSchema.parse({
      reports: [
        {
          id: "r1",
          doctrine_revision: 3,
          kind: "conflict",
          summary: "Two rules disagree on who approves a release.",
          passage: "Releases need an owner. Releases need two reviewers.",
          reporter_type: "agent",
          reporter_id: "a1",
          task_id: "t1",
          issue_id: "i1",
          status: "open",
          resolved_by: null,
          resolved_at: null,
          resolution_note: "",
          created_at: "2026-09-09T09:00:00Z",
        },
        { id: "r2", kind: "loop" },
      ],
    });
    expect(parsed.reports).toHaveLength(2);
    expect(parsed.reports[0]?.issue_id).toBe("i1");
    expect(parsed.reports[1]?.kind).toBe("loop");
    expect(parsed.reports[1]?.status).toBe("open");
  });
});

describe("byte length and the publish precondition", () => {
  it("counts UTF-8 bytes, not characters", () => {
    expect(doctrineByteLength("abc")).toBe(3);
    expect(doctrineByteLength("é")).toBe(2);
    expect(doctrineByteLength("任务")).toBe(6);
  });

  it("trims trailing whitespace the way the server does", () => {
    expect(normalizeDoctrineContent("a\r\nb  \n\n")).toBe("a\nb");
  });

  it("offers Publish only to a manager with changed text that fits", () => {
    const base = DoctrineSchema.parse(doctrine) as Doctrine;
    expect(canPublishDoctrine(base, "Ship smaller.")).toBe(true);
    // Unchanged (the server 400s on it), whitespace-only difference included.
    expect(canPublishDoctrine(base, "Ship small.")).toBe(false);
    expect(canPublishDoctrine(base, "Ship small.\n\n")).toBe(false);
    // Over the limit.
    expect(canPublishDoctrine({ ...base, byte_limit: 5 }, "Ship smaller.")).toBe(false);
    // Not a manager.
    expect(canPublishDoctrine({ ...base, can_publish: false }, "Ship smaller.")).toBe(false);
    // A proposal is already awaiting review.
    expect(
      canPublishDoctrine(
        { ...base, pending: DoctrineSchema.parse({ ...doctrine, pending: version }).pending },
        "Ship smaller.",
      ),
    ).toBe(false);
  });
});
