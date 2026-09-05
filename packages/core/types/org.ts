// Executable org chart (K75): seven models, one definition shape. Units hold
// humans and agents alike; edges, rules, committees and a market drive
// routing, escalation, approval and budgets.

export type OrgModel = "hierarchy" | "squads" | "matrix" | "circles" | "owner_network" | "taskforce" | "market";
export type OrgStatus = "draft" | "active" | "paused" | "dissolved";
export type OrgAutonomy = "read_only" | "draft" | "approve_payload" | "auto";
export type OrgProperty = "untrusted_input" | "sensitive_data" | "external_effects";
export type OrgEdgeKind = "reports_to" | "backs_up" | "escalates_to" | "consults";

export interface OrgMember {
  type: "member" | "agent";
  id: string;
  role?: string;
  role_id?: string;
}

export interface OrgRole {
  id: string;
  name: string;
  responsibilities?: string;
  keywords?: string[];
}

export interface OrgUnit {
  id: string;
  name: string;
  kind?: string;
  owner_id?: string;
  squad_id?: string;
  mission_goal_id?: string;
  budget_usd_ticks?: number;
  excludes: OrgProperty[];
  autonomy: OrgAutonomy;
  allow: string[];
  deny: string[];
  escalation_quota_per_day: number;
  human_approval?: boolean;
  approval_risk?: string;
  deciders?: Record<string, string>;
  members: OrgMember[];
  roles: OrgRole[];
}

export interface OrgEdge {
  from: string;
  to: string;
  kind: OrgEdgeKind;
  human_approval?: boolean;
}

export interface OrgRule {
  id: string;
  labels?: string[];
  paths?: string[];
  keywords?: string[];
  target_unit: string;
  priority: number;
}

export interface OrgCommittee {
  decision_type: string;
  unit_ids: string[];
  quorum: number;
  max_rounds: number;
}

export interface OrgMarket {
  price_cap_usd_ticks: number;
  offers_per_agent_per_day: number;
  min_offers: number;
}

export interface OrgDefinition {
  units: OrgUnit[];
  edges: OrgEdge[];
  rules: OrgRule[];
  committees: OrgCommittee[];
  market: OrgMarket;
}

export interface OrgStructure {
  id: string;
  workspace_id: string;
  project_id: string | null;
  model: OrgModel;
  name: string;
  status: OrgStatus;
  revision: number;
  revision_id: string | null;
  definition: OrgDefinition;
  owner_id: string | null;
  dissolve_at: string | null;
  end_condition: string;
  budget_usd_ticks: number;
  eval_attestation: string;
  paused_reason: string;
  dissolved_at: string | null;
  paused_units: string[];
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface OrgRevision {
  id: string;
  revision: number;
  model: string;
  status: string;
  note: string;
  changed_by: string | null;
  created_at: string;
}

export interface OrgTemplate {
  model: OrgModel;
  name: string;
  pattern: string;
  description: string;
  coordination_runs_per_issue: number;
  definition: OrgDefinition;
}

export interface OrgWriteRequest {
  project_id?: string | null;
  model?: OrgModel;
  name?: string;
  definition?: OrgDefinition;
  owner_id?: string | null;
  dissolve_at?: string | null;
  end_condition?: string;
  budget_usd_ticks?: number;
  eval_attestation?: string;
  note?: string;
}

export interface OrgUnitHealth {
  unit_id: string;
  name: string;
  routed: number;
  escalations: number;
  reassigned_outside: number;
  vacant_roles: string[];
  saturated_agents: string[];
  paused: boolean;
  spend_usd_ticks: number;
  budget_usd_ticks: number;
  human_review_items: number;
}

export interface OrgProposal {
  key: string;
  unit_id?: string;
  title: string;
  body: string;
  measure: string;
}

export interface OrgHealth {
  structure_id: string;
  window_days: number;
  routed: number;
  unrouted: number;
  escalations: number;
  stacked_escalations: number;
  reassigned_outside: number;
  market_short: number;
  breakers: number;
  human_review_items: number;
  drift_rate: number;
  units: OrgUnitHealth[];
  proposals: OrgProposal[];
}

export interface OrgPreflight {
  model: string;
  pattern: string;
  coordination_runs_per_issue: number;
  coordination_cost_usd_ticks_per_issue: number;
  human_review_items_per_issue: number;
  human_review_seconds_per_issue: number;
  units: number;
  units_without_owner: number;
  agents: number;
  activation_requirements: string[];
}

export interface OrgOffer {
  id: string;
  agent_id: string;
  agent_name: string;
  confidence: number;
  cost_usd_ticks: number;
  eta_hours: number;
  status: "pending" | "won" | "lost" | "over_cap";
  created_at: string;
}
