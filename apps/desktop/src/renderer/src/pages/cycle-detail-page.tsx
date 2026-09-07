import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { CycleDetail } from "@multica/views/cycles/components";
import { useWorkspaceId } from "@multica/core/hooks";
import { cycleDetailOptions } from "@multica/core/cycles";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function CycleDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: cycle } = useQuery(cycleDetailOptions(wsId, id ?? ""));

  useDocumentTitle(cycle ? cycle.name : "Cycle");

  if (!id) return null;
  return <CycleDetail cycleId={id} />;
}
