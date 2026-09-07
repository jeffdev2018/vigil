import { afterEach, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { DecisionCard, InboxDecisions } from "./inbox-decisions";
import { IssueDecisionSchema } from "@multica/core/api/schemas";
import inbox from "../../locales/en/inbox.json";
vi.mock("../../i18n", () => ({ useT: () => ({ t: (selector: (value: typeof inbox) => string, params?: Record<string,string>) => {
  let value = selector(inbox); for (const [key,replacement] of Object.entries(params ?? {})) value = value.replace(`{{${key}}}`,replacement); return value;
} }) }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ inbox: () => "/acme/inbox", issueDetail: (id: string) => `/acme/issues/${id}` }) }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ replace: vi.fn() }), AppLink: (props: React.AnchorHTMLAttributes<HTMLAnchorElement>) => <a {...props}/> }));
const id = "11111111-1111-4111-8111-111111111111";
const run = "22222222-2222-4222-8222-222222222222";
const row = { id, issue_id: id, agent_id: id, source_task_id: id, recipient_id: id, requested_by: id,
  requester_type: "agent", question: "A or B?", context: "Tradeoffs", options: ["A", "B"], status: "open", answer: null,
  answered_by: null, answered_at: null, resume_task_id: null, created_at: "2026-09-06T00:00:00Z" };
const answered = {...row,status:"answered",answer:"A",answered_by:id,answered_at:row.created_at};
const clients: QueryClient[] = [];
function mount(element: React.ReactNode) {
  setApiInstance(new ApiClient("https://api.example.test"));
  const client = new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}}); clients.push(client);
  return render(<QueryClientProvider client={client}>{element}</QueryClientProvider>);
}
function response(body: unknown, status = 200) { return new Response(JSON.stringify(body),{status,headers:{"Content-Type":"application/json"}}); }
afterEach(() => { clients.splice(0).forEach(client=>client.clear()); vi.unstubAllGlobals(); });
it("choosing an option does not submit; answer survives failed resume and retry recovers the receipt", async () => {
  let resumes=0;
  const fetch = vi.fn().mockImplementation(async (url: string) => url.endsWith("/answer") ? response(answered) : ++resumes === 1 ? response({error:"Unavailable"},409) : response({...answered,resume_task_id:run}));
  vi.stubGlobal("fetch",fetch);
  mount(<DecisionCard wsId="workspace" decision={IssueDecisionSchema.parse(row)}/>);
  fireEvent.click(screen.getByRole("button",{name:"A"}));
  expect(fetch).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button",{name:"Save answer"}));
  await screen.findByText("Answer saved"); expect(resumes).toBe(0);
  fireEvent.click(screen.getByRole("button",{name:"Start follow-up"}));
  expect(await screen.findByRole("alert")).toHaveTextContent("Your answer is saved");
  expect(screen.getByText("Answer saved")).toBeVisible();
  fireEvent.click(screen.getByRole("button",{name:"Start follow-up"}));
  expect(await screen.findByText(`Follow-up run: ${run}`)).toBeVisible();
  expect(screen.queryByRole("button",{name:"Start follow-up"})).toBeNull();
});
it("requires explicit cancellation and never shows a resume for a cancelled request", async () => {
  const fetch=vi.fn().mockResolvedValue(response({...row,status:"cancelled",answered_by:id,answered_at:row.created_at}));vi.stubGlobal("fetch",fetch);
  mount(<DecisionCard wsId="workspace" decision={IssueDecisionSchema.parse(row)}/>);
  fireEvent.click(screen.getByRole("button",{name:"Cancel request"})); expect(fetch).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button",{name:"Confirm cancellation"}));
  await screen.findByText("Request cancelled. No follow-up can be started.");
  expect(screen.queryByRole("button",{name:"Start follow-up"})).toBeNull();
});
it("shows load failures rather than an empty inbox, and separates history", async () => {
  const fetch=vi.fn().mockResolvedValueOnce(response({},503)).mockImplementation(async () => response({decisions:[],next_before_id:null}));vi.stubGlobal("fetch",fetch);
  mount(<InboxDecisions/>);
  await screen.findByRole("alert"); expect(screen.queryByText("No decisions need your attention.")).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"Try again"})); await screen.findByText("No decisions need your attention.");
  fireEvent.click(screen.getByRole("button",{name:"History"})); await screen.findByText("No resolved decisions yet.");
  await waitFor(()=>expect(fetch.mock.calls.some(([url])=>String(url).includes("history=true"))).toBe(true));
});
