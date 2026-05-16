import {
  INTEGRATION_OUTBOX_STATUSES,
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  makeId,
  nowIso,
  type ChangeRequestRecord,
  type EmployeeProjectionDocument,
  type ProposedChangeRecord,
  type Repositories,
  type RoleBindingRecord,
  type TransactionPlanRecord,
  type WorkerAssignmentRecord,
  type WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import { numberField, objectField, stringField } from "../shared/json-fields.js";
import { applyProjectionPatches } from "../shared/projection-patches.js";
import {
  resolveWorkflowTemplate,
  type WorkflowConfig,
} from "../shared/workflow-config.js";
import { createPermissionSnapshot } from "./permissions.js";
import type { ExternalWriteExecution, PlanTransactionOutput } from "./types.js";

export type AppliedTransactionWrites = {
  projectionVersion?: number;
  ledgerEventCount: number;
};

/**
 * Runs the configured Go planning block and returns deterministic plan output.
 */
export async function planApprovedChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    workflowInstance: WorkflowInstanceRecord;
    changeRequest: ChangeRequestRecord;
    proposedChanges: ProposedChangeRecord[];
    employeeDocument?: EmployeeProjectionDocument;
    idempotencyKey: string;
  },
): Promise<Result<PlanTransactionOutput, AppError>> {
  const proposedChange = input.proposedChanges[0];
  const planInputResult = resolveWorkflowTemplate(input.workflowConfig.plan.input, {
    ...(input.employeeDocument !== undefined
      ? { employee: input.employeeDocument }
      : {}),
    workflow: input.workflowInstance,
    changeRequest: input.changeRequest,
    ...(proposedChange !== undefined ? { proposedChange } : {}),
  });
  if (!planInputResult.ok) {
    return planInputResult;
  }

  const planResult =
    await dependencies.executorClient.executeBlock<PlanTransactionOutput>(
      {
        tenantId: requestContext.tenantId,
        environmentId: requestContext.environmentId,
        changeRequestId: input.changeRequest.changeRequestId,
        workflowInstanceId: input.workflowInstance.workflowInstanceId,
        workflowVersionId: input.workflowInstance.workflowVersionId,
        block: input.workflowConfig.plan.block,
        input: planInputResult.value as Record<string, unknown>,
        context: {
          actorId: requestContext.actor.actorId,
          effectiveAt: input.changeRequest.effectiveAt,
          permissions: createPermissionSnapshot(requestContext.actor),
          correlationId: requestContext.correlationId,
          idempotencyKey: input.idempotencyKey,
        },
      },
      dependencies.logger,
    );
  if (!planResult.ok) {
    return planResult;
  }
  if (planResult.value.output === undefined) {
    return err(validationFailedError({ planOutput: "missing" }));
  }

  return ok(planResult.value.output);
}

/**
 * Applies configured ledger writes and projection patches produced by a plan.
 */
