import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

// Vigil learns you (K71): what it knows about me, here, and the levers to
// forget or correct it.

export interface WorkProfileObservation {
  id: string;
  key: string;
  kind: string;
  value: Record<string, unknown>;
  source: string;
  count: number;
  corrections: number;
  auto: boolean;
  state: "learned" | "proposed";
  stake: string;
  first_observed_at: string;
  last_observed_at: string;
}

export interface WorkProfile {
  observations: WorkProfileObservation[];
  examples: number;
  auto_decided: number;
  overturned: number;
  review_load_seconds: number;
  adaptation_surface: string[];
}

export const workProfileKeys = {
  me: (wsId: string) => ["work-profile", wsId] as const,
};

export function workProfileOptions(wsId: string) {
  return queryOptions({ queryKey: workProfileKeys.me(wsId), queryFn: () => api.getWorkProfile() });
}

export function useSetObservationAuto(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; auto: boolean }) => api.patchWorkProfileObservation(v.id, v.auto),
    onSuccess: (profile) => qc.setQueryData(workProfileKeys.me(wsId), profile),
    onSettled: () => qc.invalidateQueries({ queryKey: workProfileKeys.me(wsId) }),
  });
}

export function useForgetObservation(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteWorkProfileObservation(id),
    onSettled: () => qc.invalidateQueries({ queryKey: workProfileKeys.me(wsId) }),
  });
}

export function useOverturnDecisionExample(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.overturnDecisionExample(id),
    onSettled: () => qc.invalidateQueries({ queryKey: workProfileKeys.me(wsId) }),
  });
}

/** A rule's headline numbers, tolerant of an older or partial value. */
export function ruleSummary(o: WorkProfileObservation): { option_label: string; count: number; total: number; rate: number; family: string } {
  const v = o.value;
  const count = typeof v["count"] === "number" ? (v["count"] as number) : 0;
  const total = typeof v["total"] === "number" ? (v["total"] as number) : 0;
  return {
    option_label: typeof v["option_label"] === "string" ? (v["option_label"] as string) : String(v["option_id"] ?? ""),
    count, total, rate: total > 0 ? count / total : 0,
    family: typeof v["family"] === "string" ? (v["family"] as string) : "question",
  };
}

/** The busiest hours of a decision-hour histogram, most first. */
export function busiestHours(o: WorkProfileObservation, n = 3): number[] {
  return Object.entries(o.value)
    .map(([h, c]) => [Number(h), typeof c === "number" ? c : 0] as const)
    .filter(([h, c]) => Number.isInteger(h) && c > 0)
    .sort((a, b) => b[1] - a[1] || a[0] - b[0])
    .slice(0, n)
    .map(([h]) => h);
}

export function formatReviewLoad(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const m = Math.round(seconds / 60);
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h ${m % 60} min`;
}
