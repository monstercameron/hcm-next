import {
  ok,
  validationFailedError,
  type AppError,
  type Result,
  type WorkflowState,
  type WorkflowStatus,
} from "@human-capital-management-suite/foundation";
import type {
  ActorRecord,
  ApprovalTaskRecord,
  EmployeeProjectionDocument,
  WorkflowInstanceRecord,
} from "@human-capital-management-suite/data-store";
import { workflowConditionMatches } from "../../shared/workflow-conditions.js";
import {
  resolveWorkflowTemplate,
  type WorkflowConfig,
  type WorkflowGraphNodeConfig,
  type WorkflowGraphOutcomeConfig,
  type WorkflowTemplateSources,
} from "../../shared/workflow-config.js";
import { computeConfiguredAvailableActions } from "../../runtime/permissions.js";

export type SimulatedBlockOutput = {
  outcome?: string | undefined;
  routeKey?: string | undefined;
  output?: Record<string, unknown> | undefined;
  validationErrors?: Record<string, unknown>[] | undefined;
  warnings?: Record<string, unknown>[] | undefined;
  proposedLedgerEvents?: Record<string, unknown>[] | undefined;
  transactionPlan?: Record<string, unknown> | undefined;
  projectionPatches?: Record<string, unknown>[] | undefined;
  externalCalls?: Record<string, unknown>[] | undefined;
};

export type WorkflowSimulationBlockExecutor = (input: {
  node: WorkflowGraphNodeConfig;
  blockInput: unknown;
  sources: WorkflowTemplateSources;
}) => Result<SimulatedBlockOutput, AppError>;

export type WorkflowSimulationRequest = {
  workflowConfig: WorkflowConfig;
  actor: ActorRecord;
  employeeProjection: EmployeeProjectionDocument;
  workflowInput: Record<string, unknown>;
  effectiveAt?: string | undefined;
  fakeIntegrationResponses?: Record<string, unknown> | undefined;
  fakeIntegrationErrors?: Record<string, unknown> | undefined;
  approvalDecisions?: Record<string, string> | undefined;
  nodeOutcomeOverrides?: Record<string, string> | undefined;
  pendingTasks?: ApprovalTaskRecord[] | undefined;
  blockExecutor?: WorkflowSimulationBlockExecutor | undefined;
  maxNodeVisits?: number | undefined;
};

export type WorkflowSimulationNodeTrace = {
  nodeId: string;
  nodeType: WorkflowGraphNodeConfig["type"];
  title: string;
  blockInputSummary?: unknown;
  blockOutputSummary?: Record<string, unknown>;
  externalCallPreview?: Record<string, unknown>;
  permissionCheck?: WorkflowSimulationPermissionCheck;
  approvalGatePreview?: WorkflowSimulationApprovalGatePreview;
  routeDecision?: {
    outcome: string;
    routeKey: string;
    nextNodeId?: string;
    matchedCondition?: string;
  };
  validationErrors: Record<string, unknown>[];
  warnings: Record<string, unknown>[];
  proposedLedgerEvents: Record<string, unknown>[];
};

export type WorkflowSimulationPermissionCheck = {
  actorId: string;
  state: string;
  allowedTransitions: string[];
  deniedTransitions: string[];
};

export type WorkflowSimulationApprovalGatePreview = {
  gateId: string;
  mode: string;
  resolverCount: number;
  passRuleType: string;
  selectedDecision?: string;
};

export type WorkflowSimulationResult = {
  durableWritesCreated: false;
  workflowIntent: string;
  finalNodeId?: string;
  finalWorkflowState?: WorkflowState | string;
  finalWorkflowStatus?: WorkflowStatus | string;
  traces: WorkflowSimulationNodeTrace[];
  routeDecisions: Array<NonNullable<WorkflowSimulationNodeTrace["routeDecision"]>>;
  validationErrors: Record<string, unknown>[];
  warnings: Record<string, unknown>[];
  proposedLedgerEvents: Record<string, unknown>[];
  transactionPlans: Record<string, unknown>[];
  projectionPatchPreview: Record<string, unknown>[];
  externalCallPreview: Record<string, unknown>[];
  simulatorWarnings: string[];
};

const defaultMaxNodeVisits = 50;

/**
 * Runs a workflow graph in memory against fixtures and fake integration outcomes.
 */
