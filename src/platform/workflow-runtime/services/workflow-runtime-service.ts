import {
  ACTOR_ROLES,
  APPROVAL_TASK_STATUSES,
  CHANGE_REQUEST_STATUSES,
  CHANGE_REQUEST_TYPES,
  INTEGRATION_OUTBOX_STATUSES,
  LEDGER_EVENT_TYPES,
  PERMISSION_KEYS,
  TRANSITION_ATTEMPT_STATUSES,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  WORKFLOW_TRANSITIONS,
  type WorkflowTransition,
} from "../constants";
import type { PermissionSnapshot, RequestContext } from "../context";
import type {
  Actor,
  ApprovalTask,
  ChangeRequest,
  EmployeeProjection,
  JsonObject,
  LedgerEvent,
  LegalName,
  ProposedChange,
  TransitionResponse,
  WorkflowInstance,
  WorkflowTransitionRequest,
} from "../domain";
import {
  buildExecutorRequest,
  LEGAL_NAME_PLAN_TRANSACTION_BLOCK,
  LEGAL_NAME_PREFLIGHT_BLOCK,
  type ExecutorClient,
  type LegalNamePlanOutput,
  type LegalNamePreflightOutput,
} from "../executor/executor-client";
import {
  err,
  idempotencyConflictError,
  invalidWorkflowTransitionError,
  ok,
  permissionDeniedError,
  versionConflictError,
  ERROR_CODES,
  type Result,
} from "../result";
import {
  approvedInteraction,
  canceledInteraction,
  evidenceUploadInteraction,
  executedInteraction,
  legalNameInputInteraction,
  rejectedInteraction,
  waitingApprovalInteraction,
} from "../runtime/interactions";
import { createReadableId } from "../runtime/ids";
import { asJsonObject, valuesAreEqual } from "../runtime/json";
import { LEGAL_NAME_TRANSITION_MAP } from "../runtime/legal-name-workflow";
import {
  buildPermissionSnapshot,
  canViewWorkflowInstance,
  permissionForTransition,
  requireWorkflowPermission,
} from "../runtime/permissions";
import {
  parseLegalNameInputPayload,
  parseTransitionRequest,
  parseWorkflowIntentRequest,
  requireStringPayloadField,
} from "../runtime/validation";
import type {
  ProposedChangeCreateInput,
  WorkflowRuntimeStore,
} from "../storage/workflow-store";
import { buildWorkflowLedgerEvent } from "./ledger-event-builder";
import { buildWorkflowInstanceResponse } from "./response-builder";
import { nowIso } from "../runtime/time";

type HandlerInput = {
  context: RequestContext;
  actor: Actor;
  store: WorkflowRuntimeStore;
  workflowInstance: WorkflowInstance;
  transition: WorkflowTransition;
  payload: JsonObject;
  idempotencyKey: string;
};

type HandlerResult = {
  workflowInstance: WorkflowInstance;
  changeRequest?: ChangeRequest;
  pendingApprovalTask?: ApprovalTask;
  shouldIncrementVersion: boolean;
  timelinePreview?: JsonObject[];
};

type ParsedTransitionRequest = WorkflowTransitionRequest & {
  transition: WorkflowTransition;
};

export type WorkflowRuntimeService = {
  /** Starts a workflow instance from a public workflow intent. */
  startWorkflowIntent(
    context: RequestContext,
    actor: Actor,
    body: unknown,
  ): Promise<Result<TransitionResponse>>;

  /** Loads a workflow instance response for a permitted actor. */
  getWorkflowInstance(
    context: RequestContext,
    actor: Actor,
    workflowInstanceId: string,
  ): Promise<Result<TransitionResponse>>;

  /** Computes actor-specific available actions for a workflow instance. */
  getAvailableActions(
    context: RequestContext,
    actor: Actor,
    workflowInstanceId: string,
  ): Promise<Result<{ availableActions: TransitionResponse["availableActions"] }>>;

  /** Executes a public workflow transition with idempotency and version guards. */
  executeTransition(
    context: RequestContext,
    actor: Actor,
    workflowInstanceId: string,
    body: unknown,
  ): Promise<Result<TransitionResponse>>;
};

export function createWorkflowRuntimeService(input: {
  store: WorkflowRuntimeStore;
  executorClient: ExecutorClient;
}): WorkflowRuntimeService {
  return new DefaultWorkflowRuntimeService(input.store, input.executorClient);
}

class DefaultWorkflowRuntimeService implements WorkflowRuntimeService {
  constructor(
    private readonly store: WorkflowRuntimeStore,
    private readonly executorClient: ExecutorClient,
  ) {}

  async startWorkflowIntent(
    context: RequestContext,
    actor: Actor,
    body: unknown,
  ): Promise<Result<TransitionResponse>> {
    const requestResult = parseWorkflowIntentRequest(body);
    if (!requestResult.ok) {
      return requestResult;
    }

    const permissionResult = requireWorkflowPermission(
      context,
      actor,
      PERMISSION_KEYS.LEGAL_NAME_REQUEST,
      { subjectWorkerId: requestResult.value.subject.id },
    );
    if (!permissionResult.ok) {
      return permissionResult;
    }

    const projectionResult = await this.store.findEmployeeProjectionById(
      context.tenantId,
      requestResult.value.subject.id,
    );
    if (!projectionResult.ok) {
      return projectionResult;
    }

    const workflowVersionResult = await this.store.getActiveWorkflowVersionByIntent(
      context.tenantId,
      requestResult.value.intent,
    );
    if (!workflowVersionResult.ok) {
      return workflowVersionResult;
    }

    const now = nowIso();
    const workflowInstance: WorkflowInstance = {
      workflowInstanceId: createReadableId("wfi"),
      tenantId: context.tenantId,
      environmentId: context.environmentId,
      workflowDefinitionId: workflowVersionResult.value.workflowDefinitionId,
      workflowVersionId: workflowVersionResult.value.workflowVersionId,
      intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      subject: requestResult.value.subject,
      status: WORKFLOW_STATUSES.ACTIVE,
      state: WORKFLOW_STATES.COLLECTING_INPUT,
      requesterActorId: actor.actorId,
      currentInteraction: legalNameInputInteraction(),
      context: {
        currentLegalName: normalizeLegalName(
          projectionResult.value.document.person.legalName,
        ),
        personId: projectionResult.value.document.person.personId,
      },
      startedAt: now,
      version: 1,
      correlationId: context.correlationId,
      metadata: {},
      createdAt: now,
      updatedAt: now,
    };

    return this.store.runInTransaction(async (store) => {
      const createResult = await store.createWorkflowInstance(workflowInstance);
      if (!createResult.ok) {
        return createResult;
      }

      const ledgerResult = await store.appendLedgerEvents([
        buildWorkflowLedgerEvent({
          context,
          workflowInstance: createResult.value,
          permissionSnapshot: permissionResult.value.snapshot,
          eventType: LEDGER_EVENT_TYPES.WORKFLOW_INTENT_STARTED,
          subjectType: "workflow_instance",
          subjectId: createResult.value.workflowInstanceId,
          payload: {
            intent: createResult.value.intent,
            subject: createResult.value.subject,
          },
        }),
      ]);
      if (!ledgerResult.ok) {
        return ledgerResult;
      }

      return ok({
        ...buildWorkflowInstanceResponse({
          context,
          actor,
          workflowInstance: createResult.value,
        }),
      });
    });
  }

