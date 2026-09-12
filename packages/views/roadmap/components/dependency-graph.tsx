"use client";

import { useMemo } from "react";
import {
  Background,
  Controls,
  MarkerType,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type {
  Issue,
  IssueDependencyEdge,
  IssueStatusCategory,
} from "@multica/core/types";
import { issueStatusCategory } from "@multica/core/issues";
import {
  GRAPH_NODE_HEIGHT,
  GRAPH_NODE_WIDTH,
  layoutDependencyGraph,
} from "@multica/core/roadmap";
import { cn } from "@multica/ui/lib/utils";

// Same category → semantic-token mapping as the Gantt bars: a custom status
// draws in the color of the category it behaves as.
const CATEGORY_DOT: Record<IssueStatusCategory, string> = {
  backlog: "bg-muted-foreground/60",
  todo: "bg-muted-foreground/70",
  in_progress: "bg-warning",
  in_review: "bg-success",
  done: "bg-info",
  blocked: "bg-destructive",
  cancelled: "bg-muted-foreground/40",
};

interface IssueNodeData extends Record<string, unknown> {
  title: string;
  identifier: string;
  category: IssueStatusCategory;
}

type IssueFlowNode = Node<IssueNodeData, "issue">;

function IssueNode({ data }: NodeProps<IssueFlowNode>) {
  return (
    <div className="flex h-full w-full items-center gap-2 rounded-md border border-border bg-background px-2 shadow-sm">
      <span
        aria-hidden="true"
        className={cn("size-2 shrink-0 rounded-full", CATEGORY_DOT[data.category])}
      />
      <div className="min-w-0">
        <div className="text-micro leading-tight text-muted-foreground">
          {data.identifier}
        </div>
        <div className="truncate text-caption leading-tight">{data.title}</div>
      </div>
    </div>
  );
}

const nodeTypes = { issue: IssueNode };

// Edge appearance by dependency type: blocks is the load-bearing relation
// (solid, arrowed into the blocked issue), related is a loose dashed link,
// duplicate is the faintest — it carries no ordering information.
const EDGE_APPEARANCE: Record<
  string,
  { stroke: string; strokeDasharray?: string }
> = {
  blocks: { stroke: "var(--foreground)" },
  related: { stroke: "var(--muted-foreground)", strokeDasharray: "6 4" },
  duplicate: { stroke: "var(--border)" },
};

export function DependencyGraph({
  issues,
  edges,
}: {
  issues: Issue[];
  edges: IssueDependencyEdge[];
}) {
  const layout = useMemo(
    () => layoutDependencyGraph(issues.map((issue) => ({ id: issue.id })), edges),
    [issues, edges],
  );

  const nodes = useMemo<IssueFlowNode[]>(() => {
    const byId = new Map(issues.map((issue) => [issue.id, issue]));
    return layout.flatMap((pos) => {
      const issue = byId.get(pos.id);
      if (!issue) return [];
      return [
        {
          id: pos.id,
          type: "issue" as const,
          // Dagre reports node CENTERS; React Flow positions are top-left.
          position: {
            x: pos.x - GRAPH_NODE_WIDTH / 2,
            y: pos.y - GRAPH_NODE_HEIGHT / 2,
          },
          data: {
            title: issue.title,
            identifier: issue.identifier,
            category: issueStatusCategory(issue) ?? "todo",
          },
          // Dagre laid the graph out with these exact dimensions; the node
          // must render at the same size or the layout's spacing lies.
          style: { width: GRAPH_NODE_WIDTH, height: GRAPH_NODE_HEIGHT },
        },
      ];
    });
  }, [issues, layout]);

  const flowEdges = useMemo<Edge[]>(
    () =>
      edges.map((edge) => {
        const appearance =
          EDGE_APPEARANCE[edge.type] ?? EDGE_APPEARANCE.related!;
        return {
          id: edge.id,
          source: edge.from,
          target: edge.to,
          data: { type: edge.type },
          style: {
            stroke: appearance.stroke,
            strokeDasharray: appearance.strokeDasharray,
          },
          markerEnd:
            edge.type === "blocks"
              ? { type: MarkerType.ArrowClosed, color: appearance.stroke }
              : undefined,
        };
      }),
    [edges],
  );

  return (
    <ReactFlow
      nodes={nodes}
      edges={flowEdges}
      nodeTypes={nodeTypes}
      fitView
      nodesDraggable
      nodesConnectable={false}
      proOptions={{ hideAttribution: false }}
    >
      <Background />
      <Controls />
    </ReactFlow>
  );
}
