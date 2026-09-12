import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { TwentyConnectInput, TwentySettingsInput } from "./schemas";
import { twentyKeys } from "./queries";

function invalidateAll(qc: ReturnType<typeof useQueryClient>, wsId: string) {
  qc.invalidateQueries({ queryKey: twentyKeys.status(wsId) });
  qc.invalidateQueries({ queryKey: twentyKeys.members(wsId) });
}

/**
 * Connect returns the connection with the clear inbound token, once. The
 * status query is invalidated so the card shows the connected state; the
 * token is the caller's to display until dismissed.
 */
export function useConnectTwenty(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: TwentyConnectInput) => api.connectTwenty(input),
    onSettled: () => invalidateAll(qc, wsId),
  });
}

export function useUpdateTwentySettings(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: TwentySettingsInput) => api.updateTwentySettings(input),
    onSettled: () => invalidateAll(qc, wsId),
  });
}

export function useCheckTwenty(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.checkTwenty(),
    onSettled: () => invalidateAll(qc, wsId),
  });
}

/** Awaits the server: a destructive flow never removes the card on optimism. */
export function useDisconnectTwenty(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.disconnectTwenty(),
    onSettled: () => invalidateAll(qc, wsId),
  });
}
