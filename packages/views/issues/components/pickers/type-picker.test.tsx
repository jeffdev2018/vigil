// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { buildIssueTypeCatalog } from "@multica/core/issue-types";
import type { IssueTypeEntry } from "@multica/core/types";
import en from "../../../locales/en/issues.json";
import { renderWithI18n } from "../../../test/i18n";
import { TypePicker } from "./type-picker";

// The catalogue is server state; this suite is about what the picker PAINTS
// with it, so entries are fed in directly. The resolution matrix behind
// `labelOf`/`colorOf` lives in packages/core/issue-types/queries.test.ts.
let catalogEntries: IssueTypeEntry[] | undefined;

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));
vi.mock("@multica/core/issue-types/hooks", () => ({
  useIssueTypes: () => buildIssueTypeCatalog(catalogEntries),
}));

function entry(overrides: Partial<IssueTypeEntry>): IssueTypeEntry {
  return {
    id: overrides.key ?? "id",
    workspace_id: "workspace-1",
    key: "bug",
    name: "Bug",
    description: "",
    color: "#ef4444",
    icon: "bug",
    is_system: true,
    position: 0,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

afterEach(() => {
  cleanup();
  catalogEntries = undefined;
});

describe("TypePicker", () => {
  it("renders the type's name on the trigger", () => {
    catalogEntries = [entry({}), entry({ id: "story", key: "story", name: "Story" })];
    renderWithI18n(<TypePicker issueType="story" onUpdate={vi.fn()} />);
    expect(screen.getByText("Story")).toBeTruthy();
  });

  // Acceptance 3: untyped is a real state, not a loading one, and it says so.
  it("says No type on an untyped issue", () => {
    catalogEntries = [entry({})];
    renderWithI18n(<TypePicker issueType={null} onUpdate={vi.fn()} />);
    expect(screen.getAllByText(en.issue_types.none).length).toBeGreaterThan(0);
  });

  // Acceptance 14: an issue can carry a type this client has not resolved —
  // one created moments ago in another session. The badge falls back to the
  // raw key rather than rendering blank.
  it("falls back to the raw key for a type the catalogue does not know", () => {
    catalogEntries = [entry({})];
    renderWithI18n(<TypePicker issueType="invented_elsewhere" onUpdate={vi.fn()} />);
    expect(screen.getByText("invented_elsewhere")).toBeTruthy();
  });

  it("offers every active type plus an explicit clear row, and writes the key", () => {
    catalogEntries = [entry({}), entry({ id: "story", key: "story", name: "Story" })];
    const onUpdate = vi.fn();
    renderWithI18n(<TypePicker issueType={null} onUpdate={onUpdate} open onOpenChange={vi.fn()} />);

    fireEvent.click(screen.getByText("Story"));
    expect(onUpdate).toHaveBeenCalledWith({ issue_type: "story" });
  });

  it("clears the type through the No type row", () => {
    catalogEntries = [entry({})];
    const onUpdate = vi.fn();
    renderWithI18n(<TypePicker issueType="bug" onUpdate={onUpdate} open onOpenChange={vi.fn()} />);

    // Two nodes carry the label when the list is open (trigger + row); the row
    // is the one inside the listbox.
    const rows = screen.getAllByText(en.issue_types.none);
    fireEvent.click(rows[rows.length - 1]!);
    expect(onUpdate).toHaveBeenCalledWith({ issue_type: null });
  });

  // Archiving retires a type from FUTURE assignment and leaves the issues
  // already on it alone, so the label still renders while the row is gone.
  it("hides an archived type from the list but still labels an issue on it", () => {
    catalogEntries = [
      entry({}),
      entry({ id: "old", key: "old", name: "Retired", archived_at: "2026-01-01T00:00:00Z" }),
    ];
    renderWithI18n(<TypePicker issueType="old" onUpdate={vi.fn()} open onOpenChange={vi.fn()} />);

    // Once on the trigger — and NOT a second time as an offerable row.
    expect(screen.getAllByText("Retired")).toHaveLength(1);
    expect(screen.getByText("Bug")).toBeTruthy();
  });
});
