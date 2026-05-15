import type { RequestContext } from "../context";
import type {
  Actor,
  ApprovalTask,
  ChangeRequestSummary,
  WorkflowInstance,
} from "../domain";
import { ok, type Result } from "../result";
import type { WorkflowRuntimeStore } from "../storage/workflow-store";
import { buildChangeRequestSummary } from "./response-builder";

export type VisibleApprovalTask = {
  approvalTask: ApprovalTask;
  workflowInstanceId: string;
  workflowState: WorkflowInstance["state"];
  changeRequest?: ChangeRequestSummary;
};

export type TaskService = {
  /** Lists approval tasks visible to the current actor. */
  listTasks(
    context: RequestContext,
    actor: Actor,
    status?: string,
  ): Promise<Result<VisibleApprovalTask[]>>;
};

export function createTaskService(store: WorkflowRuntimeStore): TaskService {
  return new DefaultTaskService(store);
}

class DefaultTaskService implements TaskService {
  constructor(private readonly store: WorkflowRuntimeStore) {}

  async listTasks(
    context: RequestContext,
    _actor: Actor,
    status?: string,
  ): Promise<Result<VisibleApprovalTask[]>> {
    const tasksResult = await this.store.listApprovalTasks(context, status);
    if (!tasksResult.ok) {
      return tasksResult;
    }

    const visibleTasks: VisibleApprovalTask[] = [];
    for (const approvalTask of tasksResult.value) {
      const workflowResult = await this.store.findWorkflowInstanceById(
        context.tenantId,
        approvalTask.workflowInstanceId,
      );
      if (!workflowResult.ok) {
        return workflowResult;
      }

      const changeRequestResult = await this.store.findChangeRequestById(
        context.tenantId,
        approvalTask.changeRequestId,
      );
      if (!changeRequestResult.ok) {
        return changeRequestResult;
      }

      visibleTasks.push({
        approvalTask,
        workflowInstanceId: approvalTask.workflowInstanceId,
        workflowState: workflowResult.value.state,
        changeRequest: buildChangeRequestSummary(changeRequestResult.value),
      });
    }

    return ok(visibleTasks);
  }
}
