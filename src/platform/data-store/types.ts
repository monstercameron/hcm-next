import type {
  ApprovalTaskStatus,
  ChangeRequestStatus,
  WorkflowState,
  WorkflowStatus,
} from "@hcm-next/foundation";

export type TenantRecord = {
  tenantId: string;
  name: string;
  slug: string;
  status: string;
};

export type EnvironmentRecord = {
  environmentId: string;
  tenantId: string;
  name: string;
  type: string;
  status: string;
};

export type ActorRecord = {
  actorId: string;
  tenantId: string;
  actorType: string;
  linkedWorkerId?: string | undefined;
  email?: string | undefined;
  displayName: string;
  status: string;
  roles: string[];
};

export type WorkflowDefinitionRecord = {
  workflowDefinitionId: string;
  tenantId: string;
  name: string;
  workflowType: string;
  status: string;
  currentVersionId: string;
};

export type WorkflowVersionRecord = {
  workflowVersionId: string;
  tenantId: string;
  workflowDefinitionId: string;
  versionNumber: number;
  graphDefinition: Record<string, unknown>;
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
  validationRules: Record<string, unknown>[];
  approvalRules: Record<string, unknown>[];
  aiReviewScope: Record<string, unknown>;
  status: string;
};

export type WorkflowInstanceRecord = {
  workflowInstanceId: string;
  tenantId: string;
  environmentId: string;
  workflowDefinitionId: string;
  workflowVersionId: string;
  intent: string;
  subjectType: string;
  subjectId: string;
  status: WorkflowStatus;
  state: WorkflowState;
  changeRequestId?: string | undefined;
  requesterActorId: string;
  currentInteraction: Record<string, unknown>;
  context: Record<string, unknown>;
  startedAt: string;
  completedAt?: string | undefined;
  canceledAt?: string | undefined;
  failedAt?: string | undefined;
  version: number;
  correlationId: string;
  metadata: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type WorkflowTransitionAttemptRecord = {
  workflowTransitionAttemptId: string;
  tenantId: string;
  workflowInstanceId: string;
  transition: string;
  idempotencyKey: string;
  expectedVersion: number;
  actorId: string;
  requestPayload: Record<string, unknown>;
  responsePayload?: Record<string, unknown> | undefined;
  status: "started" | "completed" | "failed";
  error?: Record<string, unknown> | undefined;
  createdAt: string;
  completedAt?: string | undefined;
};

export type LedgerEventRecord = {
  eventId: string;
  tenantId: string;
  eventType: string;
  eventVersion: number;
  eventSequence: number;
  subjectType: string;
  subjectId: string;
  occurredAt: string;
  effectiveAt?: string;
  recordedAt: string;
  actorType: string;
  actorId: string;
  actorRole?: string | undefined;
  relationshipContext: Record<string, unknown>;
  workflowInstanceId?: string | undefined;
  workflowDefinitionId?: string | undefined;
  workflowVersionId?: string | undefined;
  changeRequestId?: string | undefined;
  transactionPlanId?: string | undefined;
  approvalTaskId?: string | undefined;
  correlationId: string;
  causationId?: string | undefined;
  idempotencyKey?: string | undefined;
  permissionSnapshot: Record<string, unknown>;
  aiVisibilitySnapshot: Record<string, unknown>;
  payload: Record<string, unknown>;
};

export type ChangeRequestRecord = {
  changeRequestId: string;
  tenantId: string;
  environmentId: string;
  changeType: string;
  targetWorkerId: string;
  requesterActorId: string;
  effectiveAt: string;
  businessReason: string;
  status: ChangeRequestStatus;
  priority: string;
  currentSnapshot: Record<string, unknown>;
  proposedSnapshot: Record<string, unknown>;
  preflightResult: Record<string, unknown>;
  aiReview: Record<string, unknown>;
  workflowDefinitionId: string;
  workflowVersionId: string;
  transactionPlanId?: string | undefined;
  submittedAt?: string | undefined;
  approvedAt?: string | undefined;
  executedAt?: string | undefined;
  closedAt?: string | undefined;
  createdAt: string;
  updatedAt: string;
  createdBy: string;
  updatedBy: string;
  version: number;
  metadata: Record<string, unknown>;
};

export type ProposedChangeRecord = {
  proposedChangeId: string;
  tenantId: string;
  changeRequestId: string;
  targetObjectType: string;
  targetObjectId: string;
  fieldPath: string;
  currentValue: unknown;
  proposedValue: unknown;
  effectiveAt: string;
  reasonCode: string;
  validationStatus: string;
  riskLevel: string;
  metadata: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type DocumentRecord = {
  documentId: string;
  tenantId: string;
  purpose: string;
  filename: string;
  contentType: string;
  classification: string;
  workflowInstanceId: string;
  ownerActorId: string;
  status: string;
  metadata: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type WorkflowInstanceDocumentRecord = {
  workflowInstanceDocumentId: string;
  tenantId: string;
  workflowInstanceId: string;
  documentId: string;
  attachedByActorId: string;
  createdAt: string;
};

export type ApprovalTaskRecord = {
  approvalTaskId: string;
  tenantId: string;
  changeRequestId: string;
  workflowInstanceId: string;
  assigneeActorId: string;
  assigneeRole: string;
  assigneeRelationship?: string | undefined;
  approvalType: string;
  status: ApprovalTaskStatus;
  decision?: string | undefined;
  decisionReason?: string | undefined;
  comments?: string | undefined;
  dueAt?: string | undefined;
  delegatedToActorId?: string | undefined;
  createdAt: string;
  decidedAt?: string | undefined;
  metadata: Record<string, unknown>;
};

export type TransactionPlanRecord = {
  transactionPlanId: string;
  tenantId: string;
  changeRequestId: string;
  status: string;
  planVersion: number;
  steps: Record<string, unknown>[];
  internalWrites: Record<string, unknown>[];
  externalWrites: Record<string, unknown>[];
  rollbackPlan: Record<string, unknown>;
  compensationPlan: Record<string, unknown>;
  idempotencyKeys: Record<string, unknown>;
  simulationResult: Record<string, unknown>;
  executionResult: Record<string, unknown>;
  reconciliationResult: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
  createdBy: string;
  updatedBy: string;
};

export type EmployeeProjectionRecord = {
  tenantId: string;
  employeeId: string;
  projectionVersion: number;
  asOfEffectiveAt?: string | undefined;
  sourceEventId?: string | undefined;
  sourceEventSequence?: number | undefined;
  document: EmployeeProjectionDocument;
  indexedFields: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type EmployeeProjectionDocument = {
  employeeId: string;
  person: {
    personId: string;
    legalName: LegalName;
    displayName: string;
    preferredName: string | null;
    workEmail: string;
  };
  employment: {
    status: string;
    legalEntity: string;
  };
  manager: {
    employeeId: string;
  };
  custom: Record<string, unknown>;
};

export type LegalName = {
  first: string;
  middle: string | null;
  last: string;
};

export type IntegrationOutboxRecord = {
  outboxId: string;
  tenantId: string;
  changeRequestId?: string | undefined;
  transactionPlanId?: string | undefined;
  ledgerEventId?: string | undefined;
  destination: string;
  operation: string;
  requestPayload: Record<string, unknown>;
  responsePayload?: Record<string, unknown> | undefined;
  status: string;
  attemptCount: number;
  maxAttempts: number;
  idempotencyKey: string;
  createdAt: string;
  updatedAt: string;
};
