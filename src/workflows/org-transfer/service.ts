import {
  ACTOR_ROLES,
  ACTOR_TYPES,
  APPROVAL_TASK_STATUSES,
  CHANGE_REQUEST_STATUSES,
  CHANGE_REQUEST_TYPES,
  INTEGRATION_OUTBOX_STATUSES,
  LEDGER_EVENT_TYPES,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  err,
  invalidWorkflowTransitionError,
  ok,
  permissionDeniedError,
  validationFailedError,
  type AppError,
  type Result,
  type WorkflowState,
} from "@hcm-next/foundation";
import {
  makeId,
  nowIso,
  type ActorRecord,
  type ApprovalTaskRecord,
  type ChangeRequestRecord,
  type CompensationInfo,
  type EmployeeProjectionDocument,
  type JobInfo,
  type LedgerEventRecord,
  type OrganizationInfo,
  type OrganizationUnitRecord,
  type ProposedChangeRecord,
  type Repositories,
  type RoleBindingRecord,
  type TransactionPlanRecord,
  type WorkerAssignmentRecord,
  type WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import { createPermissionSnapshot } from "../legal-name-change/permissions.js";
import {
  buildConfiguredInteraction,
  type WorkflowActionConfig,
  type WorkflowConfig,
} from "../shared/workflow-config.js";

type OrgTransferTransitionBody = {
  transition: string;
  idempotencyKey: string;
  expectedVersion: number;
  input: Record<string, unknown>;
};

type OrgTransferInput = {
  targetLocationOrgUnitId: string;
  targetTeamOrgUnitId: string;
  targetCostCenterOrgUnitId: string;
  targetManagerEmployeeId: string;
  proposedJob?: JobInfo;
  proposedCompensation?: CompensationInfo;
  effectiveAt: string;
  businessReason: string;
  transferReason: string;
  accessImpactAcknowledged: boolean;
};

type PreflightOutput = {
  valid: boolean;
  riskLevel: string;
  requiresEvidence: boolean;
  requiresApproval: boolean;
  warnings: Record<string, unknown>[];
  errors: Record<string, unknown>[];
};

type AssignmentOperation = {
  operation: "supersede" | "create";
  assignmentType: WorkerAssignmentRecord["assignmentType"];
  currentWorkerAssignmentId?: string;
  orgUnitId?: string;
  roleType?: string;
  managerEmployeeId?: string;
  allocationPercent?: number;
  effectiveStart?: string;
  effectiveEnd?: string;
  idempotencyKey: string;
  metadata?: Record<string, unknown>;
};

type RoleBindingOperation = {
  operation: "ensure_direct_reports_binding" | "record_recalculation";
  actorEmployeeId?: string;
  roleKey?: string;
  scopeType?: string;
  effectiveStart?: string;
  idempotencyKey: string;
  metadata?: Record<string, unknown>;
};

type PlanTransactionOutput = {
  internalWrites: Array<{
    eventType: string;
    subjectType: string;
    subjectId: string;
    effectiveAt: string;
    payload: Record<string, unknown>;
  }>;
  projectionPatches: Array<{
    projection: string;
    operation: string;
    path: string;
    value: unknown;
  }>;
  assignmentOperations: AssignmentOperation[];
  roleBindingOperations: RoleBindingOperation[];
  externalCallRequests: Array<{
    connectionId: string;
    operation: string;
    idempotencyKey: string;
    payload: Record<string, unknown>;
    reconciliation?: Record<string, unknown>;
  }>;
};

type ApprovalStageKey =
  | "source_manager"
  | "destination_manager"
  | "finance"
  | "compensation"
  | "medical_director";

type ApprovalStage = {
  key: ApprovalStageKey;
  state: WorkflowState;
  interactionKey: string;
  approvalType: string;
  approvalCreatedEventType: string;
};

const orgTransferIntent = WORKFLOW_INTENTS.EMPLOYEE_ORG_TRANSFER_COMPENSATION_CHANGE;

const approvalStages: ApprovalStage[] = [
  {
    key: "source_manager",
    state: WORKFLOW_STATES.WAITING_SOURCE_MANAGER_APPROVAL,
    interactionKey: "sourceManagerApproval",
    approvalType: "source_manager_org_transfer",
    approvalCreatedEventType: LEDGER_EVENT_TYPES.SOURCE_MANAGER_APPROVAL_CREATED,
  },
  {
    key: "destination_manager",
    state: WORKFLOW_STATES.WAITING_DESTINATION_MANAGER_APPROVAL,
    interactionKey: "destinationManagerApproval",
    approvalType: "destination_manager_org_transfer",
    approvalCreatedEventType: LEDGER_EVENT_TYPES.DESTINATION_MANAGER_APPROVAL_CREATED,
  },
  {
    key: "finance",
    state: WORKFLOW_STATES.WAITING_FINANCE_APPROVAL,
    interactionKey: "financeApproval",
    approvalType: "finance_cost_center_org_transfer",
    approvalCreatedEventType: LEDGER_EVENT_TYPES.FINANCE_APPROVAL_CREATED,
  },
  {
    key: "compensation",
    state: WORKFLOW_STATES.WAITING_COMPENSATION_APPROVAL,
    interactionKey: "compensationApproval",
    approvalType: "compensation_org_transfer",
    approvalCreatedEventType: LEDGER_EVENT_TYPES.COMPENSATION_APPROVAL_CREATED,
  },
  {
    key: "medical_director",
    state: WORKFLOW_STATES.WAITING_MEDICAL_DIRECTOR_APPROVAL,
    interactionKey: "medicalDirectorApproval",
    approvalType: "clinical_placement_org_transfer",
    approvalCreatedEventType: LEDGER_EVENT_TYPES.CLINICAL_PLACEMENT_APPROVAL_CREATED,
  },
];

/**
 * Identifies the org transfer workflow without coupling callers to raw strings.
 */
export function isOrgTransferWorkflowIntent(intent: string): boolean {
  return intent === orgTransferIntent;
}

/**
 * Checks whether the current actor can act on the current org transfer approval task.
 */
export function canPerformOrgTransferApproval(input: {
  actor: ActorRecord;
  pendingApprovalTask?: ApprovalTaskRecord;
}): boolean {
  const pendingTask = input.pendingApprovalTask;

  if (pendingTask === undefined) {
    return false;
  }

  return (
    pendingTask.assigneeActorId === input.actor.actorId ||
    (pendingTask.assigneeRole !== ACTOR_ROLES.MANAGER &&
      input.actor.roles.includes(pendingTask.assigneeRole))
  );
}

/**
 * Handles the org transfer submit transition and creates the first approval task.
 */
export async function submitOrgTransferInput(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    actionConfig: WorkflowActionConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: OrgTransferTransitionBody;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const transferInputResult = parseOrgTransferInput(input.transitionBody.input);
  if (!transferInputResult.ok) {
    return transferInputResult;
  }

  const contextResult = loadOrgTransferContext(
    repositories,
    requestContext.tenantId,
    input.workflowInstance.subjectId,
    transferInputResult.value,
  );
  if (!contextResult.ok) {
    return contextResult;
  }

  const preflightInput = buildPreflightBlockInput(contextResult.value);
  const preflightResult =
    await dependencies.executorClient.executeBlock<PreflightOutput>({
      tenantId: requestContext.tenantId,
      environmentId: requestContext.environmentId,
      changeRequestId: "",
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      block: input.workflowConfig.submit.preflightBlock,
      input: preflightInput,
      context: {
        actorId: requestContext.actor.actorId,
        effectiveAt: transferInputResult.value.effectiveAt,
        permissions: createPermissionSnapshot(requestContext.actor),
        correlationId: requestContext.correlationId,
        idempotencyKey: input.transitionBody.idempotencyKey,
      },
    });
  if (!preflightResult.ok) {
    return preflightResult;
  }

  if (
    preflightResult.value.output === undefined ||
    !preflightResult.value.output.valid
  ) {
    return err(validationFailedError({ preflight: preflightResult.value.output }));
  }

  const timestamp = nowIso();
  const changeRequest = createOrgTransferChangeRequest({
    requestContext,
    workflowInstance: input.workflowInstance,
    context: contextResult.value,
    preflight: preflightResult.value.output,
    timestamp,
  });
  const changeRequestResult = repositories.changeRequests.create(changeRequest);
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const proposedChanges = createOrgTransferProposedChanges({
    tenantId: requestContext.tenantId,
    changeRequest,
    context: contextResult.value,
    timestamp,
  });
  const proposedChangesResult =
    repositories.proposedChanges.createMany(proposedChanges);
  if (!proposedChangesResult.ok) {
    return proposedChangesResult;
  }

  const firstStage = approvalStages[0];
  if (firstStage === undefined) {
    return err(validationFailedError({ approvalStages: "missing" }));
  }

  const firstApprovalTaskResult = createApprovalTaskForStage({
    repositories,
    tenantId: requestContext.tenantId,
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    changeRequestId: changeRequest.changeRequestId,
    context: contextResult.value,
    stage: firstStage,
  });
  if (!firstApprovalTaskResult.ok) {
    return firstApprovalTaskResult;
  }

  const nextInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: firstStage.interactionKey,
    employeeDocument: contextResult.value.employeeProjection.document,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const updatedWorkflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: firstStage.state,
    status: WORKFLOW_STATUSES.WAITING,
    changeRequestId: changeRequest.changeRequestId,
    currentInteraction: nextInteractionResult.value,
    context: {
      ...input.workflowInstance.context,
      orgTransfer: {
        input: transferInputResult.value,
        proposedSnapshot: changeRequest.proposedSnapshot,
        approvalStage: firstStage.key,
      },
      preflight: preflightResult.value.output,
      approvalTaskId: firstApprovalTaskResult.value.approvalTaskId,
    },
    version: input.workflowInstance.version + 1,
  });
  if (!updatedWorkflowResult.ok) {
    return updatedWorkflowResult;
  }

  const transitionLedgerResult = appendTransitionLedgerEvents(
    repositories,
    requestContext,
    {
      workflowInstance: updatedWorkflowResult.value,
      previousState: input.workflowInstance.state,
      eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_CREATED,
      idempotencyKey: input.transitionBody.idempotencyKey,
      payload: {
        changeRequest,
        proposedChanges,
        preflight: preflightResult.value.output,
      },
    },
  );
  if (!transitionLedgerResult.ok) {
    return transitionLedgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    repositories,
    requestContext,
    updatedWorkflowResult.value,
    input.transitionBody.idempotencyKey,
    [
      {
        eventType: LEDGER_EVENT_TYPES.PROPOSED_CHANGE_CREATED,
        payload: { proposedChanges },
      },
      {
        eventType: LEDGER_EVENT_TYPES.ORG_TRANSFER_PREFLIGHTED,
        payload: preflightResult.value.output,
      },
      {
        eventType: firstStage.approvalCreatedEventType,
        approvalTaskId: firstApprovalTaskResult.value.approvalTaskId,
        payload: { approvalTask: firstApprovalTaskResult.value },
      },
      {
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_SUBMITTED,
        payload: { changeRequest },
      },
      {
        eventType: LEDGER_EVENT_TYPES.ORG_TRANSFER_SUBMITTED,
        payload: {
          changeRequestId: changeRequest.changeRequestId,
          approvalStage: firstStage.key,
        },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(updatedWorkflowResult.value));
}

/**
 * Handles serial org transfer approval stages and creates the transaction plan after the final approval.
 */
export async function approveOrgTransferStage(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: OrgTransferTransitionBody;
    pendingApprovalTask?: ApprovalTaskRecord;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const decisionInputResult = parseApprovalDecisionInput(input.transitionBody.input);
  if (!decisionInputResult.ok) {
    return decisionInputResult;
  }

  const pendingTaskResult = verifyApprovalTask(
    input.pendingApprovalTask,
    decisionInputResult.value.approvalTaskId,
  );
  if (!pendingTaskResult.ok) {
    return pendingTaskResult;
  }

  const stageResult = approvalStageFromTask(pendingTaskResult.value);
  if (!stageResult.ok) {
    return stageResult;
  }

  const changeRequestResult = findChangeRequestForWorkflow(
    repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const transferInputResult = orgTransferInputFromWorkflow(input.workflowInstance);
  if (!transferInputResult.ok) {
    return transferInputResult;
  }

  const contextResult = loadOrgTransferContext(
    repositories,
    requestContext.tenantId,
    input.workflowInstance.subjectId,
    transferInputResult.value,
  );
  if (!contextResult.ok) {
    return contextResult;
  }

  if (stageResult.value.key === "finance") {
    const financeScopeResult = actorCanApproveTargetCostCenter({
      repositories,
      actor: requestContext.actor,
      tenantId: requestContext.tenantId,
      targetCostCenter: contextResult.value.targetCostCenterOrgUnit,
    });
    if (!financeScopeResult.ok) {
      return financeScopeResult;
    }
  }

  const updatedTaskResult = repositories.approvals.update({
    ...pendingTaskResult.value,
    status: APPROVAL_TASK_STATUSES.APPROVED,
    decision: "approved",
    ...(decisionInputResult.value.comment !== undefined
      ? { comments: decisionInputResult.value.comment }
      : {}),
    decidedAt: nowIso(),
  });
  if (!updatedTaskResult.ok) {
    return updatedTaskResult;
  }

  const nextStage = nextApprovalStage(stageResult.value);
  if (nextStage !== undefined) {
    return createNextOrgTransferApprovalStage({
      repositories,
      requestContext,
      workflowConfig: input.workflowConfig,
      workflowInstance: input.workflowInstance,
      changeRequest: changeRequestResult.value,
      context: contextResult.value,
      approvedTask: updatedTaskResult.value,
      nextStage,
      idempotencyKey: input.transitionBody.idempotencyKey,
    });
  }

  return approveFinalOrgTransferStage({
    dependencies,
    requestContext,
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    changeRequest: changeRequestResult.value,
    context: contextResult.value,
    approvedTask: updatedTaskResult.value,
    idempotencyKey: input.transitionBody.idempotencyKey,
  });
}

/**
 * Executes the approved org transfer transaction plan with idempotent internal writes.
 */
export async function executeApprovedOrgTransfer(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: OrgTransferTransitionBody;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const changeRequestResult = findChangeRequestForWorkflow(
    repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  if (changeRequestResult.value.status !== CHANGE_REQUEST_STATUSES.APPROVED) {
    return err(
      invalidWorkflowTransitionError({
        status: changeRequestResult.value.status,
      }),
    );
  }

  if (changeRequestResult.value.transactionPlanId === undefined) {
    return err(validationFailedError({ transactionPlanId: "missing" }));
  }

  const transactionPlanResult = repositories.transactionPlans.findById(
    changeRequestResult.value.transactionPlanId,
  );
  if (!transactionPlanResult.ok) {
    return transactionPlanResult;
  }

  const executionStartedResult = appendWorkflowLedgerEvent(
    repositories,
    requestContext,
    {
      eventType: LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_STARTED,
      workflowInstance: input.workflowInstance,
      subjectType: input.workflowInstance.subjectType,
      subjectId: input.workflowInstance.subjectId,
      idempotencyKey: input.transitionBody.idempotencyKey,
      transactionPlanId: transactionPlanResult.value.transactionPlanId,
      payload: {
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
      },
    },
  );
  if (!executionStartedResult.ok) {
    return executionStartedResult;
  }

  const planOutputResult = planOutputFromTransactionPlan(transactionPlanResult.value);
  if (!planOutputResult.ok) {
    return planOutputResult;
  }

  const assignmentWriteResult = applyAssignmentOperations({
    repositories,
    requestContext,
    workflowInstance: input.workflowInstance,
    changeRequest: changeRequestResult.value,
    transactionPlan: transactionPlanResult.value,
    operations: planOutputResult.value.assignmentOperations,
    idempotencyKey: input.transitionBody.idempotencyKey,
  });
  if (!assignmentWriteResult.ok) {
    return assignmentWriteResult;
  }

  const roleBindingResult = applyRoleBindingOperations({
    repositories,
    requestContext,
    workflowInstance: input.workflowInstance,
    changeRequest: changeRequestResult.value,
    transactionPlan: transactionPlanResult.value,
    operations: planOutputResult.value.roleBindingOperations,
    idempotencyKey: input.transitionBody.idempotencyKey,
  });
  if (!roleBindingResult.ok) {
    return roleBindingResult;
  }

  const projectionResult = applyOrgTransferProjection({
    repositories,
    requestContext,
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    changeRequest: changeRequestResult.value,
    transactionPlan: transactionPlanResult.value,
    planOutput: planOutputResult.value,
    idempotencyKey: input.transitionBody.idempotencyKey,
  });
  if (!projectionResult.ok) {
    return projectionResult;
  }

  const outboxResult = createOrgTransferOutboxRows({
    repositories,
    requestContext,
    changeRequest: changeRequestResult.value,
    transactionPlan: transactionPlanResult.value,
    externalWrites: planOutputResult.value.externalCallRequests,
  });
  if (!outboxResult.ok) {
    return outboxResult;
  }

  const closedChangeRequestResult = repositories.changeRequests.update({
    ...changeRequestResult.value,
    status: CHANGE_REQUEST_STATUSES.EXECUTED,
    executedAt: nowIso(),
    closedAt: nowIso(),
    updatedBy: requestContext.actor.actorId,
    version: changeRequestResult.value.version + 1,
  });
  if (!closedChangeRequestResult.ok) {
    return closedChangeRequestResult;
  }

  const executedPlanResult = repositories.transactionPlans.update({
    ...transactionPlanResult.value,
    status: "executed",
    executionResult: {
      assignmentWrites: assignmentWriteResult.value,
      roleBindingWrites: roleBindingResult.value,
      projection: projectionResult.value,
      outboxRows: outboxResult.value,
    },
    updatedBy: requestContext.actor.actorId,
  });
  if (!executedPlanResult.ok) {
    return executedPlanResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.EXECUTED,
    status: WORKFLOW_STATUSES.COMPLETED,
    currentInteraction: terminalInteraction("executed"),
    completedAt: nowIso(),
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const transitionLedgerResult = appendTransitionLedgerEvents(
    repositories,
    requestContext,
    {
      workflowInstance: workflowResult.value,
      previousState: input.workflowInstance.state,
      eventType: LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_COMPLETED,
      idempotencyKey: input.transitionBody.idempotencyKey,
      transactionPlanId: transactionPlanResult.value.transactionPlanId,
      payload: {
        changeRequest: closedChangeRequestResult.value,
        transactionPlan: executedPlanResult.value,
      },
    },
  );
  if (!transitionLedgerResult.ok) {
    return transitionLedgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    repositories,
    requestContext,
    workflowResult.value,
    input.transitionBody.idempotencyKey,
    [
      {
        eventType: LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED,
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
        payload: { outboxRows: outboxResult.value },
      },
      {
        eventType: LEDGER_EVENT_TYPES.ORG_TRANSFER_EXECUTED,
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
        payload: {
          changeRequestId: changeRequestResult.value.changeRequestId,
          assignmentWriteCount: assignmentWriteResult.value.length,
          outboxWriteCount: outboxResult.value.length,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED,
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
        payload: {
          terminalState: WORKFLOW_STATES.EXECUTED,
        },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

type OrgTransferContext = {
  input: OrgTransferInput;
  employeeProjection: {
    employeeId: string;
    document: EmployeeProjectionDocument;
  };
  targetLocationOrgUnit: OrganizationUnitRecord;
  targetTeamOrgUnit: OrganizationUnitRecord;
  targetCostCenterOrgUnit: OrganizationUnitRecord;
  targetManagerProjection: {
    employeeId: string;
    document: EmployeeProjectionDocument;
  };
  activeAssignments: WorkerAssignmentRecord[];
  proposedOrganization: OrganizationInfo;
  proposedJob: JobInfo;
  proposedCompensation: CompensationInfo;
};

function parseOrgTransferInput(
  input: Record<string, unknown>,
): Result<OrgTransferInput, AppError> {
  const targetLocationOrgUnitId = stringField(input, "targetLocationOrgUnitId");
  const targetTeamOrgUnitId = stringField(input, "targetTeamOrgUnitId");
  const targetCostCenterOrgUnitId = stringField(input, "targetCostCenterOrgUnitId");
  const targetManagerEmployeeId = stringField(input, "targetManagerEmployeeId");
  const effectiveAt = stringField(input, "effectiveAt");
  const businessReason = stringField(input, "businessReason");
  const transferReason = stringField(input, "transferReason");
  const accessImpactAcknowledged = booleanField(input, "accessImpactAcknowledged");
  const proposedJobResult = parseOptionalJobInfo(objectField(input, "proposedJob"));
  const proposedCompensationResult = parseOptionalCompensationInfo(
    objectField(input, "proposedCompensation"),
  );

  if (!proposedJobResult.ok) {
    return proposedJobResult;
  }
  if (!proposedCompensationResult.ok) {
    return proposedCompensationResult;
  }

  if (
    targetLocationOrgUnitId === undefined ||
    targetTeamOrgUnitId === undefined ||
    targetCostCenterOrgUnitId === undefined ||
    targetManagerEmployeeId === undefined ||
    effectiveAt === undefined ||
    businessReason === undefined ||
    transferReason === undefined ||
    accessImpactAcknowledged === undefined
  ) {
    return err(
      validationFailedError({
        targetLocationOrgUnitId,
        targetTeamOrgUnitId,
        targetCostCenterOrgUnitId,
        targetManagerEmployeeId,
        effectiveAt,
        businessReason,
        transferReason,
        accessImpactAcknowledged,
      }),
    );
  }

  return ok({
    targetLocationOrgUnitId,
    targetTeamOrgUnitId,
    targetCostCenterOrgUnitId,
    targetManagerEmployeeId,
    ...(proposedJobResult.value !== undefined
      ? { proposedJob: proposedJobResult.value }
      : {}),
    ...(proposedCompensationResult.value !== undefined
      ? { proposedCompensation: proposedCompensationResult.value }
      : {}),
    effectiveAt,
    businessReason,
    transferReason,
    accessImpactAcknowledged,
  });
}

function loadOrgTransferContext(
  repositories: Repositories,
  tenantId: string,
  employeeId: string,
  input: OrgTransferInput,
): Result<OrgTransferContext, AppError> {
  const employeeProjectionResult = repositories.employeeProjections.findByEmployeeId(
    tenantId,
    employeeId,
  );
  if (!employeeProjectionResult.ok) {
    return employeeProjectionResult;
  }

  const targetLocationResult = repositories.organizationUnits.findById(
    tenantId,
    input.targetLocationOrgUnitId,
  );
  if (!targetLocationResult.ok) {
    return targetLocationResult;
  }

  const targetTeamResult = repositories.organizationUnits.findById(
    tenantId,
    input.targetTeamOrgUnitId,
  );
  if (!targetTeamResult.ok) {
    return targetTeamResult;
  }

  const targetCostCenterResult = repositories.organizationUnits.findById(
    tenantId,
    input.targetCostCenterOrgUnitId,
  );
  if (!targetCostCenterResult.ok) {
    return targetCostCenterResult;
  }

  const targetManagerProjectionResult =
    repositories.employeeProjections.findByEmployeeId(
      tenantId,
      input.targetManagerEmployeeId,
    );
  if (!targetManagerProjectionResult.ok) {
    return targetManagerProjectionResult;
  }

  const activeAssignmentsResult = repositories.workerAssignments.findActiveForEmployee(
    tenantId,
    employeeId,
  );
  if (!activeAssignmentsResult.ok) {
    return activeAssignmentsResult;
  }

  const proposedOrganization = buildProposedOrganization({
    currentOrganization: employeeProjectionResult.value.document.organization,
    targetLocation: targetLocationResult.value,
    targetTeam: targetTeamResult.value,
    targetCostCenter: targetCostCenterResult.value,
  });

  return ok({
    input,
    employeeProjection: employeeProjectionResult.value,
    targetLocationOrgUnit: targetLocationResult.value,
    targetTeamOrgUnit: targetTeamResult.value,
    targetCostCenterOrgUnit: targetCostCenterResult.value,
    targetManagerProjection: targetManagerProjectionResult.value,
    activeAssignments: activeAssignmentsResult.value,
    proposedOrganization,
    proposedJob: input.proposedJob ?? emptyJobInfo(),
    proposedCompensation: input.proposedCompensation ?? emptyCompensationInfo(),
  });
}

function buildPreflightBlockInput(
  context: OrgTransferContext,
): Record<string, unknown> {
  return {
    employee: {
      employeeId: context.employeeProjection.employeeId,
      employmentStatus: context.employeeProjection.document.employment.status,
    },
    currentOrganization: context.employeeProjection.document.organization,
    targetLocationOrgUnit: orgUnitForBlock(context.targetLocationOrgUnit),
    targetTeamOrgUnit: orgUnitForBlock(context.targetTeamOrgUnit),
    targetCostCenterOrgUnit: orgUnitForBlock(context.targetCostCenterOrgUnit),
    targetManager: {
      employeeId: context.targetManagerProjection.employeeId,
      employmentStatus: context.targetManagerProjection.document.employment.status,
    },
    activeAssignments: context.activeAssignments.map(workerAssignmentForBlock),
    effectiveAt: context.input.effectiveAt,
    businessReason: context.input.businessReason,
    transferReason: context.input.transferReason,
    accessImpactAcknowledged: context.input.accessImpactAcknowledged,
  };
}

function buildPlanBlockInput(input: {
  changeRequest: ChangeRequestRecord;
  context: OrgTransferContext;
}): Record<string, unknown> {
  const employeeDocument = input.context.employeeProjection.document;

  return {
    changeRequestId: input.changeRequest.changeRequestId,
    workerId: input.changeRequest.targetWorkerId,
    currentOrganization: employeeDocument.organization,
    proposedOrganization: input.context.proposedOrganization,
    currentJob: employeeDocument.job,
    proposedJob: input.context.proposedJob,
    currentCompensation: employeeDocument.compensation,
    proposedCompensation: input.context.proposedCompensation,
    currentAssignments: input.context.activeAssignments.map(workerAssignmentForBlock),
    targetTeamOrgUnit: orgUnitForBlock(input.context.targetTeamOrgUnit),
    targetLocationOrgUnit: orgUnitForBlock(input.context.targetLocationOrgUnit),
    targetCostCenterOrgUnit: orgUnitForBlock(input.context.targetCostCenterOrgUnit),
    sourceManagerEmployeeId: employeeDocument.manager.employeeId ?? "",
    targetManagerEmployeeId: input.context.input.targetManagerEmployeeId,
    effectiveAt: input.changeRequest.effectiveAt,
    businessReason: input.changeRequest.businessReason,
  };
}

function createOrgTransferChangeRequest(input: {
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  context: OrgTransferContext;
  preflight: PreflightOutput;
  timestamp: string;
}): ChangeRequestRecord {
  const employeeDocument = input.context.employeeProjection.document;

  return {
    changeRequestId: makeId("chg"),
    tenantId: input.requestContext.tenantId,
    environmentId: input.requestContext.environmentId,
    changeType: CHANGE_REQUEST_TYPES.EMPLOYEE_DATA_CHANGE,
    targetWorkerId: input.workflowInstance.subjectId,
    requesterActorId: input.requestContext.actor.actorId,
    effectiveAt: input.context.input.effectiveAt,
    businessReason: input.context.input.businessReason,
    status: CHANGE_REQUEST_STATUSES.IN_APPROVAL,
    priority: "normal",
    currentSnapshot: {
      organization: employeeDocument.organization,
      manager: employeeDocument.manager,
      job: employeeDocument.job,
      compensation: employeeDocument.compensation,
    },
    proposedSnapshot: {
      organization: input.context.proposedOrganization,
      manager: {
        employeeId: input.context.input.targetManagerEmployeeId,
      },
      ...(input.context.input.proposedJob !== undefined
        ? { job: input.context.input.proposedJob }
        : {}),
      ...(input.context.input.proposedCompensation !== undefined
        ? { compensation: input.context.input.proposedCompensation }
        : {}),
    },
    preflightResult: input.preflight,
    aiReview: {
      mode: "not_configured_v0",
      summary:
        "Org transfer compensation change uses deterministic preflight and approval routing.",
    },
    workflowDefinitionId: input.workflowInstance.workflowDefinitionId,
    workflowVersionId: input.workflowInstance.workflowVersionId,
    submittedAt: input.timestamp,
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
    createdBy: input.requestContext.actor.actorId,
    updatedBy: input.requestContext.actor.actorId,
    version: 1,
    metadata: {
      transferReason: input.context.input.transferReason,
      targetLocationOrgUnitId: input.context.targetLocationOrgUnit.orgUnitId,
      targetTeamOrgUnitId: input.context.targetTeamOrgUnit.orgUnitId,
      targetCostCenterOrgUnitId: input.context.targetCostCenterOrgUnit.orgUnitId,
      targetManagerEmployeeId: input.context.input.targetManagerEmployeeId,
    },
  };
}

function createOrgTransferProposedChanges(input: {
  tenantId: string;
  changeRequest: ChangeRequestRecord;
  context: OrgTransferContext;
  timestamp: string;
}): ProposedChangeRecord[] {
  const employeeDocument = input.context.employeeProjection.document;
  const changes: Array<{
    fieldPath: string;
    currentValue: unknown;
    proposedValue: unknown;
  }> = [
    {
      fieldPath: "organization.location",
      currentValue: employeeDocument.organization.location,
      proposedValue: input.context.proposedOrganization.location,
    },
    {
      fieldPath: "organization.team",
      currentValue: employeeDocument.organization.team,
      proposedValue: input.context.proposedOrganization.team,
    },
    {
      fieldPath: "organization.costCenter",
      currentValue: employeeDocument.organization.costCenter,
      proposedValue: input.context.proposedOrganization.costCenter,
    },
    {
      fieldPath: "manager.employeeId",
      currentValue: employeeDocument.manager.employeeId,
      proposedValue: input.context.input.targetManagerEmployeeId,
    },
  ];

  if (input.context.input.proposedJob !== undefined) {
    changes.push({
      fieldPath: "job",
      currentValue: employeeDocument.job,
      proposedValue: input.context.input.proposedJob,
    });
  }

  if (input.context.input.proposedCompensation !== undefined) {
    changes.push({
      fieldPath: "compensation",
      currentValue: employeeDocument.compensation,
      proposedValue: input.context.input.proposedCompensation,
    });
  }

  return changes.map((change) => ({
    proposedChangeId: makeId("pchg"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequest.changeRequestId,
    targetObjectType: "worker",
    targetObjectId: input.changeRequest.targetWorkerId,
    fieldPath: change.fieldPath,
    currentValue: change.currentValue,
    proposedValue: change.proposedValue,
    effectiveAt: input.changeRequest.effectiveAt,
    reasonCode: input.changeRequest.businessReason,
    validationStatus: "valid",
    riskLevel: input.changeRequest.preflightResult["riskLevel"] as string,
    metadata: {},
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
  }));
}

function createApprovalTaskForStage(input: {
  repositories: Repositories;
  tenantId: string;
  workflowInstanceId: string;
  changeRequestId: string;
  context: OrgTransferContext;
  stage: ApprovalStage;
}): Result<ApprovalTaskRecord, AppError> {
  const assigneeResult = resolveApprovalAssignee(input);
  if (!assigneeResult.ok) {
    return assigneeResult;
  }

  return input.repositories.approvals.create({
    approvalTaskId: makeId("appr"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequestId,
    workflowInstanceId: input.workflowInstanceId,
    assigneeActorId: assigneeResult.value.actor.actorId,
    assigneeRole: assigneeResult.value.assigneeRole,
    approvalType: input.stage.approvalType,
    status: APPROVAL_TASK_STATUSES.PENDING,
    createdAt: nowIso(),
    metadata: {
      stageKey: input.stage.key,
      targetCostCenterOrgUnitId: input.context.targetCostCenterOrgUnit.orgUnitId,
      targetManagerEmployeeId: input.context.input.targetManagerEmployeeId,
    },
  });
}

function resolveApprovalAssignee(input: {
  repositories: Repositories;
  tenantId: string;
  context: OrgTransferContext;
  stage: ApprovalStage;
}): Result<{ actor: ActorRecord; assigneeRole: string }, AppError> {
  const sourceManagerEmployeeId =
    input.context.employeeProjection.document.manager.employeeId;

  if (input.stage.key === "source_manager") {
    if (sourceManagerEmployeeId === null) {
      return err(validationFailedError({ sourceManagerEmployeeId }));
    }

    const actorResult = input.repositories.actors.findByLinkedWorkerId(
      input.tenantId,
      sourceManagerEmployeeId,
    );
    if (!actorResult.ok) {
      return actorResult;
    }

    return ok({ actor: actorResult.value, assigneeRole: ACTOR_ROLES.MANAGER });
  }

  if (input.stage.key === "destination_manager") {
    const actorResult = input.repositories.actors.findByLinkedWorkerId(
      input.tenantId,
      input.context.input.targetManagerEmployeeId,
    );
    if (!actorResult.ok) {
      return actorResult;
    }

    return ok({ actor: actorResult.value, assigneeRole: ACTOR_ROLES.MANAGER });
  }

  if (input.stage.key === "finance") {
    const actorResult = input.repositories.actors.findFirstActiveByRole(
      input.tenantId,
      ACTOR_ROLES.FINANCE_ADMIN,
    );
    if (!actorResult.ok) {
      return actorResult;
    }

    return ok({ actor: actorResult.value, assigneeRole: ACTOR_ROLES.FINANCE_ADMIN });
  }

  if (input.stage.key === "compensation") {
    const actorResult = input.repositories.actors.findFirstActiveByRole(
      input.tenantId,
      ACTOR_ROLES.COMPENSATION_ADMIN,
    );
    if (!actorResult.ok) {
      return actorResult;
    }

    return ok({
      actor: actorResult.value,
      assigneeRole: ACTOR_ROLES.COMPENSATION_ADMIN,
    });
  }

  const medicalDirectorResult = input.repositories.actors.findFirstActiveByRole(
    input.tenantId,
    "medical_director",
  );
  if (medicalDirectorResult.ok) {
    return ok({
      actor: medicalDirectorResult.value,
      assigneeRole: "medical_director",
    });
  }

  const clinicalAdminResult = input.repositories.actors.findFirstActiveByRole(
    input.tenantId,
    "clinical_admin",
  );
  if (!clinicalAdminResult.ok) {
    return clinicalAdminResult;
  }

  return ok({
    actor: clinicalAdminResult.value,
    assigneeRole: "clinical_admin",
  });
}

function createNextOrgTransferApprovalStage(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  context: OrgTransferContext;
  approvedTask: ApprovalTaskRecord;
  nextStage: ApprovalStage;
  idempotencyKey: string;
}): Result<Record<string, unknown>, AppError> {
  const nextApprovalTaskResult = createApprovalTaskForStage({
    repositories: input.repositories,
    tenantId: input.requestContext.tenantId,
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    changeRequestId: input.changeRequest.changeRequestId,
    context: input.context,
    stage: input.nextStage,
  });
  if (!nextApprovalTaskResult.ok) {
    return nextApprovalTaskResult;
  }

  const nextInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: input.nextStage.interactionKey,
    employeeDocument: input.context.employeeProjection.document,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const workflowResult = input.repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: input.nextStage.state,
    status: WORKFLOW_STATUSES.WAITING,
    currentInteraction: nextInteractionResult.value,
    context: {
      ...input.workflowInstance.context,
      approvalTaskId: nextApprovalTaskResult.value.approvalTaskId,
      orgTransfer: {
        ...objectField(input.workflowInstance.context, "orgTransfer"),
        approvalStage: input.nextStage.key,
      },
    },
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(
    input.repositories,
    input.requestContext,
    {
      workflowInstance: workflowResult.value,
      previousState: input.workflowInstance.state,
      eventType: LEDGER_EVENT_TYPES.APPROVAL_GRANTED,
      idempotencyKey: input.idempotencyKey,
      approvalTaskId: input.approvedTask.approvalTaskId,
      payload: {
        approvalTask: input.approvedTask,
        nextApprovalTask: nextApprovalTaskResult.value,
      },
    },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    input.repositories,
    input.requestContext,
    workflowResult.value,
    input.idempotencyKey,
    [
      {
        eventType: input.nextStage.approvalCreatedEventType,
        approvalTaskId: nextApprovalTaskResult.value.approvalTaskId,
        payload: { approvalTask: nextApprovalTaskResult.value },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

async function approveFinalOrgTransferStage(input: {
  dependencies: AppDependencies;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  context: OrgTransferContext;
  approvedTask: ApprovalTaskRecord;
  idempotencyKey: string;
}): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = input.dependencies.repositories;
  const planInput = buildPlanBlockInput({
    changeRequest: input.changeRequest,
    context: input.context,
  });
  const planResult =
    await input.dependencies.executorClient.executeBlock<PlanTransactionOutput>({
      tenantId: input.requestContext.tenantId,
      environmentId: input.requestContext.environmentId,
      changeRequestId: input.changeRequest.changeRequestId,
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      block: input.workflowConfig.plan.block,
      input: planInput,
      context: {
        actorId: input.requestContext.actor.actorId,
        effectiveAt: input.changeRequest.effectiveAt,
        permissions: createPermissionSnapshot(input.requestContext.actor),
        correlationId: input.requestContext.correlationId,
        idempotencyKey: input.idempotencyKey,
      },
    });
  if (!planResult.ok) {
    return planResult;
  }

  if (planResult.value.output === undefined) {
    return err(validationFailedError({ planOutput: "missing" }));
  }

  const transactionPlan = createOrgTransferTransactionPlan({
    tenantId: input.requestContext.tenantId,
    changeRequestId: input.changeRequest.changeRequestId,
    actorId: input.requestContext.actor.actorId,
    output: planResult.value.output,
  });
  const transactionPlanResult = repositories.transactionPlans.create(transactionPlan);
  if (!transactionPlanResult.ok) {
    return transactionPlanResult;
  }

  const changeRequestResult = repositories.changeRequests.update({
    ...input.changeRequest,
    status: CHANGE_REQUEST_STATUSES.APPROVED,
    transactionPlanId: transactionPlan.transactionPlanId,
    approvedAt: nowIso(),
    updatedBy: input.requestContext.actor.actorId,
    version: input.changeRequest.version + 1,
  });
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const readyInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: "readyToExecute",
    employeeDocument: input.context.employeeProjection.document,
  });
  if (!readyInteractionResult.ok) {
    return readyInteractionResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.APPROVED,
    status: WORKFLOW_STATUSES.ACTIVE,
    currentInteraction: readyInteractionResult.value,
    context: {
      ...input.workflowInstance.context,
      transactionPlanId: transactionPlan.transactionPlanId,
      orgTransfer: {
        ...objectField(input.workflowInstance.context, "orgTransfer"),
        approvalStage: "approved",
      },
    },
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(
    repositories,
    input.requestContext,
    {
      workflowInstance: workflowResult.value,
      previousState: input.workflowInstance.state,
      eventType: LEDGER_EVENT_TYPES.APPROVAL_GRANTED,
      idempotencyKey: input.idempotencyKey,
      approvalTaskId: input.approvedTask.approvalTaskId,
      transactionPlanId: transactionPlan.transactionPlanId,
      payload: {
        approvalTask: input.approvedTask,
        transactionPlan,
        changeRequest: changeRequestResult.value,
      },
    },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    repositories,
    input.requestContext,
    workflowResult.value,
    input.idempotencyKey,
    [
      {
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_APPROVED,
        transactionPlanId: transactionPlan.transactionPlanId,
        payload: { changeRequest: changeRequestResult.value },
      },
      {
        eventType: LEDGER_EVENT_TYPES.TRANSACTION_PLAN_CREATED,
        transactionPlanId: transactionPlan.transactionPlanId,
        payload: { transactionPlan },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

function createOrgTransferTransactionPlan(input: {
  tenantId: string;
  changeRequestId: string;
  actorId: string;
  output: PlanTransactionOutput;
}): TransactionPlanRecord {
  const timestamp = nowIso();

  return {
    transactionPlanId: makeId("txnplan"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequestId,
    status: "simulated",
    planVersion: 1,
    steps: [
      ...input.output.assignmentOperations.map((operation) => ({
        kind: "worker_assignment",
        ...operation,
      })),
      ...input.output.roleBindingOperations.map((operation) => ({
        kind: "role_binding",
        ...operation,
      })),
    ],
    internalWrites: input.output.internalWrites,
    projectionPatches: input.output.projectionPatches,
    externalWrites: input.output.externalCallRequests,
    rollbackPlan: {
      mode: "compensating_change",
      reason: "Org transfers are corrected by a new approved workflow.",
    },
    compensationPlan: {
      mode: "manual_review_if_assignment_or_external_sync_fails",
    },
    idempotencyKeys: {
      assignmentOperations: input.output.assignmentOperations.map(
        (operation) => operation.idempotencyKey,
      ),
      roleBindingOperations: input.output.roleBindingOperations.map(
        (operation) => operation.idempotencyKey,
      ),
      externalWrites: input.output.externalCallRequests.map(
        (write) => write.idempotencyKey,
      ),
    },
    simulationResult: {
      valid: true,
      assignmentOperationCount: input.output.assignmentOperations.length,
      roleBindingOperationCount: input.output.roleBindingOperations.length,
      externalWriteCount: input.output.externalCallRequests.length,
    },
    executionResult: {},
    reconciliationResult: {},
    createdAt: timestamp,
    updatedAt: timestamp,
    createdBy: input.actorId,
    updatedBy: input.actorId,
  };
}

function planOutputFromTransactionPlan(
  transactionPlan: TransactionPlanRecord,
): Result<PlanTransactionOutput, AppError> {
  const assignmentOperations = transactionPlan.steps
    .filter((step) => step["kind"] === "worker_assignment")
    .map((step) => step as AssignmentOperation);
  const roleBindingOperations = transactionPlan.steps
    .filter((step) => step["kind"] === "role_binding")
    .map((step) => step as RoleBindingOperation);

  return ok({
    internalWrites:
      transactionPlan.internalWrites as PlanTransactionOutput["internalWrites"],
    projectionPatches:
      transactionPlan.projectionPatches as PlanTransactionOutput["projectionPatches"],
    assignmentOperations,
    roleBindingOperations,
    externalCallRequests:
      transactionPlan.externalWrites as PlanTransactionOutput["externalCallRequests"],
  });
}

function applyAssignmentOperations(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  transactionPlan: TransactionPlanRecord;
  operations: AssignmentOperation[];
  idempotencyKey: string;
}): Result<WorkerAssignmentRecord[], AppError> {
  const writtenAssignments: WorkerAssignmentRecord[] = [];

  for (const operation of input.operations) {
    if (operation.operation === "supersede") {
      const supersedeResult = supersedeAssignment(input, operation);
      if (!supersedeResult.ok) {
        return supersedeResult;
      }

      writtenAssignments.push(supersedeResult.value);
      continue;
    }

    const createResult = createTargetAssignment(input, operation);
    if (!createResult.ok) {
      return createResult;
    }

    writtenAssignments.push(createResult.value);
  }

  return ok(writtenAssignments);
}

function supersedeAssignment(
  input: {
    repositories: Repositories;
    requestContext: ApiRequestContext;
    workflowInstance: WorkflowInstanceRecord;
    changeRequest: ChangeRequestRecord;
    transactionPlan: TransactionPlanRecord;
    idempotencyKey: string;
  },
  operation: AssignmentOperation,
): Result<WorkerAssignmentRecord, AppError> {
  if (operation.currentWorkerAssignmentId === undefined) {
    return err(validationFailedError({ currentWorkerAssignmentId: "missing" }));
  }

  const assignmentResult = input.repositories.workerAssignments.findById(
    operation.currentWorkerAssignmentId,
  );
  if (!assignmentResult.ok) {
    return assignmentResult;
  }

  const updatedAssignment =
    assignmentResult.value.status === "superseded"
      ? assignmentResult.value
      : {
          ...assignmentResult.value,
          status: "superseded" as const,
          effectiveEnd: operation.effectiveEnd ?? input.changeRequest.effectiveAt,
          sourceWorkflowInstanceId: input.workflowInstance.workflowInstanceId,
          metadata: {
            ...assignmentResult.value.metadata,
            supersededByWorkflowInstanceId: input.workflowInstance.workflowInstanceId,
            idempotencyKey: operation.idempotencyKey,
          },
        };

  const updateResult = input.repositories.workerAssignments.update(updatedAssignment);
  if (!updateResult.ok) {
    return updateResult;
  }

  const ledgerResult = appendWorkflowLedgerEvent(
    input.repositories,
    input.requestContext,
    {
      eventType: LEDGER_EVENT_TYPES.WORKER_ASSIGNMENT_SUPERSEDED,
      workflowInstance: input.workflowInstance,
      subjectType: "worker_assignment",
      subjectId: updateResult.value.workerAssignmentId,
      idempotencyKey: input.idempotencyKey,
      transactionPlanId: input.transactionPlan.transactionPlanId,
      payload: {
        operation,
        assignment: updateResult.value,
      },
    },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(updateResult.value);
}

function createTargetAssignment(
  input: {
    repositories: Repositories;
    requestContext: ApiRequestContext;
    workflowInstance: WorkflowInstanceRecord;
    changeRequest: ChangeRequestRecord;
    transactionPlan: TransactionPlanRecord;
    idempotencyKey: string;
  },
  operation: AssignmentOperation,
): Result<WorkerAssignmentRecord, AppError> {
  const existingAssignmentsResult =
    input.repositories.workerAssignments.findBySourceWorkflow(
      input.requestContext.tenantId,
      input.workflowInstance.workflowInstanceId,
    );
  if (!existingAssignmentsResult.ok) {
    return existingAssignmentsResult;
  }

  const existingAssignment = existingAssignmentsResult.value.find((assignment) => {
    return assignment.metadata["idempotencyKey"] === operation.idempotencyKey;
  });

  if (existingAssignment !== undefined) {
    return ok(existingAssignment);
  }

  if (operation.orgUnitId === undefined || operation.effectiveStart === undefined) {
    return err(
      validationFailedError({
        orgUnitId: operation.orgUnitId,
        effectiveStart: operation.effectiveStart,
      }),
    );
  }

  const timestamp = nowIso();
  const assignment: WorkerAssignmentRecord = {
    workerAssignmentId: makeId("wa"),
    tenantId: input.requestContext.tenantId,
    employeeId: input.changeRequest.targetWorkerId,
    orgUnitId: operation.orgUnitId,
    assignmentType: operation.assignmentType,
    ...(operation.roleType !== undefined && operation.roleType.trim() !== ""
      ? { roleType: operation.roleType }
      : {}),
    ...(operation.managerEmployeeId !== undefined &&
    operation.managerEmployeeId.trim() !== ""
      ? { managerEmployeeId: operation.managerEmployeeId }
      : {}),
    allocationPercent: operation.allocationPercent ?? 100,
    status: "active",
    effectiveStart: operation.effectiveStart,
    sourceWorkflowInstanceId: input.workflowInstance.workflowInstanceId,
    metadata: {
      ...(operation.metadata ?? {}),
      idempotencyKey: operation.idempotencyKey,
      sourceChangeRequestId: input.changeRequest.changeRequestId,
    },
    createdAt: timestamp,
    updatedAt: timestamp,
  };

  const createResult = input.repositories.workerAssignments.create(assignment);
  if (!createResult.ok) {
    return createResult;
  }

  const ledgerResult = appendWorkflowLedgerEvent(
    input.repositories,
    input.requestContext,
    {
      eventType: LEDGER_EVENT_TYPES.WORKER_ASSIGNMENT_CREATED,
      workflowInstance: input.workflowInstance,
      subjectType: "worker_assignment",
      subjectId: createResult.value.workerAssignmentId,
      idempotencyKey: input.idempotencyKey,
      transactionPlanId: input.transactionPlan.transactionPlanId,
      payload: {
        operation,
        assignment: createResult.value,
      },
    },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(createResult.value);
}

function applyRoleBindingOperations(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  transactionPlan: TransactionPlanRecord;
  operations: RoleBindingOperation[];
  idempotencyKey: string;
}): Result<RoleBindingRecord[], AppError> {
  const writtenRoleBindings: RoleBindingRecord[] = [];

  for (const operation of input.operations) {
    if (operation.operation !== "ensure_direct_reports_binding") {
      continue;
    }

    if (operation.actorEmployeeId === undefined) {
      return err(validationFailedError({ actorEmployeeId: "missing" }));
    }

    const actorResult = input.repositories.actors.findByLinkedWorkerId(
      input.requestContext.tenantId,
      operation.actorEmployeeId,
    );
    if (!actorResult.ok) {
      return actorResult;
    }

    const existingBindingResult =
      input.repositories.roleBindings.findActiveByActorRoleAndScope(
        input.requestContext.tenantId,
        actorResult.value.actorId,
        operation.roleKey ?? ACTOR_ROLES.MANAGER,
        operation.scopeType ?? "direct_reports",
      );
    if (!existingBindingResult.ok) {
      return existingBindingResult;
    }

    if (existingBindingResult.value !== undefined) {
      writtenRoleBindings.push(existingBindingResult.value);
      continue;
    }

    const timestamp = nowIso();
    const roleBinding: RoleBindingRecord = {
      roleBindingId: makeId("rb"),
      tenantId: input.requestContext.tenantId,
      actorId: actorResult.value.actorId,
      roleKey: operation.roleKey ?? ACTOR_ROLES.MANAGER,
      scopeType: "direct_reports",
      relationshipType: "solid_line_manager",
      status: "active",
      effectiveStart: operation.effectiveStart ?? input.changeRequest.effectiveAt,
      sourceWorkflowInstanceId: input.workflowInstance.workflowInstanceId,
      metadata: {
        ...(operation.metadata ?? {}),
        fieldGroups: ["profile", "organization", "job", "employment"],
        idempotencyKey: operation.idempotencyKey,
      },
      createdAt: timestamp,
      updatedAt: timestamp,
    };

    const createResult = input.repositories.roleBindings.create(roleBinding);
    if (!createResult.ok) {
      return createResult;
    }

    writtenRoleBindings.push(createResult.value);
  }

  const ledgerResult = appendWorkflowLedgerEvent(
    input.repositories,
    input.requestContext,
    {
      eventType: LEDGER_EVENT_TYPES.ROLE_BINDINGS_RECALCULATED,
      workflowInstance: input.workflowInstance,
      subjectType: "worker",
      subjectId: input.changeRequest.targetWorkerId,
      idempotencyKey: input.idempotencyKey,
      transactionPlanId: input.transactionPlan.transactionPlanId,
      payload: {
        operations: input.operations,
        roleBindings: writtenRoleBindings,
      },
    },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(writtenRoleBindings);
}

function applyOrgTransferProjection(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  transactionPlan: TransactionPlanRecord;
  planOutput: PlanTransactionOutput;
  idempotencyKey: string;
}): Result<Record<string, unknown>, AppError> {
  if (isFutureEffective(input.changeRequest.effectiveAt, nowIso())) {
    return ok({
      applied: false,
      reason: "future_effective",
      effectiveAt: input.changeRequest.effectiveAt,
    });
  }

  const projectionResult = input.repositories.employeeProjections.findByEmployeeId(
    input.requestContext.tenantId,
    input.changeRequest.targetWorkerId,
  );
  if (!projectionResult.ok) {
    return projectionResult;
  }

  const updatedDocumentResult = applyProjectionPatches({
    document: projectionResult.value.document,
    patches: input.planOutput.projectionPatches,
    allowedPatchPaths: input.workflowConfig.projection.allowedPatchPaths,
  });
  if (!updatedDocumentResult.ok) {
    return updatedDocumentResult;
  }

  const orgProjectionWrite = input.planOutput.internalWrites.find((write) => {
    return write.eventType === LEDGER_EVENT_TYPES.EMPLOYEE_ORG_PROJECTION_UPDATED;
  });
  const orgProjectionLedgerResult = appendWorkflowLedgerEvent(
    input.repositories,
    input.requestContext,
    {
      eventType: LEDGER_EVENT_TYPES.EMPLOYEE_ORG_PROJECTION_UPDATED,
      workflowInstance: input.workflowInstance,
      subjectType: "worker",
      subjectId: input.changeRequest.targetWorkerId,
      idempotencyKey: input.idempotencyKey,
      transactionPlanId: input.transactionPlan.transactionPlanId,
      payload: orgProjectionWrite?.payload ?? {
        projectionPatches: input.planOutput.projectionPatches,
      },
    },
  );
  if (!orgProjectionLedgerResult.ok) {
    return orgProjectionLedgerResult;
  }

  const compensationWrite = input.planOutput.internalWrites.find((write) => {
    return write.eventType === LEDGER_EVENT_TYPES.EMPLOYEE_COMPENSATION_UPDATED;
  });
  if (compensationWrite !== undefined) {
    const compensationLedgerResult = appendWorkflowLedgerEvent(
      input.repositories,
      input.requestContext,
      {
        eventType: LEDGER_EVENT_TYPES.EMPLOYEE_COMPENSATION_UPDATED,
        workflowInstance: input.workflowInstance,
        subjectType: "worker",
        subjectId: input.changeRequest.targetWorkerId,
        idempotencyKey: input.idempotencyKey,
        transactionPlanId: input.transactionPlan.transactionPlanId,
        payload: compensationWrite.payload,
      },
    );
    if (!compensationLedgerResult.ok) {
      return compensationLedgerResult;
    }
  }

  const updateProjectionResult = input.repositories.employeeProjections.updateDocument(
    input.requestContext.tenantId,
    input.changeRequest.targetWorkerId,
    updatedDocumentResult.value,
    orgProjectionLedgerResult.value.eventId,
    orgProjectionLedgerResult.value.eventSequence,
  );
  if (!updateProjectionResult.ok) {
    return updateProjectionResult;
  }

  return ok({
    applied: true,
    employeeId: updateProjectionResult.value.employeeId,
    projectionVersion: updateProjectionResult.value.projectionVersion,
  });
}

function createOrgTransferOutboxRows(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  changeRequest: ChangeRequestRecord;
  transactionPlan: TransactionPlanRecord;
  externalWrites: PlanTransactionOutput["externalCallRequests"];
}) {
  const outboxRows = [];

  for (const externalWrite of input.externalWrites) {
    const existingOutboxResult =
      input.repositories.integrationOutbox.findByIdempotencyKey(
        input.requestContext.tenantId,
        externalWrite.idempotencyKey,
      );
    if (!existingOutboxResult.ok) {
      return existingOutboxResult;
    }

    if (existingOutboxResult.value !== undefined) {
      outboxRows.push(existingOutboxResult.value);
      continue;
    }

    const timestamp = nowIso();
    const outboxResult = input.repositories.integrationOutbox.create({
      outboxId: makeId("outbox"),
      tenantId: input.requestContext.tenantId,
      changeRequestId: input.changeRequest.changeRequestId,
      transactionPlanId: input.transactionPlan.transactionPlanId,
      destination: externalWrite.connectionId,
      operation: externalWrite.operation,
      requestPayload: externalWrite.payload,
      status: INTEGRATION_OUTBOX_STATUSES.PENDING,
      attemptCount: 0,
      maxAttempts: 3,
      idempotencyKey: externalWrite.idempotencyKey,
      createdAt: timestamp,
      updatedAt: timestamp,
    });
    if (!outboxResult.ok) {
      return outboxResult;
    }

    outboxRows.push(outboxResult.value);
  }

  return ok(outboxRows);
}

function actorCanApproveTargetCostCenter(input: {
  repositories: Repositories;
  actor: ActorRecord;
  tenantId: string;
  targetCostCenter: OrganizationUnitRecord;
}): Result<true, AppError> {
  const bindingsResult = input.repositories.roleBindings.findActiveForActor(
    input.tenantId,
    input.actor.actorId,
  );
  if (!bindingsResult.ok) {
    return bindingsResult;
  }

  const canApprove = bindingsResult.value.some((binding) => {
    return (
      binding.scopeType === "global" ||
      (binding.scopeType === "cost_center" &&
        (binding.scopeOrgUnitId === input.targetCostCenter.orgUnitId ||
          binding.scopeValue === input.targetCostCenter.name))
    );
  });

  if (canApprove) {
    return ok(true);
  }

  return err(
    permissionDeniedError({
      actorId: input.actor.actorId,
      targetCostCenterOrgUnitId: input.targetCostCenter.orgUnitId,
      requiredScope: "cost_center",
    }),
  );
}

function buildProposedOrganization(input: {
  currentOrganization: OrganizationInfo;
  targetLocation: OrganizationUnitRecord;
  targetTeam: OrganizationUnitRecord;
  targetCostCenter: OrganizationUnitRecord;
}): OrganizationInfo {
  const locationPayZone = stringField(input.targetLocation.metadata, "payZone");

  return {
    ...input.currentOrganization,
    businessUnit:
      stringField(input.targetTeam.metadata, "businessUnit") ??
      input.currentOrganization.businessUnit,
    department:
      stringField(input.targetTeam.metadata, "department") ??
      input.currentOrganization.department,
    team: input.targetTeam.name,
    location: input.targetLocation.name,
    payZone: locationPayZone ?? input.currentOrganization.payZone,
    costCenter: input.targetCostCenter.name,
  };
}

function parseOptionalJobInfo(
  value: Record<string, unknown> | undefined,
): Result<JobInfo | undefined, AppError> {
  if (value === undefined) {
    return ok(undefined);
  }

  const jobCode = stringField(value, "jobCode");
  const title = stringField(value, "title");
  const family = stringField(value, "family");
  const level = stringField(value, "level");

  if (
    jobCode === undefined ||
    title === undefined ||
    family === undefined ||
    level === undefined
  ) {
    return err(validationFailedError({ jobCode, title, family, level }));
  }

  return ok({ jobCode, title, family, level });
}

function parseOptionalCompensationInfo(
  value: Record<string, unknown> | undefined,
): Result<CompensationInfo | undefined, AppError> {
  if (value === undefined) {
    return ok(undefined);
  }

  const amount = numberField(value, "amount");
  const currency = stringField(value, "currency");
  const payFrequency = stringField(value, "payFrequency");
  const bonusTargetPercent = numberField(value, "bonusTargetPercent");
  const effectiveDate = stringField(value, "effectiveDate");

  if (
    amount === undefined ||
    currency === undefined ||
    payFrequency === undefined ||
    bonusTargetPercent === undefined ||
    effectiveDate === undefined
  ) {
    return err(
      validationFailedError({
        amount,
        currency,
        payFrequency,
        bonusTargetPercent,
        effectiveDate,
      }),
    );
  }

  return ok({
    amount,
    currency,
    payFrequency,
    bonusTargetPercent,
    effectiveDate,
  });
}

function parseApprovalDecisionInput(
  input: Record<string, unknown>,
): Result<{ approvalTaskId: string; comment?: string }, AppError> {
  const approvalTaskId = stringField(input, "approvalTaskId");
  const comment = stringField(input, "comment");

  if (approvalTaskId === undefined) {
    return err(validationFailedError({ approvalTaskId }));
  }

  return ok({
    approvalTaskId,
    ...(comment !== undefined ? { comment } : {}),
  });
}

function verifyApprovalTask(
  pendingApprovalTask: ApprovalTaskRecord | undefined,
  approvalTaskId: string,
): Result<ApprovalTaskRecord, AppError> {
  if (
    pendingApprovalTask === undefined ||
    pendingApprovalTask.approvalTaskId !== approvalTaskId
  ) {
    return err(
      invalidWorkflowTransitionError({
        approvalTaskId,
        reason: "No pending approval task is available for this workflow.",
      }),
    );
  }

  return ok(pendingApprovalTask);
}

function approvalStageFromTask(
  approvalTask: ApprovalTaskRecord,
): Result<ApprovalStage, AppError> {
  const stageKey = stringField(approvalTask.metadata, "stageKey");
  const stage = approvalStages.find((candidate) => candidate.key === stageKey);

  if (stage === undefined) {
    return err(validationFailedError({ stageKey }));
  }

  return ok(stage);
}

function nextApprovalStage(stage: ApprovalStage): ApprovalStage | undefined {
  const currentIndex = approvalStages.findIndex((candidate) => {
    return candidate.key === stage.key;
  });

  return approvalStages[currentIndex + 1];
}

function orgTransferInputFromWorkflow(
  workflowInstance: WorkflowInstanceRecord,
): Result<OrgTransferInput, AppError> {
  const orgTransferContext = objectField(workflowInstance.context, "orgTransfer");
  const inputRecord =
    orgTransferContext === undefined
      ? undefined
      : objectField(orgTransferContext, "input");

  if (inputRecord === undefined) {
    return err(validationFailedError({ orgTransferInput: "missing" }));
  }

  return parseOrgTransferInput(inputRecord);
}

function findChangeRequestForWorkflow(
  repositories: Repositories,
  workflowInstance: WorkflowInstanceRecord,
): Result<ChangeRequestRecord, AppError> {
  if (workflowInstance.changeRequestId === undefined) {
    return err(validationFailedError({ changeRequestId: "missing" }));
  }

  return repositories.changeRequests.findById(workflowInstance.changeRequestId);
}

function applyProjectionPatches(input: {
  document: EmployeeProjectionDocument;
  patches: PlanTransactionOutput["projectionPatches"];
  allowedPatchPaths: string[];
}): Result<EmployeeProjectionDocument, AppError> {
  const updatedDocument = cloneJsonValue(input.document);

  for (const patch of input.patches) {
    if (
      patch.projection !== "employee" ||
      patch.operation !== "replace" ||
      !input.allowedPatchPaths.includes(patch.path)
    ) {
      return err(
        validationFailedError({
          projection: patch.projection,
          operation: patch.operation,
          path: patch.path,
          allowedPatchPaths: input.allowedPatchPaths,
        }),
      );
    }

    const setResult = setJsonPointerValue(updatedDocument, patch.path, patch.value);
    if (!setResult.ok) {
      return setResult;
    }
  }

  return ok(updatedDocument);
}

function setJsonPointerValue(
  document: EmployeeProjectionDocument,
  path: string,
  value: unknown,
): Result<true, AppError> {
  const pathSegments = path
    .slice(1)
    .split("/")
    .filter((segment) => segment.length > 0)
    .map((segment) => segment.replace(/~1/g, "/").replace(/~0/g, "~"));
  const targetKey = pathSegments.at(-1);

  if (!path.startsWith("/") || targetKey === undefined) {
    return err(validationFailedError({ path }));
  }

  let parentValue: unknown = document;
  for (const pathSegment of pathSegments.slice(0, -1)) {
    if (typeof parentValue !== "object" || parentValue === null) {
      return err(validationFailedError({ path, pathSegment }));
    }

    parentValue = (parentValue as Record<string, unknown>)[pathSegment];
  }

  if (typeof parentValue !== "object" || parentValue === null) {
    return err(validationFailedError({ path, targetKey }));
  }

  (parentValue as Record<string, unknown>)[targetKey] = cloneJsonValue(value);

  return ok(true);
}

function orgUnitForBlock(orgUnit: OrganizationUnitRecord): Record<string, unknown> {
  return {
    orgUnitId: orgUnit.orgUnitId,
    type: orgUnit.type,
    name: orgUnit.name,
    status: orgUnit.status,
    metadata: orgUnit.metadata,
  };
}

function workerAssignmentForBlock(
  assignment: WorkerAssignmentRecord,
): Record<string, unknown> {
  return {
    workerAssignmentId: assignment.workerAssignmentId,
    employeeId: assignment.employeeId,
    orgUnitId: assignment.orgUnitId,
    assignmentType: assignment.assignmentType,
    ...(assignment.roleType !== undefined ? { roleType: assignment.roleType } : {}),
    ...(assignment.managerEmployeeId !== undefined
      ? { managerEmployeeId: assignment.managerEmployeeId }
      : {}),
    allocationPercent: assignment.allocationPercent,
    status: assignment.status,
    effectiveStart: assignment.effectiveStart,
    ...(assignment.effectiveEnd !== undefined
      ? { effectiveEnd: assignment.effectiveEnd }
      : {}),
    metadata: assignment.metadata,
  };
}

function emptyJobInfo(): JobInfo {
  return {
    jobCode: "",
    title: "",
    family: "",
    level: "",
  };
}

function emptyCompensationInfo(): CompensationInfo {
  return {
    amount: 0,
    currency: "",
    payFrequency: "",
    bonusTargetPercent: 0,
    effectiveDate: "",
  };
}

function appendTransitionLedgerEvents(
  repositories: Repositories,
  requestContext: ApiRequestContext,
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

function appendAdditionalWorkflowEvents(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  idempotencyKey: string,
  events: Array<{
    eventType: string;
    approvalTaskId?: string;
    transactionPlanId?: string;
    payload: Record<string, unknown>;
  }>,
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

function appendWorkflowLedgerEvent(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  input: {
    eventType: string;
    workflowInstance: WorkflowInstanceRecord;
    subjectType: string;
    subjectId: string;
    idempotencyKey?: string;
    approvalTaskId?: string;
    transactionPlanId?: string;
    payload: Record<string, unknown>;
  },
): Result<LedgerEventRecord, AppError> {
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
    permissionSnapshot: createPermissionSnapshot(requestContext.actor),
    aiVisibilitySnapshot: {},
    payload: input.payload,
    ...(requestContext.actor.roles[0] !== undefined
      ? { actorRole: requestContext.actor.roles[0] }
      : {}),
  });
}

function isFutureEffective(effectiveAt: string, now: string): boolean {
  return dateKey(effectiveAt) > dateKey(now);
}

function dateKey(value: string): string {
  return value.slice(0, 10);
}

function serializeWorkflowInstance(
  workflowInstance: WorkflowInstanceRecord,
): Record<string, unknown> {
  return {
    workflowInstanceId: workflowInstance.workflowInstanceId,
    intent: workflowInstance.intent,
    subjectType: workflowInstance.subjectType,
    subjectId: workflowInstance.subjectId,
    state: workflowInstance.state,
    status: workflowInstance.status,
    version: workflowInstance.version,
    changeRequestId: workflowInstance.changeRequestId,
    currentInteraction: workflowInstance.currentInteraction,
  };
}

function terminalInteraction(status: string): Record<string, unknown> {
  return {
    type: "terminal",
    status,
  };
}

function stringField(
  object: Record<string, unknown>,
  fieldName: string,
): string | undefined {
  const value = object[fieldName];

  if (typeof value !== "string") {
    return undefined;
  }

  const trimmedValue = value.trim();

  return trimmedValue.length === 0 ? undefined : trimmedValue;
}

function numberField(
  object: Record<string, unknown>,
  fieldName: string,
): number | undefined {
  const value = object[fieldName];

  return typeof value === "number" ? value : undefined;
}

function booleanField(
  object: Record<string, unknown>,
  fieldName: string,
): boolean | undefined {
  const value = object[fieldName];

  return typeof value === "boolean" ? value : undefined;
}

function objectField(
  object: Record<string, unknown>,
  fieldName: string,
): Record<string, unknown> | undefined {
  const value = object[fieldName];

  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined;
  }

  return value as Record<string, unknown>;
}

function cloneJsonValue<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}
