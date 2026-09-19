"use client";

import type { OrgTemplate } from "@multica/core/types";
import { useT } from "../../i18n";

// The catalogue of the seven structures, shared by the wizard, the org page and
// the project section.
export function OrgTemplateCards({ templates, onPick, disabled }: { templates: OrgTemplate[]; onPick: (t: OrgTemplate) => void; disabled?: boolean }) {
  const { t } = useT("org");
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {templates.map((tpl) => (
        <button
          key={tpl.composite === true ? "composite" : tpl.model}
          type="button"
          data-testid="org-template"
          disabled={disabled}
          onClick={() => onPick(tpl)}
          className="flex flex-col items-start gap-1 rounded-md border p-3 text-left hover:bg-accent/70 disabled:opacity-50"
        >
          <span className="text-body font-medium">{tpl.composite === true ? t(($) => $.new.composite_title) : t(($) => $.model[tpl.model])}</span>
          <span className="text-caption text-muted-foreground">{tpl.pattern}</span>
          <span className="text-caption">{tpl.description}</span>
          <span className="text-caption text-muted-foreground">{t(($) => $.new.runs_per_issue, { count: tpl.coordination_runs_per_issue })}</span>
        </button>
      ))}
    </div>
  );
}
