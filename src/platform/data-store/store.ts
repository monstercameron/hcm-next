import { randomUUID } from "node:crypto";
import {
  ACTOR_ROLES,
  ACTOR_TYPES,
  DOCUMENT_CLASSIFICATIONS,
  DOCUMENT_STATUSES,
  PERMISSION_KEYS,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
} from "@hcm-next/foundation";
import {
  DEMO_EMPLOYEE_IDS,
  DEMO_EMPLOYEE_SPECS,
  DEMO_FINANCE_APPROVABLE_COST_CENTERS,
  DEMO_ORGANIZATION,
  DEMO_ORG_UNIT_SPECS,
  DEMO_ROLE_BINDING_SPECS,
  DEMO_WORKER_ASSIGNMENT_SPECS,
  createDemoEmployeeDocument,
  employeeHasDirectReports,
  indexedFieldsForEmployeeDocument,
  rolesForDemoEmployee,
  type DemoAccessPersona,
  type DemoEmployeeSpec,
} from "./demo-organization.js";
import type {
  AccessGrantRecord,
  ActorRecord,
  ApprovalGroupRecord,
  ApprovalTaskRecord,
  ChangeRequestRecord,
  DocumentRecord,
  EmployeeProjectionRecord,
  EnvironmentRecord,
  EmployeeFieldGroup,
  IntegrationOutboxRecord,
  LedgerEventRecord,
  OrganizationRelationshipRecord,
  OrganizationUnitRecord,
  ProposedChangeRecord,
  RoleBindingRecord,
  TenantRecord,
  TransactionPlanRecord,
  WorkerAssignmentRecord,
  WorkflowDefinitionRecord,
  WorkflowAdminDraftRecord,
  WorkflowAdminFamilyRecord,
  WorkflowAdminVersionRecord,
  WorkflowIntegrationBindingRecord,
  WorkflowInstanceDocumentRecord,
  WorkflowInstanceRecord,
  WorkflowPublishHistoryRecord,
  WorkflowTemplateRecord,
  WorkflowTransitionAttemptRecord,
  WorkflowVersionRecord,
} from "./types.js";

export type HcmNextStore = {
  tenants: Map<string, TenantRecord>;
  environments: Map<string, EnvironmentRecord>;
  actors: Map<string, ActorRecord>;
  accessGrants: Map<string, AccessGrantRecord>;
  organizationUnits: Map<string, OrganizationUnitRecord>;
  organizationRelationships: Map<string, OrganizationRelationshipRecord>;
  workerAssignments: Map<string, WorkerAssignmentRecord>;
  roleBindings: Map<string, RoleBindingRecord>;
  workflowDefinitions: Map<string, WorkflowDefinitionRecord>;
  workflowVersions: Map<string, WorkflowVersionRecord>;
  workflowAdminFamilies: Map<string, WorkflowAdminFamilyRecord>;
  workflowAdminDrafts: Map<string, WorkflowAdminDraftRecord>;
  workflowAdminVersions: Map<string, WorkflowAdminVersionRecord>;
  workflowPublishHistory: WorkflowPublishHistoryRecord[];
  workflowTemplates: Map<string, WorkflowTemplateRecord>;
  workflowIntegrationBindings: Map<string, WorkflowIntegrationBindingRecord>;
  workflowInstances: Map<string, WorkflowInstanceRecord>;
  transitionAttempts: Map<string, WorkflowTransitionAttemptRecord>;
  ledgerEvents: LedgerEventRecord[];
  changeRequests: Map<string, ChangeRequestRecord>;
  proposedChanges: Map<string, ProposedChangeRecord>;
  documents: Map<string, DocumentRecord>;
  workflowInstanceDocuments: Map<string, WorkflowInstanceDocumentRecord>;
  approvalGroups: Map<string, ApprovalGroupRecord>;
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
    accessGrants: new Map(),
    organizationUnits: new Map(),
    organizationRelationships: new Map(),
    workerAssignments: new Map(),
    roleBindings: new Map(),
    workflowDefinitions: new Map(),
    workflowVersions: new Map(),
    workflowAdminFamilies: new Map(),
    workflowAdminDrafts: new Map(),
    workflowAdminVersions: new Map(),
    workflowPublishHistory: [],
    workflowTemplates: new Map(),
    workflowIntegrationBindings: new Map(),
    workflowInstances: new Map(),
    transitionAttempts: new Map(),
    ledgerEvents: [],
    changeRequests: new Map(),
    proposedChanges: new Map(),
    documents: new Map(),
    workflowInstanceDocuments: new Map(),
    approvalGroups: new Map(),
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
  secondAdminActorId: string;
  managerActorId: string;
  financeAdminActorId: string;
  compensationAdminActorId: string;
  destinationManagerActorId: string;
  medicalDirectorActorId: string;
  clinicOpsActorId: string;
  systemActorId: string;
  employeeId: string;
  secondEmployeeId: string;
  managerEmployeeId: string;
  destinationManagerEmployeeId: string;
  medicalDirectorEmployeeId: string;
  clinicOpsEmployeeId: string;
  personId: string;
  workflowDefinitionId: string;
  workflowVersionId: string;
  emergencyContactWorkflowDefinitionId: string;
  emergencyContactWorkflowVersionId: string;
  contactInfoWorkflowDefinitionId: string;
  contactInfoWorkflowVersionId: string;
  compensationWorkflowDefinitionId: string;
  compensationWorkflowVersionId: string;
  orgTransferWorkflowDefinitionId: string;
  orgTransferWorkflowVersionId: string;
  headcountWorkflowDefinitionId: string;
  headcountWorkflowVersionId: string;
};

