"use client";

import { Suspense, use } from "react";
import { useSearchParams } from "next/navigation";
import { AttachmentPreviewPage } from "@multica/views/attachments";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

// Lives at /:slug/attachments/:id/preview — OUTSIDE the (dashboard) group on
// purpose. The dashboard layout adds a left sidebar + top chrome; this page
// wants the full viewport for the HTML iframe. Workspace resolution still
// happens in the parent [workspaceSlug] layout so useWorkspaceId() works.
export default function AttachmentPreviewWebPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);

  // useSearchParams requires a Suspense boundary in the app router.
  return (
    <ErrorBoundary resetKeys={[id]}>
      <Suspense fallback={null}>
        <AttachmentPreviewContent attachmentId={id} />
      </Suspense>
    </ErrorBoundary>
  );
}

function AttachmentPreviewContent({ attachmentId }: { attachmentId: string }) {
  const search = useSearchParams();
  const filename = search.get("name") ?? undefined;
  return <AttachmentPreviewPage attachmentId={attachmentId} filename={filename} />;
}
