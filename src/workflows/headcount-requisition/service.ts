import {
  ACTOR_ROLES,
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
  versionConflictError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  createInitialWorkflowInstance,
  DEMO_IDS,
  makeId,
  nowIso,
  type ActorRecord,
  type ApprovalGroupRecord,
  type ApprovalTaskRecord,
  type ChangeRequestRecord,
  type IntegrationOutboxRecord,
  type ProposedChangeRecord,
  type Repositories,
  type TransactionPlanRecord,
  type WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import {
  buildConfiguredInteraction,
  getWorkflowConfigByIntent,
  type WorkflowApprovalGateApproverResolverConfig,
  type WorkflowApprovalGateConfig,
  type WorkflowConfig,
} from "../shared/workflow-config.js";
import {
  evaluateApprovalGate,
  type ApprovalGateFailurePolicy,
  type ApprovalGatePassRule,
  type ApprovalGateTaskDecision,
} from "../shared/approval-gate.js";
import { numberField, objectField, stringField } from "../shared/json-fields.js";
import {
  serializeWorkflowInstance,
  terminalInteraction,
} from "../shared/workflow-response.js";
import {
  appendAdditionalWorkflowEvents,
  appendTransitionLedgerEvents,
  appendWorkflowLedgerEvent,
} from "../shared/workflow-ledger-events.js";
import {
  parseHeadcountApprovalDecisionInput,
  parseHeadcountInput,
} from "./input-parsers.js";
import type {
  HeadcountApprovalDecisionInput,
  HeadcountInput,
  HeadcountTransitionBody,
  HeadcountTransitionInput,
  PlanTransactionOutput,
  PreflightOutput,
  ResolvedHeadcountApprover,
} from "./types.js";

const headcountIntent = WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL;
const leadershipGateId = "leadership_chain_gate";
const crossFunctionalGateId = "cross_functional_gate";

const preferredDemoActorByRole = new Map<string, string>([
  [ACTOR_ROLES.FINANCE_ADMIN, DEMO_IDS.financeAdminActorId],
  ["hrbp", DEMO_IDS.secondAdminActorId],
  [ACTOR_ROLES.COMPENSATION_ADMIN, DEMO_IDS.compensationAdminActorId],
  ["medical_director", DEMO_IDS.medicalDirectorActorId],
  ["clinic_ops_admin", DEMO_IDS.clinicOpsActorId],
]);

/**
 * Identifies the dynamic headcount approval workflow.
 */
export function isHeadcountRequisitionWorkflowIntent(intent: string): boolean {
  return intent === headcountIntent;
}

/**
 * Checks exact task ownership for headcount approval gates.
 */
export function canPerformHeadcountApproval(input: {
  actor: ActorRecord;
  pendingApprovalTask?: ApprovalTaskRecord;
}): boolean {
  const pendingTask = input.pendingApprovalTask;

  if (
    pendingTask === undefined ||
    pendingTask.status !== APPROVAL_TASK_STATUSES.PENDING
  ) {
    return false;
  }

  const assignmentMode = pendingTask.metadata["assignmentMode"];

  return (
    pendingTask.assigneeActorId === input.actor.actorId ||
    (assignmentMode !== "actor" && input.actor.roles.includes(pendingTask.assigneeRole))
  );
}

/**
 * Starts the headcount workflow with position subject context only.
 */
export function startHeadcountRequisitionWorkflow(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const startInputResult = parseHeadcountStartInput(body);
  if (!startInputResult.ok) {
    return startInputResult;
  }

  const permissionResult = canStartHeadcountWorkflow(requestContext.actor);
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const workflowConfigResult = getWorkflowConfigByIntent(headcountIntent);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const workflowVersionResult =
    dependencies.repositories.workflows.findVersionByIntent(headcountIntent);
  if (!workflowVersionResult.ok) {
    return workflowVersionResult;
  }

  const interactionResult = buildConfiguredInteraction({
    workflowConfig: workflowConfigResult.value,
    interactionKey: "input",
  });
  if (!interactionResult.ok) {
    return interactionResult;
  }

  const workflowInstance = createInitialWorkflowInstance({
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
    workflowDefinitionId: workflowVersionResult.value.workflowDefinitionId,
    workflowVersionId: workflowVersionResult.value.workflowVersionId,
    intent: headcountIntent,
    subjectType: "position",
    subjectId: startInputResult.value.subjectId,
    requesterActorId: requestContext.actor.actorId,
    currentInteraction: interactionResult.value,
    context: {},
    correlationId: requestContext.correlationId,
    metadata: { runtime: "headcount_requisition" },
  });

  const createResult =
    dependencies.repositories.workflows.createInstance(workflowInstance);
  if (!createResult.ok) {
    return createResult;
  }

  const ledgerResult = appendWorkflowLedgerEvent(
    dependencies.repositories,
    requestContext,
    {
      eventType: LEDGER_EVENT_TYPES.WORKFLOW_INTENT_STARTED,
      workflowInstance: createResult.value,
      subjectType: createResult.value.subjectType,
      subjectId: createResult.value.subjectId,
      payload: {
        intent: headcountIntent,
        currentInteraction: createResult.value.currentInteraction,
      },
    },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(createResult.value));
}

/**
 * Returns headcount actions from all currently assigned pending tasks.
 */
export function getHeadcountAvailableActions(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
): Result<Record<string, unknown>, AppError> {
  const pendingTasksResult = dependencies.repositories.approvals.findPendingForActor(
    requestContext.actor,
  );
  if (!pendingTasksResult.ok) {
    return pendingTasksResult;
  }

  const assignedTasks = pendingTasksResult.value.filter((task) => {
    return (
      task.workflowInstanceId === workflowInstance.workflowInstanceId &&
      task.assigneeActorId === requestContext.actor.actorId
    );
  });

  return ok({
    workflowInstanceId: workflowInstance.workflowInstanceId,
    version: workflowInstance.version,
    state: workflowInstance.state,
    actions: headcountAvailableActions({
      actor: requestContext.actor,
      workflowInstance,
      assignedTasks,
    }),
  });
}

/**
 * Dispatches headcount transitions while using task versions for gate decisions.
 */
export async function executeHeadcountRequisitionTransition(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: HeadcountTransitionBody;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  const workflowConfigResult = getWorkflowConfigByIntent(headcountIntent);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const transitionInput: HeadcountTransitionInput = {
    workflowConfig: workflowConfigResult.value,
    workflowInstance: input.workflowInstance,
    transitionBody: input.transitionBody,
  };

  const versionResult = verifyHeadcountWorkflowVersion(
    input.workflowInstance,
    input.transitionBody,
  );
  if (!versionResult.ok) {
    return versionResult;
  }

  if (isHeadcountApprovalDecisionTransition(input.transitionBody.transition)) {
    return decideHeadcountApprovalGateTask(
      dependencies,
      requestContext,
      transitionInput,
    );
  }

  if (
    input.transitionBody.transition === "submit_input" &&
    (input.workflowInstance.state === WORKFLOW_STATES.COLLECTING_INPUT ||
      input.workflowInstance.state === WORKFLOW_STATES.WAITING_APPROVAL_REPAIR)
  ) {
    if (requestContext.actor.actorId !== input.workflowInstance.requesterActorId) {
      return err(
        permissionDeniedError({ transition: input.transitionBody.transition }),
      );
    }

    return submitHeadcountRequisitionInput(
      dependencies,
      requestContext,
      transitionInput,
    );
  }

  if (
    input.transitionBody.transition === "execute" &&
    input.workflowInstance.state === WORKFLOW_STATES.APPROVED
  ) {
    if (
      !requestContext.actor.roles.includes(ACTOR_ROLES.HR_ADMIN) &&
      !requestContext.actor.roles.includes(ACTOR_ROLES.SYSTEM)
    ) {
      return err(
        permissionDeniedError({ transition: input.transitionBody.transition }),
      );
    }

    return executeApprovedHeadcountRequisition(
      dependencies,
      requestContext,
      transitionInput,
    );
  }

  return err(
    invalidWorkflowTransitionError({
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      state: input.workflowInstance.state,
      transition: input.transitionBody.transition,
    }),
  );
}

type HeadcountStartInput = {
  subjectId: string;
};

function parseHeadcountStartInput(
  body: Record<string, unknown>,
): Result<HeadcountStartInput, AppError> {
  const subject = objectField(body, "subject");
  const intent = stringField(body, "intent");
  const subjectType =
    stringField(body, "subjectType") ??
    (subject === undefined ? undefined : stringField(subject, "type")) ??
    "position";
  const subjectId =
    stringField(body, "subjectId") ??
    (subject === undefined ? undefined : stringField(subject, "id")) ??
    makeId("position");

  if (intent !== headcountIntent || subjectType !== "position") {
    return err(
      validationFailedError({
        intent,
        subjectType,
        expectedIntent: headcountIntent,
        expectedSubjectType: "position",
      }),
    );
  }

  return ok({ subjectId });
}

function canStartHeadcountWorkflow(actor: ActorRecord): Result<true, AppError> {
  if (
    actor.roles.includes(ACTOR_ROLES.HR_ADMIN) ||
    actor.roles.includes("clinic_ops_admin")
  ) {
    return ok(true);
  }

  return err(permissionDeniedError({ permission: "position.headcount.request" }));
}

function headcountAvailableActions(input: {
  actor: ActorRecord;
  workflowInstance: WorkflowInstanceRecord;
  assignedTasks: ApprovalTaskRecord[];
}): Array<Record<string, unknown>> {
  if (
    (input.workflowInstance.state === WORKFLOW_STATES.COLLECTING_INPUT ||
      input.workflowInstance.state === WORKFLOW_STATES.WAITING_APPROVAL_REPAIR) &&
    input.actor.actorId === input.workflowInstance.requesterActorId
  ) {
    return [
      {
        transition: "submit_input",
        label: "Submit headcount requisition",
        enabled: true,
      },
      {
        transition: "cancel",
        label: "Cancel",
        enabled: true,
      },
    ];
  }

  if (
    (input.workflowInstance.state === WORKFLOW_STATES.WAITING_SYNC_APPROVAL ||
      input.workflowInstance.state === WORKFLOW_STATES.WAITING_ASYNC_APPROVAL) &&
    input.assignedTasks.length > 0
  ) {
    return input.assignedTasks.flatMap((task) => {
      const taskVersion = taskVersionForApprovalTask(task);

      return [
        {
          transition: "approve",
          label: "Approve",
          enabled: true,
          taskId: task.approvalTaskId,
          taskVersion,
        },
        {
          transition: "reject",
          label: "Reject",
          enabled: true,
          taskId: task.approvalTaskId,
          taskVersion,
        },
        {
          transition: "request_more_info",
          label: "Request more information",
          enabled: true,
          taskId: task.approvalTaskId,
          taskVersion,
        },
      ];
    });
  }

  if (
    input.workflowInstance.state === WORKFLOW_STATES.APPROVED &&
    (input.actor.roles.includes(ACTOR_ROLES.HR_ADMIN) ||
      input.actor.roles.includes(ACTOR_ROLES.SYSTEM))
  ) {
    return [
      {
        transition: "execute",
        label: "Execute headcount requisition",
        enabled: true,
      },
    ];
  }

  return [];
}

function verifyHeadcountWorkflowVersion(
  workflowInstance: WorkflowInstanceRecord,
  transitionBody: HeadcountTransitionBody,
): Result<true, AppError> {
  if (workflowInstance.version !== transitionBody.expectedVersion) {
    return err(
      versionConflictError({
        workflowInstanceId: workflowInstance.workflowInstanceId,
        expectedVersion: transitionBody.expectedVersion,
        actualVersion: workflowInstance.version,
      }),
    );
  }

  return ok(true);
}

function isHeadcountApprovalDecisionTransition(transition: string): boolean {
  return (
    transition === "approve" ||
    transition === "reject" ||
    transition === "request_more_info"
  );
}

/**
 * Handles headcount intake and opens the user-defined sequential leadership gate.
 */
export async function submitHeadcountRequisitionInput(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: HeadcountTransitionInput,
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const headcountInputResult = parseHeadcountInput(input.transitionBody.input);
  if (!headcountInputResult.ok) {
    return headcountInputResult;
  }

  const preflightResult =
    await dependencies.executorClient.executeBlock<PreflightOutput>({
      tenantId: requestContext.tenantId,
      environmentId: requestContext.environmentId,
      changeRequestId: "",
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      block: input.workflowConfig.submit.preflightBlock,
      input: headcountInputResult.value as unknown as Record<string, unknown>,
      context: {
        actorId: requestContext.actor.actorId,
        effectiveAt: headcountInputResult.value.targetStartDate,
        permissions: createPermissionSnapshot(requestContext.actor),
        correlationId: requestContext.correlationId,
        idempotencyKey: input.transitionBody.idempotencyKey,
      },
    });
  if (!preflightResult.ok) {
    return preflightResult;
  }
  if (!preflightResult.value.output?.valid) {
    return err(validationFailedError({ preflight: preflightResult.value.output }));
  }

  const timestamp = nowIso();
  const changeRequest = createHeadcountChangeRequest({
    requestContext,
    workflowInstance: input.workflowInstance,
    headcountInput: headcountInputResult.value,
    preflight: preflightResult.value.output,
    timestamp,
  });
  const changeRequestResult = repositories.changeRequests.create(changeRequest);
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const proposedChange = createHeadcountProposedChange({
    requestContext,
    workflowInstance: input.workflowInstance,
    changeRequest,
    headcountInput: headcountInputResult.value,
    timestamp,
  });
  const proposedChangesResult = repositories.proposedChanges.createMany([
    proposedChange,
  ]);
  if (!proposedChangesResult.ok) {
    return proposedChangesResult;
  }

  const leadershipGateResult = approvalGateConfigById(
    input.workflowConfig,
    leadershipGateId,
  );
  if (!leadershipGateResult.ok) {
    return leadershipGateResult;
  }

  const leadershipApproversResult = resolveLeadershipApprovers({
    repositories,
    requestContext,
    gateConfig: leadershipGateResult.value,
    selectedActorIds: headcountInputResult.value.selectedLeadershipApprovers,
  });
  if (!leadershipApproversResult.ok) {
    return leadershipApproversResult;
  }

  const approvalGroupResult = createApprovalGroupForGate({
    repositories,
    requestContext,
    workflowInstance: input.workflowInstance,
    changeRequest,
    gateConfig: leadershipGateResult.value,
    approvers: leadershipApproversResult.value,
  });
  if (!approvalGroupResult.ok) {
    return approvalGroupResult;
  }

  const firstTaskResult = createApprovalTasksForSequence({
    repositories,
    requestContext,
    workflowInstance: input.workflowInstance,
    changeRequest,
    approvalGroup: approvalGroupResult.value,
    gateConfig: leadershipGateResult.value,
    approvers: leadershipApproversResult.value,
    sequenceIndex: 0,
  });
  if (!firstTaskResult.ok) {
    return firstTaskResult;
  }

  const nextInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: leadershipGateResult.value.interaction,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const updatedWorkflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.WAITING_SYNC_APPROVAL,
    status: WORKFLOW_STATUSES.WAITING,
    changeRequestId: changeRequest.changeRequestId,
    currentInteraction: withGateProgress(nextInteractionResult.value, {
      gateId: leadershipGateResult.value.gateId,
      mode: leadershipGateResult.value.mode,
      approvedCount: 0,
      requiredApprovals: leadershipApproversResult.value.length,
      pendingCount: firstTaskResult.value.length,
    }),
    context: {
      ...input.workflowInstance.context,
      currentPosition: null,
      submitInput: headcountInputResult.value,
      preflight: preflightResult.value.output,
      activeApprovalGroupId: approvalGroupResult.value.approvalGroupId,
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
        proposedChange,
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
        payload: { proposedChanges: proposedChangesResult.value },
      },
      {
        eventType: LEDGER_EVENT_TYPES.HEADCOUNT_REQUISITION_SUBMITTED,
        payload: {
          changeRequestId: changeRequest.changeRequestId,
          request: headcountInputResult.value,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_OPENED,
        payload: {
          approvalGroup: approvalGroupResult.value,
          gateId: leadershipGateResult.value.gateId,
          approverCount: leadershipApproversResult.value.length,
        },
      },
      ...firstTaskResult.value.map((approvalTask) => {
        return {
          eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CREATED,
          approvalTaskId: approvalTask.approvalTaskId,
          payload: { approvalTask },
        };
      }),
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(updatedWorkflowResult.value));
}

/**
 * Applies approve/reject/more-info decisions to active headcount approval gates.
 */
export async function decideHeadcountApprovalGateTask(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: HeadcountTransitionInput,
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const decisionInputResult = parseHeadcountApprovalDecisionInput(
    input.transitionBody.input,
  );
  if (!decisionInputResult.ok) {
    return decisionInputResult;
  }

  const approvalTaskResult = verifyHeadcountApprovalTask({
    repositories,
    actor: requestContext.actor,
    workflowInstance: input.workflowInstance,
    decisionInput: decisionInputResult.value,
  });
  if (!approvalTaskResult.ok) {
    return approvalTaskResult;
  }

  const approvalGroupResult = approvalGroupForTask(
    repositories,
    approvalTaskResult.value,
  );
  if (!approvalGroupResult.ok) {
    return approvalGroupResult;
  }

  const changeRequestResult = findChangeRequestForWorkflow(
    repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const decidedTaskResult = repositories.approvals.update(
    decidedApprovalTask({
      approvalTask: approvalTaskResult.value,
      transition: input.transitionBody.transition,
      decisionInput: decisionInputResult.value,
    }),
  );
  if (!decidedTaskResult.ok) {
    return decidedTaskResult;
  }

  const gateDecisionResult = evaluateApprovalGroup({
    repositories,
    approvalGroup: approvalGroupResult.value,
    decidedTask: decidedTaskResult.value,
  });
  if (!gateDecisionResult.ok) {
    return gateDecisionResult;
  }

  if (
    gateDecisionResult.value.outcome === "advance_sequence" &&
    approvalGroupResult.value.gateNodeId === leadershipGateId
  ) {
    return advanceLeadershipGate({
      repositories,
      requestContext,
      workflowConfig: input.workflowConfig,
      workflowInstance: input.workflowInstance,
      changeRequest: changeRequestResult.value,
      approvalGroup: approvalGroupResult.value,
      decidedTask: decidedTaskResult.value,
      nextSequenceIndexes: gateDecisionResult.value.openSequenceIndexes,
      idempotencyKey: input.transitionBody.idempotencyKey,
    });
  }

  if (
    gateDecisionResult.value.outcome === "passed" &&
    approvalGroupResult.value.gateNodeId === leadershipGateId
  ) {
    return passLeadershipGateAndOpenAsyncGate({
      repositories,
      requestContext,
      workflowConfig: input.workflowConfig,
      workflowInstance: input.workflowInstance,
      changeRequest: changeRequestResult.value,
      approvalGroup: approvalGroupResult.value,
      decidedTask: decidedTaskResult.value,
      idempotencyKey: input.transitionBody.idempotencyKey,
    });
  }

  if (
    gateDecisionResult.value.outcome === "passed" &&
    approvalGroupResult.value.gateNodeId === crossFunctionalGateId
  ) {
    return passAsyncGate({
      repositories,
      requestContext,
      workflowConfig: input.workflowConfig,
      workflowInstance: input.workflowInstance,
      changeRequest: changeRequestResult.value,
      approvalGroup: approvalGroupResult.value,
      decidedTask: decidedTaskResult.value,
      cancelPendingTaskIds: gateDecisionResult.value.cancelPendingTaskIds,
      idempotencyKey: input.transitionBody.idempotencyKey,
    });
  }

  if (gateDecisionResult.value.outcome === "failed") {
    return failApprovalGate({
      repositories,
      requestContext,
      workflowInstance: input.workflowInstance,
      changeRequest: changeRequestResult.value,
      approvalGroup: approvalGroupResult.value,
      decidedTask: decidedTaskResult.value,
      cancelPendingTaskIds: gateDecisionResult.value.cancelPendingTaskIds,
      idempotencyKey: input.transitionBody.idempotencyKey,
    });
  }

  if (gateDecisionResult.value.outcome === "repair") {
    return repairApprovalGate({
      repositories,
      requestContext,
      workflowConfig: input.workflowConfig,
      workflowInstance: input.workflowInstance,
      changeRequest: changeRequestResult.value,
      approvalGroup: approvalGroupResult.value,
      decidedTask: decidedTaskResult.value,
      cancelPendingTaskIds: gateDecisionResult.value.cancelPendingTaskIds,
      idempotencyKey: input.transitionBody.idempotencyKey,
    });
  }

  return recordWaitingGateDecision({
    repositories,
    requestContext,
    workflowInstance: input.workflowInstance,
    approvalGroup: approvalGroupResult.value,
    decidedTask: decidedTaskResult.value,
    idempotencyKey: input.transitionBody.idempotencyKey,
  });
}

/**
 * Executes an approved headcount requisition and records the resulting audit trail.
 */
export async function executeApprovedHeadcountRequisition(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: HeadcountTransitionInput,
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

  const planOutputResult =
    await dependencies.executorClient.executeBlock<PlanTransactionOutput>({
      tenantId: requestContext.tenantId,
      environmentId: requestContext.environmentId,
      changeRequestId: changeRequestResult.value.changeRequestId,
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      block: input.workflowConfig.plan.block,
      input: {
        changeRequestId: changeRequestResult.value.changeRequestId,
        positionId: input.workflowInstance.subjectId,
        submittedRequisition: input.workflowInstance.context["submitInput"],
        targetStartDate: changeRequestResult.value.effectiveAt,
      },
      context: {
        actorId: requestContext.actor.actorId,
        effectiveAt: changeRequestResult.value.effectiveAt,
        permissions: createPermissionSnapshot(requestContext.actor),
        correlationId: requestContext.correlationId,
        idempotencyKey: input.transitionBody.idempotencyKey,
      },
    });
  if (!planOutputResult.ok) {
    return planOutputResult;
  }
  if (planOutputResult.value.output === undefined) {
    return err(validationFailedError({ planOutput: "missing" }));
  }

  const transactionPlan = createHeadcountTransactionPlan({
    tenantId: requestContext.tenantId,
    changeRequestId: changeRequestResult.value.changeRequestId,
    actorId: requestContext.actor.actorId,
    output: planOutputResult.value.output,
  });
  const transactionPlanResult = repositories.transactionPlans.create(transactionPlan);
  if (!transactionPlanResult.ok) {
    return transactionPlanResult;
  }

  const outboxRowsResult = createHeadcountOutboxRows({
    repositories,
    requestContext,
    changeRequest: changeRequestResult.value,
    transactionPlan,
    externalWrites: planOutputResult.value.output.externalCallRequests,
  });
  if (!outboxRowsResult.ok) {
    return outboxRowsResult;
  }

  const updatedChangeRequestResult = repositories.changeRequests.update({
    ...changeRequestResult.value,
    status: CHANGE_REQUEST_STATUSES.EXECUTED,
    transactionPlanId: transactionPlan.transactionPlanId,
    executedAt: nowIso(),
    closedAt: nowIso(),
    updatedBy: requestContext.actor.actorId,
    version: changeRequestResult.value.version + 1,
  });
  if (!updatedChangeRequestResult.ok) {
    return updatedChangeRequestResult;
  }

  const executedPlanResult = repositories.transactionPlans.update({
    ...transactionPlan,
    status: "executed",
    executionResult: {
      outboxRows: outboxRowsResult.value,
    },
    updatedBy: requestContext.actor.actorId,
  });
  if (!executedPlanResult.ok) {
    return executedPlanResult;
  }

  const completedInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: "completedSummary",
  });
  if (!completedInteractionResult.ok) {
    return completedInteractionResult;
  }

  const updatedWorkflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.EXECUTED,
    status: WORKFLOW_STATUSES.COMPLETED,
    completedAt: nowIso(),
    currentInteraction: completedInteractionResult.value,
    context: {
      ...input.workflowInstance.context,
      transactionPlanId: transactionPlan.transactionPlanId,
      executionResult: {
        outboxRows: outboxRowsResult.value,
      },
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
      eventType: LEDGER_EVENT_TYPES.HEADCOUNT_REQUISITION_EXECUTED,
      idempotencyKey: input.transitionBody.idempotencyKey,
      transactionPlanId: transactionPlan.transactionPlanId,
      payload: {
        changeRequest: updatedChangeRequestResult.value,
        transactionPlan: executedPlanResult.value,
        outboxRows: outboxRowsResult.value,
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
        eventType: LEDGER_EVENT_TYPES.TRANSACTION_PLAN_CREATED,
        transactionPlanId: transactionPlan.transactionPlanId,
        payload: { transactionPlan },
      },
      {
        eventType: LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED,
        transactionPlanId: transactionPlan.transactionPlanId,
        payload: {
          changeRequestId: updatedChangeRequestResult.value.changeRequestId,
          transactionPlanId: transactionPlan.transactionPlanId,
        },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(updatedWorkflowResult.value));
}

function createHeadcountChangeRequest(input: {
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  headcountInput: HeadcountInput;
  preflight: PreflightOutput;
  timestamp: string;
}): ChangeRequestRecord {
  return {
    changeRequestId: makeId("cr"),
    tenantId: input.requestContext.tenantId,
    environmentId: input.requestContext.environmentId,
    changeType: CHANGE_REQUEST_TYPES.HEADCOUNT_REQUISITION,
    targetWorkerId: input.workflowInstance.subjectId,
    requesterActorId: input.requestContext.actor.actorId,
    effectiveAt: input.headcountInput.targetStartDate,
    businessReason: input.headcountInput.businessJustification,
    status: CHANGE_REQUEST_STATUSES.IN_APPROVAL,
    priority: "normal",
    currentSnapshot: {
      position: null,
    },
    proposedSnapshot: headcountProposedSnapshot(input.headcountInput),
    preflightResult: input.preflight,
    aiReview: {},
    workflowDefinitionId: input.workflowInstance.workflowDefinitionId,
    workflowVersionId: input.workflowInstance.workflowVersionId,
    submittedAt: input.timestamp,
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
    createdBy: input.requestContext.actor.actorId,
    updatedBy: input.requestContext.actor.actorId,
    version: 1,
    metadata: {
      positionId: input.workflowInstance.subjectId,
      workflowIntent: headcountIntent,
    },
  };
}

function createHeadcountProposedChange(input: {
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  headcountInput: HeadcountInput;
  timestamp: string;
}): ProposedChangeRecord {
  return {
    proposedChangeId: makeId("pc"),
    tenantId: input.requestContext.tenantId,
    changeRequestId: input.changeRequest.changeRequestId,
    targetObjectType: "position",
    targetObjectId: input.workflowInstance.subjectId,
    fieldPath: "position.headcountRequisition",
    currentValue: null,
    proposedValue: headcountProposedSnapshot(input.headcountInput),
    effectiveAt: input.headcountInput.targetStartDate,
    reasonCode: "headcount_requisition",
    validationStatus: "valid",
    riskLevel: "medium",
    metadata: {
      workflowIntent: headcountIntent,
    },
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
  };
}

function headcountProposedSnapshot(
  headcountInput: HeadcountInput,
): Record<string, unknown> {
  return {
    department: headcountInput.department,
    team: headcountInput.team,
    location: headcountInput.location,
    costCenter: headcountInput.costCenter,
    jobCode: headcountInput.jobCode,
    title: headcountInput.title,
    level: headcountInput.level,
    requestedFte: headcountInput.requestedFte,
    targetStartDate: headcountInput.targetStartDate,
    salaryRange: {
      min: headcountInput.salaryRangeMin,
      max: headcountInput.salaryRangeMax,
      currency: "USD",
    },
    businessJustification: headcountInput.businessJustification,
  };
}

function approvalGateConfigById(
  workflowConfig: WorkflowConfig,
  gateId: string,
): Result<WorkflowApprovalGateConfig, AppError> {
  const approvalGate = workflowConfig.graph?.nodes.find((node) => {
    return node.approvalGate?.gateId === gateId;
  })?.approvalGate;

  if (approvalGate === undefined) {
    return err(validationFailedError({ gateId }));
  }

  return ok(approvalGate);
}

function resolveLeadershipApprovers(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  gateConfig: WorkflowApprovalGateConfig;
  selectedActorIds: string[];
}): Result<ResolvedHeadcountApprover[], AppError> {
  const resolver = input.gateConfig.approverResolvers[0];
  if (resolver === undefined) {
    return err(validationFailedError({ approverResolvers: "missing" }));
  }

  const approvers: ResolvedHeadcountApprover[] = [];

  for (const [sequenceIndex, actorId] of input.selectedActorIds.entries()) {
    const actorResult = input.repositories.actors.findById(actorId);
    if (!actorResult.ok) {
      return actorResult;
    }
    if (actorResult.value.tenantId !== input.requestContext.tenantId) {
      return err(validationFailedError({ actorId, tenantId: "mismatch" }));
    }

    approvers.push(
      resolvedApproverFromActor({
        resolver,
        actor: actorResult.value,
        sequenceIndex,
        opensWithGate: sequenceIndex === 0,
        resolvedFrom: {
          type: "workflow_field",
          fieldPath: resolver.fieldPath,
        },
      }),
    );
  }

  return ok(approvers);
}

