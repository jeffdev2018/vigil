"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  issueTransitionRulesOptions,
  useDeleteIssueTransitionRule,
  useSaveIssueTransitionRule,
  type IssueTransitionRule,
} from "@multica/core/issue-transitions";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Label as FieldLabel } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { useStatusLabel } from "../../issues/utils/status-label";
import { SettingsTab } from "./settings-layout";

// Transition rules (F28). A rule says who may move an issue INTO a status
// category, and whether that move waits for an approver. Rules are written
// against categories, so this editor never lists individual statuses.
//
// The empty state is load-bearing: a workspace with no rules has completely
// free transitions, and an admin arriving here needs to know that before they
// wonder why the picker greys nothing.

const ROLES = ["owner", "admin", "member"] as const;
const ACTOR_TYPES = ["member", "agent", "squad"] as const;

const CATEGORIES = ALL_STATUSES;

interface RuleDraft {
  id?: string;
  from_category: string;
  to_category: string;
  allowed_roles: string[];
  allow_actor_types: string[];
  requires_approval: boolean;
  approver_roles: string[];
  enabled: boolean;
}

function toDraft(rule?: IssueTransitionRule): RuleDraft {
  return {
    id: rule?.id,
    from_category: rule?.from_category ?? "",
    to_category: rule?.to_category || "done",
    allowed_roles: rule?.allowed_roles ?? [],
    allow_actor_types: rule?.allow_actor_types ?? [],
    requires_approval: rule?.requires_approval ?? false,
    approver_roles: rule?.approver_roles ?? [],
    enabled: rule?.enabled ?? true,
  };
}

function toggle(list: string[], value: string): string[] {
  return list.includes(value) ? list.filter((v) => v !== value) : [...list, value];
}

// Roles and actor types are two server enums that share the word "member"
// (the member ROLE vs. the human ACTOR type), so a rule granting both reads
// "member, member" unless the labels are deduplicated after translation.
function useGrantLabels() {
  const { t: tMembers } = useT("members");
  const { t } = useT("settings");
  const roleLabel = (role: string): string => {
    switch (role) {
      case "owner":
        return tMembers(($) => $.role.owner);
      case "admin":
        return tMembers(($) => $.role.admin);
      case "member":
        return tMembers(($) => $.role.member);
      default:
        return role;
    }
  };
  const actorTypeLabel = (type: string): string => {
    switch (type) {
      case "member":
        return t(($) => $.transitions.actor_type.member);
      case "agent":
        return t(($) => $.transitions.actor_type.agent);
      case "squad":
        return t(($) => $.transitions.actor_type.squad);
      default:
        return type;
    }
  };
  return { roleLabel, actorTypeLabel };
}

export function TransitionsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();

  const [editing, setEditing] = useState<RuleDraft | null>(null);

  const { data, isLoading } = useQuery(issueTransitionRulesOptions(wsId));
  const rules = data?.rules ?? [];
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentUser = useAuthStore((s) => s.user);
  const myRole = useMemo(() => {
    if (!currentUser) return null;
    return members.find((m) => m.user_id === currentUser.id)?.role ?? null;
  }, [members, currentUser]);
  const isAdmin = myRole === "owner" || myRole === "admin";

  const remove = useDeleteIssueTransitionRule(wsId);

  return (
    <SettingsTab
      title={t(($) => $.transitions.title)}
      description={t(($) => $.transitions.description)}
    >
      <div className="space-y-4">
        {isAdmin && (
          <div className="flex justify-end">
            <Button size="sm" onClick={() => setEditing(toDraft())}>
              <Plus className="mr-1.5 h-4 w-4" />
              {t(($) => $.transitions.add)}
            </Button>
          </div>
        )}

        {isLoading ? (
          <div className="rounded-lg border border-surface-border bg-card px-4 py-12 text-center text-body text-muted-foreground">
            {t(($) => $.transitions.loading)}
          </div>
        ) : rules.length === 0 ? (
          <div className="rounded-lg border border-surface-border bg-card px-4 py-12 text-center text-body text-muted-foreground">
            {t(($) => $.transitions.empty)}
          </div>
        ) : (
          <div className="overflow-hidden rounded-lg border border-surface-border bg-card">
            {rules.map((rule) => (
              <RuleRow
                key={rule.id}
                rule={rule}
                canManage={isAdmin}
                onEdit={() => setEditing(toDraft(rule))}
                onDelete={() => {
                  remove.mutate(rule.id, {
                    onError: () => toast.error(t(($) => $.transitions.delete_failed)),
                  });
                }}
              />
            ))}
          </div>
        )}
      </div>

      <RuleEditorDialog draft={editing} onClose={() => setEditing(null)} />
    </SettingsTab>
  );
}

