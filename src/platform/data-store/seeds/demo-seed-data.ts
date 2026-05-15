import {
  ACTOR_TYPES,
  APPROVAL_TASK_STATUSES,
  CHANGE_REQUEST_TYPES,
  DEMO_SEED_ALIASES,
  DOCUMENT_CLASSIFICATIONS,
  LEDGER_EVENT_TYPES,
  PERMISSION_KEYS,
  ROLE_KEYS,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  WORKFLOW_TRANSITIONS,
} from "@hcm-next/foundation";

import {
  DEMO_EMPLOYEE_IDS,
  DEMO_EMPLOYEE_SPECS,
  DEMO_ORGANIZATION,
  DEMO_ORG_UNIT_SPECS,
  DEMO_ROLE_BINDING_SPECS,
  DEMO_WORKER_ASSIGNMENT_SPECS,
  createDemoEmployeeDocument,
  indexedFieldsForEmployeeDocument,
  rolesForDemoEmployee,
  type DemoAccessPersona,
  type DemoEmployeeSpec,
} from "../demo-organization";

function seedUuid(sequence: number): string {
  return `00000000-0000-4000-8000-${String(sequence).padStart(12, "0")}`;
}

export const DEMO_SEED_IDS = {
  TENANT_ID: seedUuid(1),
  ENVIRONMENT_ID: seedUuid(2),
  EMPLOYEE_ACTOR_ID: seedUuid(101),
  HR_ACTOR_ID: seedUuid(102),
  SYSTEM_ACTOR_ID: seedUuid(103),
  SECOND_ADMIN_ACTOR_ID: seedUuid(104),
  MANAGER_ACTOR_ID: seedUuid(105),
  FINANCE_ADMIN_ACTOR_ID: seedUuid(106),
  COMPENSATION_ADMIN_ACTOR_ID: seedUuid(107),
  WORKFLOW_DEFINITION_ID: seedUuid(201),
  WORKFLOW_VERSION_ID: seedUuid(202),
  EMPLOYEE_PERMISSION_POLICY_ID: seedUuid(301),
  HR_PERMISSION_POLICY_ID: seedUuid(302),
  SYSTEM_PERMISSION_POLICY_ID: seedUuid(303),
  PRIMARY_HR_PROFILE_POLICY_ID: seedUuid(304),
  SECONDARY_HR_PROFILE_POLICY_ID: seedUuid(305),
  MANAGER_REPORTS_POLICY_ID: seedUuid(306),
  CLINIC_OPS_POLICY_ID: seedUuid(307),
  MEDICAL_DIRECTOR_POLICY_ID: seedUuid(308),
  FINANCE_COST_CENTER_POLICY_ID: seedUuid(309),
  COMPENSATION_POLICY_ID: seedUuid(310),
  COMPLIANCE_POLICY_ID: seedUuid(311),
  IT_IDENTITY_POLICY_ID: seedUuid(312),
  EXECUTIVE_POLICY_ID: seedUuid(313),
  LEGAL_NAME_FIELD_ID: seedUuid(401),
  EVIDENCE_FIELD_ID: seedUuid(402),
} as const;

export const DEMO_EMPLOYEE_PROJECTIONS = DEMO_EMPLOYEE_SPECS.map((spec, index) =>
  createDemoEmployeeDocument(spec, index),
);

function getPrimaryDemoEmployeeProjection() {
  const projection = DEMO_EMPLOYEE_PROJECTIONS.find((employeeProjection) => {
    return employeeProjection.employeeId === DEMO_SEED_ALIASES.EMPLOYEE;
  });

  if (projection === undefined) {
    throw new Error("Primary demo employee projection is missing");
  }

  return projection;
}

export const DEMO_EMPLOYEE_PROJECTION = getPrimaryDemoEmployeeProjection();

export function demoIndexedFieldsForEmployeeProjection(
  projection: (typeof DEMO_EMPLOYEE_PROJECTIONS)[number],
): Record<string, unknown> {
  return indexedFieldsForEmployeeDocument(projection);
}

