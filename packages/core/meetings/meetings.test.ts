// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError, errorCode } from "../api/client";
import {
  MEETINGS_PAGE_SIZE,
  MEETING_SUMMARY_STALL_MS,
  isMeetingSummaryStalled,
  meetingDetailOptions,
  meetingKeys,
  meetingListOptions,
} from "./queries";
import { useMeetingPreferencesStore } from "./preferences-store";
import { useMeetingRecorderStore, openMeetingRecorder, requestStopRecording } from "./store";

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

afterEach(() => {
  vi.unstubAllGlobals();
  useMeetingRecorderStore.getState().reset();
});

const validMeeting = {
  id: "meet-1",
  title: "Weekly sync",
  app_name: "Zoom",
  status: "done",
  transcript: "hello there",
  summary_markdown: "- decided to ship",
  segment_count: 4,
  created_by: "user-1",
  started_at: "2026-01-01T09:00:00Z",
  ended_at: "2026-01-01T09:30:00Z",
  actions: [
    {
      triage_item_id: "item-1",
      title: "Send the recap",
      state: "pending",
    },
  ],
};

describe("listMeetings", () => {
  it("parses a well-formed list", async () => {
    stubFetchJson({ meetings: [validMeeting] });
    const res = await client().listMeetings();
    expect(res.meetings).toHaveLength(1);
    expect(res.meetings[0]?.title).toBe("Weekly sync");
    expect(res.meetings[0]?.actions[0]?.triage_item_id).toBe("item-1");
  });

  it("fills defaults for fields an older server omits", async () => {
    stubFetchJson({ meetings: [{ id: "meet-2" }] });
    const res = await client().listMeetings();
    expect(res.meetings[0]?.title).toBe("");
    expect(res.meetings[0]?.app_name).toBe("");
    expect(res.meetings[0]?.segment_count).toBe(0);
    expect(res.meetings[0]?.actions).toEqual([]);
    expect(res.meetings[0]?.summary_unavailable).toBe(false);
    // An older server that does not send it must not hand the UI a delete
    // affordance the backend would refuse.
    expect(res.meetings[0]?.can_manage).toBe(false);
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ meetings: "not-an-array" });
    expect(await client().listMeetings()).toEqual({ meetings: [] });
  });

  it("keeps a 500 as an ApiError", async () => {
    stubFetchJson({ error: "boom" }, 500);
    await expect(client().listMeetings()).rejects.toBeInstanceOf(ApiError);
  });
});

describe("getMeeting", () => {
  it("parses transcript, summary and actions", async () => {
    stubFetchJson(validMeeting);
    const meeting = await client().getMeeting("meet-1");
    expect(meeting.transcript).toBe("hello there");
    expect(meeting.summary_markdown).toBe("- decided to ship");
    expect(meeting.actions).toHaveLength(1);
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ id: 42 });
    const meeting = await client().getMeeting("meet-1");
    expect(meeting.id).toBe("");
    expect(meeting.status).toBe("failed");
  });
});

describe("createMeeting", () => {
  it("parses the created meeting", async () => {
    stubFetchJson({ ...validMeeting, status: "recording", transcript: "" }, 201);
    const meeting = await client().createMeeting({ title: "Weekly sync", app_name: "Zoom" });
    expect(meeting.status).toBe("recording");
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ id: null, actions: "nope" }, 201);
    const meeting = await client().createMeeting();
    expect(meeting).toMatchObject({ id: "", actions: [] });
  });

  it("surfaces the 409 stt_not_configured code so the UI can hide the feature", async () => {
    stubFetchJson({ error: "transcription is not configured", code: "stt_not_configured" }, 409);
    const err = await client().createMeeting().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(errorCode(err)).toBe("stt_not_configured");
  });
});

describe("updateMeeting", () => {
  it("parses the renamed meeting", async () => {
    stubFetchJson({ ...validMeeting, title: "Sprint review" });
    expect((await client().updateMeeting("meet-1", { title: "Sprint review" })).title).toBe(
      "Sprint review",
    );
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ id: 42 });
    expect((await client().updateMeeting("meet-1", { title: "x" })).id).toBe("");
  });

  it("keeps a 403 as an ApiError", async () => {
    stubFetchJson({ error: "forbidden" }, 403);
    await expect(client().updateMeeting("meet-1", { title: "x" })).rejects.toBeInstanceOf(ApiError);
  });
});

