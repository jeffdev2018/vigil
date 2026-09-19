import { describe, expect, it, vi } from "vitest";

vi.mock("@/lib/use-cases-source", () => ({
  getUseCasePagesForLocale: (locale: string) => {
    expect(locale).toBe("en");
    return [
      { slugs: ["support-tickets"], data: { updated_at: "2026-05-01" } },
      { slugs: ["deep", "nested-slug"], data: {} },
    ];
  },
}));

import sitemap from "./sitemap";

describe("sitemap", () => {
  it("lists /download and /usecases, statically and per use-case page", () => {
    const urls = sitemap().map((entry) => entry.url);

    expect(urls).toContain("https://www.multica.ai/download");
    expect(urls).toContain("https://www.multica.ai/usecases");
    expect(urls).toContain("https://www.multica.ai/usecases/support-tickets");
    expect(urls).toContain("https://www.multica.ai/usecases/deep/nested-slug");
  });

  it("uses the use-case's own updated_at as lastModified when present", () => {
    const entries = sitemap();
    const entry = entries.find((e) => e.url.endsWith("/usecases/support-tickets"));
    expect(entry?.lastModified).toEqual(new Date("2026-05-01"));
  });
});
