import {
  err,
  notFoundError,
  ok,
  validationFailedError,
  type AppError,
  type ChangeRequestStatus,
  type Result,
  type WorkflowState,
  type WorkflowStatus,
} from "@hcm-next/foundation";
import type { EmployeeProjectionDocument } from "@hcm-next/data-store";
import legalNameWorkflowConfigJson from "../configs/employee-legal-name-change.workflow.json";
import emergencyContactWorkflowConfigJson from "../configs/employee-emergency-contact-update.workflow.json";
import contactInfoWorkflowConfigJson from "../configs/employee-contact-info-update.workflow.json";
import compensationWorkflowConfigJson from "../configs/employee-compensation-change.workflow.json";
import orgTransferCompensationChangeWorkflowConfigJson from "../configs/employee-org-transfer-compensation-change.workflow.json";

export type WorkflowActionActor =
  | "requester"
  | "initiator"
  | "hr_admin"
  | "source_manager"
  | "destination_manager"
  | "finance_admin"
  | "compensation_admin"
  | "medical_director"
  | "clinic_ops_admin"
  | "org_transfer_approver"
  | "hr_admin_or_system";

export type WorkflowStartActor = "hr_admin" | "compensation_admin";

export type WorkflowActionHandler =
  | "submit_configured_input"
  | "provide_evidence"
  | "approve"
  | "reject"
  | "request_more_info"
  | "cancel"
  | "execute";

export type WorkflowBlockReference = {
  name: string;
  version: string;
};

export type WorkflowValueExpression = {
  $source:
    | "input"
    | "employee"
    | "workflow"
    | "changeRequest"
    | "proposedChange"
    | "approvalTask"
    | "transactionPlan"
    | "externalWriteResponse"
    | "actor"
    | "relationshipGraph"
    | "tenantPolicy";
  path: string;
};

export type WorkflowOutcomeConditionSource =
  | "externalWriteResponse"
  | "externalWriteError";

export type WorkflowOutcomeCondition = {
  $source: WorkflowOutcomeConditionSource;
  path: string;
  equals?: unknown;
  in?: unknown[];
  exists?: boolean;
};

export type WorkflowInteractionConfig = Record<string, unknown> & {
  type: string;
  title: string;
  employeeContext?: Array<{
    outputKey: string;
    path: string;
    source?: "employee" | "workerAssignments" | "roleBindings" | "workflow";
    visibility?: {
      fieldGroups?: string[];
      permissions?: string[];
      actors?: WorkflowActionActor[];
    };
  }>;
};

export type WorkflowActionConfig = {
  transition: string;
  label: string;
  actor: WorkflowActionActor;
  handler: WorkflowActionHandler;
  nextState?: WorkflowState;
  nextStatus?: WorkflowStatus;
  nextInteraction?: string;
};

export type WorkflowSubmitConfig = {
  preflightBlock: WorkflowBlockReference;
  preflightInput: Record<string, unknown>;
  effectiveAt: WorkflowValueExpression;
  businessReason: WorkflowValueExpression;
  changeRequestStatus: ChangeRequestStatus;
  markSubmitted?: boolean;
  createApprovalTask?: boolean;
  currentSnapshot: Record<string, unknown>;
  proposedSnapshot: Record<string, unknown>;
  proposedChange: {
    targetObjectType: string;
    targetObjectId: WorkflowValueExpression;
    fieldPath?: string;
    fieldPathTemplate?: string;
    currentValue: WorkflowValueExpression;
    proposedValue: WorkflowValueExpression;
  };
  additionalEvents: string[];
};

export type WorkflowApprovalConfig = {
  assigneeActorId: string;
  assigneeRole: string;
  approvalType: string;
};

export type WorkflowPlanConfig = {
  block: WorkflowBlockReference;
  input: Record<string, unknown>;
};

export type WorkflowGraphOutcomeConfig = {
  outcome: string;
  when?: WorkflowOutcomeCondition;
  eventType?: string;
  nextNodeId?: string;
  nextState?: WorkflowState;
  nextStatus?: WorkflowStatus;
  nextInteraction?: string;
};

export type WorkflowGraphNodeConfig = {
  nodeId: string;
  type:
    | "interaction"
    | "block"
    | "policy_check"
    | "approval"
    | "transaction_plan"
    | "data_write"
    | "external_write"
    | "projection_write"
    | "manual_repair"
    | "terminal";
  title: string;
  state?: WorkflowState;
  interaction?: string;
  block?: WorkflowBlockReference;
  connectionId?: string;
  operation?: string;
  approval?: Record<string, unknown>;
  policy?: Record<string, unknown>;
  transaction?: Record<string, unknown>;
  outcomes?: WorkflowGraphOutcomeConfig[];
};

export type WorkflowGraphConfig = {
  startNodeId: string;
  nodes: WorkflowGraphNodeConfig[];
};

export type WorkflowConfig = {
  intent: string;
  subjectType: string;
  selfServiceStart: boolean;
  startActors?: WorkflowStartActor[];
  interactions: Record<string, WorkflowInteractionConfig>;
  states: Record<string, { actions: WorkflowActionConfig[] }>;
  submit: WorkflowSubmitConfig;
  approval: WorkflowApprovalConfig;
  plan: WorkflowPlanConfig;
  graph?: WorkflowGraphConfig;
  projection: {
    allowedPatchPaths: string[];
  };
  timeline: {
    businessEvents: string[];
    summaries: Record<string, string>;
  };
};

export type WorkflowTemplateSources = {
  input?: Record<string, unknown>;
  employee?: EmployeeProjectionDocument;
  workflow?: Record<string, unknown>;
  changeRequest?: Record<string, unknown>;
  proposedChange?: Record<string, unknown>;
  approvalTask?: Record<string, unknown>;
  transactionPlan?: Record<string, unknown>;
  externalWriteResponse?: Record<string, unknown>;
  actor?: Record<string, unknown>;
  relationshipGraph?: Record<string, unknown>;
  tenantPolicy?: Record<string, unknown>;
};

