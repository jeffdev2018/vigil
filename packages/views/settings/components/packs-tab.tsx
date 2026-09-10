"use client";

import { useState, type ChangeEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { Blocks, Check, Download, Package, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import {
  PACK_STRATEGIES,
  packCatalogueOptions,
  packCountTotal,
  packDetailOptions,
  packInstallOptions,
  packInstallsOptions,
  useDownloadPack,
  useExportPack,
  useInstallPack,
  useInstallPackUpload,
  usePreviewPack,
  usePreviewPackUpload,
  useUninstallPack,
  type PackContents,
  type PackInstall,
  type PackPreview,
  type PackReport,
  type PackStrategy,
  type PackSummary,
  type PackUninstallReport,
} from "@multica/core/packs";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Markdown } from "@multica/ui/markdown";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

/**
 * Packs (OS plan, vague B).
 *
 * A pack turns a workspace into a ready-to-use setup for a function —
 * statuses, work item types, labels, properties, views, transition and
 * business rules, a doctrine section, a procedure skill, agents, autopilots,
 * a project, a goal, a Brain note, example issues. Every pack works with
 * zero agent: agents arrive paused with no runtime and autopilots disabled,
 * so a human team gets the whole setup and decides later.
 *
 * Nothing here is optimistic. An install runs the transfer pipeline
 * server-side — what it creates, merges or skips per kind is a server
 * decision — and an uninstall decides row by row what it may remove, so
 * both await the server (CLAUDE.md state rules). The YAML is never parsed
 * client-side either: the server previews an upload and tells us the
 * collisions, the problems and which strategy it picked.
 */

const SELECT_CLASS =
  "h-8 w-full rounded-md border border-input bg-transparent px-2 text-caption";

const PREREQUISITE_TONE: Record<string, string> = {
  met: "bg-success/10 text-success",
  missing: "bg-warning/10 text-warning",
  unknown: "bg-muted text-muted-foreground",
};

const INSTALL_STATUS_TONE: Record<string, string> = {
  installed: "bg-success/10 text-success",
  failed: "bg-destructive/10 text-destructive",
  removed: "bg-muted text-muted-foreground",
};

/** Fallback domain select value: the tab shows every pack. */
const ALL_DOMAINS = "all";

/**
 * An upload posts through the web app's Next.js `/api` rewrite, which stalls
 * on request bodies past this and answers 500 after ~30s (8 MB passes, 16 MB
 * hangs). The server itself accepts 32 MB, so a bigger pack is not refused —
 * it is refused *here*, and pointed at the CLI, which talks to the API
 * directly.
 */
const UPLOAD_MAX_BYTES = 8 * 1024 * 1024;
const UPLOAD_MAX_LABEL = `${UPLOAD_MAX_BYTES / 1024 / 1024} MB`;

/**
 * The server's own text, when there is one. It is never shown on its own: a
 * raw "API error: 500 Internal Server Error" tells a user nothing, so every
 * caller pairs it with the translated label and shows it as a second line.
 */
function errorDetail(error: unknown): string {
  return error instanceof Error && error.message ? error.message : "";
}

/** Translated label first, the server's text under it. */
function ErrorLines({ label, detail }: { label: string; detail: string }) {
  return (
    <div role="alert" className="flex flex-col gap-0.5">
      <p className="text-caption text-destructive">{label}</p>
      {detail && (
        <p className="text-caption break-words text-muted-foreground">{detail}</p>
      )}
    </div>
  );
}

type LabelMap = Record<string, string>;

/** A server key we have no translation for renders as itself, never blank. */
function labelFor(map: LabelMap, key: string): string {
  return map[key] ?? key;
}

/** "5 labels · 3 views · 2 agents" — the biggest kinds first, capped at four. */
function countsSummary(counts: Record<string, number>, kinds: LabelMap): string {
  return Object.entries(counts)
    .filter(([, n]) => n > 0)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 4)
    .map(([kind, n]) => `${n} ${labelFor(kinds, kind)}`)
    .join(" · ");
}

