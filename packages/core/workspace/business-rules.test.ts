// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { businessRuleKeys } from "./business-rules";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const rule = { id: "r1", title: "Three projects", natural_language: "at most three projects", predicate: { all: [] }, description: "…", attach_point: "project_create", status: "draft" };

describe("business rules client", () => {
  it("lists rules, tolerating a malformed list", async () => {
    stubFetch({ rules: [rule], attach_points: ["project_create"] });
    const list = await new ApiClient("https://api.example.test").listBusinessRules();
    expect(list.rules[0]?.title).toBe("Three projects");
    stubFetch({ rules: "nope" });
    expect((await new ApiClient("https://api.example.test").listBusinessRules()).rules).toEqual([]);
    expect(businessRuleKeys.violations("w", "r")).toEqual(["business-rules", "w", "violations", "r"]);
  });

  it("returns the created draft and the dry-run, rejecting malformed envelopes", async () => {
    stubFetch({ rule });
    const created = await new ApiClient("https://api.example.test").createBusinessRule({ natural_language: "x", attach_point: "project_create" });
    expect(created.status).toBe("draft");
    stubFetch({ rule, checked: 1, violations: [{ subject_type: "project_create", subject_id: "w", label: "the next project", detail: "d" }] });
    const dry = await new ApiClient("https://api.example.test").dryRunBusinessRule("r1");
    expect(dry.violations[0]?.label).toBe("the next project");
    stubFetch({ nope: 1 });
    await expect(new ApiClient("https://api.example.test").createBusinessRule({ natural_language: "x", attach_point: "project_create" })).rejects.toThrow();
    stubFetch({ rule: { ...rule, status: "active" } });
    expect((await new ApiClient("https://api.example.test").setBusinessRuleStatus("r1", "active")).status).toBe("active");
  });
});
