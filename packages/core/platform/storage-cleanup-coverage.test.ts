// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { clearWorkspaceStorage } from "./storage-cleanup";

// Logout and workspace deletion only clear the persisted keys they know about.
// A workspace-scoped persist store that is neither listed in
// WORKSPACE_SCOPED_KEYS nor registered as a draft survives on a shared device.
// This reads every store that persists through createWorkspaceAwareStorage and
// fails the day one is added without being declared.

const CORE_ROOT = fileURLToPath(new URL("..", import.meta.url));

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    if (entry.name === "node_modules") return [];
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return /\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) ? [path] : [];
  });
}

function persistNames(source: string): string[] {
  const names = [
    ...[...source.matchAll(/\bname:\s*"(multica[^"]+)"/g)].map((m) => m[1]!),
    ...[...source.matchAll(/viewStorePersistOptions\("([^"]+)"\)/g)].map((m) => m[1]!),
  ];
  for (const m of source.matchAll(/\bname:\s*([A-Z_][A-Z0-9_]*)\b/g)) {
    const value = source.match(new RegExp(`\\b${m[1]}\\s*=\\s*"([^"]+)"`));
    if (value) names.push(value[1]!);
  }
  return names;
}

describe("workspace storage cleanup coverage", () => {
  it("clears every workspace-scoped persist store", () => {
    const names = sourceFiles(CORE_ROOT)
      .map((file) => readFileSync(file, "utf8"))
      .filter((source) => source.includes("createWorkspaceAwareStorage(") && source.includes("persist"))
      .flatMap(persistNames);
    expect(names.length).toBeGreaterThan(10);

    const removed = new Set<string>();
    clearWorkspaceStorage(
      { getItem: () => null, setItem: () => {}, removeItem: (key) => removed.add(key) },
      "acme",
    );
    expect([...new Set(names)].filter((name) => !removed.has(`${name}:acme`))).toEqual([]);
  });
});
