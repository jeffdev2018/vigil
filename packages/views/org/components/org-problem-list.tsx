"use client";

import { AlertTriangle } from "lucide-react";
import type { OrgProblem } from "@multica/core/org/validate";
import { useT } from "../../i18n";
import { orgProblemParams } from "../labels";

/** The validation messages the canvas shows while the user acts. The server
 *  stays the authority at save; these only say what it would refuse. */
export function OrgProblemList({ problems, compact }: { problems: OrgProblem[]; compact?: boolean }) {
  const { t } = useT("org");
  if (problems.length === 0) return null;
  return (
    <ul role="alert" data-testid="org-problems" className="flex flex-col gap-0.5 text-caption text-warning">
      {problems.map((p, i) => (
        <li key={`${p.code}:${p.unit_id ?? ""}:${i}`} className="flex items-start gap-1">
          {!compact && <AlertTriangle className="mt-0.5 size-3 shrink-0" aria-hidden />}
          {/* autonomy_over_trust / deciders_missing carry raw domain values
           *  (an autonomy notch, a trust mode, decider classes); route them
           *  through labels.ts so the message never shows a bare enum. */}
          <span>{t(($) => $.problem[p.code], orgProblemParams(t, p))}</span>
        </li>
      ))}
    </ul>
  );
}
