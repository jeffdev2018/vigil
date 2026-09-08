// @vitest-environment node
import { describe, expect, it } from "vitest";
import { anchorForLine, hunkContainsAnchor, hunkLines } from "./schemas";
import type { PrWalkthroughHunk } from "./schemas";

// Diff anchoring (F07 / JEF-21). The whole feature rests on this arithmetic:
// a line number that is one off points the discussion at the wrong code, and
// nothing downstream can notice.

const hunk = (over: Partial<PrWalkthroughHunk>): PrWalkthroughHunk => ({
  old_start: 1,
  new_start: 1,
  lines: "",
  explanation: "",
  moved_from: "",
  ...over,
});

describe("hunkLines", () => {
  it("numbers each side independently across additions and deletions", () => {
    const got = hunkLines(hunk({
      old_start: 10,
      new_start: 10,
      lines: [" keep", "-gone", "+added", "+also added", " tail"].join("\n"),
    }));
    expect(got.map((l) => [l.kind, l.oldLine, l.newLine])).toEqual([
      ["context", 10, 10],
      ["del", 11, 0],
      ["add", 0, 11],
      ["add", 0, 12],
      ["context", 12, 13],
    ]);
  });

  it("resets both counters on an embedded @@ header", () => {
    const got = hunkLines(hunk({
      old_start: 1,
      new_start: 1,
      lines: ["@@ -12,7 +40,9 @@ func x()", " ctx"].join("\n"),
    }));
    expect(got[0]!.kind).toBe("meta");
    expect(got[1]).toMatchObject({ kind: "context", oldLine: 12, newLine: 40 });
  });

  it("does not spend a line number on the no-newline marker", () => {
    const got = hunkLines(hunk({
      old_start: 5,
      new_start: 5,
      lines: ["+last", "\\ No newline at end of file"].join("\n"),
    }));
    expect(got[1]).toMatchObject({ kind: "meta", oldLine: 0, newLine: 0 });
  });

  it("returns nothing for an empty body", () => {
    expect(hunkLines(hunk({ lines: "" }))).toEqual([]);
  });
});

describe("anchorForLine", () => {
  it("puts a deletion on the old side and everything else on the new one", () => {
    const lines = hunkLines(hunk({
      old_start: 3,
      new_start: 3,
      lines: [" ctx", "-gone", "+added", "@@ -1 +1 @@"].join("\n"),
    }));
    expect(anchorForLine(lines[0]!)).toEqual({ side: "new", line: 3 });
    expect(anchorForLine(lines[1]!)).toEqual({ side: "old", line: 4 });
    expect(anchorForLine(lines[2]!)).toEqual({ side: "new", line: 4 });
    // A header is not a place a question can be about.
    expect(anchorForLine(lines[3]!)).toBeNull();
  });
});

describe("hunkContainsAnchor", () => {
  const lines = hunkLines(hunk({
    old_start: 10,
    new_start: 20,
    lines: [" ctx", "-gone", "+added"].join("\n"),
  }));

  it("matches a range overlapping the hunk on the requested side", () => {
    expect(hunkContainsAnchor(lines, "new", 20, 21)).toBe(true);
    expect(hunkContainsAnchor(lines, "old", 11, 11)).toBe(true);
  });

  it("does not match the same number on the other side", () => {
    expect(hunkContainsAnchor(lines, "old", 20, 20)).toBe(false);
  });

  it("treats a missing line_end as a single line", () => {
    expect(hunkContainsAnchor(lines, "new", 21, 0)).toBe(true);
    expect(hunkContainsAnchor(lines, "new", 99, 0)).toBe(false);
  });
});
