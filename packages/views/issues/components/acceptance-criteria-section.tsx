"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Circle, Clock, X } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  ACCEPTANCE_PROOF_TYPES,
  acceptanceCriteriaOptions,
  isCriterionSatisfied,
  useProveAcceptanceCriterion,
  useSetAcceptanceCriteria,
} from "@multica/core/issues/acceptance";
import type { AcceptanceCriterion } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Outcome Contract (K12): the issue's acceptance criteria, each with the
 * proof behind it. The server refuses a move to done while one lacks a
 * satisfied proof, so this is where a human sees what still stands between
 * the issue and done, attaches a proof, or validates a criterion themselves.
 */
export function AcceptanceCriteriaSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: criteria = [] } = useQuery(acceptanceCriteriaOptions(wsId, issueId));
  const setCriteria = useSetAcceptanceCriteria(wsId);
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState("");
  const satisfied = criteria.filter(isCriterionSatisfied).length;

  const onError = (e: unknown) =>
    toast.error(e instanceof Error && e.message ? e.message : t(($) => $.acceptance.update_failed));

  const submit = (next: { id?: string; text: string }[]) =>
    setCriteria.mutate({ issueId, criteria: next }, { onError });

  const add = () => {
    const text = draft.trim();
    if (text === "") return;
    submit([...criteria.map((c) => ({ id: c.id, text: c.text })), { text }]);
    setDraft("");
  };

  return (
    <div data-testid="acceptance-criteria" className="text-caption">
      <div className="mb-2 flex items-center gap-1 px-2 py-1 font-medium">
        <span>{t(($) => $.acceptance.section)}</span>
        {criteria.length > 0 && (
          <span
            className={cn("ml-auto font-mono tabular-nums", satisfied === criteria.length ? "text-success" : "text-muted-foreground")}
            data-testid="acceptance-progress"
          >
            {satisfied}/{criteria.length}
          </span>
        )}
      </div>
      <div className="flex flex-col gap-1.5 pl-2">
        {criteria.map((c) => (
          <CriterionRow
            key={c.id}
            criterion={c}
            issueId={issueId}
            wsId={wsId}
            onRemove={() => submit(criteria.filter((o) => o.id !== c.id).map((o) => ({ id: o.id, text: o.text })))}
          />
        ))}
        {adding || criteria.length > 0 ? (
          <Input
            aria-label={t(($) => $.acceptance.add_placeholder)}
            placeholder={t(($) => $.acceptance.add_placeholder)}
            value={draft}
            disabled={setCriteria.isPending}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
            className="h-7"
          />
        ) : (
          <button type="button" className="self-start text-muted-foreground hover:text-foreground" onClick={() => setAdding(true)}>
            {t(($) => $.acceptance.add)}
          </button>
        )}
      </div>
    </div>
  );
}

function CriterionRow({
  criterion,
  issueId,
  wsId,
  onRemove,
}: {
  criterion: AcceptanceCriterion;
  issueId: string;
  wsId: string;
  onRemove: () => void;
}) {
  const { t } = useT("issues");
  const prove = useProveAcceptanceCriterion(wsId);
  const [editing, setEditing] = useState(false);
  const [proofType, setProofType] = useState<string>("test");
  const [ref, setRef] = useState("");
  const state = criterion.proof_state;

  const send = (proof_type: string, proof_ref?: string) =>
    prove.mutate(
      { issueId, criterionId: criterion.id, proof_type, proof_ref },
      {
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.acceptance.update_failed)),
        onSuccess: () => {
          setEditing(false);
          setRef("");
        },
      },
    );

  const stateLabel =
    state === "satisfied"
      ? t(($) => $.acceptance.state_satisfied)
      : state === "pending_human"
        ? t(($) => $.acceptance.state_pending_human)
        : t(($) => $.acceptance.state_missing);

  return (
    <div
      data-testid="acceptance-criterion"
      data-state={state}
      className={cn("flex flex-col gap-1 rounded-md border p-2", state === "satisfied" ? "border-border" : "border-warning/50")}
    >
      <div className="flex items-start gap-2">
        <span className="mt-0.5 shrink-0" title={stateLabel} aria-label={stateLabel}>
          {state === "satisfied" ? (
            <Check className="size-3.5 text-success" />
          ) : state === "pending_human" ? (
            <Clock className="size-3.5 text-warning" />
          ) : (
            <Circle className="size-3.5 text-faint-foreground" />
          )}
        </span>
        <span className="min-w-0 flex-1 whitespace-pre-wrap">{criterion.text}</span>
        <button
          type="button"
          className="shrink-0 text-faint-foreground hover:text-foreground"
          aria-label={t(($) => $.acceptance.remove)}
          onClick={onRemove}
        >
          <X className="size-3.5" />
        </button>
      </div>
      {criterion.proof_type && (
        <div className="truncate pl-5 text-muted-foreground" title={criterion.proof_ref}>
          {proofTypeLabel(criterion.proof_type, t)}
          {criterion.proof_ref && <span> · {criterion.proof_ref}</span>}
          {state === "pending_human" && <span> · {t(($) => $.acceptance.state_pending_human)}</span>}
        </div>
      )}
      {editing ? (
        <div className="flex flex-col gap-1 pl-5">
          <select
            aria-label={t(($) => $.acceptance.proof_type)}
            value={proofType}
            onChange={(e) => setProofType(e.target.value)}
            className="h-7 rounded-md border bg-background px-2 text-caption"
          >
            {ACCEPTANCE_PROOF_TYPES.filter((p) => p !== "human_validation").map((p) => (
              <option key={p} value={p}>
                {proofTypeLabel(p, t)}
              </option>
            ))}
          </select>
          <Input
            aria-label={t(($) => $.acceptance.ref_placeholder)}
            placeholder={t(($) => $.acceptance.ref_placeholder)}
            value={ref}
            onChange={(e) => setRef(e.target.value)}
            className="h-7"
          />
          <div className="flex gap-1">
            <Button type="button" size="sm" disabled={prove.isPending || ref.trim() === ""} onClick={() => send(proofType, ref.trim())}>
              {t(($) => $.acceptance.save)}
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
              {t(($) => $.acceptance.cancel)}
            </Button>
          </div>
        </div>
      ) : (
        state !== "satisfied" && (
          <div className="flex gap-2 pl-5">
            <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => setEditing(true)}>
              {t(($) => $.acceptance.attach)}
            </button>
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground"
              disabled={prove.isPending}
              onClick={() => send("human_validation")}
            >
              {t(($) => $.acceptance.validate)}
            </button>
          </div>
        )
      )}
    </div>
  );
}

function proofTypeLabel(type: string, t: ReturnType<typeof useT<"issues">>["t"]): string {
  switch (type) {
    case "test":
      return t(($) => $.acceptance.types.test);
    case "file":
      return t(($) => $.acceptance.types.file);
    case "screenshot":
      return t(($) => $.acceptance.types.screenshot);
    case "url":
      return t(($) => $.acceptance.types.url);
    case "human_validation":
      return t(($) => $.acceptance.types.human_validation);
    default:
      return type;
  }
}
