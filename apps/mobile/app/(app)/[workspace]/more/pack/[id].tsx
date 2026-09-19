/**
 * Pack detail — read it, preview the install, install or update it
 * (OS plan, vague B).
 *
 * Parity target is web's `PackDetailDialog`
 * (`packages/views/settings/components/packs-tab.tsx`): same sections in the
 * same order (badges, description, metric, prerequisites, contents,
 * changelog, author/license), same two-step install (preview then apply),
 * same disable rules — a manager only, nothing while `blocked` is set by the
 * server, nothing while the preview reports problems.
 *
 * The strategy defaults to the server's own pick (`skip` on a first install,
 * `merge` on an upgrade) and the choice list comes from the preview rather
 * than from a hardcoded enum, so a server that adds one still offers it.
 *
 * Downloading the pack.yaml stays on web/desktop — a phone has nowhere
 * useful to put it.
 */
import { useState } from "react";
import { ActivityIndicator, Alert, ScrollView, View } from "react-native";
import { useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { PackKindList } from "@/components/packs/kind-list";
import { Markdown } from "@/lib/markdown";
import { packDetailOptions } from "@/data/queries/packs";
import { useInstallPack, usePreviewPack } from "@/data/mutations/packs";
import type {
  PackPreview,
  PackReport,
  PackStrategy,
  PackSummary,
} from "@/data/schemas";
import { PACK_STRATEGIES } from "@/data/schemas";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  packCountsDetail,
  packDomainLabel,
  packKindLabel,
  packPrerequisiteStatusLabel,
  packStrategyHelp,
  packStrategyLabel,
} from "@/lib/packs-display";
import { useIsWorkspaceManager } from "@/lib/use-is-workspace-manager";
import { cn } from "@/lib/utils";

