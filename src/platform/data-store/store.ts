import { randomUUID } from "node:crypto";
import {
  ACTOR_ROLES,
  ACTOR_TYPES,
  DOCUMENT_CLASSIFICATIONS,
  DOCUMENT_STATUSES,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
} from "@hcm-next/foundation";
import type {
  ActorRecord,
  ApprovalTaskRecord,
  ChangeRequestRecord,
  DocumentRecord,
  EmployeeProjectionRecord,
  EnvironmentRecord,
  IntegrationOutboxRecord,
  LedgerEventRecord,
  ProposedChangeRecord,
  TenantRecord,
  TransactionPlanRecord,
  WorkflowDefinitionRecord,
  WorkflowInstanceDocumentRecord,
  WorkflowInstanceRecord,
  WorkflowTransitionAttemptRecord,
  WorkflowVersionRecord,
} from "./types.js";

export type HcmNextStore = {
  tenants: Map<string, TenantRecord>;
  environments: Map<string, EnvironmentRecord>;
  actors: Map<string, ActorRecord>;
  workflowDefinitions: Map<string, WorkflowDefinitionRecord>;
  workflowVersions: Map<string, WorkflowVersionRecord>;
  workflowInstances: Map<string, WorkflowInstanceRecord>;
  transitionAttempts: Map<string, WorkflowTransitionAttemptRecord>;
  ledgerEvents: LedgerEventRecord[];
  changeRequests: Map<string, ChangeRequestRecord>;
  proposedChanges: Map<string, ProposedChangeRecord>;
  documents: Map<string, DocumentRecord>;
  workflowInstanceDocuments: Map<string, WorkflowInstanceDocumentRecord>;
  approvalTasks: Map<string, ApprovalTaskRecord>;
  transactionPlans: Map<string, TransactionPlanRecord>;
  employeeProjections: Map<string, EmployeeProjectionRecord>;
  integrationOutbox: Map<string, IntegrationOutboxRecord>;
  nextLedgerSequence: number;
};

export function createEmptyStore(): HcmNextStore {
  return {
    tenants: new Map(),
    environments: new Map(),
    actors: new Map(),
    workflowDefinitions: new Map(),
    workflowVersions: new Map(),
    workflowInstances: new Map(),
    transitionAttempts: new Map(),
    ledgerEvents: [],
    changeRequests: new Map(),
    proposedChanges: new Map(),
    documents: new Map(),
    workflowInstanceDocuments: new Map(),
    approvalTasks: new Map(),
    transactionPlans: new Map(),
    employeeProjections: new Map(),
    integrationOutbox: new Map(),
    nextLedgerSequence: 1,
  };
}

export type SeededDemoIds = {
  tenantId: string;
  environmentId: string;
  employeeActorId: string;
  hrActorId: string;
  systemActorId: string;
  employeeId: string;
  personId: string;
  workflowDefinitionId: string;
  workflowVersionId: string;
  emergencyContactWorkflowDefinitionId: string;
  emergencyContactWorkflowVersionId: string;
  contactInfoWorkflowDefinitionId: string;
  contactInfoWorkflowVersionId: string;
};

export const DEMO_IDS: SeededDemoIds = {
  tenantId: "tenant_demo",
  environmentId: "env_demo",
  employeeActorId: "actor_employee_jane",
  hrActorId: "actor_hr_admin",
  systemActorId: "actor_system",
  employeeId: "emp_123",
  personId: "person_123",
  workflowDefinitionId: "workflow_legal_name_change",
  workflowVersionId: "workflow_legal_name_change_v1",
  emergencyContactWorkflowDefinitionId: "workflow_emergency_contact_update",
  emergencyContactWorkflowVersionId: "workflow_emergency_contact_update_v1",
  contactInfoWorkflowDefinitionId: "workflow_contact_info_update",
  contactInfoWorkflowVersionId: "workflow_contact_info_update_v1",
};

