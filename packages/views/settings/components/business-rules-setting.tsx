"use client";

import { useState } from "react";
import { Scale, X } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import {
  businessRulesOptions,
  useCreateBusinessRule,
  useDeleteBusinessRule,
  useDryRunBusinessRule,
  useSetBusinessRuleStatus,
} from "@multica/core/workspace/business-rules";
import type { BusinessRule, BusinessRuleDryRun, Workspace } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { SettingsCard, SettingsSection } from "./settings-layout";
import { useT } from "../../i18n";

/**
 * Business rules (K53): a PM writes a rule in plain language, previews the
 * compiled predicate in plain words with a dry-run of what it would block
 * today, and only then activates it. Active rules block their attach point
 * deterministically; disabling stops them at once, violations stay.
 */
export function BusinessRulesSetting({ workspace, canEdit }: { workspace: Workspace; canEdit: boolean }) {
  const { t } = useT("settings");
  const wsId = workspace.id;
  const { data } = useQuery(businessRulesOptions(wsId));
  const rules = data?.rules ?? [];
  const attachPoints = data?.attach_points?.length ? data.attach_points : ["project_create", "issue_submit_review"];
  const create = useCreateBusinessRule(wsId);
  const dryRun = useDryRunBusinessRule();
  const setStatus = useSetBusinessRuleStatus(wsId);
  const remove = useDeleteBusinessRule(wsId);
  const [text, setText] = useState("");
  const [attach, setAttach] = useState("project_create");
  const [preview, setPreview] = useState<BusinessRuleDryRun | null>(null);

  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.workspace.rules_failed));
  const attachLabel = (a: string) => {
    switch (a) {
      case "project_create":
        return t(($) => $.workspace.rules_attach_project_create);
      case "issue_submit_review":
        return t(($) => $.workspace.rules_attach_issue_submit_review);
      case "agent_run_dispatch":
        return t(($) => $.workspace.rules_attach_agent_run_dispatch);
      default:
        return a;
    }
  };

  const previewRule = () => {
    if (text.trim() === "") return;
    create.mutate(
      { natural_language: text.trim(), attach_point: attach },
      {
        onSuccess: (rule) => dryRun.mutate(rule.id, { onSuccess: setPreview, onError: fail }),
        onError: fail,
      },
    );
  };
  const activate = (rule: BusinessRule) =>
    setStatus.mutate(
      { id: rule.id, status: "active" },
      {
        onSuccess: () => {
          if (preview?.rule.id === rule.id) {
            setPreview(null);
            setText("");
          }
          toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
        },
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
                  <p className="text-muted-foreground">{r.description}</p>
                </li>
              ))}
            </ul>
          )}
          {canEdit && (
            <div className="flex flex-col gap-2">
              <Textarea
                aria-label={t(($) => $.workspace.rules_text)}
                placeholder={t(($) => $.workspace.rules_text_placeholder)}
                rows={2}
                value={text}
                onChange={(e) => {
                  setText(e.target.value);
                  setPreview(null);
                }}
              />
              <div className="flex flex-wrap items-center gap-2">
                <select aria-label={t(($) => $.workspace.rules_attach)} className="h-8 rounded-md border bg-background px-2" value={attach} onChange={(e) => setAttach(e.target.value)}>
                  {attachPoints.map((a) => (
                    <option key={a} value={a}>{attachLabel(a)}</option>
                  ))}
                </select>
                <Button type="button" size="sm" variant="outline" disabled={create.isPending || dryRun.isPending || text.trim() === ""} onClick={previewRule}>
                  {t(($) => $.workspace.rules_preview)}
                </Button>
              </div>
              {preview && (
                <div data-testid="rule-preview" className="flex flex-col gap-1 rounded-md border p-2">
                  <p className="font-medium">{preview.rule.title}</p>
                  <p>{preview.rule.description}</p>
                  <p className="text-muted-foreground">
                    {preview.violations.length === 0
                      ? t(($) => $.workspace.rules_dry_run_clean, { count: preview.checked })
                      : t(($) => $.workspace.rules_dry_run_violations, { count: preview.violations.length, checked: preview.checked })}
                  </p>
                  {preview.violations.slice(0, 10).map((v) => (
                    <p key={v.subject_id} className="truncate text-muted-foreground" title={v.detail}>
                      {v.label} · {v.detail}
                    </p>
                  ))}
                  <Button type="button" size="sm" className="self-start" disabled={setStatus.isPending} onClick={() => activate(preview.rule)}>
                    {t(($) => $.workspace.rules_activate)}
                  </Button>
                </div>
              )}
            </div>
          )}
        </div>
      </SettingsCard>
    </SettingsSection>
  );
}
