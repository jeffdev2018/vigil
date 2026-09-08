// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import ts from "typescript";

// ---------------------------------------------------------------------------
// Orphan components in packages/views.
//
// Four finished components (RuntimeMachineFilterDropdown, SkillAttach,
// QuickAgentBar, ConcurrencyPicker) sat in this package for months rendering
// for nobody. Each survived repo-wide refactors precisely because nothing
// complains, and each had a green test file of its own — which reads as
// evidence the component works, not as evidence anyone can reach it.
//
// Nothing else in the repo covers this:
//   * knip's config sets `include: ["files", "dependencies", "unlisted"]` and
//     deliberately omits "exports" (see knip.jsonc), because views is consumed
//     through per-file `exports` maps. It reports dead FILES, never a dead
//     export inside a live file — and every orphan above lived in a file whose
//     other exports were fine, or in a file with a live sibling.
//   * scripts/check-ui-wildcard-exports.mjs is file-level too, and scoped to
//     packages/ui.
//   * packages/eslint-config carries no rule of this kind; ESLint sees one
//     file at a time and cannot answer "does anything render this".
//
// LIVENESS IS REFERENCE, NOT IMPORT. A component rendered by its own module
// (a row cell, a sub-panel) is exported only so its co-located test can reach
// it; that export is a test seam, not an orphan. What makes the four above
// orphans is that NO production code path — in their own file or any other —
// mentions them at all.
//
// Edges come from the TypeScript parser, never from a text scan: a
// commented-out import is exactly the state a component passes through when
// its last real caller is removed, and `CreateAgentDialog` below is mentioned
// in four comments and rendered by nothing.
// ---------------------------------------------------------------------------

const VIEWS_ROOT = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(VIEWS_ROOT, "..", "..");
const PKG = "@multica/views";

const SOURCE_EXTENSIONS = [".ts", ".tsx"];
const SKIP_DIRS = new Set([
  "node_modules",
  ".git",
  ".next",
  ".turbo",
  "out",
  "dist",
  "build",
  // JSON bundles only.
  "locales",
]);

/**
 * Importers are resolved across the three consumers of this package. Mobile is
 * excluded on purpose: it is independent (see CLAUDE.md "Mobile Rules") and
 * shares only `@multica/core` types and pure functions, so it can never be the
 * reason a views component is live.
 */
const SCAN_ROOTS = [
  join(REPO_ROOT, "packages"),
  join(REPO_ROOT, "apps", "web"),
  join(REPO_ROOT, "apps", "desktop"),
];

/**
 * Known orphans this packet did not arbitrate. Each one is a real finding, not
 * an exemption: nothing renders it, and its fate (mount it where it belongs,
 * or delete it) is a product call rather than a cleanup. Keep this list
 * shrinking — an entry that cannot name a reason belongs in a diff, not here.
 */
const ALLOWED: Record<string, string> = {
  "agents/components/agent-activity-hover-content.tsx:WorkspaceAgentActivityHoverContent":
    "Workspace-wide variant of the agent activity hover card; the per-agent variant in the same file is live, this one is reached by nothing but its own test.",
  "agents/components/create-agent-dialog.tsx:CreateAgentDialog":
    "Superseded by the /agents/new page flow (paths.newAgent); the four remaining mentions in the repo are all prose in other components' comments.",
  "agents/components/inspector/thinking-prop-row.tsx:ThinkingPropRow":
    "Chip form of the thinking-level control; the inspector rebuild moved to ThinkingSettingField from the same file, which is live.",
  "agents/components/inspector/visibility-picker.tsx:VisibilityPicker":
    "Interactive visibility control; the agent detail page renders the read-only VisibilityBadge instead, whose comment still points at this picker.",
  "agents/components/tabs/memory-tab.tsx:TeachFromRunButton":
    "Run-sourced twin of TeachFromReviewButton, which the issue delivery section does render; #192 shipped it with its test but never mounted it on a run surface.",
  "runtimes/components/shared.tsx:TokenCard":
    "Runtime-detail KPI tile superseded by KpiCard in the same file; its only mention outside the file is a comment in the web landing page.",
};

function walk(dir: string, out: string[] = []): string[] {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (entry.name.startsWith(".")) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!SKIP_DIRS.has(entry.name)) walk(full, out);
    } else if (SOURCE_EXTENSIONS.some((ext) => entry.name.endsWith(ext))) {
      out.push(full);
    }
  }
  return out;
}

function parse(file: string): ts.SourceFile {
  return ts.createSourceFile(
    file,
    readFileSync(file, "utf8"),
    ts.ScriptTarget.Latest,
    true,
  );
}

const isPascalCase = (name: string) => /^[A-Z][A-Za-z0-9]*$/.test(name);

const hasModifier = (node: ts.Node, kind: ts.SyntaxKind) =>
  ts.canHaveModifiers(node) &&
  ts.getModifiers(node)?.some((m) => m.kind === kind) === true;

