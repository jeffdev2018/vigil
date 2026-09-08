"use client";

import { useState } from "react";
import { Trash2 } from "lucide-react";
import type { Agent } from "@multica/core/types";
import {
  MAX_RUN_GROUP_ATTEMPTS,
  MIN_RUN_GROUP_ATTEMPTS,
  useStartRunGroup,
} from "@multica/core/issues/run-group";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { useT } from "../../i18n";

interface Row {
  agentId: string;
  model: string;
}

const EMPTY_ROWS: Row[] = [{ agentId: "", model: "" }, { agentId: "", model: "" }];

/**
 * Start a race (F11). Between 2 and 5 attempts, each an agent and an optional
 * model that overrides the agent's own — the same agent twice on two models is
 * a legitimate race, so duplicates are not rejected here.
 */
export function RunGroupStartDialog({
  open,
  onOpenChange,
  issueId,
  wsId,
  agents,
  onError,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  issueId: string;
  wsId: string;
  agents: Agent[];
  onError: (err: unknown) => void;
}) {
  const { t } = useT("issues");
  const start = useStartRunGroup(wsId, issueId);
  const [rows, setRows] = useState<Row[]>(EMPTY_ROWS);
  const [note, setNote] = useState("");

  const setRow = (index: number, patch: Partial<Row>) =>
    setRows((prev) => prev.map((row, i) => (i === index ? { ...row, ...patch } : row)));
  const reset = () => { setRows(EMPTY_ROWS); setNote(""); };
  const valid = rows.length >= MIN_RUN_GROUP_ATTEMPTS && rows.every((row) => row.agentId !== "");

  const submit = () => {
    if (!valid) return;
    start.mutate(
      {
        attempts: rows.map((row) => (row.model.trim() ? { agent_id: row.agentId, model: row.model.trim() } : { agent_id: row.agentId })),
        ...(note.trim() ? { note: note.trim() } : {}),
      },
      { onError, onSuccess: () => { reset(); onOpenChange(false); } },
    );
  };

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) reset(); onOpenChange(next); }}>
      <DialogContent data-testid="run-group-start-dialog">
        <DialogHeader>
          <DialogTitle>{t(($) => $.race.start_title)}</DialogTitle>
          <DialogDescription>{t(($) => $.race.start_desc)}</DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-2 text-caption"
          onSubmit={(e) => { e.preventDefault(); submit(); }}
        >
          {rows.map((row, i) => (
            <div key={i} className="flex items-center gap-1.5">
              <select
                aria-label={t(($) => $.race.attempt_agent, { index: i + 1 })}
                className="min-w-0 flex-1 rounded-md border border-input bg-transparent px-2 py-1"
                value={row.agentId}
                onChange={(e) => setRow(i, { agentId: e.target.value })}
              >
                <option value="">{t(($) => $.race.pick_agent)}</option>
                {agents.map((agent) => (
                  <option key={agent.id} value={agent.id}>{agent.name}</option>
                ))}
              </select>
              <Input
                aria-label={t(($) => $.race.attempt_model, { index: i + 1 })}
                className="h-8 flex-1"
                placeholder={t(($) => $.race.model_placeholder)}
                value={row.model}
                onChange={(e) => setRow(i, { model: e.target.value })}
              />
              <Button
                type="button"
                size="icon"
                variant="ghost"
                aria-label={t(($) => $.race.remove_attempt, { index: i + 1 })}
                disabled={rows.length <= MIN_RUN_GROUP_ATTEMPTS}
                onClick={() => setRows((prev) => prev.filter((_, j) => j !== i))}
              >
                <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
              </Button>
            </div>
          ))}
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="self-start"
            disabled={rows.length >= MAX_RUN_GROUP_ATTEMPTS}
            onClick={() => setRows((prev) => [...prev, { agentId: "", model: "" }])}
          >
            {t(($) => $.race.add_attempt)}
          </Button>
          <Input
            aria-label={t(($) => $.race.note)}
            className="h-8"
            placeholder={t(($) => $.race.note_placeholder)}
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
          <DialogFooter>
            <Button type="button" size="sm" variant="ghost" onClick={() => onOpenChange(false)}>
              {t(($) => $.race.cancel)}
            </Button>
            <Button type="submit" size="sm" disabled={!valid || start.isPending}>
              {t(($) => $.race.start_confirm)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
