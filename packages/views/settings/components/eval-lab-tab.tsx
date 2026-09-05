"use client";

import { useMemo, useState, type FormEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { FlaskRound, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useCurrentWorkspace } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { agentVersionsOptions } from "@multica/core/agents/versions";
import {
  evalCasesOptions,
  evalRunsOptions,
  evalScoreTone,
  evalSuitesOptions,
  hasRunningRun,
  latestRunForSuite,
  useCreateEvalSuite,
  useRunEvalSuite,
  type EvalRun,
  type EvalRunCaseStatus,
  type EvalRunStatus,
  type EvalSuite,
} from "@multica/core/eval";
import { useT, useTimeAgo } from "../../i18n";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

/**
 * Eval Lab (K24).
 *
 * A resolved issue whose acceptance criteria are all proved can be promoted
 * to an eval case (from the issue itself). A suite groups cases; running one
 * replays every case against a single agent version and scores it on the
 * criteria the run proves again. The score is the server's verdict — nothing
 * here recomputes it, we only band it into a colour.
 */

const RUN_STATUSES: EvalRunStatus[] = ["running", "completed", "failed"];
const CASE_STATUSES: EvalRunCaseStatus[] = ["pending", "passed", "failed", "infra_failed"];

function isRunStatus(value: string): value is EvalRunStatus {
  return (RUN_STATUSES as string[]).includes(value);
}

function isCaseStatus(value: string): value is EvalRunCaseStatus {
  return (CASE_STATUSES as string[]).includes(value);
}

const TONE_CLASS = {
  success: "text-success",
  warning: "text-warning",
  destructive: "text-destructive",
} as const;

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function EvalLabTab() {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const wsId = useCurrentWorkspace()?.id ?? "";

  const casesQuery = useQuery(evalCasesOptions(wsId));
  const suitesQuery = useQuery(evalSuitesOptions(wsId));
  const runsQuery = useQuery(evalRunsOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  const createSuite = useCreateEvalSuite(wsId);

  const cases = casesQuery.data ?? [];
  const suites = suitesQuery.data ?? [];
  const runs = runsQuery.data ?? [];
  const agents = agentsQuery.data ?? [];

  const agentNames = useMemo(
    () => new Map((agentsQuery.data ?? []).map((agent) => [agent.id, agent.name])),
    [agentsQuery.data],
  );
  const agentName = (id: string) => agentNames.get(id) ?? id.slice(0, 8);

  const [name, setName] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [runningSuite, setRunningSuite] = useState<string | null>(null);

  const toggleCase = (caseId: string) =>
    setSelected((prev) => (prev.includes(caseId) ? prev.filter((id) => id !== caseId) : [...prev, caseId]));

  const handleCreate = async (event: FormEvent) => {
    event.preventDefault();
    if (name.trim() === "" || selected.length === 0) return;
    try {
      await createSuite.mutateAsync({ name: name.trim(), case_ids: selected });
      toast.success(t(($) => $.eval_lab.created_toast));
      setName("");
      setSelected([]);
    } catch (error) {
      toast.error(errorMessage(error, t(($) => $.eval_lab.create_failed)));
    }
  };

  const loading = casesQuery.isLoading || suitesQuery.isLoading;

  return (
    <SettingsTab title={t(($) => $.eval_lab.title)} description={t(($) => $.eval_lab.description)}>
      <SettingsSection
        title={t(($) => $.eval_lab.suites_title)}
        description={t(($) => $.eval_lab.suites_description)}
      >
        <SettingsCard>
          {loading ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
            </div>
          ) : suites.length === 0 ? (
            <div className="px-4 py-8 text-center" data-testid="eval-suites-empty">
              <FlaskRound className="mx-auto h-5 w-5 text-muted-foreground" aria-hidden="true" />
              <p className="mt-3 text-body font-medium">{t(($) => $.eval_lab.suites_empty_title)}</p>
              <p className="mt-1 text-caption text-muted-foreground">
                {cases.length === 0
                  ? t(($) => $.eval_lab.cases_empty_hint)
                  : t(($) => $.eval_lab.suites_empty_hint)}
              </p>
            </div>
          ) : (
            <ul className="divide-y">
              {suites.map((suite) => (
                <SuiteRow
                  key={suite.id}
                  suite={suite}
                  runs={runs}
                  wsId={wsId}
                  agents={agents}
                  agentName={agentName}
                  open={runningSuite === suite.id}
                  onOpenChange={(open) => setRunningSuite(open ? suite.id : null)}
                />
              ))}
            </ul>
          )}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.eval_lab.new_suite_title)}
        description={t(($) => $.eval_lab.new_suite_description)}
      >
        <SettingsCard className="p-4">
          {cases.length === 0 ? (
            <div className="py-4 text-center" data-testid="eval-cases-empty">
              <p className="text-body font-medium">{t(($) => $.eval_lab.cases_empty_title)}</p>
              <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.eval_lab.cases_empty_hint)}</p>
            </div>
          ) : (
            <form className="space-y-3" onSubmit={handleCreate} data-testid="eval-suite-form">
              <div className="space-y-1.5">
                <Label htmlFor="eval-suite-name">{t(($) => $.eval_lab.name_label)}</Label>
                <Input
                  id="eval-suite-name"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder={t(($) => $.eval_lab.name_placeholder)}
                />
              </div>
              <fieldset className="space-y-2">
                <legend className="text-caption font-medium text-muted-foreground">
                  {t(($) => $.eval_lab.select_cases)}
                </legend>
                {cases.map((evalCase) => (
                  <div key={evalCase.id}>
                    {/* The label wraps the title alone: an ancestor <label> wins over
                        aria-label, so anything else inside would pollute the name. */}
                    <label className="flex items-center gap-2 text-body">
                      <Checkbox
                        checked={selected.includes(evalCase.id)}
                        onCheckedChange={() => toggleCase(evalCase.id)}
                      />
                      <span className="min-w-0 truncate">{evalCase.title}</span>
                    </label>
                    <p className="pl-6 text-caption text-muted-foreground">
                      {t(($) => $.eval_lab.case_meta, {
                        number: evalCase.source_issue_number,
                        criteria: evalCase.criteria.length,
                      })}
                    </p>
                  </div>
                ))}
              </fieldset>
              <Button type="submit" size="sm" disabled={name.trim() === "" || selected.length === 0 || createSuite.isPending}>
                {t(($) => $.eval_lab.create)}
              </Button>
            </form>
          )}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.eval_lab.history_title)}
        description={t(($) => $.eval_lab.history_description)}
      >
        <SettingsCard>
          {runs.length === 0 ? (
            <p className="px-4 py-8 text-center text-caption text-muted-foreground" data-testid="eval-runs-empty">
              {t(($) => $.eval_lab.history_empty)}
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t(($) => $.eval_lab.col_suite)}</TableHead>
                  <TableHead>{t(($) => $.eval_lab.col_agent)}</TableHead>
                  <TableHead>{t(($) => $.eval_lab.col_version)}</TableHead>
                  <TableHead>{t(($) => $.eval_lab.col_status)}</TableHead>
                  <TableHead>{t(($) => $.eval_lab.col_score)}</TableHead>
                  <TableHead>{t(($) => $.eval_lab.col_started)}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {runs.map((run) => (
                  <RunRow key={run.id} run={run} agentName={agentName} timeAgo={timeAgo} />
                ))}
              </TableBody>
            </Table>
          )}
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}

