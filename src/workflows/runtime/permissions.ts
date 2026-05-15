import {
  ACTOR_ROLES,
  err,
  ok,
  permissionDeniedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  ActorRecord,
  ApprovalTaskRecord,
  WorkflowInstanceRecord,
} from "@hcm-next/data-store";
import type {
  WorkflowActionConfig,
  WorkflowConfig,
} from "../shared/workflow-config.js";

const requesterActor = "requester";
const initiatorActor = "initiator";
const approvalTaskAssigneeActor = "approval_task_assignee";

/**
 * Captures the actor attributes safe to persist with executor and ledger context.
 */
export function createPermissionSnapshot(actor: ActorRecord): Record<string, unknown> {
  return {
    actorId: actor.actorId,
    roles: actor.roles,
    linkedWorkerId: actor.linkedWorkerId,
  };
}

/**
 * Checks whether the actor can start a workflow from config-level start rules.
 */
export function canStartConfiguredWorkflow(input: {
  actor: ActorRecord;
  workflowConfig: WorkflowConfig;
  subjectId: string;
}): Result<true, AppError> {
  if (
    input.workflowConfig.selfServiceStart &&
    input.actor.roles.includes(ACTOR_ROLES.EMPLOYEE) &&
    input.actor.linkedWorkerId === input.subjectId
  ) {
    return ok(true);
  }

  const configuredStartActors = configuredStartActorTokens(input.workflowConfig);
  const canStartFromConfiguredRole = configuredStartActors.some((actorToken) => {
    return actorMatchesRoleToken(input.actor, actorToken);
  });

  if (canStartFromConfiguredRole) {
    return ok(true);
  }

  return err(
    permissionDeniedError({
      intent: input.workflowConfig.intent,
      subjectId: input.subjectId,
    }),
  );
}

/**
 * Checks workflow visibility without knowing the workflow's business domain.
 */
export function canViewConfiguredWorkflow(input: {
  actor: ActorRecord;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  pendingTasks?: ApprovalTaskRecord[];
}): Result<true, AppError> {
  if (input.actor.actorId === input.workflowInstance.requesterActorId) {
    return ok(true);
  }

  if (
    input.actor.linkedWorkerId !== undefined &&
    input.actor.linkedWorkerId === input.workflowInstance.subjectId
  ) {
    return ok(true);
  }

  if (
    (input.pendingTasks ?? []).some((task) => {
      return actorOwnsApprovalTask(input.actor, task);
    })
  ) {
    return ok(true);
  }

  const configuredActorTokens = configuredWorkflowActorTokens(input.workflowConfig);
  const hasConfiguredRole = configuredActorTokens.some((actorToken) => {
    return actorMatchesRoleToken(input.actor, actorToken);
  });

  if (hasConfiguredRole) {
    return ok(true);
  }

  return err(
    permissionDeniedError({
      workflowInstanceId: input.workflowInstance.workflowInstanceId,
    }),
  );
}

/**
 * Returns transition actions permitted for the actor in the current state.
 */
export function computeConfiguredAvailableActions(input: {
  actor: ActorRecord;
  workflowConfig: WorkflowConfig;
  workflowInstance: WorkflowInstanceRecord;
  pendingTasks?: ApprovalTaskRecord[];
}): Array<Record<string, unknown>> {
  const stateConfig = input.workflowConfig.states[input.workflowInstance.state];
  const actions = stateConfig?.actions ?? [];
  const availableActions: Array<Record<string, unknown>> = [];

  for (const actionConfig of actions) {
    const matchingTasks = matchingApprovalTasksForAction({
      actor: input.actor,
      actionConfig,
      pendingTasks: input.pendingTasks ?? [],
    });

    if (actionConfig.actor === approvalTaskAssigneeActor) {
      for (const task of matchingTasks) {
        availableActions.push(toActionResponse(actionConfig, task));
      }
      continue;
    }

    if (
      actorCanPerformConfiguredAction({
        actor: input.actor,
        actionConfig,
        workflowInstance: input.workflowInstance,
        pendingTasks: input.pendingTasks ?? [],
      })
    ) {
      availableActions.push(toActionResponse(actionConfig, matchingTasks[0]));
    }
  }

  return availableActions;
}

/**
 * Guards command transitions against the configured action and approval task state.
 */
