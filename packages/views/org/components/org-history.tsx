"use client";

import { useState } from "react";
import { toast } from "sonner";
import { orgDefinitionChanges, useUpdateOrgStructure } from "@multica/core/org";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import type { OrgRevision, OrgStatus, OrgStructure } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { orgAutonomyLabel, orgModelLabel } from "../labels";
import { OrgStatusPill } from "./org-bar";
import { orgErrorMessage } from "./org-dialogs";

/**
 * Every published revision, newest first, with who made it and why. Restoring
 * never rewrites history: it publishes the old definition as a new revision,
 * so the one being replaced stays in this list.
 */
export function OrgHistorySheet({
  structure,
  revisions,
  dirty,
  readOnly,
  onClose,
}: {
  structure: OrgStructure;
  revisions: OrgRevision[];
  /** Unpublished edits would be lost by a restore, so it waits for them. */
  dirty: boolean;
  readOnly: boolean;
  onClose: () => void;
}) {
  const { t, i18n } = useT("org");
  const { getActorName } = useActorName();
  const [review, setReview] = useState<OrgRevision | null>(null);
  const when = new Intl.DateTimeFormat(i18n.language, { dateStyle: "medium", timeStyle: "short" });

  return (
    <>
      <Sheet open onOpenChange={(open) => !open && onClose()}>
        <SheetContent side="right" className="w-full gap-0 sm:max-w-md">
          <SheetHeader className="border-b">
            <SheetTitle>{t(($) => $.bar.history)}</SheetTitle>
            <SheetDescription>{t(($) => $.history.sheet_hint)}</SheetDescription>
          </SheetHeader>
          {revisions.length === 0 ? (
            <p className="p-4 text-caption text-muted-foreground">{t(($) => $.page.no_revisions)}</p>
          ) : (
            <ol className="flex-1 divide-y overflow-y-auto">
              {revisions.map((r) => {
                const current = r.revision === structure.revision;
                return (
                  <li key={r.id} className="flex flex-col gap-1.5 px-4 py-3">
                    <div className="flex items-center gap-2">
                      <span className="text-label font-medium tabular-nums">
                        {t(($) => $.history.revision_label, { n: r.revision })}
                      </span>
                      <OrgStatusPill status={r.status as OrgStatus} />
                      {current && (
                        <span className="text-caption text-muted-foreground">{t(($) => $.history.in_force)}</span>
                      )}
                      {r.definition && !current && (
                        <Button size="xs" variant="ghost" className="ml-auto" onClick={() => setReview(r)}>
                          {t(($) => $.history.compare)}
                        </Button>
                      )}
                    </div>
                    <div className="flex items-center gap-2 text-caption text-muted-foreground">
                      {r.changed_by ? (
                        <>
                          <ActorAvatar actorType="member" actorId={r.changed_by} size="xs" profileLink={false} />
                          <span className="truncate">{getActorName("member", r.changed_by)}</span>
                        </>
                      ) : (
                        <span>{t(($) => $.history.by_system)}</span>
                      )}
                      <span aria-hidden="true">·</span>
                      <time dateTime={r.created_at} className="whitespace-nowrap">
                        {when.format(new Date(r.created_at))}
                      </time>
                      <span aria-hidden="true">·</span>
                      <span className="truncate">{orgModelLabel(t, r.model)}</span>
                    </div>
                    {r.note.trim() && <p className="text-caption text-foreground">{r.note}</p>}
                  </li>
                );
              })}
            </ol>
          )}
        </SheetContent>
      </Sheet>
      {review && (
        <OrgRevisionReview
          structure={structure}
          revision={review}
          dirty={dirty}
          readOnly={readOnly}
          onClose={() => setReview(null)}
        />
      )}
    </>
  );
}

function OrgRevisionReview({
  structure,
  revision,
  dirty,
  readOnly,
  onClose,
}: {
  structure: OrgStructure;
  revision: OrgRevision;
  dirty: boolean;
  readOnly: boolean;
  onClose: () => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const update = useUpdateOrgStructure(wsId);
  const past = revision.definition;
  const restore = () =>
    update.mutate(
      { id: structure.id, data: { restore_revision_id: revision.id, expected_revision: structure.revision } },
      {
        onSuccess: () => {
          onClose();
          toast.success(t(($) => $.history.restored));
        },
        onError: (e) => toast.error(orgErrorMessage(e, t(($) => $.form.error))),
      },
    );

  return (
    <Dialog open onOpenChange={(open) => !open && !update.isPending && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.history.title, { n: revision.revision })}</DialogTitle>
          <DialogDescription>{t(($) => $.history.description)}</DialogDescription>
        </DialogHeader>
        <div className="max-h-[60vh] space-y-3 overflow-auto">
          {past &&
            orgDefinitionChanges(structure.definition, past).map((change) => (
              <div key={change.id} className="rounded-lg border p-3">
                <p className="text-body font-medium">
                  {change.after?.name ?? change.before?.name ?? t(($) => $.coherence.sections[change.section])}
                </p>
                <div className="mt-2 grid gap-2 text-caption sm:grid-cols-2">
                  {[change.before, change.after].map((unit, i) => (
                    <div key={i} className="rounded-md bg-muted/50 p-3">
                      <span className="text-muted-foreground">
                        {i === 0 ? t(($) => $.history.current) : t(($) => $.history.previous)}
                      </span>
                      <p className="mt-1">
                        {unit
                          ? `${unit.name} · ${orgAutonomyLabel(t, unit.autonomy)} · ${t(($) => $.page.members, { count: unit.members.length })}`
                          : t(($) => $.history.absent)}
                      </p>
                      {unit && unit.roles.length > 0 && <p>{unit.roles.map((r) => r.name).join(", ")}</p>}
                    </div>
                  ))}
                </div>
              </div>
            ))}
          {past && (
            <Collapsible>
              <CollapsibleTrigger className="text-caption font-medium text-muted-foreground hover:text-foreground">
                {t(($) => $.history.full_diff)}
              </CollapsibleTrigger>
              <CollapsibleContent className="mt-3 grid gap-3 sm:grid-cols-2">
                {[structure.definition, past].map((def, i) => (
                  <div key={i}>
                    <h4 className="mb-2 text-caption font-semibold">
                      {i === 0 ? t(($) => $.history.current) : t(($) => $.history.previous)}
                    </h4>
                    <pre className="overflow-auto rounded-md bg-muted p-3 text-caption">{JSON.stringify(def, null, 2)}</pre>
                  </div>
                ))}
              </CollapsibleContent>
            </Collapsible>
          )}
        </div>
        {dirty && <p className="text-caption text-warning-strong">{t(($) => $.history.dirty)}</p>}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.actions.cancel)}
          </Button>
          <Button disabled={readOnly || dirty || update.isPending} onClick={restore}>
            {t(($) => $.history.restore)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
