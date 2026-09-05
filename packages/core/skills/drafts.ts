import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";

// Skill Miner (K58): drafts mined from recurring human corrections (and
// distilled from successful runs), waiting for a human to publish or dismiss.

export interface SkillDraftSource {
  issue_id: string;
  issue_number: number;
  issue_title: string;
  comment_id: string;
  status_regressed: boolean;
}

export interface SkillDraft {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  config: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  status: string;
  sources: SkillDraftSource[];
}

export const skillDraftKeys = {
  list: (wsId: string) => ["skill-drafts", wsId] as const,
};

export function skillDraftListOptions(wsId: string) {
  return queryOptions({ queryKey: skillDraftKeys.list(wsId), queryFn: () => api.listSkillDrafts() });
}

/** Publish (status → published) or dismiss (delete) a draft; both refresh the library. */
export function useReviewSkillDraft(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: { id: string; action: "publish" | "dismiss" }) => {
      if (v.action === "publish") await api.updateSkill(v.id, { status: "published" });
      else await api.deleteSkill(v.id);
      return v;
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: skillDraftKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
    },
  });
}

/** Where a draft came from, from its config.origin. */
export function draftOrigin(d: SkillDraft): { type: string; agent_name: string; signals: number; regressed: number; llm: boolean } {
  const o = (d.config["origin"] ?? {}) as Record<string, unknown>;
  return {
    type: typeof o["type"] === "string" ? (o["type"] as string) : "manual",
    agent_name: typeof o["agent_name"] === "string" ? (o["agent_name"] as string) : "",
    signals: typeof o["signals"] === "number" ? (o["signals"] as number) : d.sources.length,
    regressed: typeof o["status_regressed"] === "number" ? (o["status_regressed"] as number) : 0,
    llm: o["llm"] === true,
  };
}
