// Workspace export / import (K76).

export interface TransferSecret {
  scope: string;
  name: string;
  key: string;
  scoped: boolean;
}

export interface TransferManifest {
  format_version: number;
  exported_at: string;
  name: string;
  template: boolean;
  source: { Name: string; Slug: string };
  counts: Record<string, number>;
  secrets: TransferSecret[];
}

export interface TransferCollision {
  kind: string;
  name: string;
  existing_id: string;
}

export type TransferStrategy = "rename" | "merge" | "skip";

export interface TransferPreview {
  manifest: TransferManifest;
  collisions: TransferCollision[];
  secrets: TransferSecret[];
  strategies: TransferStrategy[];
}

export interface TransferReport {
  created: Record<string, number>;
  merged: Record<string, number>;
  skipped: TransferCollision[];
  secrets_pending: TransferSecret[];
  warnings: string[];
}

export interface TransferImportResult {
  run_id: string;
  report: TransferReport;
}

export interface TransferRun {
  id: string;
  direction: "export" | "import";
  status: "running" | "completed" | "failed";
  name: string;
  template: boolean;
  strategy: string;
  source_name: string;
  bundle_sha256: string;
  report: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
  completed_at: string | null;
}

export interface WorkspaceTemplate {
  id: string;
  name: string;
  source_name: string;
  workspace_name: string;
  report: Record<string, unknown>;
  created_at: string;
}

export interface TransferExportOptions {
  include_issues?: boolean;
  include_notes?: boolean;
  template?: boolean;
  name?: string;
}

/** Secrets typed back at import: agent name -> env key -> value. */
export type TransferSecretValues = Record<string, Record<string, string>>;
