import {
  ACTOR_ROLES,
  WORKFLOW_STATUSES,
  WORKFLOW_TRANSITIONS,
  ok,
  validationFailedError,
  type AppError,
  type Result,
  type WorkflowState,
  type WorkflowStatus,
} from "@human-capital-management-suite/foundation";
import type {
  ActorRecord,
  ApprovalTaskRecord,
  EmployeeProjectionRecord,
  WorkflowInstanceRecord,
} from "@human-capital-management-suite/data-store";
import {
  evaluateEmployeeAccess,
  SUPPORTED_EMPLOYEE_FIELD_GROUPS,
  type EmployeeAccessDecision,
  type EmployeeAccessEvaluationContext,
  type EmployeeFieldGroup,
} from "../../shared/employee-access.js";
import type {
  WorkflowActionConfig,
  WorkflowConfig,
} from "../../shared/workflow-config.js";
import { computeConfiguredAvailableActions } from "../../runtime/permissions.js";

export type WorkflowPermissionPreviewRequest = {
  tenantId: string;
  environmentId: string;
  actor: ActorRecord;
  employeeProjection: EmployeeProjectionRecord;
  workflowConfig: WorkflowConfig;
  workflowState: WorkflowState | string;
  workflowStatus?: WorkflowStatus | undefined;
  workflowInput?: Record<string, unknown> | undefined;
  accessContext?: EmployeeAccessEvaluationContext | undefined;
  pendingTasks?: ApprovalTaskRecord[] | undefined;
  workflowInstance?: WorkflowInstanceRecord | undefined;
  evaluatedAt?: string | undefined;
};

export type WorkflowActionPermissionPreview = {
  transition: string;
  label: string;
  actor: string;
  handler: string;
  allowed: boolean;
  reasonCode: string;
  approvalTaskIds: string[];
};

export type WorkflowFieldVisibilityPreview = {
  fieldGroup: EmployeeFieldGroup;
  allowed: boolean;
  decision: EmployeeAccessDecision["decision"];
  reasonCode: string;
  hiddenPaths: string[];
};

export type WorkflowRelationshipPreview = {
  self: boolean;
  manager: boolean;
  managerChain: boolean;
  hrbp: boolean;
  compensationAdmin: boolean;
  financeApprover: boolean;
  orgAdmin: boolean;
  workflowApprover: boolean;
};

export type WorkflowPermissionPreview = {
  tenantId: string;
  environmentId: string;
  workflowIntent: string;
  workflowState: string;
  actorId: string;
  employeeId: string;
  roles: string[];
  abacAttributes: Record<string, unknown>;
  relationships: WorkflowRelationshipPreview;
  fieldVisibility: WorkflowFieldVisibilityPreview[];
  availableActions: WorkflowActionPermissionPreview[];
  deniedActions: WorkflowActionPermissionPreview[];
  approvalAuthority: boolean;
  executionAuthority: boolean;
  repairAuthority: boolean;
  aiVisibleFieldGroups: EmployeeFieldGroup[];
  aiRedactedPaths: string[];
};

const approvalHandlers = new Set<string>([
  WORKFLOW_TRANSITIONS.APPROVE,
  WORKFLOW_TRANSITIONS.REJECT,
  WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO,
]);
const orgAdminRoles = new Set<string>([
  ACTOR_ROLES.HR_ADMIN,
  ACTOR_ROLES.CLINIC_OPS_ADMIN,
  ACTOR_ROLES.SYSTEM,
]);
const defaultWorkflowStatus = WORKFLOW_STATUSES.ACTIVE as WorkflowStatus;

const hiddenPathsByFieldGroup: Record<EmployeeFieldGroup, string[]> = {
  profile: ["person"],
  organization: ["organization", "manager"],
  job: ["job"],
  employment: ["employment"],
  compensation: ["compensation"],
  contact: ["contact"],
  emergencyContacts: ["emergencyContacts"],
  workflow: ["workflow", "changeRequest", "transactionPlan"],
};

/**
 * Builds a dry-run permission view for one actor, employee, workflow state, and config.
 */