function resolveCrossFunctionalApprovers(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  gateConfig: WorkflowApprovalGateConfig;
}): Result<ResolvedHeadcountApprover[], AppError> {
  const approvers: ResolvedHeadcountApprover[] = [];

  for (const [
    sequenceIndex,
    resolver,
  ] of input.gateConfig.approverResolvers.entries()) {
    const actorResult = resolveActorForResolver({
      repositories: input.repositories,
      tenantId: input.requestContext.tenantId,
      resolver,
    });
    if (!actorResult.ok) {
      return actorResult;
    }

    approvers.push(
      resolvedApproverFromActor({
        resolver,
        actor: actorResult.value,
        sequenceIndex,
        opensWithGate: resolver.opensWithGate === true,
        resolvedFrom: {
          type: resolver.type,
          role: resolver.role,
        },
      }),
    );
  }

  return ok(approvers);
}

function resolvedApproverFromActor(input: {
  resolver: WorkflowApprovalGateApproverResolverConfig;
  actor: ActorRecord;
  sequenceIndex: number;
  opensWithGate: boolean;
  resolvedFrom: Record<string, unknown>;
}): ResolvedHeadcountApprover {
  return {
    resolverId: input.resolver.resolverId,
    actorId: input.actor.actorId,
    role: input.resolver.role ?? input.actor.roles[0] ?? ACTOR_ROLES.EMPLOYEE,
    label: input.resolver.label,
    taskKey: input.resolver.taskKey,
    approvalType: input.resolver.approvalType,
    permission: input.resolver.permission,
    sequenceIndex: input.sequenceIndex,
    opensWithGate: input.opensWithGate,
    isVetoHolder: input.resolver.isVetoHolder === true,
    weight: input.resolver.weight ?? 1,
    resolvedFrom: input.resolvedFrom,
  };
}

