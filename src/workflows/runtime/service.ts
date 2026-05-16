import {
  APPROVAL_TASK_STATUSES,
  CHANGE_REQUEST_STATUSES,
  DOCUMENT_CLASSIFICATIONS,
  LEDGER_EVENT_TYPES,
  WORKFLOW_ROUTE_KEYS,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  err,
  invalidWorkflowTransitionError,
  ok,
  validationFailedError,
  versionConflictError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  createDemoDocumentRecord,
  createInitialWorkflowInstance,
  makeId,
  nowIso,
  type ApprovalGroupRecord,
  type ApprovalTaskRecord,
  type ChangeRequestRecord,
  type EmployeeProjectionDocument,
  type EmployeeProjectionRecord,
  type ProposedChangeRecord,
  type Repositories,
  type TransactionPlanRecord,
  type WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import { findAccessGrantsForActor } from "../shared/access-context.js";
import {
  canViewEmployee,
  filterEmployeeProjectionForActor,
} from "../shared/employee-access.js";
import { objectField, stringField, valueAtDotPath } from "../shared/json-fields.js";
import { buildTimelineView, parseTimelineView } from "../shared/timeline-view.js";
import {
  buildConfiguredInteraction,
  findWorkflowActionConfig,
  resolveWorkflowString,
  resolveWorkflowTemplate,
  type WorkflowAiReviewNodeConfig,
  type WorkflowConfig,
  type WorkflowGraphOutcomeConfig,
  type WorkflowApprovalNodeConfig,
  type WorkflowTemplateSources,
} from "../shared/workflow-config.js";
import {
  resolveCurrentPublishedWorkflowConfig,
  resolvePinnedWorkflowConfig,
  shouldLoadEmployeeProjectionForStart,
} from "../shared/workflow-config-resolution.js";
import {
  appendAdditionalWorkflowEvents,
  appendTransitionLedgerEvents,
  appendWorkflowLedgerEvent,
  type AdditionalWorkflowLedgerEvent,
} from "../shared/workflow-ledger-events.js";
import {
  serializeWorkflowInstance,
  terminalInteraction,
} from "../shared/workflow-response.js";
import {
  completeTransitionAttempt,
  createTransitionAttempt,
  replayTransitionAttempt,
} from "../shared/workflow-transition-attempts.js";
import {
  cancelPendingGateTasks,
  evaluateApprovalGroup,
  findApprovalGateNodeForState,
  openApprovalGate,
  openGateSequenceTasks,
  type OpenApprovalGateResult,
} from "./approval-gates.js";
import { executeSynchronousExternalWrites } from "./external-writes.js";
import {
  advanceWorkflowGraph,
  contextWithGraphAdvance,
  initialGraphRuntimeContext,
  routeKeyForWorkflowTransition,
  type WorkflowGraphAdvanceResult,
} from "./graph-runtime.js";
import {
  parseApprovalDecisionInput,
  parseEvidenceInput,
  parseTransitionBody,
} from "./input-parsers.js";
import {
  actionRequiresApprovalTask,
  actorOwnsApprovalTask,
  canStartConfiguredWorkflow,
  canSubmitConfiguredTransition,
  canViewConfiguredWorkflow,
  computeConfiguredAvailableActions,
  createPermissionSnapshot,
} from "./permissions.js";
import {
  approvalTaskForWorkflowConfig,
  approvedChangeRequestUpdate,
  createConfiguredApprovalTask,
  createConfiguredChangeRequest,
  createConfiguredProposedChange,
  createTransactionPlan,
} from "./records.js";
import {
  applyInternalTransactionWrites,
  createIntegrationOutboxRows,
  planApprovedChange,
} from "./transactions.js";
import type {
  AdditionalLedgerEventInput,
  ApprovalDecisionInput,
  ExternalWriteExecution,
  PreflightOutput,
  RuntimeTransitionInput,
} from "./types.js";

const genericEvidencePurpose = "workflow_evidence";

/**
 * Starts a configured workflow through the workflow-intent command surface.
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

  if (intent === undefined || subjectId === undefined) {
    dependencies.logger?.warn("workflow intent validation failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      intent: intent ?? undefined,
      subjectType: subjectType ?? undefined,
      subjectId: subjectId ?? undefined,
    });
    return err(
      validationFailedError({
        intent,
        subjectType,
        subjectId,
      }),
    );
  }

  dependencies.logger?.info("workflow intent started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    correlationId: requestContext.correlationId,
    intent,
    subjectType: subjectType ?? undefined,
    subjectId,
  });

  const workflowConfigResult = resolveCurrentPublishedWorkflowConfig(
    repositories,
    requestContext.tenantId,
    requestContext.environmentId,
    intent,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const workflowConfig = workflowConfigResult.value.workflowConfig;

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

  const permissionResult = canStartConfiguredWorkflow({
    actor: requestContext.actor,
    workflowConfig,
    subjectId,
  });
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const employeeProjectionResult = shouldLoadEmployeeProjectionForStart(workflowConfig)
    ? repositories.employeeProjections.findByEmployeeId(
        requestContext.tenantId,
        subjectId,
      )
    : ok(undefined);
  if (!employeeProjectionResult.ok) {
    return employeeProjectionResult;
  }

  const initialInteractionResult = buildConfiguredInteraction({
    workflowConfig,
    interactionKey: "input",
    ...(employeeProjectionResult.value !== undefined
      ? { employeeDocument: employeeProjectionResult.value.document }
      : {}),
  });
  if (!initialInteractionResult.ok) {
    return initialInteractionResult;
  }

  const workflowInstance = createInitialWorkflowInstance({
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
    workflowDefinitionId: workflowConfigResult.value.workflowDefinitionId,
    workflowVersionId: workflowConfigResult.value.workflowVersionId,
    intent: workflowConfig.intent,
    subjectType: workflowConfig.subjectType,
    subjectId,
    requesterActorId: requestContext.actor.actorId,
    currentInteraction: initialInteractionResult.value,
    context: {
      ...initialGraphRuntimeContext(workflowConfig),
      ...(workflowConfig.graph?.startNodeId !== undefined
        ? { activeNodeId: workflowConfig.graph.startNodeId }
        : {}),
    },
    correlationId: requestContext.correlationId,
    metadata: { runtime: "configured_generic" },
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

  dependencies.logger?.info("workflow intent created", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowInstanceId: workflowInstance.workflowInstanceId,
    workflowVersionId: workflowInstance.workflowVersionId,
    intent,
    initialState: workflowInstance.state,
  });

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

  const workflowConfigResult = resolvePinnedWorkflowConfig(
    dependencies.repositories,
    workflowResult.value,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const pendingTasksResult = pendingTasksForActor(
    dependencies.repositories,
    requestContext,
    workflowInstanceId,
  );
  if (!pendingTasksResult.ok) {
    return pendingTasksResult;
  }

  const permissionResult = canViewConfiguredWorkflow({
    actor: requestContext.actor,
    workflowConfig: workflowConfigResult.value,
    workflowInstance: workflowResult.value,
    pendingTasks: pendingTasksResult.value,
  });
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

  const workflowConfigResult = resolvePinnedWorkflowConfig(
    repositories,
    workflowResult.value,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const pendingTasksResult = pendingTasksForActor(
    repositories,
    requestContext,
    workflowInstanceId,
  );
  if (!pendingTasksResult.ok) {
    return pendingTasksResult;
  }

  const permissionResult = canViewConfiguredWorkflow({
    actor: requestContext.actor,
    workflowConfig: workflowConfigResult.value,
    workflowInstance: workflowResult.value,
    pendingTasks: pendingTasksResult.value,
  });
  if (!permissionResult.ok) {
    return permissionResult;
  }

  return ok({
    workflowInstanceId,
    version: workflowResult.value.version,
    state: workflowResult.value.state,
    actions: computeConfiguredAvailableActions({
      actor: requestContext.actor,
      workflowConfig: workflowConfigResult.value,
      workflowInstance: workflowResult.value,
      pendingTasks: pendingTasksResult.value,
    }),
  });
}

/**
 * Creates a document record and attaches it to a workflow instance.
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

  const workflowConfigResult = resolvePinnedWorkflowConfig(
    repositories,
    workflowResult.value,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const pendingTasksResult = pendingTasksForActor(
    repositories,
    requestContext,
    workflowInstanceId,
  );
  if (!pendingTasksResult.ok) {
    return pendingTasksResult;
  }

  const permissionResult = canViewConfiguredWorkflow({
    actor: requestContext.actor,
    workflowConfig: workflowConfigResult.value,
    workflowInstance: workflowResult.value,
    pendingTasks: pendingTasksResult.value,
  });
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const documentRecord = createDemoDocumentRecord({
    tenantId: requestContext.tenantId,
    purpose: stringField(body, "purpose") ?? genericEvidencePurpose,
    filename,
    contentType,
    classification:
      stringField(body, "classification") ??
      DOCUMENT_CLASSIFICATIONS.SENSITIVE_PERSON_IDENTITY,
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

  if (documentResult.value.ownerActorId === requestContext.actor.actorId) {
    return ok({ document: documentResult.value });
  }

  const workflowResult = dependencies.repositories.workflows.findInstanceById(
    documentResult.value.workflowInstanceId,
  );
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const workflowPermissionResult = getWorkflowInstance(
    dependencies,
    requestContext,
    workflowResult.value.workflowInstanceId,
  );
  if (!workflowPermissionResult.ok) {
    return workflowPermissionResult;
  }

  return ok({ document: documentResult.value });
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

  const workflowConfigResult = resolvePinnedWorkflowConfig(
    repositories,
    workflowResult.value,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const pendingTasksResult = pendingTasksForActor(
    repositories,
    requestContext,
    workflowInstanceId,
  );
  if (!pendingTasksResult.ok) {
    return pendingTasksResult;
  }

  const permissionResult = canViewConfiguredWorkflow({
    actor: requestContext.actor,
    workflowConfig: workflowConfigResult.value,
    workflowInstance: workflowResult.value,
    pendingTasks: pendingTasksResult.value,
  });
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

  if (workflowConfigResult.value.subjectType !== "worker") {
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

  const accessGrantsResult = findAccessGrantsForActor(
    dependencies,
    requestContext.actor,
  );
  if (!accessGrantsResult.ok) {
    return accessGrantsResult;
  }

  const employeeProjectionResult = repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    workflowResult.value.subjectId,
  );
  if (!employeeProjectionResult.ok) {
    return employeeProjectionResult;
  }

  return ok({
    workflowInstanceId,
    view: timelineViewResult.value,
    totalLedgerEventCount: timelineResult.value.length,
    events: buildTimelineView(
      timelineResult.value,
      timelineViewResult.value,
      workflowConfigResult.value,
      {
        actor: requestContext.actor,
        targetProjection: employeeProjectionResult.value,
        access: accessGrantsResult.value,
      },
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
    dependencies.logger?.warn("workflow transition body parse failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowInstanceId,
      errorCode: transitionBodyResult.error.code,
    });
    return transitionBodyResult;
  }

  dependencies.logger?.info("workflow transition started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    correlationId: requestContext.correlationId,
    workflowInstanceId,
    transition: transitionBodyResult.value.transition,
    idempotencyKey: transitionBodyResult.value.idempotencyKey,
    expectedVersion: transitionBodyResult.value.expectedVersion,
  });

  const workflowResult = repositories.workflows.findInstanceById(workflowInstanceId);
  if (!workflowResult.ok) {
    dependencies.logger?.warn("workflow instance not found", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowInstanceId,
      errorCode: workflowResult.error.code,
    });
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
    dependencies.logger?.info("replaying idempotent transition", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowInstanceId,
      idempotencyKey: transitionBodyResult.value.idempotencyKey,
    });
    return replayTransitionAttempt(idempotentReplayResult.value);
  }

  const workflowConfigResult = resolvePinnedWorkflowConfig(
    repositories,
    workflowResult.value,
  );
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

  const pendingTasksResult = pendingTasksForActor(
    repositories,
    requestContext,
    workflowInstanceId,
  );
  if (!pendingTasksResult.ok) {
    return pendingTasksResult;
  }

  const permissionResult = canSubmitConfiguredTransition({
    actor: requestContext.actor,
    actionConfig: actionConfigResult.value,
    workflowInstance: workflowResult.value,
    pendingTasks: pendingTasksResult.value,
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
  });

  if (!transitionResult.ok) {
    dependencies.logger?.warn("workflow transition failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowInstanceId,
      transition: transitionBodyResult.value.transition,
      errorCode: transitionResult.error.code,
    });
  } else {
    const resultBody = transitionResult.value;
    dependencies.logger?.info("workflow transition completed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowInstanceId,
      transition: transitionBodyResult.value.transition,
      newState: resultBody["state"],
      newStatus: resultBody["status"],
    });
  }

  const completedAttempt = completeTransitionAttempt(startedAttempt, transitionResult);
  const completedAttemptResult =
    repositories.workflows.saveTransitionAttempt(completedAttempt);

  if (!completedAttemptResult.ok) {
    return completedAttemptResult;
  }

  return transitionResult;
}

/**
 * Reads an employee projection after applying employee-access filtering.
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

  const accessGrantsResult = findAccessGrantsForActor(
    dependencies,
    requestContext.actor,
  );
  if (!accessGrantsResult.ok) {
    return accessGrantsResult;
  }

  const permissionResult = canViewEmployee(
    requestContext.actor,
    projectionResult.value.document,
    "profile",
    accessGrantsResult.value,
  );
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const filteredProjectionResult = filterEmployeeProjectionForActor(
    requestContext.actor,
    projectionResult.value,
    accessGrantsResult.value,
  );
  if (!filteredProjectionResult.ok) {
    return filteredProjectionResult;
  }

  return ok({
    projection: filteredProjectionResult.value,
  });
}

/**
 * Lists employee projections visible to the current actor.
 */
export function listEmployeeProjections(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): Result<Record<string, unknown>, AppError> {
  const projectionsResult = dependencies.repositories.employeeProjections.findByTenant(
    requestContext.tenantId,
  );

  if (!projectionsResult.ok) {
    return projectionsResult;
  }

  const accessGrantsResult = findAccessGrantsForActor(
    dependencies,
    requestContext.actor,
  );
  if (!accessGrantsResult.ok) {
    return accessGrantsResult;
  }

  const visibleProjections: EmployeeProjectionRecord[] = [];

  for (const projection of projectionsResult.value) {
    const permissionResult = canViewEmployee(
      requestContext.actor,
      projection.document,
      "profile",
      accessGrantsResult.value,
    );

    if (!permissionResult.ok) {
      continue;
    }

    const filteredProjectionResult = filterEmployeeProjectionForActor(
      requestContext.actor,
      projection,
      accessGrantsResult.value,
    );
    if (!filteredProjectionResult.ok) {
      return filteredProjectionResult;
    }

    visibleProjections.push(filteredProjectionResult.value);
  }

  return ok({
    employees: visibleProjections,
  });
}

async function executeTransition(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
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
  input: RuntimeTransitionInput,
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const employeeProjectionResult = loadWorkflowEmployeeProjection(
    repositories,
    requestContext,
    input.workflowConfig,
    input.workflowInstance,
  );
  if (!employeeProjectionResult.ok) {
    return employeeProjectionResult;
  }

  const templateSources = workflowTemplateSources({
    input: input.transitionBody.input,
    workflowInstance: input.workflowInstance,
    ...(employeeProjectionResult.value !== undefined
      ? { employeeDocument: employeeProjectionResult.value.document }
      : {}),
  });
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

  const preflightInput = normalizedBlockInput(
    input.workflowConfig.submit.preflightInput,
    preflightInputResult.value,
    input.transitionBody.input,
  );
  const preflightResult =
    await dependencies.executorClient.executeBlock<PreflightOutput>(
      {
        tenantId: requestContext.tenantId,
        environmentId: requestContext.environmentId,
        changeRequestId: "",
        workflowInstanceId: input.workflowInstance.workflowInstanceId,
        workflowVersionId: input.workflowInstance.workflowVersionId,
        block: input.workflowConfig.submit.preflightBlock,
        input: preflightInput,
        context: {
          actorId: requestContext.actor.actorId,
          effectiveAt: effectiveAtResult.value,
          permissions: createPermissionSnapshot(requestContext.actor),
          correlationId: requestContext.correlationId,
          idempotencyKey: input.transitionBody.idempotencyKey,
        },
      },
      dependencies.logger,
    );
  if (!preflightResult.ok) {
    return preflightResult;
  }
  if (preflightResult.value.output === undefined) {
    return err(validationFailedError({ preflight: "missing" }));
  }
  if (!preflightResult.value.output.valid) {
    return err(validationFailedError({ preflight: preflightResult.value.output }));
  }

  const timestamp = nowIso();
  const changeRequestResult = createConfiguredChangeRequest({
    workflowConfig: input.workflowConfig,
    requestContext,
    workflowInstance: input.workflowInstance,
    ...(employeeProjectionResult.value !== undefined
      ? { employeeDocument: employeeProjectionResult.value.document }
      : {}),
    transitionInput: input.transitionBody.input,
    preflight: preflightResult.value.output,
    effectiveAt: effectiveAtResult.value,
    businessReason: businessReasonResult.value,
    timestamp,
  });
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const createdChangeRequestResult = repositories.changeRequests.create(
    changeRequestResult.value,
  );
  if (!createdChangeRequestResult.ok) {
    return createdChangeRequestResult;
  }

  const proposedChangeResult = createConfiguredProposedChange({
    workflowConfig: input.workflowConfig,
    requestContext,
    workflowInstance: input.workflowInstance,
    ...(employeeProjectionResult.value !== undefined
      ? { employeeDocument: employeeProjectionResult.value.document }
      : {}),
    transitionInput: input.transitionBody.input,
    changeRequest: createdChangeRequestResult.value,
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
        approvalTaskForWorkflowConfig({
          workflowConfig: input.workflowConfig,
          tenantId: requestContext.tenantId,
          workflowInstanceId: input.workflowInstance.workflowInstanceId,
          changeRequestId: createdChangeRequestResult.value.changeRequestId,
        }),
      )
    : ok(undefined);
  if (!approvalTaskResult.ok) {
    return approvalTaskResult;
  }

  const graphAdvance = advanceGraphForTransition({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    transition: input.transitionBody.transition,
    sources: templateSources,
    automaticRouteKeysByNodeId: {
      ...preflightAutomaticRouteKeys(input.workflowConfig),
      ...aiReviewAutomaticRouteKeys(input.workflowConfig),
    },
  });
  const nextInteractionKey =
    graphAdvance.nextInteraction ?? input.actionConfig.nextInteraction ?? "input";
  const nextInteractionResult = buildNextInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: nextInteractionKey,
    ...(employeeProjectionResult.value !== undefined
      ? { employeeDocument: employeeProjectionResult.value.document }
      : {}),
    fallbackInteraction: input.workflowInstance.currentInteraction,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const updatedWorkflow = {
    ...input.workflowInstance,
    state:
      graphAdvance.nextState ??
      input.actionConfig.nextState ??
      input.workflowInstance.state,
    status:
      graphAdvance.nextStatus ??
      input.actionConfig.nextStatus ??
      input.workflowInstance.status,
    changeRequestId: createdChangeRequestResult.value.changeRequestId,
    currentInteraction: nextInteractionResult.value,
    context: {
      ...contextWithGraphAdvance({
        context: input.workflowInstance.context,
        advance: graphAdvance,
      }),
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

  await executeAiReviewNodes(dependencies, requestContext, {
    workflowConfig: input.workflowConfig,
    workflowInstance: updateWorkflowResult.value,
    traversedNodeIds: new Set(graphAdvance.completedNodes.map((n) => n.nodeId)),
    templateSources,
    idempotencyKey: input.transitionBody.idempotencyKey,
  });

  const openedGateResult = openGateForWorkflowState({
    repositories,
    requestContext,
    workflowConfig: input.workflowConfig,
    workflowInstance: updateWorkflowResult.value,
    changeRequestId: createdChangeRequestResult.value.changeRequestId,
  });
  if (!openedGateResult.ok) {
    return openedGateResult;
  }

  const stateApprovalTaskResult =
    approvalTaskResult.value === undefined && openedGateResult.value === undefined
      ? createApprovalTaskForWorkflowState({
          repositories,
          requestContext,
          workflowConfig: input.workflowConfig,
          workflowInstance: updateWorkflowResult.value,
          changeRequestId: createdChangeRequestResult.value.changeRequestId,
        })
      : ok(undefined);
  if (!stateApprovalTaskResult.ok) {
    return stateApprovalTaskResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: updateWorkflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_CREATED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    payload: {
      changeRequestId: createdChangeRequestResult.value.changeRequestId,
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
    [
      ...configuredSubmitEvents({
        workflowConfig: input.workflowConfig,
        proposedChanges: proposedChangesResult.value,
        preflight: preflightResult.value.output,
        changeRequest: createdChangeRequestResult.value,
        ...(approvalTaskResult.value !== undefined
          ? { approvalTask: approvalTaskResult.value }
          : {}),
        ...(openedGateResult.value !== undefined
          ? { approvalGate: openedGateResult.value }
          : {}),
      }),
      ...approvalTaskCreatedEvents(
        input.workflowConfig,
        updateWorkflowResult.value,
        stateApprovalTaskResult.value,
      ),
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(updateWorkflowResult.value));
}

function provideEvidence(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
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

  const changeRequestResult = findChangeRequestForWorkflow(
    repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const approvalTask = approvalTaskForWorkflowConfig({
    workflowConfig: input.workflowConfig,
    tenantId: requestContext.tenantId,
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    changeRequestId: changeRequestResult.value.changeRequestId,
  });
  const approvalTaskResult = repositories.approvals.create(approvalTask);
  if (!approvalTaskResult.ok) {
    return approvalTaskResult;
  }

  const updatedChangeRequestResult = repositories.changeRequests.update({
    ...changeRequestResult.value,
    status: CHANGE_REQUEST_STATUSES.IN_APPROVAL,
    submittedAt: nowIso(),
    updatedBy: requestContext.actor.actorId,
    version: changeRequestResult.value.version + 1,
  });
  if (!updatedChangeRequestResult.ok) {
    return updatedChangeRequestResult;
  }

  const graphAdvance = advanceGraphForTransition({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    transition: input.transitionBody.transition,
    sources: workflowTemplateSources({
      input: input.transitionBody.input,
      workflowInstance: input.workflowInstance,
    }),
  });
  const nextInteractionKey =
    graphAdvance.nextInteraction ?? input.actionConfig.nextInteraction ?? "input";
  const nextInteractionResult = buildNextInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: nextInteractionKey,
    fallbackInteraction: input.workflowInstance.currentInteraction,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state:
      graphAdvance.nextState ??
      input.actionConfig.nextState ??
      input.workflowInstance.state,
    status:
      graphAdvance.nextStatus ??
      input.actionConfig.nextStatus ??
      input.workflowInstance.status,
    currentInteraction: nextInteractionResult.value,
    context: {
      ...contextWithGraphAdvance({
        context: input.workflowInstance.context,
        advance: graphAdvance,
      }),
      evidenceDocumentId: documentResult.value.documentId,
      approvalTaskId: approvalTask.approvalTaskId,
    },
    version: input.workflowInstance.version + 1,
  });
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
      approvalTask: approvalTaskResult.value,
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
        payload: { approvalTask: approvalTaskResult.value },
      },
      {
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_SUBMITTED,
        payload: { changeRequest: updatedChangeRequestResult.value },
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
  input: RuntimeTransitionInput,
): Promise<Result<Record<string, unknown>, AppError>> {
  const decisionInputResult = parseApprovalDecisionInput(input.transitionBody.input);
  if (!decisionInputResult.ok) {
    return decisionInputResult;
  }

  const pendingTaskResult = verifyApprovalTaskForActor(
    dependencies.repositories,
    requestContext,
    input.workflowInstance,
    decisionInputResult.value,
  );
  if (!pendingTaskResult.ok) {
    return pendingTaskResult;
  }

  const approvedTaskResult = dependencies.repositories.approvals.update({
    ...pendingTaskResult.value,
    status: APPROVAL_TASK_STATUSES.APPROVED,
    decision: "approved",
    ...(decisionInputResult.value.comment !== undefined
      ? { comments: decisionInputResult.value.comment }
      : {}),
    decidedAt: nowIso(),
    taskVersion: (pendingTaskResult.value.taskVersion ?? 1) + 1,
  });
  if (!approvedTaskResult.ok) {
    return approvedTaskResult;
  }

  if (approvedTaskResult.value.approvalGroupId !== undefined) {
    return handleApprovalGateDecision(
      dependencies,
      requestContext,
      input,
      approvedTaskResult.value,
    );
  }

  if (
    input.actionConfig.nextState !== undefined &&
    input.actionConfig.nextState !== WORKFLOW_STATES.APPROVED
  ) {
    return advanceToNextApprovalState(
      dependencies,
      requestContext,
      input,
      approvedTaskResult.value,
    );
  }

  return approveFinalConfiguredChange(
    dependencies,
    requestContext,
    input,
    approvedTaskResult.value,
  );
}

function rejectChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
): Result<Record<string, unknown>, AppError> {
  const decisionInputResult = parseApprovalDecisionInput(input.transitionBody.input);
  if (!decisionInputResult.ok) {
    return decisionInputResult;
  }
  if (decisionInputResult.value.reason === undefined) {
    return err(validationFailedError({ reason: "Required for rejection." }));
  }

  const pendingTaskResult = verifyApprovalTaskForActor(
    dependencies.repositories,
    requestContext,
    input.workflowInstance,
    decisionInputResult.value,
  );
  if (!pendingTaskResult.ok) {
    return pendingTaskResult;
  }

  const rejectedTaskResult = dependencies.repositories.approvals.update({
    ...pendingTaskResult.value,
    status: APPROVAL_TASK_STATUSES.REJECTED,
    decision: "rejected",
    decisionReason: decisionInputResult.value.reason,
    ...(decisionInputResult.value.comment !== undefined
      ? { comments: decisionInputResult.value.comment }
      : {}),
    decidedAt: nowIso(),
    taskVersion: (pendingTaskResult.value.taskVersion ?? 1) + 1,
  });
  if (!rejectedTaskResult.ok) {
    return rejectedTaskResult;
  }

  if (rejectedTaskResult.value.approvalGroupId !== undefined) {
    return handleApprovalGateDecision(
      dependencies,
      requestContext,
      input,
      rejectedTaskResult.value,
    );
  }

  const changeRequestResult = findChangeRequestForWorkflow(
    dependencies.repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const changeRequestUpdateResult = dependencies.repositories.changeRequests.update({
    ...changeRequestResult.value,
    status: CHANGE_REQUEST_STATUSES.REJECTED,
    closedAt: nowIso(),
    updatedBy: requestContext.actor.actorId,
    version: changeRequestResult.value.version + 1,
  });
  if (!changeRequestUpdateResult.ok) {
    return changeRequestUpdateResult;
  }

  const graphAdvance = advanceGraphForTransition({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    transition: input.transitionBody.transition,
    sources: workflowTemplateSources({
      input: input.transitionBody.input,
      workflowInstance: input.workflowInstance,
      changeRequest: changeRequestResult.value,
    }),
  });
  const workflowResult = dependencies.repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: graphAdvance.nextState ?? WORKFLOW_STATES.REJECTED,
    status: graphAdvance.nextStatus ?? WORKFLOW_STATUSES.REJECTED,
    currentInteraction: terminalInteraction("rejected"),
    context: contextWithGraphAdvance({
      context: input.workflowInstance.context,
      advance: graphAdvance,
    }),
    completedAt: nowIso(),
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(
    dependencies.repositories,
    requestContext,
    {
      workflowInstance: workflowResult.value,
      previousState: input.workflowInstance.state,
      eventType: LEDGER_EVENT_TYPES.APPROVAL_REJECTED,
      idempotencyKey: input.transitionBody.idempotencyKey,
      approvalTaskId: rejectedTaskResult.value.approvalTaskId,
      payload: {
        approvalTask: rejectedTaskResult.value,
        changeRequest: changeRequestUpdateResult.value,
      },
    },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    dependencies.repositories,
    requestContext,
    workflowResult.value,
    input.transitionBody.idempotencyKey,
    [
      {
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_REJECTED,
        approvalTaskId: rejectedTaskResult.value.approvalTaskId,
        payload: { changeRequest: changeRequestUpdateResult.value },
      },
      {
        eventType: LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED,
        payload: { terminalState: WORKFLOW_STATES.REJECTED },
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
  input: RuntimeTransitionInput,
): Result<Record<string, unknown>, AppError> {
  const decisionInputResult = parseApprovalDecisionInput(input.transitionBody.input);
  if (!decisionInputResult.ok) {
    return decisionInputResult;
  }
  if (decisionInputResult.value.comment === undefined) {
    return err(validationFailedError({ comment: "Required for more information." }));
  }

  const pendingTaskResult = verifyApprovalTaskForActor(
    dependencies.repositories,
    requestContext,
    input.workflowInstance,
    decisionInputResult.value,
  );
  if (!pendingTaskResult.ok) {
    return pendingTaskResult;
  }

  const taskResult = dependencies.repositories.approvals.update({
    ...pendingTaskResult.value,
    status: APPROVAL_TASK_STATUSES.CANCELED,
    decision: "more_info_requested",
    comments: decisionInputResult.value.comment,
    decidedAt: nowIso(),
    taskVersion: (pendingTaskResult.value.taskVersion ?? 1) + 1,
  });
  if (!taskResult.ok) {
    return taskResult;
  }

  if (taskResult.value.approvalGroupId !== undefined) {
    return handleApprovalGateDecision(
      dependencies,
      requestContext,
      input,
      taskResult.value,
    );
  }

  const graphAdvance = advanceGraphForTransition({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    transition: input.transitionBody.transition,
    sources: workflowTemplateSources({
      input: input.transitionBody.input,
      workflowInstance: input.workflowInstance,
    }),
  });
  const nextInteractionKey =
    graphAdvance.nextInteraction ?? input.actionConfig.nextInteraction ?? "input";
  const nextInteractionResult = buildNextInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: nextInteractionKey,
    fallbackInteraction: input.workflowInstance.currentInteraction,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const workflowResult = dependencies.repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state:
      graphAdvance.nextState ??
      input.actionConfig.nextState ??
      input.workflowInstance.state,
    status:
      graphAdvance.nextStatus ??
      input.actionConfig.nextStatus ??
      input.workflowInstance.status,
    currentInteraction: nextInteractionResult.value,
    context: contextWithGraphAdvance({
      context: input.workflowInstance.context,
      advance: graphAdvance,
    }),
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(
    dependencies.repositories,
    requestContext,
    {
      workflowInstance: workflowResult.value,
      previousState: input.workflowInstance.state,
      eventType: LEDGER_EVENT_TYPES.MORE_INFORMATION_REQUESTED,
      idempotencyKey: input.transitionBody.idempotencyKey,
      approvalTaskId: taskResult.value.approvalTaskId,
      payload: {
        comment: decisionInputResult.value.comment,
        approvalTask: taskResult.value,
      },
    },
  );
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

function cancelWorkflow(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
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

  const graphAdvance = advanceGraphForTransition({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    transition: input.transitionBody.transition,
    sources: workflowTemplateSources({
      input: input.transitionBody.input,
      workflowInstance: input.workflowInstance,
      ...(changeRequestResult.value !== undefined
        ? { changeRequest: changeRequestResult.value }
        : {}),
    }),
  });
  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: graphAdvance.nextState ?? WORKFLOW_STATES.CANCELED,
    status: graphAdvance.nextStatus ?? WORKFLOW_STATUSES.CANCELED,
    currentInteraction: terminalInteraction("canceled"),
    context: contextWithGraphAdvance({
      context: input.workflowInstance.context,
      advance: graphAdvance,
    }),
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

async function executeApprovedChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const changeRequestResult = findChangeRequestForWorkflow(
    repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const transactionPlanResult = await findOrCreateTransactionPlan(
    dependencies,
    requestContext,
    input,
    changeRequestResult.value,
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

  const externalWriteExecutionsResult = await executeSynchronousExternalWrites(
    dependencies,
    repositories,
    requestContext,
    input.workflowConfig,
    input.workflowInstance,
    input.transitionBody.idempotencyKey,
    changeRequestResult.value,
    transactionPlanResult.value,
  );
  if (!externalWriteExecutionsResult.ok) {
    return externalWriteExecutionsResult;
  }
  if (externalWriteExecutionsResult.value.status === "routed") {
    return ok(
      serializeWorkflowInstance(externalWriteExecutionsResult.value.workflowInstance),
    );
  }

  const appliedWritesResult = applyInternalTransactionWrites(
    repositories,
    requestContext,
    input.workflowConfig,
    input.workflowInstance,
    changeRequestResult.value,
    transactionPlanResult.value,
  );
  if (!appliedWritesResult.ok) {
    return appliedWritesResult;
  }

  const outboxResult = createIntegrationOutboxRows(
    repositories,
    requestContext,
    changeRequestResult.value,
    transactionPlanResult.value,
    externalWriteExecutionsResult.value.executions,
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
      projectionVersion: appliedWritesResult.value.projectionVersion,
      outboxRows: outboxResult.value,
      externalWriteExecutions: externalWriteExecutionsResult.value.executions,
    },
    updatedBy: requestContext.actor.actorId,
  });
  if (!executedPlanResult.ok) {
    return executedPlanResult;
  }

  const graphAdvance = advanceGraphForExecution({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    transactionPlan: executedPlanResult.value,
    externalWriteExecutions: externalWriteExecutionsResult.value.executions,
  });
  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: graphAdvance.nextState ?? WORKFLOW_STATES.EXECUTED,
    status: graphAdvance.nextStatus ?? WORKFLOW_STATUSES.COMPLETED,
    currentInteraction: terminalInteraction("executed"),
    context: contextWithGraphAdvance({
      context: input.workflowInstance.context,
      advance: graphAdvance,
    }),
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
      externalWriteExecutions: externalWriteExecutionsResult.value.executions,
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
    executionSupplementalEvents({
      transactionPlan: transactionPlanResult.value,
      ...(appliedWritesResult.value.projectionVersion !== undefined
        ? { projectionVersion: appliedWritesResult.value.projectionVersion }
        : {}),
      outboxRows: outboxResult.value,
      externalWriteExecutions: externalWriteExecutionsResult.value.executions,
    }),
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

async function approveFinalConfiguredChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
  approvedTask: ApprovalTaskRecord,
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const changeRequestResult = findChangeRequestForWorkflow(
    repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const transactionPlanResult = await createPlannedTransaction(
    dependencies,
    requestContext,
    input,
    changeRequestResult.value,
  );
  if (!transactionPlanResult.ok) {
    return transactionPlanResult;
  }

  const updatedChangeRequestResult = repositories.changeRequests.update(
    approvedChangeRequestUpdate({
      changeRequest: changeRequestResult.value,
      transactionPlanId: transactionPlanResult.value.transactionPlanId,
      actorId: requestContext.actor.actorId,
    }),
  );
  if (!updatedChangeRequestResult.ok) {
    return updatedChangeRequestResult;
  }

  const employeeProjectionResult = loadWorkflowEmployeeProjection(
    repositories,
    requestContext,
    input.workflowConfig,
    input.workflowInstance,
  );
  if (!employeeProjectionResult.ok) {
    return employeeProjectionResult;
  }

  const graphAdvance = advanceGraphForTransition({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    transition: input.transitionBody.transition,
    sources: workflowTemplateSources({
      input: input.transitionBody.input,
      workflowInstance: input.workflowInstance,
      ...(employeeProjectionResult.value !== undefined
        ? { employeeDocument: employeeProjectionResult.value.document }
        : {}),
      changeRequest: changeRequestResult.value,
    }),
    automaticRouteKeysByNodeId: transactionPlanAutomaticRouteKeys(input.workflowConfig),
    stopBeforeNodeTypes: ["projection_write", "external_write", "data_write"],
  });
  const readyInteractionKey =
    graphAdvance.nextInteraction ??
    input.actionConfig.nextInteraction ??
    "readyToExecute";
  const readyInteractionResult = buildNextInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: readyInteractionKey,
    ...(employeeProjectionResult.value !== undefined
      ? { employeeDocument: employeeProjectionResult.value.document }
      : {}),
    fallbackInteraction: input.workflowInstance.currentInteraction,
  });
  if (!readyInteractionResult.ok) {
    return readyInteractionResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state:
      graphAdvance.nextState ??
      input.actionConfig.nextState ??
      WORKFLOW_STATES.APPROVED,
    status:
      graphAdvance.nextStatus ??
      input.actionConfig.nextStatus ??
      WORKFLOW_STATUSES.ACTIVE,
    currentInteraction: readyInteractionResult.value,
    context: {
      ...contextWithGraphAdvance({
        context: input.workflowInstance.context,
        advance: graphAdvance,
      }),
      transactionPlanId: transactionPlanResult.value.transactionPlanId,
    },
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.APPROVAL_GRANTED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    approvalTaskId: approvedTask.approvalTaskId,
    transactionPlanId: transactionPlanResult.value.transactionPlanId,
    payload: {
      approvalTask: approvedTask,
      transactionPlan: transactionPlanResult.value,
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
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
        payload: { changeRequest: updatedChangeRequestResult.value },
      },
      {
        eventType: LEDGER_EVENT_TYPES.TRANSACTION_PLAN_CREATED,
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
        payload: { transactionPlan: transactionPlanResult.value },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

function advanceToNextApprovalState(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
  approvedTask: ApprovalTaskRecord,
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const changeRequestResult = findChangeRequestForWorkflow(
    repositories,
    input.workflowInstance,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const graphAdvance = advanceGraphForTransition({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    transition: input.transitionBody.transition,
    sources: workflowTemplateSources({
      input: input.transitionBody.input,
      workflowInstance: input.workflowInstance,
      changeRequest: changeRequestResult.value,
    }),
  });
  const nextInteractionKey =
    graphAdvance.nextInteraction ?? input.actionConfig.nextInteraction ?? "input";
  const nextInteractionResult = buildNextInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: nextInteractionKey,
    fallbackInteraction: input.workflowInstance.currentInteraction,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state:
      graphAdvance.nextState ??
      input.actionConfig.nextState ??
      input.workflowInstance.state,
    status:
      graphAdvance.nextStatus ??
      input.actionConfig.nextStatus ??
      input.workflowInstance.status,
    currentInteraction: nextInteractionResult.value,
    context: contextWithGraphAdvance({
      context: input.workflowInstance.context,
      advance: graphAdvance,
    }),
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const nextApprovalTaskResult = createApprovalTaskForWorkflowState({
    repositories,
    requestContext,
    workflowConfig: input.workflowConfig,
    workflowInstance: workflowResult.value,
    changeRequestId: changeRequestResult.value.changeRequestId,
  });
  if (!nextApprovalTaskResult.ok) {
    return nextApprovalTaskResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: LEDGER_EVENT_TYPES.APPROVAL_GRANTED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    approvalTaskId: approvedTask.approvalTaskId,
    payload: {
      approvalTask: approvedTask,
      nextApprovalTask: nextApprovalTaskResult.value,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  const supplementalEvents: AdditionalWorkflowLedgerEvent[] =
    nextApprovalTaskResult.value === undefined
      ? []
      : approvalTaskCreatedEvents(
          input.workflowConfig,
          workflowResult.value,
          nextApprovalTaskResult.value,
        );
  const supplementalLedgerResult = appendAdditionalWorkflowEvents(
    repositories,
    requestContext,
    workflowResult.value,
    input.transitionBody.idempotencyKey,
    supplementalEvents,
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

function handleApprovalGateDecision(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
  decidedTask: ApprovalTaskRecord,
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const approvalGroupResult = repositories.approvalGroups.findById(
    decidedTask.approvalGroupId ?? "",
  );
  if (!approvalGroupResult.ok) {
    return approvalGroupResult;
  }

  const approvalTasksResult = repositories.approvals.findByApprovalGroup(
    approvalGroupResult.value.approvalGroupId,
  );
  if (!approvalTasksResult.ok) {
    return approvalTasksResult;
  }

  const gateDecisionResult = evaluateApprovalGroup({
    approvalGroup: approvalGroupResult.value,
    approvalTasks: approvalTasksResult.value,
  });
  if (!gateDecisionResult.ok) {
    return gateDecisionResult;
  }

  if (
    gateDecisionResult.value.outcome === "waiting" ||
    gateDecisionResult.value.outcome === "advance_sequence"
  ) {
    return recordNonTerminalGateDecision(
      dependencies,
      requestContext,
      input,
      approvalGroupResult.value,
      approvalTasksResult.value,
      decidedTask,
      gateDecisionResult.value.openSequenceIndexes,
    );
  }

  const outcomeResult = outcomeForGateDecision(
    input.workflowConfig,
    input.workflowInstance,
    gateDecisionResult.value.outcome,
  );
  if (!outcomeResult.ok) {
    return outcomeResult;
  }

  const canceledTasksResult = cancelPendingGateTasks({
    repositories,
    approvalTasks: approvalTasksResult.value.filter((approvalTask) => {
      return approvalTask.approvalTaskId !== decidedTask.approvalTaskId;
    }),
  });
  if (!canceledTasksResult.ok) {
    return canceledTasksResult;
  }

  const groupUpdateResult = repositories.approvalGroups.update({
    ...approvalGroupResult.value,
    status:
      gateDecisionResult.value.outcome === "passed"
        ? "passed"
        : gateDecisionResult.value.outcome === "repair"
          ? "repair"
          : "failed",
    ...(gateDecisionResult.value.outcome === "failed" ? { failedAt: nowIso() } : {}),
    ...(gateDecisionResult.value.outcome !== "failed" ? { completedAt: nowIso() } : {}),
    metadata: {
      ...approvalGroupResult.value.metadata,
      decision: gateDecisionResult.value,
    },
  });
  if (!groupUpdateResult.ok) {
    return groupUpdateResult;
  }

  return routeGateOutcome(
    dependencies,
    requestContext,
    input,
    groupUpdateResult.value,
    decidedTask,
    canceledTasksResult.value,
    outcomeResult.value,
  );
}

function recordNonTerminalGateDecision(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
  approvalGroup: ApprovalGroupRecord,
  approvalTasks: ApprovalTaskRecord[],
  decidedTask: ApprovalTaskRecord,
  openSequenceIndexes: number[],
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const openedTasksResult = openGateSequenceTasks({
    repositories,
    approvalGroup,
    workflowInstance: input.workflowInstance,
    openSequenceIndexes,
  });
  if (!openedTasksResult.ok) {
    return openedTasksResult;
  }

  const updatedGroupResult = repositories.approvalGroups.update({
    ...approvalGroup,
    currentSequenceIndex: openSequenceIndexes[0] ?? approvalGroup.currentSequenceIndex,
    metadata: {
      ...approvalGroup.metadata,
      decidedTaskIds: approvalTasks
        .filter((task) => task.status !== APPROVAL_TASK_STATUSES.PENDING)
        .map((task) => task.approvalTaskId),
    },
  });
  if (!updatedGroupResult.ok) {
    return updatedGroupResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const gateNode = findApprovalGateNodeForState(
    input.workflowInstance,
    input.workflowConfig.graph?.nodes,
  );
  const taskDecidedEventType =
    gateNode?.approvalGate?.events.taskDecided ??
    LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_DECIDED;

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: taskDecidedEventType,
    idempotencyKey: input.transitionBody.idempotencyKey,
    approvalTaskId: decidedTask.approvalTaskId,
    payload: {
      approvalTask: decidedTask,
      approvalGroup: updatedGroupResult.value,
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
    openedTasksResult.value.map((approvalTask) => {
      return {
        eventType:
          gateNode?.approvalGate?.events.taskCreated ??
          LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CREATED,
        approvalTaskId: approvalTask.approvalTaskId,
        payload: { approvalTask },
      };
    }),
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

function routeGateOutcome(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
  approvalGroup: ApprovalGroupRecord,
  decidedTask: ApprovalTaskRecord,
  canceledTasks: ApprovalTaskRecord[],
  outcome: WorkflowGraphOutcomeConfig,
): Result<Record<string, unknown>, AppError> {
  const repositories = dependencies.repositories;
  const graphAdvance = advanceGraphForRouteKey({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    routeKey: outcome.routeKey ?? outcome.outcome,
    sources: {
      input: input.transitionBody.input,
      workflow: input.workflowInstance,
      approvalGate: approvalGroup as unknown as Record<string, unknown>,
    },
  });
  const nextInteractionKey =
    graphAdvance.nextInteraction ?? outcome.nextInteraction ?? "input";
  const nextInteractionResult = buildNextInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: nextInteractionKey,
    fallbackInteraction: input.workflowInstance.currentInteraction,
  });
  if (!nextInteractionResult.ok) {
    return nextInteractionResult;
  }

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: graphAdvance.nextState ?? outcome.nextState ?? input.workflowInstance.state,
    status:
      graphAdvance.nextStatus ?? outcome.nextStatus ?? input.workflowInstance.status,
    currentInteraction: nextInteractionResult.value,
    context: contextWithGraphAdvance({
      context: input.workflowInstance.context,
      advance: graphAdvance,
    }),
    version: input.workflowInstance.version + 1,
  });
  if (!workflowResult.ok) {
    return workflowResult;
  }

  const nextGateResult = openGateForWorkflowState({
    repositories,
    requestContext,
    workflowConfig: input.workflowConfig,
    workflowInstance: workflowResult.value,
    changeRequestId: input.workflowInstance.changeRequestId ?? "",
  });
  if (!nextGateResult.ok) {
    return nextGateResult;
  }

  const ledgerResult = appendTransitionLedgerEvents(repositories, requestContext, {
    workflowInstance: workflowResult.value,
    previousState: input.workflowInstance.state,
    eventType: outcome.eventType ?? LEDGER_EVENT_TYPES.APPROVAL_GATE_PASSED,
    idempotencyKey: input.transitionBody.idempotencyKey,
    approvalTaskId: decidedTask.approvalTaskId,
    payload: {
      approvalTask: decidedTask,
      approvalGroup,
      canceledTasks,
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
      ...outcomeLedgerEvents(outcome, {
        approvalTask: decidedTask,
        approvalGroup,
        canceledTasks,
      }),
      ...approvalGateTaskCanceledEvents(
        input.workflowConfig,
        workflowResult.value,
        canceledTasks,
      ),
      ...gateOpenedEvents(
        input.workflowConfig,
        workflowResult.value,
        nextGateResult.value,
      ),
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(workflowResult.value));
}

async function findOrCreateTransactionPlan(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
  changeRequest: ChangeRequestRecord,
): Promise<Result<TransactionPlanRecord, AppError>> {
  if (changeRequest.transactionPlanId !== undefined) {
    return dependencies.repositories.transactionPlans.findById(
      changeRequest.transactionPlanId,
    );
  }

  const transactionPlanResult = await createPlannedTransaction(
    dependencies,
    requestContext,
    input,
    changeRequest,
  );
  if (!transactionPlanResult.ok) {
    return transactionPlanResult;
  }

  const changeRequestUpdateResult = dependencies.repositories.changeRequests.update(
    approvedChangeRequestUpdate({
      changeRequest,
      transactionPlanId: transactionPlanResult.value.transactionPlanId,
      actorId: requestContext.actor.actorId,
    }),
  );
  if (!changeRequestUpdateResult.ok) {
    return changeRequestUpdateResult;
  }

  return transactionPlanResult;
}

async function createPlannedTransaction(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: RuntimeTransitionInput,
  changeRequest: ChangeRequestRecord,
): Promise<Result<TransactionPlanRecord, AppError>> {
  const proposedChangesResult =
    dependencies.repositories.proposedChanges.findByChangeRequest(
      changeRequest.changeRequestId,
    );
  if (!proposedChangesResult.ok) {
    return proposedChangesResult;
  }

  const employeeProjectionResult = loadWorkflowEmployeeProjection(
    dependencies.repositories,
    requestContext,
    input.workflowConfig,
    input.workflowInstance,
  );
  if (!employeeProjectionResult.ok) {
    return employeeProjectionResult;
  }

  const planOutputResult = await planApprovedChange(dependencies, requestContext, {
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    changeRequest,
    proposedChanges: proposedChangesResult.value,
    ...(employeeProjectionResult.value !== undefined
      ? { employeeDocument: employeeProjectionResult.value.document }
      : {}),
    idempotencyKey: input.transitionBody.idempotencyKey,
  });
  if (!planOutputResult.ok) {
    return planOutputResult;
  }

  const transactionPlan = createTransactionPlan({
    tenantId: requestContext.tenantId,
    changeRequestId: changeRequest.changeRequestId,
    actorId: requestContext.actor.actorId,
    output: planOutputResult.value,
  });

  return dependencies.repositories.transactionPlans.create(transactionPlan);
}

function advanceGraphForTransition(input: {
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  transition: string;
  sources?: WorkflowTemplateSources | undefined;
  automaticRouteKeysByNodeId?: Record<string, string> | undefined;
  stopBeforeNodeTypes?: Array<"projection_write" | "external_write" | "data_write">;
}): WorkflowGraphAdvanceResult {
  return advanceGraphForRouteKey({
    workflowConfig: input.workflowConfig,
    workflowInstance: input.workflowInstance,
    routeKey: routeKeyForWorkflowTransition(input.transition),
    sources: input.sources,
    automaticRouteKeysByNodeId: input.automaticRouteKeysByNodeId,
    ...(input.stopBeforeNodeTypes !== undefined
      ? { stopBeforeNodeTypes: input.stopBeforeNodeTypes }
      : {}),
  });
}

function advanceGraphForRouteKey(input: {
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  routeKey?: string | undefined;
  sources?: WorkflowTemplateSources | undefined;
  automaticRouteKeysByNodeId?: Record<string, string> | undefined;
  stopBeforeNodeTypes?: Array<"projection_write" | "external_write" | "data_write">;
}): WorkflowGraphAdvanceResult {
  return advanceWorkflowGraph({
    workflowConfig: input.workflowConfig,
    workflowContext: input.workflowInstance.context,
    firstRouteKey: input.routeKey,
    sources: input.sources,
    automaticRouteKeysByNodeId: input.automaticRouteKeysByNodeId,
    stopBeforeNodeTypes: input.stopBeforeNodeTypes,
  });
}

function advanceGraphForExecution(input: {
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  transactionPlan: TransactionPlanRecord;
  externalWriteExecutions: ExternalWriteExecution[];
}): WorkflowGraphAdvanceResult {
  return advanceWorkflowGraph({
    workflowConfig: input.workflowConfig,
    workflowContext: input.workflowInstance.context,
    sources: {
      workflow: input.workflowInstance,
      transactionPlan: input.transactionPlan,
    },
    automaticRouteKeysByNodeId: executionAutomaticRouteKeys(input),
  });
}

function preflightAutomaticRouteKeys(
  workflowConfig: WorkflowConfig,
): Record<string, string> {
  return Object.fromEntries(
    (workflowConfig.graph?.nodes ?? [])
      .filter((node) => {
        return (
          node.type === "block" &&
          node.block?.name === workflowConfig.submit.preflightBlock.name
        );
      })
      .map((node) => [node.nodeId, WORKFLOW_ROUTE_KEYS.VALID]),
  );
}

function aiReviewAutomaticRouteKeys(
  workflowConfig: WorkflowConfig,
): Record<string, string> {
  return Object.fromEntries(
    (workflowConfig.graph?.nodes ?? [])
      .filter((node) => node.type === "ai_review")
      .map((node) => [node.nodeId, "completed"]),
  );
}

async function executeAiReviewNodes(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  options: {
    workflowConfig: WorkflowConfig;
    workflowInstance: WorkflowInstanceRecord;
    traversedNodeIds: Set<string>;
    templateSources: WorkflowTemplateSources;
    idempotencyKey: string;
  },
): Promise<void> {
  if (dependencies.aiClient === undefined) {
    return;
  }

  const aiReviewNodes = (options.workflowConfig.graph?.nodes ?? []).filter(
    (node): node is typeof node & { aiReview: WorkflowAiReviewNodeConfig } =>
      node.type === "ai_review" &&
      node.aiReview !== undefined &&
      options.traversedNodeIds.has(node.nodeId),
  );

  for (const node of aiReviewNodes) {
    const currentStateResult = resolveWorkflowTemplate(
      node.aiReview.currentStateTemplate,
      options.templateSources,
    );
    const proposedStateResult = resolveWorkflowTemplate(
      node.aiReview.proposedStateTemplate,
      options.templateSources,
    );

    const changeRequestId = options.workflowInstance.changeRequestId ?? "";

    const reviewResult = await dependencies.aiClient.generateChangeReview(
      {
        changeType: node.aiReview.changeType,
        currentState: currentStateResult.ok
          ? (currentStateResult.value as Record<string, unknown>)
          : {},
        proposedState: proposedStateResult.ok
          ? (proposedStateResult.value as Record<string, unknown>)
          : {},
        visibleFields: node.aiReview.visibleFields,
        workflowInstanceId: options.workflowInstance.workflowInstanceId,
        changeRequestId,
        correlationId: requestContext.correlationId,
        idempotencyKey: options.idempotencyKey + "_ai_" + node.nodeId,
      },
      dependencies.logger,
    );

    const eventType = reviewResult.ok
      ? LEDGER_EVENT_TYPES.AI_CHANGE_REVIEW_GENERATED
      : LEDGER_EVENT_TYPES.AI_CHANGE_REVIEW_FAILED;

    const payload: Record<string, unknown> = reviewResult.ok
      ? {
          nodeId: node.nodeId,
          changeType: node.aiReview.changeType,
          review: reviewResult.value,
        }
      : {
          nodeId: node.nodeId,
          changeType: node.aiReview.changeType,
          errorCode: reviewResult.error.code,
        };

    const ledgerResult = appendWorkflowLedgerEvent(
      dependencies.repositories,
      requestContext,
      {
        eventType,
        workflowInstance: options.workflowInstance,
        subjectType: options.workflowInstance.subjectType,
        subjectId: options.workflowInstance.subjectId,
        payload,
      },
    );

    if (!ledgerResult.ok) {
      dependencies.logger?.warn("ai review ledger event append failed", {
        nodeId: node.nodeId,
        errorCode: ledgerResult.error.code,
      });
    }
  }
}

function transactionPlanAutomaticRouteKeys(
  workflowConfig: WorkflowConfig,
): Record<string, string> {
  return Object.fromEntries(
    (workflowConfig.graph?.nodes ?? [])
      .filter((node) => node.type === "transaction_plan")
      .map((node) => [node.nodeId, WORKFLOW_ROUTE_KEYS.PLANNED]),
  );
}

function executionAutomaticRouteKeys(input: {
  workflowConfig: WorkflowConfig;
  externalWriteExecutions: ExternalWriteExecution[];
}): Record<string, string> {
  const routeKeys: Record<string, string> = {};

  for (const node of input.workflowConfig.graph?.nodes ?? []) {
    if (node.type === "projection_write" || node.type === "data_write") {
      routeKeys[node.nodeId] = WORKFLOW_ROUTE_KEYS.APPLIED;
      continue;
    }

    if (node.type === "ledger_event") {
      routeKeys[node.nodeId] = routeKeyIfConfigured(
        node,
        "completed",
        WORKFLOW_ROUTE_KEYS.RECORDED,
      );
      continue;
    }

    if (node.type === "external_write") {
      routeKeys[node.nodeId] =
        externalWriteOutcomeForNode(input.externalWriteExecutions, node) ??
        routeKeyIfConfigured(
          node,
          WORKFLOW_ROUTE_KEYS.REQUESTED,
          WORKFLOW_ROUTE_KEYS.ACCEPTED,
        );
    }
  }

  return routeKeys;
}

function routeKeyIfConfigured(
  node: { outcomes?: WorkflowGraphOutcomeConfig[] },
  preferredRouteKey: string,
  fallbackRouteKey: string,
): string {
  const hasPreferredRoute = node.outcomes?.some((outcome) => {
    return (
      outcome.routeKey === preferredRouteKey || outcome.outcome === preferredRouteKey
    );
  });

  return hasPreferredRoute === true ? preferredRouteKey : fallbackRouteKey;
}

function externalWriteOutcomeForNode(
  externalWriteExecutions: ExternalWriteExecution[],
  node: { connectionId?: string; operation?: string },
): string | undefined {
  return externalWriteExecutions.find((execution) => {
    return (
      execution.connectionId === node.connectionId &&
      execution.operation === node.operation
    );
  })?.outcome;
}

function verifyApprovalTaskForActor(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstance: WorkflowInstanceRecord,
  decisionInput: ApprovalDecisionInput,
): Result<ApprovalTaskRecord, AppError> {
  const pendingTasksResult = pendingTasksForActor(
    repositories,
    requestContext,
    workflowInstance.workflowInstanceId,
  );
  if (!pendingTasksResult.ok) {
    return pendingTasksResult;
  }

  const pendingTask = pendingTasksResult.value.find((task) => {
    return task.approvalTaskId === decisionInput.approvalTaskId;
  });

  if (
    pendingTask === undefined ||
    pendingTask.status !== APPROVAL_TASK_STATUSES.PENDING ||
    !actorOwnsApprovalTask(requestContext.actor, pendingTask)
  ) {
    return err(
      invalidWorkflowTransitionError({
        approvalTaskId: decisionInput.approvalTaskId,
        reason: "No pending approval task is available for this workflow.",
      }),
    );
  }

  if (
    decisionInput.taskVersion !== undefined &&
    decisionInput.taskVersion !== pendingTask.taskVersion
  ) {
    return err(
      versionConflictError({
        approvalTaskId: decisionInput.approvalTaskId,
        expectedTaskVersion: decisionInput.taskVersion,
        actualTaskVersion: pendingTask.taskVersion,
      }),
    );
  }

  return ok(pendingTask);
}

function pendingTasksForActor(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowInstanceId: string,
): Result<ApprovalTaskRecord[], AppError> {
  const tasksResult = repositories.approvals.findPendingForActor(requestContext.actor);

  if (!tasksResult.ok) {
    return tasksResult;
  }

  return ok(
    tasksResult.value.filter((task) => {
      return task.workflowInstanceId === workflowInstanceId;
    }),
  );
}

function loadWorkflowEmployeeProjection(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
): Result<EmployeeProjectionRecord | undefined, AppError> {
  if (!shouldLoadEmployeeProjectionForStart(workflowConfig)) {
    return ok(undefined);
  }

  return repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    workflowInstance.subjectId,
  );
}

function buildNextInteraction(input: {
  workflowConfig: WorkflowConfig;
  interactionKey: string;
  employeeDocument?: EmployeeProjectionDocument;
  fallbackInteraction: Record<string, unknown>;
}): Result<Record<string, unknown>, AppError> {
  if (input.workflowConfig.interactions[input.interactionKey] === undefined) {
    return ok(input.fallbackInteraction);
  }

  return buildConfiguredInteraction({
    workflowConfig: input.workflowConfig,
    interactionKey: input.interactionKey,
    ...(input.employeeDocument !== undefined
      ? { employeeDocument: input.employeeDocument }
      : {}),
  });
}

function workflowTemplateSources(input: {
  input: Record<string, unknown>;
  workflowInstance: WorkflowInstanceRecord;
  employeeDocument?: EmployeeProjectionDocument;
  changeRequest?: ChangeRequestRecord;
}): WorkflowTemplateSources {
  return {
    input: input.input,
    workflow: input.workflowInstance,
    ...(input.employeeDocument !== undefined
      ? { employee: input.employeeDocument }
      : {}),
    ...(input.changeRequest !== undefined
      ? { changeRequest: input.changeRequest }
      : {}),
  };
}

function normalizedBlockInput(
  configuredTemplate: Record<string, unknown>,
  resolvedInput: unknown,
  transitionInput: Record<string, unknown>,
): Record<string, unknown> {
  if (
    Object.keys(configuredTemplate).length === 0 &&
    typeof resolvedInput === "object" &&
    resolvedInput !== null &&
    Object.keys(resolvedInput as Record<string, unknown>).length === 0
  ) {
    return transitionInput;
  }

  return typeof resolvedInput === "object" && resolvedInput !== null
    ? (resolvedInput as Record<string, unknown>)
    : {};
}

function configuredSubmitEvents(input: {
  workflowConfig: WorkflowConfig;
  proposedChanges: ProposedChangeRecord[];
  preflight: PreflightOutput;
  approvalTask?: ApprovalTaskRecord;
  approvalGate?: OpenApprovalGateResult;
  changeRequest: ChangeRequestRecord;
}): AdditionalWorkflowLedgerEvent[] {
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

    if (eventType === LEDGER_EVENT_TYPES.APPROVAL_GATE_OPENED) {
      return {
        eventType,
        payload: { approvalGroup: input.approvalGate?.approvalGroup },
      };
    }

    if (eventType === LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CREATED) {
      return {
        eventType,
        payload: { approvalTasks: input.approvalGate?.approvalTasks ?? [] },
      };
    }

    return {
      eventType,
      payload: input.preflight as unknown as Record<string, unknown>,
    };
  });
}

function createApprovalTaskForWorkflowState(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  changeRequestId: string;
}): Result<ApprovalTaskRecord | undefined, AppError> {
  const stateConfig = input.workflowConfig.states[input.workflowInstance.state];
  const approvalAction = stateConfig?.actions.find(actionRequiresApprovalTask);

  if (approvalAction === undefined) {
    return ok(undefined);
  }

  const graphNode = input.workflowConfig.graph?.nodes.find((node) => {
    return node.state === input.workflowInstance.state && node.type === "approval";
  });
  const graphApproval = graphNode?.approval;
  const assignmentResult =
    graphApproval === undefined
      ? resolveLegacyApprovalAssignment(input)
      : resolveGraphApprovalAssignment({
          repositories: input.repositories,
          tenantId: input.requestContext.tenantId,
          workflowInstance: input.workflowInstance,
          graphApproval,
          fallbackActor: approvalAction.actor,
        });
  if (!assignmentResult.ok) {
    return assignmentResult;
  }

  const approvalTask = createConfiguredApprovalTask({
    tenantId: input.requestContext.tenantId,
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    changeRequestId: input.changeRequestId,
    assigneeActorId: assignmentResult.value.assigneeActorId,
    assigneeRole: assignmentResult.value.assigneeRole,
    approvalType:
      graphApproval?.approvalType ?? input.workflowConfig.approval.approvalType,
    ...(graphNode?.nodeId !== undefined ? { gateNodeId: graphNode.nodeId } : {}),
    assignmentMode: assignmentResult.value.assignmentMode,
  });

  return input.repositories.approvals.create(approvalTask);
}

function approvalTaskCreatedEvents(
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
  approvalTask?: ApprovalTaskRecord,
): AdditionalWorkflowLedgerEvent[] {
  if (approvalTask === undefined) {
    return [];
  }

  return [
    {
      eventType:
        approvalCreatedEventTypeForWorkflowState(workflowConfig, workflowInstance) ??
        LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED,
      approvalTaskId: approvalTask.approvalTaskId,
      payload: { approvalTask },
    },
  ];
}

function approvalCreatedEventTypeForWorkflowState(
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
): string | undefined {
  const approvalNode = workflowConfig.graph?.nodes.find((node) => {
    return node.type === "approval" && node.state === workflowInstance.state;
  });

  if (approvalNode === undefined) {
    return undefined;
  }

  const graphEdges = workflowConfig.graph?.edges ?? [];

  return graphEdges.find((edge) => {
    return edge.toNodeId === approvalNode.nodeId && edge.eventType !== undefined;
  })?.eventType;
}

function resolveLegacyApprovalAssignment(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
}): Result<
  {
    assigneeActorId: string;
    assigneeRole: string;
    assignmentMode: "actor" | "role";
  },
  AppError
> {
  const assigneeActorId = input.workflowConfig.approval.assigneeActorId;
  const assigneeRole = input.workflowConfig.approval.assigneeRole;

  if (assigneeActorId.trim().length > 0) {
    return ok({
      assigneeActorId,
      assigneeRole,
      assignmentMode: "actor",
    });
  }

  const actorResult = input.repositories.actors.findFirstActiveByRole(
    input.requestContext.tenantId,
    assigneeRole,
  );

  return ok({
    assigneeActorId: actorResult.ok ? actorResult.value.actorId : "",
    assigneeRole,
    assignmentMode: actorResult.ok ? "actor" : "role",
  });
}

function resolveGraphApprovalAssignment(input: {
  repositories: Repositories;
  tenantId: string;
  workflowInstance: WorkflowInstanceRecord;
  graphApproval: WorkflowApprovalNodeConfig;
  fallbackActor: string;
}): Result<
  {
    assigneeActorId: string;
    assigneeRole: string;
    assignmentMode: "actor" | "role";
  },
  AppError
> {
  const resolver = input.graphApproval.resolver;
  const configuredRole = resolver.role ?? input.fallbackActor;

  if (resolver.type === "actor" && resolver.actorId !== undefined) {
    return ok({
      assigneeActorId: resolver.actorId,
      assigneeRole: configuredRole,
      assignmentMode: "actor",
    });
  }

  if (resolver.type === "requester") {
    return ok({
      assigneeActorId: input.workflowInstance.requesterActorId,
      assigneeRole: configuredRole,
      assignmentMode: "actor",
    });
  }

  if (resolver.type === "manager") {
    return resolveManagerApprovalAssignment(input, configuredRole);
  }

  if (resolver.type === "workflow_field" && resolver.fieldPath !== undefined) {
    return resolveWorkflowFieldApprovalAssignment(input, configuredRole);
  }

  const role = resolver.role ?? stringField(resolver, "role") ?? input.fallbackActor;
  const actorResult = input.repositories.actors.findFirstActiveByRole(
    input.tenantId,
    role,
  );

  return ok({
    assigneeActorId: actorResult.ok ? actorResult.value.actorId : "",
    assigneeRole: role,
    assignmentMode: actorResult.ok ? "actor" : "role",
  });
}

function resolveManagerApprovalAssignment(
  input: {
    repositories: Repositories;
    tenantId: string;
    workflowInstance: WorkflowInstanceRecord;
    graphApproval: WorkflowApprovalNodeConfig;
  } & { fallbackActor: string },
  assigneeRole: string,
): Result<
  {
    assigneeActorId: string;
    assigneeRole: string;
    assignmentMode: "actor" | "role";
  },
  AppError
> {
  if (input.workflowInstance.subjectType !== "worker") {
    return err(
      validationFailedError({
        resolver: input.graphApproval.resolver.type,
        subjectType: input.workflowInstance.subjectType,
      }),
    );
  }

  const employeeProjectionResult =
    input.repositories.employeeProjections.findByEmployeeId(
      input.tenantId,
      input.workflowInstance.subjectId,
    );
  if (!employeeProjectionResult.ok) {
    return employeeProjectionResult;
  }

  const managerEmployeeId = employeeProjectionResult.value.document.manager.employeeId;
  if (managerEmployeeId === null) {
    return err(
      validationFailedError({
        resolver: input.graphApproval.resolver.type,
        managerEmployeeId,
      }),
    );
  }

  const managerActorResult = input.repositories.actors.findByLinkedWorkerId(
    input.tenantId,
    managerEmployeeId,
  );
  if (!managerActorResult.ok) {
    return managerActorResult;
  }

  return ok({
    assigneeActorId: managerActorResult.value.actorId,
    assigneeRole,
    assignmentMode: "actor",
  });
}

function resolveWorkflowFieldApprovalAssignment(
  input: {
    repositories: Repositories;
    tenantId: string;
    workflowInstance: WorkflowInstanceRecord;
    graphApproval: WorkflowApprovalNodeConfig;
  },
  assigneeRole: string,
): Result<
  {
    assigneeActorId: string;
    assigneeRole: string;
    assignmentMode: "actor" | "role";
  },
  AppError
> {
  const fieldPath = input.graphApproval.resolver.fieldPath;
  const fieldValue =
    fieldPath === undefined
      ? undefined
      : valueAtDotPath(input.workflowInstance.context, fieldPath);
  const fieldString = typeof fieldValue === "string" ? fieldValue : undefined;

  if (fieldString === undefined) {
    return err(
      validationFailedError({
        resolver: input.graphApproval.resolver.type,
        fieldPath,
        fieldValue,
      }),
    );
  }

  const actorByIdResult = input.repositories.actors.findById(fieldString);
  if (actorByIdResult.ok) {
    return ok({
      assigneeActorId: actorByIdResult.value.actorId,
      assigneeRole,
      assignmentMode: "actor",
    });
  }

  const actorByWorkerResult = input.repositories.actors.findByLinkedWorkerId(
    input.tenantId,
    fieldString,
  );
  if (!actorByWorkerResult.ok) {
    return actorByWorkerResult;
  }

  return ok({
    assigneeActorId: actorByWorkerResult.value.actorId,
    assigneeRole,
    assignmentMode: "actor",
  });
}

function openGateForWorkflowState(input: {
  repositories: Repositories;
  requestContext: ApiRequestContext;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  changeRequestId: string;
}): Result<OpenApprovalGateResult | undefined, AppError> {
  const gateNode = findApprovalGateNodeForState(
    input.workflowInstance,
    input.workflowConfig.graph?.nodes,
  );

  if (gateNode === undefined) {
    return ok(undefined);
  }

  return openApprovalGate({
    repositories: input.repositories,
    tenantId: input.requestContext.tenantId,
    workflowInstance: input.workflowInstance,
    changeRequestId: input.changeRequestId,
    gateNode,
  });
}

function gateOpenedEvents(
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
  approvalGate?: OpenApprovalGateResult,
): AdditionalWorkflowLedgerEvent[] {
  if (approvalGate === undefined) {
    return [];
  }

  const gateNode = findApprovalGateNodeForState(
    workflowInstance,
    workflowConfig.graph?.nodes,
  );

  return [
    {
      eventType:
        gateNode?.approvalGate?.events.opened ??
        LEDGER_EVENT_TYPES.APPROVAL_GATE_OPENED,
      payload: { approvalGroup: approvalGate.approvalGroup },
    },
    ...approvalGate.approvalTasks.map((approvalTask) => {
      return {
        eventType:
          gateNode?.approvalGate?.events.taskCreated ??
          LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CREATED,
        approvalTaskId: approvalTask.approvalTaskId,
        payload: { approvalTask },
      };
    }),
  ];
}

function approvalGateTaskCanceledEvents(
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
  canceledTasks: ApprovalTaskRecord[],
): AdditionalWorkflowLedgerEvent[] {
  if (canceledTasks.length === 0) {
    return [];
  }

  const gateNode = findApprovalGateNodeForState(
    workflowInstance,
    workflowConfig.graph?.nodes,
  );
  const eventType =
    gateNode?.approvalGate?.events.taskCanceled ??
    LEDGER_EVENT_TYPES.APPROVAL_GATE_TASK_CANCELED;

  return canceledTasks.map((approvalTask) => {
    return {
      eventType,
      approvalTaskId: approvalTask.approvalTaskId,
      payload: { approvalTask },
    };
  });
}

function outcomeLedgerEvents(
  outcome: WorkflowGraphOutcomeConfig,
  payload: Record<string, unknown>,
): AdditionalWorkflowLedgerEvent[] {
  return (outcome.ledgerEvents ?? []).map((eventConfig) => {
    return {
      eventType: eventConfig.eventType,
      payload,
    };
  });
}

function outcomeForGateDecision(
  workflowConfig: WorkflowConfig,
  workflowInstance: WorkflowInstanceRecord,
  decisionOutcome: string,
): Result<WorkflowGraphOutcomeConfig, AppError> {
  const gateNode = findApprovalGateNodeForState(
    workflowInstance,
    workflowConfig.graph?.nodes,
  );
  const outcomeName =
    decisionOutcome === "passed"
      ? "gate_passed"
      : decisionOutcome === "repair"
        ? "request_more_info"
        : "gate_failed";
  const outcome = gateNode?.outcomes?.find((candidate) => {
    return candidate.outcome === outcomeName;
  });

  if (outcome === undefined) {
    return err(
      validationFailedError({
        nodeId: gateNode?.nodeId,
        outcome: outcomeName,
      }),
    );
  }

  return ok(outcome);
}

function executionSupplementalEvents(input: {
  transactionPlan: TransactionPlanRecord;
  projectionVersion?: number;
  outboxRows: Record<string, unknown>[];
  externalWriteExecutions: Array<{ eventType?: string }>;
}): AdditionalLedgerEventInput[] {
  return [
    ...(input.projectionVersion !== undefined
      ? [
          {
            eventType: LEDGER_EVENT_TYPES.EMPLOYEE_PROJECTION_UPDATED,
            transactionPlanId: input.transactionPlan.transactionPlanId,
            payload: {
              projectionVersion: input.projectionVersion,
            },
          },
        ]
      : []),
    ...(input.outboxRows.length > 0
      ? [
          {
            eventType: LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED,
            transactionPlanId: input.transactionPlan.transactionPlanId,
            payload: { outboxRows: input.outboxRows },
          },
        ]
      : []),
    ...(input.externalWriteExecutions.length > 0
      ? [
          {
            eventType:
              input.externalWriteExecutions[0]?.eventType ??
              LEDGER_EVENT_TYPES.EXTERNAL_WRITE_SUCCEEDED,
            transactionPlanId: input.transactionPlan.transactionPlanId,
            payload: {
              externalWriteExecutions: input.externalWriteExecutions,
            },
          },
        ]
      : []),
    {
      eventType: LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED,
      transactionPlanId: input.transactionPlan.transactionPlanId,
      payload: { terminalState: WORKFLOW_STATES.EXECUTED },
    },
  ];
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

  return repositories.changeRequests.update({
    ...changeRequestResult.value,
    status: CHANGE_REQUEST_STATUSES.CANCELED,
    closedAt: nowIso(),
    updatedBy: actorId,
    version: changeRequestResult.value.version + 1,
  });
}