export function createSeededDemoStore(): HcmNextStore {
  const store = createEmptyStore();
  const timestamp = new Date().toISOString();

  store.tenants.set(DEMO_IDS.tenantId, {
    tenantId: DEMO_IDS.tenantId,
    name: "Demo Tenant",
    slug: "demo",
    status: "active",
  });

  store.environments.set(DEMO_IDS.environmentId, {
    environmentId: DEMO_IDS.environmentId,
    tenantId: DEMO_IDS.tenantId,
    name: "Demo",
    type: "development",
    status: "active",
  });

  store.actors.set(DEMO_IDS.employeeActorId, {
    actorId: DEMO_IDS.employeeActorId,
    tenantId: DEMO_IDS.tenantId,
    actorType: ACTOR_TYPES.HUMAN,
    linkedWorkerId: DEMO_IDS.employeeId,
    email: "jane.doe@example.com",
    displayName: "Jane Doe",
    status: "active",
    roles: [ACTOR_ROLES.EMPLOYEE],
  });

  store.actors.set(DEMO_IDS.hrActorId, {
    actorId: DEMO_IDS.hrActorId,
    tenantId: DEMO_IDS.tenantId,
    actorType: ACTOR_TYPES.HUMAN,
    email: "hr@example.com",
    displayName: "HR Admin",
    status: "active",
    roles: [ACTOR_ROLES.HR_ADMIN],
  });

  store.actors.set(DEMO_IDS.systemActorId, {
    actorId: DEMO_IDS.systemActorId,
    tenantId: DEMO_IDS.tenantId,
    actorType: ACTOR_TYPES.SYSTEM,
    displayName: "System",
    status: "active",
    roles: [ACTOR_ROLES.SYSTEM],
  });

  store.workflowDefinitions.set(DEMO_IDS.workflowDefinitionId, {
    workflowDefinitionId: DEMO_IDS.workflowDefinitionId,
    tenantId: DEMO_IDS.tenantId,
    name: "Employee Legal Name Change",
    workflowType: "employee_data_change",
    status: "active",
    currentVersionId: DEMO_IDS.workflowVersionId,
  });

  store.workflowVersions.set(DEMO_IDS.workflowVersionId, {
    workflowVersionId: DEMO_IDS.workflowVersionId,
    tenantId: DEMO_IDS.tenantId,
    workflowDefinitionId: DEMO_IDS.workflowDefinitionId,
    versionNumber: 1,
    graphDefinition: {
      intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      initialState: WORKFLOW_STATES.COLLECTING_INPUT,
    },
    inputSchema: {},
    outputSchema: {},
    validationRules: [],
    approvalRules: [],
    aiReviewScope: {},
    status: "published",
  });

  store.workflowDefinitions.set(DEMO_IDS.emergencyContactWorkflowDefinitionId, {
    workflowDefinitionId: DEMO_IDS.emergencyContactWorkflowDefinitionId,
    tenantId: DEMO_IDS.tenantId,
    name: "Employee Emergency Contact Update",
    workflowType: "employee_data_change",
    status: "active",
    currentVersionId: DEMO_IDS.emergencyContactWorkflowVersionId,
  });

  store.workflowVersions.set(DEMO_IDS.emergencyContactWorkflowVersionId, {
    workflowVersionId: DEMO_IDS.emergencyContactWorkflowVersionId,
    tenantId: DEMO_IDS.tenantId,
    workflowDefinitionId: DEMO_IDS.emergencyContactWorkflowDefinitionId,
    versionNumber: 1,
    graphDefinition: {
      intent: WORKFLOW_INTENTS.EMPLOYEE_EMERGENCY_CONTACT_UPDATE,
      initialState: WORKFLOW_STATES.COLLECTING_INPUT,
    },
    inputSchema: {},
    outputSchema: {},
    validationRules: [],
    approvalRules: [],
    aiReviewScope: {},
    status: "published",
  });

  store.workflowDefinitions.set(DEMO_IDS.contactInfoWorkflowDefinitionId, {
    workflowDefinitionId: DEMO_IDS.contactInfoWorkflowDefinitionId,
    tenantId: DEMO_IDS.tenantId,
    name: "Employee Contact Information Update",
    workflowType: "employee_data_change",
    status: "active",
    currentVersionId: DEMO_IDS.contactInfoWorkflowVersionId,
  });

  store.workflowVersions.set(DEMO_IDS.contactInfoWorkflowVersionId, {
    workflowVersionId: DEMO_IDS.contactInfoWorkflowVersionId,
    tenantId: DEMO_IDS.tenantId,
    workflowDefinitionId: DEMO_IDS.contactInfoWorkflowDefinitionId,
    versionNumber: 1,
    graphDefinition: {
      intent: "employee.contact_info.update",
      initialState: WORKFLOW_STATES.COLLECTING_INPUT,
    },
    inputSchema: {},
    outputSchema: {},
    validationRules: [],
    approvalRules: [],
    aiReviewScope: {},
    status: "published",
  });

  store.employeeProjections.set(
    employeeProjectionKey(DEMO_IDS.tenantId, DEMO_IDS.employeeId),
    {
      tenantId: DEMO_IDS.tenantId,
      employeeId: DEMO_IDS.employeeId,
      projectionVersion: 1,
      document: {
        employeeId: DEMO_IDS.employeeId,
        person: {
          personId: DEMO_IDS.personId,
          legalName: {
            first: "Jane",
            middle: null,
            last: "Doe",
          },
          displayName: "Jane Doe",
          preferredName: null,
          workEmail: "jane.doe@example.com",
        },
        contact: {
          personalEmail: "jane.personal@example.com",
          mobilePhone: "+15550001111",
          homeAddress: {
            line1: "100 Market St",
            line2: null,
            city: "San Francisco",
            region: "CA",
            postalCode: "94105",
            country: "US",
          },
        },
        employment: {
          status: "active",
          legalEntity: "US-001",
        },
        manager: {
          employeeId: "emp_456",
        },
        emergencyContacts: [
          {
            contactId: "ec_001",
            name: "Alex Doe",
            relationship: "spouse",
            phone: "+15551234567",
            email: "alex.doe@example.com",
            priority: 1,
          },
        ],
        custom: {},
      },
      indexedFields: {},
      createdAt: timestamp,
      updatedAt: timestamp,
    },
  );

  return store;
}

