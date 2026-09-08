// @vitest-environment node
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// ---------------------------------------------------------------------------
// JEF-321 closing guard-rail.
//
// CLAUDE.md's "API Compatibility" rule requires every ApiClient method that
// returns parsed JSON to validate it through parseWithFallback (or a named
// wrapper around it, e.g. parseDaemonImportPreview) rather than casting
// `this.fetch<T>(...)` straight to T. This test enforces that mechanically:
// it extracts every `async name(...): Promise<T>` method in client.ts whose
// body calls `this.fetch`, and fails if T is a type worth validating (i.e.
// not void/Blob/Response/string/number/boolean/ArrayBuffer) and the body
// shows no sign of schema validation.
//
// `unknown` is deliberately NOT a blanket-skipped return type: a method
// typed `Promise<unknown>` still gets flagged unless it is named in
// UNKNOWN_RETURN_BY_DESIGN below, so a future method that is merely
// under-typed (rather than genuinely schema-less by design) cannot slip
// through unnoticed.
//
// "Sign of validation" is intentionally broader than the literal string
// "parseWithFallback(": any call matching /parse[A-Z]\w*(<...>)?\(/
// (parseWithFallback, parseWithFallback<Foo | null>, parseDaemonImportPreview,
// parseDaemonImportResult, ...) or containing "withParsed"
// (withParsedCompliance-style helpers) counts, since this codebase already
// uses both naming conventions for a schema-validating wrapper around
// parseWithFallback.
// ---------------------------------------------------------------------------

const CLIENT_PATH = fileURLToPath(new URL("./client.ts", import.meta.url));
const SKIP_RETURN_TYPES = new Set([
  "void",
  "Blob",
  "Response",
  "string",
  "number",
  "boolean",
  "ArrayBuffer",
]);

// Methods whose `Promise<unknown>` return type is a deliberate design choice,
// not a gap: the response shape is genuinely caller/plugin-defined, so there
// is no fixed schema to validate against. Nominative and commented per
// method — never widen this by adding "unknown" back to SKIP_RETURN_TYPES,
// which would silently exempt every future under-typed method too.
const UNKNOWN_RETURN_BY_DESIGN = new Set([
  // The plugin defines its own action response shape; the client is a
  // transparent proxy to whatever the plugin's HTTP handler returns.
  "callPluginAction",
  // The epic step's approval payload shape is defined per step kind
  // (workflow-specific), not by a fixed server contract.
  "approveProjectEpicStep",
]);

// Methods JEF-321 has not yet migrated as of this push (batch D, rebased
// onto batches A/B/C). These sit outside batch D's own domain (pins/squads/
// autopilots/VCS/Lark/Composio/Slack) — workspaces, members, invitations,
// tokens, chat sessions, inbox, business rules, attachments, agent tasks,
// runtimes. This list must shrink to [] as later JEF-321 work lands; a
// method landing here without being genuinely unmigrated is a regression,
// not expected drift — validate it instead of allow-listing it.
const ALLOW_LIST = new Set([
  "archiveAllInbox",
  "archiveAllReadInbox",
  "archiveCompletedInbox",
  "batchDeleteIssues",
  "cancelAgentTasks",
  "completeOIDCLogin",
  "createComment",
  "exportWorkspace",
  "getAttachmentTextContent",
  "importSkillArchive",
  "issueCliToken",
  "markAllInboxRead",
  "moveIssue",
  "quickCreateIssue",
  "retireModelKey",
  "unbindAgentsAndDeleteRuntime",
  "updateRuntime",
]);

interface MethodInfo {
  name: string;
  returnType: string;
  body: string;
}

function extractReturnType(header: string): string | null {
  const idx = header.indexOf("Promise<");
  if (idx === -1) return null;
  let depth = 0;
  let start = -1;
  for (let i = idx + "Promise<".length - 1; i < header.length; i++) {
    const ch = header[i];
    if (ch === "<") {
      if (depth === 0) start = i + 1;
      depth++;
    } else if (ch === ">") {
      depth--;
      if (depth === 0) {
        return header.slice(start, i).trim();
      }
    }
  }
  return null;
}

