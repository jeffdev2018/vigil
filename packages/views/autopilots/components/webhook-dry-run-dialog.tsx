"use client";

import { useMemo, useState } from "react";
import { CheckCircle2, Ban, AlertTriangle, FlaskConical, Loader2 } from "lucide-react";
import { useDryRunAutopilotWebhookTrigger } from "@multica/core/autopilots";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { useT } from "../../i18n";
import { dryRunSamples, parseDryRunPayload } from "./webhook-dry-run-samples";
import { useDeliveryReasonLabel } from "./delivery-reason";
import type { AutopilotTrigger, WebhookTriggerDryRunResult } from "@multica/core/types";

/**
 * Replays one sample payload through the real delivery decision — filters,
 * routing criterion (the classifier runs for real), pause state, quota — and
 * shows the verdict. Nothing is recorded: no delivery row, no run.
 *
 * `initialPayload` / `initialHeaders` let a past delivery be re-judged against
 * the trigger's CURRENT configuration ("replay as dry-run"), which is the
 * question an operator actually has after changing a filter.
 */
export function WebhookDryRunDialog({
  open,
  onOpenChange,
  autopilotId,
  trigger,
  initialPayload,
  initialHeaders,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  autopilotId: string;
  trigger: Pick<AutopilotTrigger, "id" | "provider" | "event_filters">;
  initialPayload?: string;
  initialHeaders?: Record<string, string>;
}) {
  const { t } = useT("autopilots");
  const dryRun = useDryRunAutopilotWebhookTrigger();
  const samples = useMemo(() => dryRunSamples(trigger), [trigger]);
  const [sampleId, setSampleId] = useState(() => samples[0]?.id ?? "");
  const [text, setText] = useState(
    () => initialPayload ?? samples[0]?.payload ?? "{}",
  );
  const [headers, setHeaders] = useState<Record<string, string>>(
    () => initialHeaders ?? samples[0]?.headers ?? {},
  );
  const [result, setResult] = useState<WebhookTriggerDryRunResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  const parsed = parseDryRunPayload(text);
  // A stale verdict beside an edited payload is worse than no verdict: it
  // reads as an answer to the text currently on screen.
  const dropResult = () => {
    setResult(null);
    setError(null);
  };

  const pickSample = (id: string) => {
    const sample = samples.find((s) => s.id === id);
    if (!sample) return;
    setSampleId(id);
    setText(sample.payload);
    setHeaders(sample.headers);
    dropResult();
  };

  const handleRun = async () => {
    if (!parsed.ok) return;
    dropResult();
    try {
      setResult(
        await dryRun.mutateAsync({
          autopilotId,
          triggerId: trigger.id,
          payload: parsed.value,
          headers,
        }),
      );
    } catch (e: unknown) {
      setError(
        e instanceof Error && e.message
          ? e.message
          : t(($) => $.dry_run.toast_failed),
      );
    }
  };

  const parseError = !parsed.ok && !parsed.empty
    ? parsed.message === "object_or_array"
      ? t(($) => $.dry_run.invalid_shape)
      : t(($) => $.dry_run.invalid_json, { message: parsed.message })
    : null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[85vh] overflow-y-auto">
        <DialogTitle className="flex items-center gap-2">
          <FlaskConical className="h-4 w-4 text-muted-foreground" />
          {t(($) => $.dry_run.title)}
        </DialogTitle>
        <div className="min-w-0 space-y-4 pt-1">
          <p className="text-caption text-muted-foreground leading-relaxed">
            {t(($) => $.dry_run.description)}
          </p>

          {samples.length > 1 && (
            <div className="space-y-1.5">
              <span className="text-caption font-medium text-muted-foreground">
                {t(($) => $.dry_run.sample_label)}
              </span>
              <SegmentedToggle
                value={sampleId}
                onChange={pickSample}
                buttonClassName="px-2 py-1 text-caption font-mono"
                options={samples.map((s) => [s.id, s.id] as [string, string])}
              />
            </div>
          )}

          <div className="space-y-1.5">
            <label
              htmlFor="dry-run-payload"
              className="text-caption font-medium text-muted-foreground"
            >
              {t(($) => $.dry_run.payload_label)}
            </label>
            <Textarea
              id="dry-run-payload"
              value={text}
              onChange={(e) => {
                setText(e.target.value);
                dropResult();
              }}
              rows={10}
              spellCheck={false}
              className="font-mono text-caption"
            />
            {parseError !== null && (
              <p className="text-caption text-destructive break-all">{parseError}</p>
            )}
            {Object.keys(headers).length > 0 && (
              <p className="text-micro text-muted-foreground font-mono break-all">
                {Object.entries(headers)
                  .map(([k, v]) => `${k}: ${v}`)
                  .join(" · ")}
              </p>
            )}
          </div>

          {error !== null && (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-caption text-destructive break-all">
              {error}
            </div>
          )}
          {result !== null && <DryRunVerdict result={result} />}

          <div className="flex justify-end pt-1">
            <Button
              size="sm"
              onClick={handleRun}
              disabled={!parsed.ok || dryRun.isPending}
            >
              {dryRun.isPending ? (
                <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" />
              ) : (
                <FlaskConical className="h-3.5 w-3.5 mr-1" />
              )}
              {dryRun.isPending
                ? t(($) => $.dry_run.running)
                : t(($) => $.dry_run.run)}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// The verdict reads as one sentence with its evidence underneath: an operator
// asking "would this run?" wants the answer before the reasoning.
function DryRunVerdict({ result }: { result: WebhookTriggerDryRunResult }) {
  const { t } = useT("autopilots");
  // "unreadable" is the client-side sentinel for a response that failed schema
  // validation — never presented as a routing decision the server made.
  const unreadable = result.reason_code === "unreadable";
  const reasonLabel = useDeliveryReasonLabel(unreadable ? null : result.reason_code);
  const Icon = unreadable ? AlertTriangle : result.would_run ? CheckCircle2 : Ban;

  return (
    <div
      className={cn(
        "space-y-2 rounded-md border px-3 py-2.5",
        unreadable
          ? "border-amber-500/30 bg-amber-500/5"
          : result.would_run
            ? "border-emerald-500/30 bg-emerald-500/5"
            : "bg-muted/40",
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <Icon
          className={cn(
            "h-4 w-4 shrink-0",
            unreadable
              ? "text-amber-500"
              : result.would_run
                ? "text-emerald-500"
                : "text-muted-foreground",
          )}
        />
        <span className="text-body font-medium">
          {unreadable
            ? t(($) => $.dry_run.verdict_unreadable)
            : result.would_run
              ? t(($) => $.dry_run.verdict_would_run)
              : t(($) => $.dry_run.verdict_blocked)}
        </span>
        {reasonLabel !== null && <Badge variant="secondary">{reasonLabel}</Badge>}
        {result.event !== "" && (
          <code className="rounded bg-muted px-2 py-0.5 text-caption font-mono">
            {result.event}
          </code>
        )}
      </div>
      {result.explanation !== "" && (
        <p className="text-caption text-muted-foreground break-words">
          {result.explanation}
        </p>
      )}
      {result.matched_filters.length > 0 && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.dry_run.matched_filters)}{" "}
          <span className="font-mono text-foreground">
            {result.matched_filters
              .map((f) =>
                f.actions && f.actions.length > 0
                  ? `${f.event}:${f.actions.join(",")}`
                  : f.event,
              )
              .join(" · ")}
          </span>
        </p>
      )}
    </div>
  );
}
