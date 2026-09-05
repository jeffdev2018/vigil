"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { toast } from "sonner";
import {
  projectMembersOptions,
  useSetProjectMemberRole,
  type ProjectMemberRole,
  type ProjectRole,
} from "@multica/core/access";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../../i18n";

const ROLE_ORDER: ProjectRole[] = ["viewer", "contributor", "admin"];
const INHERIT = "__inherit";

/**
 * Members and agents of a project with their effective role (K60).
 *
 * A project role can only restrict: the select never offers a role above the
 * subject's ceiling (its workspace role). Workspace owners/admins and project
 * admins may edit; role names are schema identifiers and stay in English.
 */
export function ProjectMembersSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const currentMember = useCurrentMember(wsId);
  const [open, setOpen] = useState(true);
  const { data } = useQuery(projectMembersOptions(wsId, projectId));
  const setRole = useSetProjectMemberRole(wsId, projectId);
  const members = data?.members ?? [];

  const me = members.find((m) => m.subject_type === "member" && m.subject_id === userId);
  const canEdit =
    currentMember.role === "owner" ||
    currentMember.role === "admin" ||
    me?.effective_role === "admin";

  const change = (m: ProjectMemberRole, value: string) =>
    setRole.mutate(
      { subjectType: m.subject_type, subjectId: m.subject_id, role: value === INHERIT ? null : (value as ProjectRole) },
      {
        onSuccess: () => toast.success(t(($) => $.members.updated)),
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.members.update_failed)),
      },
    );

  return (
    <div data-testid="project-members-section">
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.members.section)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2 space-y-2">
          <p className="px-2 text-caption text-muted-foreground">{t(($) => $.members.description)}</p>
          {members.length === 0 ? (
            <p className="px-2 text-caption text-muted-foreground">{t(($) => $.members.empty)}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-body">
                <thead>
                  <tr className="text-left text-caption text-muted-foreground">
                    <th className="px-2 py-1 font-medium">{t(($) => $.members.columns.name)}</th>
                    <th className="px-2 py-1 font-medium">{t(($) => $.members.columns.workspace_role)}</th>
                    <th className="px-2 py-1 font-medium">{t(($) => $.members.columns.role)}</th>
                  </tr>
                </thead>
                <tbody>
                  {members.map((m) => {
                    const options = ROLE_ORDER.slice(0, ROLE_ORDER.indexOf(m.ceiling) + 1);
                    return (
                      <tr key={`${m.subject_type}:${m.subject_id}`} data-testid="project-member-row">
                        <td className="px-2 py-1">
                          <span className="min-w-0 truncate">{m.name || m.email || m.subject_id}</span>
                          {m.subject_type === "agent" && (
                            <span className="ml-2 text-caption text-muted-foreground">{t(($) => $.members.agent)}</span>
                          )}
                        </td>
                        <td className="px-2 py-1 font-mono text-caption text-muted-foreground">{m.workspace_role}</td>
                        <td className="px-2 py-1">
                          <div className="flex items-center gap-2">
                            <Badge variant="secondary" className="font-mono">{m.effective_role}</Badge>
                            <Badge variant="outline">
                              {m.source === "override" ? t(($) => $.members.override) : t(($) => $.members.inherited)}
                            </Badge>
                            {canEdit && (
                              <select
                                aria-label={t(($) => $.members.columns.role)}
                                className="h-7 rounded-md border bg-background px-2 text-caption"
                                value={m.override ?? INHERIT}
                                disabled={setRole.isPending}
                                onChange={(e) => change(m, e.target.value)}
                              >
                                <option value={INHERIT}>{t(($) => $.members.inherit_option, { role: m.ceiling })}</option>
                                {options.map((r) => (
                                  <option key={r} value={r}>{r}</option>
                                ))}
                              </select>
                            )}
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
          {!canEdit && !currentMember.isLoading && (
            <p className="px-2 text-caption text-muted-foreground">{t(($) => $.members.read_only)}</p>
          )}
        </div>
      )}
    </div>
  );
}
