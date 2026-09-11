// Canonical coverage for the `closeLabel` prop on DialogContent/SheetContent
// (packages/ui/components/ui/dialog.tsx, sheet.tsx). `packages/ui` has no
// i18n of its own, so the translated label must be threaded in by the
// caller — this locks the default and the override in place.
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Sheet, SheetContent, SheetTitle } from "@multica/ui/components/ui/sheet";

afterEach(cleanup);

describe("DialogContent closeLabel", () => {
  it("defaults the close button's accessible name to 'Close'", () => {
    render(
      <Dialog open modal={false}>
        <DialogContent>
          <DialogTitle>Example dialog</DialogTitle>
        </DialogContent>
      </Dialog>,
    );

    expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument();
  });

  it("uses the caller-provided translated label", () => {
    render(
      <Dialog open modal={false}>
        <DialogContent closeLabel="Fermer">
          <DialogTitle>Example dialog</DialogTitle>
        </DialogContent>
      </Dialog>,
    );

    expect(screen.getByRole("button", { name: "Fermer" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Close" })).not.toBeInTheDocument();
  });
});

describe("SheetContent closeLabel", () => {
  it("defaults the close button's accessible name to 'Close'", () => {
    render(
      <Sheet open modal={false}>
        <SheetContent>
          <SheetTitle>Example sheet</SheetTitle>
        </SheetContent>
      </Sheet>,
    );

    expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument();
  });

  it("uses the caller-provided translated label", () => {
    render(
      <Sheet open modal={false}>
        <SheetContent closeLabel="Fermer">
          <SheetTitle>Example sheet</SheetTitle>
        </SheetContent>
      </Sheet>,
    );

    expect(screen.getByRole("button", { name: "Fermer" })).toBeInTheDocument();
  });
});