export function simulateWorkflow(
  input: WorkflowSimulationRequest,
): Result<WorkflowSimulationResult, AppError> {
  const graph = input.workflowConfig.graph;

  if (graph === undefined) {
    return {
      ok: false,
      error: validationFailedError({
        intent: input.workflowConfig.intent,
        graph: "missing",
      }),
    };
  }

  const nodesById = new Map(
    graph.nodes.map((node) => {
      return [node.nodeId, node];
    }),
  );
  const startNode = nodesById.get(graph.startNodeId);

  if (startNode === undefined) {
    return {
      ok: false,
      error: validationFailedError({
        intent: input.workflowConfig.intent,
        startNodeId: graph.startNodeId,
      }),
    };
  }

  return ok(runSimulationLoop({ input, nodesById, startNode }));
}

function runSimulationLoop(input: {
  input: WorkflowSimulationRequest;
  nodesById: Map<string, WorkflowGraphNodeConfig>;
  startNode: WorkflowGraphNodeConfig;
}): WorkflowSimulationResult {
  const maxNodeVisits = input.input.maxNodeVisits ?? defaultMaxNodeVisits;
  const traces: WorkflowSimulationNodeTrace[] = [];
  const routeDecisions: Array<
    NonNullable<WorkflowSimulationNodeTrace["routeDecision"]>
  > = [];
  const validationErrors: Record<string, unknown>[] = [];
  const warnings: Record<string, unknown>[] = [];
  const proposedLedgerEvents: Record<string, unknown>[] = [];
  const transactionPlans: Record<string, unknown>[] = [];
  const projectionPatchPreview: Record<string, unknown>[] = [];
  const externalCallPreview: Record<string, unknown>[] = [];
  const simulatorWarnings: string[] = [];
  const sources = initialSources(input.input);
  let currentNode: WorkflowGraphNodeConfig | undefined = input.startNode;
  let finalWorkflowState: WorkflowState | string | undefined = input.startNode.state;
  let finalWorkflowStatus: WorkflowStatus | string | undefined = undefined;

  for (
    let visitCount = 0;
    visitCount < maxNodeVisits && currentNode !== undefined;
    visitCount += 1
  ) {
    const nodeResult = simulateNode({
      request: input.input,
      node: currentNode,
      sources,
    });
    const trace = nodeResult.trace;

    traces.push(trace);
    validationErrors.push(...trace.validationErrors);
    warnings.push(...trace.warnings);
    proposedLedgerEvents.push(...trace.proposedLedgerEvents);

    if (nodeResult.transactionPlan !== undefined) {
      transactionPlans.push(nodeResult.transactionPlan);
    }
    projectionPatchPreview.push(...nodeResult.projectionPatches);
    externalCallPreview.push(...nodeResult.externalCalls);

    if (trace.routeDecision !== undefined) {
      routeDecisions.push(trace.routeDecision);
      finalWorkflowState = nodeResult.nextWorkflowState ?? finalWorkflowState;
      finalWorkflowStatus = nodeResult.nextWorkflowStatus ?? finalWorkflowStatus;
    }

    if (currentNode.type === "terminal") {
      break;
    }

    const nextNodeId = trace.routeDecision?.nextNodeId;
    currentNode =
      nextNodeId === undefined ? undefined : input.nodesById.get(nextNodeId);

    if (nextNodeId !== undefined && currentNode === undefined) {
      simulatorWarnings.push(`Route pointed to missing node ${nextNodeId}.`);
    }
  }

  if (traces.length >= maxNodeVisits) {
    simulatorWarnings.push("Simulation stopped after max node visits.");
  }

  const finalTrace = traces.at(-1);

  return {
    durableWritesCreated: false,
    workflowIntent: input.input.workflowConfig.intent,
    ...(finalTrace !== undefined ? { finalNodeId: finalTrace.nodeId } : {}),
    ...(finalWorkflowState !== undefined ? { finalWorkflowState } : {}),
    ...(finalWorkflowStatus !== undefined ? { finalWorkflowStatus } : {}),
    traces,
    routeDecisions,
    validationErrors,
    warnings,
    proposedLedgerEvents,
    transactionPlans,
    projectionPatchPreview,
    externalCallPreview,
    simulatorWarnings,
  };
}