export const DEMO_IDS: SeededDemoIds = {
  tenantId: "tenant_demo",
  environmentId: "env_demo",
  employeeActorId: "actor_employee_jane",
  hrActorId: "actor_hr_admin",
  secondAdminActorId: "actor_hr_admin_riley",
  managerActorId: "actor_manager_morgan",
  financeAdminActorId: "actor_finance_admin",
  compensationAdminActorId: "actor_comp_admin",
  destinationManagerActorId: "actor_emp_461",
  medicalDirectorActorId: "actor_emp_920",
  clinicOpsActorId: "actor_emp_930",
  systemActorId: "actor_system",
  employeeId: DEMO_EMPLOYEE_IDS.SELF_SERVICE_EMPLOYEE,
  secondEmployeeId: DEMO_EMPLOYEE_IDS.SECOND_HR_ADMIN,
  managerEmployeeId: DEMO_EMPLOYEE_IDS.MANAGER,
  destinationManagerEmployeeId: DEMO_EMPLOYEE_IDS.CAMBRIDGE_MANAGER,
  medicalDirectorEmployeeId: DEMO_EMPLOYEE_IDS.MEDICAL_DIRECTOR,
  clinicOpsEmployeeId: DEMO_EMPLOYEE_IDS.CLINIC_OPS_DIRECTOR,
  personId: "person_123",
  workflowDefinitionId: "workflow_legal_name_change",
  workflowVersionId: "workflow_legal_name_change_v1",
  emergencyContactWorkflowDefinitionId: "workflow_emergency_contact_update",
  emergencyContactWorkflowVersionId: "workflow_emergency_contact_update_v1",
  contactInfoWorkflowDefinitionId: "workflow_contact_info_update",
  contactInfoWorkflowVersionId: "workflow_contact_info_update_v1",
  compensationWorkflowDefinitionId: "workflow_compensation_change",
  compensationWorkflowVersionId: "workflow_compensation_change_v1",
  orgTransferWorkflowDefinitionId: "workflow_org_transfer_compensation_change",
  orgTransferWorkflowVersionId: "workflow_org_transfer_compensation_change_v1",
  headcountWorkflowDefinitionId: "workflow_position_headcount_requisition",
  headcountWorkflowVersionId: "workflow_position_headcount_requisition_v1",
};

