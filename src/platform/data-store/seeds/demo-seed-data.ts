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

export const DEMO_SEED_IDS = {
  TENANT_ID: "00000000-0000-4000-8000-000000000001",
  ENVIRONMENT_ID: "00000000-0000-4000-8000-000000000002",
  EMPLOYEE_ACTOR_ID: "00000000-0000-4000-8000-000000000101",
  HR_ACTOR_ID: "00000000-0000-4000-8000-000000000102",
  SYSTEM_ACTOR_ID: "00000000-0000-4000-8000-000000000103",
  WORKFLOW_DEFINITION_ID: "00000000-0000-4000-8000-000000000201",
  WORKFLOW_VERSION_ID: "00000000-0000-4000-8000-000000000202",
  EMPLOYEE_PERMISSION_POLICY_ID: "00000000-0000-4000-8000-000000000301",
  HR_PERMISSION_POLICY_ID: "00000000-0000-4000-8000-000000000302",
  SYSTEM_PERMISSION_POLICY_ID: "00000000-0000-4000-8000-000000000303",
  LEGAL_NAME_FIELD_ID: "00000000-0000-4000-8000-000000000401",
  EVIDENCE_FIELD_ID: "00000000-0000-4000-8000-000000000402",
} as const;

export const DEMO_EMPLOYEE_PROJECTION = {
  employeeId: DEMO_SEED_ALIASES.EMPLOYEE,
  person: {
    personId: DEMO_SEED_ALIASES.PERSON,
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
    employeeId: DEMO_SEED_ALIASES.MANAGER_EMPLOYEE,
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
} as const;

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

export const DEMO_PERMISSION_POLICIES = [
  {
    permissionPolicyId: DEMO_SEED_IDS.EMPLOYEE_PERMISSION_POLICY_ID,
    name: "Demo employee self-service legal name change",
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
    ],
    fieldPermissions: {
      visible: ["person.legalName", "documents.own_metadata"],
    },
    effect: "allow",
    priority: 100,
  },
  {
    permissionPolicyId: DEMO_SEED_IDS.HR_PERMISSION_POLICY_ID,
    name: "Demo HR legal name approval",
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
    fieldPermissions: {
      visible: ["person.legalName", "documents.evidence_metadata"],
    },
    effect: "allow",
    priority: 100,
  },
  {
    permissionPolicyId: DEMO_SEED_IDS.SYSTEM_PERMISSION_POLICY_ID,
    name: "Demo system workflow execution",
    subjectScope: {
      roles: [ROLE_KEYS.SYSTEM],
    },
    resourceScope: {
      workflowIntent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    },
    actions: [PERMISSION_KEYS.LEGAL_NAME_EXECUTE],
    fieldPermissions: {
      visible: ["person.legalName"],
    },
    effect: "allow",
    priority: 100,
  },
] as const;

export const DEMO_ACTORS = [
  {
    actorId: DEMO_SEED_IDS.EMPLOYEE_ACTOR_ID,
    actorType: ACTOR_TYPES.HUMAN,
    linkedWorkerId: DEMO_SEED_ALIASES.EMPLOYEE,
    email: "jane.doe@example.com",
    displayName: "Jane Doe",
    roles: [ROLE_KEYS.EMPLOYEE],
    externalSubject: DEMO_SEED_ALIASES.EMPLOYEE_ACTOR,
  },
  {
    actorId: DEMO_SEED_IDS.HR_ACTOR_ID,
    actorType: ACTOR_TYPES.HUMAN,
    linkedWorkerId: null,
    email: "hr.admin@example.com",
    displayName: "HR Admin",
    roles: [ROLE_KEYS.HR_ADMIN],
    externalSubject: DEMO_SEED_ALIASES.HR_ACTOR,
  },
  {
    actorId: DEMO_SEED_IDS.SYSTEM_ACTOR_ID,
    actorType: ACTOR_TYPES.SYSTEM,
    linkedWorkerId: null,
    email: null,
    displayName: "HCM Next System",
    roles: [ROLE_KEYS.SYSTEM],
    externalSubject: DEMO_SEED_ALIASES.SYSTEM_ACTOR,
  },
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
