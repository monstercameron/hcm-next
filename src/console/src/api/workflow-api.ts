type ApiRequestOptions = {
  actorId?: string;
  init?: RequestInit;
};

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "/api";

const readJson = <TResponse>(
  path: string,
  options: ApiRequestOptions = {},
): Promise<TResponse> => {
  const headers = new Headers(options.init?.headers);

  if (options.actorId !== undefined) {
    headers.set("x-demo-actor-id", options.actorId);
  }

  return fetch(`${apiBaseUrl}${path}`, {
    ...options.init,
    headers,
  }).then((response) => response.json() as Promise<TResponse>);
};

export type WorkflowInstanceSummary = {
  workflowInstanceId: string;
  state: string;
  status: string;
  version: number;
};

export type ApprovalTaskSummary = {
  approvalTaskId: string;
  workflowInstanceId: string;
  assignedActorId: string;
  status: string;
};

export const workflowApi = {
  getWorkflowInstance: (workflowInstanceId: string, actorId: string) =>
    readJson<WorkflowInstanceSummary>(`/workflow-instances/${workflowInstanceId}`, {
      actorId,
    }),
  getAvailableActions: (workflowInstanceId: string, actorId: string) =>
    readJson<ReadonlyArray<string>>(
      `/workflow-instances/${workflowInstanceId}/available-actions`,
      { actorId },
    ),
  getTimeline: (workflowInstanceId: string, actorId: string) =>
    readJson<ReadonlyArray<unknown>>(
      `/workflow-instances/${workflowInstanceId}/timeline`,
      { actorId },
    ),
  getPendingTasks: (actorId: string) =>
    readJson<ReadonlyArray<ApprovalTaskSummary>>("/tasks?status=pending", {
      actorId,
    }),
};