export function PacksTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const canManage = role === "owner" || role === "admin";
  const kinds = t(($) => $.packs.kinds, { returnObjects: true }) as LabelMap;
  const domainLabels = t(($) => $.packs.domains, { returnObjects: true }) as LabelMap;

  const catalogue = useQuery(packCatalogueOptions(wsId));
  const installs = useQuery(packInstallsOptions(wsId));

  const [domain, setDomain] = useState(ALL_DOMAINS);
  const [openPackId, setOpenPackId] = useState("");

  const packs = catalogue.data?.packs ?? [];
  const domains = catalogue.data?.domains ?? [];
  const visible =
    domain === ALL_DOMAINS ? packs : packs.filter((p) => p.manifest.domain === domain);

  return (
    <SettingsTab
      title={t(($) => $.packs.title)}
      description={t(($) => $.packs.description)}
    >
      <SettingsSection
        title={
          <span className="inline-flex items-center gap-2">
            <Blocks className="h-4 w-4 text-muted-foreground" />
            {t(($) => $.packs.catalogue.section)}
          </span>
        }
        description={t(($) => $.packs.catalogue.description)}
      >
        {!canManage && (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.packs.read_only)}
          </p>
        )}
        {domains.length > 0 && (
          <div
            role="group"
            aria-label={t(($) => $.packs.catalogue.filter_label)}
            className="flex flex-wrap gap-1.5"
          >
            {[ALL_DOMAINS, ...domains].map((key) => (
              <button
                key={key}
                type="button"
                aria-pressed={domain === key}
                onClick={() => setDomain(key)}
                className={cn(
                  "rounded-full border px-3 py-1 text-caption transition-colors",
                  domain === key
                    ? "border-foreground bg-foreground font-medium text-background"
                    : "text-muted-foreground hover:bg-muted/50",
                )}
              >
                {key === ALL_DOMAINS
                  ? t(($) => $.packs.catalogue.all_domains)
                  : labelFor(domainLabels, key)}
              </button>
            ))}
          </div>
        )}
        {catalogue.isPending ? (
          <p role="status" className="text-caption text-muted-foreground">
            {t(($) => $.packs.catalogue.loading)}
          </p>
        ) : catalogue.isError ? (
          <div className="flex flex-col items-start gap-2">
            <p role="alert" className="text-caption text-destructive">
              {t(($) => $.packs.catalogue.failed)}
            </p>
            <Button variant="outline" size="sm" onClick={() => void catalogue.refetch()}>
              {t(($) => $.packs.catalogue.retry)}
            </Button>
          </div>
        ) : visible.length === 0 ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.packs.catalogue.empty)}
          </p>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            {visible.map((pack) => (
              <PackCard
                key={pack.manifest.id}
                pack={pack}
                kinds={kinds}
                domainLabels={domainLabels}
                onOpen={() => setOpenPackId(pack.manifest.id)}
              />
            ))}
          </div>
        )}
      </SettingsSection>

      <InstalledSection
        wsId={wsId}
        canManage={canManage}
        kinds={kinds}
        installs={installs.data ?? []}
        isPending={installs.isPending}
        onUpdate={(packId) => setOpenPackId(packId)}
      />

      <UploadSection wsId={wsId} canManage={canManage} kinds={kinds} />

      <ExportSection canManage={canManage} domains={domains} domainLabels={domainLabels} />

      {openPackId && (
        <PackDetailDialog
          wsId={wsId}
          packId={openPackId}
          canManage={canManage}
          kinds={kinds}
          domainLabels={domainLabels}
          onClose={() => setOpenPackId("")}
        />
      )}
    </SettingsTab>
  );
}

function PackCard({
  pack,
  kinds,
  domainLabels,
  onOpen,
}: {
  pack: PackSummary;
  kinds: LabelMap;
  domainLabels: LabelMap;
  onOpen: () => void;
}) {
  const { t } = useT("settings");
  const { manifest } = pack;
  const summary = countsSummary(pack.counts, kinds);
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex flex-col items-start gap-2 rounded-xl border p-4 text-left transition-colors hover:border-foreground/30 hover:bg-muted/20 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <div className="flex w-full items-start justify-between gap-3">
        <h4 className="min-w-0 text-title-sm font-semibold">{manifest.title}</h4>
        {pack.upgrade_available ? (
          <Badge>{t(($) => $.packs.card.update_to, { version: manifest.version })}</Badge>
        ) : pack.installed_version ? (
          <Badge variant="secondary">
            {t(($) => $.packs.card.installed, { version: pack.installed_version })}
          </Badge>
        ) : null}
      </div>
      <p className="text-caption leading-relaxed text-muted-foreground">{manifest.summary}</p>
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge variant="outline">{labelFor(domainLabels, manifest.domain)}</Badge>
        <Badge variant="outline">{t(($) => $.packs.card.wave, { wave: manifest.wave })}</Badge>
        {manifest.works_without_agents === true && (
          <Badge variant="outline">{t(($) => $.packs.card.works_without_agents)}</Badge>
        )}
      </div>
      {summary && <p className="text-caption text-muted-foreground">{summary}</p>}
      {manifest.metric.label && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.packs.card.metric, { label: manifest.metric.label })}
        </p>
      )}
    </button>
  );
}

