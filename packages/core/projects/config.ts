import type { ProjectStatus, ProjectPriority } from "../types";

export const PROJECT_STATUS_ORDER: ProjectStatus[] = [
  "planned",
  "in_progress",
  "paused",
  "completed",
  "cancelled",
];

export const PROJECT_STATUS_CONFIG: Record<
  ProjectStatus,
  { label: string; color: string; dotColor: string; badgeBg: string; badgeText: string }
> = {
  planned: { label: "Planned", color: "text-muted-foreground", dotColor: "bg-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
  in_progress: { label: "In Progress", color: "text-warning-strong", dotColor: "bg-warning", badgeBg: "bg-warning-subtle", badgeText: "text-warning-strong" },
  paused: { label: "Paused", color: "text-muted-foreground", dotColor: "bg-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
  completed: { label: "Completed", color: "text-info", dotColor: "bg-info", badgeBg: "bg-info-subtle", badgeText: "text-info-strong" },
  cancelled: { label: "Cancelled", color: "text-destructive", dotColor: "bg-destructive", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
};

export const PROJECT_PRIORITY_ORDER: ProjectPriority[] = [
  "urgent",
  "high",
  "medium",
  "low",
  "none",
];

export const PROJECT_PRIORITY_CONFIG: Record<
  ProjectPriority,
  { label: string; bars: number; color: string; badgeBg: string; badgeText: string }
> = {
  urgent: { label: "Urgent", bars: 4, color: "text-destructive", badgeBg: "bg-destructive-subtle", badgeText: "text-destructive-strong" },
  high: { label: "High", bars: 3, color: "text-warning-strong", badgeBg: "bg-warning-subtle", badgeText: "text-warning-strong" },
  medium: { label: "Medium", bars: 2, color: "text-warning-strong", badgeBg: "bg-warning-subtle", badgeText: "text-warning-strong" },
  low: { label: "Low", bars: 1, color: "text-info", badgeBg: "bg-info-subtle", badgeText: "text-info-strong" },
  none: { label: "No priority", bars: 0, color: "text-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
};