export function applyInternalTransactionWrites(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
  changeRequest: ChangeRequestRecord,
  transactionPlan: TransactionPlanRecord,
): Result<AppliedTransactionWrites, AppError> {
  let ledgerEventCount = 0;
  let projectionVersion: number | undefined;
  let lastProjectionEventId: string | undefined;
  let lastProjectionEventSequence: number | undefined;

  for (const internalWriteRecord of transactionPlan.internalWrites) {
    const internalWriteResult = parseInternalLedgerWrite(internalWriteRecord);
    if (!internalWriteResult.ok) {
      return internalWriteResult;
    }

    const internalWrite = internalWriteResult.value;
    const ledgerEventResult = repositories.ledger.append({
      tenantId: requestContext.tenantId,
      eventType: internalWrite.eventType,
      subjectType: internalWrite.subjectType,
      subjectId: internalWrite.subjectId,
      occurredAt: nowIso(),
      effectiveAt: internalWrite.effectiveAt,
      actorType: requestContext.actor.actorType,
      actorId: requestContext.actor.actorId,
      relationshipContext: {},
      workflowInstanceId: workflowInstance.workflowInstanceId,
      workflowDefinitionId: workflowInstance.workflowDefinitionId,
      workflowVersionId: workflowInstance.workflowVersionId,
      changeRequestId: changeRequest.changeRequestId,
      transactionPlanId: transactionPlan.transactionPlanId,
      correlationId: requestContext.correlationId,
      permissionSnapshot: createPermissionSnapshot(requestContext.actor),
      aiVisibilitySnapshot: {},
      payload: internalWrite.payload,
    });
    if (!ledgerEventResult.ok) {
      return ledgerEventResult;
    }

    ledgerEventCount += 1;
    lastProjectionEventId = ledgerEventResult.value.eventId;
    lastProjectionEventSequence = ledgerEventResult.value.eventSequence;
  }

  if (
    workflowConfig.subjectType === "worker" &&
    transactionPlan.projectionPatches.length > 0
  ) {
    const projectionResult = repositories.employeeProjections.findByEmployeeId(
      requestContext.tenantId,
      changeRequest.targetWorkerId,
    );
    if (!projectionResult.ok) {
      return projectionResult;
    }

    const updatedDocumentResult = applyProjectionPatches({
      document: projectionResult.value.document,
      patches: transactionPlan.projectionPatches,
      allowedPatchPaths: workflowConfig.projection.allowedPatchPaths,
    });
    if (!updatedDocumentResult.ok) {
      return updatedDocumentResult;
    }

    const sourceEventId = lastProjectionEventId ?? makeId("evt_missing");
    const sourceEventSequence = lastProjectionEventSequence ?? 0;
    const updateProjectionResult = repositories.employeeProjections.updateDocument(
      requestContext.tenantId,
      changeRequest.targetWorkerId,
      updatedDocumentResult.value,
      sourceEventId,
      sourceEventSequence,
    );
    if (!updateProjectionResult.ok) {
      return updateProjectionResult;
    }

    projectionVersion = updateProjectionResult.value.projectionVersion;
  }

  const assignmentOperationsResult = applyWorkerAssignmentOperations(
    repositories,
    requestContext,
    workflowInstance,
    changeRequest,
    transactionPlan.assignmentOperations ?? [],
  );
  if (!assignmentOperationsResult.ok) {
    return assignmentOperationsResult;
  }

  const roleBindingOperationsResult = applyRoleBindingOperations(
    repositories,
    requestContext,
    workflowInstance,
    transactionPlan.roleBindingOperations ?? [],
  );
  if (!roleBindingOperationsResult.ok) {
    return roleBindingOperationsResult;
  }

  return ok({
    ...(projectionVersion !== undefined ? { projectionVersion } : {}),
    ledgerEventCount,
  });
}

/**
 * Persists integration outbox rows for configured external writes.
 */
export function createIntegrationOutboxRows(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  changeRequest: ChangeRequestRecord,
  transactionPlan: TransactionPlanRecord,
  externalWriteExecutions: ExternalWriteExecution[],
) {
  const outboxRows = [];

  for (const externalWrite of transactionPlan.externalWrites) {
    const idempotencyKey =
      stringField(externalWrite, "idempotencyKey") ??
      `outbox_${changeRequest.changeRequestId}`;
    const matchingExecution = externalWriteExecutions.find((execution) => {
      return execution.idempotencyKey === idempotencyKey;
    });
    const outboxResult = repositories.integrationOutbox.create({
      outboxId: makeId("outbox"),
      tenantId: requestContext.tenantId,
      changeRequestId: changeRequest.changeRequestId,
      transactionPlanId: transactionPlan.transactionPlanId,
      destination: stringField(externalWrite, "connectionId") ?? "unknown",
      operation: stringField(externalWrite, "operation") ?? "unknown",
      requestPayload: objectField(externalWrite, "payload") ?? {},
      ...(matchingExecution !== undefined
        ? { responsePayload: matchingExecution.responsePayload }
        : {}),
      status:
        matchingExecution !== undefined
          ? INTEGRATION_OUTBOX_STATUSES.SUCCEEDED
          : INTEGRATION_OUTBOX_STATUSES.PENDING,
      attemptCount: matchingExecution !== undefined ? 1 : 0,
      maxAttempts: 3,
      idempotencyKey,
      createdAt: nowIso(),
      updatedAt: nowIso(),
    });

    if (!outboxResult.ok) {
      return outboxResult;
    }

    outboxRows.push(outboxResult.value);
  }

  return ok(outboxRows);
}

