"use client";

import { AudioLines } from "lucide-react";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Issue } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Provenance line for an issue that came out of a recorded meeting: accepting
 * an extracted action item stamps `origin_type = "meeting"` on the issue, and
 * until now nothing in the product showed it — the recording that asked for the
 * work was one click away and unreachable.
 *
 * Deliberately quiet, and it renders nothing at all unless the server resolved
 * the origin: `origin_type` is absent on list/board/search rows, where absent
 * means "not loaded here", not "no origin".
 */
export function MeetingOriginLink({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  if (issue.origin_type !== "meeting" || !issue.origin_id) return null;
  return (
    <AppLink
      href={paths.meetingDetail(issue.origin_id)}
      className="mt-2 inline-flex items-center gap-1.5 text-caption text-muted-foreground transition-colors hover:text-foreground"
    >
      <AudioLines aria-hidden="true" className="size-3.5 shrink-0" />
      {t(($) => $.detail.from_meeting)}
    </AppLink>
  );
}
