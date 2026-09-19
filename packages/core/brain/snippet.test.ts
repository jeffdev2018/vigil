// @vitest-environment node
import { describe, expect, it } from "vitest";
import { renderSnippet, snippetText } from "./snippet";

describe("renderSnippet", () => {
  it("keeps the server's <mark> markers live", () => {
    expect(renderSnippet("push <mark>v0.x.x</mark> on main")).toBe(
      "push <mark>v0.x.x</mark> on main",
    );
  });

  it("escapes note text that looks like HTML", () => {
    // A note is member-writable text and ts_headline escapes nothing, so this
    // is the case that would otherwise be stored XSS.
    expect(renderSnippet('<script>alert("x")</script>')).toBe(
      "&lt;script&gt;alert(&quot;x&quot;)&lt;/script&gt;",
    );
  });

  it("does not let an escaped marker in the note text become a real mark", () => {
    // The note itself contains the literal text "&lt;mark&gt;". Escaping the
    // ampersand first is what stops it from being revived as a tag.
    expect(renderSnippet("&lt;mark&gt;")).toBe("&amp;lt;mark&amp;gt;");
  });

  it("escapes an attribute-bearing tag rather than dropping it", () => {
    expect(renderSnippet('<img src=x onerror="1">')).toBe(
      "&lt;img src=x onerror=&quot;1&quot;&gt;",
    );
  });

  it("passes an empty snippet through", () => {
    expect(renderSnippet("")).toBe("");
  });
});

describe("snippetText", () => {
  it("drops the markers", () => {
    expect(snippetText("push <mark>v0.x.x</mark> on main")).toBe("push v0.x.x on main");
  });
});
