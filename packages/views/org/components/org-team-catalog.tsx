"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, Check, Users, Headphones, Code2, PenTool, ChartNoAxesCombined, Telescope } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { orgTeamCatalogOptions, usePrepareOrgTeam, useInstallOrgTeam } from "@multica/core/org";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

const TEAM_ICONS = { support: Headphones, product: Code2, agency: PenTool, finance: ChartNoAxesCombined, research: Telescope };

export function OrgTeamCatalog({ onInstalled, canInstall = false }: { onInstalled: () => void; canInstall?: boolean }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const names = t($ => $.catalog.names, { returnObjects: true });
  const countLabels = t($ => $.catalog.count_labels, { returnObjects: true });
  const descriptions = t($ => $.catalog.descriptions, { returnObjects: true });
  const { data: teams = [], isPending, isError, refetch } = useQuery(orgTeamCatalogOptions(wsId));
  const prepare = usePrepareOrgTeam();
  const install = useInstallOrgTeam(wsId);
  const [selected, setSelected] = useState("");
  const team = teams.find(t => t.id === selected);
  if (isPending) return <p role="status" className="p-4 text-caption text-muted-foreground">{t($ => $.new.loading)}</p>;
  if (isError) return <Button variant="outline" onClick={() => void refetch()}>{t($ => $.catalog.retry)}</Button>;
  const message = prepare.error ?? install.error;
  return <div className="space-y-4">
    {!canInstall && <p className="text-caption text-muted-foreground">{t($ => $.coherence.catalog_admin)}</p>}
    <div className="grid gap-3 sm:grid-cols-2">
      {teams.map(team => { const Icon = TEAM_ICONS[team.id as keyof typeof TEAM_ICONS] ?? Users; return <button key={team.id} aria-pressed={selected === team.id} disabled={!canInstall || prepare.isPending || install.isPending} onClick={() => { setSelected(team.id); install.reset(); prepare.mutate(team.id); }} className={`group relative rounded-2xl border p-5 text-left transition-[border-color,box-shadow] hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${selected === team.id ? "border-info bg-info/5 ring-2 ring-info/10" : "hover:border-info/30 hover:bg-muted/20"}`}><span className="mb-4 flex size-11 items-center justify-center rounded-xl bg-info/10 text-info"><Icon className="size-5" /></span><ArrowRight className="absolute right-5 top-6 size-4 text-muted-foreground transition-transform group-hover:translate-x-1 motion-reduce:transform-none" /><h4 className="text-title-sm font-semibold">{names[team.id as keyof typeof names] ?? team.name}</h4><p className="mt-1 text-caption leading-relaxed text-muted-foreground">{descriptions[team.id as keyof typeof descriptions] ?? team.description}</p><span className="mt-4 inline-flex items-center gap-1.5 text-caption text-muted-foreground"><Users className="size-3.5" />{t($ => $.catalog.roles, { count: team.roles.length })}</span></button>; })}
    </div>
    {message && <p role="alert" className="text-caption text-destructive">{message.message}</p>}
    {prepare.isPending && <p role="status" className="text-caption">{t($ => $.catalog.preparing)}</p>}
    {team && prepare.isSuccess && !install.isSuccess && <section className="space-y-3 rounded-xl bg-muted/50 p-4" aria-label={t($ => $.catalog.preview)}>
      <h4 className="text-body font-semibold">{t($ => $.catalog.preview)}</h4>
      <ul className="space-y-1 text-caption">{team.roles.map(role => <li key={role} className="flex items-center gap-2"><Check className="size-3.5 text-success" />{role}</li>)}</ul>
      <p className="text-caption">{t($ => $.catalog.includes)}</p>
      <p className="text-caption text-muted-foreground">{t($ => $.catalog.paused)}</p>
      {prepare.data.preview.collisions.length > 0 && <p className="text-caption text-warning">{t($ => $.catalog.collisions, { count: prepare.data.preview.collisions.length })}</p>}
      <Button disabled={!canInstall || install.isPending} className="gap-2" onClick={() => install.mutate(prepare.data.file)}>{install.isPending ? t($ => $.catalog.installing) : t($ => $.catalog.install)}<ArrowRight className="size-4" /></Button>
    </section>}
    {install.isSuccess && <div className="space-y-3 rounded-xl border border-success/30 bg-success/5 p-4"><h4 className="text-body font-semibold">{t($ => $.catalog.installed)}</h4><p className="text-caption">{t($ => $.catalog.next)}</p>{Object.entries(install.data.report.created).map(([kind,count]) => <p className="text-caption" key={kind}>{countLabels[kind as keyof typeof countLabels] ?? kind}: {count}</p>)}{install.data.report.warnings.map(w => <p key={w} className="text-caption text-warning">{w}</p>)}<Button onClick={onInstalled}>{t($ => $.catalog.done)}</Button></div>}
  </div>;
}
