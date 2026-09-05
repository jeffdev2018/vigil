"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Radius, X } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { blastRadiusPreviewOptions, blastRadiusRulesOptions, useCreateBlastRadiusRule, useDeleteBlastRadiusRule } from "@multica/core/projects/blast-radius";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

/**
 * Blast radius (K07): per-project autonomy by path pattern, listed most
 * specific first (the order the server resolves in, not editable), with a
 * preview box that names the rule a path would fall under. No rule means
 * the agent's own permissions decide.
 */
export function ProjectBlastRadiusSection({ projectId, canEdit = true }: { projectId: string; canEdit?: boolean }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data } = useQuery(blastRadiusRulesOptions(wsId, projectId));
  const rules = data?.rules ?? [];
  const levels = data?.levels?.length ? data.levels : ["autonomous", "read_only", "dual_approval"];
  const create = useCreateBlastRadiusRule(wsId, projectId);
  const remove = useDeleteBlastRadiusRule(wsId, projectId);
  const [pattern, setPattern] = useState("");
  const [level, setLevel] = useState("dual_approval");
  const [probe, setProbe] = useState("");
  const { data: preview } = useQuery(blastRadiusPreviewOptions(wsId, projectId, probe));

  const levelLabel = (l: string) => {
    switch (l) {
      case "autonomous":
        return t(($) => $.blast_radius.level_autonomous);
      case "read_only":
        return t(($) => $.blast_radius.level_read_only);
      case "dual_approval":
        return t(($) => $.blast_radius.level_dual_approval);
      case "inherit":
        return t(($) => $.blast_radius.level_inherit);
      default:
        return l;
    }
  };
  const submit = () => {
    if (pattern.trim() === "") return;
    create.mutate(
      { path_pattern: pattern.trim(), autonomy_level: level },
      {
        onSuccess: () => setPattern(""),
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.blast_radius.failed)),
      },
    );
  };

  return (
    <div data-testid="project-blast-radius" className="mt-6 text-caption">
      <div className="mb-2 flex items-center gap-2 px-2">
        <Radius className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{t(($) => $.blast_radius.section)}</span>
      </div>
      <p className="mb-2 px-2 text-muted-foreground">{t(($) => $.blast_radius.description)}</p>
      {rules.length === 0 ? (
        <p data-testid="blast-radius-empty" className="px-2 text-muted-foreground">{t(($) => $.blast_radius.empty)}</p>
      ) : (
        <ul className="flex flex-col gap-1 px-2">
          {rules.map((r) => (
            <li key={r.id} data-testid="blast-radius-rule" data-level={r.autonomy_level} className="flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate font-mono">{r.path_pattern}</span>
              <span className="shrink-0 text-muted-foreground">{levelLabel(r.autonomy_level)}</span>
              {canEdit && (
                <button type="button" aria-label={t(($) => $.blast_radius.remove)} className="text-faint-foreground hover:text-foreground" onClick={() => remove.mutate(r.id, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.blast_radius.failed)) })}>
                  <X className="size-3.5" />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {canEdit && (
        <div className="mt-2 flex flex-wrap items-center gap-2 px-2">
          <Input aria-label={t(($) => $.blast_radius.pattern)} placeholder={t(($) => $.blast_radius.pattern_placeholder)} className="h-8 w-56 font-mono" value={pattern} onChange={(e) => setPattern(e.target.value)} />
          <select aria-label={t(($) => $.blast_radius.level)} className="h-8 rounded-md border bg-background px-2" value={level} onChange={(e) => setLevel(e.target.value)}>
            {levels.map((l) => (
              <option key={l} value={l}>{levelLabel(l)}</option>
            ))}
          </select>
          <Button type="button" size="sm" disabled={create.isPending || pattern.trim() === ""} onClick={submit}>{t(($) => $.blast_radius.add)}</Button>
        </div>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-2 px-2">
        <Input aria-label={t(($) => $.blast_radius.preview)} placeholder={t(($) => $.blast_radius.preview_placeholder)} className="h-8 w-64 font-mono" value={probe} onChange={(e) => setProbe(e.target.value)} />
        {probe.trim() !== "" && preview && (
          <span data-testid="blast-radius-preview" className="text-muted-foreground">
            {levelLabel(preview.level)}
            {preview.path_pattern ? ` · ${preview.path_pattern}` : ""}
          </span>
        )}
      </div>
    </div>
  );
}