const workflowConfigs = [
  legalNameWorkflowConfigJson,
  emergencyContactWorkflowConfigJson,
  contactInfoWorkflowConfigJson,
  compensationWorkflowConfigJson,
  orgTransferCompensationChangeWorkflowConfigJson,
] as unknown as WorkflowConfig[];

/**
 * Finds a configured workflow by public intent.
 */
export function getWorkflowConfigByIntent(
  intent: string,
): Result<WorkflowConfig, AppError> {
  const workflowConfig = workflowConfigs.find((config) => {
    return config.intent === intent;
  });

  if (workflowConfig === undefined) {
    return err(notFoundError("Workflow config", { intent }));
  }

  return ok(workflowConfig);
}

/**
 * Lists configured intents so validation errors can remain explicit.
 */
export function configuredWorkflowIntents(): string[] {
  return workflowConfigs.map((config) => config.intent);
}

/**
 * Returns the transition configuration available from a state.
 */
export function findWorkflowActionConfig(input: {
  workflowConfig: WorkflowConfig;
  state: string;
  transition: string;
}): Result<WorkflowActionConfig, AppError> {
  const stateConfig = input.workflowConfig.states[input.state];
  const actionConfig = stateConfig?.actions.find((action) => {
    return action.transition === input.transition;
  });

  if (actionConfig === undefined) {
    return err(
      validationFailedError({
        intent: input.workflowConfig.intent,
        state: input.state,
        transition: input.transition,
      }),
    );
  }

  return ok(actionConfig);
}

/**
 * Builds a configured interaction and injects read-only employee context values.
 */
export function buildConfiguredInteraction(input: {
  workflowConfig: WorkflowConfig;
  interactionKey: string;
  employeeDocument?: EmployeeProjectionDocument;
}): Result<Record<string, unknown>, AppError> {
  const interactionConfig = input.workflowConfig.interactions[input.interactionKey];

  if (interactionConfig === undefined) {
    return err(
      validationFailedError({
        intent: input.workflowConfig.intent,
        interactionKey: input.interactionKey,
      }),
    );
  }

  const { employeeContext, ...interaction } = interactionConfig;
  const interactionRecord = cloneJsonRecord(interaction);

  if (employeeContext === undefined) {
    return ok(interactionRecord);
  }

  if (input.employeeDocument === undefined) {
    return err(
      validationFailedError({
        interactionKey: input.interactionKey,
        employeeDocument: "missing",
      }),
    );
  }

  for (const contextMapping of employeeContext) {
    interactionRecord[contextMapping.outputKey] = valueAtPath(
      input.employeeDocument,
      contextMapping.path,
    );
  }

  return ok(interactionRecord);
}

/**
 * Resolves a JSON-like template object against workflow source objects.
 */
export function resolveWorkflowTemplate(
  template: unknown,
  sources: WorkflowTemplateSources,
): Result<unknown, AppError> {
  if (isWorkflowValueExpression(template)) {
    return ok(valueFromExpression(template, sources));
  }

  if (Array.isArray(template)) {
    const values: unknown[] = [];

    for (const item of template) {
      const valueResult = resolveWorkflowTemplate(item, sources);
      if (!valueResult.ok) {
        return valueResult;
      }

      values.push(valueResult.value);
    }

    return ok(values);
  }

  if (typeof template === "object" && template !== null) {
    const resolvedRecord: Record<string, unknown> = {};

    for (const [key, value] of Object.entries(template)) {
      const valueResult = resolveWorkflowTemplate(value, sources);
      if (!valueResult.ok) {
        return valueResult;
      }

      resolvedRecord[key] = valueResult.value;
    }

    return ok(resolvedRecord);
  }

  return ok(template);
}

/**
 * Resolves a configured value expression and verifies it is a non-empty string.
 */
export function resolveWorkflowString(
  expression: WorkflowValueExpression,
  sources: WorkflowTemplateSources,
): Result<string, AppError> {
  const value = valueFromExpression(expression, sources);

  if (typeof value !== "string" || value.trim().length === 0) {
    return err(validationFailedError({ expression, value }));
  }

  return ok(value.trim());
}

/**
 * Resolves a field path template such as emergencyContacts.${contactId}.
 */
export function renderWorkflowTemplateString(
  template: string,
  input: Record<string, unknown>,
): string {
  return template.replace(/\$\{([^}]+)\}/g, (_match, path: string) => {
    const value = valueAtPath(input, path);
    return value === undefined || value === null ? "" : String(value);
  });
}

function isWorkflowValueExpression(value: unknown): value is WorkflowValueExpression {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }

  const record = value as Record<string, unknown>;
  return typeof record["$source"] === "string" && typeof record["path"] === "string";
}

function valueFromExpression(
  expression: WorkflowValueExpression,
  sources: WorkflowTemplateSources,
): unknown {
  const source = sources[expression.$source];

  if (source === undefined) {
    return undefined;
  }

  return valueAtPath(source, expression.path);
}

function valueAtPath(source: unknown, path: string): unknown {
  const pathSegments = path.split(".").filter((segment) => segment.length > 0);
  let currentValue = source;

  for (const pathSegment of pathSegments) {
    if (typeof currentValue !== "object" || currentValue === null) {
      return undefined;
    }

    currentValue = (currentValue as Record<string, unknown>)[pathSegment];
  }

  return currentValue;
}

function cloneJsonRecord(record: Record<string, unknown>): Record<string, unknown> {
  return JSON.parse(JSON.stringify(record)) as Record<string, unknown>;
}