export function makeId(prefix: string): string {
  return `${prefix}_${randomUUID()}`;
}

export function nowIso(): string {
  return new Date().toISOString();
}

export function employeeProjectionKey(tenantId: string, employeeId: string): string {
  return `${tenantId}:${employeeId}`;
}

export function transitionAttemptKey(
  tenantId: string,
  workflowInstanceId: string,
  idempotencyKey: string,
): string {
  return `${tenantId}:${workflowInstanceId}:${idempotencyKey}`;
}

export function createInitialWorkflowInstance(
  input: Omit<
    WorkflowInstanceRecord,
    | "workflowInstanceId"
    | "status"
    | "state"
    | "version"
    | "startedAt"
    | "createdAt"
    | "updatedAt"
  >,
): WorkflowInstanceRecord {
  const timestamp = nowIso();

  return {
    ...input,
    workflowInstanceId: makeId("wfi"),
    status: WORKFLOW_STATUSES.ACTIVE,
    state: WORKFLOW_STATES.COLLECTING_INPUT,
    startedAt: timestamp,
    version: 1,
    createdAt: timestamp,
    updatedAt: timestamp,
  };
}

export function createDemoDocumentRecord(
  input: Omit<DocumentRecord, "documentId" | "status" | "createdAt" | "updatedAt">,
): DocumentRecord {
  const timestamp = nowIso();

  return {
    ...input,
    documentId: makeId("doc"),
    classification:
      input.classification || DOCUMENT_CLASSIFICATIONS.SENSITIVE_PERSON_IDENTITY,
    status: DOCUMENT_STATUSES.UPLOADED,
    createdAt: timestamp,
    updatedAt: timestamp,
  };
}