function StatusBadge({ status }: { status: string }) {
  const { t } = useT("settings");
  const known = isRunStatus(status);
  const variant = !known || status === "failed" ? "destructive" : status === "completed" ? "secondary" : "outline";
  return (
    <Badge variant={variant} data-testid="eval-run-status" data-status={status}>
      {known ? t(($) => $.eval_lab.status[status]) : t(($) => $.eval_lab.status_unknown)}
    </Badge>
  );
}

function Score({ score }: { score: number | null }) {
  const { t } = useT("settings");
  if (score === null) return <span className="text-muted-foreground">{t(($) => $.eval_lab.no_score)}</span>;
  return (
    <span className={`font-mono ${TONE_CLASS[evalScoreTone(score)]}`} data-testid="eval-score">
      {t(($) => $.eval_lab.score_value, { score })}
    </span>
  );
}

function SuiteRow({
  suite,
  runs,
  wsId,
  agents,
  agentName,
  open,
  onOpenChange,
}: {
  suite: EvalSuite;
  runs: EvalRun[];
  wsId: string;
  agents: { id: string; name: string }[];
  agentName: (id: string) => string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("settings");
  const last = latestRunForSuite(runs, suite.id);
  const busy = hasRunningRun(runs, suite.id);

  return (
    <li className="flex flex-wrap items-center gap-3 px-4 py-3" data-testid="eval-suite" data-suite-id={suite.id}>
      <div className="min-w-0 flex-1">
        <p className="truncate text-body font-medium">{suite.name}</p>
        <p className="text-caption text-muted-foreground">
          {t(($) => $.eval_lab.case_count, { n: suite.case_count })}
          {last ? (
            <>
              {" · "}
              {t(($) => $.eval_lab.last_run, { agent: agentName(last.agent_id) })}{" "}
              <Score score={last.score} />
            </>
          ) : (
            <>{" · "}{t(($) => $.eval_lab.never_run)}</>
          )}
        </p>
      </div>
      {busy ? (
        <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
          {t(($) => $.eval_lab.status.running)}
        </span>
      ) : open ? null : (
        <Button size="sm" variant="outline" onClick={() => onOpenChange(true)}>
          {t(($) => $.eval_lab.run)}
        </Button>
      )}
      {open && !busy ? (
        <RunSuiteForm
          suiteId={suite.id}
          wsId={wsId}
          agents={agents}
          onDone={() => onOpenChange(false)}
        />
      ) : null}
    </li>
  );
}

function RunSuiteForm({
  suiteId,
  wsId,
  agents,
  onDone,
}: {
  suiteId: string;
  wsId: string;
  agents: { id: string; name: string }[];
  onDone: () => void;
}) {
  const { t } = useT("settings");
  const [agentId, setAgentId] = useState("");
  const [versionId, setVersionId] = useState("");
  const runSuite = useRunEvalSuite(wsId);
  const versionsQuery = useQuery({ ...agentVersionsOptions(wsId, agentId), enabled: agentId !== "" });
  const versions = versionsQuery.data ?? [];

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (agentId === "" || versionId === "") return;
    runSuite.mutate(
      { suiteId, agent_id: agentId, agent_version_id: versionId },
      {
        onSuccess: () => onDone(),
        onError: (error) => toast.error(errorMessage(error, t(($) => $.eval_lab.run_failed))),
      },
    );
  };

  const selectClass = "rounded-md border border-input bg-transparent px-2 py-1 text-caption";

  return (
    <form className="flex w-full flex-wrap items-center gap-2" onSubmit={submit} data-testid="eval-run-form">
      <select
        aria-label={t(($) => $.eval_lab.agent)}
        className={selectClass}
        value={agentId}
        onChange={(event) => {
          setAgentId(event.target.value);
          setVersionId("");
        }}
      >
        <option value="">{t(($) => $.eval_lab.pick_agent)}</option>
        {agents.map((agent) => (
          <option key={agent.id} value={agent.id}>{agent.name}</option>
        ))}
      </select>
      <select
        aria-label={t(($) => $.eval_lab.version)}
        className={selectClass}
        value={versionId}
        disabled={agentId === ""}
        onChange={(event) => setVersionId(event.target.value)}
      >
        <option value="">{t(($) => $.eval_lab.pick_version)}</option>
        {versions.map((version) => (
          <option key={version.id} value={version.id}>
            {t(($) => $.eval_lab.version_label, { number: version.version_number })}
            {version.note ? ` — ${version.note}` : ""}
          </option>
        ))}
      </select>
      <Button type="submit" size="sm" disabled={agentId === "" || versionId === "" || runSuite.isPending}>
        {t(($) => $.eval_lab.run)}
      </Button>
      <Button type="button" size="sm" variant="ghost" onClick={onDone}>
        {t(($) => $.eval_lab.cancel)}
      </Button>
    </form>
  );
}

