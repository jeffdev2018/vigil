import type { WorkspaceNote } from "./workspace-note";

/** What was captured. Loose on purpose: a kind added server-side must parse. */
export type BrainCaptureKind =
  | "text"
  | "link"
  | "image"
  | "audio"
  | "file"
  | "todo";

/** Where the capture came in from. */
export type BrainCaptureOrigin =
  | "web"
  | "desktop"
  | "mobile"
  | "cli"
  | "mcp"
  | "channel"
  | "agent"
  | "api";

export type BrainCaptureStatus = "raw" | "organized" | "discarded";

export type BrainTranscriptionStatus = "none" | "pending" | "done" | "failed";

/** A note the model proposes to merge into. */
export interface BrainCaptureMergeTarget {
  id: string;
  title: string;
}

/** The model's read of one capture. Absent until a model has run. */
export interface BrainCaptureSuggestion {
  title: string;
  tags: string[];
  summary: string;
  /** Loose: an action added server-side must not fail the parse. */
  action: "note" | "merge" | "discard" | (string & {});
  merge_note?: BrainCaptureMergeTarget | null;
  candidates: BrainCaptureMergeTarget[];
  reason: string;
  model?: string;
}

/** The stored file behind an image / audio / file capture. */
export interface BrainCaptureAttachment {
  id: string;
  url: string;
  download_url: string;
  attachment_download_url?: string;
  markdown_url?: string;
  filename: string;
}

/** One inbox item: captured now, organized later. */
export interface BrainCapture {
  id: string;
  workspace_id: string;
  kind: BrainCaptureKind | (string & {});
  content: string;
  url: string;
  title_hint: string;
  attachment?: BrainCaptureAttachment | null;
  origin: BrainCaptureOrigin | (string & {});
  status: BrainCaptureStatus | (string & {});
  transcription_status: BrainTranscriptionStatus | (string & {});
  suggestion?: BrainCaptureSuggestion | null;
  /** The note this capture became, once organized. */
  note_id?: string | null;
  created_by_type: string;
  created_by_id?: string | null;
  source_task_id?: string | null;
  organized_by?: string | null;
  organized_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface BrainCapturesResponse {
  captures: BrainCapture[];
  /** The raw count, whatever the requested filter — it drives the badge. */
  raw_count: number;
}

export interface CreateBrainCaptureInput {
  kind?: BrainCaptureKind;
  content?: string;
  url?: string;
  title_hint?: string;
  origin?: BrainCaptureOrigin;
}

export interface UploadBrainCaptureInput {
  file: File | Blob;
  content?: string;
  title_hint?: string;
  origin?: BrainCaptureOrigin;
}

export interface OrganizeBrainCaptureInput {
  action: "note" | "merge" | "discard";
  title?: string;
  tags?: string[];
  content?: string;
  pinned?: boolean;
  /** Required for `merge`: the note the capture is appended to. */
  note_id?: string;
}

export interface OrganizeBrainCaptureResponse {
  capture: BrainCapture;
  note: WorkspaceNote | null;
}

/** A ranked search result: the note plus why it ranked. */
export interface WorkspaceNoteSearchHit extends WorkspaceNote {
  score: number;
  /**
   * The note's text with `<mark>` inserted around the matches. Raw note
   * content — render it through `renderSnippet`, never as HTML.
   */
  snippet: string;
  lex_rank?: number | null;
  vec_rank?: number | null;
}

export interface WorkspaceNoteSearchResponse {
  notes: WorkspaceNoteSearchHit[];
  /** True when the ranking fused a vector rank, not lexical only. */
  vector: boolean;
}
