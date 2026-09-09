import {
  err,
  notFoundError,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import legalNameWorkflowConfigJson from "../configs/employee-legal-name-change.workflow.json";
import emergencyContactWorkflowConfigJson from "../configs/employee-emergency-contact-update.workflow.json";
import contactInfoWorkflowConfigJson from "../configs/employee-contact-info-update.workflow.json";
import compensationWorkflowConfigJson from "../configs/employee-compensation-change.workflow.json";
import orgTransferCompensationChangeWorkflowConfigJson from "../configs/employee-org-transfer-compensation-change.workflow.json";
import positionHeadcountRequisitionWorkflowConfigJson from "../configs/position-headcount-requisition.workflow.json";
import employeeTerminationWorkflowConfigJson from "../configs/employee-termination.workflow.json";
import { validateWorkflowConfig } from "./workflow-config-validation.js";
import type { WorkflowConfig } from "./workflow-config.js";

export { canonicalJsonString, workflowConfigHash } from "./workflow-config-hash.js";

export type WorkflowConfigFileEntry = {
  fileName: string;
  workflowConfig: WorkflowConfig;
};

const filesystemWorkflowConfigEntries = [
  {
    fileName: "employee-legal-name-change.workflow.json",
    workflowConfig: legalNameWorkflowConfigJson as unknown as WorkflowConfig,
  },
  {
    fileName: "employee-emergency-contact-update.workflow.json",
    workflowConfig: emergencyContactWorkflowConfigJson as unknown as WorkflowConfig,
  },
  {
    fileName: "employee-contact-info-update.workflow.json",
    workflowConfig: contactInfoWorkflowConfigJson as unknown as WorkflowConfig,
  },
  {
    fileName: "employee-compensation-change.workflow.json",
    workflowConfig: compensationWorkflowConfigJson as unknown as WorkflowConfig,
  },
  {
    fileName: "employee-org-transfer-compensation-change.workflow.json",
    workflowConfig:
      orgTransferCompensationChangeWorkflowConfigJson as unknown as WorkflowConfig,
  },
  {
    fileName: "position-headcount-requisition.workflow.json",
    workflowConfig:
      positionHeadcountRequisitionWorkflowConfigJson as unknown as WorkflowConfig,
  },
  {
    fileName: "employee-termination.workflow.json",
    workflowConfig: employeeTerminationWorkflowConfigJson as unknown as WorkflowConfig,
  },
] as const;

const filesystemWorkflowConfigs = filesystemWorkflowConfigEntries.map((entry) => {
  return entry.workflowConfig;
});

/**
 * Lists checked-in workflow config files with cloned executable payloads.
 */
export function listFilesystemWorkflowConfigEntries(): WorkflowConfigFileEntry[] {
  return filesystemWorkflowConfigEntries.map((entry) => {
    return {
      fileName: entry.fileName,
      workflowConfig: cloneWorkflowConfig(entry.workflowConfig),
    };
  });
}

/**
 * Returns cloned workflow configs checked into the developer filesystem.
 */
export function listFilesystemWorkflowConfigs(): WorkflowConfig[] {
  return listFilesystemWorkflowConfigEntries().map((entry) => {
    return entry.workflowConfig;
  });
}

/**
 * Lists workflow intents available from checked-in workflow config files.
 */
export function listFilesystemWorkflowConfigIntents(): string[] {
  return filesystemWorkflowConfigEntries.map((entry) => {
    return entry.workflowConfig.intent;
  });
}

/**
 * Finds a checked-in workflow config by public intent.
 */
export function findFilesystemWorkflowConfigByIntent(
  intent: string,
): Result<WorkflowConfig, AppError> {
  const workflowConfig = filesystemWorkflowConfigs.find((candidate) => {
    return candidate.intent === intent;
  });

  if (workflowConfig === undefined) {
    return err(notFoundError("Workflow config", { intent }));
  }

  return ok(cloneWorkflowConfig(workflowConfig));
}

/**
 * Verifies a persisted graph definition contains the full executable config shape.
 */
export function workflowConfigFromGraphDefinition(
  graphDefinition: Record<string, unknown>,
): Result<WorkflowConfig, AppError> {
  const validationReport = validateWorkflowConfig(graphDefinition);

  if (!validationReport.valid) {
    return err(
      validationFailedError({
        graphDefinition:
          "Persisted graph definition is not an executable workflow config.",
        validationErrors: validationReport.errors,
      }),
    );
  }

  return ok(cloneWorkflowConfig(graphDefinition as WorkflowConfig));
}

/**
 * Resolves a persisted graph definition into an executable workflow config.
 */
export function resolveWorkflowConfigFromGraphDefinition(
  graphDefinition: Record<string, unknown>,
): Result<WorkflowConfig, AppError> {
  return workflowConfigFromGraphDefinition(graphDefinition);
}

/**
 * Checks whether a JSON value has enough structure to execute as a workflow config.
 */
export function isWorkflowConfigRecord(value: unknown): value is WorkflowConfig {
  return validateWorkflowConfig(value).valid;
}

/**
 * Clones a workflow config so registry consumers cannot mutate source imports.
 */
export function cloneWorkflowConfig<TValue extends WorkflowConfig>(
  value: TValue,
): TValue {
  return JSON.parse(JSON.stringify(value)) as TValue;
}