describe("deleteMeeting", () => {
  it("resolves on 204 and keeps a 403 as an ApiError", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 204 })));
    await expect(client().deleteMeeting("meet-1")).resolves.toBeUndefined();
    stubFetchJson({ error: "forbidden" }, 403);
    await expect(client().deleteMeeting("meet-1")).rejects.toBeInstanceOf(ApiError);
  });
});

describe("appendMeetingSegment", () => {
  it("parses the transcribed chunk", async () => {
    stubFetchJson({ seq: "3", text: "and then we shipped", segment_count: 4 });
    const res = await client().appendMeetingSegment("meet-1", new Blob(["audio"]), 3);
    expect(res).toEqual({ seq: "3", text: "and then we shipped", segment_count: 4 });
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ seq: 3, text: [], segment_count: "four" });
    const res = await client().appendMeetingSegment("meet-1", new Blob(["audio"]), 3);
    expect(res).toEqual({ seq: "", text: "", segment_count: 0 });
  });

  it("surfaces meeting_not_recording so the recorder can stop", async () => {
    stubFetchJson({ error: "meeting is no longer recording", code: "meeting_not_recording" }, 409);
    const err = await client()
      .appendMeetingSegment("meet-1", new Blob(["audio"]), 1)
      .catch((e: unknown) => e);
    expect(errorCode(err)).toBe("meeting_not_recording");
  });
});

describe("finishMeeting", () => {
  it("parses the summarized meeting", async () => {
    stubFetchJson({ ...validMeeting, summary_unavailable: true });
    const meeting = await client().finishMeeting("meet-1");
    expect(meeting.summary_unavailable).toBe(true);
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ id: [], actions: 7, segment_count: "many" });
    const meeting = await client().finishMeeting("meet-1");
    expect(meeting.id).toBe("");
  });

  it("surfaces meeting_summarizing while a finish is already running", async () => {
    stubFetchJson({ error: "meeting is being summarized", code: "meeting_summarizing" }, 409);
    const err = await client().finishMeeting("meet-1").catch((e: unknown) => e);
    expect(errorCode(err)).toBe("meeting_summarizing");
  });
});

describe("resummarizeMeeting", () => {
  it("parses the re-summarized meeting", async () => {
    stubFetchJson({ ...validMeeting, summary_markdown: "- fresh" });
    expect((await client().resummarizeMeeting("meet-1")).summary_markdown).toBe("- fresh");
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ id: null, actions: "nope" });
    expect((await client().resummarizeMeeting("meet-1")).id).toBe("");
  });

  it("surfaces meeting_recording so the UI can tell the user to finish first", async () => {
    stubFetchJson({ code: "meeting_recording", error: "finish it first" }, 409);
    const err = await client().resummarizeMeeting("meet-1").catch((e: unknown) => e);
    expect(errorCode(err)).toBe("meeting_recording");
  });
});

describe("isMeetingSummaryStalled", () => {
  const startedAt = "2026-01-01T09:00:00Z";
  const start = Date.parse(startedAt);

  it("is false for every state but summarizing", () => {
    for (const status of ["recording", "done", "failed", "something-new"]) {
      expect(
        isMeetingSummaryStalled({ status, ended_at: startedAt }, start + 10 * 60_000),
      ).toBe(false);
    }
  });

  it("is false while the attempt is young and true once it is old", () => {
    expect(isMeetingSummaryStalled({ status: "summarizing", ended_at: startedAt }, start)).toBe(false);
    expect(
      isMeetingSummaryStalled(
        { status: "summarizing", ended_at: startedAt },
        start + MEETING_SUMMARY_STALL_MS - 1,
      ),
    ).toBe(false);
    expect(
      isMeetingSummaryStalled(
        { status: "summarizing", ended_at: startedAt },
        start + MEETING_SUMMARY_STALL_MS,
      ),
    ).toBe(true);
  });

  it("keeps polling rather than declaring a run stuck without a usable timestamp", () => {
    expect(isMeetingSummaryStalled({ status: "summarizing" }, start)).toBe(false);
    expect(isMeetingSummaryStalled({ status: "summarizing", ended_at: "nope" }, start)).toBe(false);
    expect(isMeetingSummaryStalled(undefined, start)).toBe(false);
  });
});

