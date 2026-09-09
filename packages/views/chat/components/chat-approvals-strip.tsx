"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, ShieldCheck } from "lucide-react";
import type { ApprovalItem } from "@multica/core/approvals";
import { ApprovalCard } from "../../approvals/approval-card";
import { useT } from "../../i18n";

/**
 * Inline approvals (OS plan, chantier 3): the pending asks THIS agent filed,
 * above the composer so a human already in the conversation can settle them
 * without leaving to the inbox or the issue. Renders nothing when the agent
 * has nothing pending.
 */
export function ChatApprovalsStrip({ approvals, wsId }: { approvals: ApprovalItem[]; wsId: string }) {
  const { t } = useT("chat");
  const [open, setOpen] = useState(true);
  if (approvals.length === 0) return null;
  return (
    <div data-testid="chat-approvals-strip" className="flex flex-col gap-1.5 border-t px-3 py-2">
      <button
        type="button"
        className="flex items-center gap-1.5 self-start text-caption font-medium text-muted-foreground outline-none hover:text-foreground"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        {open ? <ChevronDown className="size-3.5" aria-hidden="true" /> : <ChevronRight className="size-3.5" aria-hidden="true" />}
        <ShieldCheck className="size-3.5 text-warning" aria-hidden="true" />
        {t(($) => $.approvals.strip_title, { count: approvals.length })}
      </button>
      {open ? (
        <div className="flex max-h-48 flex-col gap-1.5 overflow-y-auto">
          {approvals.map((a) => (
            <ApprovalCard key={a.id} approval={a} wsId={wsId} showIssue compact />
          ))}
        </div>
      ) : null}
    </div>
  );
}