function parseInternalLedgerWrite(internalWrite: Record<string, unknown>): Result<
  {
    eventType: string;
    subjectType: string;
    subjectId: string;
    effectiveAt: string;
    payload: Record<string, unknown>;
  },
  AppError
> {
  const eventType = stringField(internalWrite, "eventType");
  const subjectType = stringField(internalWrite, "subjectType");
  const subjectId = stringField(internalWrite, "subjectId");
  const effectiveAt = stringField(internalWrite, "effectiveAt");
  const payload = objectField(internalWrite, "payload");

  if (
    eventType === undefined ||
    subjectType === undefined ||
    subjectId === undefined ||
    effectiveAt === undefined ||
    payload === undefined
  ) {
    return err(
      validationFailedError({
        eventType,
        subjectType,
        subjectId,
        effectiveAt,
        payload,
      }),
    );
  }

  return ok({
    eventType,
    subjectType,
    subjectId,
    effectiveAt,
    payload,
  });
}

function applyWorkerAssignmentOperations(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  changeRequest: ChangeRequestRecord,
  assignmentOperations: Record<string, unknown>[],
): Result<true, AppError> {
  for (const assignmentOperation of assignmentOperations) {
    const operation = stringField(assignmentOperation, "operation");

    if (operation === "supersede") {
      const supersedeResult = supersedeWorkerAssignment(
        repositories,
        workflowInstance,
        changeRequest,
        assignmentOperation,
      );
      if (!supersedeResult.ok) {
        return supersedeResult;
      }
      continue;
    }

    if (operation === "create") {
      const createResult = createWorkerAssignmentFromOperation(
        repositories,
        requestContext,
        workflowInstance,
        changeRequest,
        assignmentOperation,
      );
      if (!createResult.ok) {
        return createResult;
      }
    }
  }

  return ok(true);
}

function supersedeWorkerAssignment(
  repositories: Repositories,
  workflowInstance: WorkflowInstanceRecord,
  changeRequest: ChangeRequestRecord,
  assignmentOperation: Record<string, unknown>,
): Result<true, AppError> {
  const currentWorkerAssignmentId = stringField(
    assignmentOperation,
    "currentWorkerAssignmentId",
  );
  const assignmentType = stringField(assignmentOperation, "assignmentType");
  const effectiveEnd = stringField(assignmentOperation, "effectiveEnd");
  const activeAssignmentResult =
    currentWorkerAssignmentId === undefined
      ? findActiveAssignmentForOperation(repositories, changeRequest, assignmentType)
      : repositories.workerAssignments.findById(currentWorkerAssignmentId);
  if (!activeAssignmentResult.ok) {
    return activeAssignmentResult;
  }

  const updateResult = repositories.workerAssignments.update({
    ...activeAssignmentResult.value,
    status: "superseded",
    sourceWorkflowInstanceId: workflowInstance.workflowInstanceId,
    ...(effectiveEnd !== undefined ? { effectiveEnd } : {}),
  });
  if (!updateResult.ok) {
    return updateResult;
  }

  return ok(true);
}

