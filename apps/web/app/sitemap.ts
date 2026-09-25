import type { MetadataRoute } from "next";
import { getUseCasePagesForLocale } from "@/lib/use-cases-source";

type UseCaseFrontmatter = { updated_at?: string };

export default function sitemap(): MetadataRoute.Sitemap {
  const baseUrl = "https://www.multica.ai";

  // Default (English) locale only — hideLocale: "default-locale" serves it
  // without a locale prefix, so it's the one canonical set of URLs a
  // sitemap should list (matches canonical metadata on both use-case pages).
  const useCasePages = getUseCasePagesForLocale("en");

  return [
    {
      url: baseUrl,
      lastModified: new Date("2026-04-01"),
      changeFrequency: "weekly",
      priority: 1.0,
    },
    {
      url: `${baseUrl}/about`,
      lastModified: new Date("2026-04-01"),
      changeFrequency: "monthly",
      priority: 0.7,
    },
    {
      url: `${baseUrl}/changelog`,
      lastModified: new Date("2026-04-01"),
      changeFrequency: "weekly",
      priority: 0.6,
    },
    {
      url: `${baseUrl}/contact-sales`,
      lastModified: new Date("2026-05-21"),
      changeFrequency: "monthly",
      priority: 0.7,
    },
    {
      url: `${baseUrl}/download`,
      lastModified: new Date("2026-04-01"),
      changeFrequency: "weekly",
      priority: 0.8,
    },
    {
      url: `${baseUrl}/usecases`,
      lastModified: new Date("2026-04-01"),
      changeFrequency: "weekly",
      priority: 0.7,
    },
    ...useCasePages.map((page) => {
      const data = page.data as UseCaseFrontmatter;
      return {
        url: `${baseUrl}/usecases/${page.slugs.join("/")}`,
        lastModified: data.updated_at ? new Date(data.updated_at) : new Date("2026-04-01"),
        changeFrequency: "monthly" as const,
        priority: 0.6,
      };
    }),
  ];
}