type RuntimeDemoEmployeeSeed = {
  actorId: string;
  actorRoles: string[];
  document: ReturnType<typeof createDemoEmployeeDocument>;
  spec: DemoEmployeeSpec;
};

const DEMO_EMPLOYEES: RuntimeDemoEmployeeSeed[] = DEMO_EMPLOYEE_SPECS.map(
  (spec, index) => {
    return {
      actorId: runtimeActorIdForEmployee(spec.employeeId),
      actorRoles: rolesForDemoEmployee(spec),
      document: createDemoEmployeeDocument(spec, index),
      spec,
    };
  },
);

const profilePermissions = [
  PERMISSION_KEYS.EMPLOYEE_VIEW_PROFILE,
  PERMISSION_KEYS.EMPLOYEE_VIEW_ORGANIZATION,
  PERMISSION_KEYS.EMPLOYEE_VIEW_JOB,
  PERMISSION_KEYS.EMPLOYEE_VIEW_EMPLOYMENT,
] as const;

const contactPermissions = [
  PERMISSION_KEYS.EMPLOYEE_VIEW_CONTACT,
  PERMISSION_KEYS.EMPLOYEE_VIEW_EMERGENCY_CONTACTS,
] as const;

const compensationPermissions = [PERMISSION_KEYS.EMPLOYEE_VIEW_COMPENSATION] as const;

const workflowPermissions = [PERMISSION_KEYS.EMPLOYEE_VIEW_WORKFLOW] as const;

const profileFieldGroups = [
  "profile",
  "organization",
  "job",
  "employment",
] as const satisfies readonly EmployeeFieldGroup[];

const contactFieldGroups = [
  "contact",
  "emergency_contacts",
] as const satisfies readonly EmployeeFieldGroup[];

const compensationFieldGroups = [
  "compensation",
] as const satisfies readonly EmployeeFieldGroup[];

const workflowFieldGroups = [
  "workflow",
] as const satisfies readonly EmployeeFieldGroup[];

function runtimeActorIdForEmployee(employeeId: string): string {
  if (employeeId === DEMO_IDS.employeeId) {
    return DEMO_IDS.employeeActorId;
  }

  if (employeeId === DEMO_EMPLOYEE_IDS.PEOPLE_DIRECTOR) {
    return DEMO_IDS.hrActorId;
  }

  if (employeeId === DEMO_IDS.secondEmployeeId) {
    return DEMO_IDS.secondAdminActorId;
  }

  if (employeeId === DEMO_IDS.managerEmployeeId) {
    return DEMO_IDS.managerActorId;
  }

  if (employeeId === DEMO_IDS.destinationManagerEmployeeId) {
    return DEMO_IDS.destinationManagerActorId;
  }

  if (employeeId === DEMO_IDS.medicalDirectorEmployeeId) {
    return DEMO_IDS.medicalDirectorActorId;
  }

  if (employeeId === DEMO_IDS.clinicOpsEmployeeId) {
    return DEMO_IDS.clinicOpsActorId;
  }

  if (employeeId === DEMO_EMPLOYEE_IDS.FINANCE_ADMIN) {
    return DEMO_IDS.financeAdminActorId;
  }

  if (employeeId === DEMO_EMPLOYEE_IDS.COMPENSATION_ADMIN) {
    return DEMO_IDS.compensationAdminActorId;
  }

  return `actor_${employeeId}`;
}