describe("meetingDetailOptions", () => {
  const interval = (data: unknown) =>
    (meetingDetailOptions("ws-1", "meet-1").refetchInterval as
      (q: { state: { data: unknown } }) => number | false)({ state: { data } });

  it("polls a live summarize and stops on every other state", () => {
    const fresh = new Date().toISOString();
    expect(interval({ status: "summarizing", ended_at: fresh })).toBe(3000);
    expect(interval({ status: "done", ended_at: fresh })).toBe(false);
    expect(interval(undefined)).toBe(false);
  });

  it("stops polling a summarize that has been running too long to still be alive", () => {
    const old = new Date(Date.now() - MEETING_SUMMARY_STALL_MS - 1000).toISOString();
    expect(interval({ status: "summarizing", ended_at: old })).toBe(false);
  });
});

describe("meetingListOptions paging", () => {
  const next = (pages: number[]) => {
    const all = pages.map((n) => ({ meetings: new Array(n).fill({}) }));
    const options = meetingListOptions("ws-1");
    return (options.getNextPageParam as (
      last: unknown,
      all: unknown[],
    ) => number | undefined)(all[all.length - 1], all);
  };

  it("asks for the next offset while pages come back full", () => {
    expect(next([MEETINGS_PAGE_SIZE])).toBe(MEETINGS_PAGE_SIZE);
    expect(next([MEETINGS_PAGE_SIZE, MEETINGS_PAGE_SIZE])).toBe(2 * MEETINGS_PAGE_SIZE);
  });

  it("stops on a short page — the endpoint reports no total", () => {
    expect(next([MEETINGS_PAGE_SIZE - 1])).toBeUndefined();
    expect(next([MEETINGS_PAGE_SIZE, 0])).toBeUndefined();
  });
});

describe("meetingKeys", () => {
  it("nests list and detail under the workspace prefix", () => {
    expect(meetingKeys.list("ws-1")).toEqual([...meetingKeys.all("ws-1"), "list"]);
    expect(meetingKeys.detail("ws-1", "meet-1")).toEqual([
      ...meetingKeys.all("ws-1"),
      "detail",
      "meet-1",
    ]);
  });
});

describe("recorder store", () => {
  it("bumps a nonce so the mounted recorder can start and stop from anywhere", () => {
    const before = useMeetingRecorderStore.getState();
    openMeetingRecorder({ title: "Standup", appName: "Meet" });
    expect(useMeetingRecorderStore.getState().openNonce).toBe(before.openNonce + 1);
    expect(useMeetingRecorderStore.getState().openOptions).toEqual({
      title: "Standup",
      appName: "Meet",
    });
    requestStopRecording();
    expect(useMeetingRecorderStore.getState().stopNonce).toBe(before.stopNonce + 1);
  });

  it("reset clears the active recording but keeps the capability flag", () => {
    const store = useMeetingRecorderStore.getState();
    store.started("meet-1", "2026-01-01T09:00:00Z", false);
    store.setLastTranscript("hello");
    store.setSttUnavailable(true);
    useMeetingRecorderStore.getState().reset();
    const after = useMeetingRecorderStore.getState();
    expect(after.phase).toBe("idle");
    expect(after.meetingId).toBeNull();
    expect(after.lastTranscript).toBe("");
    expect(after.systemAudio).toBe(true);
    // The server's "no STT configured" answer is not part of one recording.
    expect(after.sttUnavailable).toBe(true);
  });
});

describe("meeting preferences", () => {
  it("watches for meetings by default and remembers being turned off", () => {
    // On by default: the watcher only prompts, it never records on its own.
    expect(useMeetingPreferencesStore.getState().detectMeetings).toBe(true);
    useMeetingPreferencesStore.getState().setDetectMeetings(false);
    expect(useMeetingPreferencesStore.getState().detectMeetings).toBe(false);
    useMeetingPreferencesStore.getState().setDetectMeetings(true);
    expect(useMeetingPreferencesStore.getState().detectMeetings).toBe(true);
  });
});