const specialActorIdsByEmployeeId = new Map<string, string>([
  [DEMO_SEED_ALIASES.EMPLOYEE, DEMO_SEED_IDS.EMPLOYEE_ACTOR_ID],
  [DEMO_EMPLOYEE_IDS.PEOPLE_DIRECTOR, DEMO_SEED_IDS.HR_ACTOR_ID],
  [DEMO_SEED_ALIASES.MANAGER_EMPLOYEE, DEMO_SEED_IDS.MANAGER_ACTOR_ID],
  [DEMO_EMPLOYEE_IDS.SECOND_HR_ADMIN, DEMO_SEED_IDS.SECOND_ADMIN_ACTOR_ID],
  [DEMO_EMPLOYEE_IDS.FINANCE_ADMIN, DEMO_SEED_IDS.FINANCE_ADMIN_ACTOR_ID],
  [DEMO_EMPLOYEE_IDS.COMPENSATION_ADMIN, DEMO_SEED_IDS.COMPENSATION_ADMIN_ACTOR_ID],
]);

function demoActorIdForEmployee(spec: DemoEmployeeSpec, index: number): string {
  return specialActorIdsByEmployeeId.get(spec.employeeId) ?? seedUuid(1000 + index);
}

function demoActorExternalSubject(spec: DemoEmployeeSpec): string {
  if (spec.employeeId === DEMO_SEED_ALIASES.EMPLOYEE) {
    return DEMO_SEED_ALIASES.EMPLOYEE_ACTOR;
  }

  if (spec.employeeId === DEMO_EMPLOYEE_IDS.PEOPLE_DIRECTOR) {
    return DEMO_SEED_ALIASES.HR_ACTOR;
  }

  if (spec.employeeId === DEMO_SEED_ALIASES.MANAGER_EMPLOYEE) {
    return "demo-manager";
  }

  if (spec.employeeId === DEMO_EMPLOYEE_IDS.SECOND_HR_ADMIN) {
    return "demo-secondary-hr-admin";
  }

  if (spec.employeeId === DEMO_EMPLOYEE_IDS.FINANCE_ADMIN) {
    return "demo-finance-admin";
  }

  if (spec.employeeId === DEMO_EMPLOYEE_IDS.COMPENSATION_ADMIN) {
    return "demo-compensation-admin";
  }

  return `demo-${spec.employeeId}`;
}

export const DEMO_ACTORS = [
  ...DEMO_EMPLOYEE_SPECS.map((spec, index) => {
    const document = createDemoEmployeeDocument(spec, index);

    return {
      actorId: demoActorIdForEmployee(spec, index),
      actorType: ACTOR_TYPES.HUMAN,
      linkedWorkerId: document.employeeId,
      email: document.person.workEmail,
      displayName: document.person.displayName,
      roles: rolesForDemoEmployee(spec),
      externalSubject: demoActorExternalSubject(spec),
      accessPersonas: spec.accessPersonas ?? [],
    };
  }),
  {
    actorId: DEMO_SEED_IDS.SYSTEM_ACTOR_ID,
    actorType: ACTOR_TYPES.SYSTEM,
    linkedWorkerId: null,
    email: null,
    displayName: "HCM Next System",
    roles: [ROLE_KEYS.SYSTEM],
    externalSubject: DEMO_SEED_ALIASES.SYSTEM_ACTOR,
    accessPersonas: [],
  },
] as const;

const orgUnitIdsByKey = new Map<string, string>(
  DEMO_ORG_UNIT_SPECS.map((spec, index) => {
    return [spec.unitKey, seedUuid(2000 + index)];
  }),
);

export const DEMO_ORGANIZATION_UNITS = DEMO_ORG_UNIT_SPECS.map((spec) => {
  return {
    orgUnitId: orgUnitSeedIdForKey(spec.unitKey),
    unitKey: spec.unitKey,
    type: spec.type,
    name: spec.name,
    status: spec.status,
    country: spec.country ?? null,
    jurisdiction: spec.jurisdiction ?? null,
    effectiveStart: "2026-01-01T00:00:00.000Z",
    effectiveEnd: null,
    metadata: spec.metadata,
  };
});

export const DEMO_ORGANIZATION_RELATIONSHIPS = DEMO_ORG_UNIT_SPECS.flatMap(
  (spec, index) => {
    if (spec.parentUnitKey === undefined || spec.relationshipToParent === undefined) {
      return [];
    }

    return [
      {
        organizationRelationshipId: seedUuid(3000 + index),
        fromOrgUnitId: orgUnitSeedIdForKey(spec.unitKey),
        toOrgUnitId: orgUnitSeedIdForKey(spec.parentUnitKey),
        relationshipType: spec.relationshipToParent,
        status: "active",
        effectiveStart: "2026-01-01T00:00:00.000Z",
        effectiveEnd: null,
        metadata: {
          demo: true,
          source: "harborcare_seed",
        },
      },
    ];
  },
);

