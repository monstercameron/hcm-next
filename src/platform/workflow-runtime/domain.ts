import type {
  ActorType,
  ApprovalTaskStatus,
  ChangeRequestStatus,
  ChangeRequestType,
  DocumentClassification,
  DocumentStatus,
  IntegrationOutboxStatus,
  LedgerEventType,
  PermissionKey,
  TransitionAttemptStatus,
  WorkflowIntent,
  WorkflowState,
  WorkflowStatus,
  WorkflowTransition,
} from "./constants";
import type { PermissionSnapshot } from "./context";

export type JsonPrimitive = string | number | boolean | null;
export type JsonValue = JsonPrimitive | JsonObject | JsonValue[];
export type JsonObject = { [key: string]: JsonValue };

export type Actor = {
  actorId: string;
  tenantId: string;
  actorType: ActorType;
  linkedWorkerId?: string;
  displayName: string;
  roles: string[];
};

export type DemoTenantEnvironment = {
  tenantId: string;
  environmentId: string;
};

export type LegalName = {
  first: string;
  middle?: string | null;
  last: string;
};

export type EmployeeProjectionDocument = {
  employeeId: string;
  person: {
    personId: string;
    legalName: LegalName;
    displayName: string;
    preferredName?: string | null;
    workEmail?: string | null;
  };
  employment: {
    status: string;
    legalEntity?: string;
  };
  manager?: {
    employeeId?: string;
  };
  custom: JsonObject;
};

export type EmployeeProjection = {
  tenantId: string;
  employeeId: string;
  projectionVersion: number;
  document: EmployeeProjectionDocument;
  updatedAt: string;
};

export type WorkflowSubject = {
  type: "worker";
  id: string;
};

export type WorkflowAction = {
  transition: WorkflowTransition;
  label: string;
  enabled: boolean;
  requiresPayload?: boolean;
  inputSchema?: JsonObject;
};

export type CurrentInteraction = {
  type: string;
  schemaVersion?: string;
  title: string;
  description?: string;
  jsonSchema?: JsonObject;
  acceptedDocumentTypes?: string[];
  maxFiles?: number;
  errors?: JsonObject;
};

export type WorkflowDefinitionVersion = {
  workflowDefinitionId: string;
  workflowVersionId: string;
  intent: WorkflowIntent;
  versionNumber: number;
  graphDefinition: JsonObject;
  inputSchema: JsonObject;
  status: "published";
};

export type WorkflowInstance = {
  workflowInstanceId: string;
  tenantId: string;
  environmentId: string;
  workflowDefinitionId: string;
  workflowVersionId: string;
  intent: WorkflowIntent;
  subject: WorkflowSubject;
  status: WorkflowStatus;
  state: WorkflowState;
  changeRequestId?: string;
  requesterActorId: string;
  currentInteraction: CurrentInteraction;
  context: JsonObject;
  startedAt: string;
  completedAt?: string;
  canceledAt?: string;
  failedAt?: string;
  version: number;
  correlationId: string;
  metadata: JsonObject;
  createdAt: string;
  updatedAt: string;
};

export type WorkflowIntentRequest = {
  intent: string;
  subject: {
    type: string;
    id: string;
  };
};

export type WorkflowTransitionRequest = {
  transition: string;
  idempotencyKey: string;
  expectedVersion: number;
  payload: JsonObject;
};

export type WorkflowTransitionAttempt = {
  workflowTransitionAttemptId: string;
  tenantId: string;
  workflowInstanceId: string;
  transition: WorkflowTransition;
  idempotencyKey: string;
  expectedVersion: number;
  actorId: string;
  requestPayload: JsonObject;
  responsePayload?: JsonObject;
  status: TransitionAttemptStatus;
  error?: JsonObject;
  createdAt: string;
  completedAt?: string;
};

