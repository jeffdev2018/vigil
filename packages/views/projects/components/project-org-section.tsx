"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { toast } from "sonner";
import { orgResolveOptions, orgTemplatesOptions, useCreateOrgStructure } from "@multica/core/org";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { OrgTemplate } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { AppLink } from "../../navigation";
import { OrgTemplateCards } from "../../org/components/org-page";
import { useT } from "../../i18n";

/**
 * Org structure in force for a project (K75): its own, or the workspace
 * default when it has none. "Choose a model" drafts one for the project.
 */
export function ProjectOrgSection({ projectId }: { projectId: string }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [open, setOpen] = useState(true);
  const [choosing, setChoosing] = useState(false);
  const { data: structure } = useQuery(orgResolveOptions(wsId, projectId));
  const { data: templates = [], isPending: templatesPending } = useQuery({ ...orgTemplatesOptions(wsId), enabled: choosing });
  const create = useCreateOrgStructure(wsId);
  const inherited = structure != null && structure.project_id === null;

  const pick = (tpl: OrgTemplate) =>
    create.mutate(
      { project_id: projectId, model: tpl.model, name: tpl.name, definition: tpl.definition },
      { onSuccess: () => setChoosing(false), onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.project_section.error)) },
    );

  return (
    <div data-testid="project-org-section">
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.project_section.header)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2 space-y-1.5">
          {structure ? (
            <div data-testid="project-org-structure" className="flex flex-wrap items-center gap-2 px-2 text-body">
              <span className="min-w-0 truncate">{structure.name}</span>
              <span className="text-caption text-muted-foreground">{t(($) => $.model[structure.model])}</span>
              <span className="text-caption text-muted-foreground">{t(($) => $.status[structure.status])}</span>
              {inherited && <span className="text-caption text-muted-foreground">{t(($) => $.project_section.inherited)}</span>}
            </div>
          ) : (
            <p className="px-2 text-caption text-muted-foreground">{t(($) => $.project_section.none)}</p>
          )}
          <div className="flex items-center gap-1">
            <AppLink href={paths.org()} className="rounded-md px-2 py-1 text-caption text-muted-foreground hover:bg-accent/70 hover:text-foreground">
              {t(($) => $.project_section.open)}
            </AppLink>
            {(structure === null || structure === undefined || inherited) && (
              <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-caption text-muted-foreground" onClick={() => setChoosing(true)}>
                {t(($) => $.project_section.choose)}
              </Button>
            )}
          </div>
        </div>
      )}
      {choosing && (
        <Dialog open onOpenChange={(o) => { if (!o) setChoosing(false); }}>
          <DialogContent className="sm:max-w-2xl">
            <DialogHeader>
              <DialogTitle>{t(($) => $.project_section.choose)}</DialogTitle>
              <DialogDescription>{t(($) => $.new.description)}</DialogDescription>
            </DialogHeader>
            {templatesPending ? (
              <p className="text-caption text-muted-foreground">{t(($) => $.project_section.loading)}</p>
            ) : (
              <div className="max-h-[60vh] overflow-y-auto">
                <OrgTemplateCards templates={templates} onPick={pick} disabled={create.isPending} />
              </div>
            )}
            <DialogFooter>
              <Button type="button" variant="outline" size="sm" onClick={() => setChoosing(false)}>{t(($) => $.new.cancel)}</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
