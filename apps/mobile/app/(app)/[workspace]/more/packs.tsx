/**
 * Packs — catalogue + install ledger (OS plan, vague B).
 *
 * A pack turns this workspace into a ready-to-use setup for one function:
 * statuses, work item types, labels, properties, views, rules, a doctrine
 * section, a procedure, agents, automations, a project, a goal and example
 * work. Every pack works with no agent at all — agents arrive paused with no
 * runtime, automations disabled.
 *
 * Parity target is web's `packages/views/settings/components/packs-tab.tsx`:
 * same permission rule (owners/admins install and remove, every member
 * reads), same domain filter, same card facts, same count summary (see
 * lib/packs-display.ts), same uninstall promise — the configuration goes,
 * the content stays.
 *
 * What mobile deliberately does NOT do: upload a pack.yaml and export the
 * workspace as one. Both are file-system flows (pick a file, save a
 * download) with no phone equivalent worth the surface; they stay in web
 * and desktop Settings.
 *
 * Nothing here is optimistic — an uninstall decides row by row what it may
 * remove, so the screen awaits the server and then shows its report.
 */
import { useState } from "react";
import { ActivityIndicator, Alert, FlatList, Pressable, View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { PackKindList } from "@/components/packs/kind-list";
import {
  packCatalogueOptions,
  packInstallOptions,
  packInstallsOptions,
} from "@/data/queries/packs";
import { useUninstallPack } from "@/data/mutations/packs";
import type {
  PackInstall,
  PackSummary,
  PackUninstallReport,
} from "@/data/schemas";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  packCountsDetail,
  packCountsSummary,
  packDomainLabel,
  packInstallStatusLabel,
  packKindLabel,
  packSourceLabel,
} from "@/lib/packs-display";
import { timeAgo } from "@/lib/time-ago";
import { useIsWorkspaceManager } from "@/lib/use-is-workspace-manager";
import { cn } from "@/lib/utils";

/** Filter value that shows every pack — web's `ALL_DOMAINS`. */
const ALL_DOMAINS = "all";

