import type { WorkflowTransition } from "@hcm-next/foundation";
import type { WorkflowInstanceRecord } from "@hcm-next/data-store";

export type EvidenceInput = {
  documentId: string;
};

export type ApprovalDecisionInput = {
  approvalTaskId: string;
  comment?: string;
  reason?: string;
};

export type TransitionBody = {
  transition: WorkflowTransition;
  idempotencyKey: string;
  expectedVersion: number;
  input: Record<string, unknown>;
};

export type PreflightOutput = {
  valid: boolean;
  riskLevel: string;
  requiresEvidence: boolean;
  requiresApproval: boolean;
  warnings: Record<string, unknown>[];
  errors: Record<string, unknown>[];
};

export type PlanTransactionOutput = {
  internalWrites: Array<{
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
  externalCallRequests: Array<{
    connectionId: string;
    operation: string;
    idempotencyKey: string;
    payload: Record<string, unknown>;
    reconciliation?: Record<string, unknown>;
  }>;
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
