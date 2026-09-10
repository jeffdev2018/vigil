// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import type { BrainCapture, BrainCapturesResponse } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { CaptureInbox } from "./capture-inbox";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspaceSlug: () => "acme",
}));

const capture = (over: Partial<BrainCapture> = {}): BrainCapture => ({
  id: "cap-1",
  workspace_id: "ws-1",
  kind: "text",
  content: "pgbouncer listens on 6432",
  url: "",
  title_hint: "",
  attachment: null,
  origin: "web",
  status: "raw",
  transcription_status: "none",
  suggestion: null,
  note_id: null,
  created_by_type: "member",
  created_by_id: "user-1",
  source_task_id: null,
  organized_by: null,
  organized_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  ...over,
});

const data = vi.hoisted(() => ({
  response: { captures: [], raw_count: 0 } as BrainCapturesResponse,
  lastStatus: "" as string,
}));

vi.mock("@multica/core/brain/queries", () => ({
  brainCapturesOptions: (_wsId: string, status: string) => {
    data.lastStatus = status;
    return {
      queryKey: ["brain", "ws-1", "captures", "list", status],
      queryFn: async () => data.response,
    };
  },
  noteSearchOptions: () => ({
    queryKey: ["brain", "ws-1", "search", ""],
    enabled: false,
    queryFn: async () => ({ notes: [], vector: false }),
  }),
}));

const mutations = vi.hoisted(() => ({
  captureText: vi.fn(),
  captureUpload: vi.fn(),
  suggest: vi.fn(),
  organize: vi.fn(),
  reopen: vi.fn(),
  remove: vi.fn(),
}));

vi.mock("@multica/core/brain/mutations", () => ({
  useCaptureText: () => ({ mutateAsync: mutations.captureText, isPending: false }),
  useCaptureUpload: () => ({
    mutateAsync: mutations.captureUpload,
    isPending: false,
    error: null,
  }),
  useSuggestCapture: () => ({ mutateAsync: mutations.suggest, isPending: false }),
  useOrganizeCapture: () => ({ mutateAsync: mutations.organize, isPending: false }),
  useReopenCapture: () => ({ mutateAsync: mutations.reopen, isPending: false }),
  useDeleteCapture: () => ({ mutateAsync: mutations.remove, isPending: false }),
}));

function renderInbox() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <QueryClientProvider client={client}>
        <CaptureInbox wsId="ws-1" />
      </QueryClientProvider>
    </NavigationProvider>,
  );
}

/** A File of a chosen size without allocating it byte by byte. */
function fileOfSize(name: string, bytes: number, type = "application/pdf"): File {
  const file = new File(["x"], name, { type });
  Object.defineProperty(file, "size", { value: bytes });
  return file;
}