function resolveActorForResolver(input: {
  repositories: Repositories;
  tenantId: string;
  resolver: WorkflowApprovalGateApproverResolverConfig;
}): Result<ActorRecord, AppError> {
  if (input.resolver.actorId !== undefined) {
    return input.repositories.actors.findById(input.resolver.actorId);
  }

  const role = input.resolver.role;
  if (role === undefined) {
    return err(validationFailedError({ resolverId: input.resolver.resolverId }));
  }

  const preferredActorId = preferredDemoActorByRole.get(role);
  if (preferredActorId !== undefined) {
    const preferredActorResult = input.repositories.actors.findById(preferredActorId);
    if (preferredActorResult.ok) {
      return preferredActorResult;
    }
  }

  const actor = [...input.repositories.store.actors.values()].find((candidate) => {
    return (
      candidate.tenantId === input.tenantId &&
      candidate.status === "active" &&
      actorMatchesResolverRole(candidate, role)
    );
  });

  if (actor === undefined) {
    return err(validationFailedError({ role, resolverId: input.resolver.resolverId }));
  }

  return ok(actor);
}

function actorMatchesResolverRole(actor: ActorRecord, role: string): boolean {
  if (actor.roles.includes(role)) {
    return true;
  }

  return role === "medical_director" && actor.roles.includes("clinical_admin");
}

