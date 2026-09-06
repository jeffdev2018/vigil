import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { reviewFlagKeys } from "./queries";
import type { ReviewFlagState } from "./schemas";

/**
 * Resolve / dismiss / reopen. Not optimistic: the counts are a server-side
 * projection over the whole list, so patching one row's state locally would
 * leave the badge disagreeing with the rows under it until the refetch lands.
 * The write is a single click on a screen the reviewer stays on, so the
 * round trip is the whole latency.
 */
export function useSetReviewFlagState(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ flagId, state }: { flagId: string; state: ReviewFlagState }) =>
      api.setReviewFlagState(issueId, flagId, state),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: reviewFlagKeys.all(wsId) });
    },
  });
}
