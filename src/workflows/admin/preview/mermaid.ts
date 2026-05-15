import {
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  WorkflowConfig,
  WorkflowGraphEdgeConfig,
  WorkflowGraphNodeConfig,
  WorkflowGraphOutcomeConfig,
} from "../../shared/workflow-config.js";
import { validateWorkflowConfigForAuthoring } from "../authoring/validation.js";

export type MermaidDirection = "LR" | "TD";

export type WorkflowMermaidPreviewRequest = {
  workflowConfig: WorkflowConfig;
  direction?: MermaidDirection;
};

export type WorkflowMermaidPreviewResponse = {
  mermaid: string;
  metadata: {
    direction: MermaidDirection;
    nodeCount: number;
    edgeCount: number;
    terminalCount: number;
    unreachableNodeIds: string[];
    validationWarnings: Array<{
      code: string;
      path: string;
      jsonPath: string;
      message: string;
    }>;
  };
};

type MermaidEdge = {
  fromNodeId: string;
  toNodeId: string;
  routeLabel: string;
};

const graphNodeTypeClasses: Record<string, string> = {
  approval: "approvalNode",
  approval_gate: "approvalGateNode",
  block: "blockNode",
  external_write: "externalNode",
  manual_repair: "repairNode",
  projection_write: "transactionNode",
  transaction_plan: "transactionNode",
};

/**
 * Converts a workflow graph config into deterministic Mermaid flowchart text.
 */
export function buildWorkflowMermaidPreview(
  request: WorkflowMermaidPreviewRequest,
): Result<WorkflowMermaidPreviewResponse, AppError> {
  const graph = request.workflowConfig.graph;
  const direction = request.direction ?? "LR";

  if (graph === undefined) {
    return err(
      validationFailedError({
        workflowConfig: request.workflowConfig.intent,
        graph: "missing",
      }),
    );
  }

  const edges = workflowMermaidEdges(request.workflowConfig);
  const adjacency = graphAdjacency(graph.nodes);
  const unreachableNodeIds = [...nodeIds(graph.nodes)].filter((nodeId) => {
    return !reachableNodes(graph.startNodeId, adjacency).has(nodeId);
  });
  const validation = validateWorkflowConfigForAuthoring(request.workflowConfig);
  const lines = [
    `flowchart ${direction}`,
    ...graph.nodes.map(renderMermaidNode),
    ...edges.map(renderMermaidEdge),
    ...renderClassAssignments(graph.nodes),
    ...mermaidClassDefinitions(),
  ];

  return ok({
    mermaid: `${lines.join("\n")}\n`,
    metadata: {
      direction,
      nodeCount: graph.nodes.length,
      edgeCount: edges.length,
      terminalCount: graph.nodes.filter((node) => {
        return node.type === "terminal";
      }).length,
      unreachableNodeIds,
      validationWarnings: validation.warnings.map((warning) => {
        return {
          code: warning.code,
          path: warning.path,
          jsonPath: warning.jsonPath,
          message: warning.message,
        };
      }),
    },
  });
}

function workflowMermaidEdges(workflowConfig: WorkflowConfig): MermaidEdge[] {
  const graph = workflowConfig.graph;
  if (graph === undefined) {
    return [];
  }

  if (graph.edges !== undefined && graph.edges.length > 0) {
    return graph.edges.flatMap((edge) => {
      const mermaidEdge = edgeToMermaidEdge(edge);
      return mermaidEdge === undefined ? [] : [mermaidEdge];
    });
  }

  return graph.nodes.flatMap((node) => {
    return (node.outcomes ?? []).flatMap((outcome) => {
      const mermaidEdge = outcomeToMermaidEdge(node.nodeId, outcome);
      return mermaidEdge === undefined ? [] : [mermaidEdge];
    });
  });
}

function edgeToMermaidEdge(edge: WorkflowGraphEdgeConfig): MermaidEdge | undefined {
  if (edge.toNodeId === undefined) {
    return undefined;
  }

  return {
    fromNodeId: edge.fromNodeId,
    toNodeId: edge.toNodeId,
    routeLabel: edge.routeKey,
  };
}