/** Contents / items / report counts, grouped by kind with translated headings. */
function KindList({ contents, kinds }: { contents: PackContents; kinds: LabelMap }) {
  const entries = Object.entries(contents).filter(([, names]) => names.length > 0);
  if (entries.length === 0) return null;
  return (
    <ul className="flex max-h-64 flex-col gap-1.5 overflow-y-auto text-caption">
      {entries.map(([kind, names]) => (
        <li key={kind}>
          <span className="font-medium">{labelFor(kinds, kind)}</span>
          <span className="text-muted-foreground">{` · ${names.join(", ")}`}</span>
        </li>
      ))}
    </ul>
  );
}

function CountRow({
  counts,
  kinds,
  label,
}: {
  counts: Record<string, number>;
  kinds: LabelMap;
  label: string;
}) {
  const total = packCountTotal(counts);
  if (total === 0) return null;
  return (
    <p className="text-caption">
      <span className="font-medium">{label}</span>
      <span className="text-muted-foreground">
        {` · ${Object.entries(counts)
          .filter(([, n]) => n > 0)
          .map(([kind, n]) => `${n} ${labelFor(kinds, kind)}`)
          .join(", ")}`}
      </span>
    </p>
  );
}

function ReportPanel({ report, kinds }: { report: PackReport; kinds: LabelMap }) {
  const { t } = useT("settings");
  return (
    <div
      data-testid="pack-report"
      className="flex flex-col gap-1.5 rounded-lg border border-success/30 bg-success/5 p-3"
    >
      <p className="text-body font-semibold">{t(($) => $.packs.report.title)}</p>
      <CountRow counts={report.created} kinds={kinds} label={t(($) => $.packs.report.created)} />
      <CountRow counts={report.merged} kinds={kinds} label={t(($) => $.packs.report.merged)} />
      {report.skipped.length > 0 && (
        <p className="text-caption">
          <span className="font-medium">{t(($) => $.packs.report.skipped)}</span>
          <span className="text-muted-foreground">
            {` · ${report.skipped.map((c) => `${labelFor(kinds, c.kind)} ${c.name}`).join(", ")}`}
          </span>
        </p>
      )}
      {report.warnings.map((w, i) => (
        <p key={i} className="text-caption text-warning">
          {w}
        </p>
      ))}
    </div>
  );
}

/**
 * The preview step, shared by the catalogue detail and the upload panel:
 * collisions, problems, the strategy select and the install button. `blocked`
 * is the server's own reason (same version installed, or a downgrade) and is
 * the only thing that disables install for a reason the user can read.
 */
function PreviewPanel({
  preview,
  strategy,
  onStrategyChange,
  onInstall,
  installing,
  canManage,
  kinds,
  installLabel,
}: {
  preview: PackPreview;
  strategy: PackStrategy;
  onStrategyChange: (next: PackStrategy) => void;
  onInstall: () => void;
  installing: boolean;
  canManage: boolean;
  kinds: LabelMap;
  installLabel: string;
}) {
  const { t } = useT("settings");
  const strategies = (
    preview.strategies.length > 0 ? preview.strategies : [...PACK_STRATEGIES]
  ) as PackStrategy[];
  const blocked = preview.blocked.length > 0;
  return (
    <div data-testid="pack-preview" className="flex flex-col gap-3 rounded-lg bg-muted/40 p-3">
      <p className="text-body font-semibold">{t(($) => $.packs.preview.title)}</p>
      {preview.collisions.length > 0 ? (
        <div className="flex flex-col gap-1">
          <p className="text-caption font-medium">
            {t(($) => $.packs.preview.collisions, { count: preview.collisions.length })}
          </p>
          <ul className="max-h-48 overflow-y-auto text-caption text-muted-foreground">
            {preview.collisions.map((c) => (
              <li key={`${c.kind}:${c.name}`}>{`${labelFor(kinds, c.kind)} · ${c.name}`}</li>
            ))}
          </ul>
        </div>
      ) : (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.packs.preview.no_collisions)}
        </p>
      )}
      {preview.problems.length > 0 && (
        <ul role="alert" className="text-caption text-destructive">
          {preview.problems.map((p) => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      )}
      <div className="flex flex-col gap-1">
        <Label htmlFor="pack-strategy" className="text-caption">
          {t(($) => $.packs.preview.strategy)}
        </Label>
        <select
          id="pack-strategy"
          className={SELECT_CLASS}
          value={strategy}
          disabled={!canManage || installing}
          onChange={(e) => onStrategyChange(e.target.value as PackStrategy)}
        >
          {strategies.map((s) => (
            <option key={s} value={s}>
              {labelFor(
                t(($) => $.packs.strategies, { returnObjects: true }) as LabelMap,
                s,
              )}
            </option>
          ))}
        </select>
        <p className="text-caption text-muted-foreground">
          {labelFor(
            t(($) => $.packs.strategy_help, { returnObjects: true }) as LabelMap,
            strategy,
          )}
        </p>
      </div>
      {blocked && (
        <p role="alert" className="text-caption text-warning">
          {preview.blocked}
        </p>
      )}
      <div>
        <Button
          type="button"
          size="sm"
          disabled={!canManage || installing || blocked || preview.problems.length > 0}
          onClick={onInstall}
        >
          {installing ? t(($) => $.packs.preview.installing) : installLabel}
        </Button>
      </div>
    </div>
  );
}

