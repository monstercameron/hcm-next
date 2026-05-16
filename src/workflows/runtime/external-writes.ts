import {
  CHANGE_REQUEST_STATUSES,
  LEDGER_EVENT_TYPES,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  ChangeRequestRecord,
  Repositories,
  TransactionPlanRecord,
  WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import { objectField, stringField } from "../shared/json-fields.js";
import type {
  ApiRequestContext,
  AppDependencies,
} from "../shared/runtime-dependencies.js";
import { workflowConditionMatches } from "../shared/workflow-conditions.js";
import {
  buildConfiguredInteraction,
  type WorkflowConfig,
  type WorkflowGraphNodeConfig,
  type WorkflowGraphOutcomeConfig,
} from "../shared/workflow-config.js";
import {
  appendAdditionalWorkflowEvents,
  appendTransitionLedgerEvents,
} from "../shared/workflow-ledger-events.js";
import type { ExternalWriteExecution, ExternalWriteExecutionResult } from "./types.js";
import { advanceWorkflowGraph, contextWithGraphAdvance } from "./graph-runtime.js";

/**
 * Executes configured synchronous external writes that can route the workflow.
 */
export async function executeSynchronousExternalWrites(
  dependencies: AppDependencies,
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
  idempotencyKey: string,
  changeRequest: ChangeRequestRecord,
  transactionPlan: TransactionPlanRecord,
): Promise<Result<ExternalWriteExecutionResult, AppError>> {
  const externalWriteExecutions: ExternalWriteExecution[] = [];

  for (const externalWrite of transactionPlan.externalWrites) {
    const connectionId = stringField(externalWrite, "connectionId");
    const operation = stringField(externalWrite, "operation") ?? "unknown";
    const externalClient =
      connectionId === undefined
        ? undefined
        : dependencies.externalWriteClients?.[connectionId];

    if (connectionId === undefined || externalClient === undefined) {
      continue;
    }

    const requestPayload = objectField(externalWrite, "payload");
    const graphNodeResult = findExternalWriteGraphNode(workflowConfig, externalWrite);
    const externalIdempotencyKey =
      stringField(externalWrite, "idempotencyKey") ??
      `${connectionId}_${transactionPlan.transactionPlanId}`;

    if (!graphNodeResult.ok) {
      return graphNodeResult;
    }

    if (requestPayload === undefined) {
      return err(
        validationFailedError({
          connectionId,
          operation,
          requestPayload: "missing",
        }),
      );
    }

    dependencies.logger?.info("external write started", {
      connectionId,
      operation,
      idempotencyKey: externalIdempotencyKey,
      workflowInstanceId: workflowInstance.workflowInstanceId,
    });

    const externalWriteResult = await externalClient.submit(
      requestPayload,
      externalIdempotencyKey,
      dependencies.logger,
    );
    if (!externalWriteResult.ok) {
      dependencies.logger?.error("external write failed", {
        connectionId,
        operation,
        idempotencyKey: externalIdempotencyKey,
        errorCode: externalWriteResult.error.code,
      });
      const failureLedgerResult = appendExternalWriteFailedEvent(
        repositories,
        requestContext,
        workflowInstance,
        idempotencyKey,
        transactionPlan.transactionPlanId,
        {
          connectionId,
          operation,
          error: externalWriteResult.error.details ?? {},
        },
      );
      if (!failureLedgerResult.ok) {
        return failureLedgerResult;
      }

      return externalWriteResult;
    }

    dependencies.logger?.info("external write succeeded", {
      connectionId,
      operation,
      idempotencyKey: externalIdempotencyKey,
    });

    const outcomeResult = selectExternalWriteOutcome(
      graphNodeResult.value,
      externalWriteResult.value.rawResponse,
    );
    if (!outcomeResult.ok) {
      return outcomeResult;
    }

    if (isTerminalExternalFailureOutcome(outcomeResult.value)) {
      const routedWorkflowResult = routeExternalWriteOutcome(
        repositories,
        requestContext,
        workflowConfig,
        workflowInstance,
        changeRequest,
        transactionPlan,
        idempotencyKey,
        outcomeResult.value,
        {
          connectionId,
          operation,
          response: externalWriteResult.value.rawResponse,
          reasonCodes: externalWriteResult.value.reasonCodes ?? [],
          outcome: outcomeResult.value.outcome,
          nodeId: graphNodeResult.value.nodeId,
        },
      );
      if (!routedWorkflowResult.ok) {
        return routedWorkflowResult;
      }

      return ok({
        status: "routed",
        workflowInstance: routedWorkflowResult.value,
      });
    }

    externalWriteExecutions.push({
      connectionId,
      operation,
      idempotencyKey: externalIdempotencyKey,
      outcome: outcomeResult.value.outcome,
      ...(outcomeResult.value.eventType !== undefined
        ? { eventType: outcomeResult.value.eventType }
        : {}),
      ...(outcomeResult.value.nextNodeId !== undefined
        ? { nextNodeId: outcomeResult.value.nextNodeId }
        : {}),
      requestPayload,
      responsePayload: externalWriteResult.value.rawResponse,
    });
  }

  return ok({
    status: "succeeded",
    executions: externalWriteExecutions,
  });
}

function findExternalWriteGraphNode(
  workflowConfig: WorkflowConfig,
  externalWrite: Record<string, unknown>,
): Result<WorkflowGraphNodeConfig, AppError> {
  const connectionId = stringField(externalWrite, "connectionId");
  const operation = stringField(externalWrite, "operation");
  const graphNode = workflowConfig.graph?.nodes.find((node) => {
    return (
      node.type === "external_write" &&
      node.connectionId === connectionId &&
      node.operation === operation
    );
  });

  if (graphNode === undefined) {
    return err(
      validationFailedError({
        intent: workflowConfig.intent,
        connectionId,
        operation,
        graphNode: "missing",
      }),
    );
  }

  return ok(graphNode);
}

function selectExternalWriteOutcome(
  graphNode: WorkflowGraphNodeConfig,
  externalWriteResponse: Record<string, unknown>,
): Result<WorkflowGraphOutcomeConfig, AppError> {
  const outcomes = graphNode.outcomes ?? [];
  const matchingOutcome = outcomes.find((outcome) => {
    return (
      outcome.when !== undefined &&
      workflowConditionMatches(outcome.when, { externalWriteResponse })
    );
  });

  if (matchingOutcome === undefined) {
    return err(
      validationFailedError({
        nodeId: graphNode.nodeId,
        response: externalWriteResponse,
        outcomes: outcomes.map((outcome) => outcome.outcome),
      }),
    );
  }

  return ok(matchingOutcome);
}

function isTerminalExternalFailureOutcome(
  outcome: WorkflowGraphOutcomeConfig,
): boolean {
  return (
    outcome.nextState === WORKFLOW_STATES.WAITING_REPAIR ||
    outcome.nextStatus === WORKFLOW_STATUSES.WAITING_REPAIR ||
    outcome.eventType === LEDGER_EVENT_TYPES.EXTERNAL_WRITE_FAILED
  );
}

function routeExternalWriteOutcome(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
  changeRequest: ChangeRequestRecord,
  transactionPlan: TransactionPlanRecord,
  idempotencyKey: string,
  outcome: WorkflowGraphOutcomeConfig,
  payload: Record<string, unknown>,
): Result<WorkflowInstanceRecord, AppError> {
  const graphAdvance = advanceWorkflowGraph({
    workflowConfig,
    workflowContext: workflowInstance.context,
    firstRouteKey: outcome.routeKey ?? outcome.outcome,
    sources: {
      workflow: workflowInstance,
      changeRequest,
      transactionPlan,
      externalWriteResponse: objectField(payload, "response") ?? {},
    },
  });
  const nextInteractionResult =
    (graphAdvance.nextInteraction ?? outcome.nextInteraction) === undefined
      ? ok(workflowInstance.currentInteraction)
      : buildConfiguredInteraction({
          workflowConfig,
          interactionKey: graphAdvance.nextInteraction ?? outcome.nextInteraction ?? "",
        });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const changeRequestResult = repositories.changeRequests.update({
    ...changeRequest,
    status: CHANGE_REQUEST_STATUSES.WAITING_REPAIR,
    updatedBy: requestContext.actor.actorId,
    version: changeRequest.version + 1,
  });
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const transactionPlanResult = repositories.transactionPlans.update({
    ...transactionPlan,
    status: CHANGE_REQUEST_STATUSES.WAITING_REPAIR,
    executionResult: {
      externalWriteOutcome: outcome,
      externalWritePayload: payload,
    },
    updatedBy: requestContext.actor.actorId,
  });
  if (!transactionPlanResult.ok) {
    return transactionPlanResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...workflowInstance,
    state:
      graphAdvance.nextState ?? outcome.nextState ?? WORKFLOW_STATES.WAITING_REPAIR,
    status:
      graphAdvance.nextStatus ?? outcome.nextStatus ?? WORKFLOW_STATUSES.WAITING_REPAIR,
    currentInteraction: nextInteractionResult.value,
    context: contextWithGraphAdvance({
      context: workflowInstance.context,
      advance: graphAdvance,
    }),
    version: workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: workflowInstance.state,
    eventType: outcome.eventType ?? LEDGER_EVENT_TYPES.EXTERNAL_WRITE_FAILED,
    idempotencyKey,
    transactionPlanId: transactionPlan.transactionPlanId,
    payload: {
      ...payload,
      changeRequest: changeRequestResult.value,
      transactionPlan: transactionPlanResult.value,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return workflowResult;
}

function appendExternalWriteFailedEvent(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  idempotencyKey: string,
  transactionPlanId: string,
  payload: Record<string, unknown>,
): Result<true, AppError> {
  return appendAdditionalWorkflowEvents(
    repositories,
    requestContext,
    workflowInstance,
    idempotencyKey,
    [
      {
        eventType: LEDGER_EVENT_TYPES.EXTERNAL_WRITE_FAILED,
        transactionPlanId,
        payload,
      },
    ],
  );
}
