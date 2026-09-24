"use client";

import { Check, ChevronDown, FlaskConical, History, LayoutGrid, MoreHorizontal, Network, Plus, Save } from "lucide-react";
import type { OrgStatus, OrgStructure } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { orgModelLabel } from "../labels";

/** A status is read from its word and its dot; the colour only speeds that up. */
export const ORG_STATUS_TONE: Record<OrgStatus, string> = {
  draft: "bg-muted text-muted-foreground",
  active: "bg-success-subtle text-success-subtle-foreground",
  paused: "bg-warning-subtle text-warning-subtle-foreground",
  dissolved: "bg-destructive-subtle text-destructive-subtle-foreground",
};

export function OrgStatusPill({ status, className }: { status: OrgStatus; className?: string }) {
  const { t } = useT("org");
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2 py-px text-caption font-medium",
        ORG_STATUS_TONE[status] ?? ORG_STATUS_TONE.draft,
        className,
      )}
    >
      <span aria-hidden="true" className="size-1.5 rounded-full bg-current" />
      {t(($) => $.status[status]) || status}
    </span>
  );
}

export type OrgBarAction = "history" | "transfer" | "activate" | "pause" | "resume" | "dissolve" | "delete";

/**
 * The page's one bar. It replaces six stacked rows — page header, structure
 * picker, back link, tabs, save strip and sub-tabs — with a single line: which
 * organization this is, what state it is in, and the few things to do next.
 * Everything else lives in the inspector beside the chart.
 */
export function OrgBar({
  structure,
  structures,
  scopeOf,
  dirty,
  saving,
  canEdit,
  canPublish,
  testing,
  onSelectStructure,
  onBrowse,
  onCreate,
  onCatalog,
  onToggleTest,
  onDiscard,
  onPublish,
  onAction,
}: {
  structure: OrgStructure;
  structures: OrgStructure[];
  scopeOf: (s: OrgStructure) => string;
  dirty: boolean;
  saving: boolean;
  /** Owner or admin: the server refuses every write to anyone else, so the controls are not offered. */
  canEdit: boolean;
  canPublish: boolean;
  testing: boolean;
  onSelectStructure: (id: string) => void;
  onBrowse: () => void;
  onCreate: () => void;
  onCatalog: () => void;
  onToggleTest: () => void;
  onDiscard: () => void;
  onPublish: () => void;
  onAction: (action: OrgBarAction) => void;
}) {
  const { t } = useT("org");
  const status = structure.status;
  const live = status === "active";

  return (
    <PageHeader className="gap-3">
      <Network aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              data-testid="org-structure-picker"
              className="flex min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 hover:bg-surface-hover"
            >
              <h1 className="truncate text-body font-semibold">{structure.name}</h1>
              <ChevronDown aria-hidden="true" className="size-3.5 shrink-0 text-faint-foreground" />
            </button>
          }
        />
        <DropdownMenuContent align="start" className="w-80">
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.bar.organizations)}</DropdownMenuLabel>
            {structures.map((s) => (
              <DropdownMenuItem key={s.id} onClick={() => onSelectStructure(s.id)}>
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{s.name}</span>
                  <span className="block truncate text-caption text-muted-foreground">
                    {scopeOf(s)} · {orgModelLabel(t, s.model)}
                  </span>
                </span>
                {s.id === structure.id ? (
                  <Check className="text-muted-foreground" />
                ) : (
                  <OrgStatusPill status={s.status} className="px-1.5" />
                )}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onBrowse}>
            <LayoutGrid />
            {t(($) => $.bar.browse)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={onCatalog}>
            <Network />
            {t(($) => $.bar.catalog)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={onCreate}>
            <Plus />
            {t(($) => $.bar.create)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <div className="hidden min-w-0 items-center gap-2.5 text-caption text-muted-foreground md:flex">
        <span className="truncate">{orgModelLabel(t, structure.model)}</span>
        <OrgStatusPill status={status} />
        <span role="status" className="flex items-center gap-1.5 whitespace-nowrap">
          {dirty ? (
            <>
              <span aria-hidden="true" className="size-1.5 rounded-full bg-warning" />
              {t(($) => $.bar.unpublished)}
            </>
          ) : (
            t(($) => $.bar.saved, { n: structure.revision })
          )}
        </span>
      </div>

      <div className="ml-auto flex shrink-0 items-center gap-1.5">
        <Button
          variant={testing ? "secondary" : "outline"}
          size="sm"
          aria-pressed={testing}
          onClick={onToggleTest}
          className={cn(testing && "text-brand")}
        >
          <FlaskConical />
          <span className="hidden sm:inline">{t(($) => $.bar.test)}</span>
        </Button>
        {canEdit && dirty && (
          <Button variant="ghost" size="sm" disabled={saving} onClick={onDiscard}>
            {t(($) => $.bar.discard)}
          </Button>
        )}
        {canEdit && dirty && status !== "dissolved" && (
          <Button size="sm" disabled={!canPublish || saving} onClick={onPublish}>
            <Save />
            {live ? t(($) => $.bar.publish) : t(($) => $.bar.save)}
          </Button>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.bar.more)}>
                <MoreHorizontal />
              </Button>
            }
          />
          <DropdownMenuContent align="end" className="w-60">
            <DropdownMenuItem onClick={() => onAction("history")}>
              <History />
              {t(($) => $.bar.history)}
            </DropdownMenuItem>
            {canEdit && (
              <DropdownMenuItem onClick={() => onAction("transfer")}>{t(($) => $.bar.transfer)}</DropdownMenuItem>
            )}
            {canEdit && (status === "draft" || status === "active" || status === "paused") && (
              <>
                <DropdownMenuSeparator />
                {status === "draft" && (
                  <DropdownMenuItem disabled={dirty} onClick={() => onAction("activate")}>
                    {t(($) => $.actions.activate)}
                  </DropdownMenuItem>
                )}
                {status === "active" && (
                  <DropdownMenuItem disabled={dirty} onClick={() => onAction("pause")}>
                    {t(($) => $.actions.pause)}
                  </DropdownMenuItem>
                )}
                {status === "paused" && (
                  <DropdownMenuItem disabled={dirty} onClick={() => onAction("resume")}>
                    {t(($) => $.actions.resume)}
                  </DropdownMenuItem>
                )}
              </>
            )}
            {canEdit && (status === "active" || status === "paused" || status === "draft" || status === "dissolved") && (
              <DropdownMenuSeparator />
            )}
            {canEdit && (status === "active" || status === "paused") && (
              <DropdownMenuItem variant="destructive" disabled={dirty} onClick={() => onAction("dissolve")}>
                {t(($) => $.actions.dissolve)}
              </DropdownMenuItem>
            )}
            {canEdit && (status === "draft" || status === "dissolved") && (
              <DropdownMenuItem variant="destructive" disabled={dirty} onClick={() => onAction("delete")}>
                {t(($) => $.actions.delete)}
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </PageHeader>
  );
}
