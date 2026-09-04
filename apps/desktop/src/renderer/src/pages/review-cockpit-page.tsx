import { useParams } from "react-router-dom";
import { ReviewCockpitRoute } from "@multica/views/issues/components";

// Review cockpit (K16): the reviewer's single screen for an issue's run.
export function ReviewCockpitPage() {
  const { id } = useParams<{ id: string }>();
  if (!id) return null;
  return <ReviewCockpitRoute routeId={id} />;
}