function createApprovalGroupForGate(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  gateConfig: WorkflowApprovalGateConfig;
  approvers: ResolvedHeadcountApprover[];
}): Result<ApprovalGroupRecord, AppError> {
  const timestamp = nowIso();
  const approvalGroup: ApprovalGroupRecord = {
    approvalGroupId: makeId("apprgrp"),
    tenantId: input.requestContext.tenantId,
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    changeRequestId: input.changeRequest.changeRequestId,
    gateNodeId: input.gateConfig.gateId,
    mode: input.gateConfig.mode,
    status: "active",
    passRule: input.gateConfig.passRule,
    failurePolicy: failurePolicyForGate(input.gateConfig),
    currentSequenceIndex: input.gateConfig.mode === "sequential" ? 0 : undefined,
    openedAt: timestamp,
    metadata: {
      gateId: input.gateConfig.gateId,
      interaction: input.gateConfig.interaction,
      resolvedApprovers: input.approvers,
      approverCount: input.approvers.length,
    },
    createdAt: timestamp,
    updatedAt: timestamp,
  };

  return input.repositories.approvalGroups.create(approvalGroup);
}

function createApprovalTasksForSequence(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  approvalGroup: ApprovalGroupRecord;
  gateConfig: WorkflowApprovalGateConfig;
  approvers: ResolvedHeadcountApprover[];
  sequenceIndex: number;
}): Result<ApprovalTaskRecord[], AppError> {
  const approversToOpen = input.approvers.filter((approver) => {
    return approver.sequenceIndex === input.sequenceIndex;
  });

  return createApprovalTasksForApprovers({
    ...input,
    approvers: approversToOpen,
  });
}

