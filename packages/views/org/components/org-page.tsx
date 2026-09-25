"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowUpRight, Network, Plus, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import {
  moveOrgMember,
  orgDetailOptions,
  orgListOptions,
  parseEditableOrgDefinition,
  useSetOrgStructureStatus,
  useUpdateOrgStructure,
} from "@multica/core/org";
import { useOrgDraftStore, saveOrgDraft, clearOrgDraft } from "@multica/core/org/draft-store";
import { validateOrgDefinition } from "@multica/core/org/validate";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { goalListOptions } from "@multica/core/goals";
import type { OrgDefinition, OrgRevision, OrgStructure } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";
import { ExportImportSetting } from "../../settings/components/export-import-setting";
import { useT } from "../../i18n";
import { orgModelLabel } from "../labels";
import { OrgAddTeamDialog } from "./org-add-team-dialog";
import { OrgBar, OrgStatusPill, type OrgBarAction } from "./org-bar";
import { OrgDirectorySheet } from "./org-directory";
import {
  OrgActivateDialog,
  OrgPublishDialog,
  OrgReasonDialog,
  orgErrorMessage,
  type OrgReasonAction,
} from "./org-dialogs";
import { orgSettingsFormOf, toRFC3339, type OrgSettingsForm } from "./org-form";
import { OrgHistorySheet } from "./org-history";
import { OrgInspector } from "./org-inspector";
import { OrgPlan, type OrgPlanTrace, type OrgSelection } from "./org-plan";
import { OrgTeamCatalog } from "./org-team-catalog";
import { OrgTester } from "./org-tester";
import { OrgWizard } from "./org-wizard";

export { OrgTemplateCards } from "./org-template-cards";

/** What the page edits before publishing: the structure's settings and its definition, as JSON text. */
type OrgEditForm = OrgSettingsForm & { definition: string };

const formOf = (s: OrgStructure): OrgEditForm => ({
  ...orgSettingsFormOf(s),
  definition: JSON.stringify(s.definition, null, 2),
});

/**
 * The server lets only a workspace owner or admin create, change, pause or
 * delete a structure. Everyone else reads it: offering them the controls only
 * to answer 403 on save was the old page's behaviour.
 */
function useCanEditOrg(): boolean {
  const wsId = useWorkspaceId();
  const me = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const role = members.find((m) => m.user_id === me)?.role;
  return role === "owner" || role === "admin";
}

// ---------------------------------------------------------------------------
// Detail: the chart is the page, the inspector sits beside it.
// ---------------------------------------------------------------------------

function OrgDetail({
  id,
  structures,
  scopeOf,
  onSelectStructure,
  onBrowse,
  onCreate,
  onCatalog,
  onDeleted,
}: {
  id: string;
  structures: OrgStructure[];
  scopeOf: (s: OrgStructure) => string;
  onSelectStructure: (id: string) => void;
  onBrowse: () => void;
  onCreate: () => void;
  onCatalog: () => void;
  onDeleted: () => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data, isPending, isError, refetch } = useQuery(orgDetailOptions(wsId, id));
  if (isError || (!isPending && !data))
    return (
      <CollectionPageState
        icon={Network}
        title={t(($) => $.form.error)}
        actions={
          <>
            <Button onClick={() => void refetch()}>{t(($) => $.catalog.retry)}</Button>
            <Button variant="ghost" onClick={onBrowse}>
              {t(($) => $.bar.browse)}
            </Button>
          </>
        }
      />
    );
  if (isPending || !data) return <CollectionPageState icon={Network} title={t(($) => $.page.loading)} />;
  return (
    <OrgDetailBody
      key={data.structure.id}
      structure={data.structure}
      revisions={data.revisions}
      structures={structures}
      scopeOf={scopeOf}
      onSelectStructure={onSelectStructure}
      onBrowse={onBrowse}
      onCreate={onCreate}
      onCatalog={onCatalog}
      onDeleted={onDeleted}
    />
  );
}

