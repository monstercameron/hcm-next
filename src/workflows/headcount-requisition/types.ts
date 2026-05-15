import type { ApprovalTaskRecord, WorkflowInstanceRecord } from "@hcm-next/data-store";
import type {
  WorkflowActionConfig,
  WorkflowConfig,
} from "../shared/workflow-config.js";

export type HeadcountTransitionBody = {
  transition: string;
  idempotencyKey: string;
  expectedVersion: number;
  input: Record<string, unknown>;
};

export type HeadcountInput = {
  department: string;
  team: string;
  location: string;
  costCenter: string;
  jobCode: string;
  title: string;
  level: string;
  requestedFte: number;
  targetStartDate: string;
  salaryRangeMin: number;
  salaryRangeMax: number;
  businessJustification: string;
  selectedLeadershipApprovers: string[];
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
  projectionPatches?: Record<string, unknown>[];
  externalCallRequests: Array<{
    connectionId: string;
    operation: string;
    idempotencyKey: string;
    payload: Record<string, unknown>;
    reconciliation?: Record<string, unknown>;
  }>;
};

export type HeadcountApprovalDecisionInput = {
  approvalTaskId: string;
  taskVersion?: number | undefined;
  comment?: string | undefined;
  reason?: string | undefined;
};

export type ResolvedHeadcountApprover = {
  resolverId: string;
  actorId: string;
  role: string;
  label: string;
  taskKey: string;
  approvalType: string;
  permission: string;
  sequenceIndex: number;
  opensWithGate: boolean;
  isVetoHolder: boolean;
  weight: number;
  resolvedFrom: Record<string, unknown>;
};

export type HeadcountTransitionInput = {
  workflowConfig: WorkflowConfig;
  actionConfig?: WorkflowActionConfig;
  workflowInstance: WorkflowInstanceRecord;
  transitionBody: HeadcountTransitionBody;
  pendingApprovalTask?: ApprovalTaskRecord;
};
