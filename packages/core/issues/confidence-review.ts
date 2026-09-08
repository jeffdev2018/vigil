import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Confidence review (JEF-240): runs whose confidence score lands under the
// workspace threshold move the issue into review and raise an inbox item.

export interface ConfidenceReviewSettings {
  enabled: boolean;
  /** 0 < threshold ≤ 1, enforced server-side. */
  threshold: number;
  /** Cascade escalations (JEF-272): integer in [0, 3], enforced server-side. */
  max_escalations: number;
}

export const confidenceReviewKeys = {
  settings: (wsId: string) => ["confidence-review", wsId, "settings"] as const,
};

export function confidenceReviewSettingsOptions(wsId: string) {
  return queryOptions({ queryKey: confidenceReviewKeys.settings(wsId), queryFn: () => api.getConfidenceReviewSettings() });
}

export function useSaveConfidenceReviewSettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: ConfidenceReviewSettings) => api.putConfidenceReviewSettings(v),
    onSettled: () => qc.invalidateQueries({ queryKey: confidenceReviewKeys.settings(wsId) }),
  });
}
