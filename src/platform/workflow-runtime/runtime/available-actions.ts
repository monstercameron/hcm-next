import {
  WORKFLOW_STATES,
  WORKFLOW_TRANSITIONS,
  type WorkflowTransition,
} from "../constants";
import type { RequestContext } from "../context";
import type { Actor, ApprovalTask, WorkflowAction, WorkflowInstance } from "../domain";
import { LEGAL_NAME_TRANSITION_MAP } from "./legal-name-workflow";
import { permissionForTransition, requireWorkflowPermission } from "./permissions";

/** Computes actor-specific actions for the current workflow state. */
export function getAvailableActionsForActor(input: {
  context: RequestContext;
  actor: Actor;
  workflowInstance: WorkflowInstance;
  pendingApprovalTask?: ApprovalTask;
}): WorkflowAction[] {
  const transitions = LEGAL_NAME_TRANSITION_MAP[input.workflowInstance.state] ?? [];

  return transitions
    .filter((transition) => isTransitionActionRelevant(transition, input))
    .map((transition) => buildWorkflowAction(transition, input))
    .filter((action): action is WorkflowAction => action !== undefined);
}

function isTransitionActionRelevant(
  transition: WorkflowTransition,
  input: {
    workflowInstance: WorkflowInstance;
    pendingApprovalTask?: ApprovalTask;
  },
): boolean {
  if (
    [
      WORKFLOW_TRANSITIONS.APPROVE,
      WORKFLOW_TRANSITIONS.REJECT,
      WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO,
    ].includes(transition)
  ) {
    return input.pendingApprovalTask !== undefined;
  }

  if (
    transition === WORKFLOW_TRANSITIONS.CANCEL &&
    input.workflowInstance.state === WORKFLOW_STATES.APPROVED
  ) {
    return true;
  }

  return true;
}

function buildWorkflowAction(
  transition: WorkflowTransition,
  input: {
    context: RequestContext;
    actor: Actor;
    workflowInstance: WorkflowInstance;
    pendingApprovalTask?: ApprovalTask;
  },
): WorkflowAction | undefined {
  const permission = permissionForTransition(transition);
  const resource =
    input.pendingApprovalTask !== undefined
      ? {
          workflowInstance: input.workflowInstance,
          approvalTask: input.pendingApprovalTask,
        }
      : { workflowInstance: input.workflowInstance };
  const permissionResult = requireWorkflowPermission(
    input.context,
    input.actor,
    permission,
    resource,
  );

  if (!permissionResult.ok) {
    return undefined;
  }

  if (transition === WORKFLOW_TRANSITIONS.SUBMIT_INPUT) {
    return { transition, label: "Continue", enabled: true, requiresPayload: true };
  }

  if (transition === WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE) {
    return {
      transition,
      label: "Submit evidence",
      enabled: true,
      requiresPayload: true,
      inputSchema: {
        type: "object",
        required: ["documentId"],
        properties: { documentId: { type: "string" } },
      },
    };
  }

  if (transition === WORKFLOW_TRANSITIONS.APPROVE) {
    return {
      transition,
      label: "Approve",
      enabled: true,
      requiresPayload: true,
      inputSchema: approvalDecisionSchema(),
    };
  }

  if (transition === WORKFLOW_TRANSITIONS.REJECT) {
    return {
      transition,
      label: "Reject",
      enabled: true,
      requiresPayload: true,
      inputSchema: {
        ...approvalDecisionSchema(),
        required: ["approvalTaskId", "reason"],
      },
    };
  }

  if (transition === WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO) {
    return {
      transition,
      label: "Request more information",
      enabled: true,
      requiresPayload: true,
      inputSchema: {
        type: "object",
        required: ["approvalTaskId", "comment"],
        properties: {
          approvalTaskId: { type: "string" },
          comment: { type: "string", minLength: 1 },
        },
      },
    };
  }

  if (transition === WORKFLOW_TRANSITIONS.EXECUTE) {
    return { transition, label: "Execute change", enabled: true };
  }

  return {
    transition,
    label: "Cancel request",
    enabled: true,
    requiresPayload: false,
  };
}

function approvalDecisionSchema() {
  return {
    type: "object",
    required: ["approvalTaskId"],
    properties: {
      approvalTaskId: { type: "string" },
      comment: { type: "string" },
      reason: { type: "string" },
    },
  };
}
