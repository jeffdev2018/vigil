/**
 * Chromium parcours rendu for ActivationReadinessCard.
 * Run from repo root: node docs/research/activation-readiness-preview-2026-09-07.cjs
 * Captures → /tmp/vigil-activation/
 */
const fs = require("node:fs/promises");
const { createRequire } = require("node:module");
const { spawn } = require("node:child_process");
const path = require("node:path");

const root = path.resolve(__dirname, "../..");
const out = "/tmp/vigil-activation";
const req = createRequire(path.join(root, "package.json"));
const webReq = createRequire(path.join(root, "apps/web/package.json"));
const { chromium, expect } = req("@playwright/test");

(async () => {
  let server;
  let browser;
  const fixture = await fs.mkdtemp(
    path.join(root, "packages/views/.activation-"),
  );
  await fs.mkdir(out, { recursive: true });
  try {
    const postcss = webReq("postcss");
    const tailwind = webReq("@tailwindcss/postcss");
    const css = await postcss([tailwind()]).process(
      await fs.readFile(path.join(root, "apps/web/app/globals.css"), "utf8"),
      { from: path.join(root, "apps/web/app/globals.css") },
    );
    let fonts = "";
    try {
      const previous = await fs.readFile(
        "/tmp/vigil-memory-usage/preview-harness/preview.css",
        "utf8",
      );
      fonts = [...previous.matchAll(/@font-face\s*\{[^}]+\}/g)]
        .map((m) => m[0])
        .join("\n");
    } catch {
      /* Inter may fall back to system */
    }
    await fs.writeFile(
      path.join(fixture, "preview.css"),
      `${fonts}\n${css.css}\n:root{--font-inter:Inter}`,
    );

    await fs.writeFile(
      path.join(fixture, "mock-hooks.ts"),
      `export const useWorkspaceId = () => "ws-1";\n`,
    );
    await fs.writeFile(
      path.join(fixture, "mock-paths.ts"),
      `export const useCurrentWorkspace = () => ({ id: "ws-1", repos: [] });
export const useWorkspacePaths = () => ({
  runtimes: () => "/ws/runtimes",
  settings: () => "/ws/settings",
});
`,
    );
    await fs.writeFile(
      path.join(fixture, "mock-auth.ts"),
      `export const useAuthStore = (selector: (s: { user: { onboarded_at: string } }) => unknown) =>
  selector({ user: { onboarded_at: "2026-09-07T00:00:00Z" } });
`,
    );
    await fs.writeFile(
      path.join(fixture, "mock-analytics.ts"),
      `export const captureEvent = (..._args: unknown[]) => {};
export const captureException = () => {};
export const initAnalytics = () => false;
`,
    );
    await fs.writeFile(
      path.join(fixture, "mock-navigation.ts"),
      `import React from "react";
export const AppLink = ({ href, children, className, ...rest }: React.AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }) =>
  React.createElement("a", { href, className, ...rest }, children);
export const useNavigation = () => ({ push: () => {}, openInNewTab: () => {}, getShareableUrl: (h: string) => h, prefetch: () => {} });
`,
    );

    await fs.writeFile(
      path.join(fixture, "main.tsx"),
      `import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import { ActivationReadinessCard } from "../runtimes/components/activation-readiness-card";
import runtimes from "../locales/en/runtimes.json";
import common from "../locales/en/common.json";

setApiInstance(new ApiClient(location.origin));

function App() {
  const [scenario, setScenario] = useState<"blocked" | "ready">("blocked");
  const [client, setClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  );
  const switchScenario = (next: "blocked" | "ready") => {
    setScenario(next);
    setClient(new QueryClient({ defaultOptions: { queries: { retry: false } } }));
  };
  return (
    <QueryClientProvider client={client}>
      <main className="mx-auto max-w-3xl p-6 space-y-4">
        <div className="flex gap-2">
          <button type="button" onClick={() => switchScenario("blocked")}>Show blocked</button>
          <button type="button" onClick={() => switchScenario("ready")}>Show ready</button>
          <span data-testid="scenario">{scenario}</span>
        </div>
        <ActivationReadinessCard key={scenario} machines={[]} />
        {scenario === "ready" ? (
          <p data-testid="ready-note">Checklist hidden when every required step is ready.</p>
        ) : null}
      </main>
    </QueryClientProvider>
  );
}

createRoot(document.getElementById("root")!).render(
  <I18nProvider locale="en" resources={{ en: { runtimes, common } }}>
    <App />
  </I18nProvider>,
);
`,
    );

    await fs.writeFile(
      path.join(fixture, "index.html"),
      `<!doctype html><html lang="en"><head><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/preview.css"></head><body class="bg-background text-foreground font-sans"><div id="root"></div><script type="module" src="/main.tsx"></script></body></html>`,
    );

    await fs.writeFile(
      path.join(fixture, "vite.config.mjs"),
      `import react from "@vitejs/plugin-react";
import path from "node:path";
import { fileURLToPath } from "node:url";
const fixture = path.dirname(fileURLToPath(import.meta.url));
const mockNav = path.join(fixture, "mock-navigation.ts");
function mockNavPlugin() {
  return {
    name: "mock-views-navigation",
    enforce: "pre",
    resolveId(id, importer) {
      if (id === mockNav) return mockNav;
      const bare =
        id === "../../navigation" ||
        id === "../navigation" ||
        id.endsWith("/navigation") ||
        id.includes("packages/views/navigation");
      if (bare) return mockNav;
      if (
        importer &&
        importer.includes("activation-readiness-card") &&
        (id.includes("navigation") || id.endsWith("/navigation"))
      ) {
        return mockNav;
      }
      return null;
    },
  };
}
export default {
  plugins: [react(), mockNavPlugin()],
  server: { host: "127.0.0.1", port: 0, fs: { allow: ["${root}"] } },
  resolve: {
    dedupe: ["react", "react-dom"],
    alias: [
      { find: "@multica/core/hooks", replacement: path.join(fixture, "mock-hooks.ts") },
      { find: "@multica/core/paths", replacement: path.join(fixture, "mock-paths.ts") },
      { find: "@multica/core/auth", replacement: path.join(fixture, "mock-auth.ts") },
      { find: "@multica/core/analytics", replacement: path.join(fixture, "mock-analytics.ts") },
      { find: path.join("${root}", "packages/views/navigation"), replacement: mockNav },
      { find: path.join("${root}", "packages/views/navigation/index.ts"), replacement: mockNav },
      { find: path.join("${root}", "packages/views/navigation/index"), replacement: mockNav },
    ],
  },
};
`,
    );

    server = spawn("pnpm", ["exec", "vite", "--host", "127.0.0.1", "--port", "0"], {
      cwd: fixture,
      detached: true,
      stdio: ["ignore", "pipe", "pipe"],
    });
    const url = await new Promise((resolve, reject) => {
      let output = "";
      const timer = setTimeout(() => reject(Error(output || "vite timeout")), 45000);
      const read = (b) => {
        output += b;
        const match = output.match(/http:\/\/127\.0\.0\.1:\d+\//);
        if (match) {
          clearTimeout(timer);
          resolve(match[0]);
        }
      };
      server.stdout.on("data", read);
      server.stderr.on("data", read);
      server.on("error", reject);
    });

    browser = await chromium.launch({ headless: true });
    const page = await browser.newPage({ viewport: { width: 1100, height: 850 } });
    const errors = [];
    page.on("pageerror", (e) => {
      errors.push(e.message);
      console.error(e.message);
    });

    let mode = "blocked";
    await page.route("**/api/**", async (route) => {
      const u = new URL(route.request().url());
      if (!u.pathname.startsWith("/api/")) return route.continue();
      if (u.pathname.startsWith("/api/runtimes")) {
        if (mode === "ready") {
          return route.fulfill({
            json: [
              {
                id: "rt-1",
                workspace_id: "ws-1",
                daemon_id: "d-1",
                name: "Claude",
                runtime_mode: "local",
                provider: "claude",
                launch_header: "",
                status: "online",
                device_info: "dev",
                metadata: { cli_auth: { authenticated: true } },
                owner_id: "u-1",
                visibility: "private",
                last_seen_at: "2026-09-07T00:00:00Z",
                created_at: "2026-09-07T00:00:00Z",
                updated_at: "2026-09-07T00:00:00Z",
              },
            ],
          });
        }
        return route.fulfill({ json: [] });
      }
      if (u.pathname.startsWith("/api/agents")) {
        if (mode === "ready") {
          return route.fulfill({
            json: [{ id: "mika", system_key: "mika", name: "Mika", workspace_id: "ws-1" }],
          });
        }
        return route.fulfill({ json: [] });
      }
      if (u.pathname.startsWith("/api/chat/sessions")) {
        if (mode === "ready") {
          return route.fulfill({
            json: [
              {
                id: "cs-1",
                agent_id: "mika",
                workspace_id: "ws-1",
                creator_id: "u-1",
                title: "Mika",
                status: "active",
                has_unread: false,
                last_message: {
                  id: "msg-1",
                  role: "assistant",
                  content: "hi",
                  created_at: "2026-09-07T00:00:00Z",
                },
                created_at: "2026-09-07T00:00:00Z",
                updated_at: "2026-09-07T00:00:00Z",
              },
            ],
          });
        }
        return route.fulfill({ json: [] });
      }
      if (u.pathname.includes("/github/installations")) {
        return route.fulfill({
          json: {
            installations:
              mode === "ready"
                ? [
                    {
                      id: "inst-1",
                      workspace_id: "ws-1",
                      account_login: "acme",
                      account_type: "Organization",
                      account_avatar_url: null,
                      created_at: "2026-09-07T00:00:00Z",
                    },
                  ]
                : [],
            configured: true,
          },
        });
      }
      return route.fulfill({ json: [] });
    });

    await page.goto(url);
    await page.waitForTimeout(1500);
    if (errors.length) {
      console.error("pageerrors", errors);
      await page.screenshot({
        path: path.join(out, "debug-error.png"),
        fullPage: true,
      });
    }
    const bodyText = await page.locator("body").innerText();
    if (!bodyText.includes("Ready for a first result")) {
      console.error("body", bodyText.slice(0, 1500));
      await page.screenshot({
        path: path.join(out, "debug-missing.png"),
        fullPage: true,
      });
    }
    await expect(
      page.getByRole("region", { name: "Ready for a first result?" }),
    ).toBeVisible({ timeout: 15000 });
    await expect(page.getByText("Machine online")).toBeVisible();
    await expect(page.getByText("Optional")).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({
      path: path.join(out, "blocked-desktop.png"),
      fullPage: true,
      animations: "disabled",
    });

    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({
      path: path.join(out, "blocked-phone.png"),
      fullPage: true,
      animations: "disabled",
    });
    const phoneMetrics = await page.evaluate(() => ({
      viewport: innerWidth,
      width: document.documentElement.scrollWidth,
      font: document.fonts.check("14px Inter"),
    }));
    expect(phoneMetrics.width).toBe(390);

    mode = "ready";
    await page.setViewportSize({ width: 1100, height: 850 });
    await page.getByRole("button", { name: "Show ready" }).click();
    await expect(page.getByTestId("ready-note")).toBeVisible();
    await expect(
      page.getByRole("region", { name: "Ready for a first result?" }),
    ).toHaveCount(0);
    await page.screenshot({
      path: path.join(out, "ready-hidden-desktop.png"),
      fullPage: true,
      animations: "disabled",
    });

    expect(errors).toEqual([]);
    console.log(
      JSON.stringify({
        out,
        phoneMetrics,
        errors,
        captures: [
          "blocked-desktop.png",
          "blocked-phone.png",
          "ready-hidden-desktop.png",
        ],
      }),
    );
  } finally {
    if (browser) await browser.close();
    if (server?.pid) {
      try {
        process.kill(-server.pid, "SIGTERM");
      } catch (e) {
        if (e.code !== "ESRCH") throw e;
      }
    }
    await fs.cp(fixture, path.join(out, "preview-harness"), { recursive: true });
    await fs.rm(fixture, { recursive: true, force: true });
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