function RunRow({
  run,
  agentName,
  timeAgo,
}: {
  run: EvalRun;
  agentName: (id: string) => string;
  timeAgo: (date: string) => string;
}) {
  const { t } = useT("settings");
  const [open, setOpen] = useState(false);

  return (
    <>
      <TableRow data-testid="eval-run" data-run-id={run.id}>
        <TableCell>
          <button
            type="button"
            className="text-left font-medium hover:underline"
            onClick={() => setOpen((prev) => !prev)}
            aria-expanded={open}
          >
            {run.suite_name}
          </button>
        </TableCell>
        <TableCell>{agentName(run.agent_id)}</TableCell>
        <TableCell className="font-mono">
          {t(($) => $.eval_lab.version_label, { number: run.agent_version_number })}
        </TableCell>
        <TableCell><StatusBadge status={run.status} /></TableCell>
        <TableCell><Score score={run.score} /></TableCell>
        <TableCell className="text-muted-foreground">
          {run.started_at ? timeAgo(run.started_at) : "—"}
        </TableCell>
      </TableRow>
      {open ? (
        <TableRow data-testid="eval-run-details">
          <TableCell colSpan={6} className="bg-muted/40">
            {run.cases.length === 0 ? (
              <p className="text-caption text-muted-foreground">{t(($) => $.eval_lab.no_cases)}</p>
            ) : (
              <ul className="space-y-1">
                {run.cases.map((runCase) => (
                  <li key={runCase.case_id} className="flex flex-wrap items-center gap-2 text-caption" data-testid="eval-run-case">
                    <span className="min-w-0 flex-1 truncate font-medium">{runCase.case_title}</span>
                    <span className={TONE_CLASS[runCase.status === "passed" ? "success" : runCase.status === "pending" ? "warning" : "destructive"]}>
                      {isCaseStatus(runCase.status)
                        ? t(($) => $.eval_lab.case_status[runCase.status])
                        : t(($) => $.eval_lab.status_unknown)}
                    </span>
                    <Score score={runCase.score} />
                    {runCase.detail ? <span className="text-muted-foreground">{runCase.detail}</span> : null}
                  </li>
                ))}
              </ul>
            )}
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}