export default function PackDetailScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const canManage = useIsWorkspaceManager();

  const { data, isLoading, error, refetch } = useQuery(
    packDetailOptions(wsId, id ?? ""),
  );
  const preview = usePreviewPack();
  const install = useInstallPack();

  // Empty until the user picks one; the server's own pick is the default.
  const [strategy, setStrategy] = useState<PackStrategy | "">("");
  const previewData = preview.data ?? null;
  const effectiveStrategy = (strategy ||
    previewData?.strategy ||
    "skip") as PackStrategy;

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  // `getPack` falls back to an empty detail on a shape mismatch, so a blank
  // id covers both "not found" and "response we could not trust".
  if (error || !data || !data.pack.manifest.id) {
    return (
      <View className="flex-1 gap-3 bg-background px-4 pt-4">
        <Text className="text-sm text-destructive">
          Could not load this pack
          {error instanceof Error ? `: ${error.message}` : "."}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  const pack: PackSummary = data.pack;
  const { manifest } = pack;
  const contents = Object.entries(data.contents ?? {});

  const runPreview = () => {
    setStrategy("");
    install.reset();
    preview.mutate(
      { id: manifest.id },
      {
        onError: (err) =>
          Alert.alert(
            "Could not preview the pack",
            err instanceof Error ? err.message : "unknown error",
          ),
      },
    );
  };

  const runInstall = () => {
    install.mutate(
      { id: manifest.id, strategy: effectiveStrategy },
      {
        onError: (err) =>
          Alert.alert(
            "Could not install the pack",
            err instanceof Error ? err.message : "unknown error",
          ),
      },
    );
  };

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="gap-5 px-4 pb-12 pt-4"
    >
      <View className="gap-2">
        <Text className="text-lg font-semibold text-foreground">
          {manifest.title || manifest.id}
        </Text>
        {manifest.summary ? (
          <Text className="text-sm leading-5 text-muted-foreground">
            {manifest.summary}
          </Text>
        ) : null}
        <View className="flex-row flex-wrap items-center gap-1">
          <Chip>{packDomainLabel(manifest.domain)}</Chip>
          <Chip>{`Wave ${manifest.wave}`}</Chip>
          <Chip>{`Version ${manifest.version}`}</Chip>
          {manifest.works_without_agents === true ? (
            <Chip>No agent needed</Chip>
          ) : null}
          {pack.upgrade_available === true ? (
            <Chip tone="warning">{`Update from ${pack.installed_version ?? "—"}`}</Chip>
          ) : pack.installed_version ? (
            <Chip>{`Installed ${pack.installed_version}`}</Chip>
          ) : null}
        </View>
      </View>

      {manifest.description ? (
        <Markdown content={manifest.description} />
      ) : null}

      {manifest.metric.label ? (
        <Section title="The one metric to watch">
          <Text className="text-sm text-foreground">
            {manifest.metric.label}
          </Text>
          {manifest.metric.description ? (
            <Text className="text-xs leading-5 text-muted-foreground">
              {manifest.metric.description}
            </Text>
          ) : null}
          {manifest.metric.hint ? (
            <Text className="text-xs leading-5 text-muted-foreground">
              {manifest.metric.hint}
            </Text>
          ) : null}
        </Section>
      ) : null}

      {pack.prerequisites.length > 0 ? (
        <Section title="Prerequisites">
          {pack.prerequisites.map((p) => (
            <View
              key={`${p.kind}:${p.name}`}
              className="flex-row flex-wrap items-center gap-1.5"
            >
              <View
                className={cn(
                  "rounded px-1.5 py-0.5",
                  p.status === "met"
                    ? "bg-success/15"
                    : p.status === "missing"
                      ? "bg-warning/15"
                      : "bg-secondary",
                )}
              >
                <Text className="text-xs text-foreground">
                  {packPrerequisiteStatusLabel(p.status)}
                </Text>
              </View>
              <Text className="text-xs font-medium text-foreground">
                {p.name}
              </Text>
              {p.optional === true ? (
                <Text className="text-xs text-muted-foreground">optional</Text>
              ) : null}
              {p.note ? (
                <Text className="text-xs text-muted-foreground">{p.note}</Text>
              ) : null}
            </View>
          ))}
        </Section>
      ) : null}

      <Section title="What it installs">
        {contents.length > 0 ? (
          <PackKindList entries={contents} />
        ) : (
          <Text className="text-xs text-muted-foreground">
            This pack declares no content.
          </Text>
        )}
      </Section>

      {manifest.changelog.length > 0 ? (
        <Section title="Changelog">
          {manifest.changelog.map((row) => (
            <Text
              key={row.version}
              className="text-xs leading-5 text-muted-foreground"
            >
              {`${row.version} · ${row.note}`}
            </Text>
          ))}
        </Section>
      ) : null}

      <Text className="text-xs text-muted-foreground">
        {`By ${manifest.author || "—"} · ${manifest.license || "—"}`}
      </Text>

      {canManage && !install.isSuccess ? (
        <Button
          variant={previewData ? "outline" : "default"}
          disabled={preview.isPending}
          onPress={runPreview}
        >
          <Text>
            {preview.isPending
              ? "Reading the pack"
              : previewData
                ? "Preview again"
                : "Preview the install"}
          </Text>
        </Button>
      ) : null}

      {!canManage ? (
        <Text className="text-xs text-muted-foreground">
          Only owners and admins can install, update or remove a pack.
        </Text>
      ) : null}

      {previewData && !install.isSuccess ? (
        <PreviewPanel
          preview={previewData}
          strategy={effectiveStrategy}
          onStrategyChange={setStrategy}
          onInstall={runInstall}
          installing={install.isPending}
          installLabel={pack.upgrade_available === true ? "Update" : "Install"}
        />
      ) : null}

      {install.isSuccess ? <ReportPanel report={install.data.report} /> : null}
    </ScrollView>
  );
}

/**
 * The preview step. `blocked` is the server's own reason (same version
 * already installed, or a downgrade) and is the only thing that disables
 * install with a sentence the user can read; `problems` block it too, as on
 * web, because installing past a problem is how a half-applied bundle
 * happens.
 */