  async getWorkflowInstance(
    context: RequestContext,
    actor: Actor,
    workflowInstanceId: string,
  ): Promise<Result<TransitionResponse>> {
    const workflowResult = await this.store.findWorkflowInstanceById(
      context.tenantId,
      workflowInstanceId,
    );
    if (!workflowResult.ok) {
      return workflowResult;
    }

    if (!canViewWorkflowInstance(context, actor, workflowResult.value)) {
      return err(permissionDeniedError({ workflowInstanceId }));
    }

    const responseResult = await this.buildResponseForWorkflow(
      context,
      actor,
      workflowResult.value,
    );
    if (!responseResult.ok) {
      return responseResult;
    }

    return ok(responseResult.value);
  }

  async getAvailableActions(
    context: RequestContext,
    actor: Actor,
    workflowInstanceId: string,
  ): Promise<Result<{ availableActions: TransitionResponse["availableActions"] }>> {
    const workflowResult = await this.getWorkflowInstance(
      context,
      actor,
      workflowInstanceId,
    );
    if (!workflowResult.ok) {
      if (workflowResult.error.code === ERROR_CODES.PERMISSION_DENIED) {
        return ok({ availableActions: [] });
      }

      return workflowResult;
    }

    return ok({ availableActions: workflowResult.value.availableActions });
  }

  async executeTransition(
    context: RequestContext,
    actor: Actor,
    workflowInstanceId: string,
    body: unknown,
  ): Promise<Result<TransitionResponse>> {
    const requestResult = parseTransitionRequest(body);
    if (!requestResult.ok) {
      return requestResult;
    }

    return this.store.runInTransaction(async (store) => {
      const workflowResult = await store.findWorkflowInstanceById(
        context.tenantId,
        workflowInstanceId,
      );
      if (!workflowResult.ok) {
        return workflowResult;
      }

      const workflowInstance = workflowResult.value;
      if (!canViewWorkflowInstance(context, actor, workflowInstance)) {
        return err(permissionDeniedError({ workflowInstanceId }));
      }

      const idempotencyResult = await this.resolveIdempotency(
        store,
        workflowInstance,
        requestResult.value,
      );
      if (!idempotencyResult.ok) {
        return idempotencyResult;
      }

      if (idempotencyResult.value) {
        return ok(idempotencyResult.value);
      }

      const versionResult = validateExpectedVersion(
        workflowInstance,
        requestResult.value.expectedVersion,
      );
      if (!versionResult.ok) {
        return versionResult;
      }

      const transitionGuardResult = validateTransitionIsAllowed(
        workflowInstance,
        requestResult.value.transition,
      );
      if (!transitionGuardResult.ok) {
        return transitionGuardResult;
      }

      const permissionResult = requireWorkflowPermission(
        context,
        actor,
        permissionForTransition(requestResult.value.transition),
        { workflowInstance },
      );
      if (!permissionResult.ok) {
        return permissionResult;
      }

      const attemptResult = await store.createTransitionAttempt({
        tenantId: context.tenantId,
        workflowInstanceId,
        transition: requestResult.value.transition,
        idempotencyKey: requestResult.value.idempotencyKey,
        expectedVersion: requestResult.value.expectedVersion,
        actorId: actor.actorId,
        requestPayload: requestResult.value as unknown as JsonObject,
        status: TRANSITION_ATTEMPT_STATUSES.IN_PROGRESS,
      });
      if (!attemptResult.ok) {
        return attemptResult;
      }

      const submittedLedgerResult = await store.appendLedgerEvents([
        buildWorkflowLedgerEvent({
          context,
          workflowInstance,
          permissionSnapshot: permissionResult.value.snapshot,
          eventType: LEDGER_EVENT_TYPES.WORKFLOW_TRANSITION_SUBMITTED,
          subjectType: "workflow_instance",
          subjectId: workflowInstance.workflowInstanceId,
          idempotencyKey: requestResult.value.idempotencyKey,
          payload: {
            transition: requestResult.value.transition,
            expectedVersion: requestResult.value.expectedVersion,
          },
        }),
      ]);
      if (!submittedLedgerResult.ok) {
        return submittedLedgerResult;
      }

      const handlerResult = await this.dispatchTransition({
        context,
        actor,
        store,
        workflowInstance,
        transition: requestResult.value.transition,
        payload: requestResult.value.payload,
        idempotencyKey: requestResult.value.idempotencyKey,
      });
      if (!handlerResult.ok) {
        await store.failTransitionAttempt(attemptResult.value, {
          code: handlerResult.error.code,
          safeMessage: handlerResult.error.safeMessage,
        });
        return handlerResult;
      }

      const persistedWorkflowResult = handlerResult.value.shouldIncrementVersion
        ? await store.updateWorkflowInstance({
            ...handlerResult.value.workflowInstance,
            version: workflowInstance.version + 1,
            updatedAt: nowIso(),
          })
        : ok(handlerResult.value.workflowInstance);
      if (!persistedWorkflowResult.ok) {
        return persistedWorkflowResult;
      }

      const responseResult = await this.buildResponseForWorkflow(
        context,
        actor,
        persistedWorkflowResult.value,
        handlerResult.value.changeRequest,
        handlerResult.value.pendingApprovalTask,
      );
      if (!responseResult.ok) {
        return responseResult;
      }

      const responsePayload = {
        ...responseResult.value,
        timelinePreview: handlerResult.value.timelinePreview ?? [],
      } as unknown as JsonObject;
      const completeAttemptResult = await store.completeTransitionAttempt(
        attemptResult.value,
        responsePayload,
      );
      if (!completeAttemptResult.ok) {
        return completeAttemptResult;
      }

      return ok(responsePayload as unknown as TransitionResponse);
    });
  }