export type ChangeRequest = {
  changeRequestId: string;
  tenantId: string;
  changeType: ChangeRequestType;
  targetWorkerId: string;
  requesterActorId: string;
  effectiveAt: string;
  businessReason: string;
  status: ChangeRequestStatus;
  currentSnapshot: JsonObject;
  proposedSnapshot: JsonObject;
  preflightResult: JsonObject;
  workflowDefinitionId: string;
  workflowVersionId: string;
  transactionPlanId?: string;
  submittedAt?: string;
  approvedAt?: string;
  executedAt?: string;
  closedAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type ProposedChange = {
  proposedChangeId: string;
  tenantId: string;
  changeRequestId: string;
  targetObjectType: "person";
  targetObjectId: string;
  fieldPath: string;
  currentValue: JsonValue;
  proposedValue: JsonValue;
  effectiveAt: string;
  reasonCode: string;
  validationStatus: "valid" | "invalid" | "pending";
  riskLevel: "low" | "medium" | "high" | "unknown";
  metadata: JsonObject;
  createdAt: string;
  updatedAt: string;
};

export type DocumentRecord = {
  documentId: string;
  tenantId: string;
  workflowInstanceId: string;
  purpose: string;
  filename: string;
  contentType: string;
  classification: DocumentClassification | string;
  status: DocumentStatus;
  uploadedByActorId: string;
  metadata: JsonObject;
  createdAt: string;
  updatedAt: string;
};

export type WorkflowInstanceDocument = {
  tenantId: string;
  workflowInstanceId: string;
  documentId: string;
  attachedByActorId: string;
  attachedAt: string;
};

export type ApprovalTask = {
  approvalTaskId: string;
  tenantId: string;
  changeRequestId: string;
  workflowInstanceId: string;
  assigneeActorId?: string;
  assigneeRole: string;
  approvalType: "legal_name_change";
  status: ApprovalTaskStatus;
  decision?: "approved" | "rejected" | "skipped";
  decisionReason?: string;
  comments?: string;
  createdAt: string;
  decidedAt?: string;
  metadata: JsonObject;
};

export type TransactionPlan = {
  transactionPlanId: string;
  tenantId: string;
  changeRequestId: string;
  status: "ready" | "executing" | "executed" | "failed" | "canceled";
  planVersion: number;
  steps: JsonObject[];
  internalWrites: JsonObject[];
  externalWrites: JsonObject[];
  idempotencyKeys: JsonObject;
  executionResult: JsonObject;
  createdAt: string;
  updatedAt: string;
};

export type IntegrationOutboxRecord = {
  outboxId: string;
  tenantId: string;
  changeRequestId: string;
  transactionPlanId: string;
  ledgerEventId?: string;
  destination: string;
  operation: string;
  requestPayload: JsonObject;
  status: IntegrationOutboxStatus;
  idempotencyKey: string;
  createdAt: string;
  updatedAt: string;
};

export type LedgerEvent = {
  eventId: string;
  sequence: number;
  tenantId: string;
  eventType: LedgerEventType;
  eventVersion: number;
  subjectType: string;
  subjectId: string;
  occurredAt: string;
  effectiveAt?: string;
  actorType: ActorType;
  actorId: string;
  actorRole?: string;
  workflowInstanceId?: string;
  workflowVersionId?: string;
  changeRequestId?: string;
  transactionPlanId?: string;
  approvalTaskId?: string;
  correlationId: string;
  causationId?: string;
  idempotencyKey?: string;
  permissionSnapshot: PermissionSnapshot;
  payload: JsonObject;
};

export type LedgerEventInput = Omit<LedgerEvent, "eventId" | "sequence">;

export type CreateDocumentRequest = {
  purpose: string;
  filename: string;
  contentType: string;
  classification: string;
  workflowInstanceId: string;
  metadata?: JsonObject;
};

export type WorkflowInstanceResponse = {
  workflowInstance: WorkflowInstance;
  currentInteraction: CurrentInteraction;
  availableActions: WorkflowAction[];
  changeRequest?: ChangeRequestSummary;
};

export type ChangeRequestSummary = {
  changeRequestId: string;
  status: ChangeRequestStatus;
  targetWorkerId: string;
  effectiveAt: string;
  businessReason: string;
};

export type TransitionResponse = WorkflowInstanceResponse & {
  idempotentReplay?: boolean;
  timelinePreview?: TimelineEntry[];
};

export type TimelineEntry = {
  eventId: string;
  eventType: LedgerEventType;
  occurredAt: string;
  actorId: string;
  summary: string;
  payloadExcerpt: JsonObject;
};

export type PermissionDecision = {
  isAllowed: boolean;
  permission: PermissionKey;
  snapshot: PermissionSnapshot;
};
