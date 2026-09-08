// @vitest-environment jsdom
import { expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentMemory } from "@multica/core/types";
import agents from "../../../locales/en/agents.json";
const load = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { getAgentMemoryUsage: (...args: unknown[]) => load(...args) } }));
import { MemoryUsageSection } from "./memory-usage-section";

afterEach(()=>{cleanup();load.mockReset()});
const memoryId="11111111-1111-4111-8111-111111111111";
const usage={since:"2026-08-06T00:00:00Z",until:"2026-09-05T00:00:00Z",started_runs:6,recorded_runs:4,unrecorded_runs:2,load_failed_runs:1,runs_with_agent_memory:2,versions:[{memory_id:memoryId,revision:1,prepared_runs:2,last_started_at:"2026-09-04T12:00:00Z"}]};
function mount(memories:AgentMemory[]=[]){
 const history=vi.fn();const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 render(<I18nProvider locale="en" resources={{en:{agents}}}><QueryClientProvider client={client}><MemoryUsageSection wsId="ws" agentId="agent" memories={memories} onHistory={history}/></QueryClientProvider></I18nProvider>);
 return history;
}
it("keeps failures visible, retries, and distinguishes unknown coverage and old versions",async()=>{
 load.mockRejectedValueOnce(new Error("Unavailable")).mockResolvedValue(usage);
 const history=mount([{id:memoryId,agent_id:"agent",content:"Current rule",revision:3,source:"manual",status:"active",source_task_id:null,source_issue_id:null,created_at:"",updated_at:""}]);
 const user=userEvent.setup();
 expect(await screen.findByRole("alert")).toBeTruthy();
 expect(screen.queryByText("0 runs started · 0 contexts recorded")).toBeNull();
 await user.click(screen.getByRole("button",{name:"Try again"}));
 expect(await screen.findByText("6 runs started · 4 contexts recorded")).toBeTruthy();
 expect(screen.getByText(/not proof that the model used it or improved/)).toBeTruthy();
 await user.click(screen.getByText("Versions included: 1"));
 expect(screen.getByText("Revision 1 · 2 runs")).toBeTruthy();
 expect(screen.getByText("Current text: Current rule")).toBeTruthy();
 await user.click(screen.getByRole("button",{name:"History"}));
 expect(history).toHaveBeenCalledWith(memoryId);
 expect(load).toHaveBeenCalledWith("agent");
});
it("keeps recorded version references visible when current memory text was removed",async()=>{
 load.mockResolvedValue(usage);mount();
 await screen.findByText("6 runs started · 4 contexts recorded");
 await userEvent.click(screen.getByText("Versions included: 1"));
 expect(screen.getByText(memoryId)).toBeTruthy();
 expect(screen.queryByRole("button",{name:"History"})).toBeNull();
});