  private async resolveIdempotency(
    store: WorkflowRuntimeStore,
    workflowInstance: WorkflowInstance,
    request: ParsedTransitionRequest,
  ): Promise<Result<TransitionResponse | undefined>> {
    const existingAttemptResult = await store.findTransitionAttemptByIdempotencyKey(
      workflowInstance.tenantId,
      workflowInstance.workflowInstanceId,
      request.idempotencyKey,
    );
    if (!existingAttemptResult.ok) {
      return existingAttemptResult;
    }

    const existingAttempt = existingAttemptResult.value;
    if (!existingAttempt) {
      return ok(undefined);
    }

    if (
      existingAttempt.status === TRANSITION_ATTEMPT_STATUSES.COMPLETED &&
      existingAttempt.responsePayload &&
      valuesAreEqual(existingAttempt.requestPayload, request as unknown as JsonObject)
    ) {
      return ok({
        ...(existingAttempt.responsePayload as unknown as TransitionResponse),
        idempotentReplay: true,
      });
    }

    return err(
      idempotencyConflictError({
        workflowInstanceId: workflowInstance.workflowInstanceId,
        idempotencyKey: request.idempotencyKey,
      }),
    );
  }

  private async dispatchTransition(
    input: HandlerInput,
  ): Promise<Result<HandlerResult>> {
    if (input.transition === WORKFLOW_TRANSITIONS.SUBMIT_INPUT) {
      return this.handleSubmitInput(input);
    }

    if (input.transition === WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE) {
      return this.handleProvideEvidence(input);
    }

    if (input.transition === WORKFLOW_TRANSITIONS.APPROVE) {
      return this.handleApprove(input);
    }

    if (input.transition === WORKFLOW_TRANSITIONS.REJECT) {
      return this.handleReject(input);
    }

    if (input.transition === WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO) {
      return this.handleRequestMoreInfo(input);
    }

    if (input.transition === WORKFLOW_TRANSITIONS.CANCEL) {
      return this.handleCancel(input);
    }

    return this.handleExecute(input);
  }

