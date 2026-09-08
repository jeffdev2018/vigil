// @vitest-environment node
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  extensionToLanguage,
  getPreviewKind,
  isPreviewable,
  type PreviewKind,
} from "./preview";

describe("getPreviewKind", () => {
  const cases: Array<[string, string, PreviewKind | null]> = [
    // Media types — typed correctly server-side
    ["application/pdf", "manual.pdf", "pdf"],
    ["video/mp4", "clip.mp4", "video"],
    ["audio/mpeg", "note.mp3", "audio"],

    // Markdown — both well-typed and sniffer-fallback paths
    ["text/markdown", "README", "markdown"],
    ["text/plain", "README.md", "markdown"],
    ["application/octet-stream", "notes.markdown", "markdown"],

    // HTML — both content-type and extension paths
    ["text/html", "page", "html"],
    ["application/octet-stream", "page.html", "html"],

    // Code / config — fallback to text after sniffer guesses "text/plain"
    ["text/plain", "main.go", "text"],
    ["application/octet-stream", "main.go", "text"],
    ["text/plain", "config.yml", "text"],
    ["application/javascript", "bundle.js", "text"],
    ["application/json", "data.json", "text"],

    // Plain text
    ["text/plain", "log.txt", "text"],

    // Build files without extension
    ["application/octet-stream", "Dockerfile", "text"],
    ["application/octet-stream", "Makefile", "text"],
    ["application/octet-stream", ".env", "text"],
    ["application/octet-stream", ".gitignore", "text"],
    ["application/octet-stream", "service.dockerfile", "text"],
    ["application/octet-stream", "rules.makefile", "text"],

    // Out of scope
    ["application/vnd.openxmlformats-officedocument.wordprocessingml.document", "report.docx", null],
    ["application/octet-stream", "blob.bin", null],
    ["application/zip", "archive.zip", null],
  ];

  for (const [ct, filename, want] of cases) {
    it(`(${ct}, ${filename}) → ${want}`, () => {
      expect(getPreviewKind(ct, filename)).toBe(want);
    });
  }

  // PDF should dispatch from extension alone when content_type is wrong.
  it("falls through to extension when content_type is mislabeled", () => {
    expect(getPreviewKind("application/octet-stream", "manual.pdf")).toBe("pdf");
  });
});

describe("isPreviewable", () => {
  it("is true for any non-null PreviewKind", () => {
    expect(isPreviewable("application/pdf", "x.pdf")).toBe(true);
    expect(isPreviewable("text/plain", "x.txt")).toBe(true);
  });

  it("is false for unsupported types", () => {
    expect(isPreviewable("application/zip", "x.zip")).toBe(false);
    expect(isPreviewable("application/octet-stream", "x.bin")).toBe(false);
  });
});

describe("extensionToLanguage", () => {
  it("maps common code extensions to hljs language tokens", () => {
    expect(extensionToLanguage("index.ts")).toBe("typescript");
    expect(extensionToLanguage("main.go")).toBe("go");
    expect(extensionToLanguage("script.py")).toBe("python");
    expect(extensionToLanguage("style.scss")).toBe("scss");
  });

  it("falls back to plaintext for non-code text files", () => {
    expect(extensionToLanguage("log.txt")).toBe("plaintext");
  });

  it("recognizes extension-less build files", () => {
    expect(extensionToLanguage("Dockerfile")).toBe("dockerfile");
    expect(extensionToLanguage("Makefile")).toBe("makefile");
    expect(extensionToLanguage(".env")).toBe("plaintext");
    expect(extensionToLanguage(".gitignore")).toBe("plaintext");
  });

  it("recognizes build file extensions allowed by the server", () => {
    expect(extensionToLanguage("service.dockerfile")).toBe("dockerfile");
    expect(extensionToLanguage("rules.makefile")).toBe("makefile");
    expect(extensionToLanguage("nested/.gitignore")).toBe("plaintext");
  });

  it("returns undefined for unknown extensions", () => {
    expect(extensionToLanguage("blob.bin")).toBeUndefined();
    expect(extensionToLanguage("noextension")).toBeUndefined();
  });
});

// The proxy (server/internal/handler/file.go, isTextPreviewable) and this
// module each keep their own extension list; the comment above
// TEXT_EXTENSIONS asks for them to be kept in sync by hand. This is the check
// that hand forgot: read both sources and diff the sets, so a drift fails
// here instead of as a 415 in the preview modal or a missing Eye button.
describe("text extensions stay in sync with the Go proxy", () => {
  const read = (rel: string) => readFileSync(join(dirname(fileURLToPath(import.meta.url)), rel), "utf8");
  const goSource = read("../../../../server/internal/handler/file.go");
  const tsSource = read("./preview.ts");

  const goExtensions = (() => {
    const m = /ext := strings\.ToLower\(path\.Ext\(filename\)\)\s*switch ext \{\s*case ([\s\S]*?):\s*return true/.exec(goSource);
    if (!m) throw new Error("extension switch not found in file.go");
    return new Set([...m[1].matchAll(/"\.([a-z0-9]+)"/g)].map((x) => x[1]));
  })();
  const tsExtensions = (() => {
    const m = /const TEXT_EXTENSIONS = new Set<string>\(\[([\s\S]*?)\]\)/.exec(tsSource);
    if (!m) throw new Error("TEXT_EXTENSIONS not found in preview.ts");
    return new Set([...m[1].matchAll(/"([a-z0-9]+)"/g)].map((x) => x[1]));
  })();

  it("both lists were found and are non-trivial", () => {
    expect(goExtensions.size).toBeGreaterThan(20);
    expect(tsExtensions.size).toBeGreaterThan(20);
  });

  it("every extension the proxy serves is previewable here, and vice versa", () => {
    const onlyGo = [...goExtensions].filter((e) => !tsExtensions.has(e));
    const onlyTs = [...tsExtensions].filter((e) => !goExtensions.has(e));
    expect({ onlyGo, onlyTs }).toEqual({ onlyGo: [], onlyTs: [] });
  });
});
