import {
  err,
  fromThrowable,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import { canonicalWorkflowConfigJsonSchema } from "../../shared/workflow-config-schema.js";
import type {
  WorkflowConfig,
  WorkflowValidationReport,
} from "../../shared/workflow-config.js";
import {
  exportWorkflowConfigJson,
  validateWorkflowConfigForAuthoring,
  workflowConfigEditorHash,
} from "./validation.js";

export type WorkflowDraftJsonRequest = {
  tenantId: string;
  environmentId: string;
  workflowDraftId: string;
};

export type WorkflowDraftJsonResponse = {
  workflowDraftId: string;
  workflowFamilyId: string;
  draftVersion: number;
  configHash: string;
  workflowConfig: WorkflowConfig;
  jsonSchema: Record<string, unknown>;
};

export type SaveWorkflowDraftJsonRequest = {
  tenantId: string;
  environmentId: string;
  workflowDraftId: string;
  expectedDraftVersion: number;
  workflowConfig: WorkflowConfig;
};

export type SaveWorkflowDraftJsonResponse = {
  workflowDraftId: string;
  draftVersion: number;
  configHash: string;
  validation: WorkflowValidationReport;
};

export type ValidateWorkflowJsonRequest = {
  workflowConfig: unknown;
};

export type ValidateWorkflowJsonResponse = {
  validation: WorkflowValidationReport;
  jsonSchema: Record<string, unknown>;
};

export type ExportWorkflowJsonRequest = {
  workflowConfig: unknown;
};

export type ExportWorkflowJsonResponse = {
  contentType: "application/json";
  fileExtension: ".workflow.json";
  configHash: string;
  body: string;
};

export type ImportWorkflowJsonRequest = {
  body: string;
};

export type ImportWorkflowJsonResponse = {
  workflowConfig: unknown;
  validation: WorkflowValidationReport;
};

/**
 * Validates unsaved JSON in the admin editor without touching persistence.
 */
export function validateWorkflowJsonForEditor(
  request: ValidateWorkflowJsonRequest,
): Result<ValidateWorkflowJsonResponse, AppError> {
  const validation = validateWorkflowConfigForAuthoring(request.workflowConfig);

  return ok({
    validation,
    jsonSchema: canonicalWorkflowConfigJsonSchema,
  });
}

/**
 * Serializes editor JSON with stable two-space formatting for import/export.
 */
export function exportWorkflowJsonForEditor(
  request: ExportWorkflowJsonRequest,
): Result<ExportWorkflowJsonResponse, AppError> {
  return ok({
    contentType: "application/json",
    fileExtension: ".workflow.json",
    configHash: workflowConfigEditorHash(request.workflowConfig),
    body: exportWorkflowConfigJson(request.workflowConfig),
  });
}

/**
 * Parses imported workflow JSON and returns the same validation shape as live editing.
 */
export function importWorkflowJsonForEditor(
  request: ImportWorkflowJsonRequest,
): Result<ImportWorkflowJsonResponse, AppError> {
  const parseResult = fromThrowable(
    () => JSON.parse(request.body) as unknown,
    (error) =>
      validationFailedError({
        importJson: "invalid_json",
        parserMessage: error instanceof Error ? error.message : String(error),
      }),
  );
  if (!parseResult.ok) {
    return err(parseResult.error);
  }

  const validationResult = validateWorkflowJsonForEditor({
    workflowConfig: parseResult.value,
  });
  if (!validationResult.ok) {
    return validationResult;
  }

  return ok({
    workflowConfig: parseResult.value,
    validation: validationResult.value.validation,
  });
}
