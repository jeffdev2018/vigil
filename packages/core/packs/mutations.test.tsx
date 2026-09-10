/**
 * @vitest-environment jsdom
 */
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { useUninstallPack } from "./mutations";
import { packKeys } from "./queries";
import { EMPTY_PACK_INSTALL, type PackInstall } from "./schemas";

// An uninstall's own row state comes from the response, not from the refetch
// the invalidation triggers: the Uninstall button must be gone the moment the
// mutation resolves, and a refetch that is slow, cached or failing would
// otherwise leave a removed pack looking installed (the server answers 409 to
// the second attempt).

const WS = "ws-1";

function install(over: Partial<PackInstall> = {}): PackInstall {
  return { ...EMPTY_PACK_INSTALL, id: "install-1", pack_id: "helpdesk-it", ...over };
}

function wrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useUninstallPack", () => {
  it("writes the returned install into the installs list", async () => {
    const removed = install({ status: "removed", removed_at: "2026-09-10T10:00:00Z" });
    setApiInstance({
      uninstallPack: vi.fn(async () => ({
        install: removed,
        report: { removed: {}, kept: [], reasons: [] },
      })),
    } as unknown as ApiClient);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    qc.setQueryData<PackInstall[]>(packKeys.installs(WS), [
      install(),
      install({ id: "install-2", pack_id: "sales" }),
    ]);

    const { result } = renderHook(() => useUninstallPack(WS), { wrapper: wrapper(qc) });
    result.current.mutate({ id: "install-1" });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const rows = qc.getQueryData<PackInstall[]>(packKeys.installs(WS)) ?? [];
    expect(rows.map((r) => r.status)).toEqual(["removed", "installed"]);
    expect(rows[0]?.removed_at).toBe("2026-09-10T10:00:00Z");
  });
});