function simulateNode(input: {
  request: WorkflowSimulationRequest;
  node: WorkflowGraphNodeConfig;
  sources: WorkflowTemplateSources;
}): {
  trace: WorkflowSimulationNodeTrace;
  nextWorkflowState?: WorkflowState | string | undefined;
  nextWorkflowStatus?: WorkflowStatus | string | undefined;
  transactionPlan?: Record<string, unknown> | undefined;
  projectionPatches: Record<string, unknown>[];
  externalCalls: Record<string, unknown>[];
} {
  const baseTrace = {
    nodeId: input.node.nodeId,
    nodeType: input.node.type,
    title: input.node.title,
    ...permissionCheckForNode(input.request, input.node),
    validationErrors: [],
    warnings: [],
    proposedLedgerEvents: [],
  } satisfies WorkflowSimulationNodeTrace;

  if (input.node.type === "block" || input.node.type === "transaction_plan") {
    return simulateBlockNode({ ...input, baseTrace });
  }

  if (input.node.type === "external_write") {
    return simulateExternalWriteNode({ ...input, baseTrace });
  }

  if (input.node.type === "approval" || input.node.type === "approval_gate") {
    return simulateApprovalNode({ ...input, baseTrace });
  }

  const routeDecision = chooseRouteForNode({
    request: input.request,
    node: input.node,
    sources: input.sources,
  });

  return {
    trace: {
      ...baseTrace,
      ...(routeDecision !== undefined ? { routeDecision } : {}),
      proposedLedgerEvents: ledgerEventsFromRoute(input.node, routeDecision),
    },
    nextWorkflowState: routeDecision?.nextState,
    nextWorkflowStatus: routeDecision?.nextStatus,
    projectionPatches:
      input.node.type === "projection_write" ? [{ nodeId: input.node.nodeId }] : [],
    externalCalls: [],
  };
}

function simulateBlockNode(input: {
  request: WorkflowSimulationRequest;
  node: WorkflowGraphNodeConfig;
  sources: WorkflowTemplateSources;
  baseTrace: WorkflowSimulationNodeTrace;
}): ReturnType<typeof simulateNode> {
  const blockInputResult = resolveWorkflowTemplate(
    input.node.blockInput ?? {},
    input.sources,
  );
  const blockInput = blockInputResult.ok ? blockInputResult.value : {};
  const blockResult =
    input.request.blockExecutor?.({
      node: input.node,
      blockInput,
      sources: input.sources,
    }) ?? ok(defaultBlockOutput(input.request, input.node));
  const blockOutput = blockResult.ok
    ? blockResult.value
    : {
        validationErrors: [
          {
            code: blockResult.error.code,
            message: blockResult.error.safeMessage,
          },
        ],
      };
  const nextSources = {
    ...input.sources,
    blockResult: blockOutput.output ?? {},
    transactionPlan: blockOutput.transactionPlan ?? {},
  };
  const routeDecision = chooseRouteForNode({
    request: input.request,
    node: input.node,
    sources: nextSources,
    preferredOutcome: blockOutput.routeKey ?? blockOutput.outcome,
  });

  return {
    trace: {
      ...input.baseTrace,
      blockInputSummary: blockInput,
      blockOutputSummary: blockOutput.output ?? {},
      ...(routeDecision !== undefined ? { routeDecision } : {}),
      validationErrors: blockOutput.validationErrors ?? [],
      warnings: blockOutput.warnings ?? [],
      proposedLedgerEvents: [
        ...(blockOutput.proposedLedgerEvents ?? []),
        ...ledgerEventsFromRoute(input.node, routeDecision),
      ],
    },
    nextWorkflowState: routeDecision?.nextState,
    nextWorkflowStatus: routeDecision?.nextStatus,
    transactionPlan: blockOutput.transactionPlan,
    projectionPatches: blockOutput.projectionPatches ?? [],
    externalCalls: blockOutput.externalCalls ?? [],
  };
}

function simulateExternalWriteNode(input: {
  request: WorkflowSimulationRequest;
  node: WorkflowGraphNodeConfig;
  sources: WorkflowTemplateSources;
  baseTrace: WorkflowSimulationNodeTrace;
}): ReturnType<typeof simulateNode> {
  const response = fakeIntegrationPayload(input.request, input.node, "response");
  const error = fakeIntegrationPayload(input.request, input.node, "error");
  const nextSources = {
    ...input.sources,
    externalWriteResponse: response ?? {},
    externalWriteError: error ?? {},
  } as WorkflowTemplateSources & { externalWriteError: Record<string, unknown> };
  const routeDecision = chooseRouteForNode({
    request: input.request,
    node: input.node,
    sources: nextSources,
  });
  const externalCall = {
    nodeId: input.node.nodeId,
    connectionId: input.node.connectionId,
    operation: input.node.operation,
    requestPayload: {
      simulated: true,
    },
    response: response ?? null,
    error: error ?? null,
  };

  return {
    trace: {
      ...input.baseTrace,
      externalCallPreview: externalCall,
      ...(routeDecision !== undefined ? { routeDecision } : {}),
      proposedLedgerEvents: ledgerEventsFromRoute(input.node, routeDecision),
    },
    nextWorkflowState: routeDecision?.nextState,
    nextWorkflowStatus: routeDecision?.nextStatus,
    projectionPatches: [],
    externalCalls: [externalCall],
  };
}

