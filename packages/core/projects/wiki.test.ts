// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  CodeWikiPageSchema,
  type CodeWikiPage,
  CodeWikiSchema,
  EMPTY_CODE_WIKI,
  citationUrl,
  formatCitation,
  repoUrlFromResource,
  type CodeWikiCitation,
} from "./wiki";

// F26: the boundary. A wiki page is generated text and its provenance fields
// are what make it usable; the panel must survive every shape a backend that
// drifted could send, including one that stopped sending citations.

const citation = (over: Partial<CodeWikiCitation> = {}): CodeWikiCitation => ({
  path: "src/app.py",
  start_line: null,
  end_line: null,
  commit_sha: "",
  ...over,
});

describe("CodeWikiSchema", () => {
  it("parses a published wiki", () => {
    const parsed = parseWithFallback(
      {
        resource: { id: "r1", resource_ref: { url: "https://github.com/acme/widget" } },
        snapshot: {
          id: "s1",
          project_resource_id: "r1",
          commit_sha: "abc1234",
          state: "published",
          page_count: 2,
          created_at: "2026-01-01T00:00:00Z",
          published_at: "2026-01-02T00:00:00Z",
          generated: true,
          stale: false,
        },
        pages: [{ id: "p1", slug: "overview", title: "Overview", citation_count: 3 }],
        building: false,
        generated: true,
      },
      CodeWikiSchema,
      EMPTY_CODE_WIKI,
      { endpoint: "test" },
    );
    expect(parsed.snapshot?.commit_sha).toBe("abc1234");
    expect(parsed.pages).toHaveLength(1);
    expect(parsed.building).toBe(false);
  });

  it("falls back to the empty wiki on a malformed response", () => {
    for (const raw of [null, "nope", 42, [], { snapshot: "not an object" }]) {
      const parsed = parseWithFallback(raw, CodeWikiSchema, EMPTY_CODE_WIKI, { endpoint: "test" });
      expect(Array.isArray(parsed.pages)).toBe(true);
      expect(parsed.generated).toBe(true);
    }
  });

  it("keeps an unknown snapshot state rather than dropping the snapshot", () => {
    const parsed = parseWithFallback(
      { snapshot: { id: "s1", state: "quarantined" }, pages: [] },
      CodeWikiSchema,
      EMPTY_CODE_WIKI,
      { endpoint: "test" },
    );
    expect(parsed.snapshot?.state).toBe("quarantined");
  });

  it("defaults the provenance flags a drifting server stopped sending", () => {
    const parsed = parseWithFallback(
      { snapshot: { id: "s1" }, pages: [{ id: "p1", slug: "a", title: "A" }] },
      CodeWikiSchema,
      EMPTY_CODE_WIKI,
      { endpoint: "test" },
    );
    expect(parsed.snapshot?.generated).toBe(true);
    expect(parsed.snapshot?.stale).toBe(false);
    expect(parsed.pages[0]?.citation_count).toBe(0);
  });
});

describe("CodeWikiPageSchema", () => {
  it("renders a page whose citations are missing or unusable", () => {
    for (const citations of [undefined, null, "not an array", 12]) {
      const parsed = parseWithFallback(
        { id: "p1", slug: "a", title: "A", content: "body", citations },
        CodeWikiPageSchema,
        { id: "", slug: "a", title: "a", content: "", citations: [], commit_sha: "", generated: true, stale: false } as CodeWikiPage,
        { endpoint: "test" },
      );
      expect(parsed.citations).toEqual([]);
      expect(parsed.content).toBe("body");
    }
  });

  it("drops nothing when a citation is well formed", () => {
    const parsed = parseWithFallback(
      {
        id: "p1", slug: "a", title: "A", content: "body",
        citations: [{ path: "src/app.py", start_line: 3, end_line: 9, commit_sha: "abc" }],
        commit_sha: "abc", generated: true, stale: true,
      },
      CodeWikiPageSchema,
      { id: "", slug: "a", title: "a", content: "", citations: [], commit_sha: "", generated: true, stale: false } as CodeWikiPage,
      { endpoint: "test" },
    );
    expect(parsed.citations[0]?.start_line).toBe(3);
    expect(parsed.stale).toBe(true);
  });
});

describe("formatCitation", () => {
  it("names a whole file, one line, or a range", () => {
    expect(formatCitation(citation())).toBe("src/app.py");
    expect(formatCitation(citation({ start_line: 12 }))).toBe("src/app.py:12");
    expect(formatCitation(citation({ start_line: 12, end_line: 40 }))).toBe("src/app.py:12-40");
    expect(formatCitation(citation({ start_line: 12, end_line: 12 }))).toBe("src/app.py:12");
  });

  it("is empty for a citation with no path", () => {
    expect(formatCitation(citation({ path: "" }))).toBe("");
  });
});

describe("citationUrl", () => {
  it("links into the forge at the commit the page describes", () => {
    expect(citationUrl("https://github.com/acme/widget", "abc1234", citation({ start_line: 3, end_line: 9 })))
      .toBe("https://github.com/acme/widget/blob/abc1234/src/app.py#L3-L9");
    expect(citationUrl("https://github.com/acme/widget.git", "abc1234", citation({ start_line: 3 })))
      .toBe("https://github.com/acme/widget/blob/abc1234/src/app.py#L3");
    expect(citationUrl("https://github.com/acme/widget/", "abc1234", citation()))
      .toBe("https://github.com/acme/widget/blob/abc1234/src/app.py");
  });

  it("returns null rather than a broken link", () => {
    expect(citationUrl(null, "abc", citation())).toBeNull();
    expect(citationUrl("", "abc", citation())).toBeNull();
    expect(citationUrl("git@github.com:acme/widget.git", "abc", citation())).toBeNull();
    expect(citationUrl("https://github.com/acme/widget", "", citation())).toBeNull();
    expect(citationUrl("https://github.com/acme/widget", "abc", citation({ path: "" }))).toBeNull();
  });
});

describe("repoUrlFromResource", () => {
  it("reads the url out of a server-owned resource ref", () => {
    expect(repoUrlFromResource({ resource_ref: { url: "https://github.com/acme/widget" } }))
      .toBe("https://github.com/acme/widget");
  });

  it("returns null for anything else", () => {
    for (const raw of [null, undefined, {}, { resource_ref: null }, { resource_ref: { url: 12 } }, { resource_ref: { url: "  " } }]) {
      expect(repoUrlFromResource(raw)).toBeNull();
    }
  });
});
