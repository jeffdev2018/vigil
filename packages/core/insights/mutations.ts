import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { insightKeys } from "./queries";
import type { CreateInsightWidgetInput, UpdateInsightWidgetInput } from "./schemas";

/**
 * Ask a question in plain language. Deliberately a mutation rather than a
 * query: it costs a model call, so it must run when the user presses Ask and
 * never on a refetch.
 */
export function useAskInsight() {
  return useMutation({
    mutationFn: (question: string) => api.askInsight(question),
  });
}

/**
 * Pinning navigates nowhere and the card list must show the server's row (id,
 * revision, position), so this awaits the server and invalidates — no
 * optimistic insert.
 */
export function usePinInsight(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateInsightWidgetInput) => api.createInsightWidget(input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: insightKeys.widgets(wsId) });
    },
  });
}

export function useUpdateInsightWidget(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateInsightWidgetInput }) =>
      api.updateInsightWidget(id, input),
    // Reordering moves several cards at once and a 409 rolls only one back, so
    // the list is re-read rather than patched.
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: insightKeys.widgets(wsId) });
    },
  });
}

export function useDeleteInsightWidget(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteInsightWidget(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: insightKeys.widgets(wsId) });
    },
  });
}
