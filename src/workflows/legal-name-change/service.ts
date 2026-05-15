import {
  ACTOR_TYPES,
  APPROVAL_TASK_STATUSES,
  CHANGE_REQUEST_STATUSES,
  CHANGE_REQUEST_TYPES,
  DOCUMENT_CLASSIFICATIONS,
  LEDGER_EVENT_TYPES,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  WORKFLOW_TRANSITIONS,
  err,
  idempotencyConflictError,
  invalidWorkflowTransitionError,
  ok,
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
  type ApprovalTaskRecord,
  type ChangeRequestRecord,
  type EmergencyContact,
  type EmployeeProjectionDocument,
  type LedgerEventRecord,
  type LegalName,
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
  canStartEmergencyContactWorkflow,
  canStartLegalNameWorkflow,
  canSubmitTransition,
  canViewDocument,
  canViewWorkflowInstance,
  computeAvailableActions,
  createPermissionSnapshot,
} from "./permissions.js";

const legalNamePreflightBlock = {
  name: "system.employee_data.legal_name.preflight",
  version: "1.0.0",
} as const;

const legalNamePlanTransactionBlock = {
  name: "system.employee_data.legal_name.plan_transaction",
  version: "1.0.0",
} as const;

const emergencyContactPreflightBlock = {
  name: "system.employee_data.emergency_contact.preflight",
  version: "1.0.0",
} as const;

const emergencyContactPlanTransactionBlock = {
  name: "system.employee_data.emergency_contact.plan_transaction",
  version: "1.0.0",
} as const;

const identityEvidencePurpose = "legal_name_change_evidence";

const supportedEmployeeDataIntents = [
  WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
  WORKFLOW_INTENTS.EMPLOYEE_EMERGENCY_CONTACT_UPDATE,
] as const;

type SupportedEmployeeDataIntent = (typeof supportedEmployeeDataIntents)[number];

type LegalNameInput = {
  newLegalName: LegalName;
  effectiveAt: string;
  businessReason: string;
};

type EmergencyContactInput = {
  proposedEmergencyContact: EmergencyContact;
  effectiveAt: string;
  businessReason: string;
};

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

