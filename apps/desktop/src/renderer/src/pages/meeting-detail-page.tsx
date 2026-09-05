import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { MeetingDetailPage as MeetingDetail } from "@multica/views/meetings";
import { useWorkspaceId } from "@multica/core/hooks";
import { meetingDetailOptions } from "@multica/core/meetings/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function MeetingDetailPage() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data } = useQuery(meetingDetailOptions(wsId, id ?? ""));

  useDocumentTitle(data?.title || "Meeting");

  if (!id) return null;
  return <MeetingDetail meetingId={id} />;
}