  private async handleSubmitInput(input: HandlerInput): Promise<Result<HandlerResult>> {
    const payloadResult = parseLegalNameInputPayload(input.payload);
    if (!payloadResult.ok) {
      return ok({
        workflowInstance: {
          ...input.workflowInstance,
          currentInteraction: legalNameInputInteraction(
            asJsonObject(payloadResult.error.details?.validationErrors),
          ),
        },
        shouldIncrementVersion: false,
      });
    }

    const projectionResult = await input.store.findEmployeeProjectionById(
      input.context.tenantId,
      input.workflowInstance.subject.id,
    );
    if (!projectionResult.ok) {
      return projectionResult;
    }

    const permissionSnapshot = buildPermissionSnapshot(input.context, input.actor, {
      workflowInstance: input.workflowInstance,
    });
    const currentLegalName = normalizeLegalName(
      projectionResult.value.document.person.legalName,
    );
    const proposedLegalName = normalizeLegalName(payloadResult.value.newLegalName);
    const preflightResult = await this.executorClient.executeBlock(
      buildExecutorRequest({
        context: input.context,
        workflowInstanceId: input.workflowInstance.workflowInstanceId,
        workflowVersionId: input.workflowInstance.workflowVersionId,
        block: LEGAL_NAME_PREFLIGHT_BLOCK,
        payload: {
          currentLegalName,
          proposedLegalName,
          effectiveAt: payloadResult.value.effectiveAt,
          businessReason: payloadResult.value.businessReason,
        },
        effectiveAt: payloadResult.value.effectiveAt,
        idempotencyKey: input.idempotencyKey,
        permissions: permissionSnapshot as unknown as JsonObject,
      }),
    );
    if (!preflightResult.ok) {
      return preflightResult;
    }

    const preflightOutput = preflightResult.value
      .output as unknown as LegalNamePreflightOutput;
    if (!preflightOutput.valid) {
      return ok({
        workflowInstance: {
          ...input.workflowInstance,
          currentInteraction: legalNameInputInteraction({
            preflightErrors: preflightOutput.errors,
          }),
        },
        shouldIncrementVersion: false,
      });
    }

    const changeRequestResult = await input.store.createChangeRequest({
      tenantId: input.context.tenantId,
      changeType: CHANGE_REQUEST_TYPES.EMPLOYEE_DATA_CHANGE,
      targetWorkerId: input.workflowInstance.subject.id,
      requesterActorId: input.actor.actorId,
      effectiveAt: payloadResult.value.effectiveAt,
      businessReason: payloadResult.value.businessReason,
      status: CHANGE_REQUEST_STATUSES.NEEDS_DATA,
      currentSnapshot: { legalName: currentLegalName },
      proposedSnapshot: { legalName: proposedLegalName },
      preflightResult: preflightOutput as unknown as JsonObject,
      workflowDefinitionId: input.workflowInstance.workflowDefinitionId,
      workflowVersionId: input.workflowInstance.workflowVersionId,
      submittedAt: nowIso(),
    });
    if (!changeRequestResult.ok) {
      return changeRequestResult;
    }

    const proposedChangesResult = await input.store.createProposedChanges(
      buildLegalNameProposedChanges({
        projection: projectionResult.value,
        changeRequest: changeRequestResult.value,
        currentLegalName,
        proposedLegalName,
      }),
    );
    if (!proposedChangesResult.ok) {
      return proposedChangesResult;
    }

    const nextWorkflowInstance: WorkflowInstance = {
      ...input.workflowInstance,
      state: WORKFLOW_STATES.COLLECTING_EVIDENCE,
      status: WORKFLOW_STATUSES.ACTIVE,
      changeRequestId: changeRequestResult.value.changeRequestId,
      currentInteraction: evidenceUploadInteraction(),
      context: {
        ...input.workflowInstance.context,
        proposedLegalName,
        effectiveAt: payloadResult.value.effectiveAt,
        businessReason: payloadResult.value.businessReason,
      },
    };
    const ledgerResult = await input.store.appendLedgerEvents([
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_CREATED,
        subjectType: "change_request",
        subjectId: changeRequestResult.value.changeRequestId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        payload: { status: CHANGE_REQUEST_STATUSES.NEEDS_DATA },
      }),
      ...proposedChangesResult.value.map((change: ProposedChange) =>
        buildWorkflowLedgerEvent({
          context: input.context,
          workflowInstance: nextWorkflowInstance,
          permissionSnapshot,
          eventType: LEDGER_EVENT_TYPES.PROPOSED_CHANGE_CREATED,
          subjectType: "proposed_change",
          subjectId: change.proposedChangeId,
          changeRequestId: change.changeRequestId,
          payload: {
            fieldPath: change.fieldPath,
            currentValue: change.currentValue,
            proposedValue: change.proposedValue,
          },
        }),
      ),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.NAME_CHANGE_PREFLIGHTED,
        subjectType: "change_request",
        subjectId: changeRequestResult.value.changeRequestId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        payload: preflightOutput as unknown as JsonObject,
      }),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.EVIDENCE_REQUESTED,
        subjectType: "workflow_instance",
        subjectId: nextWorkflowInstance.workflowInstanceId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        payload: { required: true },
      }),
      stateChangedEvent(input, nextWorkflowInstance, permissionSnapshot),
    ]);
    if (!ledgerResult.ok) {
      return ledgerResult;
    }

    return ok({
      workflowInstance: nextWorkflowInstance,
      changeRequest: changeRequestResult.value,
      shouldIncrementVersion: true,
      timelinePreview: previewLedgerEvents(ledgerResult.value),
    });
  }

  private async handleProvideEvidence(
    input: HandlerInput,
  ): Promise<Result<HandlerResult>> {
    const documentIdResult = requireStringPayloadField(input.payload, "documentId");
    if (!documentIdResult.ok) {
      return documentIdResult;
    }

    if (!input.workflowInstance.changeRequestId) {
      return err(invalidWorkflowTransitionError({ reason: "missing_change_request" }));
    }

    const documentResult = await input.store.findDocumentById(
      input.context.tenantId,
      documentIdResult.value,
    );
    if (!documentResult.ok) {
      return documentResult;
    }

    if (
      documentResult.value.workflowInstanceId !==
        input.workflowInstance.workflowInstanceId ||
      documentResult.value.uploadedByActorId !== input.actor.actorId
    ) {
      return err(permissionDeniedError({ documentId: documentIdResult.value }));
    }

    const existingLinkResult = await input.store.findWorkflowInstanceDocument(
      input.context.tenantId,
      input.workflowInstance.workflowInstanceId,
      documentIdResult.value,
    );
    if (!existingLinkResult.ok) {
      return existingLinkResult;
    }

    if (!existingLinkResult.value) {
      const attachResult = await input.store.attachDocumentToWorkflowInstance({
        tenantId: input.context.tenantId,
        workflowInstanceId: input.workflowInstance.workflowInstanceId,
        documentId: documentIdResult.value,
        attachedByActorId: input.actor.actorId,
        attachedAt: nowIso(),
      });
      if (!attachResult.ok) {
        return attachResult;
      }
    }

    const changeRequestResult = await input.store.findChangeRequestById(
      input.context.tenantId,
      input.workflowInstance.changeRequestId,
    );
    if (!changeRequestResult.ok) {
      return changeRequestResult;
    }

    const approvalTaskResult = await input.store.createApprovalTask({
      tenantId: input.context.tenantId,
      changeRequestId: changeRequestResult.value.changeRequestId,
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
      assigneeRole: ACTOR_ROLES.HR_ADMIN,
      approvalType: "legal_name_change",
      status: APPROVAL_TASK_STATUSES.PENDING,
      metadata: { documentId: documentIdResult.value },
    });
    if (!approvalTaskResult.ok) {
      return approvalTaskResult;
    }

    const updatedChangeRequestResult = await input.store.updateChangeRequest({
      ...changeRequestResult.value,
      status: CHANGE_REQUEST_STATUSES.IN_APPROVAL,
      submittedAt: nowIso(),
    });
    if (!updatedChangeRequestResult.ok) {
      return updatedChangeRequestResult;
    }

    const permissionSnapshot = buildPermissionSnapshot(input.context, input.actor, {
      workflowInstance: input.workflowInstance,
    });
    const nextWorkflowInstance: WorkflowInstance = {
      ...input.workflowInstance,
      state: WORKFLOW_STATES.WAITING_APPROVAL,
      status: WORKFLOW_STATUSES.WAITING,
      currentInteraction: waitingApprovalInteraction(),
    };
    const ledgerResult = await input.store.appendLedgerEvents([
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.EVIDENCE_PROVIDED,
        subjectType: "document",
        subjectId: documentIdResult.value,
        changeRequestId: changeRequestResult.value.changeRequestId,
        payload: { documentId: documentIdResult.value },
      }),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED,
        subjectType: "approval_task",
        subjectId: approvalTaskResult.value.approvalTaskId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        approvalTaskId: approvalTaskResult.value.approvalTaskId,
        payload: { assigneeRole: ACTOR_ROLES.HR_ADMIN },
      }),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_SUBMITTED,
        subjectType: "change_request",
        subjectId: changeRequestResult.value.changeRequestId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        payload: { status: CHANGE_REQUEST_STATUSES.IN_APPROVAL },
      }),
      stateChangedEvent(input, nextWorkflowInstance, permissionSnapshot),
    ]);
    if (!ledgerResult.ok) {
      return ledgerResult;
    }

    return ok({
      workflowInstance: nextWorkflowInstance,
      changeRequest: updatedChangeRequestResult.value,
      pendingApprovalTask: approvalTaskResult.value,
      shouldIncrementVersion: true,
      timelinePreview: previewLedgerEvents(ledgerResult.value),
    });
  }

  private async handleApprove(input: HandlerInput): Promise<Result<HandlerResult>> {
    const approvalTaskResult = await this.loadApprovalTaskForDecision(input);
    if (!approvalTaskResult.ok) {
      return approvalTaskResult;
    }

    const changeRequestResult = await input.store.findChangeRequestById(
      input.context.tenantId,
      approvalTaskResult.value.changeRequestId,
    );
    if (!changeRequestResult.ok) {
      return changeRequestResult;
    }

    const projectionResult = await input.store.findEmployeeProjectionById(
      input.context.tenantId,
      changeRequestResult.value.targetWorkerId,
    );
    if (!projectionResult.ok) {
      return projectionResult;
    }

    const proposedName = changeRequestResult.value.proposedSnapshot
      .legalName as unknown as LegalName;
    const planResult = await this.executorClient.executeBlock(
      buildExecutorRequest({
        context: input.context,
        workflowInstanceId: input.workflowInstance.workflowInstanceId,
        workflowVersionId: input.workflowInstance.workflowVersionId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        block: LEGAL_NAME_PLAN_TRANSACTION_BLOCK,
        payload: {
          changeRequestId: changeRequestResult.value.changeRequestId,
          workerId: changeRequestResult.value.targetWorkerId,
          personId: projectionResult.value.document.person.personId,
          currentLegalName: normalizeLegalName(
            projectionResult.value.document.person.legalName,
          ),
          proposedLegalName: normalizeLegalName(proposedName),
          effectiveAt: changeRequestResult.value.effectiveAt,
        },
        effectiveAt: changeRequestResult.value.effectiveAt,
        idempotencyKey: input.idempotencyKey,
        permissions: buildPermissionSnapshot(input.context, input.actor, {
          workflowInstance: input.workflowInstance,
          approvalTask: approvalTaskResult.value,
        }) as unknown as JsonObject,
      }),
    );
    if (!planResult.ok) {
      return planResult;
    }

    const planOutput = planResult.value.output as unknown as LegalNamePlanOutput;
    const transactionPlanResult = await input.store.createTransactionPlan({
      tenantId: input.context.tenantId,
      changeRequestId: changeRequestResult.value.changeRequestId,
      status: "ready",
      planVersion: 1,
      steps: [
        { type: "append_event" },
        { type: "update_projection" },
        { type: "external_write" },
      ],
      internalWrites: planOutput.internalWrites,
      externalWrites: planOutput.externalCallRequests,
      idempotencyKeys: {
        workflowTransition: input.idempotencyKey,
      },
      executionResult: {},
    });
    if (!transactionPlanResult.ok) {
      return transactionPlanResult;
    }

    const updatedTaskResult = await input.store.updateApprovalTask({
      ...approvalTaskResult.value,
      status: APPROVAL_TASK_STATUSES.APPROVED,
      decision: "approved",
      comments: stringPayload(input.payload.comment),
      decidedAt: nowIso(),
    });
    if (!updatedTaskResult.ok) {
      return updatedTaskResult;
    }

    const updatedChangeRequestResult = await input.store.updateChangeRequest({
      ...changeRequestResult.value,
      status: CHANGE_REQUEST_STATUSES.APPROVED,
      transactionPlanId: transactionPlanResult.value.transactionPlanId,
      approvedAt: nowIso(),
    });
    if (!updatedChangeRequestResult.ok) {
      return updatedChangeRequestResult;
    }

    const permissionSnapshot = buildPermissionSnapshot(input.context, input.actor, {
      workflowInstance: input.workflowInstance,
      approvalTask: approvalTaskResult.value,
    });
    const nextWorkflowInstance: WorkflowInstance = {
      ...input.workflowInstance,
      state: WORKFLOW_STATES.APPROVED,
      status: WORKFLOW_STATUSES.ACTIVE,
      currentInteraction: approvedInteraction(),
      context: {
        ...input.workflowInstance.context,
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
      },
    };
    const ledgerResult = await input.store.appendLedgerEvents([
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.APPROVAL_GRANTED,
        subjectType: "approval_task",
        subjectId: updatedTaskResult.value.approvalTaskId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        approvalTaskId: updatedTaskResult.value.approvalTaskId,
        payload: { comments: updatedTaskResult.value.comments ?? null },
      }),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_APPROVED,
        subjectType: "change_request",
        subjectId: changeRequestResult.value.changeRequestId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        payload: { status: CHANGE_REQUEST_STATUSES.APPROVED },
      }),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.TRANSACTION_PLAN_CREATED,
        subjectType: "transaction_plan",
        subjectId: transactionPlanResult.value.transactionPlanId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        transactionPlanId: transactionPlanResult.value.transactionPlanId,
        payload: {
          internalWriteCount: transactionPlanResult.value.internalWrites.length,
          externalWriteCount: transactionPlanResult.value.externalWrites.length,
        },
      }),
      stateChangedEvent(input, nextWorkflowInstance, permissionSnapshot),
    ]);
    if (!ledgerResult.ok) {
      return ledgerResult;
    }

    return ok({
      workflowInstance: nextWorkflowInstance,
      changeRequest: updatedChangeRequestResult.value,
      shouldIncrementVersion: true,
      timelinePreview: previewLedgerEvents(ledgerResult.value),
    });
  }

  private async handleReject(input: HandlerInput): Promise<Result<HandlerResult>> {
    const approvalTaskResult = await this.loadApprovalTaskForDecision(input);
    if (!approvalTaskResult.ok) {
      return approvalTaskResult;
    }

    const reasonResult = requireStringPayloadField(input.payload, "reason");
    if (!reasonResult.ok) {
      return reasonResult;
    }

    const changeRequestResult = await input.store.findChangeRequestById(
      input.context.tenantId,
      approvalTaskResult.value.changeRequestId,
    );
    if (!changeRequestResult.ok) {
      return changeRequestResult;
    }

    const updatedTaskResult = await input.store.updateApprovalTask({
      ...approvalTaskResult.value,
      status: APPROVAL_TASK_STATUSES.REJECTED,
      decision: "rejected",
      decisionReason: reasonResult.value,
      comments: stringPayload(input.payload.comment),
      decidedAt: nowIso(),
    });
    if (!updatedTaskResult.ok) {
      return updatedTaskResult;
    }

    const updatedChangeRequestResult = await input.store.updateChangeRequest({
      ...changeRequestResult.value,
      status: CHANGE_REQUEST_STATUSES.REJECTED,
      closedAt: nowIso(),
    });
    if (!updatedChangeRequestResult.ok) {
      return updatedChangeRequestResult;
    }

    const permissionSnapshot = buildPermissionSnapshot(input.context, input.actor, {
      workflowInstance: input.workflowInstance,
      approvalTask: approvalTaskResult.value,
    });
    const nextWorkflowInstance: WorkflowInstance = {
      ...input.workflowInstance,
      state: WORKFLOW_STATES.REJECTED,
      status: WORKFLOW_STATUSES.COMPLETED,
      currentInteraction: rejectedInteraction(reasonResult.value),
      completedAt: nowIso(),
    };
    const ledgerResult = await input.store.appendLedgerEvents([
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.APPROVAL_REJECTED,
        subjectType: "approval_task",
        subjectId: updatedTaskResult.value.approvalTaskId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        approvalTaskId: updatedTaskResult.value.approvalTaskId,
        payload: { reason: reasonResult.value },
      }),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_REJECTED,
        subjectType: "change_request",
        subjectId: changeRequestResult.value.changeRequestId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        payload: { status: CHANGE_REQUEST_STATUSES.REJECTED },
      }),
      stateChangedEvent(input, nextWorkflowInstance, permissionSnapshot),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED,
        subjectType: "workflow_instance",
        subjectId: nextWorkflowInstance.workflowInstanceId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        payload: { terminalState: WORKFLOW_STATES.REJECTED },
      }),
    ]);
    if (!ledgerResult.ok) {
      return ledgerResult;
    }

    return ok({
      workflowInstance: nextWorkflowInstance,
      changeRequest: updatedChangeRequestResult.value,
      shouldIncrementVersion: true,
      timelinePreview: previewLedgerEvents(ledgerResult.value),
    });
  }

  private async handleRequestMoreInfo(
    input: HandlerInput,
  ): Promise<Result<HandlerResult>> {
    const approvalTaskResult = await this.loadApprovalTaskForDecision(input);
    if (!approvalTaskResult.ok) {
      return approvalTaskResult;
    }

    const commentResult = requireStringPayloadField(input.payload, "comment");
    if (!commentResult.ok) {
      return commentResult;
    }

    const changeRequestResult = await input.store.findChangeRequestById(
      input.context.tenantId,
      approvalTaskResult.value.changeRequestId,
    );
    if (!changeRequestResult.ok) {
      return changeRequestResult;
    }

    const updatedTaskResult = await input.store.updateApprovalTask({
      ...approvalTaskResult.value,
      status: APPROVAL_TASK_STATUSES.CANCELED,
      decision: "skipped",
      comments: commentResult.value,
      decidedAt: nowIso(),
    });
    if (!updatedTaskResult.ok) {
      return updatedTaskResult;
    }

    const updatedChangeRequestResult = await input.store.updateChangeRequest({
      ...changeRequestResult.value,
      status: CHANGE_REQUEST_STATUSES.NEEDS_DATA,
    });
    if (!updatedChangeRequestResult.ok) {
      return updatedChangeRequestResult;
    }

    const permissionSnapshot = buildPermissionSnapshot(input.context, input.actor, {
      workflowInstance: input.workflowInstance,
      approvalTask: approvalTaskResult.value,
    });
    const nextWorkflowInstance: WorkflowInstance = {
      ...input.workflowInstance,
      state: WORKFLOW_STATES.COLLECTING_EVIDENCE,
      status: WORKFLOW_STATUSES.ACTIVE,
      currentInteraction: evidenceUploadInteraction(),
    };
    const ledgerResult = await input.store.appendLedgerEvents([
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.MORE_INFORMATION_REQUESTED,
        subjectType: "approval_task",
        subjectId: approvalTaskResult.value.approvalTaskId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        approvalTaskId: approvalTaskResult.value.approvalTaskId,
        payload: { comment: commentResult.value },
      }),
      stateChangedEvent(input, nextWorkflowInstance, permissionSnapshot),
    ]);
    if (!ledgerResult.ok) {
      return ledgerResult;
    }

    return ok({
      workflowInstance: nextWorkflowInstance,
      changeRequest: updatedChangeRequestResult.value,
      shouldIncrementVersion: true,
      timelinePreview: previewLedgerEvents(ledgerResult.value),
    });
  }

  private async handleCancel(input: HandlerInput): Promise<Result<HandlerResult>> {
    if (input.workflowInstance.state === WORKFLOW_STATES.EXECUTED) {
      return err(invalidWorkflowTransitionError({ transition: input.transition }));
    }

    const permissionSnapshot = buildPermissionSnapshot(input.context, input.actor, {
      workflowInstance: input.workflowInstance,
    });
    const nextWorkflowInstance: WorkflowInstance = {
      ...input.workflowInstance,
      state: WORKFLOW_STATES.CANCELED,
      status: WORKFLOW_STATUSES.CANCELED,
      currentInteraction: canceledInteraction(),
      canceledAt: nowIso(),
      completedAt: nowIso(),
    };
    const ledgerEvents = [
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.WORKFLOW_CANCELED,
        subjectType: "workflow_instance",
        subjectId: nextWorkflowInstance.workflowInstanceId,
        payload: { previousState: input.workflowInstance.state },
      }),
      stateChangedEvent(input, nextWorkflowInstance, permissionSnapshot),
    ];

    let changeRequest: ChangeRequest | undefined;
    if (input.workflowInstance.changeRequestId) {
      const changeRequestResult = await input.store.findChangeRequestById(
        input.context.tenantId,
        input.workflowInstance.changeRequestId,
      );
      if (!changeRequestResult.ok) {
        return changeRequestResult;
      }

      const updatedChangeRequestResult = await input.store.updateChangeRequest({
        ...changeRequestResult.value,
        status: CHANGE_REQUEST_STATUSES.CANCELED,
        closedAt: nowIso(),
      });
      if (!updatedChangeRequestResult.ok) {
        return updatedChangeRequestResult;
      }

      changeRequest = updatedChangeRequestResult.value;
      ledgerEvents.push(
        buildWorkflowLedgerEvent({
          context: input.context,
          workflowInstance: nextWorkflowInstance,
          permissionSnapshot,
          eventType: LEDGER_EVENT_TYPES.CHANGE_REQUEST_CANCELED,
          subjectType: "change_request",
          subjectId: updatedChangeRequestResult.value.changeRequestId,
          changeRequestId: updatedChangeRequestResult.value.changeRequestId,
          payload: { status: CHANGE_REQUEST_STATUSES.CANCELED },
        }),
      );
    }

    const ledgerResult = await input.store.appendLedgerEvents(ledgerEvents);
    if (!ledgerResult.ok) {
      return ledgerResult;
    }

    return ok({
      workflowInstance: nextWorkflowInstance,
      changeRequest,
      shouldIncrementVersion: true,
      timelinePreview: previewLedgerEvents(ledgerResult.value),
    });
  }

  private async handleExecute(input: HandlerInput): Promise<Result<HandlerResult>> {
    if (!input.workflowInstance.changeRequestId) {
      return err(invalidWorkflowTransitionError({ reason: "missing_change_request" }));
    }

    const changeRequestResult = await input.store.findChangeRequestById(
      input.context.tenantId,
      input.workflowInstance.changeRequestId,
    );
    if (!changeRequestResult.ok) {
      return changeRequestResult;
    }

    const planResult = await input.store.findTransactionPlanByChangeRequestId(
      input.context.tenantId,
      changeRequestResult.value.changeRequestId,
    );
    if (!planResult.ok) {
      return planResult;
    }

    if (changeRequestResult.value.status !== CHANGE_REQUEST_STATUSES.APPROVED) {
      return err(
        invalidWorkflowTransitionError({ status: changeRequestResult.value.status }),
      );
    }

    const projectionResult = await input.store.findEmployeeProjectionById(
      input.context.tenantId,
      changeRequestResult.value.targetWorkerId,
    );
    if (!projectionResult.ok) {
      return projectionResult;
    }

    const previousLegalName = normalizeLegalName(
      projectionResult.value.document.person.legalName,
    );
    const newLegalName = normalizeLegalName(
      changeRequestResult.value.proposedSnapshot.legalName as unknown as LegalName,
    );
    const updatedProjectionResult = await input.store.updateEmployeeProjection({
      ...projectionResult.value,
      document: {
        ...projectionResult.value.document,
        person: {
          ...projectionResult.value.document.person,
          legalName: newLegalName,
          displayName: formatLegalName(newLegalName),
        },
      },
    });
    if (!updatedProjectionResult.ok) {
      return updatedProjectionResult;
    }

    const executingPlanResult = await input.store.updateTransactionPlan({
      ...planResult.value,
      status: "executed",
      executionResult: {
        executedAt: nowIso(),
      },
    });
    if (!executingPlanResult.ok) {
      return executingPlanResult;
    }

    const updatedChangeRequestResult = await input.store.updateChangeRequest({
      ...changeRequestResult.value,
      status: CHANGE_REQUEST_STATUSES.EXECUTED,
      executedAt: nowIso(),
      closedAt: nowIso(),
    });
    if (!updatedChangeRequestResult.ok) {
      return updatedChangeRequestResult;
    }

    const permissionSnapshot = buildPermissionSnapshot(input.context, input.actor, {
      workflowInstance: input.workflowInstance,
    });
    const nextWorkflowInstance: WorkflowInstance = {
      ...input.workflowInstance,
      state: WORKFLOW_STATES.EXECUTED,
      status: WORKFLOW_STATUSES.COMPLETED,
      currentInteraction: executedInteraction(previousLegalName, newLegalName),
      completedAt: nowIso(),
    };
    const externalWrite = planResult.value.externalWrites[0] ?? {};
    const externalWriteEvent = buildWorkflowLedgerEvent({
      context: input.context,
      workflowInstance: nextWorkflowInstance,
      permissionSnapshot,
      eventType: LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED,
      subjectType: "integration_outbox",
      subjectId: "pending",
      changeRequestId: changeRequestResult.value.changeRequestId,
      transactionPlanId: planResult.value.transactionPlanId,
      payload: externalWrite,
    });
    const ledgerResult = await input.store.appendLedgerEvents([
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_STARTED,
        subjectType: "transaction_plan",
        subjectId: planResult.value.transactionPlanId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        transactionPlanId: planResult.value.transactionPlanId,
        payload: { previousState: WORKFLOW_STATES.APPROVED },
      }),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.PERSON_LEGAL_NAME_CHANGED,
        subjectType: "worker",
        subjectId: changeRequestResult.value.targetWorkerId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        transactionPlanId: planResult.value.transactionPlanId,
        effectiveAt: changeRequestResult.value.effectiveAt,
        payload: {
          personId: projectionResult.value.document.person.personId,
          previousLegalName,
          newLegalName,
        },
      }),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.EMPLOYEE_PROJECTION_UPDATED,
        subjectType: "worker",
        subjectId: changeRequestResult.value.targetWorkerId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        transactionPlanId: planResult.value.transactionPlanId,
        payload: {
          employeeId: updatedProjectionResult.value.employeeId,
          projectionVersion: updatedProjectionResult.value.projectionVersion,
        },
      }),
      externalWriteEvent,
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_COMPLETED,
        subjectType: "transaction_plan",
        subjectId: planResult.value.transactionPlanId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        transactionPlanId: planResult.value.transactionPlanId,
        payload: { status: "executed" },
      }),
      stateChangedEvent(input, nextWorkflowInstance, permissionSnapshot),
      buildWorkflowLedgerEvent({
        context: input.context,
        workflowInstance: nextWorkflowInstance,
        permissionSnapshot,
        eventType: LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED,
        subjectType: "workflow_instance",
        subjectId: nextWorkflowInstance.workflowInstanceId,
        changeRequestId: changeRequestResult.value.changeRequestId,
        payload: { terminalState: WORKFLOW_STATES.EXECUTED },
      }),
    ]);
    if (!ledgerResult.ok) {
      return ledgerResult;
    }

    const outboxResult = await input.store.createIntegrationOutbox({
      tenantId: input.context.tenantId,
      changeRequestId: changeRequestResult.value.changeRequestId,
      transactionPlanId: planResult.value.transactionPlanId,
      ledgerEventId: ledgerResult.value.find(
        (event: LedgerEvent) =>
          event.eventType === LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED,
      )?.eventId,
      destination: String(externalWrite.connectionId ?? "fake_hris"),
      operation: String(externalWrite.operation ?? "updateLegalName"),
      requestPayload: asJsonObject(externalWrite.payload),
      status: INTEGRATION_OUTBOX_STATUSES.PENDING,
      idempotencyKey: String(
        externalWrite.idempotencyKey ??
          `fake_hris_legal_name_${changeRequestResult.value.changeRequestId}`,
      ),
    });
    if (!outboxResult.ok) {
      return outboxResult;
    }

    return ok({
      workflowInstance: nextWorkflowInstance,
      changeRequest: updatedChangeRequestResult.value,
      shouldIncrementVersion: true,
      timelinePreview: previewLedgerEvents(ledgerResult.value),
    });
  }

  private async loadApprovalTaskForDecision(
    input: HandlerInput,
  ): Promise<Result<ApprovalTask>> {
    const approvalTaskIdResult = requireStringPayloadField(
      input.payload,
      "approvalTaskId",
    );
    if (!approvalTaskIdResult.ok) {
      return approvalTaskIdResult;
    }

    const approvalTaskResult = await input.store.findApprovalTaskById(
      input.context.tenantId,
      approvalTaskIdResult.value,
    );
    if (!approvalTaskResult.ok) {
      return approvalTaskResult;
    }

    if (
      approvalTaskResult.value.workflowInstanceId !==
      input.workflowInstance.workflowInstanceId
    ) {
      return err(permissionDeniedError({ approvalTaskId: approvalTaskIdResult.value }));
    }

    if (approvalTaskResult.value.status !== APPROVAL_TASK_STATUSES.PENDING) {
      return err(
        invalidWorkflowTransitionError({
          approvalTaskId: approvalTaskIdResult.value,
          status: approvalTaskResult.value.status,
        }),
      );
    }

    return ok(approvalTaskResult.value);
  }

  private async buildResponseForWorkflow(
    context: RequestContext,
    actor: Actor,
    workflowInstance: WorkflowInstance,
    knownChangeRequest?: ChangeRequest,
    knownPendingApprovalTask?: ApprovalTask,
  ): Promise<Result<TransitionResponse>> {
    const changeRequestResult = knownChangeRequest
      ? ok(knownChangeRequest)
      : await loadOptionalChangeRequest(this.store, workflowInstance);
    if (!changeRequestResult.ok) {
      return changeRequestResult;
    }

    const pendingApprovalTaskResult = knownPendingApprovalTask
      ? ok(knownPendingApprovalTask)
      : await this.store.findPendingApprovalTaskByWorkflowInstanceId(
          context.tenantId,
          workflowInstance.workflowInstanceId,
        );
    if (!pendingApprovalTaskResult.ok) {
      return pendingApprovalTaskResult;
    }

    const responseInput =
      changeRequestResult.value !== undefined
        ? {
            context,
            actor,
            workflowInstance,
            changeRequest: changeRequestResult.value,
          }
        : {
            context,
            actor,
            workflowInstance,
          };

    if (pendingApprovalTaskResult.value !== undefined) {
      return ok(
        buildWorkflowInstanceResponse({
          ...responseInput,
          pendingApprovalTask: pendingApprovalTaskResult.value,
        }),
      );
    }

    return ok(buildWorkflowInstanceResponse(responseInput));
  }
}

