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

/** How a Brain note was used (JEF-413). Loose: a kind added server-side still parses. */
export type WorkspaceNoteUsageKind = "injected" | "retrieved" | "opened" | "viewed" | (string & {});

/** One run that used a note. A private chat run keeps no task or issue. */
export interface WorkspaceNoteUsageRun {
  task_id: string;
  agent_id: string;
  agent_name: string;
  issue_id: string;
  issue_identifier: string;
  kinds: WorkspaceNoteUsageKind[];
  first_at: string;
  private: boolean;
}

/** GET /api/workspace/notes/{id}/usage. People are counted, never named. */
export interface WorkspaceNoteUsage {
  counts: { injected: number; retrieved: number; opened: number; viewed: number };
  runs_count: number;
  viewers_count: number;
  last_used_at: string | null;
  runs: WorkspaceNoteUsageRun[];
}

/** One note a run used. `deleted` notes keep only their id. */
export interface TaskNoteUsageItem {
  note_id: string;
  title: string;
  revision: number;
  kinds: WorkspaceNoteUsageKind[];
  channels: string[];
  first_at: string | null;
  deleted: boolean;
}

export interface TaskNoteUsageResponse {
  notes: TaskNoteUsageItem[];
}
