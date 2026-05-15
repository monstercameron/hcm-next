import { workflowConfigHash } from "../../shared/workflow-config-hash.js";
import type { WorkflowConfig } from "../../shared/workflow-config.js";
import {
  validateWorkflowConfig,
  type WorkflowValidationIssue,
  type WorkflowValidationReport,
} from "../../shared/workflow-config-validation.js";
import {
  findWorkflowBlockCatalogEntry,
  listWorkflowBlockCatalogEntries,
  type WorkflowBlockCatalogEntry,
} from "../block-catalog/catalog.js";

type BlockReferenceLocation = {
  path: string;
  blockName: string;
  blockVersion: string;
  runtime?: string;
};

const blockReferenceFieldNames = new Set(["block", "preflightBlock"]);

/**
 * Runs admin-facing workflow config validation, including executable schema checks
 * and deterministic block catalog checks.
 */
export function validateWorkflowConfigForAuthoring(
  workflowConfig: unknown,
): WorkflowValidationReport {
  const baseValidationReport = validateWorkflowConfig(workflowConfig);
  const blockCatalogEntries = listWorkflowBlockCatalogEntries();
  const blockCatalogIssues = validateConfiguredBlockReferences(
    workflowConfig,
    blockCatalogEntries,
  );
  const errors = [
    ...baseValidationReport.errors,
    ...blockCatalogIssues.filter((issue) => {
      return issue.severity === "error";
    }),
  ];
  const warnings = [
    ...baseValidationReport.warnings,
    ...blockCatalogIssues.filter((issue) => {
      return issue.severity === "warning";
    }),
  ];

  return {
    valid: errors.length === 0,
    configHash: baseValidationReport.configHash,
    errors,
    warnings,
  };
}

/**
 * Lists all configured deterministic block references with their config paths.
 */
export function listWorkflowConfigBlockReferences(
  workflowConfig: unknown,
): BlockReferenceLocation[] {
  const blockReferenceLocations: BlockReferenceLocation[] = [];

  collectBlockReferences({
    value: workflowConfig,
    path: "$",
    blockReferenceLocations,
  });

  return blockReferenceLocations;
}

/**
 * Builds an export-safe JSON string for authoring download/copy operations.
 */
export function exportWorkflowConfigJson(workflowConfig: unknown): string {
  return `${JSON.stringify(workflowConfig, null, 2)}\n`;
}

/**
 * Returns a deterministic hash for editor change detection.
 */
export function workflowConfigEditorHash(workflowConfig: unknown): string {
  return workflowConfigHash(
    (isRecord(workflowConfig) ? workflowConfig : {}) as WorkflowConfig,
  );
}

function validateConfiguredBlockReferences(
  workflowConfig: unknown,
  blockCatalogEntries: WorkflowBlockCatalogEntry[],
): WorkflowValidationIssue[] {
  const blockReferenceLocations = listWorkflowConfigBlockReferences(workflowConfig);
  const issues: WorkflowValidationIssue[] = [];

  for (const blockReference of blockReferenceLocations) {
    if (blockReference.runtime !== undefined && blockReference.runtime !== "go") {
      issues.push(
        issue({
          code: "workflow_config.block_runtime_unknown",
          path: `${blockReference.path}.runtime`,
          message: `Workflow block runtime ${blockReference.runtime} is not supported.`,
        }),
      );
      continue;
    }

    const matchingNameEntries = blockCatalogEntries.filter((catalogEntry) => {
      return catalogEntry.name === blockReference.blockName;
    });
    const blockLookup = {
      name: blockReference.blockName,
      version: blockReference.blockVersion,
      ...(blockReference.runtime === undefined
        ? {}
        : { runtime: blockReference.runtime }),
    };
    const matchingVersionEntry = findWorkflowBlockCatalogEntry(blockLookup);

    if (matchingNameEntries.length === 0) {
      issues.push(
        issue({
          code: "workflow_config.block_reference_unknown",
          path: blockReference.path,
          message: `Workflow block ${blockReference.blockName} is not registered.`,
        }),
      );
      continue;
    }

    if (matchingVersionEntry === undefined) {
      issues.push(
        issue({
          code: "workflow_config.block_version_unsupported",
          path: `${blockReference.path}.version`,
          message: `Workflow block ${blockReference.blockName} version ${blockReference.blockVersion} is not available.`,
        }),
      );
      continue;
    }

    if (matchingVersionEntry.deprecated) {
      issues.push(
        issue({
          severity: "warning",
          code: "workflow_config.block_version_deprecated",
          path: `${blockReference.path}.version`,
          message: `Workflow block ${blockReference.blockName} version ${blockReference.blockVersion} is deprecated.`,
        }),
      );
    }
  }

  return issues;
}

function collectBlockReferences(input: {
  value: unknown;
  path: string;
  blockReferenceLocations: BlockReferenceLocation[];
}): void {
  if (Array.isArray(input.value)) {
    input.value.forEach((item, itemIndex) => {
      collectBlockReferences({
        value: item,
        path: `${input.path}.${itemIndex}`,
        blockReferenceLocations: input.blockReferenceLocations,
      });
    });
    return;
  }

  if (!isRecord(input.value)) {
    return;
  }

  for (const [fieldName, fieldValue] of Object.entries(input.value)) {
    const fieldPath = `${input.path}.${fieldName}`;

    if (blockReferenceFieldNames.has(fieldName) && isBlockReference(fieldValue)) {
      const blockReferenceLocation: BlockReferenceLocation = {
        path: fieldPath,
        blockName: fieldValue.name,
        blockVersion: fieldValue.version,
      };

      if (typeof fieldValue.runtime === "string") {
        blockReferenceLocation.runtime = fieldValue.runtime;
      }

      input.blockReferenceLocations.push(blockReferenceLocation);
      continue;
    }

    collectBlockReferences({
      value: fieldValue,
      path: fieldPath,
      blockReferenceLocations: input.blockReferenceLocations,
    });
  }
}

function isBlockReference(value: unknown): value is {
  name: string;
  version: string;
  runtime?: string;
} {
  if (!isRecord(value)) {
    return false;
  }

  return typeof value["name"] === "string" && typeof value["version"] === "string";
}

function issue(input: {
  severity?: "error" | "warning";
  code: string;
  path: string;
  message: string;
}): WorkflowValidationIssue {
  return {
    severity: input.severity ?? "error",
    code: input.code,
    path: input.path,
    jsonPath: input.path,
    message: input.message,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