export function canSubmitConfiguredTransition(input: {
  actor: ActorRecord;
  actionConfig: WorkflowActionConfig;
  workflowInstance: WorkflowInstanceRecord;
  pendingTasks?: ApprovalTaskRecord[];
}): Result<true, AppError> {
  const canPerform = actorCanPerformConfiguredAction({
    actor: input.actor,
    actionConfig: input.actionConfig,
    workflowInstance: input.workflowInstance,
    ...(input.pendingTasks !== undefined ? { pendingTasks: input.pendingTasks } : {}),
  });

  if (canPerform) {
    return ok(true);
  }

  return err(
    permissionDeniedError({
      transition: input.actionConfig.transition,
      state: input.workflowInstance.state,
    }),
  );
}

export function actorOwnsApprovalTask(
  actor: ActorRecord,
  approvalTask: ApprovalTaskRecord,
): boolean {
  const assignmentMode = approvalTask.metadata["assignmentMode"];
  const canUseRoleFallback =
    assignmentMode !== "actor" && actor.roles.includes(approvalTask.assigneeRole);

  return approvalTask.assigneeActorId === actor.actorId || canUseRoleFallback;
}

export function actionRequiresApprovalTask(
  actionConfig: WorkflowActionConfig,
): boolean {
  return (
    actionConfig.handler === "approve" ||
    actionConfig.handler === "reject" ||
    actionConfig.handler === "request_more_info"
  );
}

function actorCanPerformConfiguredAction(input: {
  actor: ActorRecord;
  actionConfig: WorkflowActionConfig;
  workflowInstance: WorkflowInstanceRecord;
  pendingTasks?: ApprovalTaskRecord[];
}): boolean {
  if (input.actionConfig.actor === requesterActor) {
    return input.actor.linkedWorkerId === input.workflowInstance.subjectId;
  }

  if (input.actionConfig.actor === initiatorActor) {
    return input.actor.actorId === input.workflowInstance.requesterActorId;
  }

  if (input.actionConfig.actor === approvalTaskAssigneeActor) {
    return (input.pendingTasks ?? []).some((task) => {
      return actorOwnsApprovalTask(input.actor, task);
    });
  }

  if (
    actionRequiresApprovalTask(input.actionConfig) &&
    (input.pendingTasks ?? []).some((task) => {
      return actorOwnsApprovalTask(input.actor, task);
    })
  ) {
    return true;
  }

  return actorMatchesRoleToken(input.actor, input.actionConfig.actor);
}

function matchingApprovalTasksForAction(input: {
  actor: ActorRecord;
  actionConfig: WorkflowActionConfig;
  pendingTasks: ApprovalTaskRecord[];
}): ApprovalTaskRecord[] {
  if (!actionRequiresApprovalTask(input.actionConfig)) {
    return [];
  }

  return input.pendingTasks.filter((task) => {
    if (input.actionConfig.actor === approvalTaskAssigneeActor) {
      return actorOwnsApprovalTask(input.actor, task);
    }

    return actorOwnsApprovalTask(input.actor, task);
  });
}

function toActionResponse(
  actionConfig: WorkflowActionConfig,
  approvalTask?: ApprovalTaskRecord,
): Record<string, unknown> {
  return {
    transition: actionConfig.transition,
    label: actionConfig.label,
    enabled: true,
    ...(approvalTask !== undefined ? { taskId: approvalTask.approvalTaskId } : {}),
  };
}

function actorMatchesRoleToken(actor: ActorRecord, actorToken: string): boolean {
  if (actorToken.includes("_or_")) {
    return actorToken.split("_or_").some((role) => {
      return actor.roles.includes(role);
    });
  }

  return actor.roles.includes(actorToken);
}

function configuredWorkflowActorTokens(workflowConfig: WorkflowConfig): string[] {
  const actorTokens = new Set<string>(configuredStartActorTokens(workflowConfig));

  for (const stateConfig of Object.values(workflowConfig.states)) {
    for (const action of stateConfig.actions) {
      actorTokens.add(action.actor);
    }
  }

  if (workflowConfig.approval.assigneeRole.trim().length > 0) {
    actorTokens.add(workflowConfig.approval.assigneeRole);
  }

  return [...actorTokens].filter((actorToken) => {
    return (
      actorToken !== requesterActor &&
      actorToken !== initiatorActor &&
      actorToken !== approvalTaskAssigneeActor
    );
  });
}

function configuredStartActorTokens(workflowConfig: WorkflowConfig): string[] {
  return Array.isArray(workflowConfig.startActors)
    ? workflowConfig.startActors.map(String)
    : [];
}
