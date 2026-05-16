import {
  LEDGER_EVENT_TYPES,
  WORKFLOW_STATUSES,
  err,
  notFoundError,
  ok,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  ApprovalGroupRecord,
  ApprovalTaskRecord,
  IntegrationOutboxRecord,
  LedgerEventRecord,
  Repositories,
  WorkflowInstanceRecord,
  WorkflowTransitionAttemptRecord,
} from "@hcm-next/data-store";
import type {
  ApiRequestContext,
  AppDependencies,
} from "../../shared/runtime-dependencies.js";
import { requireWorkflowAdmin } from "../contracts/admin-permissions.js";
import type {
  ApprovalGateDebugInfo,
  ApprovalTaskDebugInfo,
  ExternalCallDebugInfo,
  GraphNodeDebugInfo,
  LedgerEventDebugInfo,
  NodeExecutionDebugInfo,
  RepairOptionDebugInfo,
  RepairRequirementDebugInfo,
  RuntimeDebuggerResponse,
  StuckWorkflowDetection,
  TransitionDebugInfo,
  WorkflowFailureDebugInfo,
} from "../contracts/admin-api-contracts.js";
import { redactSensitiveWorkflowDebugValue } from "./redaction.js";
import { resolvePinnedWorkflowConfig } from "../../shared/workflow-config-resolution.js";
import type {
  WorkflowConfig,
  WorkflowGraphNodeConfig,
} from "../../shared/workflow-config.js";

const approvalWaitingThresholdMs = 72 * 60 * 60 * 1000;
const runtimeNodeId = "runtime";
const terminalWorkflowStatuses = new Set<string>([
  WORKFLOW_STATUSES.COMPLETED,
  WORKFLOW_STATUSES.REJECTED,
  WORKFLOW_STATUSES.CANCELED,
  WORKFLOW_STATUSES.FAILED,
  WORKFLOW_STATUSES.SUPERSEDED,
]);

type RuntimeDebugParts = {
  workflowInstance: WorkflowInstanceRecord;
  workflowConfig: WorkflowConfig;
  timeline: LedgerEventRecord[];
  approvalGroups: ApprovalGroupRecord[];
  approvalTasks: ApprovalTaskRecord[];
  transitionAttempts: WorkflowTransitionAttemptRecord[];
  externalCalls: ExternalCallDebugInfo[];
};

/**
 * Builds the admin runtime debugger contract for one live workflow instance.
 */
export function getWorkflowRuntimeDebugger(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    dependencies.logger?.warn("workflow debugger authorization denied", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowInstanceId,
      errorCode: authorizationResult.error.code,
    });
    return authorizationResult;
  }

  dependencies.logger?.info("workflow runtime debugger requested", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowInstanceId,
  });

  const debugPartsResult = loadRuntimeDebugParts(
    dependencies.repositories,
    requestContext,
    workflowInstanceId,
  );
  if (!debugPartsResult.ok) {
    dependencies.logger?.warn("workflow runtime debugger load failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowInstanceId,
      errorCode: debugPartsResult.error.code,
    });
    return debugPartsResult;
  }

  return ok(
    buildRuntimeDebuggerResponse(
      dependencies.repositories,
      requestContext,
      debugPartsResult.value,
    ) as unknown as Record<string, unknown>,
  );
}

/**
 * Builds repair options for the current workflow state without mutating runtime state.
 */
export function buildRepairOptions(input: {
  workflowInstance: WorkflowInstanceRecord;
  currentNode?: WorkflowGraphNodeConfig;
  hasExternalFailure: boolean;
}): RepairOptionDebugInfo[] {
  const isTerminal = terminalWorkflowStatuses.has(input.workflowInstance.status);
  const isWaitingRepair =
    input.workflowInstance.status === WORKFLOW_STATUSES.WAITING_REPAIR ||
    input.workflowInstance.state === "waiting_repair";
  const isSideEffectNode = nodeHasSideEffects(input.currentNode);

  return [
    {
      action: "retry_integration",
      label: "Retry integration",
      enabled: !isTerminal && input.hasExternalFailure,
      ...(!input.hasExternalFailure
        ? { reason: "No failed integration is currently visible." }
        : {}),
    },
    {
      action: "cancel_workflow",
      label: "Cancel workflow",
      enabled: !isTerminal,
      ...(isTerminal ? { reason: "Workflow is already terminal." } : {}),
    },
    {
      action: "reopen_repair",
      label: "Reopen repair",
      enabled: !isTerminal && isWaitingRepair,
      ...(!isWaitingRepair ? { reason: "Workflow is not in repair." } : {}),
    },
    {
      action: "reevaluate_current_node",
      label: "Re-evaluate current node",
      enabled: !isTerminal && !isSideEffectNode,
      ...(isSideEffectNode
        ? { reason: "Current node has side effects and cannot be re-evaluated." }
        : {}),
    },
    {
      action: "rerun_simulation_from_current_state",
      label: "Rerun simulation from current state",
      enabled: true,
    },
  ];
}