function PackDetailDialog({
  wsId,
  packId,
  canManage,
  kinds,
  domainLabels,
  onClose,
}: {
  wsId: string;
  packId: string;
  canManage: boolean;
  kinds: LabelMap;
  domainLabels: LabelMap;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const detail = useQuery(packDetailOptions(wsId, packId));
  const preview = usePreviewPack();
  const install = useInstallPack(wsId);
  const download = useDownloadPack();
  const [strategy, setStrategy] = useState<PackStrategy | "">("");
  const statusLabels = t(($) => $.packs.prerequisite_status, {
    returnObjects: true,
  }) as LabelMap;

  const pack = detail.data?.pack;
  const previewData = preview.data ?? null;
  const effectiveStrategy = (strategy || previewData?.strategy || "skip") as PackStrategy;

  const runPreview = () => {
    setStrategy("");
    install.reset();
    preview.mutate(
      { id: packId },
      {
        onError: (error) =>
          toast.error(t(($) => $.packs.preview.failed), { description: errorDetail(error) }),
      },
    );
  };

  const runInstall = () => {
    install.mutate(
      { id: packId, strategy: effectiveStrategy },
      {
        onSuccess: () => toast.success(t(($) => $.packs.install_done)),
        onError: (error) =>
          toast.error(t(($) => $.packs.install_failed), { description: errorDetail(error) }),
      },
    );
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{pack?.manifest.title || packId}</DialogTitle>
          <DialogDescription>{pack?.manifest.summary ?? ""}</DialogDescription>
        </DialogHeader>
        {detail.isPending ? (
          <p role="status" className="text-caption text-muted-foreground">
            {t(($) => $.packs.catalogue.loading)}
          </p>
        ) : !pack ? (
          <p role="alert" className="text-caption text-destructive">
            {t(($) => $.packs.detail.failed)}
          </p>
        ) : (
          <div className="flex flex-col gap-5">
            <div className="flex flex-wrap items-center gap-1.5">
              <Badge variant="outline">{labelFor(domainLabels, pack.manifest.domain)}</Badge>
              <Badge variant="outline">
                {t(($) => $.packs.card.wave, { wave: pack.manifest.wave })}
              </Badge>
              <Badge variant="secondary">
                {t(($) => $.packs.detail.version, { version: pack.manifest.version })}
              </Badge>
              {pack.manifest.works_without_agents === true && (
                <Badge variant="outline">{t(($) => $.packs.card.works_without_agents)}</Badge>
              )}
            </div>

            {pack.manifest.description && (
              <Markdown mode="minimal">{pack.manifest.description}</Markdown>
            )}

            {pack.manifest.metric.label && (
              <section className="flex flex-col gap-1">
                <h4 className="text-body font-semibold">{t(($) => $.packs.detail.metric)}</h4>
                <p className="text-caption">{pack.manifest.metric.label}</p>
                {pack.manifest.metric.description && (
                  <p className="text-caption text-muted-foreground">
                    {pack.manifest.metric.description}
                  </p>
                )}
                {pack.manifest.metric.hint && (
                  <p className="text-caption text-muted-foreground">{pack.manifest.metric.hint}</p>
                )}
              </section>
            )}

            {pack.prerequisites.length > 0 && (
              <section className="flex flex-col gap-1.5">
                <h4 className="text-body font-semibold">
                  {t(($) => $.packs.detail.prerequisites)}
                </h4>
                <ul className="flex flex-col gap-1.5">
                  {pack.prerequisites.map((p) => (
                    <li key={`${p.kind}:${p.name}`} className="flex flex-wrap items-center gap-2">
                      <span
                        className={cn(
                          "rounded-full px-2 py-0.5 text-caption",
                          PREREQUISITE_TONE[p.status] ?? PREREQUISITE_TONE.unknown,
                        )}
                      >
                        {labelFor(statusLabels, p.status)}
                      </span>
                      <span className="text-caption font-medium">{p.name}</span>
                      {p.optional === true && (
                        <span className="text-caption text-muted-foreground">
                          {t(($) => $.packs.detail.optional)}
                        </span>
                      )}
                      {p.note && (
                        <span className="text-caption text-muted-foreground">{p.note}</span>
                      )}
                    </li>
                  ))}
                </ul>
              </section>
            )}

            <section className="flex flex-col gap-1.5">
              <h4 className="text-body font-semibold">{t(($) => $.packs.detail.contents)}</h4>
              <KindList contents={detail.data?.contents ?? {}} kinds={kinds} />
            </section>

            {pack.manifest.changelog.length > 0 && (
              <section className="flex flex-col gap-1">
                <h4 className="text-body font-semibold">{t(($) => $.packs.detail.changelog)}</h4>
                <ul className="text-caption text-muted-foreground">
                  {pack.manifest.changelog.map((row) => (
                    <li key={row.version}>{`${row.version} · ${row.note}`}</li>
                  ))}
                </ul>
              </section>
            )}

            <p className="text-caption text-muted-foreground">
              {t(($) => $.packs.detail.author_license, {
                author: pack.manifest.author || "—",
                license: pack.manifest.license || "—",
              })}
            </p>

            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={download.isPending}
                onClick={() =>
                  download.mutate(packId, {
                    onError: (error) =>
                      toast.error(t(($) => $.packs.detail.download_failed), {
                        description: errorDetail(error),
                      }),
                  })
                }
              >
                <Download className="h-4 w-4" />
                {t(($) => $.packs.detail.download)}
              </Button>
              {canManage && !install.isSuccess && (
                <Button
                  type="button"
                  size="sm"
                  disabled={preview.isPending}
                  onClick={runPreview}
                >
                  {preview.isPending
                    ? t(($) => $.packs.preview.running)
                    : t(($) => $.packs.preview.start)}
                </Button>
              )}
            </div>

            {previewData && !install.isSuccess && (
              <PreviewPanel
                preview={previewData}
                strategy={effectiveStrategy}
                onStrategyChange={setStrategy}
                onInstall={runInstall}
                installing={install.isPending}
                canManage={canManage}
                kinds={kinds}
                installLabel={
                  pack.upgrade_available
                    ? t(($) => $.packs.preview.update)
                    : t(($) => $.packs.preview.install)
                }
              />
            )}
            {install.isSuccess && <ReportPanel report={install.data.report} kinds={kinds} />}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function InstalledSection({
  wsId,
  canManage,
  kinds,
  installs,
  isPending,
  onUpdate,
}: {
  wsId: string;
  canManage: boolean;
  kinds: LabelMap;
  installs: PackInstall[];
  isPending: boolean;
  onUpdate: (packId: string) => void;
}) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const uninstall = useUninstallPack(wsId);
  const [expanded, setExpanded] = useState("");
  const [confirming, setConfirming] = useState<PackInstall | null>(null);
  const [removed, setRemoved] = useState<{ id: string; report: PackUninstallReport } | null>(null);
  const sourceLabels = t(($) => $.packs.sources, { returnObjects: true }) as LabelMap;
  const statusLabels = t(($) => $.packs.install_status, { returnObjects: true }) as LabelMap;

  const runUninstall = (install: PackInstall) => {
    setConfirming(null);
    uninstall.mutate(
      { id: install.id },
      {
        onSuccess: (result) => {
          setRemoved({ id: install.id, report: result.report });
          toast.success(t(($) => $.packs.installed.uninstall_done));
        },
        onError: (error) =>
          toast.error(t(($) => $.packs.installed.uninstall_failed), {
            description: errorDetail(error),
          }),
      },
    );
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Package className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.packs.installed.section)}
        </span>
      }
      description={t(($) => $.packs.installed.description)}
    >
      <SettingsCard>
        {isPending ? (
          <div className="px-4 py-3.5">
            <p role="status" className="text-caption text-muted-foreground">
              {t(($) => $.packs.catalogue.loading)}
            </p>
          </div>
        ) : installs.length === 0 ? (
          <div className="px-4 py-3.5">
            <p className="text-caption text-muted-foreground">
              {t(($) => $.packs.installed.empty)}
            </p>
          </div>
        ) : (
          installs.map((install) => (
            <div key={install.id} className="flex flex-col gap-2 px-4 py-3.5">
              <div className="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  aria-expanded={expanded === install.id}
                  onClick={() => setExpanded((id) => (id === install.id ? "" : install.id))}
                  className="text-body font-medium hover:underline"
                >
                  {install.title || install.pack_id}
                </button>
                <Badge variant="secondary">{install.pack_version}</Badge>
                <Badge variant="outline">{labelFor(sourceLabels, install.source)}</Badge>
                <span
                  className={cn(
                    "rounded-full px-2 py-0.5 text-caption",
                    INSTALL_STATUS_TONE[install.status] ?? INSTALL_STATUS_TONE.removed,
                  )}
                >
                  {labelFor(statusLabels, install.status)}
                </span>
                <span className="text-caption text-muted-foreground">
                  {t(($) => $.packs.installed.items, { count: install.item_count })}
                </span>
                <span className="text-caption text-muted-foreground">
                  {timeAgo(install.installed_at)}
                </span>
              </div>
              {install.metric.label && (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.packs.card.metric, { label: install.metric.label })}
                </p>
              )}
              {canManage && install.status === "installed" && (
                <div className="flex flex-wrap gap-2">
                  {install.upgrade_to && (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() => onUpdate(install.pack_id)}
                    >
                      {t(($) => $.packs.installed.update, { version: install.upgrade_to })}
                    </Button>
                  )}
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={uninstall.isPending}
                    onClick={() => setConfirming(install)}
                  >
                    <Trash2 className="h-4 w-4" />
                    {t(($) => $.packs.installed.uninstall)}
                  </Button>
                </div>
              )}
              {expanded === install.id && (
                <InstallItems wsId={wsId} installId={install.id} kinds={kinds} />
              )}
              {removed?.id === install.id && (
                <div
                  data-testid="pack-uninstall-report"
                  className="flex flex-col gap-1.5 rounded-lg border bg-muted/40 p-3"
                >
                  <p className="text-body font-semibold">
                    {t(($) => $.packs.installed.uninstall_report)}
                  </p>
                  <CountRow
                    counts={removed.report.removed}
                    kinds={kinds}
                    label={t(($) => $.packs.installed.removed)}
                  />
                  {removed.report.kept.length > 0 && (
                    <p className="text-caption">
                      <span className="font-medium">{t(($) => $.packs.installed.kept)}</span>
                      <span className="text-muted-foreground">
                        {` · ${removed.report.kept
                          .map((it) => `${labelFor(kinds, it.kind)} ${it.name}`)
                          .join(", ")}`}
                      </span>
                    </p>
                  )}
                  {removed.report.reasons.map((reason) => (
                    <p key={reason} className="text-caption text-muted-foreground">
                      {reason}
                    </p>
                  ))}
                </div>
              )}
            </div>
          ))
        )}
      </SettingsCard>

      <AlertDialog open={confirming !== null} onOpenChange={(open) => !open && setConfirming(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.packs.installed.confirm_title, { title: confirming?.title ?? "" })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.packs.installed.confirm_body)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.packs.installed.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={() => confirming && runUninstall(confirming)}>
              {t(($) => $.packs.installed.confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}

/** An install's ledger rows, grouped by kind. Mounted only while expanded. */
function InstallItems({
  wsId,
  installId,
  kinds,
}: {
  wsId: string;
  installId: string;
  kinds: LabelMap;
}) {
  const { t } = useT("settings");
  const detail = useQuery(packInstallOptions(wsId, installId));
  if (detail.isPending) {
    return (
      <p role="status" className="text-caption text-muted-foreground">
        {t(($) => $.packs.catalogue.loading)}
      </p>
    );
  }
  const grouped: PackContents = {};
  for (const item of detail.data?.items ?? []) {
    (grouped[item.kind] ??= []).push(item.name);
  }
  if (Object.keys(grouped).length === 0) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.packs.installed.no_items)}
      </p>
    );
  }
  return (
    <div data-testid="pack-install-items">
      <KindList contents={grouped} kinds={kinds} />
    </div>
  );
}

