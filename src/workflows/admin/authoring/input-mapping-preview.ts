import {
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import { valueAtDotPath } from "../../shared/json-fields.js";
import type {
  WorkflowConfig,
  WorkflowGraphNodeConfig,
} from "../../shared/workflow-config.js";

export type WorkflowInputMappingSourceType =
  | "workflow_input"
  | "actor_context"
  | "workflow_context"
  | "employee_projection"
  | "previous_node_output"
  | "ledger_fact"
  | "integration_response"
  | "constant"
  | "tenant_setting"
  | "environment_setting"
  | "unknown";

export type WorkflowInputMappingPreviewRequest = {
  workflowConfig: WorkflowConfig;
  nodeId?: string | undefined;
  mappingObject?: Record<string, unknown> | undefined;
  sources?: WorkflowInputMappingSources | Record<string, unknown> | undefined;
};

export type WorkflowInputMappingSources = {
  workflowInput?: Record<string, unknown> | undefined;
  actorContext?: Record<string, unknown> | undefined;
  workflowContext?: Record<string, unknown> | undefined;
  employeeProjection?: Record<string, unknown> | undefined;
  previousNodeOutput?: Record<string, unknown> | undefined;
  ledgerFact?: Record<string, unknown> | undefined;
  integrationResponse?: Record<string, unknown> | undefined;
  tenantSetting?: Record<string, unknown> | undefined;
  environmentSetting?: Record<string, unknown> | undefined;
};

export type WorkflowInputMappingPreviewEntry = {
  targetPath: string;
  sourceType: WorkflowInputMappingSourceType;
  sourcePath?: string;
  required: boolean;
  resolved: boolean;
  redacted: boolean;
  valuePreview?: unknown;
  defaultUsed: boolean;
  fallbackUsed: boolean;
  errors: WorkflowInputMappingPreviewError[];
};

export type WorkflowInputMappingPreviewError = {
  code: string;
  path: string;
  jsonPath: string;
  message: string;
};

export type WorkflowInputMappingPreviewResponse = {
  nodeId?: string;
  entries: WorkflowInputMappingPreviewEntry[];
  unresolved: WorkflowInputMappingPreviewEntry[];
};

type MappingExpression = {
  $source: string;
  path?: string;
  required?: boolean;
  optional?: boolean;
  defaultValue?: unknown;
  fallbackPaths?: unknown[];
};

const sensitivePathTokens = [
  "compensation",
  "salary",
  "pay",
  "ssn",
  "secret",
  "token",
  "credential",
];

/**
 * Resolves configured block input mappings against optional fixture data for admin preview.
 */
export function previewWorkflowInputMappings(
  request: WorkflowInputMappingPreviewRequest,
): Result<WorkflowInputMappingPreviewResponse, AppError> {
  const nodeResult = request.nodeId
    ? findWorkflowNode(request.workflowConfig, request.nodeId)
    : ok(undefined);
  if (!nodeResult.ok) {
    return nodeResult;
  }

  const mappingObject =
    request.mappingObject ?? mappingObjectFromNode(nodeResult.value);
  if (mappingObject === undefined) {
    return err(
      validationFailedError({
        nodeId: request.nodeId,
        mappingObject: "missing",
      }),
    );
  }

  const entries = collectMappingEntries({
    value: mappingObject,
    targetPath: "",
    configPath: request.nodeId === undefined ? "$" : `$.graph.nodes.${request.nodeId}`,
    sources: workflowInputMappingSources(request.sources),
  });

  return ok({
    ...(request.nodeId === undefined ? {} : { nodeId: request.nodeId }),
    entries,
    unresolved: entries.filter((entry) => {
      return !entry.resolved || entry.errors.length > 0;
    }),
  });
}

function workflowInputMappingSources(
  sources: WorkflowInputMappingSources | Record<string, unknown> | undefined,
): WorkflowInputMappingSources {
  return isRecord(sources) ? (sources as WorkflowInputMappingSources) : {};
}

function findWorkflowNode(
  workflowConfig: WorkflowConfig,
  nodeId: string,
): Result<WorkflowGraphNodeConfig, AppError> {
  const node = workflowConfig.graph?.nodes.find((candidate) => {
    return candidate.nodeId === nodeId;
  });

  if (node === undefined) {
    return err(validationFailedError({ nodeId, reason: "Workflow node not found." }));
  }

  return ok(node);
}

function mappingObjectFromNode(
  node: WorkflowGraphNodeConfig | undefined,
): Record<string, unknown> | undefined {
  if (node === undefined) {
    return undefined;
  }

  if (node.blockInput !== undefined) {
    return node.blockInput;
  }

  return undefined;
}

function collectMappingEntries(input: {
  value: unknown;
  targetPath: string;
  configPath: string;
  sources: WorkflowInputMappingSources;
}): WorkflowInputMappingPreviewEntry[] {
  if (isMappingExpression(input.value)) {
    return [
      resolveMappingExpression({
        expression: input.value,
        targetPath: input.targetPath,
        configPath: input.configPath,
        sources: input.sources,
      }),
    ];
  }

  if (Array.isArray(input.value)) {
    return input.value.flatMap((item, itemIndex) => {
      return collectMappingEntries({
        value: item,
        targetPath: appendPath(input.targetPath, String(itemIndex)),
        configPath: `${input.configPath}.${itemIndex}`,
        sources: input.sources,
      });
    });
  }

  if (isRecord(input.value)) {
    return Object.entries(input.value).flatMap(([fieldName, fieldValue]) => {
      return collectMappingEntries({
        value: fieldValue,
        targetPath: appendPath(input.targetPath, fieldName),
        configPath: `${input.configPath}.${fieldName}`,
        sources: input.sources,
      });
    });
  }

  return [
    {
      targetPath: input.targetPath,
      sourceType: "constant",
      required: false,
      resolved: true,
      redacted: isSensitivePath(input.targetPath),
      valuePreview: isSensitivePath(input.targetPath) ? "[redacted]" : input.value,
      defaultUsed: false,
      fallbackUsed: false,
      errors: [],
    },
  ];
}

function resolveMappingExpression(input: {
  expression: MappingExpression;
  targetPath: string;
  configPath: string;
  sources: WorkflowInputMappingSources;
}): WorkflowInputMappingPreviewEntry {
  const sourceType = normalizeSourceType(input.expression.$source);
  const isRequired =
    input.expression.optional === true ? false : (input.expression.required ?? true);
  const errors = validateMappingExpression({
    expression: input.expression,
    configPath: input.configPath,
    sourceType,
  });
  const primaryValue = resolveSourceValue({
    sourceType,
    sourcePath: input.expression.path,
    sources: input.sources,
  });
  const fallbackValue = primaryValue.resolved
    ? primaryValue
    : resolveFallbackValue({
        fallbackPaths: input.expression.fallbackPaths,
        sources: input.sources,
      });
  const defaultValue =
    fallbackValue.resolved || !hasOwnField(input.expression, "defaultValue")
      ? fallbackValue
      : {
          resolved: true,
          value: input.expression.defaultValue,
          fallbackUsed: false,
          defaultUsed: true,
        };
  const isResolved = defaultValue.resolved;
  const shouldRedact =
    sourceType === "constant"
      ? isSensitivePath(input.targetPath)
      : isSensitivePath(input.targetPath) ||
        isSensitivePath(input.expression.path ?? "");

  if (!isResolved && isRequired) {
    errors.push(
      mappingError({
        code: "workflow_mapping.required_value_unresolved",
        path: input.configPath,
        message: `Required mapping ${input.targetPath} could not be resolved from ${input.expression.$source}.${input.expression.path ?? ""}.`,
      }),
    );
  }

  return {
    targetPath: input.targetPath,
    sourceType,
    ...(input.expression.path === undefined
      ? {}
      : { sourcePath: input.expression.path }),
    required: isRequired,
    resolved: isResolved,
    redacted: shouldRedact,
    valuePreview: isResolved
      ? redactedValue(defaultValue.value, shouldRedact)
      : undefined,
    defaultUsed: defaultValue.defaultUsed,
    fallbackUsed: defaultValue.fallbackUsed,
    errors,
  };
}

function validateMappingExpression(input: {
  expression: MappingExpression;
  configPath: string;
  sourceType: WorkflowInputMappingSourceType;
}): WorkflowInputMappingPreviewError[] {
  const errors: WorkflowInputMappingPreviewError[] = [];

  if (input.sourceType === "unknown") {
    errors.push(
      mappingError({
        code: "workflow_mapping.source_type_unknown",
        path: `${input.configPath}.$source`,
        message: `Mapping source ${input.expression.$source} is not supported.`,
      }),
    );
  }

  if (
    input.sourceType !== "constant" &&
    (typeof input.expression.path !== "string" ||
      input.expression.path.trim().length === 0)
  ) {
    errors.push(
      mappingError({
        code: "workflow_mapping.path_missing",
        path: `${input.configPath}.path`,
        message: "Non-constant mappings need a non-empty source path.",
      }),
    );
  }

  if (
    typeof input.expression.path === "string" &&
    !isSupportedDotPath(input.expression.path)
  ) {
    errors.push(
      mappingError({
        code: "workflow_mapping.path_malformed",
        path: `${input.configPath}.path`,
        message: "Mapping paths must use simple dot-path syntax.",
      }),
    );
  }

  return errors;
}

function resolveSourceValue(input: {
  sourceType: WorkflowInputMappingSourceType;
  sourcePath: string | undefined;
  sources: WorkflowInputMappingSources;
}): {
  resolved: boolean;
  value: unknown;
  fallbackUsed: boolean;
  defaultUsed: boolean;
} {
  const sourceRecord = sourceRecordForType(input.sourceType, input.sources);
  if (input.sourceType === "constant") {
    return {
      resolved: true,
      value: input.sourcePath,
      fallbackUsed: false,
      defaultUsed: false,
    };
  }

  if (sourceRecord === undefined || input.sourcePath === undefined) {
    return {
      resolved: false,
      value: undefined,
      fallbackUsed: false,
      defaultUsed: false,
    };
  }

  const value = valueAtDotPath(sourceRecord, input.sourcePath);

  return {
    resolved: value !== undefined,
    value,
    fallbackUsed: false,
    defaultUsed: false,
  };
}

function resolveFallbackValue(input: {
  fallbackPaths: unknown[] | undefined;
  sources: WorkflowInputMappingSources;
}): {
  resolved: boolean;
  value: unknown;
  fallbackUsed: boolean;
  defaultUsed: boolean;
} {
  if (input.fallbackPaths === undefined) {
    return {
      resolved: false,
      value: undefined,
      fallbackUsed: false,
      defaultUsed: false,
    };
  }

  for (const fallbackPath of input.fallbackPaths) {
    if (!isMappingExpression(fallbackPath)) {
      continue;
    }

    const resolvedValue = resolveSourceValue({
      sourceType: normalizeSourceType(fallbackPath.$source),
      sourcePath: fallbackPath.path,
      sources: input.sources,
    });

    if (resolvedValue.resolved) {
      return {
        ...resolvedValue,
        fallbackUsed: true,
      };
    }
  }

  return {
    resolved: false,
    value: undefined,
    fallbackUsed: false,
    defaultUsed: false,
  };
}

function sourceRecordForType(
  sourceType: WorkflowInputMappingSourceType,
  sources: WorkflowInputMappingSources,
): Record<string, unknown> | undefined {
  switch (sourceType) {
    case "workflow_input":
      return sources.workflowInput;
    case "actor_context":
      return sources.actorContext;
    case "workflow_context":
      return sources.workflowContext;
    case "employee_projection":
      return sources.employeeProjection;
    case "previous_node_output":
      return sources.previousNodeOutput;
    case "ledger_fact":
      return sources.ledgerFact;
    case "integration_response":
      return sources.integrationResponse;
    case "tenant_setting":
      return sources.tenantSetting;
    case "environment_setting":
      return sources.environmentSetting;
    case "constant":
    case "unknown":
      return undefined;
  }
}

function normalizeSourceType(source: string): WorkflowInputMappingSourceType {
  switch (source) {
    case "input":
      return "workflow_input";
    case "actor":
      return "actor_context";
    case "workflow":
    case "changeRequest":
    case "proposedChange":
    case "approvalTask":
    case "approvalGate":
    case "transactionPlan":
      return "workflow_context";
    case "employee":
      return "employee_projection";
    case "blockResult":
    case "node":
      return "previous_node_output";
    case "ledger":
      return "ledger_fact";
    case "externalWriteResponse":
      return "integration_response";
    case "tenantPolicy":
      return "tenant_setting";
    case "environment":
      return "environment_setting";
    case "secretRef":
      return "constant";
    default:
      return "unknown";
  }
}

function redactedValue(value: unknown, shouldRedact: boolean): unknown {
  return shouldRedact ? "[redacted]" : value;
}

function isSensitivePath(path: string): boolean {
  const lowerPath = path.toLowerCase();

  return sensitivePathTokens.some((token) => {
    return lowerPath.includes(token);
  });
}

function isSupportedDotPath(path: string): boolean {
  return !path.startsWith(".") && !path.endsWith(".") && !path.includes("..");
}

function mappingError(input: {
  code: string;
  path: string;
  message: string;
}): WorkflowInputMappingPreviewError {
  return {
    code: input.code,
    path: input.path,
    jsonPath: input.path.startsWith("$") ? input.path : `$.${input.path}`,
    message: input.message,
  };
}

function appendPath(basePath: string, segment: string): string {
  return basePath.length === 0 ? segment : `${basePath}.${segment}`;
}

function isMappingExpression(value: unknown): value is MappingExpression {
  return isRecord(value) && typeof value["$source"] === "string";
}

function hasOwnField(record: Record<string, unknown>, field: string): boolean {
  return Object.prototype.hasOwnProperty.call(record, field);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
