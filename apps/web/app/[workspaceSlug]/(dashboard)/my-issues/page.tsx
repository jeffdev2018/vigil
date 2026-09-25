"use client";

import { Suspense } from "react";
import { MyIssuesPage } from "@multica/views/my-issues";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { useIssueViewUrlSync } from "../../../../platform/use-issue-view-url-sync";

function IssueViewUrlSync() {
  // useSearchParams requires a Suspense boundary in the app router.
  useIssueViewUrlSync({ scope_type: "my" });
  return null;
}

export default function Page() {
  return (
    <ErrorBoundary>
      <Suspense fallback={null}>
        <IssueViewUrlSync />
      </Suspense>
      <MyIssuesPage />
    </ErrorBoundary>
  );
}