function simulateApprovalNode(input: {
  request: WorkflowSimulationRequest;
  node: WorkflowGraphNodeConfig;
  sources: WorkflowTemplateSources;
  baseTrace: WorkflowSimulationNodeTrace;
}): ReturnType<typeof simulateNode> {
  const preferredOutcome = input.request.approvalDecisions?.[input.node.nodeId];
  const routeDecision = chooseRouteForNode({
    request: input.request,
    node: input.node,
    sources: input.sources,
    preferredOutcome,
  });

  return {
    trace: {
      ...input.baseTrace,
      ...approvalGatePreviewForNode(input.node, preferredOutcome),
      ...(routeDecision !== undefined ? { routeDecision } : {}),
      proposedLedgerEvents: ledgerEventsFromRoute(input.node, routeDecision),
    },
    nextWorkflowState: routeDecision?.nextState,
    nextWorkflowStatus: routeDecision?.nextStatus,
    projectionPatches: [],
    externalCalls: [],
  };
}

function permissionCheckForNode(
  request: WorkflowSimulationRequest,
  node: WorkflowGraphNodeConfig,
): Pick<WorkflowSimulationNodeTrace, "permissionCheck"> {
  if (node.state === undefined) {
    return {};
  }

  const configuredActions = request.workflowConfig.states[node.state]?.actions ?? [];
  if (configuredActions.length === 0) {
    return {
      permissionCheck: {
        actorId: request.actor.actorId,
        state: node.state,
        allowedTransitions: [],
        deniedTransitions: [],
      },
    };
  }

  const workflowInstance = syntheticWorkflowInstance(request, node.state);
  const availableActions = computeConfiguredAvailableActions({
    actor: request.actor,
    workflowConfig: request.workflowConfig,
    workflowInstance,
    pendingTasks: request.pendingTasks ?? [],
  });
  const allowedTransitions = availableActions.flatMap((action) => {
    return typeof action["transition"] === "string" ? [action["transition"]] : [];
  });
  const allowedTransitionSet = new Set(allowedTransitions);

  return {
    permissionCheck: {
      actorId: request.actor.actorId,
      state: node.state,
      allowedTransitions,
      deniedTransitions: configuredActions
        .map((action) => action.transition)
        .filter((transition) => !allowedTransitionSet.has(transition)),
    },
  };
}

function approvalGatePreviewForNode(
  node: WorkflowGraphNodeConfig,
  selectedDecision: string | undefined,
): Pick<WorkflowSimulationNodeTrace, "approvalGatePreview"> {
  if (node.approvalGate === undefined) {
    return {};
  }

  return {
    approvalGatePreview: {
      gateId: node.approvalGate.gateId,
      mode: node.approvalGate.mode,
      resolverCount: node.approvalGate.approverResolvers.length,
      passRuleType: node.approvalGate.passRule.type,
      ...(selectedDecision === undefined ? {} : { selectedDecision }),
    },
  };
}

function syntheticWorkflowInstance(
  request: WorkflowSimulationRequest,
  state: string,
): WorkflowInstanceRecord {
  const timestamp = new Date().toISOString();

  return {
    workflowInstanceId: "simulation_workflow_instance",
    tenantId: request.actor.tenantId,
    environmentId: "simulation",
    workflowDefinitionId: "simulation_workflow_definition",
    workflowVersionId: "simulation_workflow_version",
    intent: request.workflowConfig.intent,
    subjectType: request.workflowConfig.subjectType,
    subjectId: request.employeeProjection.employeeId,
    status: "active" as WorkflowStatus,
    state: state as WorkflowState,
    requesterActorId: request.actor.actorId,
    currentInteraction: {},
    context: request.workflowInput,
    startedAt: timestamp,
    version: 1,
    correlationId: "simulation",
    metadata: {},
    createdAt: timestamp,
    updatedAt: timestamp,
  };
}

