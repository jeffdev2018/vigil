import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { batchWindowKeys } from "./queries";
import { autopilotKeys } from "../autopilots/queries";
import type { BatchWindow } from "./schemas";

/**
 * Saving the window changes whether a batch-eligible autopilot is deferred at
 * all, and the autopilot dialog disables its toggle when no window exists — so
 * the autopilot lists are refetched with it rather than left describing a
 * window that no longer applies.
 */
export function useUpdateBatchWindow(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (window: BatchWindow) => api.putBatchWindow(window),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: batchWindowKeys.window(wsId) });
      qc.invalidateQueries({ queryKey: autopilotKeys.all(wsId) });
    },
  });
}
