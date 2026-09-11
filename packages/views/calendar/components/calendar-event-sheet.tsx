"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Pencil, X as CancelIcon, Check, HelpCircle } from "lucide-react";
import {
  calendarEventDetailOptions,
  useCancelCalendarEvent,
  useRespondCalendarEvent,
  canonicalCalendarResponse,
} from "@multica/core/calendar-events";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import type { CalendarParticipant } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { cn } from "@multica/ui/lib/utils";
import { tKnown, useT } from "../../i18n";

const STATUS_TONE: Record<string, string> = {
  proposed: "bg-warning/10 text-warning",
  scheduled: "bg-success/10 text-success",
  cancelled: "bg-muted text-muted-foreground",
};

const RESPONSE_ICON: Record<string, typeof Check> = {
  accepted: Check,
  declined: CancelIcon,
  tentative: HelpCircle,
  pending: HelpCircle,
};

function formatRange(startsAt: string, endsAt: string, tz: string, allDay: boolean): string {
  const opts: Intl.DateTimeFormatOptions = allDay
    ? { month: "short", day: "numeric", timeZone: tz }
    : { month: "short", day: "numeric", hour: "numeric", minute: "2-digit", timeZone: tz };
  const start = new Date(startsAt).toLocaleString(undefined, opts);
  const end = new Date(endsAt).toLocaleString(undefined, allDay ? opts : { hour: "numeric", minute: "2-digit", timeZone: tz });
  return `${start} – ${end} (${tz})`;
}

export function CalendarEventSheet({
  eventId,
  onClose,
  onEdit,
}: {
  eventId: string;
  onClose: () => void;
  onEdit: () => void;
}) {
  const { t } = useT("calendar-events");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: event, isLoading } = useQuery(calendarEventDetailOptions(wsId, eventId));
  const cancelEvent = useCancelCalendarEvent(wsId);
  const respond = useRespondCalendarEvent(wsId);
  const [confirmCancel, setConfirmCancel] = useState(false);

  const myParticipant: CalendarParticipant | undefined = event?.participants.find(
    (participant) => participant.type === "member" && participant.id === userId,
  );

  const runCancel = () => {
    cancelEvent.mutate(eventId, {
      onError: () => toast.error(t(($) => $.sheet.cancel_error)),
      onSettled: () => setConfirmCancel(false),
      onSuccess: () => onClose(),
    });
  };

  const runRespond = (response: "accepted" | "declined" | "tentative") => {
    respond.mutate(
      { id: eventId, response },
      { onError: () => toast.error(t(($) => $.sheet.respond_error)) },
    );
  };

  return (
    <>
      <Sheet open onOpenChange={(open) => { if (!open) onClose(); }}>
        <SheetContent side="right" className="w-[400px] overflow-y-auto p-4">
          {isLoading || !event ? (
            <div className="flex flex-1 items-center justify-center">
              <Spinner className="size-5" />
            </div>
          ) : (
            <div className="flex flex-col gap-4">
              <SheetHeader className="p-0">
                <div className="flex items-center gap-2">
                  <SheetTitle
                    className={cn(event.status === "cancelled" && "line-through text-muted-foreground")}
                  >
                    {event.title}
                  </SheetTitle>
                  <Badge className={STATUS_TONE[event.status] ?? STATUS_TONE.scheduled}>
                    {tKnown(t, "status", event.status, event.status)}
                  </Badge>
                </div>
              </SheetHeader>

              <p className="text-caption text-muted-foreground">
                {formatRange(event.starts_at, event.ends_at, event.timezone, event.all_day)}
              </p>
              {event.location && <p className="text-body">{event.location}</p>}
              {event.description && (
                <p className="whitespace-pre-wrap text-body text-muted-foreground">{event.description}</p>
              )}

              {event.issue_id && (
                <AppLink
                  href={p.issueDetail(event.issue_id)}
                  className="text-body text-primary hover:underline"
                >
                  {event.issue_identifier || t(($) => $.sheet.view_issue)}
                </AppLink>
              )}

              {event.status === "proposed" && event.issue_id && event.decision_id && (
                <p className="rounded-md bg-warning/10 px-2 py-1.5 text-caption text-warning">
                  {t(($) => $.sheet.awaiting_decision)}{" "}
                  <AppLink
                    href={`${p.issueDetail(event.issue_id)}#approval-${event.decision_id}`}
                    className="underline"
                  >
                    {t(($) => $.sheet.view_issue)}
                  </AppLink>
                </p>
              )}

              <div className="flex flex-col gap-1.5">
                <span className="text-caption font-medium text-muted-foreground">
                  {t(($) => $.sheet.participants)}
                </span>
                {event.participants.map((participant) => {
                  const response = canonicalCalendarResponse(participant.response);
                  const Icon = RESPONSE_ICON[response] ?? HelpCircle;
                  return (
                    <div
                      key={`${participant.type}:${participant.id}`}
                      className="flex items-center gap-2 text-body"
                    >
                      <ActorAvatar actorType={participant.type} actorId={participant.id} size="sm" />
                      <span className="min-w-0 flex-1 truncate">
                        {participant.name || participant.id}
                      </span>
                      {!participant.required && (
                        <span className="text-caption text-muted-foreground">
                          {t(($) => $.sheet.optional)}
                        </span>
                      )}
                      <Icon
                        className={cn(
                          "size-3.5 shrink-0",
                          response === "accepted" && "text-success",
                          response === "declined" && "text-destructive",
                          (response === "tentative" || response === "pending") && "text-muted-foreground",
                        )}
                      />
                      <span className="w-16 shrink-0 text-caption text-muted-foreground">
                        {t(($) => $.response[response])}
                      </span>
                    </div>
                  );
                })}
              </div>

              {myParticipant && event.status !== "cancelled" && (
                <div className="flex flex-col gap-1.5">
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.sheet.your_response)}
                  </span>
                  <div className="flex gap-1.5">
                    <Button
                      type="button"
                      size="sm"
                      variant={canonicalCalendarResponse(myParticipant.response) === "accepted" ? "default" : "outline"}
                      disabled={respond.isPending}
                      onClick={() => runRespond("accepted")}
                    >
                      {t(($) => $.sheet.respond_accept)}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant={canonicalCalendarResponse(myParticipant.response) === "tentative" ? "default" : "outline"}
                      disabled={respond.isPending}
                      onClick={() => runRespond("tentative")}
                    >
                      {t(($) => $.sheet.respond_tentative)}
                    </Button>
                    <Button
                      type="button"
                      size="sm"
                      variant={canonicalCalendarResponse(myParticipant.response) === "declined" ? "default" : "outline"}
                      disabled={respond.isPending}
                      onClick={() => runRespond("declined")}
                    >
                      {t(($) => $.sheet.respond_decline)}
                    </Button>
                  </div>
                </div>
              )}

              {event.status !== "cancelled" && (
                <div className="mt-2 flex gap-2 border-t pt-3">
                  <Button type="button" variant="outline" size="sm" onClick={onEdit}>
                    <Pencil className="size-3.5" />
                    {t(($) => $.sheet.edit)}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={() => setConfirmCancel(true)}
                  >
                    {t(($) => $.sheet.cancel_event)}
                  </Button>
                </div>
              )}
            </div>
          )}
        </SheetContent>
      </Sheet>

      <Dialog open={confirmCancel} onOpenChange={setConfirmCancel}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.sheet.cancel_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.sheet.cancel_description)}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={() => setConfirmCancel(false)}>
              {t(($) => $.sheet.keep)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={cancelEvent.isPending}
              onClick={runCancel}
            >
              {t(($) => $.sheet.cancel_confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
