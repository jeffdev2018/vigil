"use client";

import { Scale, X } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import {
  businessRulesOptions,
  useDeleteBusinessRule,
  useSetBusinessRuleStatus,
} from "@multica/core/workspace/business-rules";
import type { BusinessRule, Workspace } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { SettingsCard, SettingsSection } from "./settings-layout";
import { BusinessRuleForm, useAttachPointLabel } from "./business-rule-form";
import { useT } from "../../i18n";

/**
 * Business rules (K53): a PM writes a rule in plain language, previews the
 * compiled predicate in plain words with a dry-run of what it would block
 * today, and only then activates it. Active rules block their attach point
 * deterministically; disabling stops them at once, violations stay.
 *
 * The editor itself lives in `business-rule-form.tsx`, because the triage
 * queue opens the same form prefilled from an item.
 */
export function BusinessRulesSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = workspace.id;
  const { data } = useQuery(businessRulesOptions(wsId));
  const rules = data?.rules ?? [];
  const attachPoints = data?.attach_points?.length ? data.attach_points : ["project_create", "issue_submit_review"];
  const setStatus = useSetBusinessRuleStatus(wsId);
  const remove = useDeleteBusinessRule(wsId);
  const attachLabel = useAttachPointLabel();

  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.rules_failed));
  const activate = (rule: BusinessRule) =>
    setStatus.mutate(
      { id: rule.id, status: "active" },
      {
        onSuccess: () => toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" }),
        onError: fail,
      },
    );

  return (
    <SettingsSection
      title={
        <span className="inline-flex items-center gap-2">
          <Scale className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.workspace.rules_section)}
        </span>
      }
    >
      <SettingsCard>
        <div className="flex flex-col gap-2 p-3 text-caption">
          <p className="text-muted-foreground">{t(($) => $.workspace.rules_description)}</p>
          {rules.length === 0 ? (
            <p data-testid="rules-empty" className="text-muted-foreground">{t(($) => $.workspace.rules_empty)}</p>
          ) : (
            <ul className="flex flex-col gap-1.5">
              {rules.map((r) => (
                <li key={r.id} data-testid="business-rule" data-status={r.status} className="flex flex-col gap-0.5 rounded-md border p-2">
                  <div className="flex items-center gap-2">
                    <span className="min-w-0 flex-1 truncate font-medium">{r.title}</span>
                    <span className="shrink-0 text-muted-foreground">{attachLabel(r.attach_point)}</span>
                    <span className="shrink-0 rounded bg-accent px-1.5 py-0.5 uppercase">{r.status}</span>
                    {canEdit && r.status !== "active" && (
                      <Button type="button" size="sm" variant="outline" disabled={setStatus.isPending} onClick={() => activate(r)}>
                        {t(($) => $.workspace.rules_activate)}
                      </Button>
                    )}
                    {canEdit && r.status === "active" && (
                      <Button type="button" size="sm" variant="outline" disabled={setStatus.isPending} onClick={() => setStatus.mutate({ id: r.id, status: "disabled" }, { onError: fail })}>
                        {t(($) => $.workspace.rules_disable)}
                      </Button>
                    )}
                    {canEdit && r.status !== "active" && (
                      <button type="button" aria-label={t(($) => $.workspace.rules_remove)} className="text-faint-foreground hover:text-foreground" onClick={() => remove.mutate(r.id, { onError: fail })}>
                        <X className="size-3.5" />
                      </button>
                    )}
                  </div>
                  <p className="text-muted-foreground">
                    {r.description}
                    {r.action_description ? ` → ${r.action_description}` : ""}
                  </p>
                </li>
              ))}
            </ul>
          )}
          {canEdit && <BusinessRuleForm wsId={wsId} attachPoints={attachPoints} />}
        </div>
      </SettingsCard>
    </SettingsSection>
  );
}