function UploadSection({
  wsId,
  canManage,
  kinds,
}: {
  wsId: string;
  canManage: boolean;
  kinds: LabelMap;
}) {
  const { t } = useT("settings");
  const preview = usePreviewPackUpload();
  const install = useInstallPackUpload(wsId);
  const [file, setFile] = useState<File | null>(null);
  const [strategy, setStrategy] = useState<PackStrategy | "">("");

  const previewData = preview.data ?? null;
  const effectiveStrategy = (strategy || previewData?.strategy || "skip") as PackStrategy;
  // Derived from the file already in state; the message below is the whole
  // handling, so there is no second piece of state to keep in step.
  const oversize = file !== null && file.size > UPLOAD_MAX_BYTES;

  const pickFile = (event: ChangeEvent<HTMLInputElement>) => {
    const next = event.target.files?.[0] ?? null;
    setFile(next);
    setStrategy("");
    preview.reset();
    install.reset();
    if (!next || next.size > UPLOAD_MAX_BYTES) return;
    preview.mutate(
      { file: next },
      {
        onError: (error) =>
          toast.error(t(($) => $.packs.preview.failed), { description: errorDetail(error) }),
      },
    );
  };

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Upload className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.packs.upload.section)}
        </span>
      }
      description={t(($) => $.packs.upload.description)}
    >
      <SettingsCard>
        <SettingsRow
          label={t(($) => $.packs.upload.file_label)}
          description={t(($) => $.packs.upload.file_description)}
          align="start"
        >
          <div data-testid="pack-upload" className="flex flex-col gap-3">
            <input
              type="file"
              accept=".yaml,.yml"
              aria-label={t(($) => $.packs.upload.file_label)}
              disabled={!canManage || preview.isPending || install.isPending}
              onChange={pickFile}
              className="text-caption"
            />
            {oversize && (
              <p role="alert" className="text-caption text-destructive">
                {t(($) => $.packs.upload.too_large, { max: UPLOAD_MAX_LABEL })}
              </p>
            )}
            {preview.isPending && (
              <p role="status" className="text-caption text-muted-foreground">
                {t(($) => $.packs.preview.running)}
              </p>
            )}
            {/* A toast is gone in four seconds; the reason a preview or an
                install failed has to stay under the drop zone. */}
            {preview.isError && (
              <ErrorLines
                label={t(($) => $.packs.preview.failed)}
                detail={errorDetail(preview.error)}
              />
            )}
            {install.isError && (
              <ErrorLines
                label={t(($) => $.packs.install_failed)}
                detail={errorDetail(install.error)}
              />
            )}
            {previewData && !install.isSuccess && (
              <>
                <p className="text-caption font-medium">
                  {previewData.pack.manifest.title || file?.name}
                </p>
                <PreviewPanel
                  preview={previewData}
                  strategy={effectiveStrategy}
                  onStrategyChange={setStrategy}
                  onInstall={() =>
                    file &&
                    install.mutate(
                      { file, strategy: effectiveStrategy },
                      {
                        onSuccess: () => toast.success(t(($) => $.packs.install_done)),
                        onError: (error) =>
                          toast.error(t(($) => $.packs.install_failed), {
                            description: errorDetail(error),
                          }),
                      },
                    )
                  }
                  installing={install.isPending}
                  canManage={canManage}
                  kinds={kinds}
                  installLabel={t(($) => $.packs.preview.install)}
                />
              </>
            )}
            {install.isSuccess && <ReportPanel report={install.data.report} kinds={kinds} />}
          </div>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}

