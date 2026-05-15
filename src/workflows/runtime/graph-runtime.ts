import {
  WORKFLOW_ROUTE_KEYS,
  WORKFLOW_TRANSITIONS,
  type WorkflowState,
  type WorkflowStatus,
} from "@hcm-next/foundation";
import { workflowConditionMatches } from "../shared/workflow-conditions.js";
import type {
  WorkflowConfig,
  WorkflowGraphEdgeConfig,
  WorkflowGraphNodeConfig,
  WorkflowGraphOutcomeConfig,
  WorkflowTemplateSources,
} from "../shared/workflow-config.js";

export type WorkflowGraphNodeType = WorkflowGraphNodeConfig["type"];

export type WorkflowGraphNodeExecution = {
  nodeId: string;
  nodeType: WorkflowGraphNodeType;
  title: string;
  outcome: string;
  routeKey: string;
  sideEffect: boolean;
  eventType?: string | undefined;
  nextNodeId?: string | undefined;
  idempotencyKey?: string | undefined;
};

export type WorkflowGraphRouteDecision = {
  fromNodeId: string;
  routeKey: string;
  outcome: string;
  toNodeId?: string | undefined;
  nextState?: WorkflowState | undefined;
  nextStatus?: WorkflowStatus | undefined;
  nextInteraction?: string | undefined;
  eventType?: string | undefined;
};

export type WorkflowGraphRuntimeState = {
  activeNodeId?: string | undefined;
  activeNodeIds: string[];
  completedNodes: WorkflowGraphNodeExecution[];
  routeDecisions: WorkflowGraphRouteDecision[];
  warnings: string[];
};

export type WorkflowGraphAdvanceResult = {
  startNodeId: string;
  finalNodeId: string;
  finalNodeType: WorkflowGraphNodeType;
  nextState?: WorkflowState | undefined;
  nextStatus?: WorkflowStatus | undefined;
  nextInteraction?: string | undefined;
  completedNodes: WorkflowGraphNodeExecution[];
  routeDecisions: WorkflowGraphRouteDecision[];
  warnings: string[];
};

export type WorkflowGraphAdvanceInput = {
  workflowConfig: WorkflowConfig;
  workflowContext: Record<string, unknown>;
  firstRouteKey?: string | undefined;
  automaticRouteKeysByNodeId?: Record<string, string> | undefined;
  sources?: WorkflowTemplateSources | undefined;
  stopBeforeNodeTypes?: WorkflowGraphNodeType[] | undefined;
  stopAtNodeTypes?: WorkflowGraphNodeType[] | undefined;
  maxNodeVisits?: number | undefined;
};

const defaultMaxNodeVisits = 40;

const defaultStopAtNodeTypes = new Set<WorkflowGraphNodeType>([
  "interaction",
  "approval",
  "approval_gate",
  "manual_repair",
  "terminal",
]);

/**
 * Creates the initial graph cursor shape stored on workflow instances.
 */
export function initialGraphRuntimeContext(
  workflowConfig: WorkflowConfig,
): Record<string, unknown> {
  const activeNodeId = workflowConfig.graph?.startNodeId;

  if (activeNodeId === undefined) {
    return {};
  }

  return {
    activeNodeId,
    graphRuntime: {
      activeNodeId,
      activeNodeIds: [activeNodeId],
      completedNodes: [],
      routeDecisions: [],
      warnings: [],
    } satisfies WorkflowGraphRuntimeState,
  };
}

/**
 * Advances a workflow graph through configured outcomes until a wait boundary.
 */
export function advanceWorkflowGraph(
  input: WorkflowGraphAdvanceInput,
): WorkflowGraphAdvanceResult {
  const graph = input.workflowConfig.graph;
  const warningMessages: string[] = [];

  if (graph === undefined) {
    return emptyAdvanceResult("missing_graph", "terminal", [
      "Workflow graph is missing.",
    ]);
  }

  const nodesById = workflowGraphNodesById(input.workflowConfig);
  const startNodeId = activeNodeIdFromContext(
    input.workflowContext,
    input.workflowConfig,
  );
  const startNode = nodesById.get(startNodeId);

  if (startNode === undefined) {
    return emptyAdvanceResult(startNodeId, "terminal", [
      `Active graph node ${startNodeId} is missing.`,
    ]);
  }

  return runGraphAdvanceLoop({
    input,
    nodesById,
    startNode,
    warningMessages,
  });
}

