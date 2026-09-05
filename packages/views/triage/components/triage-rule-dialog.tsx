"use client";

import { useState } from "react";
import { Scale } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { businessRulesOptions } from "@multica/core/workspace/business-rules";
import type { TriageItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { BusinessRuleForm } from "../../settings/components/business-rule-form";
import { useT } from "../../i18n";

/**
 * The longest word of the title, as the keyword a rule can key on. Short words
 * ("the", "500", "on") match everything, so they are skipped; a title made of
 * nothing but short words yields no keyword and the prefill falls back to the
 * source alone.
 */
export function titleKeyword(title: string): string {
  let best = "";
  for (const word of title.split(/[^\p{L}\p{N}_-]+/u)) {
    if (word.length > best.length) best = word;
  }
  return best.length >= 4 ? best : "";
}

/**
 * "Create a rule from this item" (K62). The queue is where a human notices
 * that a whole class of deliveries is noise; drafting the rule from the item
 * in front of them beats retyping its source and title in Settings. The form
 * is the settings one, prefilled — the rule still goes through the same
 * preview and dry-run before it can be activated.
 */
export function TriageRuleFromItem({ item, wsId }: { item: TriageItem; wsId: string }) {
  const { t } = useT("triage");
  const [open, setOpen] = useState(false);
  // Only opened dialogs need the attach point list; the settings section owns
  // the same query, so this shares its cache entry.
  const { data } = useQuery({ ...businessRulesOptions(wsId), enabled: open });
  const attachPoints = data?.attach_points?.length
    ? data.attach_points
    : ["webhook_received"];

  const keyword = titleKeyword(item.title);
  const source = item.source_name || item.source_kind;
  const prefill = keyword
    ? t(($) => $.rule.prefill, { source, keyword })
    : t(($) => $.rule.prefill_source, { source });

  return (
    <>
      <Button type="button" variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Scale aria-hidden="true" className="size-3.5" />
        {t(($) => $.rule.action)}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{t(($) => $.rule.title)}</DialogTitle>
            <DialogDescription>{t(($) => $.rule.description)}</DialogDescription>
          </DialogHeader>
          <BusinessRuleForm
            wsId={wsId}
            attachPoints={attachPoints}
            initialText={prefill}
            initialAttachPoint="webhook_received"
            onActivated={() => setOpen(false)}
          />
        </DialogContent>
      </Dialog>
    </>
  );
}
