import {
  WORKFLOW_TRANSITIONS,
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import { valueAtDotPath } from "../../shared/json-fields.js";
import type {
  WorkflowActionConfig,
  WorkflowConfig,
  WorkflowInteractionConfig,
} from "../../shared/workflow-config.js";

export type WorkflowInteractionPreviewRequest = {
  workflowConfig: WorkflowConfig;
  state?: string | undefined;
  interactionKey?: string | undefined;
  actorFixture?: WorkflowInteractionActorFixture | Record<string, unknown> | undefined;
  employeeFixture?: Record<string, unknown> | undefined;
};

export type WorkflowInteractionActorFixture = {
  actorId?: string;
  roles?: string[];
  permissions?: string[];
  [key: string]: unknown;
};

export type WorkflowInteractionPreviewResponse = {
  interactionKey: string;
  state?: string;
  type: string;
  title: string;
  fields: WorkflowInteractionFieldPreview[];
  summarySections: WorkflowInteractionSummarySectionPreview[];
  submitAction?: WorkflowInteractionActionPreview;
  approvalActions: WorkflowInteractionActionPreview[];
  repairAction?: WorkflowInteractionActionPreview;
  warnings: WorkflowInteractionPreviewWarning[];
};

export type WorkflowInteractionFieldPreview = {
  path: string;
  label: string;
  type: string;
  required: boolean;
  defaultValue?: unknown;
  displayCondition?: unknown;
  validationHints: Record<string, unknown>;
};

export type WorkflowInteractionSummarySectionPreview = {
  outputKey: string;
  sourcePath: string;
  visible: boolean;
  redacted: boolean;
  valuePreview?: unknown;
  deniedReasons: string[];
};

export type WorkflowInteractionActionPreview = {
  transition: string;
  label: string;
  actor: string;
  handler: string;
  nextState?: string;
  nextInteraction?: string;
};

export type WorkflowInteractionPreviewWarning = {
  code: string;
  path: string;
  jsonPath: string;
  message: string;
};

const approvalHandlers = new Set<string>([
  WORKFLOW_TRANSITIONS.APPROVE,
  WORKFLOW_TRANSITIONS.REJECT,
  WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO,
]);
const sensitiveFieldTokens = ["compensation", "salary", "pay", "ssn", "secret"];

/**
 * Builds a generic preview of a configured admin/user interaction surface.
 */
export function previewWorkflowInteraction(
  request: WorkflowInteractionPreviewRequest,
): Result<WorkflowInteractionPreviewResponse, AppError> {
  const interactionKeyResult = resolveInteractionKey(request);
  if (!interactionKeyResult.ok) {
    return interactionKeyResult;
  }

  const interaction = request.workflowConfig.interactions[interactionKeyResult.value];
  if (interaction === undefined) {
    return err(
      validationFailedError({
        interactionKey: interactionKeyResult.value,
        reason: "Interaction is not configured.",
      }),
    );
  }

  const stateActions =
    request.state === undefined
      ? []
      : (request.workflowConfig.states[request.state]?.actions ?? []);
  const fields = interactionFields(interaction);
  const summarySections = interactionSummarySections({
    interaction,
    actorFixture: request.actorFixture,
    employeeFixture: request.employeeFixture,
  });
  const approvalActions = stateActions
    .filter((action) => {
      return approvalHandlers.has(action.handler);
    })
    .map(actionPreview);
  const submitAction = stateActions.find((action) => {
    return action.handler === "submit_configured_input";
  });
  const repairAction = stateActions.find((action) => {
    return (
      action.handler === WORKFLOW_TRANSITIONS.REQUEST_MORE_INFO ||
      action.transition.includes("repair")
    );
  });
  const warnings = interactionWarnings({
    interaction,
    interactionKey: interactionKeyResult.value,
    state: request.state,
    stateActions,
    fields,
    approvalActions,
  });

  return ok({
    interactionKey: interactionKeyResult.value,
    ...(request.state === undefined ? {} : { state: request.state }),
    type: interaction.type,
    title: interaction.title,
    fields,
    summarySections,
    ...(submitAction === undefined
      ? {}
      : { submitAction: actionPreview(submitAction) }),
    approvalActions,
    ...(repairAction === undefined
      ? {}
      : { repairAction: actionPreview(repairAction) }),
    warnings,
  });
}

function resolveInteractionKey(
  request: WorkflowInteractionPreviewRequest,
): Result<string, AppError> {
  if (request.interactionKey !== undefined) {
    return ok(request.interactionKey);
  }

  if (request.state !== undefined) {
    const configuredStateInteraction =
      request.workflowConfig.ui?.stateInteractions?.[request.state];
    if (configuredStateInteraction !== undefined) {
      return ok(configuredStateInteraction);
    }
  }

  const defaultInteraction = request.workflowConfig.ui?.defaultInteraction;
  if (defaultInteraction !== undefined) {
    return ok(defaultInteraction);
  }

  return err(
    validationFailedError({
      state: request.state,
      reason:
        "No interaction key, state interaction, or default interaction was configured.",
    }),
  );
}

function interactionFields(
  interaction: WorkflowInteractionConfig,
): WorkflowInteractionFieldPreview[] {
  const jsonSchema = isRecord(interaction["jsonSchema"])
    ? interaction["jsonSchema"]
    : undefined;
  const properties = isRecord(jsonSchema?.["properties"])
    ? jsonSchema["properties"]
    : {};
  const requiredFields = new Set(stringArray(jsonSchema?.["required"]));

  return Object.entries(properties).flatMap(([fieldName, fieldSchema]) => {
    return fieldPreviews({
      fieldName,
      fieldPath: fieldName,
      fieldSchema,
      requiredFields,
    });
  });
}

function fieldPreviews(input: {
  fieldName: string;
  fieldPath: string;
  fieldSchema: unknown;
  requiredFields: Set<string>;
}): WorkflowInteractionFieldPreview[] {
  if (!isRecord(input.fieldSchema)) {
    return [];
  }

  const propertySchemas = isRecord(input.fieldSchema["properties"])
    ? input.fieldSchema["properties"]
    : undefined;
  const requiredChildren = new Set(stringArray(input.fieldSchema["required"]));
  const field = buildFieldPreview({
    fieldName: input.fieldName,
    fieldPath: input.fieldPath,
    fieldSchema: input.fieldSchema,
    required: input.requiredFields.has(input.fieldName),
  });

  if (propertySchemas === undefined) {
    return [field];
  }

  return [
    field,
    ...Object.entries(propertySchemas).flatMap(([childFieldName, childSchema]) => {
      return fieldPreviews({
        fieldName: childFieldName,
        fieldPath: `${input.fieldPath}.${childFieldName}`,
        fieldSchema: childSchema,
        requiredFields: requiredChildren,
      });
    }),
  ];
}

function buildFieldPreview(input: {
  fieldName: string;
  fieldPath: string;
  fieldSchema: Record<string, unknown>;
  required: boolean;
}): WorkflowInteractionFieldPreview {
  const defaultValue = input.fieldSchema["default"];
  const displayCondition = input.fieldSchema["displayCondition"];

  return {
    path: input.fieldPath,
    label: humanizeLabel(input.fieldName),
    type: schemaType(input.fieldSchema["type"]),
    required: input.required,
    ...(defaultValue === undefined ? {} : { defaultValue }),
    ...(displayCondition === undefined ? {} : { displayCondition }),
    validationHints: validationHints(input.fieldSchema),
  };
}

function interactionSummarySections(input: {
  interaction: WorkflowInteractionConfig;
  actorFixture: WorkflowInteractionActorFixture | Record<string, unknown> | undefined;
  employeeFixture: Record<string, unknown> | undefined;
}): WorkflowInteractionSummarySectionPreview[] {
  const employeeContext = Array.isArray(input.interaction.employeeContext)
    ? input.interaction.employeeContext
    : [];

  return employeeContext.map((contextItem) => {
    const permissions = contextItem.visibility?.permissions ?? [];
    const isVisible =
      permissions.length === 0 || actorHasPermission(input.actorFixture, permissions);
    const isRedacted = !isVisible || isSensitiveField(contextItem.path);
    const value =
      input.employeeFixture === undefined
        ? undefined
        : valueAtDotPath(input.employeeFixture, contextItem.path);

    return {
      outputKey: contextItem.outputKey,
      sourcePath: contextItem.path,
      visible: isVisible,
      redacted: isRedacted,
      ...(value === undefined
        ? {}
        : { valuePreview: isRedacted ? "[redacted]" : value }),
      deniedReasons: isVisible
        ? []
        : permissions.map((permission) => {
            return `missing_permission:${permission}`;
          }),
    };
  });
}

function interactionWarnings(input: {
  interaction: WorkflowInteractionConfig;
  interactionKey: string;
  state: string | undefined;
  stateActions: WorkflowActionConfig[];
  fields: WorkflowInteractionFieldPreview[];
  approvalActions: WorkflowInteractionActionPreview[];
}): WorkflowInteractionPreviewWarning[] {
  const warnings: WorkflowInteractionPreviewWarning[] = [];
  const requiredInputFields = requiredJsonSchemaFields(input.interaction);

  for (const requiredInputField of requiredInputFields) {
    const fieldExists = input.fields.some((field) => {
      return (
        field.path === requiredInputField ||
        field.path.startsWith(`${requiredInputField}.`)
      );
    });
    if (!fieldExists) {
      warnings.push(
        warning({
          code: "workflow_interaction.required_field_missing",
          path: `interactions.${input.interactionKey}.jsonSchema.required`,
          message: `Required interaction field ${requiredInputField} is not represented in preview fields.`,
        }),
      );
    }
  }

  if (input.interaction.type === "waiting" && input.approvalActions.length === 0) {
    warnings.push(
      warning({
        code: "workflow_interaction.approval_actions_missing",
        path: input.state === undefined ? "states" : `states.${input.state}.actions`,
        message: "Approval interactions should expose approve/reject decision actions.",
      }),
    );
  }

  if (input.interaction.type === "repair" && requiredInputFields.length === 0) {
    warnings.push(
      warning({
        code: "workflow_interaction.repair_input_missing",
        path: `interactions.${input.interactionKey}.jsonSchema.required`,
        message: "Repair interactions should specify required repair input.",
      }),
    );
  }

  return warnings;
}

function actionPreview(action: WorkflowActionConfig): WorkflowInteractionActionPreview {
  return {
    transition: action.transition,
    label: action.label,
    actor: action.actor,
    handler: action.handler,
    ...(action.nextState === undefined ? {} : { nextState: action.nextState }),
    ...(action.nextInteraction === undefined
      ? {}
      : { nextInteraction: action.nextInteraction }),
  };
}

function requiredJsonSchemaFields(interaction: WorkflowInteractionConfig): string[] {
  const jsonSchema = isRecord(interaction["jsonSchema"])
    ? interaction["jsonSchema"]
    : undefined;

  return stringArray(jsonSchema?.["required"]);
}

function validationHints(
  fieldSchema: Record<string, unknown>,
): Record<string, unknown> {
  const hints: Record<string, unknown> = {};

  for (const key of [
    "enum",
    "format",
    "minLength",
    "maxLength",
    "minimum",
    "maximum",
  ]) {
    if (fieldSchema[key] !== undefined) {
      hints[key] = fieldSchema[key];
    }
  }

  return hints;
}

function actorHasPermission(
  actorFixture: WorkflowInteractionActorFixture | Record<string, unknown> | undefined,
  requiredPermissions: string[],
): boolean {
  if (actorFixture === undefined) {
    return false;
  }

  const actorPermissions = new Set(stringArray(actorFixture["permissions"]));

  return requiredPermissions.some((permission) => {
    return actorPermissions.has(permission);
  });
}

function schemaType(value: unknown): string {
  if (Array.isArray(value)) {
    return value.join(" | ");
  }

  return typeof value === "string" ? value : "object";
}

function humanizeLabel(fieldName: string): string {
  return fieldName
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (match) => {
      return match.toUpperCase();
    });
}

function isSensitiveField(path: string): boolean {
  const lowerPath = path.toLowerCase();

  return sensitiveFieldTokens.some((token) => {
    return lowerPath.includes(token);
  });
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }

  return value.filter((item): item is string => {
    return typeof item === "string" && item.trim().length > 0;
  });
}

function warning(input: {
  code: string;
  path: string;
  message: string;
}): WorkflowInteractionPreviewWarning {
  return {
    code: input.code,
    path: input.path,
    jsonPath: `$.${input.path}`,
    message: input.message,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