/**
 * PascalCase VALUE exports of a module. `export type` / `export interface` and
 * type-only specifiers are skipped — a type has no render site to check.
 */
function exportedComponents(source: ts.SourceFile): Set<string> {
  const names = new Set<string>();
  const isExported = (node: ts.Node) =>
    hasModifier(node, ts.SyntaxKind.ExportKeyword);

  for (const statement of source.statements) {
    if (
      (ts.isFunctionDeclaration(statement) || ts.isClassDeclaration(statement)) &&
      isExported(statement) &&
      statement.name &&
      isPascalCase(statement.name.text)
    ) {
      names.add(statement.name.text);
    } else if (ts.isVariableStatement(statement) && isExported(statement)) {
      for (const decl of statement.declarationList.declarations) {
        if (ts.isIdentifier(decl.name) && isPascalCase(decl.name.text)) {
          names.add(decl.name.text);
        }
      }
    } else if (
      ts.isExportDeclaration(statement) &&
      !statement.isTypeOnly &&
      statement.exportClause &&
      ts.isNamedExports(statement.exportClause)
    ) {
      for (const el of statement.exportClause.elements) {
        if (!el.isTypeOnly && isPascalCase(el.name.text)) names.add(el.name.text);
      }
    }
  }
  return names;
}

/** Names this module default-exports, in either syntax. */
function defaultExported(source: ts.SourceFile): Set<string> {
  const names = new Set<string>();
  for (const statement of source.statements) {
    if (
      ts.isExportAssignment(statement) &&
      !statement.isExportEquals &&
      ts.isIdentifier(statement.expression)
    ) {
      names.add(statement.expression.text);
    }
    if (
      hasModifier(statement, ts.SyntaxKind.DefaultKeyword) &&
      (ts.isFunctionDeclaration(statement) || ts.isClassDeclaration(statement)) &&
      statement.name
    ) {
      names.add(statement.name.text);
    }
  }
  return names;
}

/**
 * Identifiers the module mentions somewhere other than its own declaration or
 * export syntax — i.e. the components it actually renders. Declaration NAME
 * nodes and import/export specifiers are skipped so a symbol never counts as
 * its own user.
 */
function selfReferenced(source: ts.SourceFile): Set<string> {
  const hit = new Set<string>();
  const visit = (node: ts.Node): void => {
    if (
      (ts.isFunctionDeclaration(node) || ts.isClassDeclaration(node)) &&
      node.name
    ) {
      ts.forEachChild(node, (child) => {
        if (child !== node.name) visit(child);
      });
      return;
    }
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name)) {
      if (node.type) visit(node.type);
      if (node.initializer) visit(node.initializer);
      return;
    }
    if (ts.isExportSpecifier(node) || ts.isImportSpecifier(node)) return;
    if (ts.isIdentifier(node)) {
      hit.add(node.text);
      return;
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return hit;
}

type Edge = { specifier: string; names: string[] | null };

