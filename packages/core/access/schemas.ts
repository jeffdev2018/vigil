import { z } from "zod";

// SSO, SCIM and project roles (K60).

export const SSOConnectionSchema = z.object({
  provider: z.string().catch("oidc"),
  issuer: z.string().catch(""),
  client_id: z.string().catch(""),
  has_secret: z.boolean().catch(false),
  allowed_domains: z.array(z.string()).catch([]),
  auto_provision: z.boolean().catch(true),
  enforced: z.boolean().catch(false),
  updated_at: z.string().catch(""),
}).loose();
export type SSOConnection = z.infer<typeof SSOConnectionSchema>;

export const SSOStateSchema = z.object({
  connection: SSOConnectionSchema.nullable().catch(null).default(null),
  configured: z.boolean().catch(false).default(false),
}).loose();
export type SSOState = z.infer<typeof SSOStateSchema>;

export interface SSOConnectionRequest {
  issuer: string;
  client_id: string;
  client_secret?: string;
  allowed_domains?: string[];
  auto_provision?: boolean;
  enforced?: boolean;
}

export const ScimTokenSchema = z.object({
  id: z.string(),
  token_hint: z.string().catch(""),
  active: z.boolean().catch(false),
  created_at: z.string().catch(""),
  last_used_at: z.string().nullable().catch(null),
  /** Present once, at creation. */
  token: z.string().optional().catch(undefined),
}).loose();
export type ScimToken = z.infer<typeof ScimTokenSchema>;
export const ScimTokenListSchema = z.object({ tokens: z.array(ScimTokenSchema).catch([]).default([]) }).loose();

export const ProjectRoleSchema = z.enum(["viewer", "contributor", "admin"]);
export type ProjectRole = z.infer<typeof ProjectRoleSchema>;

export const ProjectMemberRoleSchema = z.object({
  subject_type: z.enum(["member", "agent"]).catch("member"),
  subject_id: z.string(),
  name: z.string().catch(""),
  email: z.string().optional().catch(undefined),
  workspace_role: z.string().catch(""),
  ceiling: ProjectRoleSchema.catch("contributor"),
  effective_role: ProjectRoleSchema.catch("contributor"),
  source: z.enum(["inherited", "override"]).catch("inherited"),
  override: ProjectRoleSchema.nullable().catch(null),
}).loose();
export type ProjectMemberRole = z.infer<typeof ProjectMemberRoleSchema>;

export const ProjectMembersSchema = z.object({
  members: z.array(ProjectMemberRoleSchema).catch([]).default([]),
  roles: z.array(ProjectRoleSchema).catch(["viewer", "contributor", "admin"]).default(["viewer", "contributor", "admin"]),
}).loose();
export type ProjectMembers = z.infer<typeof ProjectMembersSchema>;

export const EMPTY_PROJECT_MEMBERS: ProjectMembers = { members: [], roles: ["viewer", "contributor", "admin"] };

/** The role a subject would get if its override were cleared. */
export function inheritedProjectRole(m: Pick<ProjectMemberRole, "ceiling">): ProjectRole {
  return m.ceiling;
}
