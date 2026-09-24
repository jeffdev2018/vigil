"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { addOrgMembers } from "@multica/core/org";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { OrgDefinition, OrgMember } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { Switch } from "@multica/ui/components/ui/switch";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

interface Candidate {
  member: OrgMember;
  name: string;
  detail: string;
}

const keyOf = (m: Pick<OrgMember, "type" | "id">) => `${m.type}:${m.id}`;

/**
 * Everyone in the workspace who could join a team, humans and agents alike.
 * Opened from one team, it adds to that team. The list is searchable because a
 * workspace easily holds dozens of agents, and it can hide whoever already
 * belongs to a team here — the people usually worth adding.
 */
export function OrgDirectorySheet({
  definition,
  unitId,
  onAdd,
  onClose,
}: {
  definition: OrgDefinition;
  unitId: string;
  onAdd: (next: OrgDefinition) => void;
  onClose: () => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const agentQuery = useQuery(agentListOptions(wsId));
  const memberQuery = useQuery(memberListOptions(wsId));
  const [search, setSearch] = useState("");
  const [onlyUnplaced, setOnlyUnplaced] = useState(true);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const unit = definition.units.find((u) => u.id === unitId);

  const placed = useMemo(() => {
    const keys = new Set<string>();
    for (const u of definition.units) {
      if (u.owner_id) keys.add(keyOf({ type: "member", id: u.owner_id }));
      for (const m of u.members) keys.add(keyOf(m));
    }
    return keys;
  }, [definition]);
  const inThisTeam = useMemo(
    () => new Set([...(unit?.members ?? []).map(keyOf), ...(unit?.owner_id ? [`member:${unit.owner_id}`] : [])]),
    [unit],
  );

  const candidates: Candidate[] = useMemo(
    () => [
      ...(memberQuery.data ?? []).map((m) => ({
        member: { type: "member" as const, id: m.user_id },
        name: m.name,
        detail: t(($) => $.plan.human),
      })),
      ...(agentQuery.data ?? [])
        .filter((a) => !a.archived_at)
        .map((a) => ({
          member: { type: "agent" as const, id: a.id },
          name: a.name,
          detail: a.description?.trim() || t(($) => $.plan.agent),
        })),
    ],
    [memberQuery.data, agentQuery.data, t],
  );

  const needle = search.trim().toLocaleLowerCase();
  const shown = candidates.filter(
    (c) =>
      !inThisTeam.has(keyOf(c.member)) &&
      (!onlyUnplaced || !placed.has(keyOf(c.member)) || picked.has(keyOf(c.member))) &&
      (!needle || `${c.name} ${c.detail}`.toLocaleLowerCase().includes(needle)),
  );
  const loading = agentQuery.isPending || memberQuery.isPending;
  const failed = agentQuery.isError || memberQuery.isError;

  const toggle = (key: string, on: boolean) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (on) next.add(key);
      else next.delete(key);
      return next;
    });
  const add = () => {
    const members = candidates.filter((c) => picked.has(keyOf(c.member))).map((c) => c.member);
    onAdd(addOrgMembers(definition, unitId, members));
    onClose();
  };

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent side="right" className="w-full gap-0 sm:max-w-md">
        <SheetHeader className="border-b">
          <SheetTitle>{t(($) => $.directory.title, { team: unit?.name ?? "" })}</SheetTitle>
          <SheetDescription>{t(($) => $.directory.hint)}</SheetDescription>
        </SheetHeader>
        <div className="flex flex-col gap-3 border-b p-4">
          <div className="relative">
            <Search aria-hidden="true" className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-faint-foreground" />
            <Input
              autoFocus
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t(($) => $.directory.search)}
              aria-label={t(($) => $.directory.search)}
              className="pl-8"
            />
          </div>
          <label className="flex items-center justify-between gap-3 text-caption text-muted-foreground">
            {t(($) => $.directory.only_unplaced)}
            <Switch checked={onlyUnplaced} onCheckedChange={setOnlyUnplaced} />
          </label>
        </div>
        <div className="flex-1 overflow-y-auto">
          {failed ? (
            <div role="alert" className="flex items-center justify-between gap-3 p-4 text-caption">
              {t(($) => $.form.error)}
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  void agentQuery.refetch();
                  void memberQuery.refetch();
                }}
              >
                {t(($) => $.catalog.retry)}
              </Button>
            </div>
          ) : loading ? (
            <ul aria-hidden="true" className="flex flex-col gap-1 p-2">
              {Array.from({ length: 6 }, (_, i) => (
                <li key={i} className="flex items-center gap-3 rounded-md px-2 py-2">
                  <span className="size-4 rounded bg-muted" />
                  <span className="size-8 rounded-full bg-muted" />
                  <span className="h-3 w-40 rounded bg-muted" />
                </li>
              ))}
            </ul>
          ) : shown.length === 0 ? (
            <p className="p-4 text-caption text-muted-foreground">
              {onlyUnplaced && !needle ? t(($) => $.directory.everyone_placed) : t(($) => $.directory.no_match)}
            </p>
          ) : (
            <ul className="flex flex-col p-2">
              {shown.map((c) => {
                const key = keyOf(c.member);
                const id = `org-directory-${key}`;
                return (
                  <li key={key}>
                    <label
                      htmlFor={id}
                      className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-2 hover:bg-surface-hover"
                    >
                      <Checkbox id={id} checked={picked.has(key)} onCheckedChange={(on) => toggle(key, on === true)} />
                      <ActorAvatar
                        actorType={c.member.type}
                        actorId={c.member.id}
                        size="lg"
                        showStatusDot={c.member.type === "agent"}
                        profileLink={false}
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-label font-medium">{c.name}</span>
                        <span className="block truncate text-caption text-muted-foreground">{c.detail}</span>
                      </span>
                      {placed.has(key) && (
                        <span className="shrink-0 text-micro text-faint-foreground">{t(($) => $.directory.elsewhere)}</span>
                      )}
                    </label>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
        <SheetFooter className="border-t">
          <Button disabled={picked.size === 0} onClick={add}>
            {t(($) => $.directory.add, { count: picked.size })}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