// Matches parseWithFallback(...), parseWithFallback<Foo | null>(...), and any
// custom parseXxx(...)/parseXxx<...>(...) wrapper (e.g. parseDaemonImportPreview).
const PARSE_CALL_RE = /\bparse[A-Z]\w*(?:<[^>]*>)?\(/;

function isValidated(body: string): boolean {
  return PARSE_CALL_RE.test(body) || body.includes("withParsed");
}

function extractApiClientMethods(source: string): MethodInfo[] {
  const classStart = source.indexOf("export class ApiClient {");
  if (classStart === -1) throw new Error("could not find `export class ApiClient {` in client.ts");
  const classBody = source.slice(classStart);

  const methodRe = /^ {2}async (\w+)\(/gm;
  const matches: { name: string; start: number }[] = [];
  let m: RegExpExecArray | null;
  while ((m = methodRe.exec(classBody)) !== null) {
    const name = m[1];
    if (!name) continue;
    matches.push({ name, start: m.index });
  }

  const methods: MethodInfo[] = [];
  for (let i = 0; i < matches.length; i++) {
    const current = matches[i];
    if (!current) continue;
    const { name, start } = current;
    const next = matches[i + 1];
    const end = next ? next.start : classBody.length;
    const slice = classBody.slice(start, end);
    const returnType = extractReturnType(slice);
    methods.push({ name, returnType: returnType ?? "", body: slice });
  }
  return methods;
}

describe("client.ts: every JSON-returning method validates its response", () => {
  const source = readFileSync(CLIENT_PATH, "utf-8");
  const methods = extractApiClientMethods(source);

  it("found a plausible number of ApiClient methods (sanity check on the extractor)", () => {
    // client.ts has hundreds of methods; a low count means the regex/class
    // boundary broke, not that the API surface shrank.
    expect(methods.length).toBeGreaterThan(200);
  });

  const candidates = methods.filter((meth) => {
    if (!meth.body.includes("this.fetch")) return false;
    if (SKIP_RETURN_TYPES.has(meth.returnType)) return false;
    return true;
  });

  it.each(candidates.map((meth) => [meth.name, meth] as const))(
    "%s validates its this.fetch response through a schema",
    (_name, meth) => {
      if (UNKNOWN_RETURN_BY_DESIGN.has(meth.name)) return; // deliberately schema-less — see comment above
      const validated = isValidated(meth.body);
      if (!validated && ALLOW_LIST.has(meth.name)) return; // known gap — see ALLOW_LIST comment
      expect(
        validated,
        `${meth.name} returns Promise<${meth.returnType}> from this.fetch without parseWithFallback/withParsed. ` +
          `Either validate the response through a schema (packages/core/api/schemas.ts), or add it to ` +
          `ALLOW_LIST in client-parse-guard.test.ts with a comment explaining why.`,
      ).toBe(true);
    },
  );

  it("ALLOW_LIST has no stale entries (every listed method still exists and is still unvalidated)", () => {
    const byName = new Map(methods.map((meth) => [meth.name, meth]));
    for (const name of ALLOW_LIST) {
      const meth = byName.get(name);
      expect(meth, `${name} is in ALLOW_LIST but no longer exists in client.ts — remove it`).toBeDefined();
      if (!meth) continue;
      const validated = isValidated(meth.body);
      expect(
        validated,
        `${name} is in ALLOW_LIST but is now validated — remove it from ALLOW_LIST`,
      ).toBe(false);
    }
  });

  it("UNKNOWN_RETURN_BY_DESIGN has no stale entries (still exists and still returns unknown)", () => {
    const byName = new Map(methods.map((meth) => [meth.name, meth]));
    for (const name of UNKNOWN_RETURN_BY_DESIGN) {
      const meth = byName.get(name);
      expect(meth, `${name} is in UNKNOWN_RETURN_BY_DESIGN but no longer exists in client.ts — remove it`).toBeDefined();
      if (!meth) continue;
      expect(
        meth.returnType,
        `${name} no longer returns Promise<unknown> — remove it from UNKNOWN_RETURN_BY_DESIGN and validate it like any other method`,
      ).toBe("unknown");
    }
  });
});
