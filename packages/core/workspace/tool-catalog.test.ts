// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { McpCatalogTool, WorkspaceMcpServer } from "../types";
import {
  buildCatalogRows,
  filterCatalogRows,
  foldText,
  type CatalogRow,
} from "./tool-catalog";

function server(id: string, name: string): WorkspaceMcpServer {
  return {
    id,
    workspace_id: "ws-1",
    name,
    transport: "http",
    tool_count: 0,
    created_at: "",
    updated_at: "",
  };
}

function tool(name: string, over: Partial<McpCatalogTool> = {}): McpCatalogTool {
  return { name, risk: "read", risk_source: "auto", ...over };
}

const linear = server("srv-1", "linear");
const stripe = server("srv-2", "stripe");

const rows = buildCatalogRows(
  [linear, stripe],
  new Map([
    [
      "srv-1",
      [
        tool("list_issues", { description: "Liste les tâches créées" }),
        tool("create_issue", { risk: "internal_write" }),
      ],
    ],
    ["srv-2", [tool("refund", { risk: "external_effect", description: "Rembourse" })]],
  ]),
);

const names = (result: CatalogRow[]) => result.map((row) => row.tool.name);

describe("buildCatalogRows", () => {
  it("keeps the server order and attaches each tool's server", () => {
    expect(names(rows)).toEqual(["list_issues", "create_issue", "refund"]);
    expect(rows[2]?.server.name).toBe("stripe");
  });

  it("contributes no row for a server whose catalogue has not arrived", () => {
    expect(buildCatalogRows([linear, stripe], new Map())).toEqual([]);
  });
});

describe("foldText", () => {
  it("ignores case and accents", () => {
    expect(foldText("Résumé")).toBe("resume");
    expect(foldText("CRÉER")).toBe("creer");
  });
});

describe("filterCatalogRows", () => {
  it("returns everything with no filter", () => {
    expect(filterCatalogRows(rows, {})).toHaveLength(3);
  });

  it("matches the name and the description, case- and accent-insensitively", () => {
    expect(names(filterCatalogRows(rows, { query: "ISSUE" }))).toEqual([
      "list_issues",
      "create_issue",
    ]);
    // Unaccented input finds an accented description, and the query is trimmed.
    expect(names(filterCatalogRows(rows, { query: "  taches CREEES " }))).toEqual([
      "list_issues",
    ]);
    expect(names(filterCatalogRows(rows, { query: "nothing here" }))).toEqual([]);
  });

  it("filters by server and by risk, cumulatively", () => {
    expect(names(filterCatalogRows(rows, { serverId: "srv-1" }))).toEqual([
      "list_issues",
      "create_issue",
    ]);
    expect(names(filterCatalogRows(rows, { risk: "external_effect" }))).toEqual([
      "refund",
    ]);
    // Cumulative: the risk exists, but not on that server.
    expect(
      names(filterCatalogRows(rows, { serverId: "srv-1", risk: "external_effect" })),
    ).toEqual([]);
    expect(
      names(
        filterCatalogRows(rows, {
          serverId: "srv-1",
          risk: "internal_write",
          query: "create",
        }),
      ),
    ).toEqual(["create_issue"]);
  });

  it("does not crash on a tool with no description", () => {
    expect(names(filterCatalogRows(rows, { query: "create" }))).toEqual([
      "create_issue",
    ]);
  });
});
