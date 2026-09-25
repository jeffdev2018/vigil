// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach } from "vitest";
import { IconOptionCard, IconOtherOptionCard } from "./icon-option-card";

afterEach(cleanup);

describe("IconOptionCard", () => {
  it("renders as a real button when unselected", () => {
    const { container } = render(
      <IconOptionCard icon={<span />} label="Engineering" selected={false} onSelect={vi.fn()} />,
    );
    expect(container.querySelector("button")).not.toBeNull();
  });
});

describe("IconOtherOptionCard", () => {
  // Audit finding (P3): when selected, this rendered a native <input> INSIDE
  // a native <button> — interactive-in-interactive is invalid HTML, and
  // every click into the field bubbled to the button's own onClick.
  it("does not nest the free-text input inside a native button once selected", () => {
    const { container } = render(
      <IconOtherOptionCard
        icon={<span />}
        label="Other"
        selected
        onSelect={vi.fn()}
        otherValue=""
        onOtherChange={vi.fn()}
        onConfirm={vi.fn()}
        placeholder="Describe your role"
      />,
    );

    expect(container.querySelector("button input")).toBeNull();
    // Role/checked state still carried on the root element so screen
    // readers see the same radio/checkbox semantics as the unselected chip.
    expect(screen.getByRole("radio", { checked: true })).toBeInTheDocument();
    expect(screen.getByPlaceholderText("Describe your role")).toBeInTheDocument();
  });

  it("stays a real button before selection", () => {
    const { container } = render(
      <IconOtherOptionCard
        icon={<span />}
        label="Other"
        selected={false}
        onSelect={vi.fn()}
        otherValue=""
        onOtherChange={vi.fn()}
        onConfirm={vi.fn()}
        placeholder="Describe your role"
      />,
    );
    expect(container.querySelector("button")).not.toBeNull();
  });
});
