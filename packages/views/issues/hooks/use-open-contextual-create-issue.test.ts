// @vitest-environment node
import { describe, expect, it } from "vitest";
import { issueCreateContext } from "./use-open-contextual-create-issue";

const cycles: Record<string, string> = { "cycle-1": "project-9" };
const cycleProject = (id: string) => cycles[id];

describe("issueCreateContext", () => {
  it.each([
    ["/acme/projects/project-1", { project_id: "project-1" }],
    ["/acme/projects/project-1/", { project_id: "project-1" }],
    ["/acme/cycles/cycle-1", { project_id: "project-9", cycle_id: "cycle-1" }],
    // A cycle not in cache yet: file nothing rather than a cycle without its project.
    ["/acme/cycles/cycle-unknown", undefined],
    ["/acme/projects", undefined],
    ["/acme/issues", undefined],
    ["/acme/issues/MUL-1", undefined],
  ])("%s", (pathname, expected) => {
    expect(issueCreateContext(pathname, cycleProject)).toEqual(expected);
  });
});
