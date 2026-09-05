/** Who produced a Brain note. */
export type WorkspaceNoteSource = "manual" | "agent" | "curation";

/** One workspace Brain note. */
export interface WorkspaceNote {
  id: string;
  workspace_id: string;
  title: string;
  content: string;
  tags: string[];
  /** Loose on purpose: a source added server-side must not fail the parse. */
  source: WorkspaceNoteSource | (string & {});
  /** The run that saved this note, when an agent wrote it. */
  source_task_id?: string | null;
  source_agent_id?: string | null;
  pinned: boolean;
  archived_at?: string | null;
  /** Set on an archived note the curation pass folded into another one. */
  merged_into?: string | null;
  created_by_type: string;
  created_by_id?: string | null;
  /** Optimistic-concurrency token: send it back on PATCH. */
  revision: number;
  created_at: string;
  updated_at: string;
}

export interface WorkspaceNotesResponse {
  items: WorkspaceNote[];
  /** Every live tag in the workspace, for the filter chips. */
  tags: string[];
}

export interface CreateWorkspaceNoteInput {
  title: string;
  content?: string;
  tags?: string[];
  pinned?: boolean;
}

export interface UpdateWorkspaceNoteInput {
  title?: string;
  content?: string;
  tags?: string[];
  pinned?: boolean;
  revision: number;
}
