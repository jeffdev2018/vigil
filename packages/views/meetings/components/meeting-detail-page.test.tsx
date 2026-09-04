// @vitest-environment jsdom
import { forwardRef, useRef, useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Meeting } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { MeetingDetailPage } from "./meeting-detail-page";

// The recorder itself (MediaRecorder & friends) has no jsdom equivalent and is
// not exercised here — see meetings-page.test.tsx for the same note.

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    meetings: () => "/acme/meetings",
    meetingDetail: (id: string) => `/acme/meetings/${id}`,
    issueDetail: (id: string) => `/acme/issue/${id}`,
    triage: () => "/acme/triage",
  }),
}));

// Tiptap in jsdom buys this suite nothing; the title editor is a plain input.
vi.mock("../../editor", () => ({
  TitleEditor: forwardRef(function MockTitleEditor(
    { defaultValue, placeholder, onBlur }: any,
    _ref: any,
  ) {
    const valueRef = useRef(defaultValue ?? "");
    const [value, setValue] = useState(defaultValue ?? "");
    return (
      <input
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          valueRef.current = e.target.value;
          setValue(e.target.value);
        }}
        onBlur={() => onBlur?.(valueRef.current)}
        data-testid="title-editor"
      />
    );
  }),
}));

const data = vi.hoisted(() => ({
  meeting: null as Meeting | null,
  rename: vi.fn(async (_v: { meetingId: string; title: string }) => undefined),
  remove: vi.fn(async (_id: string) => undefined),
  finish: vi.fn(async (_id: string) => undefined),
  resummarize: vi.fn(async (_id: string) => undefined),
}));

vi.mock("@multica/core/meetings/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/meetings/queries")>()),
  meetingDetailOptions: () => ({
    queryKey: ["meetings", "ws-1", "detail", "meet-1"],
    queryFn: async () => data.meeting,
  }),
}));

vi.mock("@multica/core/meetings/mutations", () => ({
  useRenameMeeting: () => ({ mutateAsync: data.rename, isPending: false }),
  useDeleteMeeting: () => ({ mutateAsync: data.remove, isPending: false }),
  useFinishMeeting: () => ({ mutateAsync: data.finish, isPending: false }),
  useResummarizeMeeting: () => ({ mutateAsync: data.resummarize, isPending: false }),
}));

vi.mock("@multica/core/meetings/store", () => {
  const store = { meetingId: null };
  const useMeetingRecorderStore = <T,>(selector: (s: unknown) => T): T => selector(store);
  useMeetingRecorderStore.getState = () => store;
  return { useMeetingRecorderStore };
});

vi.mock("@multica/core/auth", () => {
  const store = { user: { id: "user-1" } };
  const useAuthStore = <T,>(selector: (s: unknown) => T): T => selector(store);
  useAuthStore.getState = () => store;
  return { useAuthStore };
});

function meeting(overrides: Partial<Meeting> = {}): Meeting {
  return {
    id: "meet-1",
    title: "Weekly sync",
    app_name: "Zoom",
    status: "done",
    transcript: "",
    summary_markdown: "- shipped",
    segment_count: 4,
    created_by: "user-1",
    started_at: "2026-01-01T09:00:00Z",
    ended_at: "2026-01-01T09:30:00Z",
    actions: [],
    action_count: 0,
    summary_unavailable: false,
    can_manage: true,
    ...overrides,
  };
}

function renderPage(): NavigationAdapter {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/meetings/meet-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    openInNewTab: vi.fn(),
  };
  renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={adapter}>
        <MeetingDetailPage meetingId="meet-1" />
      </NavigationProvider>
    </QueryClientProvider>,
  );
  return adapter;
}