/** Runtime import/re-export edges. `names: null` means a namespace/star form. */
function importEdges(source: ts.SourceFile): Edge[] {
  const edges: Edge[] = [];
  const visit = (node: ts.Node): void => {
    if (
      ts.isImportDeclaration(node) &&
      ts.isStringLiteral(node.moduleSpecifier)
    ) {
      const specifier = node.moduleSpecifier.text;
      const clause = node.importClause;
      if (!clause) {
        edges.push({ specifier, names: null });
      } else if (!clause.isTypeOnly) {
        if (clause.namedBindings && ts.isNamespaceImport(clause.namedBindings)) {
          edges.push({ specifier, names: null });
        } else {
          const names: string[] = [];
          if (clause.name) names.push("default");
          if (clause.namedBindings && ts.isNamedImports(clause.namedBindings)) {
            for (const el of clause.namedBindings.elements) {
              if (!el.isTypeOnly) names.push((el.propertyName ?? el.name).text);
            }
          }
          edges.push({ specifier, names });
        }
      }
    } else if (
      ts.isExportDeclaration(node) &&
      node.moduleSpecifier &&
      ts.isStringLiteral(node.moduleSpecifier) &&
      !node.isTypeOnly
    ) {
      const specifier = node.moduleSpecifier.text;
      if (!node.exportClause) {
        edges.push({ specifier, names: null });
      } else if (ts.isNamedExports(node.exportClause)) {
        const names: string[] = [];
        for (const el of node.exportClause.elements) {
          if (!el.isTypeOnly) names.push((el.propertyName ?? el.name).text);
        }
        edges.push({ specifier, names });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return edges;
}

const ORPHAN_SCAN_TIMEOUT_MS = 60_000;

describe("packages/views components are mounted somewhere", () => {
  // The scan parses every source file in the package with the TypeScript
  // compiler. It runs in ~2s on a laptop and took 7s on a loaded CI shard,
  // past vitest's default 5s — the budget is for the machine, not the code.
  it("every exported component is rendered by production code", () => {
    const files = SCAN_ROOTS.flatMap((root) => walk(root));
    const known = new Set(files);

    // `@multica/views/<subpath>` resolves through the manifest, which names a
    // specific file for all but the `locales/*` wildcard.
    const manifest = JSON.parse(
      readFileSync(join(VIEWS_ROOT, "package.json"), "utf8"),
    ) as { exports?: Record<string, string> };
    const subpathToFile = new Map<string, string>();
    for (const [subpath, target] of Object.entries(manifest.exports ?? {})) {
      if (subpath.includes("*")) continue;
      subpathToFile.set(
        subpath.replace(/^\.\//, ""),
        join(VIEWS_ROOT, target.replace(/^\.\//, "")),
      );
    }

    const resolveFile = (base: string): string | null => {
      const candidates = [
        base,
        ...SOURCE_EXTENSIONS.flatMap((ext) => [
          `${base}${ext}`,
          join(base, `index${ext}`),
        ]),
      ];
      return candidates.find((c) => known.has(c)) ?? null;
    };

    const resolveSpecifier = (
      specifier: string,
      importer: string,
    ): string | null => {
      // Resolved against the importer's own directory, never by basename:
      // this package has many same-named files across domains.
      if (specifier.startsWith(".")) {
        return resolveFile(resolve(dirname(importer), specifier));
      }
      if (specifier === PKG) return resolveFile(join(VIEWS_ROOT, "index"));
      if (specifier.startsWith(`${PKG}/`)) {
        const subpath = specifier.slice(PKG.length + 1);
        return subpathToFile.get(subpath) ?? resolveFile(join(VIEWS_ROOT, subpath));
      }
      return null;
    };

    const isTestFile = (file: string) => /\.(test|spec)\.tsx?$/.test(file);

    // Candidates: PascalCase exports of non-test .tsx in this package. Test
    // helpers under views/test/ exist for tests by definition.
    const testHelperDir = join(VIEWS_ROOT, "test") + sep;
    const candidates = new Map<string, Set<string>>();
    const parsed = new Map<string, ts.SourceFile>();
    for (const file of files) {
      if (!file.startsWith(VIEWS_ROOT + sep)) continue;
      if (!file.endsWith(".tsx") || isTestFile(file)) continue;
      if (file.startsWith(testHelperDir)) continue;
      const source = parse(file);
      parsed.set(file, source);
      const names = exportedComponents(source);
      if (names.size > 0) candidates.set(file, names);
    }

    // Names each target file has imported FROM PRODUCTION code. A mention in a
    // test is not a mount — counting it is what lets a component outlive its
    // last real use.
    const usedByOthers = new Map<string, Set<string>>();
    const starImported = new Set<string>();
    for (const file of files) {
      if (isTestFile(file)) continue;
      const source = parsed.get(file) ?? parse(file);
      for (const { specifier, names } of importEdges(source)) {
        const target = resolveSpecifier(specifier, file);
        if (!target) continue;
        if (names === null) {
          starImported.add(target);
          continue;
        }
        let set = usedByOthers.get(target);
        if (!set) usedByOthers.set(target, (set = new Set()));
        for (const name of names) set.add(name);
      }
    }

    const orphans: string[] = [];
    for (const [file, names] of candidates) {
      // A star import/re-export pulls the whole module: nothing here can tell
      // which of its exports the consumer touches.
      if (starImported.has(file)) continue;
      const source = parsed.get(file)!;
      const imported = usedByOthers.get(file) ?? new Set<string>();
      const self = selfReferenced(source);
      const asDefault = defaultExported(source);
      for (const name of names) {
        if (imported.has(name)) continue;
        if (self.has(name)) continue;
        // `export default X` reached through a `default` import.
        if (asDefault.has(name) && imported.has("default")) continue;
        const key = `${relative(VIEWS_ROOT, file).split(sep).join("/")}:${name}`;
        if (key in ALLOWED) continue;
        orphans.push(key);
      }
    }
    orphans.sort();

    expect(
      orphans,
      `Exported from packages/views and rendered by nothing:\n` +
        orphans.map((o) => `  ${o}`).join("\n") +
        `\n\nMount it where it belongs, or delete it. If it is a deliberate ` +
        `exception, add it to ALLOWED in this file with the reason on its line.`,
    ).toEqual([]);
  }, ORPHAN_SCAN_TIMEOUT_MS);

  it("every allow-list entry is still an orphan and still carries a reason", () => {
    // An allow-list that outlives its entries is how this check goes quiet.
    for (const [key, reason] of Object.entries(ALLOWED)) {
      const separator = key.lastIndexOf(":");
      const relPath = key.slice(0, separator);
      const name = key.slice(separator + 1);
      expect(reason.trim().length, `${key} needs a reason`).toBeGreaterThan(20);
      const file = join(VIEWS_ROOT, relPath);
      const names = exportedComponents(parse(file));
      expect(
        names.has(name),
        `${key} is allow-listed but ${relPath} no longer exports ${name} — drop the entry.`,
      ).toBe(true);
    }
  });
});
