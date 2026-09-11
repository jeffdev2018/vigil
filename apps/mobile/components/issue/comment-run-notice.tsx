/**
 * "Walt will start a run when you send · runs on Studio · ≈ $0.42 per run"
 * above a composer, before the user sends.
 *
 * Parity: in a comment the agent list is the server's trigger preview, the
 * same answer web's CommentTriggerChips render
 * (packages/views/issues/components/comment-trigger-chips.tsx); the
 * runtime/cost line mirrors web's AgentRunDetails. Mobile has no per-send
 * "skip this agent" toggle yet, so the notice only informs — a user who does
 * not want the run removes the mention before sending.
 */
import { useEffect, useRef, type ReactNode } from "react";
import { View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { api } from "@/data/api";
import { agentListOptions } from "@/data/queries/agents";
import { runtimeListOptions } from "@/data/queries/runtimes";
import { useWorkspaceStore } from "@/data/workspace-store";
import { agentRunNoticeText, commentTriggerSignature } from "@/lib/comment-run-notice";
import { runtimeDisplayName } from "@/lib/runtime-display";

/** One agent's line: name, runtime and recent average cost per run. */
export function AgentRunNoticeLine({ agentId, name }: { agentId: string; name: string }) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const estimate = useQuery({
    queryKey: ["agent-cost-estimate", wsId, agentId] as const,
    queryFn: ({ signal }) => api.getAgentCostEstimate(agentId, { signal }),
    enabled: !!wsId && !!agentId,
    staleTime: 60_000,
  });
  const runtimeId = agents.find((a) => a.id === agentId)?.runtime_id;
  const runtime = runtimeId ? runtimes.find((r) => r.id === runtimeId) : undefined;
  return (
    <Text className="text-xs text-foreground">
      {agentRunNoticeText({
        name,
        runtimeName: runtime ? runtimeDisplayName(runtime) : null,
        avgCostUsdTicks: estimate.data?.avg_cost_usd_ticks ?? null,
        sampleRuns: estimate.data?.sample_runs ?? 0,
        costPending: estimate.isPending && !!wsId,
      })}
    </Text>
  );
}

export function RunNoticeBox({ children }: { children: ReactNode }) {
  return (
    <View className="rounded-md bg-secondary/60 px-3 py-2 gap-1" accessibilityRole="alert">
      {children}
    </View>
  );
}

export function CommentRunNotice({ issueId, content }: { issueId: string; content: string }) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const signature = commentTriggerSignature(content);
  // The query key follows the signature only; the latest text rides a ref so
  // typing inside the same mention set never refetches (cellular-data rule).
  const contentRef = useRef(content);
  useEffect(() => {
    contentRef.current = content;
  }, [content]);
  const preview = useQuery({
    queryKey: ["comment-trigger-preview", wsId, issueId, signature] as const,
    queryFn: ({ signal }) => api.previewCommentTriggers(issueId, contentRef.current, { signal }),
    enabled: !!wsId && signature !== "empty",
    staleTime: 0,
    retry: false,
  });
  const agents = signature === "empty" ? [] : preview.data?.agents ?? [];
  if (agents.length === 0) return null;
  return (
    <RunNoticeBox>
      {agents.map((agent) => <AgentRunNoticeLine key={agent.id} agentId={agent.id} name={agent.name} />)}
    </RunNoticeBox>
  );
}