export const DEMO_WORKER_ASSIGNMENTS = DEMO_WORKER_ASSIGNMENT_SPECS.map(
  (spec, index) => {
    return {
      workerAssignmentId: seedUuid(4000 + index),
      employeeId: spec.employeeId,
      orgUnitId: orgUnitSeedIdForKey(spec.orgUnitKey),
      assignmentType: spec.assignmentType,
      roleType: spec.roleType ?? null,
      managerEmployeeId: spec.managerEmployeeId ?? null,
      allocationPercent: spec.allocationPercent,
      status: spec.status,
      effectiveStart: spec.effectiveStart,
      effectiveEnd: spec.effectiveEnd ?? null,
      metadata: spec.metadata,
    };
  },
);

export const DEMO_ROLE_BINDINGS = DEMO_ROLE_BINDING_SPECS.map((spec, index) => {
  return {
    roleBindingId: seedUuid(5000 + index),
    actorId: demoActorIdForEmployeeId(spec.employeeId),
    roleKey: spec.roleKey,
    scopeType: spec.scopeType,
    scopeOrgUnitId:
      spec.scopeOrgUnitKey === undefined
        ? null
        : orgUnitSeedIdForKey(spec.scopeOrgUnitKey),
    scopeValue: spec.scopeValue ?? null,
    relationshipType: spec.relationshipType ?? null,
    status: spec.status,
    effectiveStart: spec.effectiveStart,
    effectiveEnd: spec.effectiveEnd ?? null,
    metadata: spec.metadata,
  };
});

function orgUnitSeedIdForKey(unitKey: string): string {
  const orgUnitId = orgUnitIdsByKey.get(unitKey);

  if (orgUnitId === undefined) {
    throw new Error(`Missing demo organization unit ${unitKey}`);
  }

  return orgUnitId;
}

function demoActorIdForEmployeeId(employeeId: string): string {
  const index = DEMO_EMPLOYEE_SPECS.findIndex((spec) => {
    return spec.employeeId === employeeId;
  });

  if (index < 0) {
    throw new Error(`Missing demo actor employee ${employeeId}`);
  }

  const spec = DEMO_EMPLOYEE_SPECS[index];

  if (spec === undefined) {
    throw new Error(`Missing demo actor employee ${employeeId}`);
  }

  return demoActorIdForEmployee(spec, index);
}

export const DEMO_WORKFLOW_GRAPH = {
  workflowType: CHANGE_REQUEST_TYPES.EMPLOYEE_DATA_CHANGE,
  intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
  version: 1,
  initialState: WORKFLOW_STATES.COLLECTING_INPUT,
  states: {
    [WORKFLOW_STATES.COLLECTING_INPUT]: {
      interaction: "legal_name_form",
      transitions: {
        [WORKFLOW_TRANSITIONS.SUBMIT_INPUT]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_REQUEST,
          next: WORKFLOW_STATES.COLLECTING_EVIDENCE,
        },
        [WORKFLOW_TRANSITIONS.CANCEL]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_CANCEL_OWN,
          next: WORKFLOW_STATES.CANCELED,
        },
      },
    },
    [WORKFLOW_STATES.COLLECTING_EVIDENCE]: {
      interaction: "legal_name_evidence_upload",
      transitions: {
        [WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_PROVIDE_EVIDENCE,
          next: WORKFLOW_STATES.WAITING_APPROVAL,
        },
        [WORKFLOW_TRANSITIONS.CANCEL]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_CANCEL_OWN,
          next: WORKFLOW_STATES.CANCELED,
        },
      },
    },
    [WORKFLOW_STATES.WAITING_APPROVAL]: {
      interaction: "hr_approval_card",
      transitions: {
        [WORKFLOW_TRANSITIONS.APPROVE]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_APPROVE,
          requiresApprovalTask: true,
          next: WORKFLOW_STATES.APPROVED,
        },
        [WORKFLOW_TRANSITIONS.REJECT]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_REJECT,
          requiresApprovalTask: true,
          next: WORKFLOW_STATES.REJECTED,
        },
        [WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_REQUEST_MORE_INFO,
          requiresApprovalTask: true,
          next: WORKFLOW_STATES.COLLECTING_EVIDENCE,
        },
      },
    },
    [WORKFLOW_STATES.APPROVED]: {
      interaction: "ready_to_execute",
      transitions: {
        [WORKFLOW_TRANSITIONS.EXECUTE]: {
          requiresPermission: PERMISSION_KEYS.LEGAL_NAME_EXECUTE,
          next: WORKFLOW_STATES.EXECUTED,
        },
      },
    },
    [WORKFLOW_STATES.EXECUTED]: {
      terminal: true,
    },
    [WORKFLOW_STATES.REJECTED]: {
      terminal: true,
    },
    [WORKFLOW_STATES.CANCELED]: {
      terminal: true,
    },
    [WORKFLOW_STATES.FAILED]: {
      terminal: false,
      nextRepairState: WORKFLOW_STATES.WAITING_REPAIR,
    },
  },
  ledgerEvents: Object.values(LEDGER_EVENT_TYPES),
  approvalDefaults: {
    assigneeRole: ROLE_KEYS.HR_ADMIN,
    initialStatus: APPROVAL_TASK_STATUSES.PENDING,
  },
  documentRequirements: {
    purpose: "legal_name_change_evidence",
    classifications: [DOCUMENT_CLASSIFICATIONS.SENSITIVE_PERSON_IDENTITY],
  },
} as const;

