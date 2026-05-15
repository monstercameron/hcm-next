import {
  ACTOR_ROLES,
  ACTOR_TYPES,
  APPROVAL_TASK_STATUSES,
  CHANGE_REQUEST_STATUSES,
  CHANGE_REQUEST_TYPES,
  DOCUMENT_CLASSIFICATIONS,
  LEDGER_EVENT_TYPES,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  WORKFLOW_TRANSITIONS,
  err,
  idempotencyConflictError,
  invalidWorkflowTransitionError,
  ok,
  permissionDeniedError,
  validationFailedError,
  versionConflictError,
  type AppError,
  type Result,
  type WorkflowTransition,
} from "@hcm-next/foundation";
import {
  createDemoDocumentRecord,
  createInitialWorkflowInstance,
  makeId,
  nowIso,
  type ActorRecord,
  type ApprovalTaskRecord,
  type ChangeRequestRecord,
  type EmployeeProjectionDocument,
  type LedgerEventRecord,
  type ProposedChangeRecord,
  type Repositories,
  type TransactionPlanRecord,
  type WorkflowInstanceRecord,
  type WorkflowTransitionAttemptRecord,
} from "@hcm-next/data-store";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import {
  canCreateDocumentForWorkflow,
  canViewDocument,
  canViewWorkflowInstance,
  createPermissionSnapshot,
} from "./permissions.js";
import {
  buildConfiguredInteraction,
  configuredWorkflowIntents,
  findWorkflowActionConfig,
  getWorkflowConfigByIntent,
  renderWorkflowTemplateString,
  resolveWorkflowString,
  resolveWorkflowTemplate,
  type WorkflowActionConfig,
  type WorkflowConfig,
  type WorkflowTemplateSources,
} from "../shared/workflow-config.js";

const identityEvidencePurpose = "legal_name_change_evidence";

type EvidenceInput = {
  documentId: string;
};

type ApprovalDecisionInput = {
  approvalTaskId: string;
  comment?: string;
  reason?: string;
};

type TransitionBody = {
  transition: WorkflowTransition;
  idempotencyKey: string;
  expectedVersion: number;
  input: Record<string, unknown>;
};

type PreflightOutput = {
  valid: boolean;
  riskLevel: string;
  requiresEvidence: boolean;
  requiresApproval: boolean;
  warnings: Record<string, unknown>[];
  errors: Record<string, unknown>[];
};

type PlanTransactionOutput = {
  internalWrites: Array<{
    eventType: string;
    subjectType: string;
    subjectId: string;
    effectiveAt: string;
    payload: Record<string, unknown>;
  }>;
  projectionPatches?: Array<{
    projection: string;
    operation: string;
    path: string;
    value: unknown;
  }>;
  externalCallRequests: Array<{
    connectionId: string;
    operation: string;
    idempotencyKey: string;
    payload: Record<string, unknown>;
    reconciliation?: Record<string, unknown>;
  }>;
};

type TimelineView = "business" | "audit" | "debug";

const timelineViews = {
  BUSINESS: "business",
  AUDIT: "audit",
  DEBUG: "debug",
} as const satisfies Record<string, TimelineView>;

const debugTimelineEventTypes = new Set<string>([
  LEDGER_EVENT_TYPES.WORKFLOW_TRANSITION_SUBMITTED,
  LEDGER_EVENT_TYPES.WORKFLOW_STATE_CHANGED,
  LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_STARTED,
  LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_COMPLETED,
]);

/**
 * Starts the legal-name workflow through the workflow-intent command surface.
 */
export function startWorkflowIntent(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const intent = stringField(body, "intent");
  const subject = objectField(body, "subject");
  const subjectType =
    stringField(body, "subjectType") ?? stringField(subject ?? {}, "type");
  const subjectId = stringField(body, "subjectId") ?? stringField(subject ?? {}, "id");
  const workflowConfigResult =
    intent !== undefined
      ? getWorkflowConfigByIntent(intent)
      : err(validationFailedError({ intent }));

  if (!workflowConfigResult.ok || subjectId === undefined) {
    return err(
      validationFailedError({
        intent,
        subjectType,
        subjectId,
        expectedIntents: configuredWorkflowIntents(),
      }),
    );
  }

  const workflowConfig = workflowConfigResult.value;

  if (subjectType !== workflowConfig.subjectType) {
    return err(
      validationFailedError({
        intent,
        subjectType,
        subjectId,
        expectedSubjectType: workflowConfig.subjectType,
      }),
    );
  }

  const permissionResult = canStartConfiguredWorkflow(
    requestContext.actor,
    workflowConfig,
    subjectId,
  );
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const workflowVersionResult = repositories.workflows.findVersionByIntent(
    workflowConfig.intent,
  );
  if (!workflowVersionResult.ok) {
    return workflowVersionResult;
  }

  const employeeProjectionResult = repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    subjectId,
  );
  if (!employeeProjectionResult.ok) {
    return employeeProjectionResult;
  }

  const initialInteractionResult = buildConfiguredInteraction({
    workflowConfig,
    interactionKey: "input",
    employeeDocument: employeeProjectionResult.value.document,
  });
  if (!initialInteractionResult.ok) {
    return initialInteractionResult;
  }

  const workflowInstance = createInitialWorkflowInstance({
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
    workflowDefinitionId: workflowVersionResult.value.workflowDefinitionId,
    workflowVersionId: workflowVersionResult.value.workflowVersionId,
    intent: workflowConfig.intent,
    subjectType: workflowConfig.subjectType,
    subjectId,
    requesterActorId: requestContext.actor.actorId,
    currentInteraction: initialInteractionResult.value,
    context: {
      targetEmployee: employeeProjectionResult.value.document,
    },
    correlationId: requestContext.correlationId,
    metadata: {},
  });

  const createdWorkflowResult = repositories.workflows.createInstance(workflowInstance);
  if (!createdWorkflowResult.ok) {
    return createdWorkflowResult;
  }

  const ledgerResult = appendWorkflowLedgerEvent(repositories, requestContext, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_INTENT_STARTED,
    workflowInstance,
    subjectType,
    subjectId,
    payload: {
      intent,
      currentInteraction: workflowInstance.currentInteraction,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(createdWorkflowResult.value));
}

/**
 * Reads a workflow instance after applying workflow visibility rules.
 */
