/**
 * Runtime machine detail — read-only.
 *
 * The route id is the MACHINE id (`daemon_id`, or the runtime's own id for a
 * row with no daemon), and the screen re-groups the workspace runtime list to
 * find it. There is no `GET /api/runtimes/{id}` route on the server — the
 * whole `r.Route("/{runtimeId}", …)` block in server/cmd/server/router.go has
 * PATCH, DELETE and the sub-resources but no GET — so web reads a single
 * runtime out of the list too.
 *
 * Three sections, all read from `metadata`:
 *   - Detected CLIs: the runtime rows the daemon registered, with their status.
 *   - Rejected CLIs (`metadata.skipped_agents`): found on the machine but
 *     refused — version below minimum, undetectable, or not executable.
 *     Without this, "not installed" and "installed but rejected" both look
 *     like an absent runtime.
 *   - CLI sign-in (`metadata.cli_auth`).
 *
 * No actions: signing a CLI in, updating or deleting a runtime all act on the
 * machine the daemon runs on.
 */
import { useMemo } from "react";
import { ActivityIndicator, ScrollView, View } from "react-native";
import { router, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { runtimeListOptions } from "@/data/queries/runtimes";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  groupRuntimesByMachine,
  machineSkippedAgents,
  providerDisplayName,
  readRuntimeCliAuthState,
  runtimeDisplayName,
} from "@/lib/runtime-display";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";

export default function RuntimeDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  const { data, isLoading, error, refetch } = useQuery(
    runtimeListOptions(wsId),
  );

  const machine = useMemo(
    () => groupRuntimesByMachine(data ?? []).find((m) => m.id === id) ?? null,
    [data, id],
  );

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (error) {
    return (
      <View className="flex-1 bg-background px-4 gap-3 pt-4">
        <Text className="text-sm text-destructive">
          Could not load runtimes:{" "}
          {error instanceof Error ? error.message : "unknown error"}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>Retry</Text>
        </Button>
      </View>
    );
  }

  if (!machine) {
    return (
      <View className="flex-1 items-center justify-center bg-background px-6 gap-3">
        <Text className="text-sm text-muted-foreground text-center">
          This machine is no longer registered in the workspace.
        </Text>
        <Button variant="outline" onPress={() => router.back()}>
          <Text>Back</Text>
        </Button>
      </View>
    );
  }

  const skipped = machineSkippedAgents(machine.runtimes);
  // The auth record is machine-level; read it off the freshest row.
  const freshest =
    machine.runtimes.find((r) => r.id === machine.representativeId) ??
    machine.runtimes[0];
  const cliAuth = readRuntimeCliAuthState(freshest?.metadata);

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="p-4 gap-5 pb-10"
    >
      <View className="gap-1">
        <Text className="text-base font-medium text-foreground">
          {machine.title}
        </Text>
        <Text className="text-xs text-muted-foreground">
          {[
            `${machine.onlineCount}/${machine.runtimes.length} online`,
            machine.cliVersion ? `CLI ${machine.cliVersion}` : null,
            machine.lastSeenAt ? `Seen ${timeAgo(machine.lastSeenAt)}` : null,
          ]
            .filter(Boolean)
            .join(" · ")}
        </Text>
      </View>

      <View className="gap-1.5">
        <SectionTitle>Detected CLIs</SectionTitle>
        {machine.runtimes.map((runtime) => (
          <View key={runtime.id} className="flex-row items-center gap-2 py-1">
            <View
              className={cn(
                "size-2 rounded-full",
                runtime.status === "online"
                  ? "bg-emerald-500"
                  : "bg-muted-foreground/40",
              )}
            />
            <Text className="flex-1 text-sm text-foreground" numberOfLines={1}>
              {runtimeDisplayName(runtime)}
            </Text>
            <Text className="text-xs text-muted-foreground">
              {providerDisplayName(runtime.provider)}
            </Text>
          </View>
        ))}
      </View>

      <View className="gap-1.5">
        <SectionTitle>Rejected CLIs</SectionTitle>
        {skipped.length > 0 ? (
          skipped.map((agent) => (
            <View key={agent.provider} className="gap-0.5 py-1">
              <Text className="text-sm text-foreground">
                {providerDisplayName(agent.provider)}
              </Text>
              <Text className="text-xs text-muted-foreground">
                {agent.reason}
              </Text>
            </View>
          ))
        ) : (
          <Text className="text-xs text-muted-foreground">
            No CLI was found and refused on this machine.
          </Text>
        )}
      </View>

      <View className="gap-1.5">
        <SectionTitle>CLI sign-in</SectionTitle>
        {cliAuth ? (
          <>
            <Text className="text-sm text-foreground">
              {cliAuth.authenticated ? "Signed in" : "Not signed in"}
              {cliAuth.provider
                ? ` · ${providerDisplayName(cliAuth.provider)}`
                : ""}
            </Text>
            {cliAuth.reason ? (
              <Text className="text-xs text-muted-foreground">
                {cliAuth.reason}
              </Text>
            ) : null}
            {cliAuth.checked_at ? (
              <Text className="text-xs text-muted-foreground">
                Checked {timeAgo(cliAuth.checked_at)}
              </Text>
            ) : null}
          </>
        ) : (
          <Text className="text-xs text-muted-foreground">
            This machine has not reported a sign-in state.
          </Text>
        )}
      </View>
    </ScrollView>
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <Text className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
      {children}
    </Text>
  );
}