function createWorkerAssignmentFromOperation(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  changeRequest: ChangeRequestRecord,
  assignmentOperation: Record<string, unknown>,
): Result<true, AppError> {
  const assignmentType = stringField(assignmentOperation, "assignmentType");
  const orgUnitId = stringField(assignmentOperation, "orgUnitId");
  const allocationPercent = numberField(assignmentOperation, "allocationPercent");
  const effectiveStart = stringField(assignmentOperation, "effectiveStart");

  if (
    assignmentType === undefined ||
    orgUnitId === undefined ||
    allocationPercent === undefined ||
    effectiveStart === undefined
  ) {
    return err(
      validationFailedError({
        assignmentType,
        orgUnitId,
        allocationPercent,
        effectiveStart,
      }),
    );
  }

  const workerAssignment: WorkerAssignmentRecord = {
    workerAssignmentId: makeId("wasg"),
    tenantId: requestContext.tenantId,
    employeeId: changeRequest.targetWorkerId,
    orgUnitId,
    assignmentType: assignmentType as WorkerAssignmentRecord["assignmentType"],
    ...(stringField(assignmentOperation, "roleType") !== undefined
      ? { roleType: stringField(assignmentOperation, "roleType") }
      : {}),
    ...(stringField(assignmentOperation, "managerEmployeeId") !== undefined
      ? { managerEmployeeId: stringField(assignmentOperation, "managerEmployeeId") }
      : {}),
    allocationPercent,
    status: "active",
    effectiveStart,
    sourceWorkflowInstanceId: workflowInstance.workflowInstanceId,
    metadata: {
      idempotencyKey: stringField(assignmentOperation, "idempotencyKey"),
    },
    createdAt: nowIso(),
    updatedAt: nowIso(),
  };
  const createResult = repositories.workerAssignments.create(workerAssignment);
  if (!createResult.ok) {
    return createResult;
  }

  return ok(true);
}

function applyRoleBindingOperations(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  roleBindingOperations: Record<string, unknown>[],
): Result<true, AppError> {
  for (const roleBindingOperation of roleBindingOperations) {
    const operation = stringField(roleBindingOperation, "operation");

    if (operation !== "ensure_direct_reports_binding") {
      continue;
    }

    const roleBindingResult = createRoleBindingFromOperation(
      repositories,
      requestContext,
      workflowInstance,
      roleBindingOperation,
    );
    if (!roleBindingResult.ok) {
      return roleBindingResult;
    }
  }

  return ok(true);
}

function createRoleBindingFromOperation(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  roleBindingOperation: Record<string, unknown>,
): Result<true, AppError> {
  const actorEmployeeId = stringField(roleBindingOperation, "actorEmployeeId");
  const roleKey = stringField(roleBindingOperation, "roleKey");
  const scopeType = stringField(roleBindingOperation, "scopeType");
  const effectiveStart = stringField(roleBindingOperation, "effectiveStart");

  if (
    actorEmployeeId === undefined ||
    roleKey === undefined ||
    scopeType === undefined ||
    effectiveStart === undefined
  ) {
    return err(
      validationFailedError({
        actorEmployeeId,
        roleKey,
        scopeType,
        effectiveStart,
      }),
    );
  }

  const actorResult = repositories.actors.findByLinkedWorkerId(
    requestContext.tenantId,
    actorEmployeeId,
  );
  if (!actorResult.ok) {
    return actorResult;
  }

  const roleBinding: RoleBindingRecord = {
    roleBindingId: makeId("rb"),
    tenantId: requestContext.tenantId,
    actorId: actorResult.value.actorId,
    roleKey,
    scopeType: scopeType as RoleBindingRecord["scopeType"],
    relationshipType: scopeType,
    status: "active",
    effectiveStart,
    sourceWorkflowInstanceId: workflowInstance.workflowInstanceId,
    metadata: {
      idempotencyKey: stringField(roleBindingOperation, "idempotencyKey"),
    },
    createdAt: nowIso(),
    updatedAt: nowIso(),
  };
  const createResult = repositories.roleBindings.create(roleBinding);
  if (!createResult.ok) {
    return createResult;
  }

  return ok(true);
}

function findActiveAssignmentForOperation(
  repositories: Repositories,
  changeRequest: ChangeRequestRecord,
  assignmentType?: string,
): Result<WorkerAssignmentRecord, AppError> {
  if (assignmentType === undefined) {
    return err(validationFailedError({ assignmentType }));
  }

  const activeAssignmentsResult =
    repositories.workerAssignments.findActiveForEmployeeByTypes(
      changeRequest.tenantId,
      changeRequest.targetWorkerId,
      [assignmentType],
    );
  if (!activeAssignmentsResult.ok) {
    return activeAssignmentsResult;
  }

  const activeAssignment = activeAssignmentsResult.value[0];
  if (activeAssignment === undefined) {
    return err(
      validationFailedError({
        assignmentType,
        activeAssignment: "missing",
      }),
    );
  }

  return ok(activeAssignment);
}
