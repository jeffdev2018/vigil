"use client";

import { useQuery } from "@tanstack/react-query";
import { Sparkles } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { draftOrigin, skillDraftListOptions, useReviewSkillDraft, type SkillDraft } from "@multica/core/skills/drafts";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Skill Miner proposals (K58): drafts mined from recurring human corrections
 * or distilled from a successful run, each with its sources, waiting for a
 * human to publish (and edit) or dismiss. Hidden when there is nothing to
 * review. A draft is never assignable until published.
 */
export function SkillDraftsSection({ canManage = true }: { canManage?: boolean }) {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { data: drafts = [] } = useQuery(skillDraftListOptions(wsId));
  const review = useReviewSkillDraft(wsId);
  if (drafts.length === 0) return null;
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.miner.failed));
  return (
    <section data-testid="skill-drafts" className="flex flex-col gap-2 border-b px-4 py-3 text-caption">
      <div className="flex items-center gap-2 font-medium">
        <Sparkles className="size-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.miner.section, { n: drafts.length })}</span>
      </div>
      <p className="text-muted-foreground">{t(($) => $.miner.intro)}</p>
      <ul className="flex flex-col gap-2">
        {drafts.map((d) => (
          <DraftCard key={d.id} draft={d} canManage={canManage} onPublish={() => review.mutate({ id: d.id, action: "publish" }, { onSuccess: () => { toast.success(t(($) => $.miner.published)); navigation.push(paths.skillDetail(d.id)); }, onError: fail })} onDismiss={() => review.mutate({ id: d.id, action: "dismiss" }, { onSuccess: () => toast.success(t(($) => $.miner.dismissed)), onError: fail })} pending={review.isPending} />
        ))}
      </ul>
    </section>
  );
}

function DraftCard({ draft: d, canManage, onPublish, onDismiss, pending }: { draft: SkillDraft; canManage: boolean; onPublish: () => void; onDismiss: () => void; pending: boolean }) {
  const { t } = useT("skills");
  const paths = useWorkspacePaths();
  const origin = draftOrigin(d);
  return (
    <li data-testid="skill-draft" data-origin={origin.type} className="flex flex-col gap-1 rounded-md border p-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{d.name}</span>
        <span className="rounded bg-muted px-1 text-muted-foreground">
          {origin.type === "distilled" ? t(($) => $.miner.origin_distilled) : t(($) => $.miner.origin_mined, { agent: origin.agent_name || "?", n: origin.signals, regressed: origin.regressed })}
        </span>
      </div>
      {d.description && <p className="text-muted-foreground">{d.description}</p>}
      {d.sources.length > 0 && (
        <p className="flex flex-wrap gap-x-2 text-muted-foreground">
          <span>{t(($) => $.miner.sources)}</span>
          {d.sources.map((s) => (
            <AppLink key={s.comment_id || s.issue_id} href={paths.issueDetail(s.issue_id)} className="hover:underline" title={s.issue_title}>
              #{s.issue_number}{s.status_regressed ? " ↩" : ""}
            </AppLink>
          ))}
        </p>
      )}
      {canManage && (
        <div className="flex gap-2">
          <Button type="button" size="sm" disabled={pending} onClick={onPublish}>{t(($) => $.miner.publish)}</Button>
          <Button type="button" size="sm" variant="ghost" disabled={pending} onClick={onDismiss}>{t(($) => $.miner.dismiss)}</Button>
        </div>
      )}
    </li>
  );
}