function createDemoAccessGrants(timestamp: string): AccessGrantRecord[] {
  const accessGrants: AccessGrantRecord[] = [];

  for (const employee of DEMO_EMPLOYEES) {
    accessGrants.push(
      createAccessGrant({
        accessGrantId: `grant_self_${employee.document.employeeId}`,
        actorId: employee.actorId,
        permissions: [
          PERMISSION_KEYS.EMPLOYEE_VIEW_PROFILE,
          PERMISSION_KEYS.EMPLOYEE_VIEW_CONTACT,
          PERMISSION_KEYS.EMPLOYEE_VIEW_EMPLOYMENT,
          PERMISSION_KEYS.EMPLOYEE_VIEW_EMERGENCY_CONTACTS,
        ],
        scope: { type: "self" },
        fieldGroups: ["profile", "employment", ...contactFieldGroups],
        timestamp,
        metadata: { demo: true, accessModel: "employee_self_service" },
      }),
    );

    if (employeeHasDirectReports(employee.document.employeeId)) {
      accessGrants.push(
        createAccessGrant({
          accessGrantId: `grant_manager_reports_${employee.document.employeeId}`,
          actorId: employee.actorId,
          permissions: [...profilePermissions],
          scope: { type: "direct_reports" },
          fieldGroups: [...profileFieldGroups],
          timestamp,
          metadata: { demo: true, accessModel: "manager_direct_reports" },
        }),
      );
    }
  }

  accessGrants.push(
    ...personaAccessGrants("primary_hr_admin", timestamp, [
      {
        suffix: "global_people_ops",
        permissions: [
          ...profilePermissions,
          ...contactPermissions,
          ...workflowPermissions,
        ],
        scope: { type: "global" },
        fieldGroups: [
          ...profileFieldGroups,
          ...contactFieldGroups,
          ...workflowFieldGroups,
        ],
      },
    ]),
    ...personaAccessGrants("secondary_hr_admin", timestamp, [
      {
        suffix: "clinical_people_ops",
        permissions: [
          ...profilePermissions,
          ...contactPermissions,
          ...workflowPermissions,
        ],
        scope: {
          type: "business_unit",
          values: ["Clinical Operations", "Corporate"],
        },
        fieldGroups: [
          ...profileFieldGroups,
          ...contactFieldGroups,
          ...workflowFieldGroups,
        ],
      },
    ]),
    ...personaAccessGrants("finance_admin", timestamp, [
      {
        suffix: "finance_revenue_cost_centers",
        permissions: [...profilePermissions, ...compensationPermissions],
        scope: {
          type: "cost_center",
          values: [...DEMO_FINANCE_APPROVABLE_COST_CENTERS],
        },
        fieldGroups: [...profileFieldGroups, ...compensationFieldGroups],
      },
    ]),
    ...personaAccessGrants("compensation_admin", timestamp, [
      {
        suffix: "global_compensation",
        permissions: [...profilePermissions, ...compensationPermissions],
        scope: { type: "global" },
        fieldGroups: [...profileFieldGroups, ...compensationFieldGroups],
      },
    ]),
    ...personaAccessGrants("clinic_ops_admin", timestamp, [
      {
        suffix: "clinical_operations",
        permissions: [...profilePermissions],
        scope: { type: "business_unit", values: ["Clinical Operations"] },
        fieldGroups: [...profileFieldGroups],
      },
    ]),
    ...personaAccessGrants("medical_director", timestamp, [
      {
        suffix: "clinical_staff",
        permissions: [...profilePermissions],
        scope: {
          type: "department",
          values: ["Clinical Care", "Behavioral Health", "Care Coordination"],
        },
        fieldGroups: [...profileFieldGroups],
      },
    ]),
    ...personaAccessGrants("compliance_admin", timestamp, [
      {
        suffix: "compliance_global",
        permissions: [...profilePermissions, ...workflowPermissions],
        scope: { type: "global" },
        fieldGroups: [...profileFieldGroups, ...workflowFieldGroups],
      },
    ]),
    ...personaAccessGrants("it_admin", timestamp, [
      {
        suffix: "identity_global",
        permissions: [
          PERMISSION_KEYS.EMPLOYEE_VIEW_PROFILE,
          PERMISSION_KEYS.EMPLOYEE_VIEW_ORGANIZATION,
          PERMISSION_KEYS.EMPLOYEE_VIEW_JOB,
        ],
        scope: { type: "global" },
        fieldGroups: ["profile", "organization", "job"],
      },
    ]),
    ...personaAccessGrants("executive", timestamp, [
      {
        suffix: "executive_global",
        permissions: [...profilePermissions, ...compensationPermissions],
        scope: { type: "global" },
        fieldGroups: [...profileFieldGroups, ...compensationFieldGroups],
      },
    ]),
  );

  return accessGrants;
}