function chooseRouteForNode(input: {
  request: WorkflowSimulationRequest;
  node: WorkflowGraphNodeConfig;
  sources: WorkflowTemplateSources & { externalWriteError?: Record<string, unknown> };
  preferredOutcome?: string | undefined;
}):
  | (NonNullable<WorkflowSimulationNodeTrace["routeDecision"]> & {
      nextState?: WorkflowState | string;
      nextStatus?: WorkflowStatus | string;
    })
  | undefined {
  const outcomes = input.node.outcomes ?? [];
  const overrideOutcome = input.request.nodeOutcomeOverrides?.[input.node.nodeId];
  const preferredOutcome = overrideOutcome ?? input.preferredOutcome;
  const matchedOutcome =
    outcomeByRoute(outcomes, preferredOutcome) ??
    outcomes.find(
      (outcome) =>
        outcome.when !== undefined &&
        workflowConditionMatches(outcome.when, input.sources),
    ) ??
    outcomes[0];

  if (matchedOutcome === undefined) {
    return undefined;
  }

  return {
    outcome: matchedOutcome.outcome,
    routeKey: matchedOutcome.routeKey ?? matchedOutcome.outcome,
    ...(matchedOutcome.nextNodeId !== undefined
      ? { nextNodeId: matchedOutcome.nextNodeId }
      : {}),
    ...(matchedOutcome.when !== undefined ? { matchedCondition: "when" } : {}),
    ...(matchedOutcome.nextState !== undefined
      ? { nextState: matchedOutcome.nextState }
      : {}),
    ...(matchedOutcome.nextStatus !== undefined
      ? { nextStatus: matchedOutcome.nextStatus }
      : {}),
  };
}

function outcomeByRoute(
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

function defaultBlockOutput(
  request: WorkflowSimulationRequest,
  node: WorkflowGraphNodeConfig,
): SimulatedBlockOutput {
  const routeKey =
    request.nodeOutcomeOverrides?.[node.nodeId] ?? node.outcomes?.[0]?.routeKey;

  return {
    routeKey,
    output: {
      simulated: true,
      nodeId: node.nodeId,
      blockName: node.block?.name,
      blockVersion: node.block?.version,
    },
    warnings: [
      {
        code: "simulation.block_not_executed",
        message:
          "No block executor was supplied; simulation used configured route fixtures.",
      },
    ],
  };
}

function fakeIntegrationPayload(
  request: WorkflowSimulationRequest,
  node: WorkflowGraphNodeConfig,
  kind: "response" | "error",
): Record<string, unknown> | undefined {
  const collection =
    kind === "response"
      ? request.fakeIntegrationResponses
      : request.fakeIntegrationErrors;
  const connectionOperationKey =
    node.connectionId !== undefined && node.operation !== undefined
      ? `${node.connectionId}.${node.operation}`
      : undefined;

  const payload =
    collection?.[node.nodeId] ??
    (connectionOperationKey === undefined
      ? undefined
      : collection?.[connectionOperationKey]);

  return isRecord(payload) ? payload : undefined;
}

function ledgerEventsFromRoute(
  node: WorkflowGraphNodeConfig,
  routeDecision: NonNullable<WorkflowSimulationNodeTrace["routeDecision"]> | undefined,
): Record<string, unknown>[] {
  if (routeDecision === undefined) {
    return [];
  }

  const outcome = outcomeByRoute(node.outcomes ?? [], routeDecision.routeKey);
  const eventTypes = [
    outcome?.eventType,
    ...(outcome?.ledgerEvents ?? []).map((eventMapping) => eventMapping.eventType),
  ].filter((eventType): eventType is string => eventType !== undefined);

  return eventTypes.map((eventType) => {
    return {
      eventType,
      nodeId: node.nodeId,
      routeKey: routeDecision.routeKey,
      simulated: true,
    };
  });
}

function initialSources(input: WorkflowSimulationRequest): WorkflowTemplateSources {
  return {
    input: input.workflowInput,
    employee: input.employeeProjection,
    workflow: {
      intent: input.workflowConfig.intent,
      subjectId: input.employeeProjection.employeeId,
      effectiveAt: input.effectiveAt,
    },
    actor: input.actor as unknown as Record<string, unknown>,
    environment: {},
    ledger: {},
    node: {},
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
