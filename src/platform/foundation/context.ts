export type RequestContext = {
  tenantId: string;
  environmentId: string;
  actorId: string;
  actorType: string;
  roles: string[];
  requestId: string;
  correlationId: string;
};

export type WorkflowContext = RequestContext & {
  workflowInstanceId: string;
  changeRequestId?: string;
  permissionSnapshot: Record<string, unknown>;
};
