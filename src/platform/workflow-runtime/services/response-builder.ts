import type { RequestContext } from "../context";
import type {
  Actor,
  ApprovalTask,
  ChangeRequest,
  ChangeRequestSummary,
  WorkflowInstance,
  WorkflowInstanceResponse,
} from "../domain";
import { getAvailableActionsForActor } from "../runtime/available-actions";

/** Shapes workflow instance output for public API responses. */
export function buildWorkflowInstanceResponse(input: {
  context: RequestContext;
  actor: Actor;
  workflowInstance: WorkflowInstance;
  changeRequest?: ChangeRequest;
  pendingApprovalTask?: ApprovalTask;
}): WorkflowInstanceResponse {
  const response: WorkflowInstanceResponse = {
    workflowInstance: input.workflowInstance,
    currentInteraction: input.workflowInstance.currentInteraction,
    availableActions: getAvailableActionsForActor({
      context: input.context,
      actor: input.actor,
      workflowInstance: input.workflowInstance,
    }),
  };

  if (input.pendingApprovalTask !== undefined) {
    response.availableActions = getAvailableActionsForActor({
      context: input.context,
      actor: input.actor,
      workflowInstance: input.workflowInstance,
      pendingApprovalTask: input.pendingApprovalTask,
    });
  }

  if (input.changeRequest !== undefined) {
    response.changeRequest = buildChangeRequestSummary(input.changeRequest);
  }

  return response;
}

export function buildChangeRequestSummary(
  changeRequest: ChangeRequest,
): ChangeRequestSummary {
  return {
    changeRequestId: changeRequest.changeRequestId,
    status: changeRequest.status,
    targetWorkerId: changeRequest.targetWorkerId,
    effectiveAt: changeRequest.effectiveAt,
    businessReason: changeRequest.businessReason,
  };
}