function outcomeToMermaidEdge(
  fromNodeId: string,
  outcome: WorkflowGraphOutcomeConfig,
): MermaidEdge | undefined {
  if (outcome.nextNodeId === undefined) {
    return undefined;
  }

  return {
    fromNodeId,
    toNodeId: outcome.nextNodeId,
    routeLabel: outcome.routeKey ?? outcome.outcome,
  };
}

function renderMermaidNode(node: WorkflowGraphNodeConfig): string {
  const mermaidNodeId = mermaidSafeId(node.nodeId);
  const label = escapeMermaidLabel(`${node.type}: ${node.title}`);

  return `  ${mermaidNodeId}["${label}"]`;
}

function renderMermaidEdge(edge: MermaidEdge): string {
  const fromNodeId = mermaidSafeId(edge.fromNodeId);
  const toNodeId = mermaidSafeId(edge.toNodeId);
  const routeLabel = escapeMermaidLabel(edge.routeLabel);

  return `  ${fromNodeId} -->|"${routeLabel}"| ${toNodeId}`;
}

function renderClassAssignments(nodes: WorkflowGraphNodeConfig[]): string[] {
  return nodes.flatMap((node) => {
    const className = classNameForNode(node);
    if (className === undefined) {
      return [];
    }

    return [`  class ${mermaidSafeId(node.nodeId)} ${className}`];
  });
}

function classNameForNode(node: WorkflowGraphNodeConfig): string | undefined {
  if (node.type === "terminal") {
    if (node.state === "executed" || node.nodeId.includes("completed")) {
      return "terminalSuccess";
    }

    if (node.state === "failed" || node.nodeId.includes("failed")) {
      return "terminalFailure";
    }

    return "terminalStopped";
  }

  return graphNodeTypeClasses[node.type];
}

function mermaidClassDefinitions(): string[] {
  return [
    "  classDef blockNode fill:#eef2ff,stroke:#4f46e5,color:#111827",
    "  classDef approvalNode fill:#fff7ed,stroke:#ea580c,color:#111827",
    "  classDef approvalGateNode fill:#fef3c7,stroke:#d97706,color:#111827",
    "  classDef externalNode fill:#ecfeff,stroke:#0891b2,color:#111827",
    "  classDef transactionNode fill:#f0fdf4,stroke:#16a34a,color:#111827",
    "  classDef repairNode fill:#fdf2f8,stroke:#db2777,color:#111827",
    "  classDef terminalSuccess fill:#dcfce7,stroke:#15803d,color:#111827",
    "  classDef terminalFailure fill:#fee2e2,stroke:#dc2626,color:#111827",
    "  classDef terminalStopped fill:#f3f4f6,stroke:#6b7280,color:#111827",
  ];
}

function graphAdjacency(nodes: WorkflowGraphNodeConfig[]): Map<string, string[]> {
  const adjacency = new Map<string, string[]>();

  for (const node of nodes) {
    adjacency.set(
      node.nodeId,
      (node.outcomes ?? []).flatMap((outcome) => {
        return outcome.nextNodeId === undefined ? [] : [outcome.nextNodeId];
      }),
    );
  }

  return adjacency;
}

function reachableNodes(
  startNodeId: string,
  adjacency: Map<string, string[]>,
): Set<string> {
  const visitedNodeIds = new Set<string>();
  const pendingNodeIds = [startNodeId];

  while (pendingNodeIds.length > 0) {
    const nodeId = pendingNodeIds.pop();
    if (nodeId === undefined || visitedNodeIds.has(nodeId)) {
      continue;
    }

    visitedNodeIds.add(nodeId);

    for (const nextNodeId of adjacency.get(nodeId) ?? []) {
      pendingNodeIds.push(nextNodeId);
    }
  }

  return visitedNodeIds;
}

function nodeIds(nodes: WorkflowGraphNodeConfig[]): Set<string> {
  return new Set(
    nodes.map((node) => {
      return node.nodeId;
    }),
  );
}

function mermaidSafeId(nodeId: string): string {
  const safeNodeId = nodeId.replace(/[^a-zA-Z0-9_]/g, "_");

  if (/^[a-zA-Z_]/.test(safeNodeId)) {
    return safeNodeId;
  }

  return `n_${safeNodeId}`;
}

function escapeMermaidLabel(label: string): string {
  return label.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, " ");
}
