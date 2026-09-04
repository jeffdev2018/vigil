// @vitest-environment node
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  cliAuthDocsHref,
  customRuntimeDocsHref,
  daemonRuntimesDocsHref,
} from "./runtime-docs";

describe("runtime docs links", () => {
  it.each([
    ["en", "https://multica.ai/docs/daemon-runtimes"],
    ["zh-Hans", "https://multica.ai/docs/zh/daemon-runtimes"],
    ["ja", "https://multica.ai/docs/ja/daemon-runtimes"],
    ["ko", "https://multica.ai/docs/ko/daemon-runtimes"],
  ])("localizes the daemon guide for %s", (language, expected) => {
    expect(daemonRuntimesDocsHref(language)).toBe(expected);
  });

  it("adds the localized custom runtime section", () => {
    expect(customRuntimeDocsHref("zh-Hans")).toBe(
      `https://multica.ai/docs/zh/daemon-runtimes#${encodeURIComponent("自定义运行时配置")}`,
    );
  });
});

// The English docs page is the source of truth for the anchors the runtime UI
// links into; a renamed heading must fail here rather than 404 in production.
const docsPath = fileURLToPath(
  new URL(
    "../../../../apps/docs/content/docs/daemon-runtimes.mdx",
    import.meta.url,
  ),
);

// Same rule as github-slugger for the plain ASCII headings the English page
// uses: lowercase, drop punctuation, spaces become hyphens.
function slugify(heading: string): string {
  return heading
    .toLowerCase()
    .replace(/[^\p{L}\p{N} -]/gu, "")
    .trim()
    .replace(/ +/g, "-");
}

describe("runtime docs anchors", () => {
  const anchors = readFileSync(docsPath, "utf8")
    .split("\n")
    .filter((line) => /^#{2,} /.test(line))
    .map((line) => slugify(line.replace(/^#+ /, "")));

  it.each([
    ["cliAuthDocsHref", cliAuthDocsHref()],
    ["customRuntimeDocsHref", customRuntimeDocsHref()],
  ])("%s points at an existing heading", (_name, href) => {
    expect(anchors).toContain(decodeURIComponent(href.split("#")[1] ?? ""));
  });
});