function createApprovalTasksForApprovers(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  approvalGroup: ApprovalGroupRecord;
  gateConfig: WorkflowApprovalGateConfig;
  approvers: ResolvedHeadcountApprover[];
}): Result<ApprovalTaskRecord[], AppError> {
  const createdTasks: ApprovalTaskRecord[] = [];

  for (const approver of input.approvers) {
    const approvalTask: ApprovalTaskRecord = {
      approvalTaskId: makeId("approval"),
      tenantId: input.requestContext.tenantId,
      changeRequestId: input.changeRequest.changeRequestId,
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      approvalGroupId: input.approvalGroup.approvalGroupId,
      gateNodeId: input.gateConfig.gateId,
      assigneeActorId: approver.actorId,
      assigneeRole: approver.role,
      assigneeRelationship: approver.label,
      approvalType: approver.approvalType,
      status: APPROVAL_TASK_STATUSES.PENDING,
      sequenceIndex: approver.sequenceIndex,
      weight: approver.weight,
      isVetoHolder: approver.isVetoHolder,
      resolvedFrom: approver.resolvedFrom,
      taskVersion: 1,
      createdAt: nowIso(),
      metadata: {
        assignmentMode: "actor",
        approvalGateId: input.gateConfig.gateId,
        gateId: input.gateConfig.gateId,
        gateNodeId: input.gateConfig.gateId,
        approvalGroupId: input.approvalGroup.approvalGroupId,
        resolverId: approver.resolverId,
        taskKey: approver.taskKey,
        permission: approver.permission,
        sequenceIndex: approver.sequenceIndex,
        weight: approver.weight,
        isVetoHolder: approver.isVetoHolder,
        taskVersion: 1,
      },
    };
    const createTaskResult = input.repositories.approvals.create(approvalTask);
    if (!createTaskResult.ok) {
      return createTaskResult;
    }

    createdTasks.push(createTaskResult.value);
  }

  return ok(createdTasks);
}

function verifyHeadcountApprovalTask(input: {
  repositories: Repositories;
  actor: ActorRecord;
  workflowInstance: WorkflowInstanceRecord;
  decisionInput: HeadcountApprovalDecisionInput;
}): Result<ApprovalTaskRecord, AppError> {
  const approvalTaskResult = input.repositories.approvals.findById(
    input.decisionInput.approvalTaskId,
  );
  if (!approvalTaskResult.ok) {
    return approvalTaskResult;
  }

  if (
    approvalTaskResult.value.workflowInstanceId !==
      input.workflowInstance.workflowInstanceId ||
    approvalTaskResult.value.status !== APPROVAL_TASK_STATUSES.PENDING
  ) {
    return err(
      invalidWorkflowTransitionError({
        approvalTaskId: input.decisionInput.approvalTaskId,
        workflowInstanceId: input.workflowInstance.workflowInstanceId,
      }),
    );
  }

  if (
    !canPerformHeadcountApproval({
      actor: input.actor,
      pendingApprovalTask: approvalTaskResult.value,
    })
  ) {
    return err(
      permissionDeniedError({
        approvalTaskId: approvalTaskResult.value.approvalTaskId,
      }),
    );
  }

  const actualTaskVersion = taskVersionForApprovalTask(approvalTaskResult.value);
  if (
    input.decisionInput.taskVersion !== undefined &&
    input.decisionInput.taskVersion !== actualTaskVersion
  ) {
    return err(
      versionConflictError({
        approvalTaskId: approvalTaskResult.value.approvalTaskId,
        expectedTaskVersion: input.decisionInput.taskVersion,
        actualTaskVersion,
      }),
    );
  }

  return ok(approvalTaskResult.value);
}

