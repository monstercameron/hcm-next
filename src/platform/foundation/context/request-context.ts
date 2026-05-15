import type { ActorType } from "../constants";
import type { JsonRecord } from "../domain";

export type RequestContext = {
  tenantId: string;
  environmentId: string;
  actorId: string;
  actorType: ActorType;
  roles: readonly string[];
  requestId: string;
  correlationId: string;
  permissions?: readonly string[];
  metadata?: JsonRecord;
};
