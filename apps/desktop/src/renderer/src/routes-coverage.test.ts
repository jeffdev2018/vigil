// @vitest-environment node
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { WORKSPACE_PAGES, type WorkspacePageKey } from "@multica/core/paths";

// The sidebar is shared between web and desktop, so a page wired on web only
// leaves every desktop tab pointing at a route that does not exist — the
// "this page does not exist" report on Roadmap (#393) and again on the next
// page (#397). This guard is why a third one should not happen.
//
// Ceiling: it reads routes.tsx as TEXT rather than importing appRoutes, which
// would pull the whole views tree (and Electron preload types) into a unit
// test. It therefore proves a route is DECLARED, not that it renders. If that
// stops being enough, import appRoutes here instead and walk the tree.
const ROUTES_FILE = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "routes.tsx",
);

describe("desktop workspace routes", () => {
  it("declares a route for every workspace nav page", () => {
    const source = readFileSync(ROUTES_FILE, "utf8");
    const missing = (Object.keys(WORKSPACE_PAGES) as WorkspacePageKey[])
      .map((key) => WORKSPACE_PAGES[key].segment)
      .filter((segment) => !source.includes(`path: "${segment}"`));

    expect(
      missing,
      `these sidebar destinations have no desktop route, so their tabs render the route error page: ${missing.join(", ")}`,
    ).toEqual([]);
  });
});
