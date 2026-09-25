"use client";

import { useEffect, useState } from "react";
import { Play } from "lucide-react";
import { ApiError } from "@multica/core/api";
import { useSimulateOrg } from "@multica/core/org";
import { orgEscalationLabel, orgFormFromIssue, orgRequestFromText } from "@multica/core/org/tester";
import { contestCostUsd } from "@multica/core/issues/contest";
import type { Goal, Issue, OrgDefinition, OrgModel, OrgSimulationActor, OrgStatus } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { useT } from "../../i18n";
import { orgAutonomyLabel, orgCapabilityLabel, orgSimulateNoteText } from "../labels";
import type { OrgPlanTrace } from "./org-plan";

export interface OrgTesterProps {
  structureId: string;
  /** The definition the canvas shows right now — draft edits included. */
  definition: OrgDefinition;
  model: OrgModel;
  status: OrgStatus;
  revision: number;
  /** The canvas holds edits the server has not stored yet. */
  dirty: boolean;
  goals: Goal[];
  onSelectUnit: (unitId: string) => void;
  /** The result as a path for the chart to light up; null while there is none. */
  onTrace?: (trace: OrgPlanTrace | null) => void;
}

/**
 * "Where would this request go?" — run a request against the chart as drawn,
 * before anyone activates it. The basis is always the definition on the canvas,
 * so an unsaved edit is what gets answered about; nothing is written.
 */
