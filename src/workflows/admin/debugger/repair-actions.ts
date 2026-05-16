import {
  CHANGE_REQUEST_STATUSES,
  INTEGRATION_OUTBOX_STATUSES,
  LEDGER_EVENT_TYPES,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  err,
  idempotencyConflictError,
  notFoundError,
  ok,
  validationFailedError,
  versionConflictError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  nowIso,
  type LedgerEventRecord,
  type Repositories,
  type WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import type {
  ApiRequestContext,
  AppDependencies,
} from "../../shared/runtime-dependencies.js";
import { appendWorkflowLedgerEvent } from "../../shared/workflow-ledger-events.js";
import { terminalInteraction } from "../../shared/workflow-response.js";
import { numberField, stringField } from "../../shared/json-fields.js";
import { resolvePinnedWorkflowConfig } from "../../shared/workflow-config-resolution.js";
import type { WorkflowGraphNodeConfig } from "../../shared/workflow-config.js";
import { requireWorkflowAdmin } from "../contracts/admin-permissions.js";
import type {
  RepairActionResponse,
  RepairOptionDebugInfo,
} from "../contracts/admin-api-contracts.js";
import { buildRepairOptions } from "./debugger.js";

type WorkflowAdminRepairAction =
  | "retry_integration"
  | "cancel_workflow"
  | "reopen_repair"
  | "reevaluate_current_node"
  | "rerun_simulation_from_current_state";

type ParsedRepairActionRequest = {
  action: WorkflowAdminRepairAction;
  idempotencyKey: string;
  expectedVersion: number;
};

const terminalWorkflowStatuses = new Set<string>([
  WORKFLOW_STATUSES.COMPLETED,
  WORKFLOW_STATUSES.REJECTED,
  WORKFLOW_STATUSES.CANCELED,
  WORKFLOW_STATUSES.FAILED,
  WORKFLOW_STATUSES.SUPERSEDED,
]);

const supportedRepairActions = new Set<string>([
  "retry_integration",
  "cancel_workflow",
  "reopen_repair",
  "reevaluate_current_node",
  "rerun_simulation_from_current_state",
]);

/**
 * Applies an admin repair action request and records the operator action in the ledger.
 */
export function submitWorkflowAdminRepairAction(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const { logger } = dependencies;

  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    logger?.warn("repair action authorization denied", {
      workflowInstanceId,
      actorId: requestContext.actor.actorId,
    });
    return authorizationResult;
  }

  const requestResult = parseRepairActionRequest(body);
  if (!requestResult.ok) {
    return requestResult;
  }

  const workflowResult =
    dependencies.repositories.workflows.findInstanceById(workflowInstanceId);
  if (!workflowResult.ok) {
    return workflowResult;
  }

  if (workflowResult.value.tenantId !== requestContext.tenantId) {
    return err(notFoundError("Workflow instance", { workflowInstanceId }));
  }

  const { action, idempotencyKey } = requestResult.value;

  logger?.info("repair action started", {
    workflowInstanceId,
    action,
    actorId: requestContext.actor.actorId,
    idempotencyKey,
  });

  const replayResult = replayExistingRepairAction(
    dependencies.repositories,
    requestContext,
    workflowResult.value,
    requestResult.value,
  );
  if (replayResult !== undefined) {
    if (replayResult.ok) {
      logger?.info("replaying idempotent repair action", {
        workflowInstanceId,
        action,
        idempotencyKey,
      });
    }
    return replayResult;
  }

  const guardResult = guardRepairAction(workflowResult.value, requestResult.value);
  if (!guardResult.ok) {
    logger?.warn("repair action guard failed", {
      workflowInstanceId,
      action,
      errorCode: guardResult.error.code,
    });
    return guardResult;
  }

  const applyResult = applyRepairAction(
    dependencies.repositories,
    requestContext,
    workflowResult.value,
    requestResult.value,
  );

  if (applyResult.ok) {
    logger?.info("repair action completed", { workflowInstanceId, action });
  }

  return applyResult;
}

