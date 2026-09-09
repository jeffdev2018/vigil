"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ORG_PURPOSE_MAX,
  buildOrgDefinition,
  orgDefaultAssignments,
  orgModelFromAnswers,
  orgRoutingWords,
  orgStructureName,
  orgTemplateRoot,
  pickOrgTemplate,
  type OrgDecider,
  type OrgShape,
  type OrgTeamShape,
} from "@multica/core/org/templates";
import { orgListOptions, orgTemplatesOptions, useCreateOrgStructure } from "@multica/core/org";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { OrgMember, OrgModel, OrgTemplate } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { validateOrgDefinition } from "@multica/core/org/validate";
import { OrgProblemList } from "./org-problem-list";
import { useOrgWizardDraftStore } from "@multica/core/org/draft-store";
import { projectListOptions } from "@multica/core/projects/queries";
import { OrgTemplateCards } from "./org-template-cards";

const SELECT_CLASS = "h-8 w-full rounded-md border bg-background px-2 text-body";
const STEPS = 4;

const errorMessage = (e: unknown, fallback: string): string => (e instanceof Error && e.message ? e.message : fallback);

/** One question with its exclusive answers. Native radios: the whole wizard is
 *  reachable with Tab and the arrow keys without a control of our own. */
function Choice<T extends string>({
  legend,
  name,
  options,
  value,
  onPick,
}: {
  legend: string;
  name: string;
  options: { value: T; label: string }[];
  value: T | null;
  onPick: (v: T) => void;
}) {
  return (
    <fieldset className="flex flex-col gap-1">
      <legend className="text-caption text-muted-foreground">{legend}</legend>
      {options.map((o) => (
        <label key={o.value} className="flex items-center gap-2 text-body">
          <input type="radio" name={name} value={o.value} checked={value === o.value} onChange={() => onPick(o.value)} />
          {o.label}
        </label>
      ))}
    </fieldset>
  );
}

export interface OrgWizardProps {
  onClose: () => void;
  onCreated: (id: string) => void;
}

/**
 * Create an organisation from an empty screen, in four steps: the sentence that
 * says what the team is for, the decision table that names one of the seven
 * models, who is in it, and a preview that opens the draft in the canvas.
 *
 * The sentence does the routing work. Its words become the primary unit's rule
 * keywords and that unit's role keywords, which is what the server's
 * `orgMatchUnit` reads — a unit carries no keyword field of its own.
 */
