import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Getting-started checklist (OS plan, chantier 5). Backs the dismissible
 * card shown on a fresh workspace's landing page until every row is done.
 */
export const onboardingChecklistKeys = {
  all: (wsId: string) => ["onboarding-checklist", wsId] as const,
};

export function onboardingChecklistOptions(wsId: string) {
  return queryOptions({
    queryKey: onboardingChecklistKeys.all(wsId),
    queryFn: () => api.getOnboardingChecklist(),
  });
}
