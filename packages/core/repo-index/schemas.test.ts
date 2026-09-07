// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  REPO_INDEX_EMPTY_SETTINGS,
  RepoIndexSettingsSchema,
  repoIndexState,
  shortCommit,
  type RepoIndexRepo,
} from "./schemas";
import { parseWithFallback } from "../api/schema";

// Canonical layer for the repo-index settings contract (K47). The component
// suite (packages/views/settings/components/repo-index-section.test.tsx) keeps
// the wiring and the happy path; the parsing matrix lives here.

const repo = (over: Partial<RepoIndexRepo> = {}): RepoIndexRepo => ({
  repo_identifier: "git@example.com:team/app.git",
  enabled: true,
  chunk_count: 12,
  file_count: 3,
  last_indexed_commit: "abc1234def",
  last_indexed_at: "2026-01-02T03:04:05Z",
  unusable_embedding_count: 0,
  ...over,
});

describe("RepoIndexSettingsSchema", () => {
  it("parses a well-formed response", () => {
    const parsed = parseWithFallback(
      { repos: [repo()], embeddings_enabled: true },
      RepoIndexSettingsSchema,
      REPO_INDEX_EMPTY_SETTINGS,
      { endpoint: "test" },
    );
    expect(parsed.repos).toHaveLength(1);
    expect(parsed.repos[0]?.chunk_count).toBe(12);
    expect(parsed.embeddings_enabled).toBe(true);
  });

  it("falls back rather than throwing on a malformed response", () => {
    // The desktop client can outlive a backend field; a drifted shape must
    // degrade the block, not white-screen the settings page.
    for (const malformed of [null, "nope", 42, { repos: "not an array" }]) {
      const parsed = parseWithFallback(
        malformed,
        RepoIndexSettingsSchema,
        REPO_INDEX_EMPTY_SETTINGS,
        { endpoint: "test" },
      );
      expect(parsed.repos).toEqual([]);
      expect(parsed.embeddings_enabled).toBe(false);
    }
  });

  it("keeps a row whose optional fields are missing or wrong-typed", () => {
    const parsed = parseWithFallback(
      { repos: [{ repo_identifier: "git@x:a.git", chunk_count: "many" }] },
      RepoIndexSettingsSchema,
      REPO_INDEX_EMPTY_SETTINGS,
      { endpoint: "test" },
    );
    expect(parsed.repos[0]?.repo_identifier).toBe("git@x:a.git");
    expect(parsed.repos[0]?.chunk_count).toBe(0);
    expect(parsed.repos[0]?.enabled).toBe(false);
  });
});

describe("repoIndexState", () => {
  it("separates disabled, waiting and indexed", () => {
    expect(repoIndexState(repo({ enabled: false }))).toBe("disabled");
    // Enabled with nothing stored is the real gap between flipping the toggle
    // and the first run finishing, not an error state.
    expect(repoIndexState(repo({ chunk_count: 0 }))).toBe("indexing");
    expect(repoIndexState(repo())).toBe("indexed");
  });

  it("reads a missing or non-boolean enabled as disabled", () => {
    expect(repoIndexState({ ...repo(), enabled: undefined as unknown as boolean })).toBe("disabled");
    expect(repoIndexState(undefined as unknown as RepoIndexRepo)).toBe("disabled");
  });
});

describe("shortCommit", () => {
  it("shortens and tolerates an empty commit", () => {
    expect(shortCommit("abc1234def5678")).toBe("abc1234");
    expect(shortCommit("")).toBe("");
    expect(shortCommit(undefined as unknown as string)).toBe("");
  });
});

describe("RepoIndexRepoSchema: unusable embeddings", () => {
  it("reports how many chunks the current embedding model cannot compare against", () => {
    const parsed = parseWithFallback(
      { repos: [repo({ unusable_embedding_count: 4 })], embeddings_enabled: true },
      RepoIndexSettingsSchema,
      REPO_INDEX_EMPTY_SETTINGS,
      { endpoint: "test" },
    );
    expect(parsed.repos[0]?.unusable_embedding_count).toBe(4);
  });

  it("reads an older backend that omits the field as nothing unusable", () => {
    // The field arrived with the embedding-model column; a desktop client
    // talking to a server from before it must not render "NaN chunks".
    const { unusable_embedding_count: _omitted, ...older } = repo();
    const parsed = parseWithFallback(
      { repos: [older], embeddings_enabled: true },
      RepoIndexSettingsSchema,
      REPO_INDEX_EMPTY_SETTINGS,
      { endpoint: "test" },
    );
    expect(parsed.repos[0]?.unusable_embedding_count).toBe(0);
  });
});
