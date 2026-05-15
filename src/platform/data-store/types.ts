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
  intent?: string | undefined;
  subjectType?: string | undefined;
  name: string;
  description?: string | undefined;
  workflowType: string;
  status: string;
  currentVersionId?: string | undefined;
  createdByActorId?: string | undefined;
  updatedByActorId?: string | undefined;
  activatedAt?: string | undefined;
  deprecatedAt?: string | undefined;
  createdAt?: string | undefined;
  updatedAt?: string | undefined;
  metadata?: Record<string, unknown> | undefined;
};

export type WorkflowVersionValidationStatus =
  | "not_validated"
  | "pending"
  | "valid"
  | "invalid";

export type WorkflowVersionValidationResultRecord = {
  status: WorkflowVersionValidationStatus;
  errors: Record<string, unknown>[];
  warnings: Record<string, unknown>[];
  summary: Record<string, unknown>;
  validatedAt?: string | undefined;
  validatedByActorId?: string | undefined;
};

export type WorkflowVersionRecord = {
  workflowVersionId: string;
  tenantId: string;
  workflowDefinitionId: string;
  versionNumber: number;
  configHash?: string | undefined;
  graphDefinition: Record<string, unknown>;
  inputSchema: Record<string, unknown>;
  outputSchema: Record<string, unknown>;
  validationRules: Record<string, unknown>[];
  approvalRules: Record<string, unknown>[];
  aiReviewScope: Record<string, unknown>;
  status: string;
  validationStatus?: WorkflowVersionValidationStatus | undefined;
  validationResult?: WorkflowVersionValidationResultRecord | undefined;
  authorActorId?: string | undefined;
  createdByActorId?: string | undefined;
  updatedByActorId?: string | undefined;
  publishedByActorId?: string | undefined;
  deprecatedByActorId?: string | undefined;
  publishedAt?: string | undefined;
  deprecatedAt?: string | undefined;
  createdAt?: string | undefined;
  updatedAt?: string | undefined;
  metadata?: Record<string, unknown> | undefined;
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

export type ApprovalGroupStatus =
  | "active"
  | "passed"
  | "failed"
  | "repair"
  | "canceled";

export type ApprovalGroupRecord = {
  approvalGroupId: string;
  tenantId: string;
  workflowInstanceId: string;
  changeRequestId?: string | undefined;
  gateNodeId: string;
  mode: string;
  status: ApprovalGroupStatus;
  passRule: Record<string, unknown>;
  failurePolicy: string;
  currentSequenceIndex?: number | undefined;
  openedAt: string;
  completedAt?: string | undefined;
  failedAt?: string | undefined;
  canceledAt?: string | undefined;
  metadata: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type ApprovalTaskRecord = {
  approvalTaskId: string;
  tenantId: string;
  changeRequestId: string;
  workflowInstanceId: string;
  approvalGroupId?: string | undefined;
  gateNodeId?: string | undefined;
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
  sequenceIndex?: number | undefined;
  weight?: number | undefined;
  isVetoHolder?: boolean | undefined;
  resolvedFrom?: Record<string, unknown> | undefined;
  taskVersion?: number | undefined;
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
  projectionPatches: Record<string, unknown>[];
  externalWrites: Record<string, unknown>[];
  assignmentOperations?: Record<string, unknown>[];
  roleBindingOperations?: Record<string, unknown>[];
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
  contact: ContactInfo;
  employment: EmploymentInfo;
  organization: OrganizationInfo;
  manager: {
    employeeId: string | null;
  };
  job: JobInfo;
  compensation: CompensationInfo;
  emergencyContacts: EmergencyContact[];
  custom: Record<string, unknown>;
};

export type ContactInfo = {
  personalEmail: string | null;
  mobilePhone: string | null;
  homeAddress: PostalAddress;
  workPhone?: string | null;
};

export type EmploymentInfo = {
  status: string;
  legalEntity: string;
  hireDate: string;
  workerType: string;
};

export type OrganizationInfo = {
  legalEntity: string;
  businessUnit: string;
  department: string;
  team: string;
  location: string;
  payZone: string;
  costCenter: string;
};

export type JobInfo = {
  jobCode: string;
  title: string;
  family: string;
  level: string;
};

export type CompensationInfo = {
  amount: number;
  currency: string;
  payFrequency: string;
  bonusTargetPercent: number;
  effectiveDate: string;
};

export type PostalAddress = {
  line1: string;
  line2: string | null;
  city: string;
  region: string;
  postalCode: string;
  country: string;
};

export type LegalName = {
  first: string;
  middle: string | null;
  last: string;
};

export type EmergencyContact = {
  contactId: string;
  name: string;
  relationship: string;
  phone: string;
  email: string | null;
  priority: number;
};

export type AccessScopeType =
  | "self"
  | "direct_reports"
  | "manager_chain"
  | "business_unit"
  | "department"
  | "team"
  | "location"
  | "cost_center"
  | "legal_entity"
  | "global";

export type EmployeeFieldGroup =
  | "profile"
  | "organization"
  | "job"
  | "employment"
  | "contact"
  | "emergency_contacts"
  | "emergencyContacts"
  | "workflow"
  | "compensation";

export type AccessGrantRecord = {
  accessGrantId: string;
  tenantId: string;
  actorId: string;
  permissions: string[];
  scope: {
    type: AccessScopeType;
    values?: string[];
  };
  fieldGroups: EmployeeFieldGroup[];
  status: string;
  startsAt: string;
  expiresAt?: string | undefined;
  metadata: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type OrgLifecycleStatus =
  | "proposed"
  | "pending_approval"
  | "active"
  | "suspended"
  | "expired"
  | "revoked"
  | "superseded"
  | "inactive";

export type OrganizationUnitType =
  | "enterprise"
  | "legal_entity"
  | "employing_entity"
  | "payroll_unit"
  | "business_unit"
  | "division"
  | "department"
  | "team"
  | "location"
  | "region"
  | "cost_center"
  | "project"
  | "program"
  | "clinic"
  | "store"
  | "franchisee"
  | "supplier"
  | "joint_venture"
  | "board"
  | "committee"
  | "works_council"
  | "union"
  | "volunteer_group"
  | "member_group";

export type OrganizationUnitRecord = {
  orgUnitId: string;
  tenantId: string;
  unitKey: string;
  type: OrganizationUnitType;
  name: string;
  status: OrgLifecycleStatus;
  country?: string | undefined;
  jurisdiction?: string | undefined;
  effectiveStart: string;
  effectiveEnd?: string | undefined;
  metadata: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type OrganizationRelationshipType =
  | "part_of"
  | "reports_to"
  | "owns"
  | "controls"
  | "employs"
  | "operates"
  | "funds"
  | "governs"
  | "represents"
  | "franchises"
  | "supplies"
  | "located_in"
  | "allocated_to";

export type OrganizationRelationshipRecord = {
  organizationRelationshipId: string;
  tenantId: string;
  fromOrgUnitId: string;
  toOrgUnitId: string;
  relationshipType: OrganizationRelationshipType;
  status: OrgLifecycleStatus;
  effectiveStart: string;
  effectiveEnd?: string | undefined;
  metadata: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type AssignmentLifecycleStatus = Exclude<OrgLifecycleStatus, "inactive">;

export type WorkerAssignmentType =
  | "legal_employer"
  | "primary_team"
  | "work_location"
  | "cost_center"
  | "project"
  | "program"
  | "committee"
  | "board_seat"
  | "volunteer_assignment"
  | "member_affiliation"
  | "representative_body";

export type WorkerAssignmentRecord = {
  workerAssignmentId: string;
  tenantId: string;
  employeeId: string;
  orgUnitId: string;
  assignmentType: WorkerAssignmentType;
  roleType?: string | undefined;
  managerEmployeeId?: string | undefined;
  allocationPercent: number;
  status: AssignmentLifecycleStatus;
  effectiveStart: string;
  effectiveEnd?: string | undefined;
  sourceWorkflowInstanceId?: string | undefined;
  metadata: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
};

export type RoleBindingScopeType =
  | "self"
  | "direct_reports"
  | "manager_chain"
  | "org_unit"
  | "org_unit_descendants"
  | "legal_entity"
  | "location"
  | "country"
  | "cost_center"
  | "project"
  | "assignment"
  | "workflow_instance"
  | "global";

export type RoleBindingRecord = {
  roleBindingId: string;
  tenantId: string;
  actorId: string;
  roleKey: string;
  scopeType: RoleBindingScopeType;
  scopeOrgUnitId?: string | undefined;
  scopeValue?: string | undefined;
  relationshipType?: string | undefined;
  status: AssignmentLifecycleStatus;
  effectiveStart: string;
  effectiveEnd?: string | undefined;
  sourceWorkflowInstanceId?: string | undefined;
  metadata: Record<string, unknown>;
  createdAt: string;
  updatedAt: string;
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
