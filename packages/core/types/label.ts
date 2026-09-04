/**
 * Issue labels — workspace-scoped, applied as many-to-many to issues.
 *
 * Labels are lightweight metadata (name + color) distinct from projects:
 * projects group related work, labels are cross-cutting tags (bug, feature,
 * performance, …). Colors are normalized to lowercase `#RRGGBB`.
 */
export type LabelResourceType = "issue" | "agent" | "skill";

// Module ownership (K33).
export interface ModuleOwnershipRule {
  id: string;
  workspace_id: string;
  path_pattern: string | null;
  label_id: string | null;
  owner_user_id: string;
  referent_agent_id: string | null;
  priority: number;
  created_at: string;
}

export interface OwnershipSuggestion {
  rule_id: string;
  owner_user_id: string;
  referent_agent_id: string | null;
  /** "label:<id>" or "path:<path>". */
  matched: string;
  pattern: string;
}

export interface Label {
  id: string;
  workspace_id: string;
  resource_type?: LabelResourceType;
  name: string;
  description?: string;
  /** Normalized lowercase hex color, e.g. `#3b82f6`. */
  color: string;
  usage_count?: number;
  created_at: string;
  updated_at: string;
}

export interface CreateLabelRequest {
  resource_type?: LabelResourceType;
  name: string;
  description?: string;
  color: string;
}

export interface UpdateLabelRequest {
  name?: string;
  description?: string;
  color?: string;
}

export interface ListLabelsResponse {
  labels: Label[];
  total: number;
}

export interface IssueLabelsResponse {
  labels: Label[];
  issue_revision?: number;
}

export type ResourceLabelsResponse = IssueLabelsResponse;
