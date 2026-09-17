"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ScrollText } from "lucide-react";
import { agentTasksOptions } from "@multica/core/agents/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { TranscriptButton } from "./transcript-button";

/**
 * Opens the transcript of one run known only by its agent and task ids.
 *
 * There is no single-task endpoint to resolve the id with, so the button reads
 * the agent's run list — the same cached query the agent's Activity tab uses —
 * and finds the row there. A run that has aged out of that list (or whose
 * agent was deleted) renders nothing rather than a control that would open an
 * empty dialog.
 */
export function OpenRunButton({
  wsId,
  agentId,
  taskId,
  label,
}: {
  wsId: string;
  agentId: string;
  taskId: string;
  /** Localized by the caller: the button and its dialog title. */
  label: string;
}) {
  const [open, setOpen] = useState(false);
  const { data: tasks = [] } = useQuery(agentTasksOptions(wsId, agentId));
  // Workspace-wide agent list: already in cache on every dashboard surface,
  // and the dialog header needs a name the task row does not carry.
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const task = tasks.find((candidate) => candidate.id === taskId);
  if (!task) return null;
  const agentName = agents.find((agent) => agent.id === agentId)?.name ?? "";
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <ScrollText aria-hidden="true" className="size-3.5" />
        {label}
      </Button>
      <TranscriptButton
        task={task}
        agentName={agentName}
        renderButton={false}
        title={label}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  );
}