function loadRuntimeDebugParts(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
): Result<RuntimeDebugParts, AppError> {
  const workflowResult = repositories.workflows.findInstanceById(workflowInstanceId);
  if (!workflowResult.ok) {
    return workflowResult;
  }

  if (workflowResult.value.tenantId !== requestContext.tenantId) {
    return err(notFoundError("Workflow instance", { workflowInstanceId }));
  }

  const workflowConfigResult = resolvePinnedWorkflowConfig(
    repositories,
    workflowResult.value,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const timelineResult = repositories.ledger.findTimelineForWorkflow(
    requestContext.tenantId,
    workflowInstanceId,
    workflowResult.value.changeRequestId,
  );
  if (!timelineResult.ok) {
    return timelineResult;
  }

  const approvalGroupsResult =
    repositories.approvalGroups.findByWorkflow(workflowInstanceId);
  if (!approvalGroupsResult.ok) {
    return approvalGroupsResult;
  }

  return ok({
    workflowInstance: workflowResult.value,
    workflowConfig: workflowConfigResult.value,
    timeline: timelineResult.value,
    approvalGroups: approvalGroupsResult.value,
    approvalTasks: approvalTasksForWorkflow(repositories, workflowInstanceId),
    transitionAttempts: transitionAttemptsForWorkflow(
      repositories,
      requestContext.tenantId,
      workflowInstanceId,
    ),
    externalCalls: externalCallsForWorkflow(repositories, workflowResult.value),
  });
}

function buildRuntimeDebuggerResponse(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  parts: RuntimeDebugParts,
): RuntimeDebuggerResponse {
  const graphNode = currentGraphNode(parts.workflowConfig, parts.workflowInstance);
  const ledgerEventsByNode = ledgerEventsGroupedByNode(parts);
  const externalCallsByNode = externalCallsGroupedByNode(
    parts.externalCalls,
    ledgerEventsByNode,
  );
  const hasExternalFailure = hasFailedExternalCall(parts.externalCalls, parts.timeline);
  const failure = failureDebugInfo(parts, ledgerEventsByNode);
  const stuckDetections = detectStuckWorkflow({
    workflowInstance: parts.workflowInstance,
    workflowConfig: parts.workflowConfig,
    approvalTasks: parts.approvalTasks,
    transitionAttempts: parts.transitionAttempts,
    hasExternalFailure,
  });
  const currentNode = graphNodeToDebugInfo(graphNode);
  const workflowVersion = repositories.store.workflowVersions.get(
    parts.workflowInstance.workflowVersionId,
  );
  const lastTransitionInfo = lastTransition(parts.transitionAttempts);
  const activeApprovalGateInfo = activeApprovalGate(parts.approvalGroups);
  const currentRepairInfo = currentRepairRequirement(parts.workflowInstance);

  return {
    correlationId: requestContext.correlationId,
    generatedAt: new Date().toISOString(),
    workflowInstanceId: parts.workflowInstance.workflowInstanceId,
    workflowIntent: parts.workflowInstance.intent,
    pinnedWorkflowVersion: {
      workflowDefinitionId: parts.workflowInstance.workflowDefinitionId,
      workflowVersionId: parts.workflowInstance.workflowVersionId,
      ...(workflowVersion !== undefined
        ? {
            versionNumber: workflowVersion.versionNumber,
            status: workflowVersion.status,
            ...(workflowVersion.configHash !== undefined
              ? { configHash: workflowVersion.configHash }
              : {}),
          }
        : {}),
    },
    currentState: parts.workflowInstance.state,
    ...(currentNode !== undefined ? { currentGraphNode: currentNode } : {}),
    workflowStatus: parts.workflowInstance.status,
    workflowVersion: parts.workflowInstance.version,
    ...(lastTransitionInfo !== undefined ? { lastTransition: lastTransitionInfo } : {}),
    pendingApprovalTasks: parts.approvalTasks
      .filter((task) => task.status === "pending")
      .map(approvalTaskDebugInfo),
    ...(activeApprovalGateInfo !== undefined
      ? {
          approvalGate: approvalGateDebugInfo(
            activeApprovalGateInfo,
            parts.approvalTasks,
          ),
        }
      : {}),
    ...(currentRepairInfo !== undefined ? { currentRepair: currentRepairInfo } : {}),
    nodeExecutionHistory: nodeExecutionHistory(
      parts.workflowConfig,
      ledgerEventsByNode,
    ),
    ledgerEventsByNode,
    externalCallsByNode,
    ...(failure !== undefined ? { failure } : {}),
    compensationEligibility: compensationEligibility(parts, failure),
    stuckDetections,
    repairOptions: buildRepairOptions({
      workflowInstance: parts.workflowInstance,
      ...(graphNode !== undefined ? { currentNode: graphNode } : {}),
      hasExternalFailure,
    }),
  };
}

function failureDebugInfo(
  parts: RuntimeDebugParts,
  ledgerEventsByNode: Record<string, LedgerEventDebugInfo[]>,
): WorkflowFailureDebugInfo | undefined {
  for (const [nodeId, events] of Object.entries(ledgerEventsByNode)) {
    const failedEvent = events.find((event) => {
      return event.eventType.endsWith("Failed") || event.eventType.includes("Failed");
    });

    if (failedEvent === undefined) {
      continue;
    }

    const payload = isRecord(failedEvent.payload) ? failedEvent.payload : {};

    return {
      failedNodeId: nodeId,
      errorCode:
        stringValue(payload["code"]) ?? stringValue(payload["errorCode"]) ?? "FAILED",
      safeMessage:
        stringValue(payload["safeMessage"]) ??
        stringValue(payload["message"]) ??
        `Workflow node ${nodeId} recorded ${failedEvent.eventType}.`,
    };
  }

  if (parts.workflowInstance.failedAt !== undefined) {
    return {
      failedNodeId:
        stringValue(parts.workflowInstance.context["activeNodeId"]) ?? runtimeNodeId,
      errorCode: "WORKFLOW_FAILED",
      safeMessage: "Workflow instance is marked failed.",
    };
  }

  return undefined;
}

function compensationEligibility(
  parts: RuntimeDebugParts,
  failure: WorkflowFailureDebugInfo | undefined,
): RuntimeDebuggerResponse["compensationEligibility"] {
  if (failure === undefined) {
    return {
      eligible: false,
      reason: "No failed node is visible.",
    };
  }

  const failedNode = (parts.workflowConfig.graph?.nodes ?? []).find((node) => {
    return node.nodeId === failure.failedNodeId;
  });
  const hasSagaPolicy = parts.workflowConfig.saga !== undefined;
  const hasNodeCompensation = failedNode?.compensation !== undefined;
  const eligible = hasSagaPolicy || hasNodeCompensation;

  return {
    eligible,
    reason: eligible
      ? "Workflow has configured saga or node compensation metadata."
      : "No configured compensation policy is available for the failed node.",
  };
}

function currentGraphNode(
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
): WorkflowGraphNodeConfig | undefined {
  const activeNodeId = stringValue(workflowInstance.context["activeNodeId"]);
  const nodes = workflowConfig.graph?.nodes ?? [];

  if (activeNodeId !== undefined) {
    const activeNode = nodes.find((node) => node.nodeId === activeNodeId);

    if (activeNode !== undefined) {
      return activeNode;
    }
  }

  return nodes.find((node) => node.state === workflowInstance.state);
}

function graphNodeToDebugInfo(
  graphNode: WorkflowGraphNodeConfig | undefined,
): GraphNodeDebugInfo | undefined {
  if (graphNode === undefined) {
    return undefined;
  }

  return {
    nodeId: graphNode.nodeId,
    type: graphNode.type,
    title: graphNode.title,
    ...(graphNode.state !== undefined ? { state: graphNode.state } : {}),
    routeKeys: (graphNode.outcomes ?? []).map((outcome) => {
      return outcome.routeKey ?? outcome.outcome;
    }),
  };
}

function lastTransition(
  transitionAttempts: WorkflowTransitionAttemptRecord[],
): TransitionDebugInfo | undefined {
  const attempt = [...transitionAttempts].sort((left, right) => {
    return left.createdAt.localeCompare(right.createdAt);
  })[transitionAttempts.length - 1];

  if (attempt === undefined) {
    return undefined;
  }

  return {
    transition: attempt.transition,
    actorId: attempt.actorId,
    idempotencyKey: attempt.idempotencyKey,
    expectedVersion: attempt.expectedVersion,
    status: attempt.status,
    ...(attempt.responsePayload !== undefined
      ? {
          result: redactSensitiveWorkflowDebugValue(attempt.responsePayload) as Record<
            string,
            unknown
          >,
        }
      : {}),
    ...(attempt.error !== undefined
      ? {
          error: redactSensitiveWorkflowDebugValue(attempt.error) as Record<
            string,
            unknown
          >,
        }
      : {}),
  };
}

function approvalTaskDebugInfo(
  approvalTask: ApprovalTaskRecord,
): ApprovalTaskDebugInfo {
  return {
    approvalTaskId: approvalTask.approvalTaskId,
    assigneeActorId: approvalTask.assigneeActorId,
    assigneeRole: approvalTask.assigneeRole,
    status: approvalTask.status,
    ...(approvalTask.gateNodeId !== undefined
      ? { gateNodeId: approvalTask.gateNodeId }
      : {}),
    ...(approvalTask.taskVersion !== undefined
      ? { taskVersion: approvalTask.taskVersion }
      : {}),
  };
}

function approvalGateDebugInfo(
  approvalGroup: ApprovalGroupRecord,
  approvalTasks: ApprovalTaskRecord[],
): ApprovalGateDebugInfo {
  const groupTasks = approvalTasks.filter((task) => {
    return task.approvalGroupId === approvalGroup.approvalGroupId;
  });

  return {
    approvalGroupId: approvalGroup.approvalGroupId,
    gateNodeId: approvalGroup.gateNodeId,
    mode: approvalGroup.mode,
    status: approvalGroup.status,
    passRule: approvalGroup.passRule,
    failurePolicy: approvalGroup.failurePolicy,
    taskCount: groupTasks.length,
  };
}

function currentRepairRequirement(
  workflowInstance: WorkflowInstanceRecord,
): RepairRequirementDebugInfo | undefined {
  const interactionType = stringValue(workflowInstance.currentInteraction["type"]);
  const isRepairState =
    workflowInstance.status === WORKFLOW_STATUSES.WAITING_REPAIR ||
    workflowInstance.state === "waiting_repair" ||
    interactionType === "repair";

  if (!isRepairState) {
    return undefined;
  }

  return {
    state: workflowInstance.state,
    status: workflowInstance.status,
    interaction: redactSensitiveWorkflowDebugValue(
      workflowInstance.currentInteraction,
    ) as Record<string, unknown>,
    requiredFields: requiredFieldsFromInteraction(workflowInstance.currentInteraction),
  };
}

function ledgerEventsGroupedByNode(
  parts: RuntimeDebugParts,
): Record<string, LedgerEventDebugInfo[]> {
  const eventGroups: Record<string, LedgerEventDebugInfo[]> = {};

  for (const event of parts.timeline) {
    const nodeId = nodeIdForLedgerEvent(
      parts.workflowConfig,
      parts.approvalTasks,
      event,
    );
    const eventInfo = ledgerEventDebugInfo(event);

    eventGroups[nodeId] = [...(eventGroups[nodeId] ?? []), eventInfo];
  }

  return eventGroups;
}

function ledgerEventDebugInfo(event: LedgerEventRecord): LedgerEventDebugInfo {
  return {
    eventId: event.eventId,
    eventType: event.eventType,
    eventSequence: event.eventSequence,
    occurredAt: event.occurredAt,
    actorId: event.actorId,
    payload: redactSensitiveWorkflowDebugValue(event.payload),
  };
}

function nodeIdForLedgerEvent(
  workflowConfig: WorkflowConfig,
  approvalTasks: ApprovalTaskRecord[],
  event: LedgerEventRecord,
): string {
  const payloadNodeId = nodeIdFromPayload(event.payload);
  if (payloadNodeId !== undefined) {
    return payloadNodeId;
  }

  const approvalTask = approvalTasks.find((task) => {
    return task.approvalTaskId === event.approvalTaskId;
  });
  if (approvalTask?.gateNodeId !== undefined) {
    return approvalTask.gateNodeId;
  }

  const mappedNode = workflowConfig.ledger?.nodeEvents?.find((nodeEvent) => {
    return nodeEvent.eventType === event.eventType && nodeEvent.nodeId !== undefined;
  });

  return mappedNode?.nodeId ?? runtimeNodeId;
}

function nodeIdFromPayload(payload: Record<string, unknown>): string | undefined {
  const nodeId = stringValue(payload["nodeId"]);
  if (nodeId !== undefined) {
    return nodeId;
  }

  const externalWritePayload = payload["externalWritePayload"];
  if (typeof externalWritePayload === "object" && externalWritePayload !== null) {
    return stringValue((externalWritePayload as Record<string, unknown>)["nodeId"]);
  }

  const externalWriteExecutions = payload["externalWriteExecutions"];
  if (Array.isArray(externalWriteExecutions)) {
    for (const execution of externalWriteExecutions) {
      if (typeof execution !== "object" || execution === null) {
        continue;
      }

      const nextNodeId = stringValue(
        (execution as Record<string, unknown>)["nextNodeId"],
      );
      if (nextNodeId !== undefined) {
        return nextNodeId;
      }
    }
  }

  return undefined;
}

function externalCallsGroupedByNode(
  externalCalls: ExternalCallDebugInfo[],
  ledgerEventsByNode: Record<string, LedgerEventDebugInfo[]>,
): Record<string, ExternalCallDebugInfo[]> {
  const groupedCalls: Record<string, ExternalCallDebugInfo[]> = {};

  for (const call of externalCalls) {
    const matchingNodeId =
      nodeIdForExternalCall(call, ledgerEventsByNode) ?? runtimeNodeId;

    groupedCalls[matchingNodeId] = [...(groupedCalls[matchingNodeId] ?? []), call];
  }

  return groupedCalls;
}

function nodeIdForExternalCall(
  externalCall: ExternalCallDebugInfo,
  ledgerEventsByNode: Record<string, LedgerEventDebugInfo[]>,
): string | undefined {
  for (const [nodeId, events] of Object.entries(ledgerEventsByNode)) {
    const matchingEvent = events.find((event) => {
      const payload = event.payload;

      if (typeof payload !== "object" || payload === null) {
        return false;
      }

      const record = payload as Record<string, unknown>;
      return (
        record["connectionId"] === externalCall.connectionId ||
        record["operation"] === externalCall.operation
      );
    });

    if (matchingEvent !== undefined) {
      return nodeId;
    }
  }

  return undefined;
}

function nodeExecutionHistory(
  workflowConfig: WorkflowConfig,
  ledgerEventsByNode: Record<string, LedgerEventDebugInfo[]>,
): NodeExecutionDebugInfo[] {
  const nodesById = new Map(
    (workflowConfig.graph?.nodes ?? []).map((node) => {
      return [node.nodeId, node];
    }),
  );

  return Object.entries(ledgerEventsByNode)
    .map(([nodeId, events]) => {
      const graphNode = nodesById.get(nodeId);
      const routeKeys = routeKeysForNode(workflowConfig, nodeId, events);

      return {
        nodeId,
        ...(graphNode !== undefined
          ? {
              nodeType: graphNode.type,
              title: graphNode.title,
            }
          : {}),
        eventTypes: events.map((event) => event.eventType),
        firstEventSequence: events[0]?.eventSequence ?? 0,
        lastEventSequence: events[events.length - 1]?.eventSequence ?? 0,
        routeKeys,
        ...(routeKeys.length === 0
          ? {}
          : {
              routeDecision: {
                routeKeys,
                matchedCondition: "ledger_event",
              },
            }),
        ...(graphNode?.blockInput !== undefined
          ? {
              blockInputSummary: redactSensitiveWorkflowDebugValue(
                graphNode.blockInput,
              ),
            }
          : {}),
        blockOutputSummary: redactSensitiveWorkflowDebugValue(
          events.map((event) => event.payload),
        ),
      };
    })
    .sort((left, right) => left.firstEventSequence - right.firstEventSequence);
}

function routeKeysForNode(
  workflowConfig: WorkflowConfig,
  nodeId: string,
  events: LedgerEventDebugInfo[],
): string[] {
  const eventTypes = new Set(
    events.map((event) => {
      return event.eventType;
    }),
  );

  return (
    workflowConfig.ledger?.nodeEvents
      ?.filter((mapping) => {
        return mapping.nodeId === nodeId && eventTypes.has(mapping.eventType);
      })
      .flatMap((mapping) => {
        return mapping.routeKey === undefined ? [] : [mapping.routeKey];
      }) ?? []
  );
}

function detectStuckWorkflow(input: {
  workflowInstance: WorkflowInstanceRecord;
  workflowConfig: WorkflowConfig;
  approvalTasks: ApprovalTaskRecord[];
  transitionAttempts: WorkflowTransitionAttemptRecord[];
  hasExternalFailure: boolean;
}): StuckWorkflowDetection[] {
  return [
    ...waitingApprovalDetections(input.approvalTasks),
    ...(input.hasExternalFailure ? [failedExternalIntegrationDetection()] : []),
    ...missingRepairInputDetections(input.workflowInstance),
    ...noAvailableActionDetections(input.workflowConfig, input.workflowInstance),
    ...versionConflictDetections(input.transitionAttempts),
  ];
}

function waitingApprovalDetections(
  approvalTasks: ApprovalTaskRecord[],
): StuckWorkflowDetection[] {
  const nowMs = Date.now();

  return approvalTasks
    .filter((task) => {
      return (
        task.status === "pending" &&
        nowMs - Date.parse(task.createdAt) > approvalWaitingThresholdMs
      );
    })
    .map((task) => {
      return {
        code: "waiting_approval_beyond_threshold",
        severity: "warning",
        message: "A pending approval task has exceeded the admin threshold.",
        details: {
          approvalTaskId: task.approvalTaskId,
          assigneeActorId: task.assigneeActorId,
          createdAt: task.createdAt,
        },
      };
    });
}

function failedExternalIntegrationDetection(): StuckWorkflowDetection {
  return {
    code: "failed_external_integration",
    severity: "error",
    message: "An external integration failed or routed the workflow to repair.",
    details: {},
  };
}

function missingRepairInputDetections(
  workflowInstance: WorkflowInstanceRecord,
): StuckWorkflowDetection[] {
  const repairRequirement = currentRepairRequirement(workflowInstance);

  if (repairRequirement === undefined || repairRequirement.requiredFields.length > 0) {
    return [];
  }

  return [
    {
      code: "missing_repair_input_schema",
      severity: "warning",
      message: "Workflow is in repair without declared repair input requirements.",
      details: {
        currentInteraction: workflowInstance.currentInteraction,
      },
    },
  ];
}

function noAvailableActionDetections(
  input: {
    states: WorkflowConfig["states"];
  },
  workflowInstance: WorkflowInstanceRecord,
): StuckWorkflowDetection[] {
  if (terminalWorkflowStatuses.has(workflowInstance.status)) {
    return [];
  }

  const stateActions = input.states[workflowInstance.state]?.actions ?? [];
  if (stateActions.length > 0) {
    return [];
  }

  return [
    {
      code: "no_available_configured_actions",
      severity: "warning",
      message: "The current workflow state has no configured actions.",
      details: {
        state: workflowInstance.state,
      },
    },
  ];
}

function versionConflictDetections(
  transitionAttempts: WorkflowTransitionAttemptRecord[],
): StuckWorkflowDetection[] {
  return transitionAttempts
    .filter((attempt) => {
      return (
        attempt.status === "failed" && attempt.error?.["code"] === "VERSION_CONFLICT"
      );
    })
    .map((attempt) => {
      return {
        code: "workflow_version_conflict_attempt",
        severity: "info",
        message: "A transition failed because the expected workflow version was stale.",
        details: {
          idempotencyKey: attempt.idempotencyKey,
          transition: attempt.transition,
        },
      };
    });
}

function activeApprovalGate(
  approvalGroups: ApprovalGroupRecord[],
): ApprovalGroupRecord | undefined {
  return approvalGroups.find((approvalGroup) => {
    return approvalGroup.status === "active";
  });
}

function approvalTasksForWorkflow(
  repositories: Repositories,
  workflowInstanceId: string,
): ApprovalTaskRecord[] {
  return [...repositories.store.approvalTasks.values()].filter((task) => {
    return task.workflowInstanceId === workflowInstanceId;
  });
}

function transitionAttemptsForWorkflow(
  repositories: Repositories,
  tenantId: string,
  workflowInstanceId: string,
): WorkflowTransitionAttemptRecord[] {
  return [...repositories.store.transitionAttempts.values()]
    .filter((attempt) => {
      return (
        attempt.tenantId === tenantId &&
        attempt.workflowInstanceId === workflowInstanceId
      );
    })
    .sort((left, right) => left.createdAt.localeCompare(right.createdAt));
}

function externalCallsForWorkflow(
  repositories: Repositories,
  workflowInstance: WorkflowInstanceRecord,
): ExternalCallDebugInfo[] {
  const outboxRowsResult =
    workflowInstance.changeRequestId === undefined
      ? ok([])
      : repositories.integrationOutbox.findByChangeRequest(
          workflowInstance.changeRequestId,
        );
  const outboxCalls = outboxRowsResult.ok
    ? integrationOutboxCalls(outboxRowsResult.value)
    : [];
  const failedLedgerCalls = repositories.store.ledgerEvents
    .filter((event) => {
      return (
        event.tenantId === workflowInstance.tenantId &&
        event.workflowInstanceId === workflowInstance.workflowInstanceId &&
        event.eventType === LEDGER_EVENT_TYPES.EXTERNAL_WRITE_FAILED
      );
    })
    .map(failedExternalCallFromLedgerEvent);

  return [...outboxCalls, ...failedLedgerCalls];
}

function integrationOutboxCalls(
  outboxRows: IntegrationOutboxRecord[],
): ExternalCallDebugInfo[] {
  return outboxRows.map((outboxRow) => {
    return {
      connectionId: outboxRow.destination,
      operation: outboxRow.operation,
      status: outboxRow.status,
      idempotencyKey: outboxRow.idempotencyKey,
      attemptCount: outboxRow.attemptCount,
      requestPayload: redactSensitiveWorkflowDebugValue(outboxRow.requestPayload),
      ...(outboxRow.responsePayload !== undefined
        ? {
            responsePayload: redactSensitiveWorkflowDebugValue(
              outboxRow.responsePayload,
            ),
          }
        : {}),
    };
  });
}

function failedExternalCallFromLedgerEvent(
  ledgerEvent: LedgerEventRecord,
): ExternalCallDebugInfo {
  return {
    connectionId: stringValue(ledgerEvent.payload["connectionId"]) ?? "unknown",
    operation: stringValue(ledgerEvent.payload["operation"]) ?? "unknown",
    status: "failed",
    ...(ledgerEvent.idempotencyKey !== undefined
      ? { idempotencyKey: ledgerEvent.idempotencyKey }
      : {}),
    responsePayload: redactSensitiveWorkflowDebugValue(ledgerEvent.payload),
  };
}

function hasFailedExternalCall(
  externalCalls: ExternalCallDebugInfo[],
  timeline: LedgerEventRecord[],
): boolean {
  return (
    externalCalls.some((call) => {
      return call.status === "failed" || call.status === "dead_letter";
    }) ||
    timeline.some((event) => {
      return event.eventType === LEDGER_EVENT_TYPES.EXTERNAL_WRITE_FAILED;
    })
  );
}

function requiredFieldsFromInteraction(interaction: Record<string, unknown>): string[] {
  const jsonSchema = interaction["jsonSchema"];
  if (typeof jsonSchema !== "object" || jsonSchema === null) {
    return [];
  }

  const required = (jsonSchema as Record<string, unknown>)["required"];
  if (!Array.isArray(required)) {
    return [];
  }

  return required.filter((field): field is string => {
    return typeof field === "string" && field.trim().length > 0;
  });
}

function nodeHasSideEffects(graphNode: WorkflowGraphNodeConfig | undefined): boolean {
  if (graphNode === undefined) {
    return false;
  }

  return (
    graphNode.transaction?.sideEffect === true ||
    graphNode.type === "data_write" ||
    graphNode.type === "external_write" ||
    graphNode.type === "projection_write"
  );
}

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0
    ? value.trim()
    : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
