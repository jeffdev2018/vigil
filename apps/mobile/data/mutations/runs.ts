/**
 * Runs fleet mutations (OS plan, chantier 4) — cancel, kill switch, lift
 * halt. None of these are optimistic: a bulk cancel's per-run outcome
 * (`cancelled` / `already_over` / `not_found` / `error`) isn't locally
 * predictable, and the kill switch / halt toggle change what every gated
 * action in the workspace does — exactly the kind of thing that must
 * reflect what the server actually holds, not what this client hoped to
 * set (root CLAUDE.md optimistic-update gate; mirrors web's
 * `useSetRunHalt` in packages/core/run-halt/mutations.ts).
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/data/api";
import { runsKeys } from "@/data/queries/runs";

/** Cancels one or many runs and reports every outcome. */
export function useCancelRuns(wsId: string | null) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (taskIds: string[]) => api.cancelRuns(taskIds),
    onSettled: () => qc.invalidateQueries({ queryKey: runsKeys.all(wsId) }),
  });
}

/** Owner/admin: halts the fleet then cancels every run in flight. */
export function useKillSwitch(wsId: string | null) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (reason: string) => api.killSwitch(reason),
    onSettled: () => qc.invalidateQueries({ queryKey: runsKeys.all(wsId) }),
  });
}

/** Owner/admin: lifts a halt the fleet is currently under. Mobile never
 *  sets the halt through this call — that's the kill switch's job, so a
 *  halt is never left on without the cancel sweep that makes it safe. */
export function useLiftRunHalt(wsId: string | null) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.putRunHalt({ halted: false, reason: "" }),
    onSettled: () => qc.invalidateQueries({ queryKey: runsKeys.all(wsId) }),
  });
}
