import {
  ACTOR_ROLES,
  PERMISSION_KEYS,
  WORKFLOW_STATES,
  WORKFLOW_TRANSITIONS,
  err,
  ok,
  permissionDeniedError,
  type AppError,
  type Result,
  type WorkflowTransition,
} from "@hcm-next/foundation";
import type {
  ActorRecord,
  ApprovalTaskRecord,
  WorkflowInstanceRecord,
} from "@hcm-next/data-store";

export function createPermissionSnapshot(actor: ActorRecord) {
  return {
    actorId: actor.actorId,
    roles: actor.roles,
    linkedWorkerId: actor.linkedWorkerId,
  };
}

export function canStartLegalNameWorkflow(
  actor: ActorRecord,
  subjectId: string,
): Result<true, AppError> {
  if (
    actor.roles.includes(ACTOR_ROLES.EMPLOYEE) &&
    actor.linkedWorkerId === subjectId
  ) {
    return ok(true);
  }

  return err(permissionDeniedError({ permission: PERMISSION_KEYS.LEGAL_NAME_REQUEST }));
}

export function canViewWorkflowInstance(
  actor: ActorRecord,
  workflowInstance: WorkflowInstanceRecord,
): Result<true, AppError> {
  if (actor.roles.includes(ACTOR_ROLES.HR_ADMIN)) {
    return ok(true);
  }

  if (actor.linkedWorkerId === workflowInstance.subjectId) {
    return ok(true);
  }

  return err(
    permissionDeniedError({ workflowInstanceId: workflowInstance.workflowInstanceId }),
  );
}

export function canCreateDocumentForWorkflow(
  actor: ActorRecord,
  workflowInstance: WorkflowInstanceRecord,
): Result<true, AppError> {
  if (actor.linkedWorkerId === workflowInstance.subjectId) {
    return ok(true);
  }

  return err(permissionDeniedError({ permission: "document.create" }));
}

export function canViewDocument(
  actor: ActorRecord,
  ownerActorId: string,
): Result<true, AppError> {
  if (actor.actorId === ownerActorId || actor.roles.includes(ACTOR_ROLES.HR_ADMIN)) {
    return ok(true);
  }

  return err(
    permissionDeniedError({ permission: PERMISSION_KEYS.LEGAL_NAME_VIEW_EVIDENCE }),
  );
}

export function computeAvailableActions(input: {
  actor: ActorRecord;
  workflowInstance: WorkflowInstanceRecord;
  pendingApprovalTask?: ApprovalTaskRecord;
}): Array<Record<string, unknown>> {
  const { actor, workflowInstance, pendingApprovalTask } = input;
  const isRequester = actor.linkedWorkerId === workflowInstance.subjectId;
  const isHrAdmin = actor.roles.includes(ACTOR_ROLES.HR_ADMIN);
  const isSystem = actor.roles.includes(ACTOR_ROLES.SYSTEM);

  if (workflowInstance.state === WORKFLOW_STATES.COLLECTING_INPUT && isRequester) {
    return [
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        label: "Continue",
        enabled: true,
      },
      { transition: WORKFLOW_TRANSITIONS.CANCEL, label: "Cancel", enabled: true },
    ];
  }

  if (workflowInstance.state === WORKFLOW_STATES.COLLECTING_EVIDENCE && isRequester) {
    return [
      {
        transition: WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE,
        label: "Submit evidence",
        enabled: true,
      },
      {
        transition: WORKFLOW_TRANSITIONS.CANCEL,
        label: "Cancel request",
        enabled: true,
      },
    ];
  }

  if (workflowInstance.state === WORKFLOW_STATES.WAITING_APPROVAL && isHrAdmin) {
    return [
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        label: "Approve",
        enabled: true,
        taskId: pendingApprovalTask?.approvalTaskId,
      },
      {
        transition: WORKFLOW_TRANSITIONS.REJECT,
        label: "Reject",
        enabled: true,
        taskId: pendingApprovalTask?.approvalTaskId,
      },
      {
        transition: WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO,
        label: "Request more information",
        enabled: true,
        taskId: pendingApprovalTask?.approvalTaskId,
      },
    ];
  }

  if (workflowInstance.state === WORKFLOW_STATES.WAITING_APPROVAL && isRequester) {
    return [
      {
        transition: WORKFLOW_TRANSITIONS.CANCEL,
        label: "Cancel request",
        enabled: true,
      },
    ];
  }

  if (workflowInstance.state === WORKFLOW_STATES.APPROVED && (isHrAdmin || isSystem)) {
    return [
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        label: "Execute change",
        enabled: true,
      },
    ];
  }

  return [];
}

export function canSubmitTransition(input: {
  actor: ActorRecord;
  workflowInstance: WorkflowInstanceRecord;
  transition: WorkflowTransition;
  pendingApprovalTask?: ApprovalTaskRecord;
}): Result<true, AppError> {
  const availableActions = computeAvailableActions(input);
  const hasAction = availableActions.some((action) => {
    return action["transition"] === input.transition;
  });

  if (!hasAction) {
    return err(
      permissionDeniedError({
        transition: input.transition,
        state: input.workflowInstance.state,
      }),
    );
  }

  return ok(true);
}