type PersonaGrantInput = {
  suffix: string;
  permissions: readonly string[];
  scope: AccessGrantRecord["scope"];
  fieldGroups: readonly EmployeeFieldGroup[];
};

function personaAccessGrants(
  persona: DemoAccessPersona,
  timestamp: string,
  grants: readonly PersonaGrantInput[],
): AccessGrantRecord[] {
  const employees = DEMO_EMPLOYEES.filter((employee) => {
    return employee.spec.accessPersonas?.includes(persona) === true;
  });

  return employees.flatMap((employee) => {
    return grants.map((grant) => {
      return createAccessGrant({
        accessGrantId: `grant_${employee.document.employeeId}_${grant.suffix}`,
        actorId: employee.actorId,
        permissions: [...grant.permissions],
        scope: grant.scope,
        fieldGroups: [...grant.fieldGroups],
        timestamp,
        metadata: { demo: true, accessModel: persona },
      });
    });
  });
}

function createAccessGrant(input: {
  accessGrantId: string;
  actorId: string;
  permissions: string[];
  scope: AccessGrantRecord["scope"];
  fieldGroups: EmployeeFieldGroup[];
  timestamp: string;
  metadata: Record<string, unknown>;
}): AccessGrantRecord {
  return {
    accessGrantId: input.accessGrantId,
    tenantId: DEMO_IDS.tenantId,
    actorId: input.actorId,
    permissions: input.permissions,
    scope: input.scope,
    fieldGroups: input.fieldGroups,
    status: "active",
    startsAt: "2026-01-01T00:00:00.000Z",
    metadata: input.metadata,
    createdAt: input.timestamp,
    updatedAt: input.timestamp,
  };
}

function createDemoOrganizationUnitRecords(
  timestamp: string,
): OrganizationUnitRecord[] {
  return DEMO_ORG_UNIT_SPECS.map((spec) => {
    return {
      orgUnitId: orgUnitRecordIdForKey(spec.unitKey),
      tenantId: DEMO_IDS.tenantId,
      unitKey: spec.unitKey,
      type: spec.type,
      name: spec.name,
      status: spec.status,
      country: spec.country,
      jurisdiction: spec.jurisdiction,
      effectiveStart: "2026-01-01T00:00:00.000Z",
      metadata: spec.metadata,
      createdAt: timestamp,
      updatedAt: timestamp,
    };
  });
}

function createDemoOrganizationRelationshipRecords(
  timestamp: string,
): OrganizationRelationshipRecord[] {
  return DEMO_ORG_UNIT_SPECS.flatMap((spec) => {
    if (spec.parentUnitKey === undefined || spec.relationshipToParent === undefined) {
      return [];
    }

    return [
      {
        organizationRelationshipId: `orgrel_${spec.unitKey}_${spec.relationshipToParent}`,
        tenantId: DEMO_IDS.tenantId,
        fromOrgUnitId: orgUnitRecordIdForKey(spec.unitKey),
        toOrgUnitId: orgUnitRecordIdForKey(spec.parentUnitKey),
        relationshipType: spec.relationshipToParent,
        status: "active",
        effectiveStart: "2026-01-01T00:00:00.000Z",
        metadata: {
          demo: true,
          source: "harborcare_seed",
        },
        createdAt: timestamp,
        updatedAt: timestamp,
      },
    ];
  });
}

function createDemoWorkerAssignmentRecords(
  timestamp: string,
): WorkerAssignmentRecord[] {
  return DEMO_WORKER_ASSIGNMENT_SPECS.map((spec) => {
    return {
      workerAssignmentId: `wa_${spec.assignmentKey}`,
      tenantId: DEMO_IDS.tenantId,
      employeeId: spec.employeeId,
      orgUnitId: orgUnitRecordIdForKey(spec.orgUnitKey),
      assignmentType: spec.assignmentType,
      roleType: spec.roleType,
      managerEmployeeId: spec.managerEmployeeId,
      allocationPercent: spec.allocationPercent,
      status: spec.status,
      effectiveStart: spec.effectiveStart,
      effectiveEnd: spec.effectiveEnd,
      metadata: spec.metadata,
      createdAt: timestamp,
      updatedAt: timestamp,
    };
  });
}

