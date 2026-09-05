"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { targetContestsOptions, type ContestTargetType } from "@multica/core/issues/contest";
import { ContestList } from "./contests-section";

/** Contests on one output that has no issue (a meeting summary, a triage verdict). */
export function TargetContests({ targetType, targetId }: { targetType: ContestTargetType; targetId: string }) {
  const wsId = useWorkspaceId();
  const { data: contests = [] } = useQuery(targetContestsOptions(wsId, targetType, targetId));
  return <ContestList contests={contests} />;
}
