"use client";

import { useState } from "react";
import { toast } from "sonner";
import {
  useCreateBusinessRule,
  useDryRunBusinessRule,
  useSetBusinessRuleStatus,
} from "@multica/core/workspace/business-rules";
import type {
  BusinessRule,
  BusinessRuleDryRun,
  IssueAssigneeType,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { AssigneePicker } from "../../issues/components/pickers";
import { useT } from "../../i18n";

const ISSUE_PRIORITIES = ["urgent", "high", "medium", "low", "none"] as const;

/** Plain-language label for an attach point, shared with the rule list. */
export function useAttachPointLabel(): (attachPoint: string) => string {
  const { t } = useT("settings");
  return (attachPoint: string) => {
    switch (attachPoint) {
      case "project_create":
        return t(($) => $.workspace.rules_attach_project_create);
      case "issue_submit_review":
        return t(($) => $.workspace.rules_attach_issue_submit_review);
      case "agent_run_dispatch":
        return t(($) => $.workspace.rules_attach_agent_run_dispatch);
      case "webhook_received":
        return t(($) => $.workspace.rules_attach_webhook_received);
      default:
        return attachPoint;
    }
  };
}

/**
 * Draft a rule, preview the compiled predicate with its dry-run, activate.
 * Extracted from the settings section so the triage queue can open the same
 * form prefilled from the item a human is looking at — one form, one contract
 * with the API, rather than a second half-form next to the queue.
 */
export function BusinessRuleForm({
  wsId,
  attachPoints,
  initialText = "",
  initialAttachPoint = "project_create",
  onActivated,
}: {
  wsId: string;
  attachPoints: string[];
  initialText?: string;
  initialAttachPoint?: string;
  /** Called once the drafted rule is active, so a dialog can close itself. */
  onActivated?: () => void;
}) {
  const { t } = useT("settings");
  const attachLabel = useAttachPointLabel();
  const create = useCreateBusinessRule(wsId);
  const dryRun = useDryRunBusinessRule();
  const setStatus = useSetBusinessRuleStatus(wsId);

  const [text, setText] = useState(initialText);
  const [attach, setAttach] = useState(initialAttachPoint);
  const [preview, setPreview] = useState<BusinessRuleDryRun | null>(null);
  // Triage rules (K62): what a matching webhook delivery becomes.
  const [actionKind, setActionKind] = useState<"dismiss" | "accept">("dismiss");
  const [actionPriority, setActionPriority] = useState("");
  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | null>(null);
  const [assigneeId, setAssigneeId] = useState<string | null>(null);
  const isWebhook = attach === "webhook_received";

  const fail = (e: unknown) =>
    toast.error(
      e instanceof Error && e.message ? e.message : t(($) => $.workspace.rules_failed),
    );

  const previewRule = () => {
    if (text.trim() === "") return;
    // The backend accepts member and agent assignees alike; the picker is the
    // one from the issue views, so a rule can route to a human on call.
    const action = isWebhook
      ? actionKind === "dismiss"
        ? { kind: "dismiss" }
        : {
            kind: "accept",
            priority: actionPriority || undefined,
            assignee_type: assigneeId
              ? assigneeType === "agent"
                ? "agent"
                : "member"
              : undefined,
            assignee_id: assigneeId || undefined,
          }
      : undefined;
    create.mutate(
      { natural_language: text.trim(), attach_point: attach, action },
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
          setPreview(null);
          setText("");
          toast.success(t(($) => $.auto_save.toast_saved), { id: "settings-auto-save" });
          onActivated?.();
        },
        onError: fail,
      },
    );

  return (
    <div className="flex flex-col gap-2 text-caption">
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
        <select
          aria-label={t(($) => $.workspace.rules_attach)}
          className="h-8 rounded-md border bg-background px-2"
          value={attach}
          onChange={(e) => setAttach(e.target.value)}
        >
          {attachPoints.map((a) => (
            <option key={a} value={a}>{attachLabel(a)}</option>
          ))}
        </select>
        {isWebhook && (
          <>
            <select
              aria-label={t(($) => $.workspace.rules_action)}
              className="h-8 rounded-md border bg-background px-2"
              value={actionKind}
              onChange={(e) => setActionKind(e.target.value as "dismiss" | "accept")}
            >
              <option value="dismiss">{t(($) => $.workspace.rules_action_dismiss)}</option>
              <option value="accept">{t(($) => $.workspace.rules_action_accept)}</option>
            </select>
            {actionKind === "accept" && (
              <>
                <select
                  aria-label={t(($) => $.workspace.rules_action_priority)}
                  className="h-8 rounded-md border bg-background px-2"
                  value={actionPriority}
                  onChange={(e) => setActionPriority(e.target.value)}
                >
                  <option value="">{t(($) => $.workspace.rules_action_priority_keep)}</option>
                  {ISSUE_PRIORITIES.map((p) => (
                    <option key={p} value={p}>{p}</option>
                  ))}
                </select>
                <AssigneePicker
                  assigneeType={assigneeType}
                  assigneeId={assigneeId}
                  onUpdate={(updates) => {
                    setAssigneeType((updates.assignee_type ?? null) as IssueAssigneeType | null);
                    setAssigneeId(updates.assignee_id ?? null);
                  }}
                  align="start"
                  trigger={
                    <span className="text-caption">
                      {assigneeId
                        ? t(($) => $.workspace.rules_action_assignee_set)
                        : t(($) => $.workspace.rules_action_assignee)}
                    </span>
                  }
                />
              </>
            )}
          </>
        )}
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={create.isPending || dryRun.isPending || text.trim() === ""}
          onClick={previewRule}
        >
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
              : t(($) => $.workspace.rules_dry_run_violations, {
                  count: preview.violations.length,
                  checked: preview.checked,
                })}
          </p>
          {preview.violations.slice(0, 10).map((v) => (
            <p key={v.subject_id} className="truncate text-muted-foreground" title={v.detail}>
              {v.label} · {v.detail}
            </p>
          ))}
          <Button
            type="button"
            size="sm"
            className="self-start"
            disabled={setStatus.isPending}
            onClick={() => activate(preview.rule)}
          >
            {t(($) => $.workspace.rules_activate)}
          </Button>
        </div>
      )}
    </div>
  );
}