/**
 * Merges a graph advance result into workflow instance context.
 */
export function contextWithGraphAdvance(input: {
  context: Record<string, unknown>;
  advance: WorkflowGraphAdvanceResult;
}): Record<string, unknown> {
  const previousRuntime = graphRuntimeStateFromContext(input.context);
  const completedNodes = [
    ...previousRuntime.completedNodes,
    ...input.advance.completedNodes,
  ];
  const routeDecisions = [
    ...previousRuntime.routeDecisions,
    ...input.advance.routeDecisions,
  ];
  const warnings = [...previousRuntime.warnings, ...input.advance.warnings];
  const graphRuntime: WorkflowGraphRuntimeState = {
    activeNodeId: input.advance.finalNodeId,
    activeNodeIds: [input.advance.finalNodeId],
    completedNodes,
    routeDecisions,
    warnings,
  };

  return {
    ...input.context,
    activeNodeId: input.advance.finalNodeId,
    graphRuntime,
  };
}

/**
 * Returns a transition's conventional graph route key.
 */
export function routeKeyForWorkflowTransition(transition: string): string | undefined {
  const transitionRouteKeys: Record<string, string> = {
    [WORKFLOW_TRANSITIONS.SUBMIT_INPUT]: WORKFLOW_ROUTE_KEYS.SUBMITTED,
    [WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE]: WORKFLOW_ROUTE_KEYS.PROVIDED,
    [WORKFLOW_TRANSITIONS.APPROVE]: WORKFLOW_ROUTE_KEYS.APPROVED,
    [WORKFLOW_TRANSITIONS.REJECT]: WORKFLOW_ROUTE_KEYS.REJECTED,
    [WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO]: WORKFLOW_ROUTE_KEYS.REQUEST_MORE_INFO,
    [WORKFLOW_TRANSITIONS.CANCEL]: WORKFLOW_ROUTE_KEYS.CANCELED,
  };

  return transitionRouteKeys[transition];
}

/**
 * Finds a configured graph node by ID.
 */
export function workflowGraphNodeById(
  workflowConfig: WorkflowConfig,
  nodeId: string,
): WorkflowGraphNodeConfig | undefined {
  return workflowGraphNodesById(workflowConfig).get(nodeId);
}

/**
 * Selects the next route decision for one node.
 */
export function routeDecisionForNode(input: {
  workflowConfig: WorkflowConfig;
  node: WorkflowGraphNodeConfig;
  routeKey?: string | undefined;
  sources?: WorkflowTemplateSources | undefined;
}): WorkflowGraphRouteDecision | undefined {
  const selectedOutcome = selectedOutcomeForNode({
    node: input.node,
    routeKey: input.routeKey,
    sources: input.sources,
  });

  if (selectedOutcome === undefined) {
    return undefined;
  }

  const routeKey = selectedOutcome.routeKey ?? selectedOutcome.outcome;
  const matchingEdge = edgeForOutcome({
    workflowConfig: input.workflowConfig,
    fromNodeId: input.node.nodeId,
    routeKey,
  });

  return {
    fromNodeId: input.node.nodeId,
    routeKey,
    outcome: selectedOutcome.outcome,
    toNodeId: matchingEdge?.toNodeId ?? selectedOutcome.nextNodeId,
    nextState: matchingEdge?.nextState ?? selectedOutcome.nextState,
    nextStatus: matchingEdge?.nextStatus ?? selectedOutcome.nextStatus,
    nextInteraction: matchingEdge?.nextInteraction ?? selectedOutcome.nextInteraction,
    eventType: matchingEdge?.eventType ?? selectedOutcome.eventType,
  };
}

