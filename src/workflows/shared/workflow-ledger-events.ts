import {
  ACTOR_TYPES,
  LEDGER_EVENT_TYPES,
  ok,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import {
  nowIso,
  type ActorRecord,
  type Repositories,
  type WorkflowInstanceRecord,
} from "@human-capital-management-suite/data-store";

type WorkflowLedgerRequestContext = {
  tenantId: string;
  correlationId: string;
  actor: ActorRecord;
};

export type WorkflowLedgerEventInput = {
  eventType: string;
  workflowInstance: WorkflowInstanceRecord;
  subjectType: string;
  subjectId: string;
  idempotencyKey?: string;
  approvalTaskId?: string;
  transactionPlanId?: string;
  payload: Record<string, unknown>;
};

export type AdditionalWorkflowLedgerEvent = {
  eventType: string;
  approvalTaskId?: string;
  transactionPlanId?: string;
  payload: Record<string, unknown>;
};

/**
 * Appends the standard transition, business, and state-change ledger events.
 */
export function appendTransitionLedgerEvents(
  repositories: Repositories,
  requestContext: WorkflowLedgerRequestContext,
  input: {
    workflowInstance: WorkflowInstanceRecord;
    previousState: string;
    eventType: string;
    idempotencyKey: string;
    approvalTaskId?: string;
    transactionPlanId?: string;
    payload: Record<string, unknown>;
  },
): Result<true, AppError> {
  const transitionLedgerResult = appendWorkflowLedgerEvent(
    repositories,
    requestContext,
    {
      eventType: LEDGER_EVENT_TYPES.WORKFLOW_TRANSITION_SUBMITTED,
      workflowInstance: input.workflowInstance,
      subjectType: input.workflowInstance.subjectType,
      subjectId: input.workflowInstance.subjectId,
      idempotencyKey: input.idempotencyKey,
      payload: {
        previousState: input.previousState,
        currentState: input.workflowInstance.state,
      },
    },
  );
  if (!transitionLedgerResult.ok) {
    return transitionLedgerResult;
  }

  const businessLedgerResult = appendWorkflowLedgerEvent(repositories, requestContext, {
    eventType: input.eventType,
    workflowInstance: input.workflowInstance,
    subjectType: input.workflowInstance.subjectType,
    subjectId: input.workflowInstance.subjectId,
    idempotencyKey: input.idempotencyKey,
    ...(input.approvalTaskId !== undefined
      ? { approvalTaskId: input.approvalTaskId }
      : {}),
    ...(input.transactionPlanId !== undefined
      ? { transactionPlanId: input.transactionPlanId }
      : {}),
    payload: input.payload,
  });
  if (!businessLedgerResult.ok) {
    return businessLedgerResult;
  }

  const stateLedgerResult = appendWorkflowLedgerEvent(repositories, requestContext, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_STATE_CHANGED,
    workflowInstance: input.workflowInstance,
    subjectType: input.workflowInstance.subjectType,
    subjectId: input.workflowInstance.subjectId,
    idempotencyKey: input.idempotencyKey,
    payload: {
      previousState: input.previousState,
      currentState: input.workflowInstance.state,
    },
  });
  if (!stateLedgerResult.ok) {
    return stateLedgerResult;
  }

  return ok(true);
}

/**
 * Appends supplemental workflow ledger events with shared workflow context.
 */
export function appendAdditionalWorkflowEvents(
  repositories: Repositories,
  requestContext: WorkflowLedgerRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  idempotencyKey: string,
  events: AdditionalWorkflowLedgerEvent[],
): Result<true, AppError> {
  for (const event of events) {
    const eventResult = appendWorkflowLedgerEvent(repositories, requestContext, {
      eventType: event.eventType,
      workflowInstance,
      subjectType: workflowInstance.subjectType,
      subjectId: workflowInstance.subjectId,
      idempotencyKey,
      ...(event.approvalTaskId !== undefined
        ? { approvalTaskId: event.approvalTaskId }
        : {}),
      ...(event.transactionPlanId !== undefined
        ? { transactionPlanId: event.transactionPlanId }
        : {}),
      payload: event.payload,
    });

    if (!eventResult.ok) {
      return eventResult;
    }
  }

  return ok(true);
}

/**
 * Appends a single workflow-scoped ledger event.
 */
export function appendWorkflowLedgerEvent(
  repositories: Repositories,
  requestContext: WorkflowLedgerRequestContext,
  input: WorkflowLedgerEventInput,
) {
  return repositories.ledger.append({
    tenantId: requestContext.tenantId,
    eventType: input.eventType,
    subjectType: input.subjectType,
    subjectId: input.subjectId,
    occurredAt: nowIso(),
    actorType: requestContext.actor.actorType || ACTOR_TYPES.HUMAN,
    actorId: requestContext.actor.actorId,
    relationshipContext: {
      linkedWorkerId: requestContext.actor.linkedWorkerId ?? null,
    },
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    workflowDefinitionId: input.workflowInstance.workflowDefinitionId,
    workflowVersionId: input.workflowInstance.workflowVersionId,
    ...(input.workflowInstance.changeRequestId !== undefined
      ? { changeRequestId: input.workflowInstance.changeRequestId }
      : {}),
    ...(input.transactionPlanId !== undefined
      ? { transactionPlanId: input.transactionPlanId }
      : {}),
    ...(input.approvalTaskId !== undefined
      ? { approvalTaskId: input.approvalTaskId }
      : {}),
    correlationId: requestContext.correlationId,
    ...(input.idempotencyKey !== undefined
      ? { idempotencyKey: input.idempotencyKey }
      : {}),
    permissionSnapshot: createWorkflowPermissionSnapshot(requestContext.actor),
    aiVisibilitySnapshot: {},
    payload: input.payload,
    ...(requestContext.actor.roles[0] !== undefined
      ? { actorRole: requestContext.actor.roles[0] }
      : {}),
  });
}

function createWorkflowPermissionSnapshot(actor: ActorRecord) {
  return {
    actorId: actor.actorId,
    roles: actor.roles,
    linkedWorkerId: actor.linkedWorkerId,
  };
}