export default function PacksScreen() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const canManage = useIsWorkspaceManager();
  const [domain, setDomain] = useState(ALL_DOMAINS);

  const catalogue = useQuery(packCatalogueOptions(wsId));
  const installs = useQuery(packInstallsOptions(wsId));

  const packs = catalogue.data?.packs ?? [];
  const domains = catalogue.data?.domains ?? [];
  const visible =
    domain === ALL_DOMAINS
      ? packs
      : packs.filter((p) => p.manifest.domain === domain);

  if (catalogue.isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (catalogue.error) {
    return (
      <View className="flex-1 gap-3 bg-background px-4 pt-4">
        <Text className="text-sm text-destructive">
          Could not load the catalogue
          {catalogue.error instanceof Error
            ? `: ${catalogue.error.message}`
            : "."}
        </Text>
        <Button variant="outline" onPress={() => catalogue.refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  return (
    <FlatList
      className="flex-1 bg-background"
      data={visible}
      keyExtractor={(item) => item.manifest.id}
      contentContainerClassName="pb-10"
      refreshing={catalogue.isRefetching}
      onRefresh={() => {
        catalogue.refetch();
        installs.refetch();
      }}
      ListHeaderComponent={
        <View className="gap-3 px-4 pb-3 pt-4">
          <Text className="text-xs leading-5 text-muted-foreground">
            A pack sets this workspace up for one function. Installing never
            overwrites what you already have unless you ask it to.
            {canManage
              ? ""
              : " Only owners and admins can install, update or remove one."}
          </Text>
          {domains.length > 0 ? (
            /* Pill row rather than a UISegmentedControl: every other
               filtered list on mobile (Doctrine, Triage, Runs) uses this
               shape, and apps/mobile/CLAUDE.md puts "existing pattern
               first" ahead of the native waterfall. */
            <View className="flex-row flex-wrap items-center gap-1">
              {[ALL_DOMAINS, ...domains].map((key) => {
                const active = key === domain;
                return (
                  <Button
                    key={key}
                    variant="outline"
                    size="sm"
                    onPress={() => setDomain(key)}
                    className={active ? "bg-accent" : ""}
                    accessibilityState={{ selected: active }}
                  >
                    <Text
                      className={
                        active
                          ? "text-accent-foreground"
                          : "text-muted-foreground"
                      }
                    >
                      {key === ALL_DOMAINS ? "All" : packDomainLabel(key)}
                    </Text>
                  </Button>
                );
              })}
            </View>
          ) : null}
        </View>
      }
      ListEmptyComponent={
        <Text className="px-6 py-10 text-center text-sm text-muted-foreground">
          No pack in this domain yet.
        </Text>
      }
      renderItem={({ item }) => <PackCard pack={item} />}
      ListFooterComponent={
        <InstalledSection
          installs={installs.data?.installs ?? []}
          isLoading={installs.isLoading}
          canManage={canManage}
        />
      }
    />
  );
}

function PackCard({ pack }: { pack: PackSummary }) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { manifest } = pack;
  const summary = packCountsSummary(pack.counts);

  return (
    <Pressable
      className="mx-4 mb-3 gap-2 rounded-md border border-border bg-card p-3 active:opacity-70"
      accessibilityRole="button"
      accessibilityLabel={manifest.title || manifest.id}
      onPress={() =>
        wsSlug && router.push(`/${wsSlug}/more/pack/${manifest.id}`)
      }
    >
      <View className="flex-row items-start gap-2">
        <Text className="flex-1 text-base font-semibold text-foreground">
          {manifest.title || manifest.id}
        </Text>
        {pack.upgrade_available === true ? (
          <Chip tone="warning">{`Update to ${manifest.version}`}</Chip>
        ) : pack.installed_version ? (
          <Chip>{`Installed ${pack.installed_version}`}</Chip>
        ) : null}
      </View>

      {manifest.summary ? (
        <Text className="text-sm leading-5 text-muted-foreground">
          {manifest.summary}
        </Text>
      ) : null}

      <View className="flex-row flex-wrap items-center gap-1">
        <Chip>{packDomainLabel(manifest.domain)}</Chip>
        <Chip>{`Wave ${manifest.wave}`}</Chip>
        {manifest.works_without_agents === true ? (
          <Chip>No agent needed</Chip>
        ) : null}
      </View>

      {summary ? (
        <Text className="text-xs text-muted-foreground">{summary}</Text>
      ) : null}
      {manifest.metric.label ? (
        <Text className="text-xs text-muted-foreground">
          {`Metric: ${manifest.metric.label}`}
        </Text>
      ) : null}
    </Pressable>
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

/**
 * The install ledger. Every row a pack created is recorded, so an install
 * can be updated in place (from the pack's own screen) or removed here.
 */
function InstalledSection({
  installs,
  isLoading,
  canManage,
}: {
  installs: PackInstall[];
  isLoading: boolean;
  canManage: boolean;
}) {
  return (
    <View className="mt-4 border-t border-border pt-4">
      <Text className="px-4 pb-1 text-base font-semibold text-foreground">
        Installed
      </Text>
      <Text className="px-4 pb-2 text-xs leading-5 text-muted-foreground">
        Every row a pack created is recorded, so it can be updated in place or
        removed.
      </Text>
      {isLoading ? (
        <View className="py-4">
          <ActivityIndicator />
        </View>
      ) : installs.length === 0 ? (
        <Text className="px-4 py-4 text-sm text-muted-foreground">
          No pack installed yet.
        </Text>
      ) : (
        installs.map((install) => (
          <InstalledRow
            key={install.id}
            install={install}
            canManage={canManage}
          />
        ))
      )}
    </View>
  );
}

function InstalledRow({
  install,
  canManage,
}: {
  install: PackInstall;
  canManage: boolean;
}) {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const uninstall = useUninstallPack();
  const [expanded, setExpanded] = useState(false);
  const [report, setReport] = useState<PackUninstallReport | null>(null);

  const meta = [
    install.pack_version,
    packSourceLabel(install.source),
    packInstallStatusLabel(install.status),
    `${install.item_count} item${install.item_count === 1 ? "" : "s"}`,
    install.installed_at ? timeAgo(install.installed_at) : null,
  ]
    .filter(Boolean)
    .join(" · ");

  /**
   * The confirm spells out the deal, exactly like web's
   * `packs.installed.confirm_body`: the configuration goes, the content
   * stays. iOS-native `Alert.alert` per the waterfall in
   * apps/mobile/CLAUDE.md — no hand-rolled confirm sheet.
   */
  const confirmUninstall = () =>
    Alert.alert(
      `Remove ${install.title || install.pack_id}?`,
      "The configuration the pack created goes: its views, labels, properties, rules, statuses and work item types are removed, its agents and automations archived. Your content stays: projects, goals, notes and work items the pack brought are yours now, and anything it merged into a row you already had is left alone.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Remove the pack",
          style: "destructive",
          onPress: () =>
            uninstall.mutate(
              { id: install.id },
              {
                onSuccess: (result) => setReport(result.report),
                onError: (err) =>
                  Alert.alert(
                    "Could not remove the pack",
                    err instanceof Error ? err.message : "unknown error",
                  ),
              },
            ),
        },
      ],
    );

  return (
    <View className="gap-2 border-t border-border px-4 py-3">
      <Pressable
        accessibilityRole="button"
        accessibilityState={{ expanded }}
        accessibilityLabel={`${install.title || install.pack_id} — what it installed`}
        onPress={() => setExpanded((v) => !v)}
      >
        <Text className="text-sm font-medium text-foreground">
          {install.title || install.pack_id}
        </Text>
        <Text className="text-xs text-muted-foreground">{meta}</Text>
      </Pressable>

      {install.metric.label ? (
        <Text className="text-xs text-muted-foreground">
          {`Metric: ${install.metric.label}`}
        </Text>
      ) : null}

      {canManage && install.status === "installed" ? (
        <View className="flex-row gap-2">
          {install.upgrade_to ? (
            <Button
              className="flex-1"
              variant="outline"
              size="sm"
              onPress={() =>
                wsSlug &&
                router.push(`/${wsSlug}/more/pack/${install.pack_id}`)
              }
            >
              <Text>{`Update to ${install.upgrade_to}`}</Text>
            </Button>
          ) : null}
          <Button
            className="flex-1"
            variant="outline"
            size="sm"
            disabled={uninstall.isPending}
            onPress={confirmUninstall}
          >
            <Text>{uninstall.isPending ? "Removing" : "Uninstall"}</Text>
          </Button>
        </View>
      ) : null}

      {expanded ? <InstallItems installId={install.id} /> : null}

      {report ? (
        <View className="gap-1 rounded-md bg-secondary p-2">
          <Text className="text-sm font-medium text-foreground">
            Uninstall report
          </Text>
          {packCountsDetail(report.removed) ? (
            <Text className="text-xs text-muted-foreground">
              {`Removed · ${packCountsDetail(report.removed)}`}
            </Text>
          ) : null}
          {report.kept.length > 0 ? (
            <Text className="text-xs text-muted-foreground">
              {`Kept · ${report.kept
                .map((it) => `${packKindLabel(it.kind)} ${it.name}`)
                .join(", ")}`}
            </Text>
          ) : null}
          {report.reasons.map((reason) => (
            <Text key={reason} className="text-xs text-muted-foreground">
              {reason}
            </Text>
          ))}
        </View>
      ) : null}
    </View>
  );
}

/** An install's ledger rows, grouped by kind. Mounted only while expanded. */
function InstallItems({ installId }: { installId: string }) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data, isLoading } = useQuery(packInstallOptions(wsId, installId));

  if (isLoading) return <ActivityIndicator />;

  const grouped: Record<string, string[]> = {};
  for (const item of data?.items ?? []) {
    (grouped[item.kind] ??= []).push(item.name);
  }
  const entries = Object.entries(grouped);

  if (entries.length === 0) {
    return (
      <Text className="text-xs text-muted-foreground">
        This install recorded no rows.
      </Text>
    );
  }

  return <PackKindList entries={entries} />;
}
