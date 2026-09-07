/**
 * A workspace's work item type catalogue (F30 / JEF-34).
 *
 * A type says what an issue IS — bug, story, epic, task, or whatever else the
 * workspace defines. Unlike a status it carries NO platform behavior: it
 * groups, it filters, and it decides which custom properties apply. An issue
 * with `issue_type: null` is UNTYPED, which is the state of every issue created
 * before F30 and of every issue nobody has classified.
 */
export interface IssueTypeEntry {
  id: string;
  workspace_id: string;
  /**
   * Stable machine handle, immutable after creation. This is the value stored
   * in `issue.issue_type`, accepted by the API, and referenced by a property's
   * type scope — so it does NOT track renames of `name`.
   */
  key: string;
  /** Human-facing label. Editable, including for the four system types. */
  name: string;
  description: string;
  /** "#rrggbb". */
  color: string;
  /** Icon catalogue key, shared with custom properties. "" means none. */
  icon: string;
  /**
   * True for the four seeded types (bug, story, epic, task). They cannot be
   * archived — the product and agent instructions reference those handles —
   * but they CAN be renamed, recoloured and reordered, because none of that
   * changes what the key means.
   */
  is_system: boolean;
  position: number;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface ListIssueTypesResponse {
  types: IssueTypeEntry[];
  total: number;
}

export interface CreateIssueTypeRequest {
  /** Optional; derived from `name` when omitted. Immutable once created. */
  key?: string;
  name: string;
  description?: string;
  color: string;
  icon?: string;
}

/** `key` is absent by design: changing it would strand every issue carrying it
 *  and every property scoped to it. */
export interface UpdateIssueTypeRequest {
  name?: string;
  description?: string;
  color?: string;
  icon?: string;
  position?: number;
}

/** One stored dependency edge, flattened to ids — what the Gantt's arrow layer
 *  reads. `from` blocks / relates-to `to`. */
export interface IssueDependencyEdge {
  id: string;
  from: string;
  to: string;
  type: string;
}
