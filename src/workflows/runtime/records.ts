import {
  APPROVAL_TASK_STATUSES,
  CHANGE_REQUEST_STATUSES,
  CHANGE_REQUEST_TYPES,
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  makeId,
  nowIso,
  type ApprovalTaskRecord,
  type ChangeRequestRecord,
  type EmployeeProjectionDocument,
  type ProposedChangeRecord,
  type TransactionPlanRecord,
  type WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import type { ApiRequestContext } from "../../api/request-context.js";
import { stringField } from "../shared/json-fields.js";
import {
  renderWorkflowTemplateString,
  resolveWorkflowTemplate,
  type WorkflowApprovalGateApproverResolverConfig,
  type WorkflowConfig,
  type WorkflowTemplateSources,
} from "../shared/workflow-config.js";
import type { PlanTransactionOutput, PreflightOutput } from "./types.js";

export type ApprovalTaskCreateInput = {
  tenantId: string;
  workflowInstanceId: string;
  changeRequestId: string;
  assigneeActorId: string;
  assigneeRole: string;
  approvalType: string;
  approvalGroupId?: string;
  gateNodeId?: string;
  sequenceIndex?: number;
  weight?: number;
  isVetoHolder?: boolean;
  resolver?: WorkflowApprovalGateApproverResolverConfig;
  assignmentMode?: "actor" | "role";
};

/**
 * Builds a generic change request from workflow config templates.
 */
export function createConfiguredChangeRequest(input: {
  workflowConfig: WorkflowConfig;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  employeeDocument?: EmployeeProjectionDocument;
  transitionInput: Record<string, unknown>;
  preflight: PreflightOutput;
  effectiveAt: string;
  businessReason: string;
  timestamp: string;
}): Result<ChangeRequestRecord, AppError> {
  const templateSources = workflowTemplateSources(input);
  const currentSnapshotResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.currentSnapshot,
    templateSources,
  );
  if (!currentSnapshotResult.ok) {
    return currentSnapshotResult;
  }

  const proposedSnapshotResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.proposedSnapshot,
    templateSources,
  );
  if (!proposedSnapshotResult.ok) {
    return proposedSnapshotResult;
  }

  return ok({
    changeRequestId: makeId("chg"),
    tenantId: input.requestContext.tenantId,
    environmentId: input.requestContext.environmentId,
    workflowDefinitionId: input.workflowInstance.workflowDefinitionId,
    workflowVersionId: input.workflowInstance.workflowVersionId,
    changeType: configuredChangeRequestType(input.workflowConfig),
    targetWorkerId: input.workflowInstance.subjectId,
    requesterActorId: input.requestContext.actor.actorId,
    effectiveAt: input.effectiveAt,
    businessReason: input.businessReason,
    status: input.workflowConfig.submit.changeRequestStatus,
    priority: "normal",
    currentSnapshot: currentSnapshotResult.value as Record<string, unknown>,
    proposedSnapshot: proposedSnapshotResult.value as Record<string, unknown>,
    preflightResult: input.preflight as unknown as Record<string, unknown>,
    aiReview: {
      mode: "not_configured_v0",
      summary: `${input.workflowConfig.intent} uses deterministic runtime preflight.`,
    },
    ...(input.workflowConfig.submit.markSubmitted
      ? { submittedAt: input.timestamp }
      : {}),
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
    createdBy: input.requestContext.actor.actorId,
    updatedBy: input.requestContext.actor.actorId,
    version: 1,
    metadata: {
      workflowIntent: input.workflowConfig.intent,
      subjectType: input.workflowConfig.subjectType,
    },
  });
}

/**
 * Builds the proposed change row from config-defined source expressions.
 */
