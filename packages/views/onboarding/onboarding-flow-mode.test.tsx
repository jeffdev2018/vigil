import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enOnboarding from "../locales/en/onboarding.json";
import enWorkspace from "../locales/en/workspace.json";

const TEST_RESOURCES = {
  en: { common: enCommon, onboarding: enOnboarding, workspace: enWorkspace },
};

vi.mock("../auth", () => ({ useLogout: () => vi.fn() }));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (
    selector: (s: { workspaceCreationDisabled: boolean; daemonAppUrl: string }) => unknown,
  ) => selector({ workspaceCreationDisabled: false, daemonAppUrl: "" }),
}));

vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "https://multica.ai" },
}));

const created = vi.hoisted(() => ({ id: "ws-new", name: "Fresh", slug: "fresh" }));
vi.mock("@multica/core/workspace/mutations", () => ({
  // Answers like the server: the workspace exists, onSuccess advances the flow.
  useCreateWorkspace: () => ({
    mutate: (_input: unknown, opts: { onSuccess: (ws: unknown) => void }) => opts.onSuccess(created),
    isPending: false,
  }),
}));

// The runtime step has its own suites; here it only has to show it was reached.
vi.mock("./steps/step-runtime-connect", () => ({
  StepRuntimeConnect: ({ wsSlug }: { wsSlug: string }) => <p>runtime step for {wsSlug}</p>,
}));

vi.mock("@multica/core/workspace/transfer", () => ({
  workspaceTemplatesOptions: () => ({ queryKey: ["workspace-templates"], queryFn: async () => [] }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (s: { user: unknown }) => unknown) =>
      selector({ user: { id: "u-1", onboarding_questionnaire: {} } }),
    { getState: () => ({ user: { id: "u-1" } }) },
  ),
}));

// Returning one workspace proves new-workspace mode does not offer to
// continue with it.
vi.mock("@multica/core/workspace", () => {
  return {
    useWorkspaceList: () => ({
      workspaces: [
        { id: "ws-1", name: "Existing", slug: "existing" },
        { id: "ws-new", name: "Fresh", slug: "fresh" },
      ],
      ready: true,
    }),
  };
});

vi.mock("@multica/core/onboarding", async () => {
  const actual = await vi.importActual<Record<string, unknown>>(
    "@multica/core/onboarding",
  );
  return { ...actual, useBootstrapMika: () => ({ mutateAsync: vi.fn() }) };
});

import { OnboardingFlow } from "./onboarding-flow";

function renderFlow(props: Record<string, unknown>) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <OnboardingFlow onComplete={vi.fn()} {...props} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  window.sessionStorage.clear();
});

describe("OnboardingFlow — new-workspace mode", () => {
  it("starts at the workspace step instead of the product intro", () => {
    renderFlow({ mode: "new_workspace", onCancel: vi.fn() });

    // The welcome screen teaches what Multica is; someone creating a second
    // workspace already knows, so the flow opens on naming it.
    expect(
      screen.getByRole("heading", { name: /Name your workspace/i }),
    ).toBeInTheDocument();
  });

  it("always creates a workspace rather than offering the existing one", () => {
    renderFlow({ mode: "new_workspace", onCancel: vi.fn() });

    // First-run onboarding offers "Continue with {name}" when an abandoned
    // workspace exists. Doing that here would defeat the whole intent.
    expect(screen.queryByText(/Continue with/i)).not.toBeInTheDocument();
    expect(screen.getByLabelText("Workspace name")).toBeInTheDocument();
  });

  it("still opens on the product intro in first-run mode", () => {
    renderFlow({});

    expect(
      screen.queryByRole("heading", { name: /Name your workspace/i }),
    ).not.toBeInTheDocument();
  });

  // Regression (audit): reloading /workspaces/new right after creating the
  // workspace showed the empty naming form again, inviting a duplicate.
  it("resumes on the runtime step of the workspace it just created after a reload", async () => {
    const first = renderFlow({ mode: "new_workspace", onCancel: vi.fn() });
    fireEvent.change(screen.getByLabelText("Workspace name"), { target: { value: "Fresh" } });
    fireEvent.click(screen.getByRole("button", { name: /Create Fresh/i }));
    expect(await screen.findByText("runtime step for fresh")).toBeInTheDocument();
    first.unmount();

    renderFlow({ mode: "new_workspace", onCancel: vi.fn() });
    expect(await screen.findByText("runtime step for fresh")).toBeInTheDocument();
    expect(screen.queryByLabelText("Workspace name")).not.toBeInTheDocument();
  });
});