function ExportSection({
  canManage,
  domains,
  domainLabels,
}: {
  canManage: boolean;
  domains: string[];
  domainLabels: LabelMap;
}) {
  const { t } = useT("settings");
  const exportPack = useExportPack();
  const [id, setId] = useState("");
  const [version, setVersion] = useState("1.0.0");
  const [title, setTitle] = useState("");
  const [summary, setSummary] = useState("");
  const [domain, setDomain] = useState("other");
  const [metricLabel, setMetricLabel] = useState("");
  const [metricDescription, setMetricDescription] = useState("");
  const [includeIssues, setIncludeIssues] = useState(false);
  const [includeNotes, setIncludeNotes] = useState(false);
  // Matches the server default. Skills discovered on a connected computer
  // (origin `runtime_local`) are dropped there whatever this says.
  const [includeSkills, setIncludeSkills] = useState(true);

  // The server validates the manifest and answers 400 with the exact reason,
  // so this only gates on the fields it always rejects when empty.
  const canExport =
    canManage &&
    id.trim().length > 0 &&
    version.trim().length > 0 &&
    title.trim().length > 0 &&
    summary.trim().length > 0;

  // A select whose value is not one of its options shows the first option
  // while submitting something else; the catalogue's domain list arrives
  // asynchronously, so the selected value is always resolved against it.
  const options = domains.length > 0 ? domains : ["other"];
  const selectedDomain = options.includes(domain) ? domain : (options[0] ?? "other");

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Download className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.packs.export.section)}
        </span>
      }
      description={t(($) => $.packs.export.description)}
    >
      <SettingsCard>
        <SettingsRow label={t(($) => $.packs.export.manifest)} align="start">
          <div data-testid="pack-export" className="flex flex-col gap-2">
            <Input
              aria-label={t(($) => $.packs.export.id)}
              placeholder={t(($) => $.packs.export.id)}
              value={id}
              disabled={!canManage}
              onChange={(e) => setId(e.target.value)}
            />
            <Input
              aria-label={t(($) => $.packs.export.version)}
              placeholder={t(($) => $.packs.export.version)}
              value={version}
              disabled={!canManage}
              onChange={(e) => setVersion(e.target.value)}
            />
            <Input
              aria-label={t(($) => $.packs.export.pack_title)}
              placeholder={t(($) => $.packs.export.pack_title)}
              value={title}
              disabled={!canManage}
              onChange={(e) => setTitle(e.target.value)}
            />
            <Textarea
              aria-label={t(($) => $.packs.export.summary)}
              placeholder={t(($) => $.packs.export.summary)}
              value={summary}
              disabled={!canManage}
              onChange={(e) => setSummary(e.target.value)}
            />
            <select
              aria-label={t(($) => $.packs.export.domain)}
              className={SELECT_CLASS}
              value={selectedDomain}
              disabled={!canManage}
              onChange={(e) => setDomain(e.target.value)}
            >
              {options.map((key) => (
                <option key={key} value={key}>
                  {labelFor(domainLabels, key)}
                </option>
              ))}
            </select>
            <Input
              aria-label={t(($) => $.packs.export.metric_label)}
              placeholder={t(($) => $.packs.export.metric_label)}
              value={metricLabel}
              disabled={!canManage}
              onChange={(e) => setMetricLabel(e.target.value)}
            />
            <Input
              aria-label={t(($) => $.packs.export.metric_description)}
              placeholder={t(($) => $.packs.export.metric_description)}
              value={metricDescription}
              disabled={!canManage}
              onChange={(e) => setMetricDescription(e.target.value)}
            />
            <label className="flex items-center gap-2 text-caption">
              <input
                type="checkbox"
                checked={includeIssues}
                disabled={!canManage}
                onChange={(e) => setIncludeIssues(e.target.checked)}
              />
              {t(($) => $.packs.export.include_issues)}
            </label>
            <label className="flex items-center gap-2 text-caption">
              <input
                type="checkbox"
                checked={includeNotes}
                disabled={!canManage}
                onChange={(e) => setIncludeNotes(e.target.checked)}
              />
              {t(($) => $.packs.export.include_notes)}
            </label>
            <label className="flex items-center gap-2 text-caption">
              <input
                type="checkbox"
                checked={includeSkills}
                disabled={!canManage}
                onChange={(e) => setIncludeSkills(e.target.checked)}
              />
              {t(($) => $.packs.export.include_skills)}
            </label>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.packs.export.skills_note)}
            </p>
            <div>
              <Button
                type="button"
                size="sm"
                disabled={!canExport || exportPack.isPending}
                onClick={() =>
                  exportPack.mutate(
                    {
                      manifest: {
                        id: id.trim(),
                        version: version.trim(),
                        title: title.trim(),
                        summary: summary.trim(),
                        domain: selectedDomain,
                        metric: {
                          label: metricLabel.trim(),
                          description: metricDescription.trim(),
                        },
                      },
                      include_issues: includeIssues,
                      include_notes: includeNotes,
                      include_skills: includeSkills,
                    },
                    {
                      onSuccess: () => toast.success(t(($) => $.packs.export.done)),
                      onError: (error) =>
                        toast.error(t(($) => $.packs.export.failed), {
                          description: errorDetail(error),
                        }),
                    },
                  )
                }
              >
                {exportPack.isPending ? (
                  t(($) => $.packs.export.exporting)
                ) : (
                  <>
                    <Check className="h-4 w-4" />
                    {t(($) => $.packs.export.button)}
                  </>
                )}
              </Button>
            </div>
          </div>
        </SettingsRow>
      </SettingsCard>
    </SettingsSection>
  );
}