function RuleRow({
  rule,
  canManage,
  onEdit,
  onDelete,
}: {
  rule: IssueTransitionRule;
  canManage: boolean;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const statusLabel = useStatusLabel(wsId);
  const { roleLabel, actorTypeLabel } = useGrantLabels();
  const grants = [
    ...new Set([...rule.allowed_roles.map(roleLabel), ...rule.allow_actor_types.map(actorTypeLabel)]),
    ...(rule.actors.length > 0 ? [t(($) => $.transitions.named_actors, { count: rule.actors.length })] : []),
  ];
  return (
    <div className="flex items-center gap-3 border-b border-surface-border px-4 py-3 last:border-b-0">
      <div className="min-w-0 flex-1">
        <div className="truncate text-body font-medium">
          {(rule.from_category ? statusLabel(rule.from_category) : t(($) => $.transitions.any_origin)) + " → " + statusLabel(rule.to_category)}
        </div>
        <div className="truncate text-caption text-muted-foreground">
          {grants.length > 0
            ? t(($) => $.transitions.grants, { list: grants.join(", ") })
            : t(($) => $.transitions.no_grants)}
          {rule.requires_approval ? " · " + t(($) => $.transitions.needs_approval) : ""}
          {rule.enabled ? "" : " · " + t(($) => $.transitions.disabled)}
        </div>
      </div>
      {canManage && (
        <div className="flex shrink-0 items-center gap-1">
          <Button variant="ghost" size="icon" onClick={onEdit} aria-label={t(($) => $.transitions.edit)}>
            <Pencil className="h-4 w-4" />
          </Button>
          <Button variant="ghost" size="icon" onClick={onDelete} aria-label={t(($) => $.transitions.delete)}>
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      )}
    </div>
  );
}

function RuleEditorDialog({ draft, onClose }: { draft: RuleDraft | null; onClose: () => void }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const save = useSaveIssueTransitionRule(wsId);
  const statusLabel = useStatusLabel(wsId);
  const { roleLabel, actorTypeLabel } = useGrantLabels();
  const [local, setLocal] = useState<RuleDraft | null>(draft);

  // Re-seed when a different rule is opened. Keeping the draft in state is what
  // lets every toggle be local until Save; the dialog is remounted per rule by
  // the key below.
  if (draft && local?.id !== draft.id) setLocal(draft);

  if (!draft || !local) return null;

  const submit = () => {
    save.mutate(
      {
        id: local.id,
        from_category: local.from_category || null,
        to_category: local.to_category,
        allowed_roles: local.allowed_roles,
        allow_actor_types: local.allow_actor_types,
        requires_approval: local.requires_approval,
        approver_roles: local.approver_roles,
        enabled: local.enabled,
      },
      {
        onSuccess: onClose,
        onError: () => toast.error(t(($) => $.transitions.save_failed)),
      },
    );
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {local.id ? t(($) => $.transitions.editor.edit_title) : t(($) => $.transitions.editor.create_title)}
          </DialogTitle>
          <DialogDescription>{t(($) => $.transitions.editor.hint)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <FieldLabel htmlFor="transition-from">{t(($) => $.transitions.editor.from)}</FieldLabel>
              <select
                id="transition-from"
                className="h-9 w-full rounded-md border border-surface-border bg-background px-2 text-body"
                value={local.from_category}
                onChange={(e) => setLocal({ ...local, from_category: e.target.value })}
              >
                <option value="">{t(($) => $.transitions.any_origin)}</option>
                {CATEGORIES.map((c) => (
                  <option key={c} value={c}>{statusLabel(c)}</option>
                ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <FieldLabel htmlFor="transition-to">{t(($) => $.transitions.editor.to)}</FieldLabel>
              <select
                id="transition-to"
                className="h-9 w-full rounded-md border border-surface-border bg-background px-2 text-body"
                value={local.to_category}
                onChange={(e) => setLocal({ ...local, to_category: e.target.value })}
              >
                {CATEGORIES.map((c) => (
                  <option key={c} value={c}>{statusLabel(c)}</option>
                ))}
              </select>
            </div>
          </div>

          <ToggleRow
            label={t(($) => $.transitions.editor.roles)}
            options={ROLES}
            labelOf={roleLabel}
            selected={local.allowed_roles}
            onToggle={(v) => setLocal({ ...local, allowed_roles: toggle(local.allowed_roles, v) })}
          />
          <ToggleRow
            label={t(($) => $.transitions.editor.actor_types)}
            options={ACTOR_TYPES}
            labelOf={actorTypeLabel}
            selected={local.allow_actor_types}
            onToggle={(v) => setLocal({ ...local, allow_actor_types: toggle(local.allow_actor_types, v) })}
          />

          <label className="flex items-center justify-between gap-2 text-body">
            {t(($) => $.transitions.editor.requires_approval)}
            <Switch
              checked={local.requires_approval}
              onCheckedChange={(v) => setLocal({ ...local, requires_approval: v })}
            />
          </label>
          {local.requires_approval && (
            <ToggleRow
              label={t(($) => $.transitions.editor.approver_roles)}
              options={ROLES}
              labelOf={roleLabel}
              selected={local.approver_roles}
              onToggle={(v) => setLocal({ ...local, approver_roles: toggle(local.approver_roles, v) })}
            />
          )}
          <label className="flex items-center justify-between gap-2 text-body">
            {t(($) => $.transitions.editor.enabled)}
            <Switch checked={local.enabled} onCheckedChange={(v) => setLocal({ ...local, enabled: v })} />
          </label>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>{t(($) => $.transitions.editor.cancel)}</Button>
          <Button onClick={submit} disabled={save.isPending}>
            {t(($) => $.transitions.editor.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ToggleRow({
  label,
  options,
  labelOf,
  selected,
  onToggle,
}: {
  label: string;
  options: readonly string[];
  labelOf: (value: string) => string;
  selected: string[];
  onToggle: (value: string) => void;
}) {
  return (
    <div className="space-y-1.5">
      <FieldLabel>{label}</FieldLabel>
      <div className="flex flex-wrap gap-2">
        {options.map((option) => {
          const active = selected.includes(option);
          return (
            <button
              key={option}
              type="button"
              aria-pressed={active}
              onClick={() => onToggle(option)}
              className={`rounded-md border px-2.5 py-1 text-caption transition-colors ${
                active
                  ? "border-primary bg-primary/10 font-medium text-foreground"
                  : "border-surface-border text-muted-foreground hover:bg-accent"
              }`}
            >
              {labelOf(option)}
            </button>
          );
        })}
      </div>
    </div>
  );
}
