// @vitest-environment node
import { describe, expect, it } from "vitest";
import { dryRunSamples, parseDryRunPayload } from "./webhook-dry-run-samples";

describe("dryRunSamples", () => {
  it("falls back to a generic event when the trigger declares no filters", () => {
    const samples = dryRunSamples({ provider: "generic", event_filters: [] });
    expect(samples).toHaveLength(1);
    expect(JSON.parse(samples[0]!.payload)).toMatchObject({ event: "deploy.finished" });
    expect(samples[0]!.headers).toEqual({});
  });

  it("emits one sample per action so every matcher branch is reachable", () => {
    const samples = dryRunSamples({
      provider: "github",
      event_filters: [{ event: "pull_request", actions: ["opened", "closed"] }],
    });
    expect(samples.map((s) => s.id)).toEqual(["pull_request.opened", "pull_request.closed"]);
    // GitHub infers the event from the header, not the body.
    expect(samples[0]!.headers).toEqual({ "X-GitHub-Event": "pull_request" });
    expect(JSON.parse(samples[0]!.payload)).toEqual({ action: "opened" });
  });

  it("uses the envelope shape for non-github providers", () => {
    const samples = dryRunSamples({
      provider: "generic",
      event_filters: [{ event: "deploy" }],
    });
    expect(samples[0]!.headers).toEqual({});
    expect(JSON.parse(samples[0]!.payload)).toEqual({ event: "deploy", eventPayload: {} });
  });

  it("skips filter rows with no event name", () => {
    const samples = dryRunSamples({
      provider: "generic",
      event_filters: [{ event: "" }, { event: "push" }],
    });
    expect(samples.map((s) => s.id)).toEqual(["push"]);
  });
});

describe("parseDryRunPayload", () => {
  it("accepts objects and arrays", () => {
    expect(parseDryRunPayload('{"a":1}')).toEqual({ ok: true, value: { a: 1 } });
    expect(parseDryRunPayload("[1,2]")).toEqual({ ok: true, value: [1, 2] });
  });

  it("reports an empty box without an error message", () => {
    expect(parseDryRunPayload("   ")).toEqual({ ok: false, empty: true, message: "" });
  });

  it("rejects scalars the server would 400 on", () => {
    expect(parseDryRunPayload('"hello"')).toMatchObject({ ok: false, message: "object_or_array" });
    expect(parseDryRunPayload("null")).toMatchObject({ ok: false, message: "object_or_array" });
  });

  it("surfaces the parser's own message for broken JSON", () => {
    const result = parseDryRunPayload("{oops");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.empty).toBe(false);
      expect(result.message).not.toBe("");
    }
  });
});