function OrgDetailBody({
  structure,
  revisions,
  structures,
  scopeOf,
  onSelectStructure,
  onBrowse,
  onCreate,
  onCatalog,
  onDeleted,
}: {
  structure: OrgStructure;
  revisions: OrgRevision[];
  structures: OrgStructure[];
  scopeOf: (s: OrgStructure) => string;
  onSelectStructure: (id: string) => void;
  onBrowse: () => void;
  onCreate: () => void;
  onCatalog: () => void;
  onDeleted: () => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const canEdit = useCanEditOrg();
  const update = useUpdateOrgStructure(wsId);
  const setStatus = useSetOrgStructureStatus(wsId);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: goals = [] } = useQuery(goalListOptions(wsId));

  // A draft survives navigation and reloads. One persisted before the model
  // selector existed has no model, and the structure's own fills the gap.
  const original = useMemo(() => formOf(structure), [structure]);
  const [form, setForm] = useState<OrgEditForm>(() => {
    const draft = useOrgDraftStore.getState().draft.edits[structure.id]?.form;
    return draft ? { ...original, ...draft } : original;
  });
  const [baseRevision, setBaseRevision] = useState(
    () => useOrgDraftStore.getState().draft.edits[structure.id]?.revision ?? structure.revision,
  );
  useEffect(() => {
    if (!useOrgDraftStore.getState().draft.edits[structure.id]) {
      setForm(original);
      setBaseRevision(structure.revision);
    }
  }, [structure, original]);

  const [selection, setSelection] = useState<OrgSelection>({ kind: "org" });
  const [testing, setTesting] = useState(false);
  const [trace, setTrace] = useState<OrgPlanTrace | null>(null);
  const [directoryUnit, setDirectoryUnit] = useState<string | null>(null);
  const [addingTeam, setAddingTeam] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [transferring, setTransferring] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [dialog, setDialog] = useState<"activate" | OrgReasonAction | null>(null);

  const set = useCallback(
    <K extends keyof OrgEditForm>(key: K, value: OrgEditForm[K]) => {
      setForm((prev) => {
        const next = { ...prev, [key]: value };
        saveOrgDraft(structure.id, { revision: baseRevision, form: next });
        return next;
      });
    },
    [structure.id, baseRevision],
  );
  const setDefinition = useCallback(
    (def: OrgDefinition) => set("definition", JSON.stringify(def, null, 2)),
    [set],
  );
  const discard = () => {
    clearOrgDraft(structure.id);
    setForm(original);
    setBaseRevision(structure.revision);
  };

  const parsed = useMemo(() => parseEditableOrgDefinition(form.definition), [form.definition]);
  const definition = "def" in parsed ? parsed.def : null;
  const readOnly = structure.status === "dissolved" || update.isPending || !canEdit;
  const problems = useMemo(
    () =>
      definition
        ? validateOrgDefinition(definition, {
            model: form.model,
            agentTrust: Object.fromEntries(agents.map((a) => [a.id, a.trust_mode ?? ""])),
            agentName: Object.fromEntries(agents.map((a) => [a.id, a.name])),
          })
        : [],
    [definition, form.model, agents],
  );
  const dirty = (Object.keys(original) as (keyof OrgEditForm)[]).some((key) => form[key] !== original[key]);
  const conflict = baseRevision !== structure.revision && dirty;
  const canPublish = dirty && !!definition && !!form.name.trim() && problems.length === 0 && !conflict;

  useEffect(() => {
    if (!dirty) return;
    const preventLoss = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", preventLoss);
    return () => window.removeEventListener("beforeunload", preventLoss);
  }, [dirty]);

  const save = () => {
    if (!definition || !canPublish) return;
    update.mutate(
      {
        id: structure.id,
        data: {
          expected_revision: baseRevision,
          definition,
          model: form.model,
          name: form.name.trim(),
          owner_id: form.owner_id,
          dissolve_at: toRFC3339(form.dissolve_at) ?? "",
          end_condition: form.end_condition,
          budget_usd_ticks: Number(form.budget) || 0,
        },
      },
      {
        onSuccess: (saved) => {
          clearOrgDraft(structure.id);
          if (saved) {
            setBaseRevision(saved.revision);
            setForm(formOf(saved));
          }
          setPublishing(false);
          toast.success(t(($) => $.form.saved));
        },
        onError: (e) => toast.error(orgErrorMessage(e, t(($) => $.form.error))),
      },
    );
  };

  const onAction = (action: OrgBarAction) => {
    if (action === "history") setHistoryOpen(true);
    else if (action === "transfer") setTransferring(true);
    else if (action === "resume")
      setStatus.mutate(
        { id: structure.id, action: "resume" },
        { onError: (e) => toast.error(orgErrorMessage(e, t(($) => $.actions.error))) },
      );
    else setDialog(action);
  };

  const toggleTest = () => {
    setTesting((on) => {
      if (on) setTrace(null);
      return !on;
    });
  };

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <OrgBar
        structure={structure}
        structures={structures}
        scopeOf={scopeOf}
        dirty={dirty}
        saving={update.isPending}
        canEdit={canEdit}
        canPublish={canPublish}
        testing={testing}
        onSelectStructure={onSelectStructure}
        onBrowse={onBrowse}
        onCreate={onCreate}
        onCatalog={onCatalog}
        onToggleTest={toggleTest}
        onDiscard={discard}
        onPublish={() => (structure.status === "active" ? setPublishing(true) : save())}
        onAction={onAction}
      />

      {(structure.paused_reason || structure.status === "dissolved" || !canEdit || conflict) && (
        <div className="flex flex-col gap-1 border-b bg-surface px-4 py-2 text-caption">
          {structure.paused_reason && (
            <p className="flex items-center gap-2 text-warning-strong">
              <TriangleAlert className="size-3.5 shrink-0" aria-hidden="true" />
              {t(($) => $.page.paused_because, { reason: structure.paused_reason })}
            </p>
          )}
          {structure.status === "dissolved" && <p className="text-muted-foreground">{t(($) => $.page.read_only)}</p>}
          {structure.status !== "dissolved" && !canEdit && (
            <p className="text-muted-foreground">{t(($) => $.page.read_only_role)}</p>
          )}
          {conflict && (
            <p role="alert" className="text-warning-strong">
              {t(($) => $.coherence.conflict)}
            </p>
          )}
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
        {definition ? (
          <OrgPlan
            definition={definition}
            selection={selection}
            onSelect={(next) => {
              setSelection(next);
              if (testing && next.kind !== "org") setTesting(false);
            }}
            trace={testing ? trace : null}
            pausedUnits={structure.paused_units}
            agents={agents}
            readOnly={readOnly}
            onMoveMember={(from, to, member) => setDefinition(moveOrgMember(definition, from, to, member))}
            onAddMembers={setDirectoryUnit}
            onAddUnit={() => setAddingTeam(true)}
            empty={
              <div className="flex flex-1 flex-col items-start justify-center gap-3 bg-page-canvas p-10">
                <h2 className="text-title-sm font-semibold">{t(($) => $.plan.empty_title)}</h2>
                <p className="max-w-md text-body text-muted-foreground">{t(($) => $.plan.empty_hint)}</p>
                {!readOnly && (
                  <div className="flex gap-2">
                    <Button onClick={() => setAddingTeam(true)}>
                      <Plus />
                      {t(($) => $.plan.add_team)}
                    </Button>
                    <Button variant="outline" onClick={onCatalog}>
                      {t(($) => $.bar.catalog)}
                    </Button>
                  </div>
                )}
              </div>
            }
          />
        ) : (
          <div className="flex flex-1 items-center justify-center bg-page-canvas p-10 text-body text-muted-foreground">
            {t(($) => $.canvas.json_broken)}
          </div>
        )}

        {/* The column only sizes and borders; each panel inside names itself. */}
        <div className="flex max-h-[45vh] min-h-0 flex-col border-t bg-surface lg:max-h-none lg:w-[360px] lg:shrink-0 lg:border-l lg:border-t-0">
          {testing && definition ? (
            <aside aria-label={t(($) => $.bar.test)} className="min-h-0 flex-1 overflow-y-auto p-4">
              <OrgTester
                structureId={structure.id}
                definition={definition}
                model={form.model}
                status={structure.status}
                revision={structure.revision}
                dirty={dirty}
                goals={goals}
                onSelectUnit={(unitId) => setSelection({ kind: "unit", unitId })}
                onTrace={setTrace}
              />
            </aside>
          ) : (
            <OrgInspector
              structure={structure}
              definition={definition ?? structure.definition}
              onChange={setDefinition}
              form={form}
              // OrgEditForm is OrgSettingsForm plus the definition, so every
              // settings key maps to the same field type on both.
              onFormChange={set as <K extends keyof OrgSettingsForm>(key: K, value: OrgSettingsForm[K]) => void}
              jsonText={form.definition}
              onJsonChange={(text) => set("definition", text)}
              jsonError={"error" in parsed ? parsed.error : null}
              selection={selection}
              onSelect={setSelection}
              problems={problems}
              readOnly={readOnly || !definition}
              onAddMembers={setDirectoryUnit}
            />
          )}
        </div>
      </div>

      {directoryUnit && definition && (
        <OrgDirectorySheet
          definition={definition}
          unitId={directoryUnit}
          onAdd={setDefinition}
          onClose={() => setDirectoryUnit(null)}
        />
      )}
      {addingTeam && definition && (
        <OrgAddTeamDialog
          definition={definition}
          model={form.model}
          onAdd={(next, unitId) => {
            setDefinition(next);
            setSelection({ kind: "unit", unitId });
          }}
          onClose={() => setAddingTeam(false)}
        />
      )}
      {historyOpen && (
        <OrgHistorySheet
          structure={structure}
          revisions={revisions}
          dirty={dirty}
          readOnly={readOnly}
          onClose={() => setHistoryOpen(false)}
        />
      )}
      {publishing && definition && (
        <OrgPublishDialog
          before={structure.definition}
          after={definition}
          pending={update.isPending}
          onCancel={() => setPublishing(false)}
          onConfirm={save}
        />
      )}
      {transferring && (
        <Dialog open onOpenChange={setTransferring}>
          <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>{t(($) => $.wizard.transfer)}</DialogTitle>
              <DialogDescription>{t(($) => $.wizard.transfer_hint)}</DialogDescription>
            </DialogHeader>
            <ExportImportSetting canEdit={canEdit} />
          </DialogContent>
        </Dialog>
      )}
      {dialog === "activate" && <OrgActivateDialog structure={structure} onClose={() => setDialog(null)} />}
      {dialog && dialog !== "activate" && (
        <OrgReasonDialog
          structure={structure}
          action={dialog}
          onClose={() => setDialog(null)}
          onDone={dialog === "delete" ? onDeleted : undefined}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Page: one structure at a time, or the list of all of them.
// ---------------------------------------------------------------------------

export function OrgPage() {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const canEdit = useCanEditOrg();
  const { data: structures = [], isLoading, isError, refetch } = useQuery(orgListOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const selectedId = useOrgDraftStore((s) => s.draft.selectedId);
  const [browsing, setBrowsing] = useState(false);
  const [search, setSearch] = useState("");
  const [creating, setCreating] = useState(false);
  const [catalogOpen, setCatalogOpen] = useState(false);
  const select = (id: string | null) => {
    useOrgDraftStore.getState().setDraft({ selectedId: id });
    setBrowsing(false);
  };

  const projectTitle = new Map(projects.map((p) => [p.id, p.title]));
  // The workspace default first, then project structures by age.
  const sorted = [...structures].sort(
    (a, b) => Number(a.project_id !== null) - Number(b.project_id !== null) || a.created_at.localeCompare(b.created_at),
  );
  const current =
    sorted.find((s) => s.id === selectedId) ??
    sorted.find((s) => s.project_id === null && s.status !== "dissolved") ??
    sorted.find((s) => s.status === "active") ??
    sorted[0];
  const scopeOf = (s: OrgStructure) =>
    s.project_id ? (projectTitle.get(s.project_id) ?? t(($) => $.page.unknown_project)) : t(($) => $.page.workspace_default);

  const dialogs = (
    <>
      {catalogOpen && (
        <Dialog open onOpenChange={setCatalogOpen}>
          <DialogContent className="max-h-[85vh] overflow-auto sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>{t(($) => $.wizard.catalog)}</DialogTitle>
              <DialogDescription>{t(($) => $.catalog.includes)}</DialogDescription>
            </DialogHeader>
            <OrgTeamCatalog canInstall={canEdit} onInstalled={() => setCatalogOpen(false)} />
          </DialogContent>
        </Dialog>
      )}
      {creating && <OrgWizard onClose={() => setCreating(false)} onCreated={select} />}
    </>
  );

  if (isLoading || isError)
    return (
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        <CollectionPageHeader icon={Network} title={t(($) => $.page.title)} />
        <CollectionPageState
          icon={Network}
          title={isError ? t(($) => $.form.error) : t(($) => $.page.loading)}
          actions={isError ? <Button onClick={() => void refetch()}>{t(($) => $.catalog.retry)}</Button> : undefined}
        />
      </div>
    );

  if (current && !browsing)
    return (
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        <OrgDetail
          key={current.id}
          id={current.id}
          structures={sorted}
          scopeOf={scopeOf}
          onSelectStructure={select}
          onBrowse={() => setBrowsing(true)}
          onCreate={() => setCreating(true)}
          onCatalog={() => setCatalogOpen(true)}
          onDeleted={() => {
            select(null);
            setBrowsing(true);
          }}
        />
        {dialogs}
      </div>
    );

  const needle = search.trim().toLocaleLowerCase();
  const shown = sorted.filter((s) => !needle || `${s.name} ${scopeOf(s)}`.toLocaleLowerCase().includes(needle));
  return (
    <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={Network}
        title={t(($) => $.bar.browse)}
        count={sorted.length}
        actions={
          canEdit ? (
            <CollectionPageHeaderAction icon={Plus} label={t(($) => $.bar.create)} onClick={() => setCreating(true)} />
          ) : undefined
        }
      />
      <div className="flex-1 overflow-auto px-4 py-6 md:px-8">
        {sorted.length === 0 ? (
          <div className="flex max-w-xl flex-col items-start gap-3 py-10">
            <h2 className="text-title-lg font-semibold">{t(($) => $.studio.empty_title)}</h2>
            <p className="text-body text-muted-foreground">{t(($) => $.studio.empty_hint)}</p>
            {canEdit && (
              <div className="flex gap-2">
                <Button onClick={() => setCreating(true)}>
                  <Plus />
                  {t(($) => $.bar.create)}
                </Button>
                <Button variant="outline" onClick={() => setCatalogOpen(true)}>
                  {t(($) => $.bar.catalog)}
                </Button>
              </div>
            )}
          </div>
        ) : (
          <div className="mx-auto flex max-w-4xl flex-col gap-4">
            <Input
              className="max-w-sm"
              aria-label={t(($) => $.studio.search_org)}
              placeholder={t(($) => $.studio.search_org)}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {shown.length === 0 ? (
              <p className="text-caption text-muted-foreground">{t(($) => $.studio.no_match, { query: search.trim() })}</p>
            ) : (
              <ul className="divide-y rounded-xl border bg-surface">
                {shown.map((s) => (
                  <li key={s.id}>
                    <button
                      type="button"
                      data-testid="org-structure"
                      onClick={() => select(s.id)}
                      className="group flex w-full items-center gap-4 px-4 py-3 text-left hover:bg-surface-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-body font-medium">{s.name}</span>
                        <span className="mt-0.5 block truncate text-caption text-muted-foreground">
                          {scopeOf(s)} · {orgModelLabel(t, s.model)}
                        </span>
                      </span>
                      <span className="hidden text-caption tabular-nums text-muted-foreground sm:block">
                        {t(($) => $.visual.team_count, { count: s.definition.units.length })}
                      </span>
                      <OrgStatusPill status={s.status} />
                      <ArrowUpRight
                        aria-hidden="true"
                        className="size-4 text-faint-foreground transition-transform group-hover:translate-x-0.5"
                      />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
      {dialogs}
    </div>
  );
}
