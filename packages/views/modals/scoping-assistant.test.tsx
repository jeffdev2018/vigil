// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";

// Pure helpers and schema fallbacks: packages/core/issues/scoping.test.ts.

const state = vi.hoisted(() => ({
  mutate: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("sonner", () => ({ toast: { error: state.toastError, success: vi.fn() } }));
vi.mock("@multica/core/issues/scoping", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/scoping")>()),
  useProposeIssueScoping: () => ({ mutate: state.mutate, isPending: false }),
}));

import { ScopingAssistant } from "./scoping-assistant";

beforeEach(() => {
  state.mutate.mockReset();
  state.toastError.mockReset();
});

function open() {
  const onDraft = vi.fn();
  const onCriteriaChange = vi.fn();
  renderWithI18n(<ScopingAssistant projectId="p1" onDraft={onDraft} onCriteriaChange={onCriteriaChange} />);
  fireEvent.click(screen.getByTestId("scoping-toggle"));
  const box = screen.getByLabelText("Describe the issue in a few sentences…");
  fireEvent.change(box, { target: { value: "  Export issues as CSV  " } });
  return { onDraft, onCriteriaChange, box };
}

describe("ScopingAssistant", () => {
  it("drafts title, description with probable files, and editable criteria", () => {
    const { onDraft, onCriteriaChange } = open();
    fireEvent.click(screen.getByRole("button", { name: "Draft" }));
    expect(state.mutate).toHaveBeenCalledWith({ raw_text: "Export issues as CSV", project_id: "p1" }, expect.anything());
    act(() =>
      state.mutate.mock.calls[0]![1].onSuccess({
        title: "Add CSV export",
        description: "Body",
        acceptance_criteria: ["Button on the list", "Archived excluded"],
        probable_files: [{ path: "a.go", reason: "list endpoint" }],
      }),
    );
    expect(onDraft).toHaveBeenCalledWith({ title: "Add CSV export", description: "Body\n\n## Probable files (indicative)\n\n- `a.go` — list endpoint" });
    expect(onCriteriaChange).toHaveBeenLastCalledWith(["Button on the list", "Archived excluded"]);
    fireEvent.change(screen.getByLabelText("Acceptance criteria (one per line)"), { target: { value: "Only one" } });
    expect(onCriteriaChange).toHaveBeenLastCalledWith(["Only one"]);
  });

  it("keeps the text and reports the failure without touching the form", () => {
    const { onDraft, box } = open();
    fireEvent.click(screen.getByRole("button", { name: "Draft" }));
    act(() => state.mutate.mock.calls[0]![1].onError(new Error("the scoping assistant needs the server LLM to be configured")));
    expect(state.toastError).toHaveBeenCalledWith("the scoping assistant needs the server LLM to be configured");
    expect(onDraft).not.toHaveBeenCalled();
    expect((box as HTMLTextAreaElement).value).toBe("  Export issues as CSV  ");
  });
});
