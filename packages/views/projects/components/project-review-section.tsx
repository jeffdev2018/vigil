"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ClipboardCheck, X } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectReviewConfigOptions, useSaveProjectReviewConfig } from "@multica/core/projects/review-config";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

/**
 * Agent review (JEF-238): the checklist a reviewer agent checks each
 * delivered diff against, an optional fixed reviewer (automatic = any agent
 * other than the worker), the done-gate, and the rework-cycle cap. Draft
 * edits are local until Save; the server validates (1..10 cycles, ≤20
 * non-empty items) and a rejection surfaces as a toast.
 */
export function ProjectReviewSection({ projectId, canEdit = true }: { projectId: string; canEdit?: boolean }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: config, isLoading, isError } = useQuery(projectReviewConfigOptions(wsId, projectId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const save = useSaveProjectReviewConfig(wsId, projectId);
  const [checklist, setChecklist] = useState<string[]>([]);
  const [reviewer, setReviewer] = useState("");
  const [gate, setGate] = useState(false);
  const [maxCycles, setMaxCycles] = useState(3);
  const [newItem, setNewItem] = useState("");
  useEffect(() => {
    if (!config) return;
    setChecklist(config.checklist);
    setReviewer(config.reviewer_agent_id ?? "");
    setGate(config.gate_enabled);
    setMaxCycles(config.max_cycles);
  }, [config]);

  const disabled = !canEdit || save.isPending;
  const reviewerAgents = agents.filter((a) => !a.archived_at);

  const submit = () => {
    save.mutate(
      {
        checklist: checklist.map((item) => item.trim()).filter((item) => item !== ""),
        reviewer_agent_id: reviewer === "" ? null : reviewer,
        gate_enabled: gate,
        max_cycles: maxCycles,
      },
      { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.review.failed)) },
    );
  };
  const addItem = () => {
    const item = newItem.trim();
    if (item === "") return;
    setChecklist((items) => [...items, item]);
    setNewItem("");
  };

  return (
    <div data-testid="project-review" className="mt-6 text-caption">
      <div className="mb-2 flex items-center gap-2 px-2">
        <ClipboardCheck className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{t(($) => $.review.section)}</span>
      </div>
      <p className="mb-2 px-2 text-muted-foreground">{t(($) => $.review.description)}</p>
      {isLoading && <p className="px-2 text-muted-foreground animate-pulse">…</p>}
      {isError && <p className="px-2 text-destructive">{t(($) => $.review.failed)}</p>}
      {config && (
        <>
          <div className="px-2 font-medium">{t(($) => $.review.checklist)}</div>
          {checklist.length === 0 ? (
            <p data-testid="review-checklist-empty" className="px-2 text-muted-foreground">{t(($) => $.review.checklist_empty)}</p>
          ) : (
            <ul className="mt-1 flex flex-col gap-1 px-2">
              {checklist.map((item, i) => (
                <li key={i} data-testid="review-checklist-item" className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate">{item}</span>
                  {canEdit && (
                    <button type="button" aria-label={t(($) => $.review.remove)} className="text-faint-foreground hover:text-foreground" disabled={disabled} onClick={() => setChecklist((items) => items.filter((_, j) => j !== i))}>
                      <X className="size-3.5" />
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
          {canEdit && (
            <div className="mt-2 flex flex-wrap items-center gap-2 px-2">
              <Input
                aria-label={t(($) => $.review.checklist)}
                placeholder={t(($) => $.review.item_placeholder)}
                className="h-8 w-56"
                value={newItem}
                onChange={(e) => setNewItem(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") addItem(); }}
              />
              <Button type="button" size="sm" variant="outline" disabled={disabled || newItem.trim() === ""} onClick={addItem}>{t(($) => $.review.add)}</Button>
            </div>
          )}
          <div className="mt-3 flex flex-wrap items-center gap-2 px-2">
            <label className="flex items-center gap-2">
              <span className="text-muted-foreground">{t(($) => $.review.reviewer)}</span>
              <select aria-label={t(($) => $.review.reviewer)} className="h-8 rounded-md border bg-background px-2" value={reviewer} disabled={disabled} onChange={(e) => setReviewer(e.target.value)}>
                <option value="">{t(($) => $.review.reviewer_auto)}</option>
                {reviewerAgents.map((a) => (
                  <option key={a.id} value={a.id}>{a.name}</option>
                ))}
              </select>
            </label>
            <label className="flex items-center gap-2">
              <span className="text-muted-foreground">{t(($) => $.review.max_cycles)}</span>
              <Input aria-label={t(($) => $.review.max_cycles)} type="number" min={1} max={10} className="h-8 w-16" value={maxCycles} disabled={disabled} onChange={(e) => setMaxCycles(Math.max(1, Math.min(10, Number(e.target.value) || 1)))} />
            </label>
          </div>
          <label className="mt-2 flex items-center gap-2 px-2">
            <input type="checkbox" aria-label={t(($) => $.review.gate)} checked={gate} disabled={disabled} onChange={(e) => setGate(e.target.checked)} />
            <span>{t(($) => $.review.gate)}</span>
          </label>
          <p className="mt-1 px-2 text-muted-foreground">{t(($) => $.review.gate_description)}</p>
          {canEdit && (
            <div className="mt-2 px-2">
              <Button type="button" size="sm" disabled={disabled} onClick={submit}>{t(($) => $.review.save)}</Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
