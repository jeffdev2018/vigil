import { z } from "zod";

// Cross-repo mirror issues (K54). A mirror link says "an issue of this project
// carrying <trigger_label> must also exist in <target_project>"; a mirror is
// one issue that link produced, and it blocks its source until it is closed.

export const MirrorLinkSchema = z.object({
  id: z.string(),
  source_project_id: z.string().catch(""),
  target_project_id: z.string().catch(""),
  target_project_title: z.string().catch(""),
  trigger_label: z.string().catch(""),
  created_at: z.string().catch(""),
}).loose();
export type MirrorLink = z.infer<typeof MirrorLinkSchema>;

export const MirrorLinkListSchema = z.object({
  links: z.array(MirrorLinkSchema).catch([]).default([]),
}).loose();
export type MirrorLinkList = z.infer<typeof MirrorLinkListSchema>;

export const EMPTY_MIRROR_LINKS: MirrorLinkList = { links: [] };

export const IssueMirrorSchema = z.object({
  id: z.string(),
  mirror_issue_id: z.string().catch(""),
  identifier: z.string().catch(""),
  number: z.number().catch(0),
  title: z.string().catch(""),
  status: z.string().catch(""),
  project_id: z.string().catch(""),
  project_title: z.string().catch(""),
  type_synced: z.boolean().catch(false),
}).loose();
export type IssueMirror = z.infer<typeof IssueMirrorSchema>;

export const MirrorSourceSchema = z.object({
  id: z.string(),
  source_issue_id: z.string().catch(""),
  identifier: z.string().catch(""),
  number: z.number().catch(0),
  title: z.string().catch(""),
  status: z.string().catch(""),
  project_id: z.string().catch(""),
  project_title: z.string().catch(""),
  type_synced: z.boolean().catch(false),
}).loose();
export type MirrorSource = z.infer<typeof MirrorSourceSchema>;

export const IssueMirrorsSchema = z.object({
  mirrors: z.array(IssueMirrorSchema).catch([]).default([]),
  mirror_of: MirrorSourceSchema.nullable().catch(null).default(null),
}).loose();
export type IssueMirrors = z.infer<typeof IssueMirrorsSchema>;

export const EMPTY_ISSUE_MIRRORS: IssueMirrors = { mirrors: [], mirror_of: null };

/** A mirror still holding its source back is one that is neither done nor cancelled. */
export function isMirrorOpen(m: Pick<IssueMirror, "status">): boolean {
  return m.status !== "done" && m.status !== "cancelled";
}
