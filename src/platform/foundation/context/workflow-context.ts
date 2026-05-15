import type { JsonRecord } from "../domain";
import type { RequestContext } from "./request-context";

export type WorkflowContext = RequestContext & {
  workflowInstanceId: string;
  changeRequestId?: string;
  permissionSnapshot: JsonRecord;
};
