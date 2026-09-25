import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { StatusBadge, type StatusBadgeConfig } from "./status-badge";

const CONFIG: StatusBadgeConfig = {
  completed: { tone: "success", label: "Completed" },
  running: { tone: "warning", label: "Running" },
  failed: { tone: "destructive", label: "Failed" },
  archived: { tone: "muted", label: "Archived" },
};

describe("StatusBadge", () => {
  it("renders the success tone and label for a known status", () => {
    render(<StatusBadge status="completed" config={CONFIG} />);
    const badge = screen.getByText("Completed");
    expect(badge).toHaveClass("text-success");
  });

  it("renders the warning tone and label for a known status", () => {
    render(<StatusBadge status="running" config={CONFIG} />);
    const badge = screen.getByText("Running");
    expect(badge).toHaveClass("text-warning");
  });

  it("renders the destructive variant for a destructive-tone status", () => {
    render(<StatusBadge status="failed" config={CONFIG} />);
    const badge = screen.getByText("Failed");
    expect(badge).toHaveClass("text-destructive");
    expect(badge.className).toContain("bg-destructive");
  });

  it("renders the muted tone for a muted-tone status", () => {
    render(<StatusBadge status="archived" config={CONFIG} />);
    expect(screen.getByText("Archived")).toHaveClass("text-muted-foreground");
  });

  it("falls back to a humanized label and muted tone for an unknown status", () => {
    render(<StatusBadge status="context_overflow" config={CONFIG} />);
    const badge = screen.getByText("Context overflow");
    expect(badge).toHaveClass("text-muted-foreground");
    expect(badge).toHaveAttribute("data-status", "context_overflow");
  });
});
