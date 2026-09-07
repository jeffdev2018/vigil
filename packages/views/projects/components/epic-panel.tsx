"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, ChevronRight, Loader2, Lock, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectResourcesOptions } from "@multica/core/projects";
import {
  EPIC_STEP_KINDS,
  epicRailState,
  epicRemainingTickets,
  epicStep,
  nextEnabledStep,
  projectEpicOptions,
  stepLockedBy,
  useApplyEpicTickets,
  useApproveEpicStep,
  useGenerateEpicStep,
  useSaveEpicStep,
  type EpicArtifact,
  type EpicRailState,
} from "@multica/core/projects/epic";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { RichContent } from "../../rich-content";
import { useT } from "../../i18n";

/**
 * Epic Mode (F18): the project's PRD -> tech plan -> wireframe -> tickets rail.
 *
 * Every step renders the same five affordances so the pipeline reads as one
 * mechanism: its state, its content, Generate, Edit and Approve. A locked step
 * says WHICH approval it is waiting on rather than being merely greyed out —
 * "disabled" alone makes a user hunt for the reason.
 *
 * The last step is the only one that does anything: Apply turns the approved
 * breakdown into real child issues. It previews the tickets first, and a replay
 * previews only the ones an earlier apply has not created yet.
 */
export function EpicPanel({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState("");

  const { data: epic } = useQuery(projectEpicOptions(wsId, projectId));
  const { data: resources } = useQuery(projectResourcesOptions(wsId, projectId));
  const generate = useGenerateEpicStep(wsId, projectId);
  const save = useSaveEpicStep(wsId, projectId);
  const approve = useApproveEpicStep(wsId, projectId);
  const apply = useApplyEpicTickets(wsId, projectId);

  const next = nextEnabledStep(epic);
  const hasRepo = (resources ?? []).some((r) => r.resource_type === "github_repo");

  const fail = (e: unknown, fallback: string) =>
    toast.error(e instanceof Error && e.message ? e.message : fallback);

  return (
    <div data-testid="project-epic-panel">
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.epic.section)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2 space-y-3">
          <p className="px-2 text-caption text-muted-foreground">{t(($) => $.epic.description)}</p>
          {!hasRepo && (
            <p className="px-2 text-caption text-muted-foreground" data-testid="epic-no-repo-warning">
              {t(($) => $.epic.no_repo_warning)}
            </p>
          )}

          <ol className="space-y-2">
            {EPIC_STEP_KINDS.map((kind) => {
              const step = epicStep(epic, kind);
              const state = epicRailState(step);
              const lockedBy = stepLockedBy(epic, kind);
              const isEditing = editing === kind;
              const busy =
                (generate.isPending && generate.variables?.kind === kind) ||
                (save.isPending && save.variables?.kind === kind) ||
                (approve.isPending && approve.variables === kind);

              return (
                <li
                  key={kind}
                  data-testid={`epic-step-${kind}`}
                  data-state={state}
                  className="rounded-md border px-2 py-2"
                >
                  <div className="flex items-center gap-2">
                    <EpicStateIcon state={state} locked={!!lockedBy} />
                    <span className={`min-w-0 flex-1 truncate text-body ${next === kind ? "font-medium" : ""}`}>
                      {t(($) => $.epic.steps[kind])}
                    </span>
                    {step.latest && (
                      <Badge variant="secondary" className="font-mono">
                        {t(($) => $.epic.version_badge, { version: step.latest.version })}
                      </Badge>
                    )}
                    <Badge variant={state === "approved" ? "default" : "secondary"}>
                      {t(($) => $.epic.state[state])}
                    </Badge>
                  </div>

                  {lockedBy ? (
                    <p className="mt-1 px-1 text-caption text-muted-foreground" data-testid={`epic-locked-${kind}`}>
                      {t(($) => $.epic.locked_explanation, { step: t(($) => $.epic.steps[lockedBy]) })}
                    </p>
                  ) : (
                    <>
                      {isEditing ? (
                        <div className="mt-2 space-y-2">
                          <Textarea
                            aria-label={t(($) => $.epic.edit_label)}
                            className="min-h-40 text-caption font-mono"
                            value={draft}
                            onChange={(e) => setDraft(e.target.value)}
                          />
                          <div className="flex gap-2">
                            <Button
                              size="sm"
                              disabled={save.isPending || !draft.trim()}
                              onClick={() =>
                                save.mutate(
                                  { kind, content: draft, payload: step.latest?.payload },
                                  {
                                    onSuccess: (res) => {
                                      setEditing(null);
                                      toast.success(
                                        res.reopened_steps.length > 0
                                          ? t(($) => $.epic.saved_reopened, { count: res.reopened_steps.length })
                                          : t(($) => $.epic.saved),
                                      );
                                    },
                                    onError: (e) => fail(e, t(($) => $.epic.save_failed)),
                                  },
                                )
                              }
                            >
                              {t(($) => $.epic.save)}
                            </Button>
                            <Button size="sm" variant="ghost" onClick={() => setEditing(null)}>
                              {t(($) => $.epic.cancel)}
                            </Button>
                          </div>
                        </div>
                      ) : (
                        <>
                          {step.latest?.content ? (
                            <div
                              className="mt-2 max-h-72 overflow-y-auto overflow-x-auto rounded-md bg-muted/40 px-2 py-1"
                              data-testid={`epic-content-${kind}`}
                            >
                              <RichContent content={step.latest.content} density="compact" phase="settled" />
                            </div>
                          ) : (
                            <p className="mt-1 px-1 text-caption text-muted-foreground">
                              {state === "generating" ? t(($) => $.epic.generating_hint) : t(($) => $.epic.empty)}
                            </p>
                          )}

                          {kind === "tickets" && step.approved && (
                            <TicketPreview approved={step.approved} />
                          )}

                          <div className="mt-2 flex flex-wrap gap-2">
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={busy || state === "generating"}
                              onClick={() =>
                                generate.mutate(
                                  { kind },
                                  {
                                    onSuccess: () => toast.success(t(($) => $.epic.generation_started)),
                                    onError: (e) => fail(e, t(($) => $.epic.generate_failed)),
                                  },
                                )
                              }
                            >
                              <Sparkles className="size-3.5" aria-hidden="true" />
                              {step.latest ? t(($) => $.epic.regenerate) : t(($) => $.epic.generate)}
                            </Button>
                            <Button
                              size="sm"
                              variant="ghost"
                              disabled={busy}
                              onClick={() => {
                                setEditing(kind);
                                setDraft(step.latest?.content ?? "");
                              }}
                            >
                              {t(($) => $.epic.edit)}
                            </Button>
                            {state === "draft" && (
                              <Button
                                size="sm"
                                disabled={busy}
                                onClick={() =>
                                  approve.mutate(kind, {
                                    onSuccess: () => toast.success(t(($) => $.epic.approved)),
                                    onError: (e) => fail(e, t(($) => $.epic.approve_failed)),
                                  })
                                }
                              >
                                {t(($) => $.epic.approve)}
                              </Button>
                            )}
                            {kind === "tickets" && state === "approved" && (
                              <Button
                                size="sm"
                                disabled={apply.isPending}
                                onClick={() =>
                                  apply.mutate(undefined, {
                                    onSuccess: (res) =>
                                      toast.success(t(($) => $.epic.applied, { count: res.created.length })),
                                    onError: (e) => fail(e, t(($) => $.epic.apply_failed)),
                                  })
                                }
                              >
                                {t(($) => $.epic.apply)}
                              </Button>
                            )}
                          </div>
                        </>
                      )}
                    </>
                  )}
                </li>
              );
            })}
          </ol>
        </div>
      )}
    </div>
  );
}