function validateExpectedVersion(
  workflowInstance: WorkflowInstance,
  expectedVersion: number,
): Result<void> {
  if (workflowInstance.version !== expectedVersion) {
    return err(
      versionConflictError({
        workflowInstanceId: workflowInstance.workflowInstanceId,
        expectedVersion,
        actualVersion: workflowInstance.version,
      }),
    );
  }

  return ok(undefined);
}

function validateTransitionIsAllowed(
  workflowInstance: WorkflowInstance,
  transition: WorkflowTransition,
): Result<void> {
  const allowedTransitions = LEGAL_NAME_TRANSITION_MAP[workflowInstance.state] ?? [];
  if (!allowedTransitions.includes(transition)) {
    return err(
      invalidWorkflowTransitionError({
        state: workflowInstance.state,
        transition,
      }),
    );
  }

  return ok(undefined);
}

function buildLegalNameProposedChanges(input: {
  projection: EmployeeProjection;
  changeRequest: ChangeRequest;
  currentLegalName: LegalName;
  proposedLegalName: LegalName;
}): ProposedChangeCreateInput[] {
  const personId = input.projection.document.person.personId;
  const fieldChanges = [
    {
      fieldPath: "person.legalName.first",
      currentValue: input.currentLegalName.first,
      proposedValue: input.proposedLegalName.first,
    },
    {
      fieldPath: "person.legalName.middle",
      currentValue: input.currentLegalName.middle,
      proposedValue: input.proposedLegalName.middle,
    },
    {
      fieldPath: "person.legalName.last",
      currentValue: input.currentLegalName.last,
      proposedValue: input.proposedLegalName.last,
    },
  ];

  return fieldChanges.map((fieldChange) => ({
    tenantId: input.changeRequest.tenantId,
    changeRequestId: input.changeRequest.changeRequestId,
    targetObjectType: "person",
    targetObjectId: personId,
    fieldPath: fieldChange.fieldPath,
    currentValue: fieldChange.currentValue ?? null,
    proposedValue: fieldChange.proposedValue ?? null,
    effectiveAt: input.changeRequest.effectiveAt,
    reasonCode: input.changeRequest.businessReason,
    validationStatus: "valid",
    riskLevel: "low",
    metadata: {},
  }));
}

