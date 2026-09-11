import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import issues from "../../locales/en/issues.json";
import common from "../../locales/en/common.json";
import { IssueDeliverySection } from "./issue-delivery-section";

const api = vi.hoisted(() => ({ getIssueDelivery: vi.fn(), getIssueDeliveryHistory: vi.fn(), listTasksByIssue: vi.fn(), updateIssueDeliveryCriteria: vi.fn(), reviewIssueDelivery: vi.fn(), startIssueDeliveryCorrection: vi.fn() }));
vi.mock("@multica/core/api", async (original) => ({ ...await original<object>(), api }));
// Protocol and malformed-response cases live in core/api/issue-delivery.test.ts.
const initial = { title: "Inbox", description: null, criteria: ["Show a next step"], revision: 1,
  snapshotToken: "a".repeat(64), latestReview: null, reviewStale: false, pullRequests: [],
  run: { id: "run-1", status: "completed", result: { summary: "Added the next step" }, error: null, completedAt: "2026-09-05T00:00:00Z" } };
function mount(props: {
  boardStatusIsReview?: boolean;
  canProposeDone?: boolean;
  onMarkDone?: () => void;
} = {}) {
  return render(<I18nProvider locale="en" resources={{ en: { issues, common } }}>
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <IssueDeliverySection
        wsId="ws-1"
        issueId="issue-1"
        identifier="MUL-1"
        boardStatusIsReview={props.boardStatusIsReview}
        canProposeDone={props.canProposeDone}
        onMarkDone={props.onMarkDone}
      />
    </QueryClientProvider>
  </I18nProvider>);
}