export function OrgWizard({ onClose, onCreated }: OrgWizardProps) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: structures = [] } = useQuery(orgListOptions(wsId));
  const { data: templates = [] } = useQuery(orgTemplatesOptions(wsId));
  const create = useCreateOrgStructure(wsId);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const draft = useOrgWizardDraftStore(s => s.draft);
  const setDraft = useOrgWizardDraftStore(s => s.setDraft);


  const { step, projectId, purpose, decider, teamShape, hasEnd, compete, chosenModel, placement } = draft;
  const setStep = (step: number) => setDraft({ step });
  const setProjectId = (projectId: string) => setDraft({ projectId });
  const setPurpose = (purpose: string) => setDraft({ purpose });
  const setDecider = (decider: OrgDecider) => setDraft({ decider });
  const setTeamShape = (teamShape: OrgTeamShape) => setDraft({ teamShape });
  const setHasEnd = (hasEnd: boolean) => setDraft({ hasEnd });
  const setCompete = (compete: boolean) => setDraft({ compete });
  const setChosenModel = (chosenModel: OrgModel | null) => setDraft({ chosenModel });
  const [catalogOpen, setCatalogOpen] = useState(false);

  const purposeText = purpose.trim();
  const routingWords = useMemo(() => orgRoutingWords(purposeText), [purposeText]);
  const template = useMemo(() => {
    const base = pickOrgTemplate(purposeText);
    const labels = t($ => $.wizard.unit_templates, { returnObjects: true });
    return { ...base, units: base.units.map(unit => ({ ...unit, ...labels[unit.id as keyof typeof labels] })) };
  }, [purposeText, t]);
  const shape: OrgShape = useMemo(
    () =>
      chosenModel !== null
        ? { model: chosenModel, base: chosenModel }
        : orgModelFromAnswers({ decider: decider ?? "one_person", teamShape: teamShape ?? undefined, hasEnd: hasEnd === true, compete: compete === true }),
    [chosenModel, decider, teamShape, hasEnd, compete],
  );

  // Everyone the workspace has, humans first, in a stable order.
  const actors: (OrgMember & { name: string })[] = useMemo(
    () => [
      ...members.map((m) => ({ type: "member" as const, id: m.user_id, name: m.name })),
      ...agents.map((a) => ({ type: "agent" as const, id: a.id, name: a.name })),
    ],
    [members, agents],
  );
  const ownerId = members.find((m) => m.role === "owner")?.user_id ?? members[0]?.user_id ?? "";

  // The suggested placement follows the template until the user moves someone.
  const suggested = useMemo(() => {
    const byUnit = orgDefaultAssignments(template, actors);
    const out: Record<string, string> = {};
    for (const [unitId, keys] of Object.entries(byUnit)) for (const key of keys) out[key] = unitId;
    return out;
  }, [template, actors]);
  const placedIn = (key: string): string => placement[key] === "" || template.units.some(u => u.id === placement[key]) ? placement[key]! : suggested[key] ?? orgTemplateRoot(template).id;

  const assignments = useMemo(() => {
    const root = orgTemplateRoot(template).id;
    const out: Record<string, string[]> = {};
    for (const u of template.units) out[u.id] = [];
    for (const actor of actors) {
      const key = `${actor.type}:${actor.id}`;
      const target = placement[key] === "" || template.units.some(u => u.id === placement[key]) ? placement[key]! : suggested[key] ?? root;
      out[target]?.push(key);
    }
    return out;
  }, [template, actors, placement, suggested]);

  const definition = useMemo(
    () => buildOrgDefinition({ template, shape, assignments, ownerId, routingWords }),
    [template, shape, assignments, ownerId, routingWords],
  );

  const problems = validateOrgDefinition(definition, { model: shape.model, agentTrust: Object.fromEntries(agents.map(a => [a.id, a.trust_mode ?? ""])), agentName: Object.fromEntries(agents.map(a => [a.id, a.name])) });

  // Creating must never overwrite an existing scope.
  const existing = structures.find((s) => s.status !== "dissolved" && s.project_id === (projectId || null));

  const stepValid =
    step === 1 ? purposeText !== "" && purposeText.length <= ORG_PURPOSE_MAX
      : step === 2 ? chosenModel !== null || (decider !== null && (decider !== "each_team" || teamShape !== null) && hasEnd !== null && compete !== null)
        : true;

  const submit = () => {
    if (existing || problems.length) return;
    const body = { owner_id: ownerId, project_id: projectId || null, model: shape.model, name: orgStructureName(purposeText), definition };
    const onError = (e: unknown) => toast.error(errorMessage(e, t(($) => $.wizard.review.error)));
    create.mutate(body, {
      onSuccess: (s: unknown) => {
        useOrgWizardDraftStore.getState().clearDraft();
        onClose();
        // The shared mutation helper erases the response type; the server answers with the created structure.
        if (s !== null && typeof s === "object" && "id" in s && typeof s.id === "string") onCreated(s.id);
      },
      onError,
    });
  };

  const pending = create.isPending;
  const overflow = purposeText.length - ORG_PURPOSE_MAX;

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !pending) onClose(); }}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.wizard.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.wizard.step, { n: step, total: STEPS })}</DialogDescription>
        </DialogHeader>

        <div className="flex max-h-[60vh] flex-col gap-3 overflow-y-auto">
          {step === 1 && (
            <>
              <label className="space-y-1 text-caption">{t($ => $.new.project)}<select className={SELECT_CLASS} value={projectId} onChange={e => setProjectId(e.target.value)}><option value="">{t($ => $.new.workspace_default)}</option>{projects.map(p => <option key={p.id} value={p.id}>{p.title}</option>)}</select></label>
              {existing && <p role="alert" className="text-caption text-warning">{t($ => $.coherence.scope_taken)}</p>}
              <h3 className="text-body font-medium">{t(($) => $.wizard.purpose.question)}</h3>
              <p className="text-caption text-muted-foreground">{t(($) => $.wizard.purpose.help)}</p>
              <label className="flex flex-col gap-1 text-caption text-muted-foreground">
                {t(($) => $.wizard.purpose.label)}
                <Textarea
                  value={purpose}
                  rows={3}
                  onChange={(e) => setPurpose(e.target.value)}
                  placeholder={t(($) => $.wizard.purpose.placeholder)}
                />
              </label>
              <p className="text-caption text-muted-foreground" role="status">
                {overflow > 0
                  ? t(($) => $.wizard.purpose.too_long, { count: overflow })
                  : t(($) => $.wizard.purpose.remaining, { count: ORG_PURPOSE_MAX - purposeText.length })}
              </p>
            </>
          )}

          {step === 2 && !catalogOpen && (
            <>
              <h3 className="text-body font-medium">{t(($) => $.wizard.flow.question)}</h3>
              <Choice
                legend={t(($) => $.wizard.flow.decider)}
                name="org-decider"
                value={decider}
                onPick={(v) => { setDecider(v); setChosenModel(null); }}
                options={[
                  { value: "one_person" as OrgDecider, label: t(($) => $.wizard.flow.decider_one_person) },
                  { value: "each_team" as OrgDecider, label: t(($) => $.wizard.flow.decider_each_team) },
                  { value: "topic_owner" as OrgDecider, label: t(($) => $.wizard.flow.decider_topic_owner) },
                ]}
              />
              {decider === "each_team" && (
                <Choice
                  legend={t(($) => $.wizard.flow.shape)}
                  name="org-shape"
                  value={teamShape}
                  onPick={(v) => { setTeamShape(v); setChosenModel(null); }}
                  options={[
                    { value: "project" as OrgTeamShape, label: t(($) => $.wizard.flow.shape_project) },
                    { value: "role" as OrgTeamShape, label: t(($) => $.wizard.flow.shape_role) },
                    { value: "both" as OrgTeamShape, label: t(($) => $.wizard.flow.shape_both) },
                  ]}
                />
              )}
              <Choice
                legend={t(($) => $.wizard.flow.end)}
                name="org-end"
                value={hasEnd === null ? null : hasEnd ? "yes" : "no"}
                onPick={(v) => { setHasEnd(v === "yes"); setChosenModel(null); }}
                options={[
                  { value: "yes", label: t(($) => $.wizard.flow.yes) },
                  { value: "no", label: t(($) => $.wizard.flow.no) },
                ]}
              />
              <Choice
                legend={t(($) => $.wizard.flow.compete)}
                name="org-compete"
                value={compete === null ? null : compete ? "yes" : "no"}
                onPick={(v) => { setCompete(v === "yes"); setChosenModel(null); }}
                options={[
                  { value: "yes", label: t(($) => $.wizard.flow.yes) },
                  { value: "no", label: t(($) => $.wizard.flow.no) },
                ]}
              />
              {stepValid && (
                <div data-testid="org-wizard-model" className="flex flex-col gap-0.5 rounded-md border p-3">
                  <span className="text-body font-medium">{t(($) => $.wizard.flow.result, { model: t(($) => $.model[shape.model]) })}</span>
                  <span className="text-caption text-muted-foreground">{t(($) => $.wizard.model_line[shape.model])}</span>
                </div>
              )}
              <Button type="button" variant="link" size="sm" className="self-start px-0" onClick={() => setCatalogOpen(true)}>
                {t(($) => $.wizard.flow.pick_another)}
              </Button>
            </>
          )}

          {step === 2 && catalogOpen && (
            <>
              <h3 className="text-body font-medium">{t(($) => $.wizard.flow.catalog)}</h3>
              <OrgTemplateCards
                templates={templates.filter((tpl: OrgTemplate) => tpl.composite !== true)}
                onPick={(tpl: OrgTemplate) => { setChosenModel(tpl.model); setCatalogOpen(false); }}
              />
              <Button type="button" variant="link" size="sm" className="self-start px-0" onClick={() => setCatalogOpen(false)}>
                {t(($) => $.wizard.flow.catalog_close)}
              </Button>
            </>
          )}

          {step === 3 && (
            <>
              <h3 className="text-body font-medium">{t(($) => $.wizard.people.question)}</h3>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.wizard.people.template, { template: t(($) => $.wizard.template[template.key]) })}
              </p>
              {actors.length === 0 ? (
                <p className="text-caption text-muted-foreground">{t(($) => $.wizard.people.empty)}</p>
              ) : (
                <ul className="flex flex-col gap-1">
                  {actors.map((actor) => {
                    const key = `${actor.type}:${actor.id}`;
                    return (
                      <li key={key} data-testid="org-wizard-actor" className="flex items-center gap-2 text-caption">
                        <span className="truncate">{actor.name}</span>
                        <select
                          className={`${SELECT_CLASS} ml-auto w-48`}
                          aria-label={t(($) => $.wizard.people.unit_of, { name: actor.name })}
                          value={placedIn(key)}
                          onChange={(e) => setDraft({ placement: { ...placement, [key]: e.target.value } })}
                        >
                          <option value="">{t($ => $.coherence.excluded)}</option>
                          {template.units.map((u) => (
                            <option key={u.id} value={u.id}>{u.name}</option>
                          ))}
                        </select>
                      </li>
                    );
                  })}
                </ul>
              )}
            </>
          )}

          {step === 4 && (
            <>
              <h3 className="text-body font-medium">{t(($) => $.wizard.review.question)}</h3>
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-caption">
                <dt className="text-muted-foreground">{t(($) => $.wizard.review.model)}</dt>
                <dd>{t(($) => $.model[shape.model])}</dd>
                <dt className="text-muted-foreground">{t(($) => $.wizard.review.name)}</dt>
                <dd>{orgStructureName(purposeText) || t(($) => $.wizard.review.unnamed)}</dd>
              </dl>
              <ul className="flex flex-col gap-1">
                {definition.units.map((u) => (
                  <li key={u.id} data-testid="org-wizard-unit" className="flex flex-col rounded-md border p-2">
                    <span className="text-body font-medium">{u.name}</span>
                    <span className="text-caption text-muted-foreground">{u.mission}</span>
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.wizard.review.members, { count: u.members.length })} · {t(($) => $.autonomy[u.autonomy])}
                    </span>
                  </li>
                ))}
              </ul>
              <OrgProblemList problems={problems} />
              <p className="text-caption text-muted-foreground">{t(($) => $.wizard.review.draft_note)}</p>
              {existing !== undefined && (
                <p className="text-caption text-muted-foreground" role="note">
                  {t($ => $.coherence.scope_taken)}
                </p>
              )}
            </>
          )}
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" size="sm" disabled={pending} onClick={step === 1 ? onClose : () => setStep(step - 1)}>
            {step === 1 ? t(($) => $.wizard.cancel) : t(($) => $.wizard.back)}
          </Button>
          {step < STEPS ? (
            <Button
              type="button"
              size="sm"
              disabled={!stepValid || !!existing}
              onClick={() => setStep(step + 1)}
            >
              {t(($) => $.wizard.next)}
            </Button>
          ) : (
            <Button type="button" size="sm" disabled={pending || !!existing || problems.length > 0} onClick={submit}>
              {t(($) => $.wizard.review.open)}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
