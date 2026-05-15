import {
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";

export type WorkflowBlockRuntime = "go";

export type WorkflowBlockSideEffectClassification =
  | "pure_validation"
  | "transaction_planning"
  | "integration_request_planning"
  | "projection_planning";

export type WorkflowBlockCatalogEntry = {
  name: string;
  version: string;
  domain: string;
  description: string;
  runtime: WorkflowBlockRuntime;
  sideEffectClassification: WorkflowBlockSideEffectClassification;
  inputContract: WorkflowBlockContractSummary;
  outputContract: WorkflowBlockContractSummary;
  requiredCapabilities: string[];
  requiredIntegrationBindings: string[];
  requiredSecretReferences: string[];
  deprecated: boolean;
  replacementBlock?: {
    name: string;
    version: string;
  };
};

export type WorkflowBlockContractSummary = {
  requiredFields: string[];
  optionalFields: string[];
  outputFields: string[];
};

export type WorkflowBlockReferenceInput = {
  name: string;
  version: string;
  runtime?: string;
};

const goRuntime = "go" as const;

const workflowBlockCatalogEntries: WorkflowBlockCatalogEntry[] = [
  preflightBlock({
    name: "system.employee_data.legal_name.preflight",
    domain: "employee_legal_name",
    description: "Validates a proposed legal name change against the current name.",
    requiredFields: [
      "currentLegalName",
      "proposedLegalName",
      "effectiveAt",
      "businessReason",
    ],
  }),
  transactionPlanningBlock({
    name: "system.employee_data.legal_name.plan_transaction",
    domain: "employee_legal_name",
    description: "Plans ledger events and projection patches for a legal name change.",
    requiredFields: [
      "changeRequestId",
      "workerId",
      "personId",
      "currentLegalName",
      "proposedLegalName",
      "effectiveAt",
    ],
  }),
  preflightBlock({
    name: "system.employee_data.contact_info.preflight",
    domain: "employee_contact_info",
    description: "Validates proposed employee contact information.",
    requiredFields: ["currentContactInfo", "proposedContactInfo", "effectiveAt"],
    optionalFields: ["businessReason"],
  }),
  transactionPlanningBlock({
    name: "system.employee_data.contact_info.plan_transaction",
    domain: "employee_contact_info",
    description: "Plans ledger events and projection patches for contact info updates.",
    requiredFields: [
      "changeRequestId",
      "workerId",
      "currentContactInfo",
      "proposedContactInfo",
      "effectiveAt",
    ],
  }),
  preflightBlock({
    name: "system.employee_data.emergency_contact.preflight",
    domain: "employee_emergency_contact",
    description: "Validates proposed emergency contact changes.",
    requiredFields: ["currentEmergencyContacts", "proposedEmergencyContact"],
    optionalFields: ["effectiveAt", "businessReason"],
  }),
  transactionPlanningBlock({
    name: "system.employee_data.emergency_contact.plan_transaction",
    domain: "employee_emergency_contact",
    description:
      "Plans ledger events and projection patches for emergency contact updates.",
    requiredFields: [
      "changeRequestId",
      "workerId",
      "currentEmergencyContacts",
      "proposedEmergencyContact",
      "effectiveAt",
    ],
  }),
  preflightBlock({
    name: "system.employee_data.compensation.preflight",
    domain: "employee_compensation",
    description: "Validates proposed compensation changes before approval.",
    requiredFields: [
      "currentCompensation",
      "proposedCompensation",
      "currentJob",
      "currentOrganization",
      "effectiveAt",
      "businessReason",
    ],
  }),
  transactionPlanningBlock({
    name: "system.employee_data.compensation.plan_transaction",
    domain: "employee_compensation",
    description:
      "Plans ledger events, projection patches, and vendor payload hints for compensation changes.",
    requiredFields: [
      "changeRequestId",
      "workerId",
      "currentCompensation",
      "proposedCompensation",
      "effectiveAt",
      "businessReason",
    ],
    sideEffectClassification: "integration_request_planning",
  }),
  preflightBlock({
    name: "system.employee_data.org_transfer.preflight",
    domain: "employee_org_transfer",
    description:
      "Validates org transfer, manager, job, compensation, and access impact inputs.",
    requiredFields: [
      "currentOrganization",
      "proposedOrganization",
      "currentJob",
      "proposedJob",
      "currentCompensation",
      "proposedCompensation",
      "effectiveAt",
      "businessReason",
    ],
  }),
  transactionPlanningBlock({
    name: "system.employee_data.org_transfer.plan_transaction",
    domain: "employee_org_transfer",
    description:
      "Plans worker assignment, role binding, compensation, and external sync transactions for org transfer.",
    requiredFields: [
      "changeRequestId",
      "workerId",
      "currentOrganization",
      "proposedOrganization",
      "currentJob",
      "proposedJob",
      "currentCompensation",
      "proposedCompensation",
      "effectiveAt",
      "businessReason",
    ],
    sideEffectClassification: "integration_request_planning",
  }),
];

/**
 * Lists the deterministic Go blocks available to workflow configs.
 */
export function listWorkflowBlockCatalogEntries(): WorkflowBlockCatalogEntry[] {
  return workflowBlockCatalogEntries.map((entry) => {
    return cloneCatalogEntry(entry);
  });
}

/**
 * Finds one catalog block by name and version.
 */
export function findWorkflowBlockCatalogEntry(
  blockReference: WorkflowBlockReferenceInput,
): WorkflowBlockCatalogEntry | undefined {
  return workflowBlockCatalogEntries.find((entry) => {
    return (
      entry.name === blockReference.name && entry.version === blockReference.version
    );
  });
}

/**
 * Validates a config block reference against the available deterministic block catalog.
 */
export function validateWorkflowBlockReference(
  blockReference: WorkflowBlockReferenceInput,
): Result<WorkflowBlockCatalogEntry, AppError> {
  if (blockReference.runtime !== undefined && blockReference.runtime !== goRuntime) {
    return err(
      validationFailedError({
        blockReference,
        reason: "Only Go workflow blocks are registered for v0.",
      }),
    );
  }

  const catalogEntry = findWorkflowBlockCatalogEntry(blockReference);
  if (catalogEntry === undefined) {
    return err(
      validationFailedError({
        blockReference,
        reason: "Workflow block is not registered in the catalog.",
      }),
    );
  }

  return ok(cloneCatalogEntry(catalogEntry));
}

function preflightBlock(input: {
  name: string;
  domain: string;
  description: string;
  requiredFields: string[];
  optionalFields?: string[];
}): WorkflowBlockCatalogEntry {
  return blockCatalogEntry({
    ...input,
    sideEffectClassification: "pure_validation",
    outputFields: ["isValid", "riskLevel", "requiresEvidence", "requiresApproval"],
  });
}

function transactionPlanningBlock(input: {
  name: string;
  domain: string;
  description: string;
  requiredFields: string[];
  optionalFields?: string[];
  sideEffectClassification?: WorkflowBlockSideEffectClassification;
}): WorkflowBlockCatalogEntry {
  return blockCatalogEntry({
    ...input,
    sideEffectClassification: input.sideEffectClassification ?? "transaction_planning",
    outputFields: [
      "ledgerEvents",
      "projectionPatches",
      "externalCalls",
      "validationMessages",
    ],
  });
}

function blockCatalogEntry(input: {
  name: string;
  domain: string;
  description: string;
  requiredFields: string[];
  optionalFields?: string[];
  sideEffectClassification: WorkflowBlockSideEffectClassification;
  outputFields: string[];
}): WorkflowBlockCatalogEntry {
  return {
    name: input.name,
    version: "1.0.0",
    domain: input.domain,
    description: input.description,
    runtime: goRuntime,
    sideEffectClassification: input.sideEffectClassification,
    inputContract: {
      requiredFields: [...input.requiredFields],
      optionalFields: [...(input.optionalFields ?? [])],
      outputFields: [],
    },
    outputContract: {
      requiredFields: [],
      optionalFields: [],
      outputFields: [...input.outputFields],
    },
    requiredCapabilities: [],
    requiredIntegrationBindings: [],
    requiredSecretReferences: [],
    deprecated: false,
  };
}

function cloneCatalogEntry(
  entry: WorkflowBlockCatalogEntry,
): WorkflowBlockCatalogEntry {
  return JSON.parse(JSON.stringify(entry)) as WorkflowBlockCatalogEntry;
}
