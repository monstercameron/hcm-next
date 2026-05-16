import {
  ACTOR_ROLES,
  PERMISSION_KEYS,
  WORKFLOW_STATES,
  WORKFLOW_TRANSITIONS,
  type PermissionKey,
  type WorkflowTransition,
} from "../constants";
import type { RequestContext } from "../context";
import type {
  Actor,
  ApprovalTask,
  PermissionDecision,
  WorkflowInstance,
} from "../domain";
import type { PermissionSnapshot } from "../context";
import { err, ok, permissionDeniedError, type Result } from "../result";

export type PermissionResource = {
  workflowInstance?: WorkflowInstance;
  approvalTask?: ApprovalTask;
  subjectWorkerId?: string;
};

/** Builds the auditable permission snapshot included on workflow ledger events. */
export function buildPermissionSnapshot(
  context: RequestContext,
  actor: Actor,
  resource: PermissionResource,
): PermissionSnapshot {
  const subjectWorkerId =
    resource.subjectWorkerId ?? resource.workflowInstance?.subject.id;
  const relationship = resolveRelationship(context, actor, subjectWorkerId);
  const allowedPermissions = Object.values(PERMISSION_KEYS).filter((permission) =>
    canUsePermission(context, actor, permission, resource, relationship),
  );

  return {
    actorId: context.actorId,
    roles: context.roles,
    relationship,
    allowedPermissions,
  };
}

/** Enforces a V0 workflow permission and returns a snapshot for audit events. */
export function requireWorkflowPermission(
  context: RequestContext,
  actor: Actor,
  permission: PermissionKey,
  resource: PermissionResource,
): Result<PermissionDecision> {
  const snapshot = buildPermissionSnapshot(context, actor, resource);
  const isAllowed = snapshot.allowedPermissions.includes(permission);

  if (!isAllowed) {
    return err(
      permissionDeniedError({
        permission,
        workflowInstanceId: resource.workflowInstance?.workflowInstanceId,
      }),
    );
  }

  return ok({ isAllowed, permission, snapshot });
}

export function canViewWorkflowInstance(
  context: RequestContext,
  actor: Actor,
  workflowInstance: WorkflowInstance,
): boolean {
  const relationship = resolveRelationship(context, actor, workflowInstance.subject.id);
  return (
    relationship === "self" ||
    relationship === ACTOR_ROLES.HR_ADMIN ||
    relationship === ACTOR_ROLES.SYSTEM
  );
}

export function canViewEvidence(
  context: RequestContext,
  actor: Actor,
  workflowInstance: WorkflowInstance,
): boolean {
  const snapshot = buildPermissionSnapshot(context, actor, { workflowInstance });
  return snapshot.allowedPermissions.includes(PERMISSION_KEYS.LEGAL_NAME_VIEW_EVIDENCE);
}

export function permissionForTransition(transition: WorkflowTransition): PermissionKey {
  if (transition === WORKFLOW_TRANSITIONS.SUBMIT_INPUT) {
    return PERMISSION_KEYS.LEGAL_NAME_REQUEST;
  }

  if (transition === WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE) {
    return PERMISSION_KEYS.LEGAL_NAME_PROVIDE_EVIDENCE;
  }

  if (transition === WORKFLOW_TRANSITIONS.APPROVE) {
    return PERMISSION_KEYS.LEGAL_NAME_APPROVE;
  }

  if (transition === WORKFLOW_TRANSITIONS.REJECT) {
    return PERMISSION_KEYS.LEGAL_NAME_REJECT;
  }

  if (transition === WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO) {
    return PERMISSION_KEYS.LEGAL_NAME_REJECT;
  }

  if (transition === WORKFLOW_TRANSITIONS.EXECUTE) {
    return PERMISSION_KEYS.LEGAL_NAME_EXECUTE;
  }

  return PERMISSION_KEYS.LEGAL_NAME_CANCEL_OWN;
}

export function isTerminalWorkflowState(workflowInstance: WorkflowInstance): boolean {
  return [
    WORKFLOW_STATES.EXECUTED,
    WORKFLOW_STATES.REJECTED,
    WORKFLOW_STATES.CANCELED,
    WORKFLOW_STATES.FAILED,
  ].includes(workflowInstance.state);
}

function resolveRelationship(
  context: RequestContext,
  actor: Actor,
  subjectWorkerId?: string,
): PermissionSnapshot["relationship"] {
  if (context.roles.includes(ACTOR_ROLES.SYSTEM)) {
    return ACTOR_ROLES.SYSTEM;
  }

  if (context.roles.includes(ACTOR_ROLES.HR_ADMIN)) {
    return ACTOR_ROLES.HR_ADMIN;
  }

  if (actor.linkedWorkerId && actor.linkedWorkerId === subjectWorkerId) {
    return "self";
  }

  return "none";
}

function canUsePermission(
  _context: RequestContext,
  actor: Actor,
  permission: PermissionKey,
  resource: PermissionResource,
  relationship: PermissionSnapshot["relationship"],
): boolean {
  if (relationship === ACTOR_ROLES.SYSTEM) {
    return permission === PERMISSION_KEYS.LEGAL_NAME_EXECUTE;
  }

  if (relationship === ACTOR_ROLES.HR_ADMIN) {
    return [
      PERMISSION_KEYS.LEGAL_NAME_APPROVE,
      PERMISSION_KEYS.LEGAL_NAME_REJECT,
      PERMISSION_KEYS.LEGAL_NAME_EXECUTE,
      PERMISSION_KEYS.LEGAL_NAME_VIEW_EVIDENCE,
    ].includes(permission);
  }

  if (relationship !== "self") {
    return false;
  }

  if (permission === PERMISSION_KEYS.LEGAL_NAME_REQUEST) {
    return true;
  }

  if (permission === PERMISSION_KEYS.LEGAL_NAME_PROVIDE_EVIDENCE) {
    return true;
  }

  if (permission === PERMISSION_KEYS.LEGAL_NAME_VIEW_EVIDENCE) {
    return true;
  }

  if (permission !== PERMISSION_KEYS.LEGAL_NAME_CANCEL_OWN) {
    return false;
  }

  const workflowInstance = resource.workflowInstance;
  return (
    workflowInstance !== undefined &&
    workflowInstance.requesterActorId === actor.actorId &&
    [
      WORKFLOW_STATES.COLLECTING_INPUT,
      WORKFLOW_STATES.COLLECTING_EVIDENCE,
      WORKFLOW_STATES.WAITING_APPROVAL,
    ].includes(workflowInstance.state)
  );
}
