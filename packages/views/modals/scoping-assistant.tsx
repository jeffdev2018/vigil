"use client";

import { useState } from "react";
import { Sparkles } from "lucide-react";
import { toast } from "sonner";
import { parseCriteriaLines, scopingDescription, useProposeIssueScoping } from "@multica/core/issues/scoping";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../i18n";

/**
 * Issue scoping assistant (K14): a few sentences become a draft title,
 * description (with the model's probable files as an editable list) and
 * acceptance criteria. The draft lands in the form; the human still creates
 * the issue. On failure the text stays here, and the form stays usable.
 */
export function ScopingAssistant({
  projectId,
  onDraft,
  onCriteriaChange,
}: {
  projectId?: string;
  onDraft: (draft: { title: string; description: string }) => void;
  onCriteriaChange: (criteria: string[]) => void;
}) {
  const { t } = useT("modals");
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [criteria, setCriteria] = useState("");
  const [proposed, setProposed] = useState(false);
  const propose = useProposeIssueScoping();

  const draft = () => {
    const raw_text = text.trim();
    if (raw_text === "") return;
    propose.mutate(
      { raw_text, project_id: projectId },
      {
        onSuccess: (p) => {
          onDraft({ title: p.title, description: scopingDescription(p) });
          const lines = p.acceptance_criteria.join("\n");
          setCriteria(lines);
          onCriteriaChange(parseCriteriaLines(lines));
          setProposed(true);
        },
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.create_issue.scoping_failed)),
      },
    );
  };

  if (!open) {
    return (
      <button
        type="button"
        data-testid="scoping-toggle"
        className="inline-flex items-center gap-1 text-caption text-muted-foreground hover:text-foreground"
        onClick={() => setOpen(true)}
      >
        <Sparkles className="size-3.5" />
        {t(($) => $.create_issue.scoping_toggle)}
      </button>
    );
  }

  return (
    <div data-testid="scoping-assistant" className="flex flex-col gap-2 rounded-md border p-2 text-caption">
      <Textarea
        aria-label={t(($) => $.create_issue.scoping_placeholder)}
        placeholder={t(($) => $.create_issue.scoping_placeholder)}
        value={text}
        rows={3}
        disabled={propose.isPending}
        onChange={(e) => setText(e.target.value)}
      />
      <div className="flex items-center gap-2">
        <Button type="button" size="sm" disabled={propose.isPending || text.trim() === ""} onClick={draft}>
          {propose.isPending ? t(($) => $.create_issue.scoping_drafting) : t(($) => $.create_issue.scoping_submit)}
        </Button>
        <span className="text-muted-foreground">{t(($) => $.create_issue.scoping_note)}</span>
      </div>
      {proposed && (
        <label className="flex flex-col gap-1">
          <span className="text-muted-foreground">{t(($) => $.create_issue.scoping_criteria_label)}</span>
          <Textarea
            aria-label={t(($) => $.create_issue.scoping_criteria_label)}
            value={criteria}
            rows={3}
            onChange={(e) => {
              setCriteria(e.target.value);
              onCriteriaChange(parseCriteriaLines(e.target.value));
            }}
          />
        </label>
      )}
    </div>
  );
}
