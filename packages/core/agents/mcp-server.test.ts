// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  MCPServerCatalogToolSchema,
  MCPServerSettingsEnvelopeSchema,
  MCPServerSettingsSchema,
} from "../api/schemas";

// Vigil as an MCP server (OS plan, chantier 1). A malformed or drifted
// response must degrade to "server off, no overrides" rather than throw —
// an exception here would white-screen the settings tab, and a partially
// parsed override map must never be read as a real tightening.

describe("MCPServerSettingsSchema", () => {
  it("fills in every field a sparse response omits", () => {
    const parsed = MCPServerSettingsSchema.parse({});
    expect(parsed.enabled).toBe(true);
    expect(parsed.default_surface).toBe("compound");
    expect(parsed.tools).toEqual({});
  });

  it("keeps a valid per-tool override map", () => {
    const parsed = MCPServerSettingsSchema.parse({
      enabled: false,
      default_surface: "granular",
      tools: { issue_create: "ask", note_delete: "deny" },
    });
    expect(parsed.enabled).toBe(false);
    expect(parsed.default_surface).toBe("granular");
    expect(parsed.tools).toEqual({ issue_create: "ask", note_delete: "deny" });
  });

  it("falls back to compound for an unknown surface", () => {
    const parsed = MCPServerSettingsSchema.parse({ default_surface: "nope" });
    expect(parsed.default_surface).toBe("compound");
  });

  it("degrades the whole override map to empty when one decision is invalid", () => {
    const parsed = MCPServerSettingsSchema.parse({
      tools: { issue_create: "ask", issue_delete: "obliterate" },
    });
    expect(parsed.tools).toEqual({});
  });
});

describe("MCPServerCatalogToolSchema", () => {
  it("falls back to an unknown risk rather than throwing", () => {
    const parsed = MCPServerCatalogToolSchema.parse({ name: "issue_create", risk: 42 });
    expect(parsed.risk).toBe("unknown");
    expect(parsed.agent_only).toBe(false);
  });
});

describe("MCPServerSettingsEnvelopeSchema", () => {
  it("falls back rather than throwing on a wholly malformed response", () => {
    for (const bad of [null, "nope", 42, []]) {
      const result = MCPServerSettingsEnvelopeSchema.safeParse(bad);
      expect(result.success).toBe(false);
    }
  });

  it("degrades a malformed catalogue to an empty list, not a thrown error", () => {
    const parsed = MCPServerSettingsEnvelopeSchema.parse({
      settings: { enabled: true, default_surface: "compound", tools: {} },
      tools: "not-a-list",
      endpoint: "/api/mcp/acme",
    });
    expect(parsed.tools).toEqual([]);
    expect(parsed.endpoint).toBe("/api/mcp/acme");
  });

  it("keeps fields a newer server added", () => {
    const parsed = MCPServerSettingsEnvelopeSchema.parse({
      settings: { enabled: true, default_surface: "compound", tools: {} },
      tools: [],
      endpoint: "/api/mcp/acme",
      future_field: "x",
    });
    expect((parsed as Record<string, unknown>).future_field).toBe("x");
  });
});
