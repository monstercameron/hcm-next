import type { WorkflowTransition } from "@hcm-next/foundation";
import type { WorkflowInstanceRecord } from "@hcm-next/data-store";
import type {
  WorkflowActionConfig,
  WorkflowConfig,
} from "../shared/workflow-config.js";

export type EvidenceInput = {
  documentId: string;
};

export type ApprovalDecisionInput = {
  approvalTaskId: string;
  taskVersion?: number;
  comment?: string;
  reason?: string;
};

export type TransitionBody = {
  transition: WorkflowTransition;
  idempotencyKey: string;
  expectedVersion: number;
  input: Record<string, unknown>;
};

export type RuntimeTransitionInput = {
  workflowConfig: WorkflowConfig;
  actionConfig: WorkflowActionConfig;
  workflowInstance: WorkflowInstanceRecord;
  transitionBody: TransitionBody;
};

export type PreflightOutput = {
  valid: boolean;
  riskLevel: string;
  requiresEvidence?: boolean;
  requiresApproval?: boolean;
  warnings?: Record<string, unknown>[];
  errors?: Record<string, unknown>[];
};

export type PlanTransactionOutput = {
  internalWrites?: Array<{
    eventType: string;
    subjectType: string;
    subjectId: string;
    effectiveAt: string;
    payload: Record<string, unknown>;
  }>;
  projectionPatches?: Array<{
    projection: string;
    operation: string;
    path: string;
    value: unknown;
  }>;
  externalCallRequests?: Array<{
    connectionId: string;
    operation: string;
    idempotencyKey: string;
    payload: Record<string, unknown>;
    reconciliation?: Record<string, unknown>;
  }>;
  assignmentOperations?: Record<string, unknown>[];
  roleBindingOperations?: Record<string, unknown>[];
};

export type ExternalWriteExecution = {
  connectionId: string;
  operation: string;
  idempotencyKey: string;
  outcome: string;
  eventType?: string;
  nextNodeId?: string;
  requestPayload: Record<string, unknown>;
  responsePayload: Record<string, unknown>;
};

export type ExternalWriteExecutionResult =
  | {
      status: "succeeded";
      executions: ExternalWriteExecution[];
    }
  | {
      status: "routed";
      workflowInstance: WorkflowInstanceRecord;
    };

export type AdditionalLedgerEventInput = {
  eventType: string;
  approvalTaskId?: string;
  transactionPlanId?: string;
  payload: Record<string, unknown>;
};