function approvalGroupForTask(
  repositories: Repositories,
  approvalTask: ApprovalTaskRecord,
): Result<ApprovalGroupRecord, AppError> {
  const approvalGroupId =
    approvalTask.approvalGroupId ??
    stringField(approvalTask.metadata, "approvalGroupId");

  if (approvalGroupId === undefined) {
    return err(validationFailedError({ approvalGroupId: "missing" }));
  }

  return repositories.approvalGroups.findById(approvalGroupId);
}

function decidedApprovalTask(input: {
  approvalTask: ApprovalTaskRecord;
  transition: string;
  decisionInput: HeadcountApprovalDecisionInput;
}): ApprovalTaskRecord {
  const taskVersion = taskVersionForApprovalTask(input.approvalTask) + 1;
  const decision = decisionForTransition(input.transition);

  return {
    ...input.approvalTask,
    status:
      decision === "approved"
        ? APPROVAL_TASK_STATUSES.APPROVED
        : APPROVAL_TASK_STATUSES.REJECTED,
    decision,
    ...(input.decisionInput.reason !== undefined
      ? { decisionReason: input.decisionInput.reason }
      : {}),
    ...(input.decisionInput.comment !== undefined
      ? { comments: input.decisionInput.comment }
      : {}),
    decidedAt: nowIso(),
    taskVersion,
    metadata: {
      ...input.approvalTask.metadata,
      taskVersion,
    },
  };
}

function decisionForTransition(transition: string): ApprovalGateTaskDecision {
  if (transition === "request_more_info") {
    return "more_info_requested";
  }

  return transition === "reject" ? "rejected" : "approved";
}

function evaluateApprovalGroup(input: {
  repositories: Repositories;
  approvalGroup: ApprovalGroupRecord;
  decidedTask: ApprovalTaskRecord;
}) {
  const groupTasksResult = input.repositories.approvals.findByApprovalGroup(
    input.approvalGroup.approvalGroupId,
  );
  if (!groupTasksResult.ok) {
    return groupTasksResult;
  }

  const totalTaskCount = numberField(input.approvalGroup.metadata, "approverCount");

  return evaluateApprovalGate({
    mode: input.approvalGroup.mode === "parallel" ? "parallel" : "sequential",
    passRule: input.approvalGroup.passRule as ApprovalGatePassRule,
    failurePolicy: evaluatorFailurePolicy(input.approvalGroup),
    totalTaskCount,
    tasks: groupTasksResult.value.map((task) => {
      return {
        approvalTaskId: task.approvalTaskId,
        status: task.status,
        decision:
          typeof task.decision === "string"
            ? (task.decision as ApprovalGateTaskDecision)
            : undefined,
        assigneeRole: task.assigneeRole,
        sequenceIndex: task.sequenceIndex,
        weight: task.weight,
        isVetoHolder: task.isVetoHolder,
        taskVersion: taskVersionForApprovalTask(task),
        expectedTaskVersion:
          task.approvalTaskId === input.decidedTask.approvalTaskId
            ? taskVersionForApprovalTask(input.decidedTask)
            : undefined,
      };
    }),
  });
}

function advanceLeadershipGate(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  approvalGroup: ApprovalGroupRecord;
  decidedTask: ApprovalTaskRecord;
  nextSequenceIndexes: number[];
  idempotencyKey: string;
}): Result<Record<string, unknown>, AppError> {
  const gateConfigResult = approvalGateConfigById(
    input.workflowConfig,
    leadershipGateId,
  );
  if (!gateConfigResult.ok) {
    return gateConfigResult;
  }
  const approversResult = approversFromApprovalGroup(input.approvalGroup);
  if (!approversResult.ok) {
    return approversResult;
  }

  const createdTasks: ApprovalTaskRecord[] = [];
  for (const sequenceIndex of input.nextSequenceIndexes) {
    const taskResult = createApprovalTasksForSequence({
      repositories: input.repositories,
      requestContext: input.requestContext,
      workflowInstance: input.workflowInstance,
      changeRequest: input.changeRequest,
      approvalGroup: input.approvalGroup,
      gateConfig: gateConfigResult.value,
      approvers: approversResult.value,
      sequenceIndex,
    });
    if (!taskResult.ok) {
      return taskResult;
    }
    createdTasks.push(...taskResult.value);
  }

  const updatedGroupResult = input.repositories.approvalGroups.update({
    ...input.approvalGroup,
    currentSequenceIndex: input.nextSequenceIndexes[0],
  });
  if (!updatedGroupResult.ok) {
    return updatedGroupResult;
  }

  const updatedWorkflowResult = input.repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.WAITING_SYNC_APPROVAL,
    status: WORKFLOW_STATUSES.WAITING,
    currentInteraction: withGateProgress(input.workflowInstance.currentInteraction, {
      gateId: leadershipGateId,
      mode: "sequential",
      approvedCount: input.nextSequenceIndexes[0] ?? 1,
      requiredApprovals: approversResult.value.length,
      pendingCount: createdTasks.length,
    }),
    version: input.workflowInstance.version + 1,
  });
  if (!updatedWorkflowResult.ok) {
    return updatedWorkflowResult;
  }

  const ledgerResult = recordGateDecisionLedger({
    repositories: input.repositories,
    requestContext: input.requestContext,
    workflowInstance: updatedWorkflowResult.value,
    previousState: input.workflowInstance.state,
    decidedTask: input.decidedTask,
    idempotencyKey: input.idempotencyKey,
    additionalEvents: createdTasks.map((approvalTask) => {
      return {
        eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CREATED,
        approvalTaskId: approvalTask.approvalTaskId,
        payload: { approvalTask },
      };
    }),
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(updatedWorkflowResult.value));
}