export function createConfiguredProposedChange(input: {
  workflowConfig: WorkflowConfig;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  employeeDocument?: EmployeeProjectionDocument;
  transitionInput: Record<string, unknown>;
  changeRequest: ChangeRequestRecord;
  preflight: PreflightOutput;
  effectiveAt: string;
  businessReason: string;
  timestamp: string;
}): Result<ProposedChangeRecord, AppError> {
  const templateSources = workflowTemplateSources(input);
  const targetObjectIdResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.proposedChange.targetObjectId,
    templateSources,
  );
  if (!targetObjectIdResult.ok) {
    return targetObjectIdResult;
  }

  const currentValueResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.proposedChange.currentValue,
    templateSources,
  );
  if (!currentValueResult.ok) {
    return currentValueResult;
  }

  const proposedValueResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.proposedChange.proposedValue,
    templateSources,
  );
  if (!proposedValueResult.ok) {
    return proposedValueResult;
  }

  const targetObjectId =
    typeof targetObjectIdResult.value === "string"
      ? targetObjectIdResult.value
      : undefined;
  const fieldPath = configuredProposedChangeFieldPath(
    input.workflowConfig,
    input.transitionInput,
  );

  if (targetObjectId === undefined || fieldPath === undefined) {
    return err(
      validationFailedError({
        targetObjectId: targetObjectIdResult.value,
        fieldPath,
      }),
    );
  }

  return ok({
    proposedChangeId: makeId("pchg"),
    tenantId: input.requestContext.tenantId,
    changeRequestId: input.changeRequest.changeRequestId,
    targetObjectType: input.workflowConfig.submit.proposedChange.targetObjectType,
    targetObjectId,
    fieldPath,
    currentValue: currentValueResult.value,
    proposedValue: proposedValueResult.value,
    effectiveAt: input.effectiveAt,
    reasonCode: input.businessReason,
    validationStatus: "valid",
    riskLevel: input.preflight.riskLevel,
    metadata: {
      workflowIntent: input.workflowConfig.intent,
    },
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
  });
}

/**
 * Creates a pending approval task from either legacy approval config or graph gate config.
 */
