import type { ActorType } from "./constants";

export type RequestContext = {
  tenantId: string;
  environmentId: string;
  actorId: string;
  actorType: ActorType;
  roles: string[];
  requestId: string;
  correlationId: string;
};

export type WorkflowContext = RequestContext & {
  workflowInstanceId: string;
  changeRequestId?: string;
  permissionSnapshot: PermissionSnapshot;
};

export type PermissionSnapshot = {
  actorId: string;
  roles: string[];
  relationship: "self" | "hr_admin" | "system" | "none";
  allowedPermissions: string[];
};