function runGraphAdvanceLoop(input: {
  input: WorkflowGraphAdvanceInput;
  nodesById: Map<string, WorkflowGraphNodeConfig>;
  startNode: WorkflowGraphNodeConfig;
  warningMessages: string[];
}): WorkflowGraphAdvanceResult {
  const completedNodes: WorkflowGraphNodeExecution[] = [];
  const routeDecisions: WorkflowGraphRouteDecision[] = [];
  const maxNodeVisits = input.input.maxNodeVisits ?? defaultMaxNodeVisits;
  const stopBeforeNodeTypes = new Set(input.input.stopBeforeNodeTypes ?? []);
  const stopAtNodeTypes = new Set(
    input.input.stopAtNodeTypes ?? [...defaultStopAtNodeTypes],
  );
  const sources = input.input.sources ?? {};
  let currentNode = input.startNode;
  let finalNode = input.startNode;
  let nextState: WorkflowState | undefined;
  let nextStatus: WorkflowStatus | undefined;
  let nextInteraction: string | undefined;

  for (let nodeVisitCount = 0; nodeVisitCount < maxNodeVisits; nodeVisitCount += 1) {
    if (nodeVisitCount > 0 && stopBeforeNodeTypes.has(currentNode.type)) {
      finalNode = currentNode;
      break;
    }

    if (nodeVisitCount > 0 && stopAtNodeTypes.has(currentNode.type)) {
      finalNode = currentNode;
      break;
    }

    const routeKey =
      nodeVisitCount === 0
        ? input.input.firstRouteKey
        : input.input.automaticRouteKeysByNodeId?.[currentNode.nodeId];
    const routeDecision = routeDecisionForNode({
      workflowConfig: input.input.workflowConfig,
      node: currentNode,
      routeKey,
      sources,
    });

    if (routeDecision === undefined) {
      finalNode = currentNode;
      input.warningMessages.push(
        `No graph outcome matched node ${currentNode.nodeId}.`,
      );
      break;
    }

    completedNodes.push(nodeExecutionFromDecision(currentNode, routeDecision));
    routeDecisions.push(routeDecision);
    nextState = routeDecision.nextState ?? nextState;
    nextStatus = routeDecision.nextStatus ?? nextStatus;
    nextInteraction = routeDecision.nextInteraction ?? nextInteraction;

    if (routeDecision.toNodeId === undefined) {
      finalNode = currentNode;
      break;
    }

    const nextNode = input.nodesById.get(routeDecision.toNodeId);
    if (nextNode === undefined) {
      finalNode = currentNode;
      input.warningMessages.push(
        `Graph route ${currentNode.nodeId}.${routeDecision.routeKey} points to missing node ${routeDecision.toNodeId}.`,
      );
      break;
    }

    currentNode = nextNode;
    finalNode = nextNode;
  }

  if (completedNodes.length >= maxNodeVisits) {
    input.warningMessages.push("Graph advancement stopped after max node visits.");
  }

  return {
    startNodeId: input.startNode.nodeId,
    finalNodeId: finalNode.nodeId,
    finalNodeType: finalNode.type,
    ...(nextState !== undefined ? { nextState } : {}),
    ...(nextStatus !== undefined ? { nextStatus } : {}),
    ...(nextInteraction !== undefined ? { nextInteraction } : {}),
    completedNodes,
    routeDecisions,
    warnings: input.warningMessages,
  };
}

function selectedOutcomeForNode(input: {
  node: WorkflowGraphNodeConfig;
  routeKey?: string | undefined;
  sources?: WorkflowTemplateSources | undefined;
}): WorkflowGraphOutcomeConfig | undefined {
  const outcomes = input.node.outcomes ?? [];
  const configuredRouteOutcome = outcomeForRouteKey(outcomes, input.routeKey);

  if (input.routeKey !== undefined) {
    return configuredRouteOutcome;
  }

  return (
    outcomes.find((outcome) => {
      return (
        outcome.when !== undefined &&
        workflowConditionMatches(outcome.when, input.sources ?? {})
      );
    }) ?? outcomes[0]
  );
}

