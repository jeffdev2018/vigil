import { z } from "zod";

/**
 * F27 insights. The server compiles a closed-vocabulary document into SQL; the
 * client's job is to carry that document around unchanged and render what came
 * back.
 *
 * Every schema here is lenient on purpose. `entity`, `metric` and `shape` stay
 * `z.string()` rather than enums so a backend that learned a new metric does not
 * blank the tab on an installed desktop client — the UI branches on them through
 * `default` arms instead.
 */

export const InsightFilterSchema = z
  .object({
    field: z.string().default(""),
    op: z.string().default(""),
    values: z.array(z.string()).default([]),
  })
  .loose();

export const InsightTimeRangeSchema = z
  .object({
    field: z.string().default(""),
    last_days: z.number().default(0),
    start: z.string().default(""),
    end: z.string().default(""),
  })
  .partial()
  .loose();

export const InsightQuerySchema = z
  .object({
    entity: z.string().default("issue"),
    metric: z.string().default("count"),
    group_by: z.array(z.string()).default([]),
    filters: z.array(InsightFilterSchema).default([]),
    time_range: InsightTimeRangeSchema.nullable().optional(),
    granularity: z.string().optional(),
    limit: z.number().optional(),
  })
  .loose();

/**
 * A result cell. The server normalizes everything into this shape, so widening
 * it here would only hide a contract break.
 */
export const InsightRowSchema = z.record(
  z.string(),
  z.union([z.string(), z.number(), z.null()]),
);

export const InsightRunResponseSchema = z
  .object({
    rows: z.array(InsightRowSchema).default([]),
    shape: z.string().default("bar"),
    warnings: z.array(z.string()).nullable().default([]),
    duration_ms: z.number().default(0),
  })
  .loose();

export const InsightAskResponseSchema = z
  .object({
    query: InsightQuerySchema.nullable().default(null),
    rows: z.array(InsightRowSchema).default([]),
    shape: z.string().default("bar"),
    warnings: z.array(z.string()).nullable().default([]),
    duration_ms: z.number().default(0),
  })
  .loose();

export const InsightWidgetSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string().default(""),
    owner_id: z.string().default(""),
    name: z.string().default(""),
    question: z.string().default(""),
    definition_version: z.number().default(1),
    query: InsightQuerySchema.nullable().default(null),
    display: z.record(z.string(), z.unknown()).default({}),
    visibility: z.string().default("private"),
    position: z.number().default(0),
    revision: z.number().default(1),
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
  })
  .loose();

export const InsightWidgetListSchema = z.array(InsightWidgetSchema);

export type InsightFilter = z.infer<typeof InsightFilterSchema>;
export type InsightQuery = z.infer<typeof InsightQuerySchema>;
export type InsightRow = z.infer<typeof InsightRowSchema>;
export type InsightRunResponse = z.infer<typeof InsightRunResponseSchema>;
export type InsightAskResponse = z.infer<typeof InsightAskResponseSchema>;
export type InsightWidget = z.infer<typeof InsightWidgetSchema>;

export interface CreateInsightWidgetInput {
  name: string;
  question?: string;
  query: InsightQuery;
  display?: Record<string, unknown>;
  visibility?: "private" | "workspace";
}

export interface UpdateInsightWidgetInput {
  name?: string;
  question?: string;
  query?: InsightQuery;
  display?: Record<string, unknown>;
  visibility?: "private" | "workspace";
  position?: number;
  expected_revision: number;
}

export const EMPTY_INSIGHT_RUN_RESPONSE: InsightRunResponse = Object.freeze({
  rows: [],
  shape: "bar",
  warnings: [],
  duration_ms: 0,
}) as InsightRunResponse;

export const EMPTY_INSIGHT_ASK_RESPONSE: InsightAskResponse = Object.freeze({
  query: null,
  rows: [],
  shape: "bar",
  warnings: [],
  duration_ms: 0,
}) as InsightAskResponse;

export const EMPTY_INSIGHT_WIDGET: InsightWidget = Object.freeze({
  id: "",
  workspace_id: "",
  owner_id: "",
  name: "",
  question: "",
  definition_version: 1,
  query: null,
  display: {},
  visibility: "private",
  position: 0,
  revision: 1,
  created_at: "",
  updated_at: "",
}) as InsightWidget;

export const EMPTY_INSIGHT_WIDGETS: InsightWidget[] = Object.freeze([]) as unknown as InsightWidget[];