function parseRepairActionRequest(
  body: Record<string, unknown>,
): Result<ParsedRepairActionRequest, AppError> {
  const action = stringField(body, "action");
  const idempotencyKey = stringField(body, "idempotencyKey");
  const expectedVersion = numberField(body, "expectedVersion");

  if (
    action === undefined ||
    !supportedRepairActions.has(action) ||
    idempotencyKey === undefined ||
    expectedVersion === undefined
  ) {
    return err(validationFailedError({ action, idempotencyKey, expectedVersion }));
  }

  return ok({
    action: action as WorkflowAdminRepairAction,
    idempotencyKey,
    expectedVersion,
  });
}

function replayExistingRepairAction(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  request: ParsedRepairActionRequest,
): Result<Record<string, unknown>, AppError> | undefined {
  const existingEvent = repositories.store.ledgerEvents.find((event) => {
    return (
      event.tenantId === requestContext.tenantId &&
      event.workflowInstanceId === workflowInstance.workflowInstanceId &&
      event.idempotencyKey === request.idempotencyKey &&
      event.eventType === LEDGER_EVENT_TYPES.WORKFLOW_ADMIN_REPAIR_ACTION_REQUESTED
    );
  });

  if (existingEvent === undefined) {
    return undefined;
  }

  if (existingEvent.payload["action"] !== request.action) {
    return err(
      idempotencyConflictError({
        idempotencyKey: request.idempotencyKey,
        previousAction: existingEvent.payload["action"],
        requestedAction: request.action,
      }),
    );
  }

  return ok(
    repairActionResponse({
      requestContext,
      workflowInstance,
      action: request.action,
      accepted: true,
      idempotentReplay: true,
      ledgerEvents: [existingEvent],
      repairOptions: currentRepairOptions(repositories, workflowInstance),
    }) as unknown as Record<string, unknown>,
  );
}

function guardRepairAction(
  workflowInstance: WorkflowInstanceRecord,
  request: ParsedRepairActionRequest,
): Result<true, AppError> {
  if (workflowInstance.version !== request.expectedVersion) {
    return err(
      versionConflictError({
        workflowInstanceId: workflowInstance.workflowInstanceId,
        expectedVersion: request.expectedVersion,
        actualVersion: workflowInstance.version,
      }),
    );
  }

  if (
    terminalWorkflowStatuses.has(workflowInstance.status) &&
    request.action !== "rerun_simulation_from_current_state"
  ) {
    return err(
      validationFailedError({
        action: request.action,
        status: workflowInstance.status,
        reason: "Terminal workflows cannot be repaired by this action.",
      }),
    );
  }

  return ok(true);
}

function applyRepairAction(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  request: ParsedRepairActionRequest,
): Result<Record<string, unknown>, AppError> {
  const genericLedgerResult = appendAdminRepairLedgerEvent(
    repositories,
    requestContext,
    workflowInstance,
    LEDGER_EVENT_TYPES.WORKFLOW_ADMIN_REPAIR_ACTION_REQUESTED,
    request,
    { action: request.action },
  );
  if (!genericLedgerResult.ok) {
    return genericLedgerResult;
  }

  const actionResult = applySpecificRepairAction(
    repositories,
    requestContext,
    workflowInstance,
    request,
  );
  if (!actionResult.ok) {
    return actionResult;
  }

  return ok(
    repairActionResponse({
      requestContext,
      workflowInstance: actionResult.value.workflowInstance,
      action: request.action,
      accepted: true,
      ledgerEvents: [
        genericLedgerResult.value,
        ...(actionResult.value.ledgerEvent === undefined
          ? []
          : [actionResult.value.ledgerEvent]),
      ],
      repairOptions: currentRepairOptions(
        repositories,
        actionResult.value.workflowInstance,
      ),
    }) as unknown as Record<string, unknown>,
  );
}

function applySpecificRepairAction(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  request: ParsedRepairActionRequest,
): Result<
  {
    workflowInstance: WorkflowInstanceRecord;
    ledgerEvent?: LedgerEventRecord;
  },
  AppError
> {
  if (request.action === "retry_integration") {
    return retryIntegration(repositories, requestContext, workflowInstance, request);
  }

  if (request.action === "cancel_workflow") {
    return cancelWorkflow(repositories, requestContext, workflowInstance, request);
  }

  if (request.action === "reopen_repair") {
    return reopenRepair(repositories, requestContext, workflowInstance, request);
  }

  return ok({ workflowInstance });
}