function passLeadershipGateAndOpenAsyncGate(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  approvalGroup: ApprovalGroupRecord;
  decidedTask: ApprovalTaskRecord;
  idempotencyKey: string;
}): Result<Record<string, unknown>, AppError> {
  const asyncGateConfigResult = approvalGateConfigById(
    input.workflowConfig,
    crossFunctionalGateId,
  );
  if (!asyncGateConfigResult.ok) {
    return asyncGateConfigResult;
  }
  const asyncApproversResult = resolveCrossFunctionalApprovers({
    repositories: input.repositories,
    requestContext: input.requestContext,
    gateConfig: asyncGateConfigResult.value,
  });
  if (!asyncApproversResult.ok) {
    return asyncApproversResult;
  }

  const passedGroupResult = input.repositories.approvalGroups.update({
    ...input.approvalGroup,
    status: "passed",
    completedAt: nowIso(),
  });
  if (!passedGroupResult.ok) {
    return passedGroupResult;
  }

  const asyncGroupResult = createApprovalGroupForGate({
    repositories: input.repositories,
    requestContext: input.requestContext,
    workflowInstance: input.workflowInstance,
    changeRequest: input.changeRequest,
    gateConfig: asyncGateConfigResult.value,
    approvers: asyncApproversResult.value,
  });
  if (!asyncGroupResult.ok) {
    return asyncGroupResult;
  }

  const asyncTasksResult = createApprovalTasksForApprovers({
    repositories: input.repositories,
    requestContext: input.requestContext,
    workflowInstance: input.workflowInstance,
    changeRequest: input.changeRequest,
    approvalGroup: asyncGroupResult.value,
    gateConfig: asyncGateConfigResult.value,
    approvers: asyncApproversResult.value,
  });
  if (!asyncTasksResult.ok) {
    return asyncTasksResult;
  }

  const nextInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: asyncGateConfigResult.value.interaction,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const updatedWorkflowResult = input.repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.WAITING_ASYNC_APPROVAL,
    status: WORKFLOW_STATUSES.WAITING,
    currentInteraction: withGateProgress(nextInteractionResult.value, {
      gateId: crossFunctionalGateId,
      mode: "parallel",
      approvedCount: 0,
      requiredApprovals: requiredApprovalsFromPassRule(
        asyncGateConfigResult.value.passRule as Record<string, unknown>,
      ),
      pendingCount: asyncTasksResult.value.length,
    }),
    context: {
      ...input.workflowInstance.context,
      activeApprovalGroupId: asyncGroupResult.value.approvalGroupId,
    },
    version: input.workflowInstance.version + 1,
  });
  if (!updatedWorkflowResult.ok) {
    return updatedWorkflowResult;
  }

  const ledgerResult = recordGateDecisionLedger({
    repositories: input.repositories,
    requestContext: input.requestContext,
    workflowInstance: updatedWorkflowResult.value,
    previousState: input.workflowInstance.state,
    decidedTask: input.decidedTask,
    idempotencyKey: input.idempotencyKey,
    additionalEvents: [
      {
        eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_PASSED,
        payload: {
          approvalGroupId: passedGroupResult.value.approvalGroupId,
          gateId: leadershipGateId,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_OPENED,
        payload: {
          approvalGroup: asyncGroupResult.value,
          gateId: crossFunctionalGateId,
          approverCount: asyncApproversResult.value.length,
        },
      },
      ...asyncTasksResult.value.map((approvalTask) => {
        return {
          eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CREATED,
          approvalTaskId: approvalTask.approvalTaskId,
          payload: { approvalTask },
        };
      }),
    ],
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(updatedWorkflowResult.value));
}

function passAsyncGate(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  approvalGroup: ApprovalGroupRecord;
  decidedTask: ApprovalTaskRecord;
  cancelPendingTaskIds: string[];
  idempotencyKey: string;
}): Result<Record<string, unknown>, AppError> {
  const canceledTasksResult = cancelPendingApprovalTasks(
    input.repositories,
    input.cancelPendingTaskIds,
  );
  if (!canceledTasksResult.ok) {
    return canceledTasksResult;
  }

  const passedGroupResult = input.repositories.approvalGroups.update({
    ...input.approvalGroup,
    status: "passed",
    completedAt: nowIso(),
  });
  if (!passedGroupResult.ok) {
    return passedGroupResult;
  }

  const approvedChangeRequestResult = input.repositories.changeRequests.update({
    ...input.changeRequest,
    status: CHANGE_REQUEST_STATUSES.APPROVED,
    approvedAt: nowIso(),
    updatedBy: input.requestContext.actor.actorId,
    version: input.changeRequest.version + 1,
  });
  if (!approvedChangeRequestResult.ok) {
    return approvedChangeRequestResult;
  }

  const readyInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: "readyToExecute",
  });
  if (!readyInteractionResult.ok) {
    return readyInteractionResult;
  }

  const updatedWorkflowResult = input.repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.APPROVED,
    status: WORKFLOW_STATUSES.ACTIVE,
    currentInteraction: withGateProgress(readyInteractionResult.value, {
      gateId: crossFunctionalGateId,
      mode: "parallel",
      approvedCount: 3,
      requiredApprovals: 3,
      pendingCount: 0,
    }),
    context: {
      ...input.workflowInstance.context,
      activeApprovalGroupId: undefined,
    },
    version: input.workflowInstance.version + 1,
  });
  if (!updatedWorkflowResult.ok) {
    return updatedWorkflowResult;
  }

  const ledgerResult = recordGateDecisionLedger({
    repositories: input.repositories,
    requestContext: input.requestContext,
    workflowInstance: updatedWorkflowResult.value,
    previousState: input.workflowInstance.state,
    decidedTask: input.decidedTask,
    idempotencyKey: input.idempotencyKey,
    additionalEvents: [
      ...canceledTasksResult.value.map((approvalTask) => {
        return {
          eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CANCELED,
          approvalTaskId: approvalTask.approvalTaskId,
          payload: { approvalTask },
        };
      }),
      {
        eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_PASSED,
        payload: {
          approvalGroupId: passedGroupResult.value.approvalGroupId,
          gateId: crossFunctionalGateId,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.HEADCOUNT_REQUISITION_APPROVED,
        payload: {
          changeRequest: approvedChangeRequestResult.value,
        },
      },
    ],
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(updatedWorkflowResult.value));
}

function failApprovalGate(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  approvalGroup: ApprovalGroupRecord;
  decidedTask: ApprovalTaskRecord;
  cancelPendingTaskIds: string[];
  idempotencyKey: string;
}): Result<Record<string, unknown>, AppError> {
  const canceledTasksResult = cancelPendingApprovalTasks(
    input.repositories,
    input.cancelPendingTaskIds,
  );
  if (!canceledTasksResult.ok) {
    return canceledTasksResult;
  }

  const failedGroupResult = input.repositories.approvalGroups.update({
    ...input.approvalGroup,
    status: "failed",
    failedAt: nowIso(),
  });
  if (!failedGroupResult.ok) {
    return failedGroupResult;
  }

  const rejectedChangeRequestResult = input.repositories.changeRequests.update({
    ...input.changeRequest,
    status: CHANGE_REQUEST_STATUSES.REJECTED,
    closedAt: nowIso(),
    updatedBy: input.requestContext.actor.actorId,
    version: input.changeRequest.version + 1,
  });
  if (!rejectedChangeRequestResult.ok) {
    return rejectedChangeRequestResult;
  }

  const updatedWorkflowResult = input.repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.APPROVAL_GATE_FAILED,
    status: WORKFLOW_STATUSES.REJECTED,
    currentInteraction: terminalInteraction("rejected"),
    version: input.workflowInstance.version + 1,
  });
  if (!updatedWorkflowResult.ok) {
    return updatedWorkflowResult;
  }

  const ledgerResult = recordGateDecisionLedger({
    repositories: input.repositories,
    requestContext: input.requestContext,
    workflowInstance: updatedWorkflowResult.value,
    previousState: input.workflowInstance.state,
    decidedTask: input.decidedTask,
    idempotencyKey: input.idempotencyKey,
    additionalEvents: [
      ...canceledTasksResult.value.map((approvalTask) => {
        return {
          eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CANCELED,
          approvalTaskId: approvalTask.approvalTaskId,
          payload: { approvalTask },
        };
      }),
      {
        eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_FAILED,
        payload: {
          approvalGroupId: failedGroupResult.value.approvalGroupId,
          gateId: failedGroupResult.value.gateNodeId,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.HEADCOUNT_REQUISITION_REJECTED,
        payload: {
          changeRequest: rejectedChangeRequestResult.value,
        },
      },
    ],
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(updatedWorkflowResult.value));
}

function repairApprovalGate(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  changeRequest: ChangeRequestRecord;
  approvalGroup: ApprovalGroupRecord;
  decidedTask: ApprovalTaskRecord;
  cancelPendingTaskIds: string[];
  idempotencyKey: string;
}): Result<Record<string, unknown>, AppError> {
  const canceledTasksResult = cancelPendingApprovalTasks(
    input.repositories,
    input.cancelPendingTaskIds,
  );
  if (!canceledTasksResult.ok) {
    return canceledTasksResult;
  }

  const repairGroupResult = input.repositories.approvalGroups.update({
    ...input.approvalGroup,
    status: "repair",
    completedAt: nowIso(),
  });
  if (!repairGroupResult.ok) {
    return repairGroupResult;
  }

  const repairChangeRequestResult = input.repositories.changeRequests.update({
    ...input.changeRequest,
    status: CHANGE_REQUEST_STATUSES.WAITING_REPAIR,
    updatedBy: input.requestContext.actor.actorId,
    version: input.changeRequest.version + 1,
  });
  if (!repairChangeRequestResult.ok) {
    return repairChangeRequestResult;
  }

  const repairInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: "repair",
  });
  if (!repairInteractionResult.ok) {
    return repairInteractionResult;
  }

  const updatedWorkflowResult = input.repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.WAITING_APPROVAL_REPAIR,
    status: WORKFLOW_STATUSES.WAITING_REPAIR,
    currentInteraction: repairInteractionResult.value,
    version: input.workflowInstance.version + 1,
  });
  if (!updatedWorkflowResult.ok) {
    return updatedWorkflowResult;
  }

  const ledgerResult = recordGateDecisionLedger({
    repositories: input.repositories,
    requestContext: input.requestContext,
    workflowInstance: updatedWorkflowResult.value,
    previousState: input.workflowInstance.state,
    decidedTask: input.decidedTask,
    idempotencyKey: input.idempotencyKey,
    additionalEvents: [
      ...canceledTasksResult.value.map((approvalTask) => {
        return {
          eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CANCELED,
          approvalTaskId: approvalTask.approvalTaskId,
          payload: { approvalTask },
        };
      }),
      {
        eventType: LEDGER_EVENT_TYPES.MORE_INFORMATION_REQUESTED,
        approvalTaskId: input.decidedTask.approvalTaskId,
        payload: {
          approvalTask: input.decidedTask,
          changeRequest: repairChangeRequestResult.value,
          approvalGroup: repairGroupResult.value,
        },
      },
    ],
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(updatedWorkflowResult.value));
}