export function OrgTester({ structureId, definition, model, status, revision, dirty, goals, onSelectUnit, onTrace }: OrgTesterProps) {
  const { t } = useT("org");
  const [text, setText] = useState("");
  const [labels, setLabels] = useState<string[]>([]);
  const [picking, setPicking] = useState(false);
  const simulate = useSimulateOrg();
  const request = { model, definition, structure_id: structureId, request: { ...orgRequestFromText(text), labels } };
  const stale = !!simulate.data && JSON.stringify(simulate.variables) !== JSON.stringify(request);
  const sim = stale || simulate.isPending ? null : simulate.data ?? null;

  const basis = dirty
    ? t(($) => $.tester.basis_unsaved, { n: revision })
    : status === "draft"
      ? t(($) => $.tester.basis_draft, { n: revision })
      : t(($) => $.tester.basis_active, { n: revision });

  const run = () => simulate.mutate(request);

  // The chart draws the same path the result lists, in the same order. A
  // squad or nobody has no card on the chart, so that step is left unmarked.
  useEffect(() => {
    if (!onTrace) return;
    if (!sim?.receives) {
      onTrace(null);
      return;
    }
    const card = (actor: OrgSimulationActor) =>
      actor.kind === "agent" || actor.kind === "member" ? { type: actor.kind, id: actor.id } : null;
    onTrace({
      unitId: sim.receives.unit_id,
      prepares: card(sim.prepares),
      decides: card(sim.decides),
      escalation: sim.escalation_path.map((ref) => ref.unit_id),
    });
  }, [sim, onTrace]);
  useEffect(() => () => onTrace?.(null), [onTrace]);

  const applyExample = (example: string) => {
    setText(example);
    setLabels([]);
  };

  const pickIssue = (issue: Issue) => {
    const form = orgFormFromIssue(issue);
    setText(form.text);
    setLabels(form.labels);
  };

  const actorLine = (actor: OrgSimulationActor): string => {
    if (actor.kind === "none") return t(($) => $.tester.nobody);
    const kind = actor.kind === "agent" ? t(($) => $.tester.kind.agent) : actor.kind === "member" ? t(($) => $.tester.kind.member) : t(($) => $.tester.kind.squad);
    return `${actor.name || actor.id || t(($) => $.tester.nobody)} · ${kind}`;
  };

  // The autonomy comes back as a free string: an unknown level falls back to
  // a generic label (orgAutonomyLabel), never to a missing translation key.
  const trust = sim?.unit ? orgAutonomyLabel(t, sim.unit.autonomy) : "";

  // Each note pairs with a code at the same index (note_codes); a code the
  // client recognizes is translated, one it does not falls back to the
  // server's own English sentence.
  const notes = sim?.notes.map((text, i) => orgSimulateNoteText(t, text, sim.note_codes[i]?.code, sim.note_codes[i]?.params)) ?? [];

  const receivingUnit = sim?.receives ? definition.units.find((u) => u.id === sim.receives?.unit_id) : undefined;
  // A linked goal names the mission best; the team's own sentence is next.
  const mission =
    goals.find((g) => g.id === receivingUnit?.mission_goal_id)?.title ?? (receivingUnit?.mission?.trim() || t(($) => $.unit.mission_none));
  // Same numbers as the badges on the chart, so the list and the drawing read
  // as one path: the decider only gets its own step when it is someone else.
  const decidesStep = sim && sim.decides.kind !== "none" && !(sim.prepares.kind === sim.decides.kind && sim.prepares.id === sim.decides.id) ? 3 : null;
  const escalateStep = decidesStep ? 4 : 3;

  const error = simulate.error;
  const errorMessage = !error
    ? null
    : error instanceof ApiError && error.status === 422
      ? t(($) => $.tester.error_invalid, { error: error.message })
      : error.message || t(($) => $.tester.error);

  const row = (label: string, testId: string, body: React.ReactNode, step?: number | null) => (
    <div data-testid={testId} className="grid grid-cols-[7rem_1fr] gap-x-3 gap-y-0.5 border-t border-border/60 py-1.5 first:border-t-0">
      <dt className="flex items-start gap-1.5 text-muted-foreground">
        {step ? (
          <span aria-hidden="true" className="mt-px grid size-4 shrink-0 place-items-center rounded-full bg-brand text-micro font-bold text-brand-foreground">
            {step}
          </span>
        ) : null}
        {label}
      </dt>
      <dd className="min-w-0">{body}</dd>
    </div>
  );

  return (
    <div data-testid="org-tester" className="flex flex-col gap-3">
      <div className="flex flex-col gap-2 rounded-md border p-3">
        <p className="text-caption text-muted-foreground">{basis}</p>{stale && <p role="status" className="text-caption text-warning">{t($ => $.tester.stale)}</p>}
        <label className="flex flex-col gap-1 text-caption text-muted-foreground">
          {t(($) => $.tester.request)}
          <Textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={4}
            placeholder={t(($) => $.tester.placeholder)}
          />
        </label>
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-caption text-muted-foreground">{t(($) => $.tester.examples)}</span>
          {([1, 2] as const).map((n) => {
            const example = n === 1 ? t(($) => $.tester.example_1) : t(($) => $.tester.example_2);
            return (
              <button
                key={n}
                type="button"
                data-testid="org-tester-example"
                onClick={() => applyExample(example)}
                className="max-w-full truncate rounded-md border px-2 py-1 text-left text-caption hover:bg-accent/70"
              >
                {example}
              </button>
            );
          })}
        </div>
        {labels.length > 0 && (
          <div data-testid="org-tester-labels" className="flex flex-wrap items-center gap-1">
            <span className="text-caption text-muted-foreground">{t(($) => $.tester.labels)}</span>
            {labels.map((l) => <Badge key={l} variant="outline">{l}</Badge>)}
          </div>
        )}
        <div className="flex flex-wrap items-center gap-2">
          <Button type="button" size="sm" className="gap-1" disabled={simulate.isPending || text.trim() === ""} onClick={run}>
            <Play className="size-3.5" />
            {simulate.isPending ? t(($) => $.tester.running) : t(($) => $.tester.submit)}
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={() => setPicking(true)}>
            {t(($) => $.tester.from_issue)}
          </Button>
        </div>
        {errorMessage !== null && <p role="alert" className="text-caption text-destructive">{errorMessage}</p>}
      </div>

      <div className="rounded-md border p-3">
        {sim === null ? (
          <p data-testid="org-tester-empty" className="text-caption text-muted-foreground">{t(($) => $.tester.empty)}</p>
        ) : sim.receives === null ? (
          <div className="flex flex-col gap-1.5 text-caption">
            <p data-testid="org-tester-no-unit" className="text-warning">{t(($) => $.tester.no_unit)}</p>
            <TesterNotes notes={notes} label={t(($) => $.tester.notes)} />
          </div>
        ) : (
          <dl data-testid="org-tester-result" className="flex flex-col text-caption">
            {row(
              t(($) => $.tester.receives),
              "org-tester-receives",
              <>
                <button
                  type="button"
                  data-testid="org-tester-unit-link"
                  onClick={() => onSelectUnit(sim.receives?.unit_id ?? "")}
                  className="text-left font-medium underline decoration-dotted underline-offset-2 hover:decoration-solid"
                  title={t(($) => $.tester.open_unit, { name: sim.receives?.unit_name ?? "" })}
                >
                  {sim.receives.unit_name}
                </button>
                <span className="block text-muted-foreground">{mission}</span>
              </>,
              1,
            )}
            {row(
              t(($) => $.tester.prepares),
              "org-tester-prepares",
              <>
                <span className="block">{actorLine(sim.prepares)}</span>
                {trust !== "" && <span className="block text-muted-foreground">{trust}</span>}
              </>,
              2,
            )}
            {row(t(($) => $.tester.decides), "org-tester-decides", actorLine(sim.decides), decidesStep)}
            {row(
              t(($) => $.tester.escalates),
              "org-tester-escalates",
              orgEscalationLabel(sim.escalation_path, t(($) => $.tester.root)),
              sim.escalation_path.length > 0 ? escalateStep : null,
            )}
            {row(
              t(($) => $.tester.blocked),
              "org-tester-blocked",
              sim.blocking_denies.length === 0 ? (
                <span className="text-muted-foreground">{t(($) => $.tester.blocked_none)}</span>
              ) : (
                <>
                  <span className="flex flex-wrap gap-1">
                    {sim.blocking_denies.map((d) => <Badge key={d} className="bg-destructive-subtle text-destructive-subtle-foreground">{orgCapabilityLabel(t, d)}</Badge>)}
                  </span>
                  <span className="mt-0.5 block text-warning">{t(($) => $.tester.blocked_consequence)}</span>
                </>
              ),
            )}
            {row(t(($) => $.tester.cost), "org-tester-cost", <span className="tabular-nums">{`$${contestCostUsd(sim.cost_estimate_usd_ticks)}`}</span>)}
            {notes.length > 0 && row(t(($) => $.tester.notes), "org-tester-notes", <TesterNotes notes={notes} label="" />)}
          </dl>
        )}
      </div>

      <IssuePickerModal
        open={picking}
        onOpenChange={setPicking}
        title={t(($) => $.tester.picker_title)}
        description={t(($) => $.tester.picker_description)}
        excludeIds={[]}
        onSelect={pickIssue}
      />
    </div>
  );
}

function TesterNotes({ notes, label }: { notes: string[]; label: string }) {
  if (notes.length === 0) return null;
  return (
    <div data-testid="org-tester-note-list">
      {label !== "" && <span className="text-muted-foreground">{label}</span>}
      <ul className="list-disc pl-4 text-muted-foreground">
        {notes.map((n) => <li key={n}>{n}</li>)}
      </ul>
    </div>
  );
}
