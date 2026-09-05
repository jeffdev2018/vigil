import { test, expect } from "@playwright/test";
import { loginAsDefault, createTestApi } from "./helpers";
import type { TestApiClient } from "./fixtures";

// Chromium's fake media stack: getUserMedia yields a synthetic microphone
// without a prompt, and getDisplayMedia auto-picks the whole screen. The
// recorder therefore runs its real code path (mix, MediaRecorder, upload).
test.use({
  launchOptions: {
    args: [
      "--use-fake-device-for-media-capture",
      "--use-fake-ui-for-media-stream",
      "--auto-select-desktop-capture-source=Entire screen",
    ],
  },
  permissions: ["microphone"],
  screenshot: "only-on-failure",
});

test.describe("Meetings", () => {
  let api: TestApiClient;
  let slug: string;

  test.beforeEach(async ({ page }) => {
    api = await createTestApi();
    slug = await loginAsDefault(page);
  });

  test.afterEach(async () => {
    await api?.cleanup();
  });

  test("lists meetings created through the API", async ({ page }) => {
    const res = await api.authedFetch("/api/meetings", {
      method: "POST",
      body: JSON.stringify({ title: "E2E listed meeting", app_name: "Zoom" }),
    });
    // 409 means the server has no transcription provider: the page must then
    // show the capability banner instead of the list action.
    if (res.status === 409) {
      await page.goto(`/${slug}/meetings`, { waitUntil: "domcontentloaded" });
      await expect(page.getByText("Transcription is not configured")).toBeVisible();
      await expect(page.getByRole("button", { name: /record a meeting/i })).toBeDisabled();
      return;
    }
    expect(res.status).toBe(201);
    const created = await res.json();
    await api.authedFetch(`/api/meetings/${created.id}/finish`, { method: "POST" });

    await page.goto(`/${slug}/meetings`, { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: "Meetings" })).toBeVisible();
    await expect(page.getByText("E2E listed meeting")).toBeVisible();
    await expect(page.getByText("Zoom")).toBeVisible();

    await page.getByText("E2E listed meeting").click();
    await expect(page).toHaveURL(new RegExp(`/${slug}/meetings/${created.id}$`));
    await expect(page.getByRole("heading", { name: "Summary" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Action items" })).toBeVisible();
    await expect(page.getByText("Nothing transcribed yet")).toBeVisible();
  });

  test("records a meeting from the browser and finishes it", async ({ page }) => {
    await page.goto(`/${slug}/meetings`, { waitUntil: "domcontentloaded" });
    const record = page.getByRole("button", { name: /record a meeting/i });
    if (await record.isDisabled()) {
      test.skip(true, "server has no transcription provider configured");
    }
    await record.click();

    // Starting navigates to the new meeting, whose page hosts the live panel;
    // the pill is the shell-level control that follows the user around.
    await expect(page).toHaveURL(new RegExp(`/${slug}/meetings/[0-9a-f-]{36}$`), {
      timeout: 15000,
    });
    // The status chip in the header and the live panel both say "Recording".
    await expect(page.locator("header").getByText("Recording", { exact: true })).toBeVisible();
    // "Live transcript" when the server has a realtime provider, "Latest transcript" in batch mode.
    await expect(page.getByText(/^(Live|Latest) transcript$/)).toBeVisible();
    await expect(page.getByText(/Let participants know/)).toBeVisible();
    const stop = page.getByRole("button", { name: "Stop" }).first();
    await expect(stop).toBeEnabled();

    await page.waitForTimeout(1500);
    await stop.click();

    // Finishing flushes the tail chunk, uploads it, then summarizes.
    await expect(page.getByText("Meeting summarized")).toBeVisible({ timeout: 60000 });
    await expect(page.locator("header").getByText("Done", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Stop" })).toHaveCount(0);

    await page.goto(`/${slug}/meetings`, { waitUntil: "domcontentloaded" });
    // A cold dev server can take a while to hand over the route (same budget
    // as waitForPageText in helpers.ts).
    await expect(page.getByRole("heading", { name: "Meetings" })).toBeVisible({ timeout: 30000 });
    // The default title is "Meeting <date> <time>", set server-side.
    await expect(page.getByText(/^Meeting \d{4}-\d{2}-\d{2}/).first()).toBeVisible({
      timeout: 15000,
    });
  });
});
