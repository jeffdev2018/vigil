import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { triageKeys } from "./queries";

/**
 * Mint the workspace's email intake endpoint, or rotate its token when one
 * already exists.
 *
 * Not optimistic, and deliberately so: the token exists only in the server's
 * response — it is stored as a digest — so there is nothing to predict and
 * nothing to roll back. Rotation revokes the previous token the moment the
 * server answers, which is also why the caller must await it before showing
 * anything.
 *
 * The stats query carries the source list the settings screen reads to know
 * whether intake is on, so it is invalidated on settle.
 */
export function useCreateTriageEmailSource(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.createTriageEmailSource(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: triageKeys.stats(wsId) });
    },
  });
}
