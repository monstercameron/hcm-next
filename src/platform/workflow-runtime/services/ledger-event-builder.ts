import type { RequestContext } from "../context";
import type { PermissionSnapshot } from "../context";
import type { LedgerEventType } from "../constants";
import type { JsonObject, LedgerEventInput, WorkflowInstance } from "../domain";
import { nowIso } from "../runtime/time";

export type WorkflowLedgerEventInput = {
  context: RequestContext;
  workflowInstance: WorkflowInstance;
  permissionSnapshot: PermissionSnapshot;
  eventType: LedgerEventType;
  subjectType: string;
  subjectId: string;
  payload: JsonObject;
  effectiveAt?: string;
  changeRequestId?: string;
  transactionPlanId?: string;
  approvalTaskId?: string;
  idempotencyKey?: string;
};

/** Builds a tenant-scoped ledger event from workflow context. */
export function buildWorkflowLedgerEvent(
  input: WorkflowLedgerEventInput,
): LedgerEventInput {
  const ledgerEvent: LedgerEventInput = {
    tenantId: input.context.tenantId,
    eventType: input.eventType,
    eventVersion: 1,
    subjectType: input.subjectType,
    subjectId: input.subjectId,
    occurredAt: nowIso(),
    actorType: input.context.actorType,
    actorId: input.context.actorId,
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    workflowVersionId: input.workflowInstance.workflowVersionId,
    correlationId: input.context.correlationId,
    permissionSnapshot: input.permissionSnapshot,
    payload: input.payload,
  };

  if (input.effectiveAt !== undefined) {
    ledgerEvent.effectiveAt = input.effectiveAt;
  }
  if (input.context.roles[0] !== undefined) {
    ledgerEvent.actorRole = input.context.roles[0];
  }
  const changeRequestId =
    input.changeRequestId ?? input.workflowInstance.changeRequestId;
  if (changeRequestId !== undefined) {
    ledgerEvent.changeRequestId = changeRequestId;
  }
  if (input.transactionPlanId !== undefined) {
    ledgerEvent.transactionPlanId = input.transactionPlanId;
  }
  if (input.approvalTaskId !== undefined) {
    ledgerEvent.approvalTaskId = input.approvalTaskId;
  }
  if (input.idempotencyKey !== undefined) {
    ledgerEvent.idempotencyKey = input.idempotencyKey;
  }

  return ledgerEvent;
}