export function previewWorkflowPermissions(
  input: WorkflowPermissionPreviewRequest,
): Result<WorkflowPermissionPreview, AppError> {
  const stateConfig = input.workflowConfig.states[input.workflowState];

  if (stateConfig === undefined) {
    return {
      ok: false,
      error: validationFailedError({
        workflowState: input.workflowState,
        intent: input.workflowConfig.intent,
      }),
    };
  }

  const evaluatedAt = input.evaluatedAt ?? new Date().toISOString();
  const workflowInstance =
    input.workflowInstance ??
    syntheticWorkflowInstance({
      input,
      evaluatedAt,
    });
  const accessContext = {
    ...(input.accessContext ?? {}),
    evaluatedAt,
  };
  const fieldVisibility = SUPPORTED_EMPLOYEE_FIELD_GROUPS.map((fieldGroup) =>
    fieldVisibilityForGroup({
      actor: input.actor,
      employeeProjection: input.employeeProjection,
      accessContext,
      fieldGroup,
    }),
  );
  const availableRuntimeActions = computeConfiguredAvailableActions({
    actor: input.actor,
    workflowConfig: input.workflowConfig,
    workflowInstance,
    pendingTasks: input.pendingTasks ?? [],
  });
  const allowedTransitionKeys = new Set(
    availableRuntimeActions.map((action) => String(action["transition"])),
  );
  const actionPreviews = stateConfig.actions.map((actionConfig) =>
    actionPreviewForConfig({
      actionConfig,
      allowedTransitionKeys,
      actor: input.actor,
      pendingTasks: input.pendingTasks ?? [],
      workflowInstance,
    }),
  );
  const allowedActions = actionPreviews.filter((action) => action.allowed);
  const deniedActions = actionPreviews.filter((action) => !action.allowed);
  const aiVisibleFieldGroups = fieldVisibility
    .filter((fieldGroupPreview) => fieldGroupPreview.allowed)
    .map((fieldGroupPreview) => fieldGroupPreview.fieldGroup);
  const aiRedactedPaths = fieldVisibility.flatMap((fieldGroupPreview) =>
    fieldGroupPreview.allowed ? [] : fieldGroupPreview.hiddenPaths,
  );

  return ok({
    tenantId: input.tenantId,
    environmentId: input.environmentId,
    workflowIntent: input.workflowConfig.intent,
    workflowState: input.workflowState,
    actorId: input.actor.actorId,
    employeeId: input.employeeProjection.employeeId,
    roles: [...input.actor.roles],
    abacAttributes: abacAttributesFrom(input.employeeProjection),
    relationships: relationshipPreviewFrom({
      actor: input.actor,
      employeeProjection: input.employeeProjection,
      pendingTasks: input.pendingTasks ?? [],
    }),
    fieldVisibility,
    availableActions: allowedActions,
    deniedActions,
    approvalAuthority: allowedActions.some((action) =>
      approvalHandlers.has(action.handler),
    ),
    executionAuthority: allowedActions.some(
      (action) => action.handler === WORKFLOW_TRANSITIONS.EXECUTE,
    ),
    repairAuthority:
      input.workflowState.includes("repair") ||
      allowedActions.some((action) => action.transition.includes("repair")),
    aiVisibleFieldGroups,
    aiRedactedPaths,
  });
}

function fieldVisibilityForGroup(input: {
  actor: ActorRecord;
  employeeProjection: EmployeeProjectionRecord;
  accessContext: EmployeeAccessEvaluationContext;
  fieldGroup: EmployeeFieldGroup;
}): WorkflowFieldVisibilityPreview {
  const accessDecision = evaluateEmployeeAccess({
    actor: input.actor,
    employeeDocument: input.employeeProjection.document,
    indexedFields: input.employeeProjection.indexedFields,
    fieldGroup: input.fieldGroup,
    access: input.accessContext,
  });
  const allowed = accessDecision.decision === "allow";

  return {
    fieldGroup: input.fieldGroup,
    allowed,
    decision: accessDecision.decision,
    reasonCode: accessDecision.reason,
    hiddenPaths: allowed ? [] : hiddenPathsByFieldGroup[input.fieldGroup],
  };
}

function actionPreviewForConfig(input: {
  actionConfig: WorkflowActionConfig;
  allowedTransitionKeys: Set<string>;
  actor: ActorRecord;
  pendingTasks: ApprovalTaskRecord[];
  workflowInstance: WorkflowInstanceRecord;
}): WorkflowActionPermissionPreview {
  const matchingApprovalTaskIds = input.pendingTasks
    .filter(
      (task) => task.workflowInstanceId === input.workflowInstance.workflowInstanceId,
    )
    .filter((task) => {
      return (
        task.assigneeActorId === input.actor.actorId ||
        input.actor.roles.includes(task.assigneeRole)
      );
    })
    .map((task) => task.approvalTaskId);
  const allowed = input.allowedTransitionKeys.has(input.actionConfig.transition);

  return {
    transition: input.actionConfig.transition,
    label: input.actionConfig.label,
    actor: input.actionConfig.actor,
    handler: input.actionConfig.handler,
    allowed,
    reasonCode: allowed
      ? "configured_action_allowed"
      : deniedActionReasonCode(input.actionConfig, matchingApprovalTaskIds),
    approvalTaskIds: matchingApprovalTaskIds,
  };
}

