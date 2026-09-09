import type { WorkflowInstanceRecord } from "@human-capital-management-suite/data-store";

/**
 * Serializes the stable public workflow-instance shape used by API responses.
 */
export function serializeWorkflowInstance(
  workflowInstance: WorkflowInstanceRecord,
): Record<string, unknown> {
  return {
    workflowInstanceId: workflowInstance.workflowInstanceId,
    workflowDefinitionId: workflowInstance.workflowDefinitionId,
    workflowVersionId: workflowInstance.workflowVersionId,
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

/**
 * Creates a terminal interaction payload for completed, rejected, or canceled flows.
 */
export function terminalInteraction(status: string): Record<string, unknown> {
  return {
    type: "terminal",
    status,
  };
}