function EpicStateIcon({ state, locked }: { state: EpicRailState; locked: boolean }) {
  if (locked) return <Lock className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />;
  if (state === "generating") {
    return <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" aria-hidden="true" />;
  }
  if (state === "approved") return <Check className="size-3.5 shrink-0 text-primary" aria-hidden="true" />;
  return <span className="size-3.5 shrink-0 rounded-full border" aria-hidden="true" />;
}

/**
 * What Apply would create, before it creates it. A replay lists only the
 * tickets an earlier apply has not turned into issues yet, so a second press
 * reads as "add the two new ones", not "do all fifteen again".
 */
function TicketPreview({ approved }: { approved: EpicArtifact }) {
  const { t } = useT("projects");
  const remaining = epicRemainingTickets(approved);
  if (remaining.length === 0) {
    return (
      <p className="mt-2 px-1 text-caption text-muted-foreground" data-testid="epic-tickets-all-applied">
        {t(($) => $.epic.all_applied)}
      </p>
    );
  }
  return (
    <div className="mt-2 overflow-x-auto" data-testid="epic-ticket-preview">
      <table className="w-full text-caption">
        <thead className="text-muted-foreground">
          <tr>
            <th className="px-1 py-1 text-left font-medium">{t(($) => $.epic.ticket_title)}</th>
            <th className="px-1 py-1 text-left font-medium">{t(($) => $.epic.ticket_description)}</th>
            <th className="px-1 py-1 text-left font-medium">{t(($) => $.epic.ticket_depends_on)}</th>
          </tr>
        </thead>
        <tbody>
          {remaining.map((ticket) => (
            <tr key={ticket.external_key} data-testid="epic-ticket-row" className="align-top">
              <td className="px-1 py-1">{ticket.title}</td>
              <td className="max-w-64 truncate px-1 py-1 text-muted-foreground">{ticket.description}</td>
              <td className="px-1 py-1 font-mono text-muted-foreground">{ticket.depends_on.join(", ") || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