describe("CaptureInbox", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mutations.captureText.mockResolvedValue(capture({ id: "cap-new" }));
    mutations.captureUpload.mockResolvedValue(capture({ id: "cap-file", kind: "file" }));
    mutations.organize.mockResolvedValue({ capture: capture({ status: "organized" }), note: null });
    mutations.reopen.mockResolvedValue(capture({ status: "raw" }));
    mutations.remove.mockResolvedValue(undefined);
    mutations.suggest.mockResolvedValue(capture());
    data.response = { captures: [capture()], raw_count: 1 };
  });

  it("captures typed text on Enter, and keeps Shift+Enter for a newline", async () => {
    renderInbox();
    const box = await screen.findByLabelText("Capture text");
    fireEvent.change(box, { target: { value: "pgbouncer listens on 6432" } });
    fireEvent.keyDown(box, { key: "Enter", shiftKey: true });
    expect(mutations.captureText).not.toHaveBeenCalled();
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() =>
      expect(mutations.captureText).toHaveBeenCalledWith({
        content: "pgbouncer listens on 6432",
        kind: "text",
      }),
    );
  });

  it("captures a pasted URL as a link, with no Enter", async () => {
    renderInbox();
    const box = await screen.findByLabelText("Capture text");
    fireEvent.paste(box, {
      clipboardData: { getData: () => "https://example.test/post" },
    });
    await waitFor(() =>
      expect(mutations.captureText).toHaveBeenCalledWith({
        url: "https://example.test/post",
        kind: undefined,
      }),
    );
  });

  it("captures as a todo when the toggle is on", async () => {
    renderInbox();
    fireEvent.click(await screen.findByRole("button", { name: "Todo" }));
    fireEvent.change(screen.getByLabelText("Capture text"), {
      target: { value: "call the registrar" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Capture" }));
    await waitFor(() =>
      expect(mutations.captureText).toHaveBeenCalledWith({
        content: "call the registrar",
        kind: "todo",
      }),
    );
  });

  it("uploads a picked file", async () => {
    renderInbox();
    const input = (await screen.findByTestId("capture-file-input")) as HTMLInputElement;
    const file = fileOfSize("runbook.pdf", 1024);
    fireEvent.change(input, { target: { files: [file] } });
    await waitFor(() =>
      expect(mutations.captureUpload).toHaveBeenCalledWith({
        file,
        title_hint: undefined,
      }),
    );
  });

  it("refuses a file over 8 MB before the request, and points at the CLI", async () => {
    renderInbox();
    const input = (await screen.findByTestId("capture-file-input")) as HTMLInputElement;
    fireEvent.change(input, {
      target: { files: [fileOfSize("dump.zip", 9 * 1024 * 1024)] },
    });
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("cannot capture a file over 8 MB");
    expect(alert.textContent).toContain("CLI");
    expect(mutations.captureUpload).not.toHaveBeenCalled();
  });

  it("uploads a file dropped on the inbox", async () => {
    renderInbox();
    await screen.findByLabelText("Capture text");
    const file = fileOfSize("screenshot.png", 2048, "image/png");
    const dropTarget = screen.getByLabelText("Capture status").parentElement as HTMLElement;
    fireEvent.drop(dropTarget, {
      dataTransfer: { types: ["Files"], files: [file] },
    });
    await waitFor(() =>
      expect(mutations.captureUpload).toHaveBeenCalledWith({
        file,
        title_hint: undefined,
      }),
    );
  });

  it("lets a capture without suggestion or title hint be saved: the server derives the title", async () => {
    data.response = { captures: [capture({ content: "pgbouncer listens on 6432\nsecond line" })], raw_count: 1 };
    renderInbox();
    fireEvent.click(await screen.findByRole("button", { name: "Save as note" }));
    const dialog = await screen.findByRole("dialog");
    const title = within(dialog).getByLabelText("Title") as HTMLInputElement;
    expect(title.value).toBe("");
    // The default the server will use is shown where the title would go.
    expect(title.placeholder).toBe("pgbouncer listens on 6432");
    const submit = within(dialog).getByRole("button", { name: "Save as note" });
    expect(submit).not.toBeDisabled();
    fireEvent.click(submit);
    await waitFor(() =>
      expect(mutations.organize).toHaveBeenCalledWith({
        id: "cap-1",
        input: { action: "note", title: "", tags: [], content: undefined, pinned: false },
      }),
    );
  });

  it("organizes a capture into a note through the dialog", async () => {
    data.response = {
      captures: [
        capture({
          suggestion: {
            title: "pgbouncer listens on 6432",
            tags: ["db", "infra"],
            summary: "Connection pooling port.",
            action: "note",
            merge_note: null,
            candidates: [],
            reason: "New fact.",
            model: "test",
          },
        }),
      ],
      raw_count: 1,
    };
    renderInbox();
    fireEvent.click(await screen.findByRole("button", { name: "Save as note" }));
    const dialog = await screen.findByRole("dialog");
    // Prefilled from the suggestion: the user confirms rather than retypes.
    expect((within(dialog).getByLabelText("Title") as HTMLInputElement).value).toBe(
      "pgbouncer listens on 6432",
    );
    expect((within(dialog).getByLabelText("Tags") as HTMLInputElement).value).toBe(
      "db, infra",
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Save as note" }));
    await waitFor(() =>
      expect(mutations.organize).toHaveBeenCalledWith({
        id: "cap-1",
        input: {
          action: "note",
          title: "pgbouncer listens on 6432",
          tags: ["db", "infra"],
          content: undefined,
          pinned: false,
        },
      }),
    );
    expect(toast.success).toHaveBeenCalled();
  });

  it("merges into the note the model proposed", async () => {
    data.response = {
      captures: [
        capture({
          suggestion: {
            title: "pgbouncer",
            tags: ["db"],
            summary: "",
            action: "merge",
            merge_note: { id: "note-7", title: "Postgres runbook" },
            candidates: [],
            reason: "Same topic.",
          },
        }),
      ],
      raw_count: 1,
    };
    renderInbox();
    fireEvent.click(await screen.findByRole("button", { name: "Merge into…" }));
    const dialog = await screen.findByRole("dialog");
    // Preselected from the suggestion, and the append preview names it.
    expect(
      within(dialog).getByRole("button", { name: "Postgres runbook" }).getAttribute("aria-pressed"),
    ).toBe("true");
    expect(within(dialog).getByText(/Appended to the end of/)).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Merge" }));
    await waitFor(() =>
      expect(mutations.organize).toHaveBeenCalledWith({
        id: "cap-1",
        input: { action: "merge", note_id: "note-7", tags: ["db"], content: undefined },
      }),
    );
  });

  it("discards a capture", async () => {
    renderInbox();
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));
    await waitFor(() =>
      expect(mutations.organize).toHaveBeenCalledWith({
        id: "cap-1",
        input: { action: "discard" },
      }),
    );
  });

  it("reopens a discarded capture", async () => {
    data.response = { captures: [capture({ status: "discarded" })], raw_count: 0 };
    renderInbox();
    fireEvent.click(await screen.findByRole("button", { name: "Discarded" }));
    await waitFor(() => expect(data.lastStatus).toBe("discarded"));
    fireEvent.click(await screen.findByRole("button", { name: "Reopen" }));
    await waitFor(() => expect(mutations.reopen).toHaveBeenCalledWith("cap-1"));
  });

  it("deletes a capture only after the confirm", async () => {
    renderInbox();
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    expect(mutations.remove).not.toHaveBeenCalled();
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(mutations.remove).toHaveBeenCalledWith("cap-1"));
  });

  it("reads a 503 from suggest as 'no model configured', not as a failure", async () => {
    const { ApiError } = await import("@multica/core/api");
    mutations.suggest.mockRejectedValue(new ApiError("no model", 503, "Service Unavailable"));
    renderInbox();
    fireEvent.click(await screen.findByRole("button", { name: "Suggest" }));
    // A notice, page-wide and sticky; and the button stops offering the call.
    const notice = await screen.findByRole("status");
    expect(notice.textContent).toContain("No model is configured");
    expect(screen.getByRole("button", { name: "Suggest" })).toHaveProperty("disabled", true);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows a link capture's URL and an image capture's thumbnail", async () => {
    data.response = {
      captures: [
        capture({
          id: "cap-link",
          kind: "link",
          content: "",
          url: "https://example.test/post",
        }),
        capture({
          id: "cap-img",
          kind: "image",
          content: "",
          attachment: {
            id: "att-1",
            url: "https://cdn.test/shot.png",
            download_url: "https://cdn.test/shot.png?dl",
            filename: "shot.png",
          },
        }),
      ],
      raw_count: 2,
    };
    renderInbox();
    const link = await screen.findByRole("link", { name: "https://example.test/post" });
    expect(link.getAttribute("href")).toBe("https://example.test/post");
    expect(screen.getByRole("img", { name: "shot.png" }).getAttribute("src")).toBe(
      "https://cdn.test/shot.png",
    );
  });

  it("says a voice memo is still being transcribed", async () => {
    data.response = {
      captures: [capture({ kind: "audio", transcription_status: "pending", content: "" })],
      raw_count: 1,
    };
    renderInbox();
    expect(await screen.findByText("Transcribing…")).toBeTruthy();
  });
});
