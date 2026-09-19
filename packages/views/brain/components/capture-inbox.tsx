"use client";

import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Inbox } from "lucide-react";
import type { BrainCapture, BrainCaptureStatus } from "@multica/core/types";
import { brainCapturesOptions } from "@multica/core/brain/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { CollectionPageState } from "../../layout/collection-page";
import { CaptureCard } from "./capture-card";
import { CaptureComposer, useFileCapture } from "./capture-composer";
import { OrganizeDialog, type OrganizeAction } from "./organize-dialog";

const FILTERS: BrainCaptureStatus[] = ["raw", "organized", "discarded"];

export function CaptureInbox({ wsId }: { wsId: string }) {
  const { t } = useT("brain");
  const [status, setStatus] = useState<BrainCaptureStatus>("raw");
  const [organizing, setOrganizing] = useState<{
    capture: BrainCapture;
    action: OrganizeAction;
  } | null>(null);
  // Sticky per page visit: the server answers 503 on suggest when no model is
  // configured, and that is a deployment fact, not a per-capture failure.
  const [noModel, setNoModel] = useState(false);
  const [dragging, setDragging] = useState(false);

  const file = useFileCapture(wsId);
  const captures = useQuery(brainCapturesOptions(wsId, status));
  const items = captures.data?.captures ?? [];

  const handleOrganize = useCallback(
    (capture: BrainCapture, action: OrganizeAction) =>
      setOrganizing({ capture, action }),
    [],
  );

  return (
    <div
      className={cn(
        "flex min-h-0 flex-1 flex-col",
        dragging && "outline outline-2 -outline-offset-2 outline-primary",
      )}
      onDragOver={(e) => {
        // Only a file drag; a text selection dropped here means nothing.
        if (!e.dataTransfer.types.includes("Files")) return;
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        if (!e.dataTransfer.types.includes("Files")) return;
        e.preventDefault();
        setDragging(false);
        const dropped = e.dataTransfer.files[0];
        if (dropped) void file.capture(dropped);
      }}
    >
      <CaptureComposer wsId={wsId} file={file} />

      <div
        role="group"
        aria-label={t(($) => $.inbox.filter_label)}
        className="flex shrink-0 flex-wrap items-center gap-1 border-b px-4 py-2"
      >
        {FILTERS.map((key) => (
          <button
            key={key}
            type="button"
            aria-pressed={status === key}
            onClick={() => setStatus(key)}
            className={cn(
              "rounded-md px-2 py-0.5 text-caption transition-colors",
              // Weight and text colour carry the selection; hover only moves
              // the background, so a hovered active filter still reads active.
              status === key
                ? "bg-accent font-medium text-foreground"
                : "text-muted-foreground hover:bg-accent",
            )}
          >
            {t(($) =>
              key === "raw" ? $.inbox.raw : key === "organized" ? $.inbox.organized : $.inbox.discarded,
            )}
          </button>
        ))}
        {dragging ? (
          <span className="ml-auto text-caption text-muted-foreground">
            {t(($) => $.capture.drop_hint)}
          </span>
        ) : null}
      </div>

      {noModel ? (
        <p role="status" className="shrink-0 border-b px-4 py-1.5 text-caption text-muted-foreground">
          {t(($) => $.card.no_model)}
        </p>
      ) : null}

      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {captures.isLoading ? (
          <div className="flex flex-col gap-2" aria-busy="true">
            <span className="sr-only">{t(($) => $.inbox.loading)}</span>
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-24 w-full rounded-lg" />
            ))}
          </div>
        ) : captures.isError ? (
          <CollectionPageState
            icon={Inbox}
            title={t(($) => $.inbox.load_error)}
            tone="destructive"
            role="alert"
          />
        ) : items.length === 0 ? (
          <CollectionPageState
            icon={Inbox}
            title={t(($) =>
              status === "raw"
                ? $.inbox.empty_raw_title
                : status === "organized"
                  ? $.inbox.empty_organized_title
                  : $.inbox.empty_discarded_title,
            )}
            description={t(($) =>
              status === "raw"
                ? $.inbox.empty_raw_description
                : status === "organized"
                  ? $.inbox.empty_organized_description
                  : $.inbox.empty_discarded_description,
            )}
          />
        ) : (
          <ul className="flex flex-col gap-2">
            {items.map((capture) => (
              <CaptureCard
                key={capture.id}
                wsId={wsId}
                capture={capture}
                noModel={noModel}
                onNoModel={() => setNoModel(true)}
                onOrganize={handleOrganize}
              />
            ))}
          </ul>
        )}
      </div>

      {organizing ? (
        <OrganizeDialog
          wsId={wsId}
          capture={organizing.capture}
          action={organizing.action}
          onClose={() => setOrganizing(null)}
        />
      ) : null}
    </div>
  );
}
