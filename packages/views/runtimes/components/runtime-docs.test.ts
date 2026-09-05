// @vitest-environment node
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  CLI_AUTH_PROVIDERS,
  cliAuthLogoutSupported,
  cliAuthSupported,
} from "@multica/core/runtimes/cli-auth";
import {
  cliAuthDocsHref,
  cliAuthProviderDocsHref,
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

// Every provider Multica cannot sign in for gets a link telling the user how
// to do it themselves — a generic "read our guide" is not that.
describe("cliAuthProviderDocsHref", () => {
  it.each([
    ["cursor-agent", "https://cursor.com/docs/cli/reference/authentication"],
    ["opencode", "https://opencode.ai/docs/cli/"],
    ["qwen", "https://qwenlm.github.io/qwen-code-docs/en/users/configuration/auth/"],
  ])("points %s at its own documentation", (provider, expected) => {
    expect(cliAuthProviderDocsHref(provider)).toBe(expected);
  });

  it("falls back to our own guide for a CLI that publishes none", () => {
    expect(cliAuthProviderDocsHref("dsh", "ja")).toBe(cliAuthDocsHref("ja"));
    expect(cliAuthProviderDocsHref("")).toBe(cliAuthDocsHref());
  });

  // The provider docs are absolute URLs, so a mistyped one is a broken link
  // rather than a 404 on our own site.
  it("only carries absolute https links", () => {
    for (const provider of ["claude", "codex", "copilot", "codearts", "omp"]) {
      expect(cliAuthProviderDocsHref(provider)).toMatch(/^https:\/\//);
    }
  });
});

// Every provider offered a button must be one the API accepts, and every one
// of them must have a documentation link for the terminal case too.
describe("CLI_AUTH_PROVIDERS", () => {
  it("mirrors the server table and documents each provider", () => {
    expect([...CLI_AUTH_PROVIDERS].sort()).toEqual([
      "claude",
      "codex",
      "copilot",
      "cursor-agent",
    ]);
    for (const provider of CLI_AUTH_PROVIDERS) {
      expect(cliAuthSupported(provider)).toBe(true);
      expect(cliAuthProviderDocsHref(provider)).not.toBe(cliAuthDocsHref());
    }
    expect(cliAuthSupported("opencode")).toBe(false);
    // Copilot signs out from an in-session slash command only.
    expect(cliAuthLogoutSupported("copilot")).toBe(false);
    expect(cliAuthLogoutSupported("claude")).toBe(true);
  });
});