function stateChangedEvent(
  input: HandlerInput,
  nextWorkflowInstance: WorkflowInstance,
  permissionSnapshot: PermissionSnapshot,
) {
  const inputEvent = {
    context: input.context,
    workflowInstance: nextWorkflowInstance,
    permissionSnapshot,
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_STATE_CHANGED,
    subjectType: "workflow_instance",
    subjectId: nextWorkflowInstance.workflowInstanceId,
    payload: {
      previousState: input.workflowInstance.state,
      nextState: nextWorkflowInstance.state,
    },
  };

  if (nextWorkflowInstance.changeRequestId !== undefined) {
    return buildWorkflowLedgerEvent({
      ...inputEvent,
      changeRequestId: nextWorkflowInstance.changeRequestId,
    });
  }

  return buildWorkflowLedgerEvent(inputEvent);
}

function previewLedgerEvents(events: LedgerEvent[]): JsonObject[] {
  return events.map((event) => ({
    eventId: event.eventId,
    eventType: event.eventType,
    occurredAt: event.occurredAt,
  }));
}

async function loadOptionalChangeRequest(
  store: WorkflowRuntimeStore,
  workflowInstance: WorkflowInstance,
): Promise<Result<ChangeRequest | undefined>> {
  if (!workflowInstance.changeRequestId) {
    return ok(undefined);
  }

  const changeRequestResult = await store.findChangeRequestById(
    workflowInstance.tenantId,
    workflowInstance.changeRequestId,
  );
  if (!changeRequestResult.ok) {
    return changeRequestResult;
  }

  return ok(changeRequestResult.value);
}

function normalizeLegalName(legalName: LegalName): LegalName {
  return {
    first: legalName.first.trim(),
    middle: legalName.middle?.trim() || null,
    last: legalName.last.trim(),
  };
}

function formatLegalName(legalName: LegalName): string {
  return [legalName.first, legalName.middle, legalName.last].filter(Boolean).join(" ");
}

function stringPayload(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0
    ? value.trim()
    : undefined;
}
