// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ promote: vi.fn(), pending: false, workspace: { id: "ws-1", name: "Acme", slug: "acme" } as { id: string; name: string; slug: string } | null }));

vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => state.workspace }));
vi.mock("@multica/core/eval", () => ({
  usePromoteIssueToEvalCase: () => ({ mutate: state.promote, isPending: state.pending }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...rest }: { href: string; children: ReactNode }) => (
    <a href={href} {...rest}>{children}</a>
  ),
}));

import { EvalPromoteSection } from "./eval-promote-section";

beforeEach(() => {
  state.promote.mockReset();
  state.pending = false;
  state.workspace = { id: "ws-1", name: "Acme", slug: "acme" };
});

describe("EvalPromoteSection", () => {
  it("promotes the issue and then links to the Eval Lab tab", () => {
    state.promote.mockImplementation((_input: undefined, opts: { onSuccess: () => void }) => opts.onSuccess());
    renderWithI18n(<EvalPromoteSection issueId="i1" />);

    fireEvent.click(screen.getByRole("button", { name: "Promote to eval case" }));
    expect(screen.getByText("Added to Eval Lab")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Open Eval Lab" }).getAttribute("href")).toBe(
      "/acme/settings?tab=eval-lab",
    );
    // The button is gone: promoting twice would only 409.
    expect(screen.queryByRole("button", { name: "Promote to eval case" })).toBeNull();
  });

  it("shows the server's 409 reason verbatim instead of a generic failure", () => {
    state.promote.mockImplementation((_input: undefined, opts: { onError: (e: Error) => void }) =>
      opts.onError(Object.assign(new Error("issue has unproved acceptance criteria"), { status: 409 })),
    );
    renderWithI18n(<EvalPromoteSection issueId="i1" />);

    fireEvent.click(screen.getByRole("button", { name: "Promote to eval case" }));
    expect(screen.getByRole("alert").textContent).toBe("issue has unproved acceptance criteria");
    expect(screen.getByRole("button", { name: "Promote to eval case" })).toBeTruthy();
  });

  it("falls back to a translated message when the error carries none", () => {
    state.promote.mockImplementation((_input: undefined, opts: { onError: (e: Error) => void }) =>
      opts.onError(new Error("")),
    );
    renderWithI18n(<EvalPromoteSection issueId="i1" />);

    fireEvent.click(screen.getByRole("button", { name: "Promote to eval case" }));
    expect(screen.getByRole("alert").textContent).toBe("Could not promote this issue");
  });

  it("still confirms success without a workspace slug to link to", () => {
    state.workspace = null;
    state.promote.mockImplementation((_input: undefined, opts: { onSuccess: () => void }) => opts.onSuccess());
    renderWithI18n(<EvalPromoteSection issueId="i1" />);

    fireEvent.click(screen.getByRole("button", { name: "Promote to eval case" }));
    expect(screen.getByText("Added to Eval Lab")).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
  });
});