function PreviewPanel({
  preview,
  strategy,
  onStrategyChange,
  onInstall,
  installing,
  installLabel,
}: {
  preview: PackPreview;
  strategy: PackStrategy;
  onStrategyChange: (next: PackStrategy) => void;
  onInstall: () => void;
  installing: boolean;
  installLabel: string;
}) {
  const strategies = (
    preview.strategies.length > 0 ? preview.strategies : [...PACK_STRATEGIES]
  ) as PackStrategy[];
  const blocked = preview.blocked.length > 0;

  return (
    <View className="gap-3 rounded-md bg-secondary/60 p-3">
      <Text className="text-sm font-semibold text-foreground">Preview</Text>

      {preview.collisions.length > 0 ? (
        <View className="gap-1">
          <Text className="text-xs font-medium text-foreground">
            {`${preview.collisions.length} name collision${
              preview.collisions.length === 1 ? "" : "s"
            } with what this workspace already has`}
          </Text>
          {preview.collisions.map((c) => (
            <Text
              key={`${c.kind}:${c.name}`}
              className="text-xs text-muted-foreground"
            >
              {`${packKindLabel(c.kind)} · ${c.name}`}
            </Text>
          ))}
        </View>
      ) : (
        <Text className="text-xs text-muted-foreground">
          Nothing in this workspace collides with the pack.
        </Text>
      )}

      {preview.problems.map((p) => (
        <Text key={p} className="text-xs text-destructive">
          {p}
        </Text>
      ))}

      <View className="gap-1">
        <Text className="text-xs font-medium text-foreground">
          On collision
        </Text>
        {/* Pill row, same shape as the domain filter on the catalogue
            screen — mobile has no segmented-control primitive and
            apps/mobile/CLAUDE.md forbids adding one for two callers. */}
        <View className="flex-row flex-wrap items-center gap-1">
          {strategies.map((s) => {
            const active = s === strategy;
            return (
              <Button
                key={s}
                variant="outline"
                size="sm"
                disabled={installing}
                onPress={() => onStrategyChange(s)}
                className={active ? "bg-accent" : ""}
                accessibilityState={{ selected: active }}
              >
                <Text
                  className={
                    active ? "text-accent-foreground" : "text-muted-foreground"
                  }
                >
                  {packStrategyLabel(s)}
                </Text>
              </Button>
            );
          })}
        </View>
        <Text className="text-xs leading-5 text-muted-foreground">
          {packStrategyHelp(strategy)}
        </Text>
      </View>

      {blocked ? (
        <Text className="text-xs text-warning">{preview.blocked}</Text>
      ) : null}

      <Button
        disabled={installing || blocked || preview.problems.length > 0}
        onPress={onInstall}
      >
        <Text>{installing ? "Installing" : installLabel}</Text>
      </Button>
    </View>
  );
}

function ReportPanel({ report }: { report: PackReport }) {
  const created = packCountsDetail(report.created);
  const merged = packCountsDetail(report.merged);
  return (
    <View className="gap-1 rounded-md border border-success/40 bg-success/10 p-3">
      <Text className="text-sm font-semibold text-foreground">
        Install report
      </Text>
      {created ? (
        <Text className="text-xs text-muted-foreground">
          {`Created · ${created}`}
        </Text>
      ) : null}
      {merged ? (
        <Text className="text-xs text-muted-foreground">
          {`Merged · ${merged}`}
        </Text>
      ) : null}
      {report.skipped.length > 0 ? (
        <Text className="text-xs text-muted-foreground">
          {`Skipped · ${report.skipped
            .map((c) => `${packKindLabel(c.kind)} ${c.name}`)
            .join(", ")}`}
        </Text>
      ) : null}
      {report.warnings.map((w) => (
        <Text key={w} className="text-xs text-warning">
          {w}
        </Text>
      ))}
    </View>
  );
}

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <View className="gap-1.5">
      <Text className="text-sm font-semibold text-foreground">{title}</Text>
      {children}
    </View>
  );
}

function Chip({
  children,
  tone,
}: {
  children: React.ReactNode;
  tone?: "warning";
}) {
  return (
    <View
      className={cn(
        "rounded px-1.5 py-0.5",
        tone === "warning" ? "bg-warning/15" : "bg-secondary",
      )}
    >
      <Text className="text-xs text-foreground">{children}</Text>
    </View>
  );
}