export function createConfiguredApprovalTask(
  input: ApprovalTaskCreateInput,
): ApprovalTaskRecord {
  return {
    approvalTaskId: makeId("appr"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequestId,
    workflowInstanceId: input.workflowInstanceId,
    ...(input.approvalGroupId !== undefined
      ? { approvalGroupId: input.approvalGroupId }
      : {}),
    ...(input.gateNodeId !== undefined ? { gateNodeId: input.gateNodeId } : {}),
    assigneeActorId: input.assigneeActorId,
    assigneeRole: input.assigneeRole,
    approvalType: input.approvalType,
    status: APPROVAL_TASK_STATUSES.PENDING,
    ...(input.sequenceIndex !== undefined
      ? { sequenceIndex: input.sequenceIndex }
      : {}),
    ...(input.weight !== undefined ? { weight: input.weight } : {}),
    ...(input.isVetoHolder !== undefined ? { isVetoHolder: input.isVetoHolder } : {}),
    taskVersion: 1,
    createdAt: nowIso(),
    metadata: {
      ...(input.approvalGroupId !== undefined
        ? { approvalGroupId: input.approvalGroupId }
        : {}),
      ...(input.gateNodeId !== undefined
        ? { gateNodeId: input.gateNodeId, approvalGateId: input.gateNodeId }
        : {}),
      ...(input.resolver !== undefined
        ? {
            resolverId: input.resolver.resolverId,
            taskKey: input.resolver.taskKey,
            permission: input.resolver.permission,
          }
        : {}),
      ...(input.assignmentMode !== undefined
        ? { assignmentMode: input.assignmentMode }
        : {}),
    },
  };
}

/**
 * Converts deterministic plan output into the transaction-plan envelope.
 */
export function createTransactionPlan(input: {
  tenantId: string;
  changeRequestId: string;
  actorId: string;
  output?: PlanTransactionOutput;
}): TransactionPlanRecord {
  const timestamp = nowIso();
  const internalWrites = input.output?.internalWrites ?? [];
  const projectionPatches = input.output?.projectionPatches ?? [];
  const externalWrites = input.output?.externalCallRequests ?? [];
  const assignmentOperations = input.output?.assignmentOperations ?? [];
  const roleBindingOperations = input.output?.roleBindingOperations ?? [];

  return {
    transactionPlanId: makeId("txnplan"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequestId,
    status: "simulated",
    planVersion: 1,
    steps: [
      ...internalWrites.map((write, index) => ({
        stepId: `internal_write_${index + 1}`,
        kind: "ledger_write",
        eventType: write.eventType,
      })),
      ...projectionPatches.map((patch, index) => ({
        stepId: `projection_patch_${index + 1}`,
        kind: "projection_write",
        path: patch.path,
      })),
      ...externalWrites.map((write, index) => ({
        stepId: `external_write_${index + 1}`,
        kind: "integration_outbox",
        destination: write.connectionId,
        operation: write.operation,
      })),
      ...assignmentOperations.map((operation, index) => ({
        stepId: `assignment_operation_${index + 1}`,
        kind: "worker_assignment",
        operation,
      })),
      ...roleBindingOperations.map((operation, index) => ({
        stepId: `role_binding_operation_${index + 1}`,
        kind: "role_binding",
        operation,
      })),
    ],
    internalWrites,
    projectionPatches,
    externalWrites,
    assignmentOperations,
    roleBindingOperations,
    rollbackPlan: {
      mode: "compensating_change",
      reason: "Configured workflow changes are corrected by a later approved event.",
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
      projectionPatchCount: projectionPatches.length,
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

export function approvalTaskForWorkflowConfig(input: {
  workflowConfig: WorkflowConfig;
  tenantId: string;
  workflowInstanceId: string;
  changeRequestId: string;
}): ApprovalTaskRecord {
  return createConfiguredApprovalTask({
    tenantId: input.tenantId,
    workflowInstanceId: input.workflowInstanceId,
    changeRequestId: input.changeRequestId,
    assigneeActorId: input.workflowConfig.approval.assigneeActorId,
    assigneeRole: input.workflowConfig.approval.assigneeRole,
    approvalType: input.workflowConfig.approval.approvalType,
    assignmentMode:
      input.workflowConfig.approval.assigneeActorId.trim().length > 0
        ? "actor"
        : "role",
  });
}

export function approvedChangeRequestUpdate(input: {
  changeRequest: ChangeRequestRecord;
  transactionPlanId?: string;
  actorId: string;
}): ChangeRequestRecord {
  return {
    ...input.changeRequest,
    status: CHANGE_REQUEST_STATUSES.APPROVED,
    ...(input.transactionPlanId !== undefined
      ? { transactionPlanId: input.transactionPlanId }
      : {}),
    approvedAt: nowIso(),
    updatedBy: input.actorId,
    version: input.changeRequest.version + 1,
  };
}

function workflowTemplateSources(input: {
  workflowInstance: WorkflowInstanceRecord;
  employeeDocument?: EmployeeProjectionDocument;
  transitionInput: Record<string, unknown>;
  changeRequest?: ChangeRequestRecord;
}): WorkflowTemplateSources {
  return {
    input: input.transitionInput,
    workflow: input.workflowInstance,
    ...(input.employeeDocument !== undefined
      ? { employee: input.employeeDocument }
      : {}),
    ...(input.changeRequest !== undefined
      ? { changeRequest: input.changeRequest }
      : {}),
  };
}

function configuredChangeRequestType(workflowConfig: WorkflowConfig): string {
  const workflowRecord = workflowConfig as unknown as Record<string, unknown>;
  const submitRecord = workflowConfig.submit as unknown as Record<string, unknown>;

  return (
    stringField(submitRecord, "changeRequestType") ??
    stringField(workflowRecord, "changeRequestType") ??
    CHANGE_REQUEST_TYPES.EMPLOYEE_DATA_CHANGE
  );
}

function configuredProposedChangeFieldPath(
  workflowConfig: WorkflowConfig,
  transitionInput: Record<string, unknown>,
): string | undefined {
  const proposedChangeConfig = workflowConfig.submit.proposedChange;

  if (proposedChangeConfig.fieldPath !== undefined) {
    return proposedChangeConfig.fieldPath;
  }

  if (proposedChangeConfig.fieldPathTemplate !== undefined) {
    return renderWorkflowTemplateString(
      proposedChangeConfig.fieldPathTemplate,
      transitionInput,
    );
  }

  return undefined;
}
