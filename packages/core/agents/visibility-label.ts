import type { AgentVisibility } from "../types";

/**
 * Display labels for agent visibility. The DB stores `private` as the value
 * but the UI surface name is "Personal" — it reads better next to "Workspace"
 * and matches the wording used in the access picker.
 */
export const VISIBILITY_LABEL: Record<AgentVisibility, string> = {
  workspace: "Workspace",
  private: "Personal",
};

/**
 * Descriptions for the visibility CHOICE (create dialog / picker), where
 * "Personal" is submitted as `permission_mode: "private"` — owner-only, with
 * no workspace-admin bypass since MUL-3963 (`canInvokeAgent` in
 * `server/internal/handler/agent_access.go`). The older
 * "…and workspace admins…" copy predates that gate and is no longer true.
 */
export const VISIBILITY_DESCRIPTION: Record<AgentVisibility, string> = {
  workspace: "All members can assign",
  private: "Only you can assign",
};

// A hardcoded-English VISIBILITY_TOOLTIP Record used to live here and was
// rendered as-is (no t()) by agents-page.tsx's read-only lock-icon tooltip.
// Removed: that call site now uses the canonical VisibilityBadge component
// (packages/views/agents/components/visibility-badge.tsx), whose
// useT("agents") visibility.<value>.tooltip keys already cover this text in
// all 5 locales. VISIBILITY_LABEL/VISIBILITY_DESCRIPTION above have no
// runtime consumer left (grep confirms), so they're untouched here rather
// than fixed speculatively.

export function visibilityLabel(v: AgentVisibility): string {
  return VISIBILITY_LABEL[v];
}
