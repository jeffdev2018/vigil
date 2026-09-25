"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { OrgDefinition, OrgModel, OrgUnit } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { OrgSelect } from "./org-select";

/**
 * A new team starts where the old editor started one: it drafts rather than
 * acts, it reads, comments and proposes, and it stays out of external effects
 * until someone decides otherwise. The name is required so an empty "New team"
 * never lands on the chart by accident.
 */
export function OrgAddTeamDialog({
  definition,
  model,
  onAdd,
  onClose,
}: {
  definition: OrgDefinition;
  model: OrgModel;
  onAdd: (next: OrgDefinition, unitId: string) => void;
  onClose: () => void;
}) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [name, setName] = useState("");
  const [parent, setParent] = useState("");
  const [owner, setOwner] = useState("");
  // In a hierarchy every team but the first answers to someone.
  const parentRequired = model === "hierarchy" && definition.units.length > 0;
  const valid = name.trim() !== "" && (!parentRequired || parent !== "");

  const add = () => {
    if (!valid) return;
    const id = crypto.randomUUID();
    const unit: OrgUnit = {
      id,
      name: name.trim(),
      owner_id: owner || undefined,
      kind: "unit",
      members: [],
      roles: model === "circles" ? [{ id: crypto.randomUUID(), name: t(($) => $.visual.new_role) }] : [],
      autonomy: "draft",
      excludes: ["external_effects"],
      allow: ["read", "comment", "propose_plan"],
      deny: [],
      escalation_quota_per_day: 5,
    };
    onAdd(
      {
        ...definition,
        units: [...definition.units, unit],
        edges: parent ? [...definition.edges, { from: id, to: parent, kind: "reports_to" }] : definition.edges,
      },
      id,
    );
    onClose();
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.add_team.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.add_team.hint)}</DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            add();
          }}
        >
          <label className="flex flex-col gap-1.5 text-caption text-muted-foreground">
            {t(($) => $.add_team.name)}
            <Input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.add_team.name_placeholder)}
            />
          </label>
          <label className="flex flex-col gap-1.5 text-caption text-muted-foreground">
            {parentRequired ? t(($) => $.add_team.parent_required) : t(($) => $.add_team.parent)}
            <OrgSelect
              className="w-full"
              value={parent}
              onValueChange={setParent}
              items={[
                ...(parentRequired ? [] : [{ value: "", label: t(($) => $.add_team.no_parent) }]),
                ...definition.units.map((u) => ({ value: u.id, label: u.name })),
              ]}
            />
          </label>
          <label className="flex flex-col gap-1.5 text-caption text-muted-foreground">
            {t(($) => $.add_team.owner)}
            <OrgSelect
              className="w-full"
              value={owner}
              onValueChange={setOwner}
              items={[
                { value: "", label: t(($) => $.add_team.no_owner) },
                ...members.map((m) => ({ value: m.user_id, label: m.name })),
              ]}
            />
          </label>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t(($) => $.actions.cancel)}
            </Button>
            <Button type="submit" disabled={!valid}>
              {t(($) => $.add_team.submit)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