export function getWorkflowInstance(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
): Result<Record<string, unknown>, AppError> {
  const workflowResult =
    dependencies.repositories.workflows.findInstanceById(workflowInstanceId);

  if (!workflowResult.ok) {
    return workflowResult;
  }

  const permissionResult = canViewWorkflowInstance(
    requestContext.actor,
    workflowResult.value,
  );
  if (!permissionResult.ok) {
    return permissionResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

/**
 * Returns transition actions permitted for the current actor and workflow state.
 */
export function getAvailableActions(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const workflowResult = repositories.workflows.findInstanceById(workflowInstanceId);

  if (!workflowResult.ok) {
    return workflowResult;
  }

  const permissionResult = canViewWorkflowInstance(
    requestContext.actor,
    workflowResult.value,
  );
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const pendingApprovalTaskResult =
    repositories.approvals.findPendingByWorkflow(workflowInstanceId);
  if (!pendingApprovalTaskResult.ok) {
    return pendingApprovalTaskResult;
  }

  const workflowConfigResult = getWorkflowConfigByIntent(workflowResult.value.intent);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  return ok({
    workflowInstanceId,
    version: workflowResult.value.version,
    state: workflowResult.value.state,
    actions: computeConfiguredAvailableActions({
      actor: requestContext.actor,
      workflowConfig: workflowConfigResult.value,
      workflowInstance: workflowResult.value,
      ...(pendingApprovalTaskResult.value !== undefined
        ? { pendingApprovalTask: pendingApprovalTaskResult.value }
        : {}),
    }),
  });
}

/**
 * Creates a fake document record and attaches it to the workflow for the V0 demo.
 */
export function createDocument(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const workflowInstanceId = stringField(body, "workflowInstanceId");
  const filename = stringField(body, "filename");
  const contentType = stringField(body, "contentType");

  if (
    workflowInstanceId === undefined ||
    filename === undefined ||
    contentType === undefined
  ) {
    return err(
      validationFailedError({
        workflowInstanceId,
        filename,
        contentType,
      }),
    );
  }

  const workflowResult = repositories.workflows.findInstanceById(workflowInstanceId);
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const permissionResult = canCreateDocumentForWorkflow(
    requestContext.actor,
    workflowResult.value,
  );
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const documentRecord = createDemoDocumentRecord({
    tenantId: requestContext.tenantId,
    purpose: identityEvidencePurpose,
    filename,
    contentType,
    classification: DOCUMENT_CLASSIFICATIONS.SENSITIVE_PERSON_IDENTITY,
    workflowInstanceId,
    ownerActorId: requestContext.actor.actorId,
    metadata: {
      fakeUpload: true,
      suppliedMetadata: objectField(body, "metadata") ?? {},
    },
  });
  const createdDocumentResult = repositories.documents.create(documentRecord);
  if (!createdDocumentResult.ok) {
    return createdDocumentResult;
  }

  const attachmentResult = repositories.documents.attachToWorkflow({
    workflowInstanceDocumentId: makeId("wfidoc"),
    tenantId: requestContext.tenantId,
    workflowInstanceId,
    documentId: createdDocumentResult.value.documentId,
    attachedByActorId: requestContext.actor.actorId,
    createdAt: nowIso(),
  });
  if (!attachmentResult.ok) {
    return attachmentResult;
  }

  const ledgerResult = appendWorkflowLedgerEvent(repositories, requestContext, {
    eventType: LEDGER_EVENT_TYPES.DOCUMENT_CREATED,
    workflowInstance: workflowResult.value,
    subjectType: "document",
    subjectId: createdDocumentResult.value.documentId,
    payload: {
      documentId: createdDocumentResult.value.documentId,
      purpose: documentRecord.purpose,
      classification: documentRecord.classification,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok({
    document: createdDocumentResult.value,
  });
}

/**
 * Reads document metadata without returning file contents.
 */
export function getDocument(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  documentId: string,
): Result<Record<string, unknown>, AppError> {
  const documentResult = dependencies.repositories.documents.findById(documentId);

  if (!documentResult.ok) {
    return documentResult;
  }

  const permissionResult = canViewDocument(
    requestContext.actor,
    documentResult.value.ownerActorId,
  );
  if (!permissionResult.ok) {
    return permissionResult;
  }

  return ok({
    document: documentResult.value,
  });
}

/**
 * Returns approval tasks visible to the current actor.
 */
export function getTasks(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): Result<Record<string, unknown>, AppError> {
  const tasksResult = dependencies.repositories.approvals.findPendingForActor(
    requestContext.actor,
  );

  if (!tasksResult.ok) {
    return tasksResult;
  }

  return ok({
    tasks: tasksResult.value,
  });
}

/**
 * Returns the workflow timeline as ordered ledger events.
 */
export function getTimeline(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
  requestedView?: string,
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const timelineViewResult = parseTimelineView(requestedView);

  if (!timelineViewResult.ok) {
    return timelineViewResult;
  }

  const workflowResult = repositories.workflows.findInstanceById(workflowInstanceId);

  if (!workflowResult.ok) {
    return workflowResult;
  }

  const permissionResult = canViewWorkflowInstance(
    requestContext.actor,
    workflowResult.value,
  );
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const timelineResult = repositories.ledger.findTimelineForWorkflow(
    requestContext.tenantId,
    workflowInstanceId,
    workflowResult.value.changeRequestId,
  );
  if (!timelineResult.ok) {
    return timelineResult;
  }

  const workflowConfigResult = getWorkflowConfigByIntent(workflowResult.value.intent);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  return ok({
    workflowInstanceId,
    view: timelineViewResult.value,
    totalLedgerEventCount: timelineResult.value.length,
    events: buildTimelineView(
      timelineResult.value,
      timelineViewResult.value,
      workflowConfigResult.value,
    ),
  });
}

/**
 * Executes one checked workflow transition with idempotency and version guards.
 */
export async function transitionWorkflow(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
  body: Record<string, unknown>,
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const transitionBodyResult = parseTransitionBody(body);

  if (!transitionBodyResult.ok) {
    return transitionBodyResult;
  }

  const workflowResult = repositories.workflows.findInstanceById(workflowInstanceId);
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const idempotentReplayResult = repositories.workflows.findTransitionAttempt(
    requestContext.tenantId,
    workflowInstanceId,
    transitionBodyResult.value.idempotencyKey,
  );
  if (!idempotentReplayResult.ok) {
    return idempotentReplayResult;
  }
  if (idempotentReplayResult.value !== undefined) {
    return replayTransitionAttempt(idempotentReplayResult.value);
  }

  const workflowConfigResult = getWorkflowConfigByIntent(workflowResult.value.intent);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const actionConfigResult = findWorkflowActionConfig({
    workflowConfig: workflowConfigResult.value,
    state: workflowResult.value.state,
    transition: transitionBodyResult.value.transition,
  });
  if (!actionConfigResult.ok) {
    return err(
      invalidWorkflowTransitionError({
        workflowInstanceId,
        state: workflowResult.value.state,
        transition: transitionBodyResult.value.transition,
      }),
    );
  }

  if (workflowResult.value.version !== transitionBodyResult.value.expectedVersion) {
    return err(
      versionConflictError({
        workflowInstanceId,
        expectedVersion: transitionBodyResult.value.expectedVersion,
        actualVersion: workflowResult.value.version,
      }),
    );
  }

  const pendingApprovalTaskResult =
    repositories.approvals.findPendingByWorkflow(workflowInstanceId);
  if (!pendingApprovalTaskResult.ok) {
    return pendingApprovalTaskResult;
  }

  const permissionResult = canSubmitConfiguredTransition({
    actor: requestContext.actor,
    actionConfig: actionConfigResult.value,
    workflowInstance: workflowResult.value,
    ...(pendingApprovalTaskResult.value !== undefined
      ? { pendingApprovalTask: pendingApprovalTaskResult.value }
      : {}),
  });
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const startedAttempt = createTransitionAttempt(
    requestContext,
    workflowInstanceId,
    transitionBodyResult.value,
  );
  const saveAttemptResult =
    repositories.workflows.saveTransitionAttempt(startedAttempt);
  if (!saveAttemptResult.ok) {
    return saveAttemptResult;
  }

  const transitionResult = await executeTransition(dependencies, requestContext, {
    workflowConfig: workflowConfigResult.value,
    actionConfig: actionConfigResult.value,
    workflowInstance: workflowResult.value,
    transitionBody: transitionBodyResult.value,
    ...(pendingApprovalTaskResult.value !== undefined
      ? { pendingApprovalTask: pendingApprovalTaskResult.value }
      : {}),
  });
  const completedAttempt = completeTransitionAttempt(startedAttempt, transitionResult);
  const completedAttemptResult =
    repositories.workflows.saveTransitionAttempt(completedAttempt);

  if (!completedAttemptResult.ok) {
    return completedAttemptResult;
  }

  return transitionResult;
}

/**
 * Reads the demo employee projection for API smoke checks.
 */
export function getEmployeeProjection(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  employeeId: string,
): Result<Record<string, unknown>, AppError> {
  const projectionResult =
    dependencies.repositories.employeeProjections.findByEmployeeId(
      requestContext.tenantId,
      employeeId,
    );

  if (!projectionResult.ok) {
    return projectionResult;
  }

  return ok({
    projection: projectionResult.value,
  });
}

function canStartConfiguredWorkflow(
  actor: ActorRecord,
  workflowConfig: WorkflowConfig,
  subjectId: string,
): Result<true, AppError> {
  const isSelfServiceRequester =
    workflowConfig.selfServiceStart &&
    actor.roles.includes(ACTOR_ROLES.EMPLOYEE) &&
    actor.linkedWorkerId === subjectId;

  if (isSelfServiceRequester) {
    return ok(true);
  }

  return err(permissionDeniedError({ intent: workflowConfig.intent, subjectId }));
}

function computeConfiguredAvailableActions(input: {
  actor: ActorRecord;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  pendingApprovalTask?: ApprovalTaskRecord;
}): Array<Record<string, unknown>> {
  const stateConfig = input.workflowConfig.states[input.workflowInstance.state];
  const actions = stateConfig?.actions ?? [];

  return actions
    .filter((actionConfig) => {
      return canActorPerformConfiguredAction({
        actor: input.actor,
        actionConfig,
        workflowInstance: input.workflowInstance,
      });
    })
    .map((actionConfig) => {
      const action: Record<string, unknown> = {
        transition: actionConfig.transition,
        label: actionConfig.label,
        enabled: true,
      };

      if (
        input.pendingApprovalTask !== undefined &&
        actionRequiresApprovalTask(actionConfig)
      ) {
        action["taskId"] = input.pendingApprovalTask.approvalTaskId;
      }

      return action;
    });
}

function canSubmitConfiguredTransition(input: {
  actor: ActorRecord;
  actionConfig: WorkflowActionConfig;
  workflowInstance: WorkflowInstanceRecord;
}): Result<true, AppError> {
  const isAllowed = canActorPerformConfiguredAction({
    actor: input.actor,
    actionConfig: input.actionConfig,
    workflowInstance: input.workflowInstance,
  });

  if (isAllowed) {
    return ok(true);
  }

  return err(
    permissionDeniedError({
      transition: input.actionConfig.transition,
      state: input.workflowInstance.state,
    }),
  );
}

function canActorPerformConfiguredAction(input: {
  actor: ActorRecord;
  actionConfig: WorkflowActionConfig;
  workflowInstance: WorkflowInstanceRecord;
}): boolean {
  if (input.actionConfig.actor === "requester") {
    return input.actor.linkedWorkerId === input.workflowInstance.subjectId;
  }

  if (input.actionConfig.actor === "hr_admin") {
    return input.actor.roles.includes(ACTOR_ROLES.HR_ADMIN);
  }

  if (input.actionConfig.actor === "hr_admin_or_system") {
    return (
      input.actor.roles.includes(ACTOR_ROLES.HR_ADMIN) ||
      input.actor.roles.includes(ACTOR_ROLES.SYSTEM)
    );
  }

  return false;
}

function actionRequiresApprovalTask(actionConfig: WorkflowActionConfig): boolean {
  return (
    actionConfig.handler === "approve" ||
    actionConfig.handler === "reject" ||
    actionConfig.handler === "request_more_info"
  );
}

async function executeTransition(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    actionConfig: WorkflowActionConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
    pendingApprovalTask?: ApprovalTaskRecord;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  if (input.actionConfig.handler === "submit_configured_input") {
    return submitConfiguredInput(dependencies, requestContext, input);
  }

  if (input.actionConfig.handler === "provide_evidence") {
    return provideEvidence(dependencies, requestContext, input);
  }

  if (input.actionConfig.handler === "approve") {
    return approveChange(dependencies, requestContext, input);
  }

  if (input.actionConfig.handler === "reject") {
    return rejectChange(dependencies, requestContext, input);
  }

  if (input.actionConfig.handler === "request_more_info") {
    return requestMoreInformation(dependencies, requestContext, input);
  }

  if (input.actionConfig.handler === "cancel") {
    return cancelWorkflow(dependencies, requestContext, input);
  }

  if (input.actionConfig.handler === "execute") {
    return executeApprovedChange(dependencies, requestContext, input);
  }

  return err(
    invalidWorkflowTransitionError({
      transition: input.transitionBody.transition,
    }),
  );
}

async function submitConfiguredInput(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    actionConfig: WorkflowActionConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const projectionResult = repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    input.workflowInstance.subjectId,
  );
  if (!projectionResult.ok) {
    return projectionResult;
  }

  const templateSources: WorkflowTemplateSources = {
    input: input.transitionBody.input,
    employee: projectionResult.value.document,
    workflow: input.workflowInstance,
  };
  const effectiveAtResult = resolveWorkflowString(
    input.workflowConfig.submit.effectiveAt,
    templateSources,
  );
  if (!effectiveAtResult.ok) {
    return effectiveAtResult;
  }

  const businessReasonResult = resolveWorkflowString(
    input.workflowConfig.submit.businessReason,
    templateSources,
  );
  if (!businessReasonResult.ok) {
    return businessReasonResult;
  }

  const preflightInputResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.preflightInput,
    templateSources,
  );
  if (!preflightInputResult.ok) {
    return preflightInputResult;
  }

  const preflightResult =
    await dependencies.executorClient.executeBlock<PreflightOutput>({
      tenantId: requestContext.tenantId,
      environmentId: requestContext.environmentId,
      changeRequestId: "",
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      block: input.workflowConfig.submit.preflightBlock,
      input: preflightInputResult.value as Record<string, unknown>,
      context: {
        actorId: requestContext.actor.actorId,
        effectiveAt: effectiveAtResult.value,
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
  const changeRequest = createConfiguredChangeRequest({
    workflowConfig: input.workflowConfig,
    requestContext,
    workflowInstance: input.workflowInstance,
    employeeDocument: projectionResult.value.document,
    input: input.transitionBody.input,
    preflight: preflightResult.value.output,
    effectiveAt: effectiveAtResult.value,
    businessReason: businessReasonResult.value,
    timestamp,
  });
  const changeRequestResult = repositories.changeRequests.create(changeRequest);
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const proposedChangeResult = createConfiguredProposedChange({
    workflowConfig: input.workflowConfig,
    requestContext,
    workflowInstance: input.workflowInstance,
    employeeDocument: projectionResult.value.document,
    input: input.transitionBody.input,
    changeRequest,
    preflight: preflightResult.value.output,
    effectiveAt: effectiveAtResult.value,
    businessReason: businessReasonResult.value,
    timestamp,
  });
  if (!proposedChangeResult.ok) {
    return proposedChangeResult;
  }

  const proposedChangesResult = repositories.proposedChanges.createMany([
    proposedChangeResult.value,
  ]);
  if (!proposedChangesResult.ok) {
    return proposedChangesResult;
  }

  const approvalTaskResult = input.workflowConfig.submit.createApprovalTask
    ? repositories.approvals.create(
        createConfiguredApprovalTask({
          workflowConfig: input.workflowConfig,
          tenantId: requestContext.tenantId,
          workflowInstanceId: input.workflowInstance.workflowInstanceId,
          changeRequestId: changeRequest.changeRequestId,
        }),
      )
    : ok(undefined);
  if (!approvalTaskResult.ok) {
    return approvalTaskResult;
  }

  const nextInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: input.actionConfig.nextInteraction ?? "input",
    employeeDocument: projectionResult.value.document,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const updatedWorkflow = {
    ...input.workflowInstance,
    state: input.actionConfig.nextState ?? input.workflowInstance.state,
    status: input.actionConfig.nextStatus ?? input.workflowInstance.status,
    changeRequestId: changeRequest.changeRequestId,
    currentInteraction: nextInteractionResult.value,
    context: {
      ...input.workflowInstance.context,
      submitInput: input.transitionBody.input,
      preflight: preflightResult.value.output,
      ...(approvalTaskResult.value !== undefined
        ? { approvalTaskId: approvalTaskResult.value.approvalTaskId }
        : {}),
    },
    version: input.workflowInstance.version + 1,
  };
  const updateWorkflowResult = repositories.workflows.updateInstance(updatedWorkflow);
  if (!updateWorkflowResult.ok) {
    return updateWorkflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: updateWorkflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_CREATED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    payload: {
      changeRequestId: changeRequest.changeRequestId,
      preflight: preflightResult.value.output,
      proposedChanges: proposedChangesResult.value,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    repositories,
    requestContext,
    updateWorkflowResult.value,
    input.transitionBody.idempotencyKey,
    configuredSubmitEvents({
      workflowConfig: input.workflowConfig,
      proposedChanges: proposedChangesResult.value,
      preflight: preflightResult.value.output,
      changeRequest,
      ...(approvalTaskResult.value !== undefined
        ? { approvalTask: approvalTaskResult.value }
        : {}),
    }),
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(updateWorkflowResult.value));
}

function provideEvidence(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    actionConfig: WorkflowActionConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
  },
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const evidenceInputResult = parseEvidenceInput(input.transitionBody.input);

  if (!evidenceInputResult.ok) {
    return evidenceInputResult;
  }

  const documentResult = repositories.documents.findById(
    evidenceInputResult.value.documentId,
  );
  if (!documentResult.ok) {
    return documentResult;
  }

  if (
    documentResult.value.workflowInstanceId !==
    input.workflowInstance.workflowInstanceId
  ) {
    return err(
      validationFailedError({
        documentId: documentResult.value.documentId,
        workflowInstanceId: documentResult.value.workflowInstanceId,
      }),
    );
  }

  const changeRequestId = input.workflowInstance.changeRequestId;
  if (changeRequestId === undefined) {
    return err(validationFailedError({ changeRequestId: "missing" }));
  }

  const approvalTask = createConfiguredApprovalTask({
    workflowConfig: input.workflowConfig,
    tenantId: requestContext.tenantId,
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    changeRequestId,
  });
  const approvalTaskResult = repositories.approvals.create(approvalTask);
  if (!approvalTaskResult.ok) {
    return approvalTaskResult;
  }

  const changeRequestResult = repositories.changeRequests.findById(changeRequestId);
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }
  const updatedChangeRequest = {
    ...changeRequestResult.value,
    status: CHANGE_REQUEST_STATUSES.IN_APPROVAL,
    submittedAt: nowIso(),
    updatedBy: requestContext.actor.actorId,
    version: changeRequestResult.value.version + 1,
  };
  const updatedChangeRequestResult =
    repositories.changeRequests.update(updatedChangeRequest);
  if (!updatedChangeRequestResult.ok) {
    return updatedChangeRequestResult;
  }

  const nextInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: input.actionConfig.nextInteraction ?? "waitingApproval",
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const updatedWorkflow = {
    ...input.workflowInstance,
    state: input.actionConfig.nextState ?? input.workflowInstance.state,
    status: input.actionConfig.nextStatus ?? input.workflowInstance.status,
    currentInteraction: nextInteractionResult.value,
    context: {
      ...input.workflowInstance.context,
      evidenceDocumentId: documentResult.value.documentId,
      approvalTaskId: approvalTask.approvalTaskId,
    },
    version: input.workflowInstance.version + 1,
  };
  const workflowResult = repositories.workflows.updateInstance(updatedWorkflow);
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.EVIDENCE_PROVIDED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    approvalTaskId: approvalTask.approvalTaskId,
    payload: {
      documentId: documentResult.value.documentId,
      approvalTask,
      changeRequest: updatedChangeRequestResult.value,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    repositories,
    requestContext,
    workflowResult.value,
    input.transitionBody.idempotencyKey,
    [
      {
        eventType: LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED,
        approvalTaskId: approvalTask.approvalTaskId,
        payload: {
          approvalTask,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_SUBMITTED,
        payload: {
          changeRequest: updatedChangeRequestResult.value,
        },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

async function approveChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
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

  const changeRequestId = input.workflowInstance.changeRequestId;
  if (changeRequestId === undefined) {
    return err(validationFailedError({ changeRequestId: "missing" }));
  }

  const changeRequestResult = repositories.changeRequests.findById(changeRequestId);
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const proposedChangesResult =
    repositories.proposedChanges.findByChangeRequest(changeRequestId);
  if (!proposedChangesResult.ok) {
    return proposedChangesResult;
  }

  const projectionResult = repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    changeRequestResult.value.targetWorkerId,
  );
  if (!projectionResult.ok) {
    return projectionResult;
  }

  const planOutputResult = await planApprovedChange(dependencies, requestContext, {
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    changeRequest: changeRequestResult.value,
    proposedChanges: proposedChangesResult.value,
    employeeDocument: projectionResult.value.document,
    idempotencyKey: input.transitionBody.idempotencyKey,
  });
  if (!planOutputResult.ok) {
    return planOutputResult;
  }

  const transactionPlanInput = {
    tenantId: requestContext.tenantId,
    changeRequestId,
    actorId: requestContext.actor.actorId,
  };
  const transactionPlan = createTransactionPlan({
    ...transactionPlanInput,
    output: planOutputResult.value,
  });
  const transactionPlanResult = repositories.transactionPlans.create(transactionPlan);
  if (!transactionPlanResult.ok) {
    return transactionPlanResult;
  }

  const approvedTask: ApprovalTaskRecord = {
    ...pendingTaskResult.value,
    status: APPROVAL_TASK_STATUSES.APPROVED,
    decision: "approved",
    decidedAt: nowIso(),
  };
  if (decisionInputResult.value.comment !== undefined) {
    approvedTask.comments = decisionInputResult.value.comment;
  }

  const updatedTaskResult = repositories.approvals.update(approvedTask);
  if (!updatedTaskResult.ok) {
    return updatedTaskResult;
  }

  const updatedChangeRequestResult = repositories.changeRequests.update({
    ...changeRequestResult.value,
    status: CHANGE_REQUEST_STATUSES.APPROVED,
    transactionPlanId: transactionPlan.transactionPlanId,
    approvedAt: nowIso(),
    updatedBy: requestContext.actor.actorId,
    version: changeRequestResult.value.version + 1,
  });
  if (!updatedChangeRequestResult.ok) {
    return updatedChangeRequestResult;
  }

  const readyInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: "readyToExecute",
    employeeDocument: projectionResult.value.document,
  });
  if (!readyInteractionResult.ok) {
    return readyInteractionResult;
  }

  const updatedWorkflow = {
    ...input.workflowInstance,
    state: WORKFLOW_STATES.APPROVED,
    status: WORKFLOW_STATUSES.ACTIVE,
    currentInteraction: readyInteractionResult.value,
    context: {
      ...input.workflowInstance.context,
      transactionPlanId: transactionPlan.transactionPlanId,
    },
    version: input.workflowInstance.version + 1,
  };
  const workflowResult = repositories.workflows.updateInstance(updatedWorkflow);
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.APPROVAL_GRANTED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    approvalTaskId: pendingTaskResult.value.approvalTaskId,
    transactionPlanId: transactionPlan.transactionPlanId,
    payload: {
      approvalTask: updatedTaskResult.value,
      transactionPlan,
      changeRequest: updatedChangeRequestResult.value,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    repositories,
    requestContext,
    workflowResult.value,
    input.transitionBody.idempotencyKey,
    [
      {
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_APPROVED,
        transactionPlanId: transactionPlan.transactionPlanId,
        payload: {
          changeRequest: updatedChangeRequestResult.value,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.TRANSACTION_PLAN_CREATED,
        transactionPlanId: transactionPlan.transactionPlanId,
        payload: {
          transactionPlan,
        },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

function rejectChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
    pendingApprovalTask?: ApprovalTaskRecord;
  },
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const decisionInputResult = parseApprovalDecisionInput(input.transitionBody.input);

  if (!decisionInputResult.ok) {
    return decisionInputResult;
  }
  if (decisionInputResult.value.reason === undefined) {
    return err(validationFailedError({ reason: "Required for rejection." }));
  }

  const pendingTaskResult = verifyApprovalTask(
    input.pendingApprovalTask,
    decisionInputResult.value.approvalTaskId,
  );
  if (!pendingTaskResult.ok) {
    return pendingTaskResult;
  }

  const changeRequestResult = findChangeRequestForWorkflow(
    repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const rejectedTask: ApprovalTaskRecord = {
    ...pendingTaskResult.value,
    status: APPROVAL_TASK_STATUSES.REJECTED,
    decision: "rejected",
    decisionReason: decisionInputResult.value.reason,
    decidedAt: nowIso(),
  };
  if (decisionInputResult.value.comment !== undefined) {
    rejectedTask.comments = decisionInputResult.value.comment;
  }

  const taskResult = repositories.approvals.update(rejectedTask);
  if (!taskResult.ok) {
    return taskResult;
  }

  const changeRequestUpdateResult = repositories.changeRequests.update({
    ...changeRequestResult.value,
    status: CHANGE_REQUEST_STATUSES.REJECTED,
    closedAt: nowIso(),
    updatedBy: requestContext.actor.actorId,
    version: changeRequestResult.value.version + 1,
  });
  if (!changeRequestUpdateResult.ok) {
    return changeRequestUpdateResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.REJECTED,
    status: WORKFLOW_STATUSES.REJECTED,
    currentInteraction: terminalInteraction("rejected"),
    completedAt: nowIso(),
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.APPROVAL_REJECTED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    approvalTaskId: pendingTaskResult.value.approvalTaskId,
    payload: {
      approvalTask: taskResult.value,
      changeRequest: changeRequestUpdateResult.value,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    repositories,
    requestContext,
    workflowResult.value,
    input.transitionBody.idempotencyKey,
    [
      {
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_REJECTED,
        approvalTaskId: pendingTaskResult.value.approvalTaskId,
        payload: {
          changeRequest: changeRequestUpdateResult.value,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED,
        payload: {
          terminalState: WORKFLOW_STATES.REJECTED,
        },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

function requestMoreInformation(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    actionConfig: WorkflowActionConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
    pendingApprovalTask?: ApprovalTaskRecord;
  },
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const decisionInputResult = parseApprovalDecisionInput(input.transitionBody.input);

  if (!decisionInputResult.ok) {
    return decisionInputResult;
  }
  if (decisionInputResult.value.comment === undefined) {
    return err(validationFailedError({ comment: "Required for more information." }));
  }

  const pendingTaskResult = verifyApprovalTask(
    input.pendingApprovalTask,
    decisionInputResult.value.approvalTaskId,
  );
  if (!pendingTaskResult.ok) {
    return pendingTaskResult;
  }

  const taskResult = repositories.approvals.update({
    ...pendingTaskResult.value,
    status: APPROVAL_TASK_STATUSES.CANCELED,
    decision: "more_info_requested",
    comments: decisionInputResult.value.comment,
    decidedAt: nowIso(),
  });
  if (!taskResult.ok) {
    return taskResult;
  }

  const nextInteractionResult = buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: input.actionConfig.nextInteraction ?? "input",
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: input.actionConfig.nextState ?? input.workflowInstance.state,
    status: input.actionConfig.nextStatus ?? input.workflowInstance.status,
    currentInteraction: nextInteractionResult.value,
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.MORE_INFORMATION_REQUESTED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    approvalTaskId: pendingTaskResult.value.approvalTaskId,
    payload: {
      comment: decisionInputResult.value.comment,
      approvalTask: taskResult.value,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

function cancelWorkflow(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
  },
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const changeRequestResult = maybeCancelChangeRequest(
    repositories,
    requestContext.actor.actorId,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.CANCELED,
    status: WORKFLOW_STATUSES.CANCELED,
    currentInteraction: terminalInteraction("canceled"),
    canceledAt: nowIso(),
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_CANCELED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    payload: {
      changeRequest: changeRequestResult.value,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

function executeApprovedChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowConfig: WorkflowConfig;
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
  },
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const changeRequestResult = findChangeRequestForWorkflow(
    repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
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

  const projectedDocumentResult = applyInternalTransactionWrites(
    repositories,
    requestContext,
    input.workflowConfig,
    changeRequestResult.value,
    transactionPlanResult.value,
  );
  if (!projectedDocumentResult.ok) {
    return projectedDocumentResult;
  }

  const outboxResult = createIntegrationOutboxRows(
    repositories,
    requestContext,
    changeRequestResult.value,
    transactionPlanResult.value,
  );
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
      projectionVersion: projectedDocumentResult.value.projectionVersion,
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

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_COMPLETED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    transactionPlanId: transactionPlanResult.value.transactionPlanId,
    payload: {
      changeRequest: closedChangeRequestResult.value,
      transactionPlan: executedPlanResult.value,
      outboxRows: outboxResult.value,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    repositories,
    requestContext,
    workflowResult.value,
    input.transitionBody.idempotencyKey,
    [
      {
        eventType: LEDGER_EVENT_TYPES.EMPLOYEE_PROJECTION_UPDATED,
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
        payload: {
          employeeId: changeRequestResult.value.targetWorkerId,
          projectionVersion: projectedDocumentResult.value.projectionVersion,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED,
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
        payload: {
          outboxRows: outboxResult.value,
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

function parseTransitionBody(
  body: Record<string, unknown>,
): Result<TransitionBody, AppError> {
  const transition = stringField(body, "transition");
  const idempotencyKey = stringField(body, "idempotencyKey");
  const expectedVersion = numberField(body, "expectedVersion");
  const input = objectField(body, "input") ?? objectField(body, "payload") ?? {};
  const validTransitions = Object.values(WORKFLOW_TRANSITIONS);

  if (
    transition === undefined ||
    !validTransitions.includes(transition as WorkflowTransition) ||
    idempotencyKey === undefined ||
    expectedVersion === undefined
  ) {
    return err(
      validationFailedError({
        transition,
        idempotencyKey,
        expectedVersion,
      }),
    );
  }

  return ok({
    transition: transition as WorkflowTransition,
    idempotencyKey,
    expectedVersion,
    input,
  });
}

function parseEvidenceInput(
  input: Record<string, unknown>,
): Result<EvidenceInput, AppError> {
  const documentId = stringField(input, "documentId");

  if (documentId === undefined) {
    return err(validationFailedError({ documentId }));
  }

  return ok({ documentId });
}

function parseApprovalDecisionInput(
  input: Record<string, unknown>,
): Result<ApprovalDecisionInput, AppError> {
  const approvalTaskId = stringField(input, "approvalTaskId");
  const comment = stringField(input, "comment");
  const reason = stringField(input, "reason");

  if (approvalTaskId === undefined) {
    return err(validationFailedError({ approvalTaskId }));
  }

  return ok({
    approvalTaskId,
    ...(comment !== undefined ? { comment } : {}),
    ...(reason !== undefined ? { reason } : {}),
  });
}

function replayTransitionAttempt(
  attempt: WorkflowTransitionAttemptRecord,
): Result<Record<string, unknown>, AppError> {
  if (attempt.status === "completed" && attempt.responsePayload !== undefined) {
    return ok({
      ...attempt.responsePayload,
      idempotentReplay: true,
    });
  }

  return err(
    idempotencyConflictError({
      workflowInstanceId: attempt.workflowInstanceId,
      idempotencyKey: attempt.idempotencyKey,
      status: attempt.status,
    }),
  );
}

function createTransitionAttempt(
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
  transitionBody: TransitionBody,
): WorkflowTransitionAttemptRecord {
  return {
    workflowTransitionAttemptId: makeId("wfta"),
    tenantId: requestContext.tenantId,
    workflowInstanceId,
    transition: transitionBody.transition,
    idempotencyKey: transitionBody.idempotencyKey,
    expectedVersion: transitionBody.expectedVersion,
    actorId: requestContext.actor.actorId,
    requestPayload: transitionBody.input,
    status: "started",
    createdAt: nowIso(),
  };
}

function completeTransitionAttempt(
  attempt: WorkflowTransitionAttemptRecord,
  result: Result<Record<string, unknown>, AppError>,
): WorkflowTransitionAttemptRecord {
  if (result.ok) {
    return {
      ...attempt,
      responsePayload: result.value,
      status: "completed",
      completedAt: nowIso(),
    };
  }

  return {
    ...attempt,
    status: "failed",
    error: {
      code: result.error.code,
      message: result.error.safeMessage,
      details: result.error.details ?? {},
    },
    completedAt: nowIso(),
  };
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

function createConfiguredChangeRequest(input: {
  workflowConfig: WorkflowConfig;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  employeeDocument: EmployeeProjectionDocument;
  input: Record<string, unknown>;
  preflight: PreflightOutput;
  effectiveAt: string;
  businessReason: string;
  timestamp: string;
}): ChangeRequestRecord {
  const templateSources: WorkflowTemplateSources = {
    input: input.input,
    employee: input.employeeDocument,
    workflow: input.workflowInstance,
  };
  const currentSnapshotResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.currentSnapshot,
    templateSources,
  );
  const proposedSnapshotResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.proposedSnapshot,
    templateSources,
  );

  return {
    changeRequestId: makeId("chg"),
    tenantId: input.requestContext.tenantId,
    environmentId: input.requestContext.environmentId,
    changeType: CHANGE_REQUEST_TYPES.EMPLOYEE_DATA_CHANGE,
    targetWorkerId: input.workflowInstance.subjectId,
    requesterActorId: input.requestContext.actor.actorId,
    effectiveAt: input.effectiveAt,
    businessReason: input.businessReason,
    status: input.workflowConfig.submit.changeRequestStatus,
    priority: "normal",
    currentSnapshot: currentSnapshotResult.ok
      ? (currentSnapshotResult.value as Record<string, unknown>)
      : {},
    proposedSnapshot: proposedSnapshotResult.ok
      ? (proposedSnapshotResult.value as Record<string, unknown>)
      : {},
    preflightResult: input.preflight,
    aiReview: {
      mode: "not_configured_v0",
      summary: `${input.workflowConfig.intent} uses deterministic preflight. AI review plugs into this envelope later.`,
    },
    workflowDefinitionId: input.workflowInstance.workflowDefinitionId,
    workflowVersionId: input.workflowInstance.workflowVersionId,
    ...(input.workflowConfig.submit.markSubmitted
      ? { submittedAt: input.timestamp }
      : {}),
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
    createdBy: input.requestContext.actor.actorId,
    updatedBy: input.requestContext.actor.actorId,
    version: 1,
    metadata: {},
  };
}

function createConfiguredProposedChange(input: {
  workflowConfig: WorkflowConfig;
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  employeeDocument: EmployeeProjectionDocument;
  input: Record<string, unknown>;
  changeRequest: ChangeRequestRecord;
  preflight: PreflightOutput;
  effectiveAt: string;
  businessReason: string;
  timestamp: string;
}): Result<ProposedChangeRecord, AppError> {
  const templateSources: WorkflowTemplateSources = {
    input: input.input,
    employee: input.employeeDocument,
    workflow: input.workflowInstance,
    changeRequest: input.changeRequest,
  };
  const targetObjectIdResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.proposedChange.targetObjectId,
    templateSources,
  );
  const currentValueResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.proposedChange.currentValue,
    templateSources,
  );
  const proposedValueResult = resolveWorkflowTemplate(
    input.workflowConfig.submit.proposedChange.proposedValue,
    templateSources,
  );

  if (!targetObjectIdResult.ok) {
    return targetObjectIdResult;
  }
  if (!currentValueResult.ok) {
    return currentValueResult;
  }
  if (!proposedValueResult.ok) {
    return proposedValueResult;
  }
  if (typeof targetObjectIdResult.value !== "string") {
    return err(validationFailedError({ targetObjectId: targetObjectIdResult.value }));
  }

  return ok({
    proposedChangeId: makeId("pchg"),
    tenantId: input.requestContext.tenantId,
    changeRequestId: input.changeRequest.changeRequestId,
    targetObjectType: input.workflowConfig.submit.proposedChange.targetObjectType,
    targetObjectId: targetObjectIdResult.value,
    fieldPath:
      input.workflowConfig.submit.proposedChange.fieldPath ??
      renderWorkflowTemplateString(
        input.workflowConfig.submit.proposedChange.fieldPathTemplate ?? "",
        input.input,
      ),
    currentValue: currentValueResult.value,
    proposedValue: proposedValueResult.value,
    effectiveAt: input.effectiveAt,
    reasonCode: input.businessReason,
    validationStatus: "valid",
    riskLevel: input.preflight.riskLevel,
    metadata: {},
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
  });
}

function createConfiguredApprovalTask(input: {
  workflowConfig: WorkflowConfig;
  tenantId: string;
  workflowInstanceId: string;
  changeRequestId: string;
}): ApprovalTaskRecord {
  return {
    approvalTaskId: makeId("appr"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequestId,
    workflowInstanceId: input.workflowInstanceId,
    assigneeActorId: input.workflowConfig.approval.assigneeActorId,
    assigneeRole: input.workflowConfig.approval.assigneeRole,
    approvalType: input.workflowConfig.approval.approvalType,
    status: APPROVAL_TASK_STATUSES.PENDING,
    createdAt: nowIso(),
    metadata: {},
  };
}

function configuredSubmitEvents(input: {
  workflowConfig: WorkflowConfig;
  proposedChanges: ProposedChangeRecord[];
  preflight: PreflightOutput;
  approvalTask?: ApprovalTaskRecord;
  changeRequest: ChangeRequestRecord;
}): Array<{
  eventType: string;
  approvalTaskId?: string;
  transactionPlanId?: string;
  payload: Record<string, unknown>;
}> {
  return input.workflowConfig.submit.additionalEvents.map((eventType) => {
    if (eventType === LEDGER_EVENT_TYPES.PROPOSED_CHANGE_CREATED) {
      return {
        eventType,
        payload: { proposedChanges: input.proposedChanges },
      };
    }

    if (eventType === LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED) {
      return {
        eventType,
        ...(input.approvalTask !== undefined
          ? { approvalTaskId: input.approvalTask.approvalTaskId }
          : {}),
        payload: { approvalTask: input.approvalTask },
      };
    }

    if (eventType === LEDGER_EVENT_TYPES.CHANGE_REQUEST_SUBMITTED) {
      return {
        eventType,
        payload: { changeRequest: input.changeRequest },
      };
    }

    if (eventType === LEDGER_EVENT_TYPES.EVIDENCE_REQUESTED) {
      return {
        eventType,
        payload: { required: input.preflight.requiresEvidence },
      };
    }

    return {
      eventType,
      payload: input.preflight,
    };
  });
}

async function planApprovedChange(
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

function createTransactionPlan(input: {
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

function applyInternalTransactionWrites(
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

function applyProjectionPatches(input: {
  document: EmployeeProjectionDocument;
  patches: Record<string, unknown>[];
  allowedPatchPaths: string[];
}): Result<EmployeeProjectionDocument, AppError> {
  if (input.patches.length === 0) {
    return err(validationFailedError({ projectionPatches: "missing" }));
  }

  const updatedDocument = cloneJsonValue(input.document);

  for (const patch of input.patches) {
    const patchResult = parseProjectionPatch(patch, input.allowedPatchPaths);
    if (!patchResult.ok) {
      return patchResult;
    }

    const applyResult = setJsonPointerValue(updatedDocument, patchResult.value);
    if (!applyResult.ok) {
      return applyResult;
    }
  }

  return ok(updatedDocument);
}

function parseProjectionPatch(
  patch: Record<string, unknown>,
  allowedPatchPaths: string[],
): Result<
  {
    projection: string;
    operation: string;
    path: string;
    value: unknown;
  },
  AppError
> {
  const projection = stringField(patch, "projection");
  const operation = stringField(patch, "operation");
  const path = stringField(patch, "path");

  if (
    projection !== "employee" ||
    operation !== "replace" ||
    path === undefined ||
    !allowedPatchPaths.includes(path)
  ) {
    return err(
      validationFailedError({
        projection,
        operation,
        path,
        allowedPatchPaths,
      }),
    );
  }

  return ok({
    projection,
    operation,
    path,
    value: patch["value"],
  });
}

function setJsonPointerValue(
  document: EmployeeProjectionDocument,
  patch: {
    path: string;
    value: unknown;
  },
): Result<true, AppError> {
  const pathSegmentsResult = parseJsonPointerSegments(patch.path);
  if (!pathSegmentsResult.ok) {
    return pathSegmentsResult;
  }

  const pathSegments = pathSegmentsResult.value;
  const targetKey = pathSegments.at(-1);

  if (targetKey === undefined) {
    return err(validationFailedError({ path: patch.path }));
  }

  let parentValue: unknown = document;

  for (const pathSegment of pathSegments.slice(0, -1)) {
    if (typeof parentValue !== "object" || parentValue === null) {
      return err(validationFailedError({ path: patch.path, pathSegment }));
    }

    parentValue = (parentValue as Record<string, unknown>)[pathSegment];
  }

  if (typeof parentValue !== "object" || parentValue === null) {
    return err(validationFailedError({ path: patch.path, targetKey }));
  }

  (parentValue as Record<string, unknown>)[targetKey] = cloneJsonValue(patch.value);

  return ok(true);
}

function parseJsonPointerSegments(path: string): Result<string[], AppError> {
  if (!path.startsWith("/")) {
    return err(validationFailedError({ path }));
  }

  const pathSegments = path
    .slice(1)
    .split("/")
    .filter((segment) => segment.length > 0)
    .map((segment) => {
      return segment.replace(/~1/g, "/").replace(/~0/g, "~");
    });

  if (pathSegments.length === 0) {
    return err(validationFailedError({ path }));
  }

  return ok(pathSegments);
}

function cloneJsonValue<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function createIntegrationOutboxRows(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  changeRequest: ChangeRequestRecord,
  transactionPlan: TransactionPlanRecord,
) {
  const outboxRows = [];

  for (const externalWrite of transactionPlan.externalWrites) {
    const outboxResult = repositories.integrationOutbox.create({
      outboxId: makeId("outbox"),
      tenantId: requestContext.tenantId,
      changeRequestId: changeRequest.changeRequestId,
      transactionPlanId: transactionPlan.transactionPlanId,
      destination: stringField(externalWrite, "connectionId") ?? "unknown",
      operation: stringField(externalWrite, "operation") ?? "unknown",
      requestPayload: objectField(externalWrite, "payload") ?? {},
      status: "pending",
      attemptCount: 0,
      maxAttempts: 3,
      idempotencyKey:
        stringField(externalWrite, "idempotencyKey") ??
        `outbox_${changeRequest.changeRequestId}`,
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

function findChangeRequestForWorkflow(
  repositories: Repositories,
  workflowInstance: WorkflowInstanceRecord,
): Result<ChangeRequestRecord, AppError> {
  if (workflowInstance.changeRequestId === undefined) {
    return err(validationFailedError({ changeRequestId: "missing" }));
  }

  return repositories.changeRequests.findById(workflowInstance.changeRequestId);
}

function maybeCancelChangeRequest(
  repositories: Repositories,
  actorId: string,
  workflowInstance: WorkflowInstanceRecord,
): Result<ChangeRequestRecord | undefined, AppError> {
  if (workflowInstance.changeRequestId === undefined) {
    return ok(undefined);
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

  return updateResult;
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

  const businessLedgerInput = {
    eventType: input.eventType,
    workflowInstance: input.workflowInstance,
    subjectType: input.workflowInstance.subjectType,
    subjectId: input.workflowInstance.subjectId,
    idempotencyKey: input.idempotencyKey,
    payload: input.payload,
  };
  const businessLedgerResult = appendWorkflowLedgerEvent(repositories, requestContext, {
    ...businessLedgerInput,
    ...(input.approvalTaskId !== undefined
      ? { approvalTaskId: input.approvalTaskId }
      : {}),
    ...(input.transactionPlanId !== undefined
      ? { transactionPlanId: input.transactionPlanId }
      : {}),
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
    permissionSnapshot: createPermissionSnapshot(requestContext.actor),
    aiVisibilitySnapshot: {},
    payload: input.payload,
    ...(requestContext.actor.roles[0] !== undefined
      ? { actorRole: requestContext.actor.roles[0] }
      : {}),
  });
}

function parseTimelineView(view?: string): Result<TimelineView, AppError> {
  if (view === undefined || view.trim() === "") {
    return ok(timelineViews.BUSINESS);
  }

  const normalizedView = view.trim().toLowerCase();
  const allowedViews = Object.values(timelineViews);

  if (!allowedViews.includes(normalizedView as TimelineView)) {
    return err(
      validationFailedError({
        view,
        allowedViews,
      }),
    );
  }

  return ok(normalizedView as TimelineView);
}

function buildTimelineView(
  ledgerEvents: LedgerEventRecord[],
  view: TimelineView,
  workflowConfig: WorkflowConfig,
): Record<string, unknown>[] {
  if (view === timelineViews.AUDIT) {
    return ledgerEvents;
  }

  if (view === timelineViews.DEBUG) {
    return ledgerEvents
      .filter((event) => debugTimelineEventTypes.has(event.eventType))
      .map((event) => createDebugTimelineEntry(event));
  }

  const businessTimelineEventTypes = new Set<string>(
    workflowConfig.timeline.businessEvents,
  );

  return ledgerEvents
    .filter((event) => businessTimelineEventTypes.has(event.eventType))
    .map((event) => createBusinessTimelineEntry(event, workflowConfig));
}

function createBusinessTimelineEntry(
  event: LedgerEventRecord,
  workflowConfig: WorkflowConfig,
): Record<string, unknown> {
  return {
    eventType: event.eventType,
    occurredAt: event.occurredAt,
    actorId: event.actorId,
    summary: businessSummaryForEvent(event, workflowConfig),
    payloadExcerpt: businessPayloadExcerpt(event),
  };
}

function createDebugTimelineEntry(event: LedgerEventRecord): Record<string, unknown> {
  return {
    eventType: event.eventType,
    occurredAt: event.occurredAt,
    actorId: event.actorId,
    idempotencyKey: event.idempotencyKey,
    payloadExcerpt: event.payload,
  };
}

function businessSummaryForEvent(
  event: LedgerEventRecord,
  workflowConfig: WorkflowConfig,
): string {
  return workflowConfig.timeline.summaries[event.eventType] ?? event.eventType;
}

function businessPayloadExcerpt(event: LedgerEventRecord): Record<string, unknown> {
  if (event.eventType === LEDGER_EVENT_TYPES.PERSON_LEGAL_NAME_CHANGED) {
    return {
      previousLegalName: event.payload["previousLegalName"],
      newLegalName: event.payload["newLegalName"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMPLOYEE_EMERGENCY_CONTACT_UPDATED) {
    return {
      changedEmergencyContact: event.payload["changedEmergencyContact"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED) {
    const outboxRows = event.payload["outboxRows"];

    return {
      outboxRequestCount: Array.isArray(outboxRows) ? outboxRows.length : 0,
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EVIDENCE_PROVIDED) {
    return {
      documentId: event.payload["documentId"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED) {
    const approvalTask = objectField(event.payload, "approvalTask");

    return {
      approvalTaskId: approvalTask?.["approvalTaskId"],
      assigneeRole: approvalTask?.["assigneeRole"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.NAME_CHANGE_PREFLIGHTED) {
    return {
      valid: event.payload["valid"],
      riskLevel: event.payload["riskLevel"],
      requiresEvidence: event.payload["requiresEvidence"],
      requiresApproval: event.payload["requiresApproval"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMERGENCY_CONTACT_PREFLIGHTED) {
    return {
      valid: event.payload["valid"],
      riskLevel: event.payload["riskLevel"],
      requiresEvidence: event.payload["requiresEvidence"],
      requiresApproval: event.payload["requiresApproval"],
    };
  }

  return {};
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

function terminalInteraction(status: string) {
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

  if (trimmedValue.length === 0) {
    return undefined;
  }

  return trimmedValue;
}

function numberField(
  object: Record<string, unknown>,
  fieldName: string,
): number | undefined {
  const value = object[fieldName];

  return typeof value === "number" ? value : undefined;
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
