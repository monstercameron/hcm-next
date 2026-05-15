export const GO_EXECUTOR_BLOCKS = {
  LEGAL_NAME_PREFLIGHT: "system.employee_data.legal_name.preflight",
  LEGAL_NAME_PLAN_TRANSACTION: "system.employee_data.legal_name.plan_transaction",
} as const;

export const GO_EXECUTOR_BLOCK_VERSION = {
  V1: "1.0.0",
} as const;

export const GO_EXECUTOR_STATUS = {
  SUCCEEDED: "succeeded",
  FAILED: "failed",
} as const;

export type GoExecutorBlockName =
  (typeof GO_EXECUTOR_BLOCKS)[keyof typeof GO_EXECUTOR_BLOCKS];

export type GoExecutorStatus =
  (typeof GO_EXECUTOR_STATUS)[keyof typeof GO_EXECUTOR_STATUS];

export type GoExecutorBlockReference = {
  name: string;
  version: string;
};

export type GoExecutorContext = {
  actorId: string;
  effectiveAt: string;
  permissions: Record<string, unknown>;
  correlationId: string;
  idempotencyKey: string;
};

export type GoExecutorRequest<Input = unknown> = {
  tenantId: string;
  environmentId: string;
  changeRequestId?: string;
  workflowInstanceId: string;
  workflowVersionId: string;
  block: GoExecutorBlockReference;
  input: Input;
  context: GoExecutorContext;
};

export type GoExecutorProposedEvent = {
  eventType: string;
  subjectType: string;
  subjectId: string;
  effectiveAt?: string;
  payload: Record<string, unknown>;
};

export type GoExecutorExternalCallRequest = {
  connectionId: string;
  operation: string;
  idempotencyKey: string;
  payload: Record<string, unknown>;
  reconciliation?: Record<string, unknown>;
  capabilityHints?: string[];
};

export type GoExecutorLog = {
  level: string;
  message: string;
  fields?: Record<string, unknown>;
};

export type GoExecutorMetrics = {
  durationMs: number;
};

export type GoExecutorErrorResponse = {
  code: string;
  message: string;
  safeMessage: string;
  details?: Record<string, unknown>;
};

export type GoExecutorResponse<Output = unknown> = {
  requestId?: string;
  correlationId?: string;
  status: GoExecutorStatus;
  output: Output;
  proposedEvents: GoExecutorProposedEvent[];
  externalCallRequests: GoExecutorExternalCallRequest[];
  logs: GoExecutorLog[];
  metrics: GoExecutorMetrics;
  error?: GoExecutorErrorResponse;
};

export type LegalName = {
  first: string;
  middle?: string | null;
  last: string;
};

export type LegalNameValidationMessage = {
  code: string;
  field: string;
  message: string;
};

export type LegalNamePreflightInput = {
  currentLegalName: LegalName;
  proposedLegalName: LegalName;
  effectiveAt: string;
  businessReason: string;
};

export type LegalNamePreflightOutput = {
  valid: boolean;
  riskLevel: string;
  requiresEvidence: boolean;
  requiresApproval: boolean;
  warnings: LegalNameValidationMessage[];
  errors: LegalNameValidationMessage[];
};

export type LegalNameInternalWriteSpec = {
  eventType: string;
  subjectType: string;
  subjectId: string;
  effectiveAt: string;
  payload: {
    personId: string;
    previousLegalName: LegalName;
    newLegalName: LegalName;
  };
};

export type LegalNamePlanTransactionInput = {
  changeRequestId: string;
  workerId: string;
  personId: string;
  currentLegalName: LegalName;
  proposedLegalName: LegalName;
  effectiveAt: string;
};

export type LegalNamePlanTransactionOutput = {
  internalWrites: LegalNameInternalWriteSpec[];
  externalCallRequests: GoExecutorExternalCallRequest[];
};
