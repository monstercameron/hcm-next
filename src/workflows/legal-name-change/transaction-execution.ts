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
  type TransactionPlanRecord,
  type WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import { createPermissionSnapshot } from "./permissions.js";
import type { ExternalWriteExecution, PlanTransactionOutput } from "./types.js";
import { objectField, stringField } from "../shared/json-fields.js";
import { applyProjectionPatches } from "../shared/projection-patches.js";
import {
  resolveWorkflowTemplate,
  type WorkflowConfig,
} from "../shared/workflow-config.js";

/**
 * Runs the configured Go planning block and returns its deterministic transaction plan output.
 */
export async function planApprovedChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    workflowInstance: WorkflowInstanceRecord;
    changeRequest: ChangeRequestRecord;
    proposedChanges: ProposedChangeRecord[];
    employeeDocument: EmployeeProjectionDocument;
    idempotencyKey: string;
  },
): Promise<Result<PlanTransactionOutput, AppError>> {
  const proposedChange = input.proposedChanges[0];
  if (proposedChange === undefined) {
    return err(validationFailedError({ proposedChange: "missing" }));
  }

  const planInputResult = resolveWorkflowTemplate(input.workflowConfig.plan.input, {
    employee: input.employeeDocument,
    workflow: input.workflowInstance,
    changeRequest: input.changeRequest,
    proposedChange,
  });
  if (!planInputResult.ok) {
    return planInputResult;
  }

  const planResult =
    await dependencies.executorClient.executeBlock<PlanTransactionOutput>({
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
    });
  if (!planResult.ok) {
    return planResult;
  }
  if (planResult.value.output === undefined) {
    return err(validationFailedError({ planOutput: "missing" }));
  }

  return ok(planResult.value.output);
}

/**
 * Converts block output into a persisted transaction plan envelope.
 */
export function createTransactionPlan(input: {
  tenantId: string;
  changeRequestId: string;
  actorId: string;
  output?: PlanTransactionOutput;
}): TransactionPlanRecord {
  const timestamp = nowIso();
  const internalWrites = input.output?.internalWrites ?? [];
  const externalWrites = input.output?.externalCallRequests ?? [];
  const projectionPatches = input.output?.projectionPatches ?? [];

  return {
    transactionPlanId: makeId("txnplan"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequestId,
    status: "simulated",
    planVersion: 1,
    steps: [
      {
        stepId: "update_internal_projection",
        kind: "ledger_projection_write",
      },
      {
        stepId: "enqueue_fake_hris_write",
        kind: "integration_outbox",
      },
    ],
    internalWrites,
    projectionPatches,
    externalWrites,
    rollbackPlan: {
      mode: "compensating_change",
      reason: "Employee data changes are corrected by a new approved event.",
    },
    compensationPlan: {
      mode: "manual_review_if_external_write_fails",
    },
    idempotencyKeys: {
      externalWrites: externalWrites.map((write) => write.idempotencyKey),
    },
    simulationResult: {
      valid: true,
      internalWriteCount: internalWrites.length,
      externalWriteCount: externalWrites.length,
    },
    executionResult: {},
    reconciliationResult: {},
    createdAt: timestamp,
    updatedAt: timestamp,
    createdBy: input.actorId,
    updatedBy: input.actorId,
  };
}

/**
 * Applies approved internal ledger writes to the employee projection.
 */
export function applyInternalTransactionWrites(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowConfig: WorkflowConfig,
  changeRequest: ChangeRequestRecord,
  transactionPlan: TransactionPlanRecord,
) {
  const projectionResult = repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    changeRequest.targetWorkerId,
  );
  if (!projectionResult.ok) {
    return projectionResult;
  }

  const internalWriteResult = parseInternalLedgerWrite(transactionPlan);
  if (!internalWriteResult.ok) {
    return internalWriteResult;
  }

  const updatedDocumentResult = applyProjectionPatches({
    document: projectionResult.value.document,
    patches: transactionPlan.projectionPatches,
    allowedPatchPaths: workflowConfig.projection.allowedPatchPaths,
  });
  if (!updatedDocumentResult.ok) {
    return updatedDocumentResult;
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

  return repositories.employeeProjections.updateDocument(
    requestContext.tenantId,
    changeRequest.targetWorkerId,
    updatedDocumentResult.value,
    ledgerEventResult.value.eventId,
    ledgerEventResult.value.eventSequence,
  );
}

/**
 * Persists outbox rows for external writes created by the planning block.
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

function parseInternalLedgerWrite(transactionPlan: TransactionPlanRecord): Result<
  {
    eventType: string;
    subjectType: string;
    subjectId: string;
    effectiveAt: string;
    payload: Record<string, unknown>;
  },
  AppError
> {
  const firstWrite = transactionPlan.internalWrites[0];

  if (firstWrite === undefined) {
    return err(validationFailedError({ internalWrites: "missing" }));
  }

  const eventType = stringField(firstWrite, "eventType");
  const subjectType = stringField(firstWrite, "subjectType");
  const subjectId = stringField(firstWrite, "subjectId");
  const effectiveAt = stringField(firstWrite, "effectiveAt");
  const payload = objectField(firstWrite, "payload");

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