const businessTimelineEventTypes = new Set<string>([
  LEDGER_EVENT_TYPES.WORKFLOW_INTENT_STARTED,
  LEDGER_EVENT_TYPES.CHANGE_REQUEST_CREATED,
  LEDGER_EVENT_TYPES.NAME_CHANGE_PREFLIGHTED,
  LEDGER_EVENT_TYPES.EMERGENCY_CONTACT_PREFLIGHTED,
  LEDGER_EVENT_TYPES.EVIDENCE_PROVIDED,
  LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED,
  LEDGER_EVENT_TYPES.APPROVAL_GRANTED,
  LEDGER_EVENT_TYPES.TRANSACTION_PLAN_CREATED,
  LEDGER_EVENT_TYPES.PERSON_LEGAL_NAME_CHANGED,
  LEDGER_EVENT_TYPES.EMPLOYEE_EMERGENCY_CONTACT_UPDATED,
  LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED,
  LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED,
]);

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

  if (
    !isSupportedEmployeeDataIntent(intent) ||
    subjectType !== "worker" ||
    subjectId === undefined
  ) {
    return err(
      validationFailedError({
        intent,
        subjectType,
        subjectId,
        expectedIntents: supportedEmployeeDataIntents,
      }),
    );
  }

  const permissionResult = canStartEmployeeDataWorkflow(
    requestContext.actor,
    intent,
    subjectId,
  );
  if (!permissionResult.ok) {
    return permissionResult;
  }

  const workflowVersionResult = repositories.workflows.findVersionByIntent(intent);
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

  const workflowInstance = createInitialWorkflowInstance({
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
    workflowDefinitionId: workflowVersionResult.value.workflowDefinitionId,
    workflowVersionId: workflowVersionResult.value.workflowVersionId,
    intent,
    subjectType,
    subjectId,
    requesterActorId: requestContext.actor.actorId,
    currentInteraction: createInitialInteractionForIntent(
      intent,
      employeeProjectionResult.value.document,
    ),
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

  return ok({
    workflowInstanceId,
    version: workflowResult.value.version,
    state: workflowResult.value.state,
    actions: computeAvailableActions({
      actor: requestContext.actor,
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

  return ok({
    workflowInstanceId,
    view: timelineViewResult.value,
    totalLedgerEventCount: timelineResult.value.length,
    events: buildTimelineView(timelineResult.value, timelineViewResult.value),
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

  const permissionResult = canSubmitTransition({
    actor: requestContext.actor,
    workflowInstance: workflowResult.value,
    transition: transitionBodyResult.value.transition,
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

async function executeTransition(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
    pendingApprovalTask?: ApprovalTaskRecord;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  if (input.transitionBody.transition === WORKFLOW_TRANSITIONS.SUBMIT_INPUT) {
    if (
      input.workflowInstance.intent ===
      WORKFLOW_INTENTS.EMPLOYEE_EMERGENCY_CONTACT_UPDATE
    ) {
      return submitEmergencyContactInput(dependencies, requestContext, input);
    }

    return submitLegalNameInput(dependencies, requestContext, input);
  }

  if (input.transitionBody.transition === WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE) {
    return provideEvidence(dependencies, requestContext, input);
  }

  if (input.transitionBody.transition === WORKFLOW_TRANSITIONS.APPROVE) {
    return approveChange(dependencies, requestContext, input);
  }

  if (input.transitionBody.transition === WORKFLOW_TRANSITIONS.REJECT) {
    return rejectChange(dependencies, requestContext, input);
  }

  if (input.transitionBody.transition === WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO) {
    return requestMoreInformation(dependencies, requestContext, input);
  }

  if (input.transitionBody.transition === WORKFLOW_TRANSITIONS.CANCEL) {
    return cancelWorkflow(dependencies, requestContext, input);
  }

  if (input.transitionBody.transition === WORKFLOW_TRANSITIONS.EXECUTE) {
    return executeApprovedChange(dependencies, requestContext, input);
  }

  return err(
    invalidWorkflowTransitionError({
      transition: input.transitionBody.transition,
    }),
  );
}

async function submitLegalNameInput(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const legalNameInputResult = parseLegalNameInput(input.transitionBody.input);

  if (!legalNameInputResult.ok) {
    return legalNameInputResult;
  }

  const projectionResult = repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    input.workflowInstance.subjectId,
  );
  if (!projectionResult.ok) {
    return projectionResult;
  }

  const preflightResult =
    await dependencies.executorClient.executeBlock<PreflightOutput>({
      tenantId: requestContext.tenantId,
      environmentId: requestContext.environmentId,
      changeRequestId: "",
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      block: legalNamePreflightBlock,
      input: {
        currentLegalName: projectionResult.value.document.person.legalName,
        proposedLegalName: legalNameInputResult.value.newLegalName,
        effectiveAt: legalNameInputResult.value.effectiveAt,
        businessReason: legalNameInputResult.value.businessReason,
      },
      context: {
        actorId: requestContext.actor.actorId,
        effectiveAt: legalNameInputResult.value.effectiveAt,
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
  const changeRequest = createLegalNameChangeRequest({
    requestContext,
    workflowInstance: input.workflowInstance,
    employeeDocument: projectionResult.value.document,
    legalNameInput: legalNameInputResult.value,
    preflight: preflightResult.value.output,
    timestamp,
  });
  const changeRequestResult = repositories.changeRequests.create(changeRequest);
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const proposedChangesResult = repositories.proposedChanges.createMany([
    {
      proposedChangeId: makeId("pchg"),
      tenantId: requestContext.tenantId,
      changeRequestId: changeRequest.changeRequestId,
      targetObjectType: "person",
      targetObjectId: projectionResult.value.document.person.personId,
      fieldPath: "person.legalName",
      currentValue: projectionResult.value.document.person.legalName,
      proposedValue: legalNameInputResult.value.newLegalName,
      effectiveAt: legalNameInputResult.value.effectiveAt,
      reasonCode: legalNameInputResult.value.businessReason,
      validationStatus: "valid",
      riskLevel: preflightResult.value.output.riskLevel,
      metadata: {},
      createdAt: timestamp,
      updatedAt: timestamp,
    },
  ]);
  if (!proposedChangesResult.ok) {
    return proposedChangesResult;
  }

  const updatedWorkflow = {
    ...input.workflowInstance,
    state: WORKFLOW_STATES.COLLECTING_EVIDENCE,
    status: WORKFLOW_STATUSES.ACTIVE,
    changeRequestId: changeRequest.changeRequestId,
    currentInteraction: legalNameEvidenceInteraction(),
    context: {
      ...input.workflowInstance.context,
      legalNameInput: legalNameInputResult.value,
      preflight: preflightResult.value.output,
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
    [
      {
        eventType: LEDGER_EVENT_TYPES.PROPOSED_CHANGE_CREATED,
        payload: {
          proposedChanges: proposedChangesResult.value,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.NAME_CHANGE_PREFLIGHTED,
        payload: preflightResult.value.output,
      },
      {
        eventType: LEDGER_EVENT_TYPES.EVIDENCE_REQUESTED,
        payload: {
          required: preflightResult.value.output.requiresEvidence,
        },
      },
    ],
  );
  if (!supplementalLedgerResult.ok) {
    return supplementalLedgerResult;
  }

  return ok(serializeWorkflowInstance(updateWorkflowResult.value));
}

async function submitEmergencyContactInput(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowInstance: WorkflowInstanceRecord;
    transitionBody: TransitionBody;
  },
): Promise<Result<Record<string, unknown>, AppError>> {
  const repositories = dependencies.repositories;
  const emergencyContactInputResult = parseEmergencyContactInput(
    input.transitionBody.input,
  );

  if (!emergencyContactInputResult.ok) {
    return emergencyContactInputResult;
  }

  const projectionResult = repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    input.workflowInstance.subjectId,
  );
  if (!projectionResult.ok) {
    return projectionResult;
  }

  const preflightResult =
    await dependencies.executorClient.executeBlock<PreflightOutput>({
      tenantId: requestContext.tenantId,
      environmentId: requestContext.environmentId,
      changeRequestId: "",
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      block: emergencyContactPreflightBlock,
      input: {
        currentEmergencyContacts: projectionResult.value.document.emergencyContacts,
        proposedEmergencyContact:
          emergencyContactInputResult.value.proposedEmergencyContact,
        effectiveAt: emergencyContactInputResult.value.effectiveAt,
        businessReason: emergencyContactInputResult.value.businessReason,
      },
      context: {
        actorId: requestContext.actor.actorId,
        effectiveAt: emergencyContactInputResult.value.effectiveAt,
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
  const changeRequest = createEmergencyContactChangeRequest({
    requestContext,
    workflowInstance: input.workflowInstance,
    employeeDocument: projectionResult.value.document,
    emergencyContactInput: emergencyContactInputResult.value,
    preflight: preflightResult.value.output,
    timestamp,
  });
  const changeRequestResult = repositories.changeRequests.create(changeRequest);
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  const proposedChangesResult = repositories.proposedChanges.createMany([
    {
      proposedChangeId: makeId("pchg"),
      tenantId: requestContext.tenantId,
      changeRequestId: changeRequest.changeRequestId,
      targetObjectType: "worker",
      targetObjectId: projectionResult.value.employeeId,
      fieldPath: `emergencyContacts.${emergencyContactInputResult.value.proposedEmergencyContact.contactId}`,
      currentValue: projectionResult.value.document.emergencyContacts,
      proposedValue: emergencyContactInputResult.value.proposedEmergencyContact,
      effectiveAt: emergencyContactInputResult.value.effectiveAt,
      reasonCode: emergencyContactInputResult.value.businessReason,
      validationStatus: "valid",
      riskLevel: preflightResult.value.output.riskLevel,
      metadata: {},
      createdAt: timestamp,
      updatedAt: timestamp,
    },
  ]);
  if (!proposedChangesResult.ok) {
    return proposedChangesResult;
  }

  const approvalTask = createEmergencyContactApprovalTask({
    tenantId: requestContext.tenantId,
    workflowInstanceId: input.workflowInstance.workflowInstanceId,
    changeRequestId: changeRequest.changeRequestId,
  });
  const approvalTaskResult = repositories.approvals.create(approvalTask);
  if (!approvalTaskResult.ok) {
    return approvalTaskResult;
  }

  const updatedWorkflow = {
    ...input.workflowInstance,
    state: WORKFLOW_STATES.WAITING_APPROVAL,
    status: WORKFLOW_STATUSES.WAITING,
    changeRequestId: changeRequest.changeRequestId,
    currentInteraction: legalNameWaitingInteraction(),
    context: {
      ...input.workflowInstance.context,
      emergencyContactInput: emergencyContactInputResult.value,
      preflight: preflightResult.value.output,
      approvalTaskId: approvalTask.approvalTaskId,
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
    [
      {
        eventType: LEDGER_EVENT_TYPES.PROPOSED_CHANGE_CREATED,
        payload: {
          proposedChanges: proposedChangesResult.value,
        },
      },
      {
        eventType: LEDGER_EVENT_TYPES.EMERGENCY_CONTACT_PREFLIGHTED,
        payload: preflightResult.value.output,
      },
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
          changeRequest,
        },
      },
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
  input: {
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

  const approvalTask = createLegalNameApprovalTask({
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

  const updatedWorkflow = {
    ...input.workflowInstance,
    state: WORKFLOW_STATES.WAITING_APPROVAL,
    status: WORKFLOW_STATUSES.WAITING,
    currentInteraction: legalNameWaitingInteraction(),
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

  const updatedWorkflow = {
    ...input.workflowInstance,
    state: WORKFLOW_STATES.APPROVED,
    status: WORKFLOW_STATUSES.ACTIVE,
    currentInteraction: readyToExecuteInteractionForIntent(
      input.workflowInstance.intent,
    ),
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

  const workflowResult = repositories.workflows.updateInstance({
    ...input.workflowInstance,
    state: WORKFLOW_STATES.COLLECTING_EVIDENCE,
    status: WORKFLOW_STATUSES.ACTIVE,
    currentInteraction: legalNameEvidenceInteraction(),
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

function parseLegalNameInput(
  input: Record<string, unknown>,
): Result<LegalNameInput, AppError> {
  const newLegalNameResult = parseLegalNameValue(input["newLegalName"]);
  const effectiveAt = stringField(input, "effectiveAt");
  const businessReason = stringField(input, "businessReason");

  if (!newLegalNameResult.ok) {
    return newLegalNameResult;
  }
  if (effectiveAt === undefined || businessReason === undefined) {
    return err(validationFailedError({ effectiveAt, businessReason }));
  }

  return ok({
    newLegalName: newLegalNameResult.value,
    effectiveAt,
    businessReason,
  });
}

function parseEmergencyContactInput(
  input: Record<string, unknown>,
): Result<EmergencyContactInput, AppError> {
  const proposedContactResult = parseEmergencyContactValue(
    input["proposedEmergencyContact"],
  );
  const effectiveAt = stringField(input, "effectiveAt");
  const businessReason = stringField(input, "businessReason");

  if (!proposedContactResult.ok) {
    return proposedContactResult;
  }
  if (effectiveAt === undefined || businessReason === undefined) {
    return err(validationFailedError({ effectiveAt, businessReason }));
  }

  return ok({
    proposedEmergencyContact: proposedContactResult.value,
    effectiveAt,
    businessReason,
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

function parseLegalNameValue(value: unknown): Result<LegalName, AppError> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return err(validationFailedError({ legalName: "Expected an object." }));
  }

  const objectValue = value as Record<string, unknown>;
  const first = stringField(objectValue, "first");
  const middle = nullableStringField(objectValue, "middle");
  const last = stringField(objectValue, "last");

  if (first === undefined || last === undefined) {
    return err(validationFailedError({ first, last }));
  }

  return ok({
    first,
    middle,
    last,
  });
}

function parseEmergencyContactValue(
  value: unknown,
): Result<EmergencyContact, AppError> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return err(validationFailedError({ emergencyContact: "Expected an object." }));
  }

  const objectValue = value as Record<string, unknown>;
  const contactId = stringField(objectValue, "contactId") ?? makeId("ec");
  const name = stringField(objectValue, "name");
  const relationship = stringField(objectValue, "relationship");
  const phone = stringField(objectValue, "phone");
  const email = nullableStringField(objectValue, "email");
  const priority = numberField(objectValue, "priority");

  if (
    name === undefined ||
    relationship === undefined ||
    phone === undefined ||
    priority === undefined
  ) {
    return err(
      validationFailedError({
        name,
        relationship,
        phone,
        priority,
      }),
    );
  }

  return ok({
    contactId,
    name,
    relationship,
    phone,
    email,
    priority,
  });
}

function parseEmergencyContactArray(
  value: unknown,
): Result<EmergencyContact[], AppError> {
  if (!Array.isArray(value)) {
    return err(validationFailedError({ emergencyContacts: "Expected an array." }));
  }

  const contacts: EmergencyContact[] = [];

  for (const contactValue of value) {
    const contactResult = parseEmergencyContactValue(contactValue);
    if (!contactResult.ok) {
      return contactResult;
    }

    contacts.push(contactResult.value);
  }

  return ok(contacts);
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

function createLegalNameChangeRequest(input: {
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  employeeDocument: EmployeeProjectionDocument;
  legalNameInput: LegalNameInput;
  preflight: PreflightOutput;
  timestamp: string;
}): ChangeRequestRecord {
  return {
    changeRequestId: makeId("chg"),
    tenantId: input.requestContext.tenantId,
    environmentId: input.requestContext.environmentId,
    changeType: CHANGE_REQUEST_TYPES.EMPLOYEE_DATA_CHANGE,
    targetWorkerId: input.workflowInstance.subjectId,
    requesterActorId: input.requestContext.actor.actorId,
    effectiveAt: input.legalNameInput.effectiveAt,
    businessReason: input.legalNameInput.businessReason,
    status: CHANGE_REQUEST_STATUSES.PREFLIGHTED,
    priority: "normal",
    currentSnapshot: {
      legalName: input.employeeDocument.person.legalName,
    },
    proposedSnapshot: {
      legalName: input.legalNameInput.newLegalName,
    },
    preflightResult: input.preflight,
    aiReview: {
      mode: "not_configured_v0",
      summary:
        "Legal-name workflow V0 uses deterministic preflight. AI review plugs into this envelope later.",
    },
    workflowDefinitionId: input.workflowInstance.workflowDefinitionId,
    workflowVersionId: input.workflowInstance.workflowVersionId,
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
    createdBy: input.requestContext.actor.actorId,
    updatedBy: input.requestContext.actor.actorId,
    version: 1,
    metadata: {},
  };
}

function createLegalNameApprovalTask(input: {
  tenantId: string;
  workflowInstanceId: string;
  changeRequestId: string;
}): ApprovalTaskRecord {
  return {
    approvalTaskId: makeId("appr"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequestId,
    workflowInstanceId: input.workflowInstanceId,
    assigneeActorId: "actor_hr_admin",
    assigneeRole: "hr_admin",
    approvalType: "hr_legal_name_review",
    status: APPROVAL_TASK_STATUSES.PENDING,
    createdAt: nowIso(),
    metadata: {},
  };
}

function createEmergencyContactChangeRequest(input: {
  requestContext: ApiRequestContext;
  workflowInstance: WorkflowInstanceRecord;
  employeeDocument: EmployeeProjectionDocument;
  emergencyContactInput: EmergencyContactInput;
  preflight: PreflightOutput;
  timestamp: string;
}): ChangeRequestRecord {
  return {
    changeRequestId: makeId("chg"),
    tenantId: input.requestContext.tenantId,
    environmentId: input.requestContext.environmentId,
    changeType: CHANGE_REQUEST_TYPES.EMPLOYEE_DATA_CHANGE,
    targetWorkerId: input.workflowInstance.subjectId,
    requesterActorId: input.requestContext.actor.actorId,
    effectiveAt: input.emergencyContactInput.effectiveAt,
    businessReason: input.emergencyContactInput.businessReason,
    status: CHANGE_REQUEST_STATUSES.IN_APPROVAL,
    priority: "normal",
    currentSnapshot: {
      emergencyContacts: input.employeeDocument.emergencyContacts,
    },
    proposedSnapshot: {
      emergencyContact: input.emergencyContactInput.proposedEmergencyContact,
    },
    preflightResult: input.preflight,
    aiReview: {
      mode: "not_configured_v0",
      summary:
        "Emergency-contact workflow V0 uses deterministic preflight. AI review plugs into this envelope later.",
    },
    workflowDefinitionId: input.workflowInstance.workflowDefinitionId,
    workflowVersionId: input.workflowInstance.workflowVersionId,
    submittedAt: input.timestamp,
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
    createdBy: input.requestContext.actor.actorId,
    updatedBy: input.requestContext.actor.actorId,
    version: 1,
    metadata: {},
  };
}

function createEmergencyContactApprovalTask(input: {
  tenantId: string;
  workflowInstanceId: string;
  changeRequestId: string;
}): ApprovalTaskRecord {
  return {
    approvalTaskId: makeId("appr"),
    tenantId: input.tenantId,
    changeRequestId: input.changeRequestId,
    workflowInstanceId: input.workflowInstanceId,
    assigneeActorId: "actor_hr_admin",
    assigneeRole: "hr_admin",
    approvalType: "hr_emergency_contact_review",
    status: APPROVAL_TASK_STATUSES.PENDING,
    createdAt: nowIso(),
    metadata: {},
  };
}

async function planApprovedChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowInstance: WorkflowInstanceRecord;
    changeRequest: ChangeRequestRecord;
    proposedChanges: ProposedChangeRecord[];
    employeeDocument: EmployeeProjectionDocument;
    idempotencyKey: string;
  },
): Promise<Result<PlanTransactionOutput, AppError>> {
  if (
    input.workflowInstance.intent === WORKFLOW_INTENTS.EMPLOYEE_EMERGENCY_CONTACT_UPDATE
  ) {
    return planEmergencyContactChange(dependencies, requestContext, input);
  }

  return planLegalNameChange(dependencies, requestContext, input);
}

async function planLegalNameChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowInstance: WorkflowInstanceRecord;
    changeRequest: ChangeRequestRecord;
    proposedChanges: ProposedChangeRecord[];
    employeeDocument: EmployeeProjectionDocument;
    idempotencyKey: string;
  },
): Promise<Result<PlanTransactionOutput, AppError>> {
  const proposedName = input.proposedChanges[0]?.proposedValue;
  const legalNameResult = parseLegalNameValue(proposedName);
  if (!legalNameResult.ok) {
    return legalNameResult;
  }

  const planResult =
    await dependencies.executorClient.executeBlock<PlanTransactionOutput>({
      tenantId: requestContext.tenantId,
      environmentId: requestContext.environmentId,
      changeRequestId: input.changeRequest.changeRequestId,
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      block: legalNamePlanTransactionBlock,
      input: {
        changeRequestId: input.changeRequest.changeRequestId,
        workerId: input.workflowInstance.subjectId,
        personId: input.employeeDocument.person.personId,
        currentLegalName: input.employeeDocument.person.legalName,
        proposedLegalName: legalNameResult.value,
        effectiveAt: input.changeRequest.effectiveAt,
      },
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

async function planEmergencyContactChange(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  input: {
    workflowInstance: WorkflowInstanceRecord;
    changeRequest: ChangeRequestRecord;
    proposedChanges: ProposedChangeRecord[];
    employeeDocument: EmployeeProjectionDocument;
    idempotencyKey: string;
  },
): Promise<Result<PlanTransactionOutput, AppError>> {
  const proposedContact = input.proposedChanges[0]?.proposedValue;
  const emergencyContactResult = parseEmergencyContactValue(proposedContact);
  if (!emergencyContactResult.ok) {
    return emergencyContactResult;
  }

  const planResult =
    await dependencies.executorClient.executeBlock<PlanTransactionOutput>({
      tenantId: requestContext.tenantId,
      environmentId: requestContext.environmentId,
      changeRequestId: input.changeRequest.changeRequestId,
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      block: emergencyContactPlanTransactionBlock,
      input: {
        changeRequestId: input.changeRequest.changeRequestId,
        workerId: input.workflowInstance.subjectId,
        currentEmergencyContacts: input.employeeDocument.emergencyContacts,
        proposedEmergencyContact: emergencyContactResult.value,
        effectiveAt: input.changeRequest.effectiveAt,
      },
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
  changeRequest: ChangeRequestRecord,
  transactionPlan: TransactionPlanRecord,
) {
  const firstWrite = transactionPlan.internalWrites[0];
  const eventType = stringField(firstWrite ?? {}, "eventType");

  if (eventType === LEDGER_EVENT_TYPES.EMPLOYEE_EMERGENCY_CONTACT_UPDATED) {
    return applyInternalEmergencyContactWrites(
      repositories,
      requestContext,
      changeRequest,
      transactionPlan,
    );
  }

  return applyInternalLegalNameWrites(
    repositories,
    requestContext,
    changeRequest,
    transactionPlan,
  );
}

function applyInternalLegalNameWrites(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  changeRequest: ChangeRequestRecord,
  transactionPlan: TransactionPlanRecord,
) {
  const firstWrite = transactionPlan.internalWrites[0];
  const payload = objectField(firstWrite ?? {}, "payload");
  const newLegalNameResult = parseLegalNameValue(payload?.["newLegalName"]);

  if (!newLegalNameResult.ok) {
    return newLegalNameResult;
  }

  const projectionResult = repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    changeRequest.targetWorkerId,
  );
  if (!projectionResult.ok) {
    return projectionResult;
  }

  const legalNameChangedEvent = repositories.ledger.append({
    tenantId: requestContext.tenantId,
    eventType: LEDGER_EVENT_TYPES.PERSON_LEGAL_NAME_CHANGED,
    subjectType: "worker",
    subjectId: changeRequest.targetWorkerId,
    occurredAt: nowIso(),
    effectiveAt: changeRequest.effectiveAt,
    actorType: requestContext.actor.actorType,
    actorId: requestContext.actor.actorId,
    relationshipContext: {},
    changeRequestId: changeRequest.changeRequestId,
    transactionPlanId: transactionPlan.transactionPlanId,
    correlationId: requestContext.correlationId,
    permissionSnapshot: createPermissionSnapshot(requestContext.actor),
    aiVisibilitySnapshot: {},
    payload: payload ?? {},
  });
  if (!legalNameChangedEvent.ok) {
    return legalNameChangedEvent;
  }

  const updatedDocument: EmployeeProjectionDocument = {
    ...projectionResult.value.document,
    person: {
      ...projectionResult.value.document.person,
      legalName: newLegalNameResult.value,
      displayName: displayNameFromLegalName(newLegalNameResult.value),
    },
  };

  return repositories.employeeProjections.updateDocument(
    requestContext.tenantId,
    changeRequest.targetWorkerId,
    updatedDocument,
    legalNameChangedEvent.value.eventId,
    legalNameChangedEvent.value.eventSequence,
  );
}

function applyInternalEmergencyContactWrites(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  changeRequest: ChangeRequestRecord,
  transactionPlan: TransactionPlanRecord,
) {
  const firstWrite = transactionPlan.internalWrites[0];
  const payload = objectField(firstWrite ?? {}, "payload");
  const newContactsResult = parseEmergencyContactArray(
    payload?.["newEmergencyContacts"],
  );

  if (!newContactsResult.ok) {
    return newContactsResult;
  }

  const projectionResult = repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    changeRequest.targetWorkerId,
  );
  if (!projectionResult.ok) {
    return projectionResult;
  }

  const emergencyContactUpdatedEvent = repositories.ledger.append({
    tenantId: requestContext.tenantId,
    eventType: LEDGER_EVENT_TYPES.EMPLOYEE_EMERGENCY_CONTACT_UPDATED,
    subjectType: "worker",
    subjectId: changeRequest.targetWorkerId,
    occurredAt: nowIso(),
    effectiveAt: changeRequest.effectiveAt,
    actorType: requestContext.actor.actorType,
    actorId: requestContext.actor.actorId,
    relationshipContext: {},
    changeRequestId: changeRequest.changeRequestId,
    transactionPlanId: transactionPlan.transactionPlanId,
    correlationId: requestContext.correlationId,
    permissionSnapshot: createPermissionSnapshot(requestContext.actor),
    aiVisibilitySnapshot: {},
    payload: payload ?? {},
  });
  if (!emergencyContactUpdatedEvent.ok) {
    return emergencyContactUpdatedEvent;
  }

  const updatedDocument: EmployeeProjectionDocument = {
    ...projectionResult.value.document,
    emergencyContacts: newContactsResult.value,
  };

  return repositories.employeeProjections.updateDocument(
    requestContext.tenantId,
    changeRequest.targetWorkerId,
    updatedDocument,
    emergencyContactUpdatedEvent.value.eventId,
    emergencyContactUpdatedEvent.value.eventSequence,
  );
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
): Record<string, unknown>[] {
  if (view === timelineViews.AUDIT) {
    return ledgerEvents;
  }

  if (view === timelineViews.DEBUG) {
    return ledgerEvents
      .filter((event) => debugTimelineEventTypes.has(event.eventType))
      .map((event) => createDebugTimelineEntry(event));
  }

  return ledgerEvents
    .filter((event) => businessTimelineEventTypes.has(event.eventType))
    .map((event) => createBusinessTimelineEntry(event));
}

function createBusinessTimelineEntry(
  event: LedgerEventRecord,
): Record<string, unknown> {
  return {
    eventType: event.eventType,
    occurredAt: event.occurredAt,
    actorId: event.actorId,
    summary: businessSummaryForEvent(event),
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

function businessSummaryForEvent(event: LedgerEventRecord): string {
  if (event.eventType === LEDGER_EVENT_TYPES.WORKFLOW_INTENT_STARTED) {
    return "Employee data change workflow started.";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.CHANGE_REQUEST_CREATED) {
    return "Employee data change request submitted.";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.NAME_CHANGE_PREFLIGHTED) {
    const riskLevel = stringField(event.payload, "riskLevel") ?? "unknown";

    return `Preflight completed with ${riskLevel} risk.`;
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMERGENCY_CONTACT_PREFLIGHTED) {
    const riskLevel = stringField(event.payload, "riskLevel") ?? "unknown";

    return `Emergency contact preflight completed with ${riskLevel} risk.`;
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EVIDENCE_PROVIDED) {
    return "Evidence was provided for HR review.";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED) {
    return "HR approval task created.";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.APPROVAL_GRANTED) {
    return "HR approved the legal name change.";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.TRANSACTION_PLAN_CREATED) {
    return "Transaction plan created.";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.PERSON_LEGAL_NAME_CHANGED) {
    return "Legal name changed in the employee projection.";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMPLOYEE_EMERGENCY_CONTACT_UPDATED) {
    return "Emergency contact updated in the employee projection.";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED) {
    return "External HRIS sync was queued.";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED) {
    return "Workflow completed.";
  }

  return event.eventType;
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

function isSupportedEmployeeDataIntent(
  intent: string | undefined,
): intent is SupportedEmployeeDataIntent {
  return supportedEmployeeDataIntents.some((supportedIntent) => {
    return supportedIntent === intent;
  });
}

function canStartEmployeeDataWorkflow(
  actor: Parameters<typeof canStartLegalNameWorkflow>[0],
  intent: SupportedEmployeeDataIntent,
  subjectId: string,
): Result<true, AppError> {
  if (intent === WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE) {
    return canStartLegalNameWorkflow(actor, subjectId);
  }

  return canStartEmergencyContactWorkflow(actor, subjectId);
}

function createInitialInteractionForIntent(
  intent: SupportedEmployeeDataIntent,
  employeeDocument: EmployeeProjectionDocument,
) {
  if (intent === WORKFLOW_INTENTS.EMPLOYEE_EMERGENCY_CONTACT_UPDATE) {
    return createEmergencyContactFormInteraction(employeeDocument.emergencyContacts);
  }

  return createLegalNameFormInteraction(employeeDocument.person.legalName);
}

function createLegalNameFormInteraction(currentLegalName: LegalName) {
  return {
    type: "form",
    schemaVersion: "v0.1",
    title: "Request legal name change",
    currentLegalName,
    jsonSchema: {
      type: "object",
      required: ["newLegalName", "effectiveAt", "businessReason"],
      properties: {
        newLegalName: {
          type: "object",
          required: ["first", "last"],
          properties: {
            first: { type: "string", minLength: 1 },
            middle: { type: ["string", "null"] },
            last: { type: "string", minLength: 1 },
          },
        },
        effectiveAt: { type: "string", format: "date" },
        businessReason: {
          type: "string",
          enum: ["marriage", "divorce", "legal_name_change", "correction", "other"],
        },
      },
    },
    uiSchema: {
      layout: "wizard",
      submitLabel: "Continue",
    },
  };
}

function createEmergencyContactFormInteraction(
  currentEmergencyContacts: EmergencyContact[],
) {
  return {
    type: "form",
    schemaVersion: "v0.1",
    title: "Update emergency contact",
    currentEmergencyContacts,
    jsonSchema: {
      type: "object",
      required: ["proposedEmergencyContact", "effectiveAt", "businessReason"],
      properties: {
        proposedEmergencyContact: {
          type: "object",
          required: ["name", "relationship", "phone", "priority"],
          properties: {
            contactId: { type: "string" },
            name: { type: "string", minLength: 1 },
            relationship: {
              type: "string",
              enum: [
                "spouse",
                "partner",
                "parent",
                "sibling",
                "child",
                "friend",
                "other",
              ],
            },
            phone: { type: "string", minLength: 7 },
            email: { type: ["string", "null"] },
            priority: { type: "number", minimum: 1 },
          },
        },
        effectiveAt: { type: "string", format: "date" },
        businessReason: {
          type: "string",
          enum: ["employee_self_service", "correction", "annual_review", "other"],
        },
      },
    },
    uiSchema: {
      layout: "single_page",
      submitLabel: "Submit for review",
    },
  };
}

function legalNameEvidenceInteraction() {
  return {
    type: "evidence_upload",
    title: "Upload legal name change evidence",
    acceptedDocumentTypes: [
      "marriage_certificate",
      "court_order",
      "government_id",
      "other_legal_document",
    ],
    maxFiles: 1,
  };
}

function legalNameWaitingInteraction() {
  return {
    type: "waiting",
    title: "Waiting for HR approval",
  };
}

function legalNameReadyToExecuteInteraction() {
  return {
    type: "ready_to_execute",
    title: "Ready to execute legal name change",
  };
}

function readyToExecuteInteractionForIntent(intent: string) {
  if (intent === WORKFLOW_INTENTS.EMPLOYEE_EMERGENCY_CONTACT_UPDATE) {
    return {
      type: "ready_to_execute",
      title: "Ready to execute emergency contact update",
    };
  }

  return legalNameReadyToExecuteInteraction();
}

function terminalInteraction(status: string) {
  return {
    type: "terminal",
    status,
  };
}

function displayNameFromLegalName(legalName: LegalName): string {
  return [legalName.first, legalName.middle, legalName.last]
    .filter((value) => value !== null && value.trim().length > 0)
    .join(" ");
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

function nullableStringField(
  object: Record<string, unknown>,
  fieldName: string,
): string | null {
  const value = object[fieldName];

  if (value === null || value === undefined) {
    return null;
  }

  if (typeof value !== "string") {
    return null;
  }

  const trimmedValue = value.trim();

  return trimmedValue.length > 0 ? trimmedValue : null;
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