export const DEMO_LEGAL_NAME_INPUT_SCHEMA = {
  type: "object",
  required: ["newLegalName", "effectiveAt", "businessReason"],
  properties: {
    newLegalName: {
      type: "object",
      required: ["first", "last"],
      properties: {
        first: {
          type: "string",
          minLength: 1,
        },
        middle: {
          type: ["string", "null"],
        },
        last: {
          type: "string",
          minLength: 1,
        },
      },
    },
    effectiveAt: {
      type: "string",
      format: "date",
    },
    businessReason: {
      type: "string",
      enum: ["marriage", "divorce", "legal_name_change", "correction", "other"],
    },
  },
} as const;

export const DEMO_WORKFLOW_OUTPUT_SCHEMA = {
  type: "object",
  properties: {
    workflowInstance: {
      type: "object",
    },
    currentInteraction: {
      type: "object",
    },
    availableActions: {
      type: "array",
    },
  },
} as const;

type DemoPermissionPolicy = {
  permissionPolicyId: string;
  name: string;
  subjectScope: Record<string, unknown>;
  resourceScope: Record<string, unknown>;
  actions: readonly string[];
  fieldPermissions: Record<string, unknown>;
  effect: "allow";
  priority: number;
};

const profileViewActions = [
  PERMISSION_KEYS.EMPLOYEE_VIEW_PROFILE,
  PERMISSION_KEYS.EMPLOYEE_VIEW_ORGANIZATION,
  PERMISSION_KEYS.EMPLOYEE_VIEW_JOB,
  PERMISSION_KEYS.EMPLOYEE_VIEW_EMPLOYMENT,
] as const;

const contactViewActions = [
  PERMISSION_KEYS.EMPLOYEE_VIEW_CONTACT,
  PERMISSION_KEYS.EMPLOYEE_VIEW_EMERGENCY_CONTACTS,
] as const;

const compensationViewActions = [PERMISSION_KEYS.EMPLOYEE_VIEW_COMPENSATION] as const;
const workflowViewActions = [PERMISSION_KEYS.EMPLOYEE_VIEW_WORKFLOW] as const;

const profileVisibleFields = [
  "person.displayName",
  "organization.legalEntity",
  "organization.businessUnit",
  "organization.department",
  "organization.team",
  "organization.location",
  "organization.costCenter",
  "manager.employeeId",
  "job.jobCode",
  "job.title",
  "job.family",
  "job.level",
  "employment.status",
  "employment.workerType",
  "employment.hireDate",
] as const;

const contactVisibleFields = [
  "person.workEmail",
  "contact.personalEmail",
  "contact.mobilePhone",
  "contact.homeAddress",
  "emergencyContacts",
] as const;

const compensationVisibleFields = ["compensation"] as const;
const workflowVisibleFields = [
  "workflow.metadata",
  "documents.evidence_metadata",
] as const;

function permissionPolicy(input: {
  permissionPolicyId: string;
  name: string;
  subjectScope: Record<string, unknown>;
  resourceScope: Record<string, unknown>;
  actions: readonly string[];
  visible: readonly string[];
  fieldGroups: readonly string[];
  priority?: number;
}): DemoPermissionPolicy {
  return {
    permissionPolicyId: input.permissionPolicyId,
    name: input.name,
    subjectScope: input.subjectScope,
    resourceScope: {
      organization: DEMO_ORGANIZATION.slug,
      ...input.resourceScope,
    },
    actions: input.actions,
    fieldPermissions: {
      visible: input.visible,
      fieldGroups: input.fieldGroups,
    },
    effect: "allow",
    priority: input.priority ?? 100,
  };
}

