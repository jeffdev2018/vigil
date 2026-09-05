// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError, errorCode } from "../api/client";
import { CALENDAR_DEFAULT_WITHIN, calendarKeys, calendarUpcomingOptions } from "./queries";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(typeof body === "string" ? body : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

function client() {
  return new ApiClient("https://api.example.test");
}

afterEach(() => vi.unstubAllGlobals());

const validEvent = {
  summary: "Sprint review",
  url: "https://meet.example.test/x",
  start: "2026-09-04T09:00:00Z",
  end: "2026-09-04T10:00:00Z",
  in_progress: true,
};

describe("calendarUpcoming", () => {
  it("parses a well-formed answer and passes the window through", async () => {
    stubFetchJson({ events: [validEvent], configured: true });
    const res = await client().calendarUpcoming("30m");
    expect(res.configured).toBe(true);
    expect(res.events[0]?.summary).toBe("Sprint review");
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toContain("/api/calendar/upcoming?within=30m");
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ events: "not an array", configured: "yes" });
    const res = await client().calendarUpcoming();
    expect(res.events).toEqual([]);
    expect(res.configured).toBe(false);
  });

  // A feed with nothing in it and a user with no feed both answer 200 with an
  // empty list; only `configured` tells them apart, so it must not be guessed.
  it("keeps configured false when the field is absent", async () => {
    stubFetchJson({ events: [] });
    expect((await client().calendarUpcoming()).configured).toBe(false);
  });

  it("keeps the 502 code the Check-feed button reports", async () => {
    stubFetchJson({ error: "the calendar feed answered 404", code: "calendar_feed_failed" }, 502);
    await client()
      .calendarUpcoming()
      .then(
        () => expect.unreachable("502 must reject"),
        (err) => {
          expect(err).toBeInstanceOf(ApiError);
          expect(errorCode(err)).toBe("calendar_feed_failed");
        },
      );
  });
});

describe("calendar feed CRUD", () => {
  it("parses the saved feed", async () => {
    stubFetchJson({ url: "https://cal.example.test/f.ics", last_fetched_at: "2026-09-04T09:00:00Z" });
    const feed = await client().setCalendarFeed("webcal://cal.example.test/f.ics");
    expect(feed.url).toBe("https://cal.example.test/f.ics");
    expect(vi.mocked(fetch).mock.calls[0]?.[1]?.method).toBe("PUT");
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ url: 42 });
    expect((await client().getCalendarFeed()).url).toBe("");
  });

  it("keeps a 400 on a refused URL as an ApiError", async () => {
    stubFetchJson({ error: "url must start with https:// or webcal://" }, 400);
    await expect(client().setCalendarFeed("http://x/y.ics")).rejects.toBeInstanceOf(ApiError);
  });

  it("resolves the delete on 204", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
    await expect(client().deleteCalendarFeed()).resolves.toBeUndefined();
  });
});

describe("calendar query keys", () => {
  // Workspace-scoped: the feed is saved per workspace, so two workspaces must
  // not read each other's cached answer.
  it("scopes every key by workspace and window", () => {
    expect(calendarKeys.feed("ws-1")).not.toEqual(calendarKeys.feed("ws-2"));
    expect(calendarKeys.upcoming("ws-1", "30m")).not.toEqual(
      calendarKeys.upcoming("ws-1", "336h"),
    );
    expect(calendarUpcomingOptions("ws-1").queryKey).toEqual(
      calendarKeys.upcoming("ws-1", CALENDAR_DEFAULT_WITHIN),
    );
    // Nothing is asked before a workspace is known.
    expect(calendarUpcomingOptions("").enabled).toBe(false);
  });
});