describe("IssueDeliverySection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.getIssueDelivery.mockResolvedValue(initial);
    api.listTasksByIssue.mockResolvedValue([]);
    api.getIssueDeliveryHistory.mockResolvedValue({ reviews: [], nextBeforeId: null });
    api.reviewIssueDelivery.mockResolvedValue({ id: "saved-review" });
    api.updateIssueDeliveryCriteria.mockResolvedValue({ criteria: ["Updated"], revision: 2 });
  });
  it("requires a human assessment and evidence before accepting the displayed run", async () => {
    const user = userEvent.setup(); mount();
    expect(await screen.findByText("Added the next step")).toBeInTheDocument();
    expect(screen.getByText("Cost unavailable")).toBeInTheDocument();
    expect(screen.getByText(/Board status \(including In Review\)/)).toBeInTheDocument();
    expect(screen.getByText(/waiting for your accept or correction decision/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Review delivery" }));
    expect(screen.getByRole("dialog", { name: "Review delivery" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Accept delivery" })).toBeDisabled();
    await user.click(screen.getByRole("checkbox", { name: "Show a next step" }));
    await user.type(screen.getByRole("textbox", { name: "Evidence for criterion 1" }), "Checked on mobile");
    await user.click(screen.getByRole("button", { name: "Accept delivery" }));
    expect(api.reviewIssueDelivery).toHaveBeenCalledWith("issue-1", expect.objectContaining({
      snapshotToken: initial.snapshotToken, expectedReviewId: "", decision: "accepted",
      assessments: [{ passed: true, evidence: "Checked on mobile" }],
    }));
  });
  // Audit UX (sept. 2026): the reported result was dumped as raw JSON, with
  // the absolute work_dir of the agent's machine, the session id and the
  // judge's signature. Parsing matrix: core/issues/run-result.test.ts.
  it("renders the reported output and goal verdict for humans and folds the rest away", async () => {
    api.getIssueDelivery.mockResolvedValue({ ...initial, run: { ...initial.run, result: {
      output: "**Market scan** done", pr_url: "https://github.com/acme/app/pull/7",
      work_dir: "/Users/jeff/multica_workspaces/one/x", session_id: "sess-9", branch_name: "agent/one-96",
      goal_loop: { continuation: 1, signature: "deadbeef", no_progress: 0, outcome: "stopped:needs_user_input", blocker: "needs_user_input", reason: "Market name missing", next_step: "Name the market" },
    } } });
    const { container } = mount();
    expect(await screen.findByText("Market scan")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "https://github.com/acme/app/pull/7" })).toBeInTheDocument();
    expect(screen.getByText("Market name missing")).toBeInTheDocument();
    expect(screen.getByText("Name the market")).toBeInTheDocument();
    expect(screen.getByText("Technical details")).toBeInTheDocument();
    expect(container.textContent).not.toMatch(/Users\/jeff|sess-9|deadbeef|"output"/);
  });

  it("explains that board In Review is not acceptance or a merge lock", async () => {
    mount({ boardStatusIsReview: true });
    expect(await screen.findByRole("note")).toHaveTextContent(/workflow signal only/);
    expect(screen.getByRole("note")).toHaveTextContent(/does not block merge or deploy/);
  });
  it("offers an optional Done status after acceptance without applying it automatically", async () => {
    const onMarkDone = vi.fn();
    const user = userEvent.setup();
    const first = mount({ canProposeDone: true, onMarkDone });
    await user.click(await screen.findByRole("button", { name: "Review delivery" }));
    await user.click(screen.getByRole("checkbox", { name: "Show a next step" }));
    await user.type(screen.getByRole("textbox", { name: "Evidence for criterion 1" }), "Checked on mobile");
    await user.click(screen.getByRole("button", { name: "Accept delivery" }));
    expect(await screen.findByText("Mark the issue Done?")).toBeInTheDocument();
    expect(onMarkDone).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Keep current status" }));
    expect(screen.queryByText("Mark the issue Done?")).not.toBeInTheDocument();
    first.unmount();

    mount({ canProposeDone: true, onMarkDone });
    await user.click(await screen.findByRole("button", { name: "Review delivery" }));
    await user.click(screen.getByRole("checkbox", { name: "Show a next step" }));
    await user.type(screen.getByRole("textbox", { name: "Evidence for criterion 1" }), "Checked again");
    await user.click(screen.getByRole("button", { name: "Accept delivery" }));
    await user.click(await screen.findByRole("button", { name: "Mark as Done" }));
    expect(onMarkDone).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Mark the issue Done?")).not.toBeInTheDocument();
  });
  it("does not propose Done when the issue is already terminal", async () => {
    const user = userEvent.setup();
    mount({ canProposeDone: false, onMarkDone: vi.fn() });
    await user.click(await screen.findByRole("button", { name: "Review delivery" }));
    await user.click(screen.getByRole("checkbox", { name: "Show a next step" }));
    await user.type(screen.getByRole("textbox", { name: "Evidence for criterion 1" }), "Checked on mobile");
    await user.click(screen.getByRole("button", { name: "Accept delivery" }));
    expect(api.reviewIssueDelivery).toHaveBeenCalled();
    expect(screen.queryByText("Mark the issue Done?")).not.toBeInTheDocument();
  });
  it("preserves a conflicting review draft and blocks approval after a refetch changes the evidence", async () => {
    const user = userEvent.setup(); mount();
    await user.click(await screen.findByRole("button", { name: "Review delivery" }));
    const feedback = screen.getByRole("textbox", { name: "Reservations or correction instructions" });
    await user.type(feedback, "The next step still needs a link");
    api.getIssueDelivery.mockResolvedValue({ ...initial, snapshotToken: "b".repeat(64) });
    api.reviewIssueDelivery.mockRejectedValue(new ApiError("changed", 409, "Conflict"));
    await user.click(screen.getByRole("button", { name: "Request corrections" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("delivery or review changed");
    expect(feedback).toHaveValue("The next step still needs a link");
    expect(screen.getByRole("button", { name: "Request corrections" })).toBeDisabled();
  });
  it("edits criteria against their revision and refuses an unavailable delivery", async () => {
    const user = userEvent.setup(); const view = mount();
    await user.click(await screen.findByRole("button", { name: "Edit criteria" }));
    const input = screen.getByRole("textbox", { name: "Acceptance criteria" });
    await user.clear(input); await user.type(input, "Updated");
    await user.click(screen.getByRole("button", { name: "Save criteria" }));
    expect(api.updateIssueDeliveryCriteria).toHaveBeenCalledWith("issue-1", ["Updated"], 1);
    view.unmount();
    api.getIssueDelivery.mockResolvedValue(null); mount();
    expect(await screen.findByRole("alert")).toHaveTextContent("could not be loaded");
    expect(screen.queryByRole("button", { name: "Review delivery" })).not.toBeInTheDocument();
  });

  it("retries a saved correction review and replaces the launch button with its receipt", async () => {
    const saved = { id: "saved-review", decision: "changes_requested", feedback: "Add a working link",
      snapshot: initial, snapshotToken: initial.snapshotToken, reviewedBy: "member-1", createdAt: "2026-09-05T00:01:00Z",
      assessments: [{ passed: false, evidence: "The link is missing" }], correctionTaskId: null };
    api.getIssueDelivery.mockResolvedValue({ ...initial, latestReview: saved });
    api.startIssueDeliveryCorrection.mockRejectedValueOnce(new ApiError("not confirmed", 503, "Unavailable"));
    const user = userEvent.setup(); mount();
    expect(await screen.findByText(/Start a correction run, then review its result/)).toBeInTheDocument();
    await user.click(await screen.findByRole("button", { name: "Start correction" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Your review is saved");
    api.startIssueDeliveryCorrection.mockResolvedValue({ reviewId: saved.id, taskId: "correction-run" });
    api.getIssueDelivery.mockResolvedValue({ ...initial, reviewStale: true, latestReview: { ...saved, correctionTaskId: "correction-run" } });
    await user.click(screen.getByRole("button", { name: "Start correction" }));
    expect(await screen.findByRole("status")).toHaveTextContent("new human review");
    expect(screen.queryByRole("button", { name: "Start correction" })).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(api.startIssueDeliveryCorrection.mock.calls).toEqual([["issue-1", saved.id], ["issue-1", saved.id]]);
  });
  it("shows immutable acceptance cost and reads older results without replacing current evidence", async () => {
  const saved = { id: "review-1", decision: "accepted", feedback: "Accepted before the follow-up", snapshot: { ...initial, run: { ...initial.run, result: { summary: "Original accepted result" } } }, snapshotToken: initial.snapshotToken,
    reviewedBy: "member-1", createdAt: "2026-09-05T00:01:00Z", assessments: [{passed: true, evidence: "Original acceptance evidence"}], correctionTaskId: null,
    usageSnapshot: {capturedAt:"2026-09-05T00:01:00Z", status:"partial", availableUsd:"1.7500000000", reportedUsd:"1.0000000000", estimatedUsd:"0.7500000000", runIds:["run-1","run-2"], runsWithoutUsage:1, nonterminalRuns:0, unpricedSlices:0}, reviewDelaySeconds:60 };
  api.getIssueDelivery.mockResolvedValue({...initial, latestReview:saved, metrics:{reviewCount:1,reviewedResults:1,acceptedResults:1,correctionRequests:0,acceptanceReversals:0}});
  api.getIssueDeliveryHistory.mockResolvedValue({reviews:[saved],nextBeforeId:null});
  const user = userEvent.setup(); mount();
  expect(await screen.findByText(/Cost at acceptance:/)).toHaveTextContent("$1.75");
  expect(screen.getByText(/1 \/ 1 reviewed results/)).toBeInTheDocument();
  await user.click(screen.getByText("Review history",{exact:true}));
  expect(await screen.findAllByText(/Cost at acceptance:/)).toHaveLength(2);
  expect(api.getIssueDeliveryHistory).toHaveBeenCalledWith("issue-1",undefined);
  expect(screen.getByText("Added the next step")).toBeInTheDocument();
});
});
