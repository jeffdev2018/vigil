"use client";

import { use } from "react";
import { ReviewCockpitRoute } from "@multica/views/issues/components";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

// Review cockpit (K16): the reviewer's single screen for an issue's run.
export default function IssueReviewPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <ReviewCockpitRoute routeId={id} />
    </ErrorBoundary>
  );
}