describe("MeetingDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.meeting = meeting();
  });

  it("renames on blur, trimming and ignoring a no-op edit", async () => {
    renderPage();
    const input = (await screen.findByTestId("title-editor")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "  Sprint review  " } });
    fireEvent.blur(input);
    expect(data.rename).toHaveBeenCalledWith({ meetingId: "meet-1", title: "Sprint review" });

    // Blurring on the title the server already holds is not an edit.
    data.rename.mockClear();
    fireEvent.change(input, { target: { value: "Weekly sync" } });
    fireEvent.blur(input);
    expect(data.rename).not.toHaveBeenCalled();

    // An empty title is refused by the server; never send it.
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.blur(input);
    expect(data.rename).not.toHaveBeenCalled();
  });

  it("a viewer who cannot manage the meeting gets a read-only title and no delete", async () => {
    data.meeting = meeting({ can_manage: false });
    renderPage();
    expect(await screen.findByRole("heading", { name: "Weekly sync" })).toBeTruthy();
    expect(screen.queryByTestId("title-editor")).toBeNull();
    expect(screen.queryByRole("button", { name: /^delete$/i })).toBeNull();
  });

  it("offers Regenerate summary on a finished meeting and calls the server", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: /regenerate summary/i }));
    expect(data.resummarize).toHaveBeenCalledWith("meet-1");
  });

  it("hides Regenerate summary while the meeting is still recording", async () => {
    data.meeting = meeting({ status: "recording" });
    renderPage();
    expect(await screen.findByTestId("title-editor")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /regenerate summary/i })).toBeNull();
  });

  it("hides Regenerate summary from a viewer who cannot manage the meeting", async () => {
    data.meeting = meeting({ can_manage: false });
    renderPage();
    expect(await screen.findByRole("heading", { name: "Weekly sync" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /regenerate summary/i })).toBeNull();
  });

  it("a live summarize shows the spinner and no way to regenerate", async () => {
    data.meeting = meeting({
      status: "summarizing",
      summary_markdown: "",
      ended_at: new Date().toISOString(),
    });
    renderPage();
    expect(await screen.findByText(/writing the summary/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /regenerate summary/i })).toBeNull();
  });

  it("a summarize old enough to be dead says so and offers to regenerate", async () => {
    data.meeting = meeting({
      status: "summarizing",
      summary_markdown: "",
      ended_at: new Date(Date.now() - 10 * 60_000).toISOString(),
    });
    renderPage();
    expect(await screen.findByText(/taking longer than expected/i)).toBeTruthy();
    expect(screen.queryByText(/writing the summary/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /regenerate summary/i }));
    expect(data.resummarize).toHaveBeenCalledWith("meet-1");
  });

  it("a recording left open offers Finish meeting to anyone who may manage it", async () => {
    data.meeting = meeting({ status: "recording", created_by: "someone-else", can_manage: true });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: /finish meeting/i }));
    expect(data.finish).toHaveBeenCalledWith("meet-1");
  });

  it("a plain member sees the recording notice without a way to finish it", async () => {
    data.meeting = meeting({ status: "recording", created_by: "someone-else", can_manage: false });
    renderPage();
    expect(await screen.findByText(/recorded from another device or tab/i)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /finish meeting/i })).toBeNull();
  });

  // The toast the recorder shows on finish branches on this same flag; the
  // recorder hook itself is not DOM-testable (MediaRecorder), and the server
  // side of the flag is pinned by TestMeetingGetReportsSummaryUnavailable.
  it("distinguishes a summary that was never written from one not yet written", async () => {
    data.meeting = meeting({ summary_markdown: "", summary_unavailable: true });
    renderPage();
    expect(await screen.findByText(/no language model was available/i)).toBeTruthy();
    cleanup();

    data.meeting = meeting({ summary_markdown: "", summary_unavailable: false });
    renderPage();
    expect(await screen.findByText(/no summary yet/i)).toBeTruthy();
  });

  it("deleting confirms, awaits the server, then returns to the list", async () => {
    const adapter = renderPage();
    fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
    expect(data.remove).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getAllByRole("button", { name: /^delete$/i }).find((b) => b !== null)!,
    );
    await vi.waitFor(() => expect(data.remove).toHaveBeenCalledWith("meet-1"));
    await vi.waitFor(() => expect(adapter.push).toHaveBeenCalledWith("/acme/meetings"));
  });
});