function recordWaitingGateDecision(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  approvalGroup: ApprovalGroupRecord;
  decidedTask: ApprovalTaskRecord;
  idempotencyKey: string;
}): Result<Record<string, unknown>, AppError> {
  const updatedWorkflowResult = input.repositories.workflows.updateInstance({
    ...input.workflowInstance,
    version: input.workflowInstance.version + 1,
  });
  if (!updatedWorkflowResult.ok) {
    return updatedWorkflowResult;
  }

  const ledgerResult = recordGateDecisionLedger({
    repositories: input.repositories,
    requestContext: input.requestContext,
    workflowInstance: updatedWorkflowResult.value,
    previousState: input.workflowInstance.state,
    decidedTask: input.decidedTask,
    idempotencyKey: input.idempotencyKey,
    additionalEvents: [],
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(updatedWorkflowResult.value));
}

function cancelPendingApprovalTasks(
  repositories: Repositories,
  approvalTaskIds: string[],
): Result<ApprovalTaskRecord[], AppError> {
  const canceledTasks: ApprovalTaskRecord[] = [];

  for (const approvalTaskId of approvalTaskIds) {
    const approvalTaskResult = repositories.approvals.findById(approvalTaskId);
    if (!approvalTaskResult.ok) {
      return approvalTaskResult;
    }
    if (approvalTaskResult.value.status !== APPROVAL_TASK_STATUSES.PENDING) {
      continue;
    }

    const taskVersion = taskVersionForApprovalTask(approvalTaskResult.value) + 1;
    const updateResult = repositories.approvals.update({
      ...approvalTaskResult.value,
      status: APPROVAL_TASK_STATUSES.CANCELED,
      decidedAt: nowIso(),
      taskVersion,
      metadata: {
        ...approvalTaskResult.value.metadata,
        taskVersion,
      },
    });
    if (!updateResult.ok) {
      return updateResult;
    }

    canceledTasks.push(updateResult.value);
  }

  return ok(canceledTasks);
}

function approversFromApprovalGroup(
  approvalGroup: ApprovalGroupRecord,
): Result<ResolvedHeadcountApprover[], AppError> {
  const value = approvalGroup.metadata["resolvedApprovers"];

  if (!Array.isArray(value)) {
    return err(validationFailedError({ resolvedApprovers: "missing" }));
  }

  return ok(value as ResolvedHeadcountApprover[]);
}

function createHeadcountTransactionPlan(input: {
  tenantId: string;
  changeRequestId: string;
  actorId: string;
  output: PlanTransactionOutput;
}): TransactionPlanRecord {
  const timestamp = nowIso();

  return {
    transactionPlanId: makeId("tp"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequestId,
    status: "planned",
    planVersion: 1,
    steps: [],
    internalWrites: input.output.internalWrites,
    projectionPatches: input.output.projectionPatches ?? [],
    externalWrites: input.output.externalCallRequests,
    rollbackPlan: {},
    compensationPlan: {},
    idempotencyKeys: Object.fromEntries(
      input.output.externalCallRequests.map((externalWrite) => {
        return [externalWrite.connectionId, externalWrite.idempotencyKey];
      }),
    ),
    simulationResult: {},
    executionResult: {},
    reconciliationResult: {},
    createdAt: timestamp,
    updatedAt: timestamp,
    createdBy: input.actorId,
    updatedBy: input.actorId,
  };
}

function createHeadcountOutboxRows(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  changeRequest: ChangeRequestRecord;
  transactionPlan: TransactionPlanRecord;
  externalWrites: PlanTransactionOutput["externalCallRequests"];
}): Result<IntegrationOutboxRecord[], AppError> {
  const outboxRows: IntegrationOutboxRecord[] = [];

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

function recordGateDecisionLedger(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  previousState: string;
  decidedTask: ApprovalTaskRecord;
  idempotencyKey: string;
  additionalEvents: Array<{
    eventType: string;
    approvalTaskId?: string;
    transactionPlanId?: string;
    payload: Record<string, unknown>;
  }>;
}): Result<true, AppError> {
  const transitionLedgerResult = appendTransitionLedgerEvents(
    input.repositories,
    input.requestContext,
    {
      workflowInstance: input.workflowInstance,
      previousState: input.previousState,
      eventType: LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_DECIDED,
      idempotencyKey: input.idempotencyKey,
      approvalTaskId: input.decidedTask.approvalTaskId,
      payload: {
        approvalTask: input.decidedTask,
      },
    },
  );
  if (!transitionLedgerResult.ok) {
    return transitionLedgerResult;
  }

  return appendAdditionalWorkflowEvents(
    input.repositories,
    input.requestContext,
    input.workflowInstance,
    input.idempotencyKey,
    input.additionalEvents,
  );
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

function taskVersionForApprovalTask(approvalTask: ApprovalTaskRecord): number {
  if (approvalTask.taskVersion !== undefined) {
    return approvalTask.taskVersion;
  }

  const metadataTaskVersion = approvalTask.metadata["taskVersion"];

  return typeof metadataTaskVersion === "number" ? metadataTaskVersion : 1;
}

function failurePolicyForGate(gateConfig: WorkflowApprovalGateConfig): string {
  if (gateConfig.mode === "parallel") {
    return "continue_until_threshold_impossible";
  }

  return gateConfig.failurePolicies[0]?.type ?? "stop_workflow";
}

function evaluatorFailurePolicy(
  approvalGroup: ApprovalGroupRecord,
): ApprovalGateFailurePolicy {
  if (approvalGroup.mode === "parallel") {
    return "continue_until_threshold_impossible";
  }

  return approvalGroup.failurePolicy as ApprovalGateFailurePolicy;
}

function requiredApprovalsFromPassRule(passRule: Record<string, unknown>): number {
  const requiredApprovals = passRule["requiredApprovals"];

  return typeof requiredApprovals === "number" ? requiredApprovals : 0;
}

function withGateProgress(
  interaction: Record<string, unknown>,
  progress: Record<string, unknown>,
): Record<string, unknown> {
  return {
    ...interaction,
    gateProgress: progress,
  };
}

function createPermissionSnapshot(actor: ActorRecord) {
  return {
    actorId: actor.actorId,
    roles: actor.roles,
    linkedWorkerId: actor.linkedWorkerId,
  };
}
