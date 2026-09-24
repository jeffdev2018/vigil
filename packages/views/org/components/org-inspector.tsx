"use client";

import { useQuery } from "@tanstack/react-query";
import type { OrgDefinition, OrgStructure } from "@multica/core/types";
import type { OrgProblem } from "@multica/core/org/validate";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { goalListOptions } from "@multica/core/goals";
import { Button } from "@multica/ui/components/ui/button";
import type { OrgSelection } from "./org-plan";
import type { OrgSettingsForm } from "./org-form";
import { OrgOverviewPanel } from "./org-overview-panel";
import { OrgUnitPanel } from "./org-unit-panel";
import { OrgPersonPanel } from "./org-person-panel";
import { useT } from "../../i18n";

/**
 * The 360px fiche at the right of the chart: the whole organization, one
 * team, or one person, depending on `selection`. It loads its own directory
 * (agents, members, squads, goals) and hands the same instances to whichever
 * panel is showing, so a rename or a status change never reads stale.
 *
 * It holds no state of its own that could diverge from `definition` — every
 * edit a panel makes calls `onChange` with a freshly computed definition, or
 * `onFormChange` for the organization's own settings.
 */
export function OrgInspector({
  structure,
  definition,
  onChange,
  form,
  onFormChange,
  jsonText,
  onJsonChange,
  jsonError,
  selection,
  onSelect,
  problems,
  readOnly,
  onAddMembers,
}: {
  structure: OrgStructure;
  definition: OrgDefinition;
  onChange: (next: OrgDefinition) => void;
  form: OrgSettingsForm;
  onFormChange: <K extends keyof OrgSettingsForm>(key: K, value: OrgSettingsForm[K]) => void;
  jsonText: string;
  onJsonChange: (text: string) => void;
  jsonError: string | null;
  selection: OrgSelection;
  onSelect: (selection: OrgSelection) => void;
  problems: OrgProblem[];
  readOnly: boolean;
  onAddMembers: (unitId: string) => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const agentsQ = useQuery(agentListOptions(wsId));
  const membersQ = useQuery(memberListOptions(wsId));
  const squadsQ = useQuery(squadListOptions(wsId));
  const goalsQ = useQuery(goalListOptions(wsId));

  const loading = agentsQ.isPending || membersQ.isPending || squadsQ.isPending || goalsQ.isPending;
  const errored = agentsQ.isError || membersQ.isError || squadsQ.isError || goalsQ.isError;
  const retry = () => { void agentsQ.refetch(); void membersQ.refetch(); void squadsQ.refetch(); void goalsQ.refetch(); };

  return (
    <aside aria-label={t(($) => $.inspector.title)} className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-surface">
      {loading ? (
        <p className="p-4 text-caption text-muted-foreground">{t(($) => $.inspector.loading)}</p>
      ) : errored ? (
        <div role="alert" className="flex flex-col items-start gap-2 p-4 text-caption text-destructive">
          <span>{t(($) => $.inspector.directory_error)}</span>
          <Button size="sm" variant="outline" onClick={retry}>{t(($) => $.catalog.retry)}</Button>
        </div>
      ) : selection.kind === "org" ? (
        <OrgOverviewPanel
          structure={structure}
          definition={definition}
          onChange={onChange}
          form={form}
          onFormChange={onFormChange}
          jsonText={jsonText}
          onJsonChange={onJsonChange}
          jsonError={jsonError}
          problems={problems}
          readOnly={readOnly}
          onSelect={(unitId) => onSelect({ kind: "unit", unitId })}
          members={membersQ.data ?? []}
        />
      ) : selection.kind === "unit" ? (
        definition.units.some((u) => u.id === selection.unitId) ? (
          <OrgUnitPanel
            definition={definition}
            unit={definition.units.find((u) => u.id === selection.unitId)!}
            orgModel={form.model}
            onChange={onChange}
            onRemoved={() => onSelect({ kind: "org" })}
            onAddMembers={onAddMembers}
            problems={problems}
            readOnly={readOnly}
            members={membersQ.data ?? []}
            squads={squadsQ.data ?? []}
            goals={goalsQ.data ?? []}
          />
        ) : (
          <p className="p-4 text-caption text-muted-foreground">{t(($) => $.canvas.no_selection)}</p>
        )
      ) : definition.units.some((u) => u.id === selection.unitId) ? (
        <OrgPersonPanel
          definition={definition}
          unitId={selection.unitId}
          member={selection.member}
          onChange={onChange}
          onSelect={(unitId) => onSelect({ kind: "unit", unitId })}
          readOnly={readOnly}
          agents={agentsQ.data ?? []}
        />
      ) : (
        <p className="p-4 text-caption text-muted-foreground">{t(($) => $.canvas.no_selection)}</p>
      )}
    </aside>
  );
}