function createDemoRoleBindingRecords(timestamp: string): RoleBindingRecord[] {
  return DEMO_ROLE_BINDING_SPECS.map((spec) => {
    return {
      roleBindingId: `rb_${spec.bindingKey}`,
      tenantId: DEMO_IDS.tenantId,
      actorId: runtimeActorIdForEmployee(spec.employeeId),
      roleKey: spec.roleKey,
      scopeType: spec.scopeType,
      scopeOrgUnitId:
        spec.scopeOrgUnitKey === undefined
          ? undefined
          : orgUnitRecordIdForKey(spec.scopeOrgUnitKey),
      scopeValue: spec.scopeValue,
      relationshipType: spec.relationshipType,
      status: spec.status,
      effectiveStart: spec.effectiveStart,
      effectiveEnd: spec.effectiveEnd,
      metadata: spec.metadata,
      createdAt: timestamp,
      updatedAt: timestamp,
    };
  });
}

function orgUnitRecordIdForKey(unitKey: string): string {
  return `orgunit_${unitKey}`;
}

export function createSeededDemoStore(): HcmNextStore {
  const store = createEmptyStore();
  const timestamp = new Date().toISOString();

  store.tenants.set(DEMO_IDS.tenantId, {
    tenantId: DEMO_IDS.tenantId,
    name: DEMO_ORGANIZATION.name,
    slug: "tenant_demo",
    status: "active",
  });

  store.environments.set(DEMO_IDS.environmentId, {
    environmentId: DEMO_IDS.environmentId,
    tenantId: DEMO_IDS.tenantId,
    name: "HarborCare Development",
    type: "development",
    status: "active",
  });

  for (const employee of DEMO_EMPLOYEES) {
    store.actors.set(employee.actorId, {
      actorId: employee.actorId,
      tenantId: DEMO_IDS.tenantId,
      actorType: ACTOR_TYPES.HUMAN,
      linkedWorkerId: employee.document.employeeId,
      email: employee.document.person.workEmail,
      displayName: employee.document.person.displayName,
      status: "active",
      roles: employee.actorRoles,
    });
  }

  store.actors.set(DEMO_IDS.systemActorId, {
    actorId: DEMO_IDS.systemActorId,
    tenantId: DEMO_IDS.tenantId,
    actorType: ACTOR_TYPES.SYSTEM,
    displayName: "System",
    status: "active",
    roles: [ACTOR_ROLES.SYSTEM],
  });

  for (const organizationUnit of createDemoOrganizationUnitRecords(timestamp)) {
    store.organizationUnits.set(organizationUnit.orgUnitId, organizationUnit);
  }

  for (const organizationRelationship of createDemoOrganizationRelationshipRecords(
    timestamp,
  )) {
    store.organizationRelationships.set(
      organizationRelationship.organizationRelationshipId,
      organizationRelationship,
    );
  }

  for (const workerAssignment of createDemoWorkerAssignmentRecords(timestamp)) {
    store.workerAssignments.set(workerAssignment.workerAssignmentId, workerAssignment);
  }

  for (const roleBinding of createDemoRoleBindingRecords(timestamp)) {
    store.roleBindings.set(roleBinding.roleBindingId, roleBinding);
  }

  const demoAccessGrants = createDemoAccessGrants(timestamp);

  for (const accessGrant of demoAccessGrants) {
    store.accessGrants.set(accessGrant.accessGrantId, accessGrant);
  }

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
      intent: WORKFLOW_INTENTS.EMPLOYEE_CONTACT_INFO_UPDATE,
      initialState: WORKFLOW_STATES.COLLECTING_INPUT,
    },
    inputSchema: {},
    outputSchema: {},
    validationRules: [],
    approvalRules: [],
    aiReviewScope: {},
    status: "published",
  });

  store.workflowDefinitions.set(DEMO_IDS.compensationWorkflowDefinitionId, {
    workflowDefinitionId: DEMO_IDS.compensationWorkflowDefinitionId,
    tenantId: DEMO_IDS.tenantId,
    name: "Employee Compensation Change",
    workflowType: "employee_data_change",
    status: "active",
    currentVersionId: DEMO_IDS.compensationWorkflowVersionId,
  });

  store.workflowVersions.set(DEMO_IDS.compensationWorkflowVersionId, {
    workflowVersionId: DEMO_IDS.compensationWorkflowVersionId,
    tenantId: DEMO_IDS.tenantId,
    workflowDefinitionId: DEMO_IDS.compensationWorkflowDefinitionId,
    versionNumber: 1,
    graphDefinition: {
      intent: WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
      initialState: WORKFLOW_STATES.COLLECTING_INPUT,
    },
    inputSchema: {},
    outputSchema: {},
    validationRules: [],
    approvalRules: [],
    aiReviewScope: {},
    status: "published",
  });

  store.workflowDefinitions.set(DEMO_IDS.orgTransferWorkflowDefinitionId, {
    workflowDefinitionId: DEMO_IDS.orgTransferWorkflowDefinitionId,
    tenantId: DEMO_IDS.tenantId,
    name: "Employee Org Transfer Compensation Change",
    workflowType: "employee_data_change",
    status: "active",
    currentVersionId: DEMO_IDS.orgTransferWorkflowVersionId,
  });

  store.workflowVersions.set(DEMO_IDS.orgTransferWorkflowVersionId, {
    workflowVersionId: DEMO_IDS.orgTransferWorkflowVersionId,
    tenantId: DEMO_IDS.tenantId,
    workflowDefinitionId: DEMO_IDS.orgTransferWorkflowDefinitionId,
    versionNumber: 1,
    graphDefinition: {
      intent: WORKFLOW_INTENTS.EMPLOYEE_ORG_TRANSFER_COMPENSATION_CHANGE,
      initialState: WORKFLOW_STATES.COLLECTING_INPUT,
    },
    inputSchema: {},
    outputSchema: {},
    validationRules: [],
    approvalRules: [],
    aiReviewScope: {},
    status: "published",
  });

  store.workflowDefinitions.set(DEMO_IDS.headcountWorkflowDefinitionId, {
    workflowDefinitionId: DEMO_IDS.headcountWorkflowDefinitionId,
    tenantId: DEMO_IDS.tenantId,
    name: "Position Headcount Requisition Approval",
    workflowType: "position_workforce_planning",
    status: "active",
    currentVersionId: DEMO_IDS.headcountWorkflowVersionId,
  });

  store.workflowVersions.set(DEMO_IDS.headcountWorkflowVersionId, {
    workflowVersionId: DEMO_IDS.headcountWorkflowVersionId,
    tenantId: DEMO_IDS.tenantId,
    workflowDefinitionId: DEMO_IDS.headcountWorkflowDefinitionId,
    versionNumber: 1,
    graphDefinition: {
      intent: WORKFLOW_INTENTS.POSITION_HEADCOUNT_REQUISITION_APPROVAL,
      initialState: WORKFLOW_STATES.COLLECTING_INPUT,
    },
    inputSchema: {},
    outputSchema: {},
    validationRules: [],
    approvalRules: [],
    aiReviewScope: {},
    status: "published",
  });

  for (const employee of DEMO_EMPLOYEES) {
    store.employeeProjections.set(
      employeeProjectionKey(DEMO_IDS.tenantId, employee.document.employeeId),
      {
        tenantId: DEMO_IDS.tenantId,
        employeeId: employee.document.employeeId,
        projectionVersion: 1,
        document: employee.document,
        indexedFields: indexedFieldsForEmployeeDocument(employee.document),
        createdAt: timestamp,
        updatedAt: timestamp,
      },
    );
  }

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