function deniedActionReasonCode(
  actionConfig: WorkflowActionConfig,
  matchingApprovalTaskIds: string[],
): string {
  if (
    approvalHandlers.has(actionConfig.handler) &&
    matchingApprovalTaskIds.length === 0
  ) {
    return "missing_approval_task_assignment";
  }

  return "actor_token_or_scope_not_matched";
}

function relationshipPreviewFrom(input: {
  actor: ActorRecord;
  employeeProjection: EmployeeProjectionRecord;
  pendingTasks: ApprovalTaskRecord[];
}): WorkflowRelationshipPreview {
  const managerChain = stringValuesAtPaths(input.employeeProjection.document, [
    "manager.chain",
    "manager.managerChain",
    "manager.managerChainEmployeeIds",
    "organization.managerChain",
    "custom.managerChain",
    "custom.managerChainEmployeeIds",
  ]);
  const actorLinkedWorkerId = input.actor.linkedWorkerId;
  const roles = new Set(input.actor.roles);

  return {
    self:
      actorLinkedWorkerId !== undefined &&
      actorLinkedWorkerId === input.employeeProjection.employeeId,
    manager:
      actorLinkedWorkerId !== undefined &&
      actorLinkedWorkerId === input.employeeProjection.document.manager.employeeId,
    managerChain:
      actorLinkedWorkerId !== undefined && managerChain.includes(actorLinkedWorkerId),
    hrbp: roles.has("hrbp"),
    compensationAdmin: roles.has(ACTOR_ROLES.COMPENSATION_ADMIN),
    financeApprover:
      roles.has(ACTOR_ROLES.FINANCE_ADMIN) || roles.has("finance_approver"),
    orgAdmin: [...roles].some((role) => orgAdminRoles.has(role)),
    workflowApprover: input.pendingTasks.some((task) => {
      return (
        task.assigneeActorId === input.actor.actorId ||
        input.actor.roles.includes(task.assigneeRole)
      );
    }),
  };
}

function abacAttributesFrom(
  employeeProjection: EmployeeProjectionRecord,
): Record<string, unknown> {
  return {
    tenantId: employeeProjection.tenantId,
    employeeId: employeeProjection.employeeId,
    department: employeeProjection.document.organization.department,
    team: employeeProjection.document.organization.team,
    businessUnit: employeeProjection.document.organization.businessUnit,
    legalEntity: employeeProjection.document.organization.legalEntity,
    location: employeeProjection.document.organization.location,
    costCenter: employeeProjection.document.organization.costCenter,
    jobLevel: employeeProjection.document.job.level,
    employmentStatus: employeeProjection.document.employment.status,
  };
}

function syntheticWorkflowInstance(input: {
  input: WorkflowPermissionPreviewRequest;
  evaluatedAt: string;
}): WorkflowInstanceRecord {
  const workflowStatus = input.input.workflowStatus ?? defaultWorkflowStatus;

  return {
    workflowInstanceId: "permission_preview_workflow_instance",
    tenantId: input.input.tenantId,
    environmentId: input.input.environmentId,
    workflowDefinitionId: "permission_preview_workflow_definition",
    workflowVersionId: "permission_preview_workflow_version",
    intent: input.input.workflowConfig.intent,
    subjectType: input.input.workflowConfig.subjectType,
    subjectId: input.input.employeeProjection.employeeId,
    status: workflowStatus,
    state: input.input.workflowState as WorkflowState,
    requesterActorId: input.input.actor.actorId,
    currentInteraction: {},
    context: input.input.workflowInput ?? {},
    startedAt: input.evaluatedAt,
    version: 1,
    correlationId: "permission_preview",
    metadata: {},
    createdAt: input.evaluatedAt,
    updatedAt: input.evaluatedAt,
  };
}

function stringValuesAtPaths(
  source: Record<string, unknown>,
  paths: string[],
): string[] {
  return paths.flatMap((path) => stringValues(valueAtPath(source, path)));
}

function stringValues(value: unknown): string[] {
  if (typeof value === "string" && value.trim().length > 0) {
    return [value.trim()];
  }

  if (Array.isArray(value)) {
    return value.flatMap((item) => stringValues(item));
  }

  return [];
}

function valueAtPath(source: Record<string, unknown>, path: string): unknown {
  const pathSegments = path.split(".").filter((segment) => segment.length > 0);
  let currentValue: unknown = source;

  for (const pathSegment of pathSegments) {
    if (typeof currentValue !== "object" || currentValue === null) {
      return undefined;
    }

    currentValue = (currentValue as Record<string, unknown>)[pathSegment];
  }

  return currentValue;
}
