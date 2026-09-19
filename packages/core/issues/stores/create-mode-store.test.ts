import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  openCreateIssueWithPreference,
  useCreateModeStore,
} from "./create-mode-store";
import { useModalStore } from "../../modals";

describe("openCreateIssueWithPreference", () => {
  const initialMode = useCreateModeStore.getState().lastMode;

  beforeEach(() => {
    useModalStore.getState().close();
  });

  afterEach(() => {
    useCreateModeStore.getState().setLastMode(initialMode);
    useModalStore.getState().close();
  });

  // Audit UX (sept. 2026): agent mode starts a real, billed run on submit, so a
  // user who never picked it must land on the manual form.
  it("defaults to manual so a generic entry point never starts an agent run", () => {
    expect(useCreateModeStore.getInitialState().lastMode).toBe("manual");
  });

  it("opens quick-create-issue when last mode is agent", () => {
    useCreateModeStore.getState().setLastMode("agent");
    openCreateIssueWithPreference();
    expect(useModalStore.getState().modal).toBe("quick-create-issue");
    expect(useModalStore.getState().data).toBeNull();
  });

  it("opens create-issue when last mode is manual", () => {
    useCreateModeStore.getState().setLastMode("manual");
    openCreateIssueWithPreference();
    expect(useModalStore.getState().modal).toBe("create-issue");
  });

  it("forwards seed data to whichever modal is opened", () => {
    useCreateModeStore.getState().setLastMode("manual");
    openCreateIssueWithPreference({ project_id: "p1" });
    expect(useModalStore.getState().modal).toBe("create-issue");
    expect(useModalStore.getState().data).toEqual({ project_id: "p1" });

    useCreateModeStore.getState().setLastMode("agent");
    openCreateIssueWithPreference({ project_id: "p2" });
    expect(useModalStore.getState().modal).toBe("quick-create-issue");
    expect(useModalStore.getState().data).toEqual({ project_id: "p2" });
  });
});