function retryIntegration(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  request: ParsedRepairActionRequest,
): Result<
  {
    workflowInstance: WorkflowInstanceRecord;
    ledgerEvent: LedgerEventRecord;
  },
  AppError
> {
  const updatedOutboxIds = resetFailedIntegrationOutboxRows(
    repositories,
    workflowInstance,
  );
  const ledgerResult = appendAdminRepairLedgerEvent(
    repositories,
    requestContext,
    workflowInstance,
    LEDGER_EVENT_TYPES.WORKFLOW_ADMIN_INTEGRATION_RETRY_REQUESTED,
    request,
    { updatedOutboxIds },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok({ workflowInstance, ledgerEvent: ledgerResult.value });
}

function cancelWorkflow(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  request: ParsedRepairActionRequest,
): Result<
  {
    workflowInstance: WorkflowInstanceRecord;
    ledgerEvent: LedgerEventRecord;
  },
  AppError
> {
  const updatedWorkflowResult = repositories.workflows.updateInstance({
    ...workflowInstance,
    state: WORKFLOW_STATES.CANCELED,
    status: WORKFLOW_STATUSES.CANCELED,
    canceledAt: nowIso(),
    currentInteraction: terminalInteraction(WORKFLOW_STATUSES.CANCELED),
    version: workflowInstance.version + 1,
  });
  if (!updatedWorkflowResult.ok) {
    return updatedWorkflowResult;
  }

  const changeRequestUpdateResult = maybeCancelChangeRequest(
    repositories,
    requestContext.actor.actorId,
    workflowInstance,
  );
  if (!changeRequestUpdateResult.ok) {
    return changeRequestUpdateResult;
  }

  const ledgerResult = appendAdminRepairLedgerEvent(
    repositories,
    requestContext,
    updatedWorkflowResult.value,
    LEDGER_EVENT_TYPES.WORKFLOW_ADMIN_WORKFLOW_CANCELED,
    request,
    { previousState: workflowInstance.state },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok({
    workflowInstance: updatedWorkflowResult.value,
    ledgerEvent: ledgerResult.value,
  });
}

function reopenRepair(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  request: ParsedRepairActionRequest,
): Result<
  {
    workflowInstance: WorkflowInstanceRecord;
    ledgerEvent: LedgerEventRecord;
  },
  AppError
> {
  const updatedWorkflowResult = repositories.workflows.updateInstance({
    ...workflowInstance,
    state: WORKFLOW_STATES.WAITING_REPAIR,
    status: WORKFLOW_STATUSES.WAITING_REPAIR,
    currentInteraction: repairInteraction(workflowInstance),
    version: workflowInstance.version + 1,
  });
  if (!updatedWorkflowResult.ok) {
    return updatedWorkflowResult;
  }

  const ledgerResult = appendAdminRepairLedgerEvent(
    repositories,
    requestContext,
    updatedWorkflowResult.value,
    LEDGER_EVENT_TYPES.WORKFLOW_ADMIN_REPAIR_REOPENED,
    request,
    { previousState: workflowInstance.state },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok({
    workflowInstance: updatedWorkflowResult.value,
    ledgerEvent: ledgerResult.value,
  });
}

function appendAdminRepairLedgerEvent(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  eventType: string,
  request: ParsedRepairActionRequest,
  payload: Record<string, unknown>,
): Result<LedgerEventRecord, AppError> {
  return appendWorkflowLedgerEvent(repositories, requestContext, {
    eventType,
    workflowInstance,
    subjectType: "workflow_instance",
    subjectId: workflowInstance.workflowInstanceId,
    idempotencyKey: request.idempotencyKey,
    payload: {
      action: request.action,
      expectedVersion: request.expectedVersion,
      ...payload,
    },
  });
}

function resetFailedIntegrationOutboxRows(
  repositories: Repositories,
  workflowInstance: WorkflowInstanceRecord,
): string[] {
  if (workflowInstance.changeRequestId === undefined) {
    return [];
  }

  const updatedOutboxIds: string[] = [];

  for (const outboxRow of repositories.store.integrationOutbox.values()) {
    if (
      outboxRow.changeRequestId !== workflowInstance.changeRequestId ||
      (outboxRow.status !== INTEGRATION_OUTBOX_STATUSES.FAILED &&
        outboxRow.status !== INTEGRATION_OUTBOX_STATUSES.DEAD_LETTER)
    ) {
      continue;
    }

    repositories.store.integrationOutbox.set(outboxRow.outboxId, {
      ...outboxRow,
      status: INTEGRATION_OUTBOX_STATUSES.PENDING,
      updatedAt: nowIso(),
    });
    updatedOutboxIds.push(outboxRow.outboxId);
  }

  return updatedOutboxIds;
}

function maybeCancelChangeRequest(
  repositories: Repositories,
  actorId: string,
  workflowInstance: WorkflowInstanceRecord,
): Result<true, AppError> {
  if (workflowInstance.changeRequestId === undefined) {
    return ok(true);
  }

  const changeRequestResult = repositories.changeRequests.findById(
    workflowInstance.changeRequestId,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const updateResult = repositories.changeRequests.update({
    ...changeRequestResult.value,
    status: CHANGE_REQUEST_STATUSES.CANCELED,
    closedAt: nowIso(),
    updatedBy: actorId,
    version: changeRequestResult.value.version + 1,
  });
  if (!updateResult.ok) {
    return updateResult;
  }

  return ok(true);
}

function repairInteraction(
  workflowInstance: WorkflowInstanceRecord,
): Record<string, unknown> {
  if (workflowInstance.currentInteraction["type"] === "repair") {
    return workflowInstance.currentInteraction;
  }

  return {
    type: "repair",
    title: "Workflow repair required",
    jsonSchema: {
      type: "object",
      required: ["repairAction"],
      properties: {
        repairAction: {
          type: "string",
        },
        notes: {
          type: "string",
        },
      },
    },
  };
}

function currentRepairOptions(
  repositories: Repositories,
  workflowInstance: WorkflowInstanceRecord,
): RepairOptionDebugInfo[] {
  const workflowConfigResult = resolvePinnedWorkflowConfig(
    repositories,
    workflowInstance,
  );
  const currentNode = workflowConfigResult.ok
    ? currentGraphNode(workflowConfigResult.value, workflowInstance)
    : undefined;

  return buildRepairOptions({
    workflowInstance,
    ...(currentNode !== undefined ? { currentNode } : {}),
    hasExternalFailure: hasExternalFailure(repositories, workflowInstance),
  });
}

function currentGraphNode(
  workflowConfig: { graph?: { nodes: WorkflowGraphNodeConfig[] } },
  workflowInstance: WorkflowInstanceRecord,
): WorkflowGraphNodeConfig | undefined {
  const activeNodeId = stringField(workflowInstance.context, "activeNodeId");
  const nodes = workflowConfig.graph?.nodes ?? [];

  if (activeNodeId !== undefined) {
    const activeNode = nodes.find((node) => node.nodeId === activeNodeId);

    if (activeNode !== undefined) {
      return activeNode;
    }
  }

  return nodes.find((node) => node.state === workflowInstance.state);
}

function hasExternalFailure(
  repositories: Repositories,
  workflowInstance: WorkflowInstanceRecord,
): boolean {
  return repositories.store.ledgerEvents.some((event) => {
    return (
      event.tenantId === workflowInstance.tenantId &&
      event.workflowInstanceId === workflowInstance.workflowInstanceId &&
      event.eventType === LEDGER_EVENT_TYPES.EXTERNAL_WRITE_FAILED
    );
  });
}

function repairActionResponse(input: {
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  action: WorkflowAdminRepairAction;
  accepted: boolean;
  idempotentReplay?: boolean;
  ledgerEvents: LedgerEventRecord[];
  repairOptions: RepairOptionDebugInfo[];
}): RepairActionResponse {
  return {
    correlationId: input.requestContext.correlationId,
    generatedAt: new Date().toISOString(),
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    action: input.action,
    accepted: input.accepted,
    ...(input.idempotentReplay !== undefined
      ? { idempotentReplay: input.idempotentReplay }
      : {}),
    workflowVersion: input.workflowInstance.version,
    ledgerEventIds: input.ledgerEvents.map((event) => event.eventId),
    repairOptions: input.repairOptions,
  };
}