function personaSubject(persona: DemoAccessPersona, roles: readonly string[]) {
  return {
    roles,
    accessPersonas: [persona],
  };
}

export const DEMO_PERMISSION_POLICIES = [
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.EMPLOYEE_PERMISSION_POLICY_ID,
    name: "HarborCare employee self-service",
    subjectScope: {
      roles: [ROLE_KEYS.EMPLOYEE],
    },
    resourceScope: {
      relationship: "own_worker_record",
    },
    actions: [
      PERMISSION_KEYS.LEGAL_NAME_REQUEST,
      PERMISSION_KEYS.LEGAL_NAME_PROVIDE_EVIDENCE,
      PERMISSION_KEYS.LEGAL_NAME_CANCEL_OWN,
      PERMISSION_KEYS.EMPLOYEE_VIEW_PROFILE,
      PERMISSION_KEYS.EMPLOYEE_VIEW_CONTACT,
      PERMISSION_KEYS.EMPLOYEE_VIEW_EMPLOYMENT,
      PERMISSION_KEYS.EMPLOYEE_VIEW_EMERGENCY_CONTACTS,
    ],
    visible: [
      "person.legalName",
      "person.displayName",
      "person.workEmail",
      "employment.status",
      "employment.workerType",
      "contact.personalEmail",
      "contact.mobilePhone",
      "contact.homeAddress",
      "emergencyContacts",
      "documents.own_metadata",
    ],
    fieldGroups: ["profile", "employment", "contact", "emergency_contacts"],
    priority: 90,
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.HR_PERMISSION_POLICY_ID,
    name: "HarborCare legal name approval",
    subjectScope: {
      roles: [ROLE_KEYS.HR_ADMIN],
    },
    resourceScope: {
      workflowIntent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    },
    actions: [
      PERMISSION_KEYS.LEGAL_NAME_APPROVE,
      PERMISSION_KEYS.LEGAL_NAME_REJECT,
      PERMISSION_KEYS.LEGAL_NAME_REQUEST_MORE_INFO,
      PERMISSION_KEYS.LEGAL_NAME_EXECUTE,
      PERMISSION_KEYS.LEGAL_NAME_VIEW_EVIDENCE,
    ],
    visible: ["person.legalName", ...workflowVisibleFields],
    fieldGroups: ["profile", "workflow"],
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.SYSTEM_PERMISSION_POLICY_ID,
    name: "HarborCare system workflow execution",
    subjectScope: {
      roles: [ROLE_KEYS.SYSTEM],
    },
    resourceScope: {
      workflowIntent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    },
    actions: [PERMISSION_KEYS.LEGAL_NAME_EXECUTE],
    visible: ["person.legalName"],
    fieldGroups: ["profile"],
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.PRIMARY_HR_PROFILE_POLICY_ID,
    name: "HarborCare primary HR global profile access",
    subjectScope: personaSubject("primary_hr_admin", [ROLE_KEYS.HR_ADMIN]),
    resourceScope: {
      scope: "global",
    },
    actions: [...profileViewActions, ...contactViewActions, ...workflowViewActions],
    visible: [
      ...profileVisibleFields,
      ...contactVisibleFields,
      ...workflowVisibleFields,
    ],
    fieldGroups: [
      "profile",
      "organization",
      "job",
      "employment",
      "contact",
      "emergency_contacts",
      "workflow",
    ],
    priority: 80,
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.SECONDARY_HR_PROFILE_POLICY_ID,
    name: "HarborCare secondary HR clinical and corporate profile access",
    subjectScope: personaSubject("secondary_hr_admin", [ROLE_KEYS.HR_ADMIN]),
    resourceScope: {
      scope: "business_unit",
      values: ["Clinical Operations", "Corporate"],
    },
    actions: [...profileViewActions, ...contactViewActions, ...workflowViewActions],
    visible: [
      ...profileVisibleFields,
      ...contactVisibleFields,
      ...workflowVisibleFields,
    ],
    fieldGroups: [
      "profile",
      "organization",
      "job",
      "employment",
      "contact",
      "emergency_contacts",
      "workflow",
    ],
    priority: 85,
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.MANAGER_REPORTS_POLICY_ID,
    name: "HarborCare manager direct reports",
    subjectScope: {
      roles: [ROLE_KEYS.MANAGER],
    },
    resourceScope: {
      relationship: "direct_reports",
    },
    actions: [...profileViewActions],
    visible: [...profileVisibleFields],
    fieldGroups: ["profile", "organization", "job", "employment"],
    priority: 90,
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.CLINIC_OPS_POLICY_ID,
    name: "HarborCare clinic operations profile access",
    subjectScope: personaSubject("clinic_ops_admin", [ROLE_KEYS.MANAGER]),
    resourceScope: {
      scope: "business_unit",
      values: ["Clinical Operations"],
    },
    actions: [...profileViewActions],
    visible: [...profileVisibleFields],
    fieldGroups: ["profile", "organization", "job", "employment"],
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.MEDICAL_DIRECTOR_POLICY_ID,
    name: "HarborCare medical director clinical staff access",
    subjectScope: personaSubject("medical_director", [ROLE_KEYS.MANAGER]),
    resourceScope: {
      scope: "department",
      values: ["Clinical Care", "Behavioral Health", "Care Coordination"],
    },
    actions: [...profileViewActions],
    visible: [...profileVisibleFields],
    fieldGroups: ["profile", "organization", "job", "employment"],
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.FINANCE_COST_CENTER_POLICY_ID,
    name: "HarborCare finance revenue cost centers",
    subjectScope: personaSubject("finance_admin", [ROLE_KEYS.FINANCE_ADMIN]),
    resourceScope: {
      scope: "cost_center",
      values: ["FIN-200", "FIN-220", "REV-500", "REV-510", "REV-520"],
    },
    actions: [...profileViewActions, ...compensationViewActions],
    visible: [...profileVisibleFields, ...compensationVisibleFields],
    fieldGroups: ["profile", "organization", "job", "employment", "compensation"],
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.COMPENSATION_POLICY_ID,
    name: "HarborCare compensation global access",
    subjectScope: personaSubject("compensation_admin", [ROLE_KEYS.COMPENSATION_ADMIN]),
    resourceScope: {
      scope: "global",
    },
    actions: [...profileViewActions, ...compensationViewActions],
    visible: [...profileVisibleFields, ...compensationVisibleFields],
    fieldGroups: ["profile", "organization", "job", "employment", "compensation"],
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.COMPLIANCE_POLICY_ID,
    name: "HarborCare compliance workflow access",
    subjectScope: personaSubject("compliance_admin", [ROLE_KEYS.HR_ADMIN]),
    resourceScope: {
      scope: "global",
    },
    actions: [...profileViewActions, ...workflowViewActions],
    visible: [...profileVisibleFields, ...workflowVisibleFields],
    fieldGroups: ["profile", "organization", "job", "employment", "workflow"],
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.IT_IDENTITY_POLICY_ID,
    name: "HarborCare IT identity access",
    subjectScope: personaSubject("it_admin", [ROLE_KEYS.MANAGER]),
    resourceScope: {
      scope: "global",
    },
    actions: [
      PERMISSION_KEYS.EMPLOYEE_VIEW_PROFILE,
      PERMISSION_KEYS.EMPLOYEE_VIEW_ORGANIZATION,
      PERMISSION_KEYS.EMPLOYEE_VIEW_JOB,
    ],
    visible: [
      "person.displayName",
      "person.workEmail",
      "organization.businessUnit",
      "organization.department",
      "organization.team",
      "organization.location",
      "job.jobCode",
      "job.title",
      "job.level",
    ],
    fieldGroups: ["profile", "organization", "job"],
  }),
  permissionPolicy({
    permissionPolicyId: DEMO_SEED_IDS.EXECUTIVE_POLICY_ID,
    name: "HarborCare executive global access",
    subjectScope: personaSubject("executive", [ROLE_KEYS.MANAGER]),
    resourceScope: {
      scope: "global",
    },
    actions: [...profileViewActions, ...compensationViewActions],
    visible: [...profileVisibleFields, ...compensationVisibleFields],
    fieldGroups: ["profile", "organization", "job", "employment", "compensation"],
  }),
] as const;

export const DEMO_WORKFLOW_INITIAL_INTERACTION = {
  type: "form",
  schemaVersion: "2026-05-15.1",
  title: "Request legal name change",
  jsonSchema: DEMO_LEGAL_NAME_INPUT_SCHEMA,
  uiSchema: {
    layout: "wizard",
    submitLabel: "Continue",
  },
  state: WORKFLOW_STATES.COLLECTING_INPUT,
  status: WORKFLOW_STATUSES.ACTIVE,
} as const;
