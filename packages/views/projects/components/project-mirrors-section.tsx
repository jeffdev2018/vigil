"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects";
import { mirrorLinksOptions, useCreateMirrorLink, useDeleteMirrorLink } from "@multica/core/mirrors";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

/**
 * Cross-repo mirrors (K54): the links configured on this project.
 *
 * A link fires when an issue of THIS project carries the trigger label; the
 * mirror is created in the target project and blocks the source until it is
 * closed. Removing a link stops future mirrors and keeps the existing ones —
 * they are real issues, so the copy says so.
 */
export function ProjectMirrorsSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const [target, setTarget] = useState("");
  const [label, setLabel] = useState("");

  const { data } = useQuery(mirrorLinksOptions(wsId, projectId));
  const { data: projects } = useQuery(projectListOptions(wsId));
  const createLink = useCreateMirrorLink(wsId, projectId);
  const deleteLink = useDeleteMirrorLink(wsId, projectId);

  const links = data?.links ?? [];
  const targets = (projects ?? []).filter((p) => p.id !== projectId);

  const fail = (e: unknown, fallback: string) =>
    toast.error(e instanceof Error && e.message ? e.message : fallback);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!target || !label.trim()) return;
    createLink.mutate(
      { targetProjectId: target, triggerLabel: label.trim() },
      {
        onSuccess: () => {
          setTarget("");
          setLabel("");
          toast.success(t(($) => $.mirrors.added));
        },
        onError: (e) => fail(e, t(($) => $.mirrors.add_failed)),
      },
    );
  };

  return (
    <div data-testid="project-mirrors-section">
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.mirrors.section)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2 space-y-3">
          <p className="px-2 text-caption text-muted-foreground">{t(($) => $.mirrors.description)}</p>

          {links.length === 0 ? (
            <p className="px-2 text-caption text-muted-foreground">{t(($) => $.mirrors.empty)}</p>
          ) : (
            <ul className="space-y-1">
              {links.map((link) => (
                <li
                  key={link.id}
                  data-testid="project-mirror-link"
                  className="flex items-center gap-2 rounded-md px-2 py-1 hover:bg-accent/70"
                >
                  <span className="min-w-0 flex-1 truncate text-body">
                    {link.target_project_title || t(($) => $.mirrors.unknown_project)}
                  </span>
                  <Badge variant="secondary" className="font-mono">{link.trigger_label}</Badge>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label={t(($) => $.mirrors.remove)}
                    disabled={deleteLink.isPending}
                    onClick={() =>
                      deleteLink.mutate(link.id, {
                        onSuccess: () => toast.success(t(($) => $.mirrors.removed)),
                        onError: (e) => fail(e, t(($) => $.mirrors.remove_failed)),
                      })
                    }
                  >
                    <Trash2 className="size-3.5" aria-hidden="true" />
                  </Button>
                </li>
              ))}
            </ul>
          )}

          <form className="flex flex-wrap items-center gap-2 px-2" onSubmit={submit}>
            <Select
              items={[
                { value: "", label: t(($) => $.mirrors.target_placeholder) },
                ...targets.map((p) => ({ value: p.id, label: p.title })),
              ]}
              value={target}
              onValueChange={(value) => value !== null && setTarget(value)}
            >
              <SelectTrigger size="sm" className="min-w-40" aria-label={t(($) => $.mirrors.target)}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">{t(($) => $.mirrors.target_placeholder)}</SelectItem>
                {targets.map((p) => (
                  <SelectItem key={p.id} value={p.id}>{p.title}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              aria-label={t(($) => $.mirrors.trigger_label)}
              placeholder={t(($) => $.mirrors.trigger_placeholder)}
              className="h-8 w-40 text-caption"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
            />
            <Button type="submit" size="sm" variant="outline" disabled={createLink.isPending || !target || !label.trim()}>
              {t(($) => $.mirrors.add)}
            </Button>
          </form>
        </div>
      )}
    </div>
  );
}