function outcomeForRouteKey(
  outcomes: WorkflowGraphOutcomeConfig[],
  routeKey: string | undefined,
): WorkflowGraphOutcomeConfig | undefined {
  if (routeKey === undefined) {
    return undefined;
  }

  return outcomes.find((outcome) => {
    return outcome.routeKey === routeKey || outcome.outcome === routeKey;
  });
}

function edgeForOutcome(input: {
  workflowConfig: WorkflowConfig;
  fromNodeId: string;
  routeKey: string;
}): WorkflowGraphEdgeConfig | undefined {
  return input.workflowConfig.graph?.edges?.find((edge) => {
    return edge.fromNodeId === input.fromNodeId && edge.routeKey === input.routeKey;
  });
}

function activeNodeIdFromContext(
  workflowContext: Record<string, unknown>,
  workflowConfig: WorkflowConfig,
): string {
  const graphRuntime = graphRuntimeStateFromContext(workflowContext);

  return (
    graphRuntime.activeNodeId ??
    stringFromRecord(workflowContext, "activeNodeId") ??
    workflowConfig.graph?.startNodeId ??
    ""
  );
}

function graphRuntimeStateFromContext(
  workflowContext: Record<string, unknown>,
): WorkflowGraphRuntimeState {
  const runtimeValue = workflowContext["graphRuntime"];

  if (typeof runtimeValue !== "object" || runtimeValue === null) {
    return {
      activeNodeId: stringFromRecord(workflowContext, "activeNodeId"),
      activeNodeIds: [],
      completedNodes: [],
      routeDecisions: [],
      warnings: [],
    };
  }

  const runtimeRecord = runtimeValue as Record<string, unknown>;
  const activeNodeId = stringFromRecord(runtimeRecord, "activeNodeId");
  const completedNodes = arrayFromRecord<WorkflowGraphNodeExecution>(
    runtimeRecord,
    "completedNodes",
  );
  const routeDecisions = arrayFromRecord<WorkflowGraphRouteDecision>(
    runtimeRecord,
    "routeDecisions",
  );
  const warnings = arrayFromRecord<string>(runtimeRecord, "warnings");

  return {
    activeNodeId,
    activeNodeIds: activeNodeId === undefined ? [] : [activeNodeId],
    completedNodes,
    routeDecisions,
    warnings,
  };
}

function workflowGraphNodesById(
  workflowConfig: WorkflowConfig,
): Map<string, WorkflowGraphNodeConfig> {
  return new Map(
    (workflowConfig.graph?.nodes ?? []).map((node) => {
      return [node.nodeId, node];
    }),
  );
}

function nodeExecutionFromDecision(
  node: WorkflowGraphNodeConfig,
  routeDecision: WorkflowGraphRouteDecision,
): WorkflowGraphNodeExecution {
  return {
    nodeId: node.nodeId,
    nodeType: node.type,
    title: node.title,
    outcome: routeDecision.outcome,
    routeKey: routeDecision.routeKey,
    sideEffect: node.transaction?.sideEffect === true,
    ...(routeDecision.eventType !== undefined
      ? { eventType: routeDecision.eventType }
      : {}),
    ...(routeDecision.toNodeId !== undefined
      ? { nextNodeId: routeDecision.toNodeId }
      : {}),
    ...(node.transaction?.idempotencyKeyPath !== undefined
      ? { idempotencyKey: node.transaction.idempotencyKeyPath }
      : {}),
  };
}

function emptyAdvanceResult(
  nodeId: string,
  nodeType: WorkflowGraphNodeType,
  warnings: string[],
): WorkflowGraphAdvanceResult {
  return {
    startNodeId: nodeId,
    finalNodeId: nodeId,
    finalNodeType: nodeType,
    completedNodes: [],
    routeDecisions: [],
    warnings,
  };
}

function stringFromRecord(
  record: Record<string, unknown>,
  key: string,
): string | undefined {
  const value = record[key];

  return typeof value === "string" && value.trim().length > 0 ? value : undefined;
}

function arrayFromRecord<TValue>(
  record: Record<string, unknown>,
  key: string,
): TValue[] {
  const value = record[key];

  return Array.isArray(value) ? (value as TValue[]) : [];
}
