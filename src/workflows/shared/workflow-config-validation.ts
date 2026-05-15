import {
  LEDGER_EVENT_TYPES,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
} from "@hcm-next/foundation";
import { workflowConfigHash } from "./workflow-config-hash.js";
import type { WorkflowConfig } from "./workflow-config.js";

export type WorkflowValidationSeverity = "error" | "warning";

export type WorkflowValidationIssue = {
  severity: WorkflowValidationSeverity;
  code: string;
  path: string;
  message: string;
};

export type WorkflowValidationReport = {
  valid: boolean;
  configHash: string;
  errors: WorkflowValidationIssue[];
  warnings: WorkflowValidationIssue[];
};

const requiredTopLevelFields = [
  "intent",
  "subjectType",
  "selfServiceStart",
  "interactions",
  "states",
  "submit",
  "approval",
  "plan",
  "projection",
  "timeline",
] as const;

const knownLedgerEventTypes = new Set<string>(Object.values(LEDGER_EVENT_TYPES));
const knownWorkflowStates = new Set<string>(Object.values(WORKFLOW_STATES));
const knownWorkflowStatuses = new Set<string>(Object.values(WORKFLOW_STATUSES));

/**
 * Validates an executable workflow config before it is published.
 */
export function validateWorkflowConfig(value: unknown): WorkflowValidationReport {
  const issues: WorkflowValidationIssue[] = [];
  const workflowConfigRecord = isRecord(value) ? value : undefined;

  if (workflowConfigRecord === undefined) {
    issues.push(
      issue({
        code: "workflow_config.shape_invalid",
        path: "$",
        message: "Workflow config must be a JSON object.",
      }),
    );

    return buildValidationReport(value, issues);
  }

  validateTopLevelFields(workflowConfigRecord, issues);

  const interactions = readRequiredRecord(
    workflowConfigRecord,
    "interactions",
    "interactions",
    issues,
  );
  const states = readRequiredRecord(workflowConfigRecord, "states", "states", issues);
  const timeline = readRequiredRecord(
    workflowConfigRecord,
    "timeline",
    "timeline",
    issues,
  );
  const projection = readRequiredRecord(
    workflowConfigRecord,
    "projection",
    "projection",
    issues,
  );

  readRequiredString(workflowConfigRecord, "intent", "intent", issues);
  readRequiredString(workflowConfigRecord, "subjectType", "subjectType", issues);
  readRequiredBoolean(
    workflowConfigRecord,
    "selfServiceStart",
    "selfServiceStart",
    issues,
  );
  readRequiredRecord(workflowConfigRecord, "submit", "submit", issues);
  readRequiredRecord(workflowConfigRecord, "approval", "approval", issues);
  readRequiredRecord(workflowConfigRecord, "plan", "plan", issues);

  validateInteractions(interactions, issues);
  validateStates(states, interactions, issues);
  validateProjection(projection, issues);
  validateGraph(workflowConfigRecord["graph"], states, interactions, issues);
  validateTimeline(timeline, issues);

  return buildValidationReport(value, issues);
}

function buildValidationReport(
  value: unknown,
  issues: WorkflowValidationIssue[],
): WorkflowValidationReport {
  const errors = issues.filter((validationIssue) => {
    return validationIssue.severity === "error";
  });
  const warnings = issues.filter((validationIssue) => {
    return validationIssue.severity === "warning";
  });

  return {
    valid: errors.length === 0,
    configHash: workflowConfigHash((isRecord(value) ? value : {}) as WorkflowConfig),
    errors,
    warnings,
  };
}

function validateTopLevelFields(
  workflowConfig: Record<string, unknown>,
  issues: WorkflowValidationIssue[],
): void {
  for (const fieldName of requiredTopLevelFields) {
    if (!hasOwnField(workflowConfig, fieldName)) {
      issues.push(
        issue({
          code: "workflow_config.required_field_missing",
          path: fieldName,
          message: `Workflow config is missing ${fieldName}.`,
        }),
      );
    }
  }
}

function validateInteractions(
  interactions: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (interactions === undefined) {
    return;
  }

  for (const [interactionKey, interaction] of Object.entries(interactions)) {
    const interactionPath = `interactions.${interactionKey}`;

    if (!isRecord(interaction)) {
      issues.push(
        issue({
          code: "workflow_config.interaction_invalid",
          path: interactionPath,
          message: "Workflow interaction must be an object.",
        }),
      );
      continue;
    }

    readRequiredString(interaction, "type", `${interactionPath}.type`, issues);
    readRequiredString(interaction, "title", `${interactionPath}.title`, issues);
  }
}

function validateStates(
  states: Record<string, unknown> | undefined,
  interactions: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (states === undefined) {
    return;
  }

  const stateKeys = workflowStateReferenceSet(states);
  const interactionKeys = new Set(Object.keys(interactions ?? {}));

  for (const [stateKey, stateConfig] of Object.entries(states)) {
    const statePath = `states.${stateKey}`;

    if (!isRecord(stateConfig)) {
      issues.push(
        issue({
          code: "workflow_config.state_invalid",
          path: statePath,
          message: "Workflow state must be an object.",
        }),
      );
      continue;
    }

    const actions = stateConfig["actions"];
    if (!Array.isArray(actions)) {
      issues.push(
        issue({
          code: "workflow_config.state_actions_invalid",
          path: `${statePath}.actions`,
          message: "Workflow state actions must be an array.",
        }),
      );
      continue;
    }

    actions.forEach((actionConfig, actionIndex) => {
      validateStateAction(
        actionConfig,
        `${statePath}.actions.${actionIndex}`,
        stateKeys,
        interactionKeys,
        issues,
      );
    });
  }
}

function validateStateAction(
  actionConfig: unknown,
  actionPath: string,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(actionConfig)) {
    issues.push(
      issue({
        code: "workflow_config.action_invalid",
        path: actionPath,
        message: "Workflow action must be an object.",
      }),
    );
    return;
  }

  readRequiredString(actionConfig, "transition", `${actionPath}.transition`, issues);
  readRequiredString(actionConfig, "label", `${actionPath}.label`, issues);
  readRequiredString(actionConfig, "actor", `${actionPath}.actor`, issues);
  readRequiredString(actionConfig, "handler", `${actionPath}.handler`, issues);
  validateOptionalReference(actionConfig, "nextState", actionPath, stateKeys, {
    code: "workflow_config.action_next_state_missing",
    label: "state",
    issues,
  });
  validateOptionalReference(
    actionConfig,
    "nextInteraction",
    actionPath,
    interactionKeys,
    {
      code: "workflow_config.action_next_interaction_missing",
      label: "interaction",
      issues,
    },
  );
  validateOptionalReference(
    actionConfig,
    "nextStatus",
    actionPath,
    knownWorkflowStatuses,
    {
      code: "workflow_config.action_next_status_unknown",
      label: "workflow status",
      issues,
    },
  );
}

function validateProjection(
  projection: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (projection === undefined) {
    return;
  }

  readStringArray(
    projection["allowedPatchPaths"],
    "projection.allowedPatchPaths",
    issues,
  );
}

function validateGraph(
  graph: unknown,
  states: Record<string, unknown> | undefined,
  interactions: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (graph === undefined) {
    issues.push(
      issue({
        severity: "warning",
        code: "workflow_config.graph_missing",
        path: "graph",
        message: "Workflow config does not define a graph.",
      }),
    );
    return;
  }

  if (!isRecord(graph)) {
    issues.push(
      issue({
        code: "workflow_config.graph_invalid",
        path: "graph",
        message: "Workflow graph must be an object.",
      }),
    );
    return;
  }

  const stateKeys = workflowStateReferenceSet(states);
  const interactionKeys = new Set(Object.keys(interactions ?? {}));
  const startNodeId = readRequiredString(
    graph,
    "startNodeId",
    "graph.startNodeId",
    issues,
  );
  const nodes = graph["nodes"];

  if (!Array.isArray(nodes)) {
    issues.push(
      issue({
        code: "workflow_config.graph_nodes_invalid",
        path: "graph.nodes",
        message: "Workflow graph nodes must be an array.",
      }),
    );
    return;
  }

  const nodeIds = collectGraphNodeIds(nodes, issues);

  if (startNodeId !== undefined && !nodeIds.has(startNodeId)) {
    issues.push(
      issue({
        code: "workflow_config.graph_start_node_missing",
        path: "graph.startNodeId",
        message: `Graph start node ${startNodeId} is not defined.`,
      }),
    );
  }

  nodes.forEach((node, nodeIndex) => {
    validateGraphNode(
      node,
      `graph.nodes.${nodeIndex}`,
      nodeIds,
      stateKeys,
      interactionKeys,
      issues,
    );
  });
}

function collectGraphNodeIds(
  nodes: unknown[],
  issues: WorkflowValidationIssue[],
): Set<string> {
  const nodeIds = new Set<string>();

  nodes.forEach((node, nodeIndex) => {
    const nodePath = `graph.nodes.${nodeIndex}`;

    if (!isRecord(node)) {
      issues.push(
        issue({
          code: "workflow_config.graph_node_invalid",
          path: nodePath,
          message: "Workflow graph node must be an object.",
        }),
      );
      return;
    }

    const nodeId = readRequiredString(node, "nodeId", `${nodePath}.nodeId`, issues);
    if (nodeId === undefined) {
      return;
    }

    if (nodeIds.has(nodeId)) {
      issues.push(
        issue({
          code: "workflow_config.graph_duplicate_node_id",
          path: `${nodePath}.nodeId`,
          message: `Graph node ID ${nodeId} is duplicated.`,
        }),
      );
    }

    nodeIds.add(nodeId);
  });

  return nodeIds;
}

function validateGraphNode(
  node: unknown,
  nodePath: string,
  nodeIds: Set<string>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(node)) {
    return;
  }

  readRequiredString(node, "type", `${nodePath}.type`, issues);
  readRequiredString(node, "title", `${nodePath}.title`, issues);
  validateOptionalReference(node, "state", nodePath, stateKeys, {
    code: "workflow_config.node_state_missing",
    label: "state",
    issues,
  });
  validateOptionalReference(node, "interaction", nodePath, interactionKeys, {
    code: "workflow_config.node_interaction_missing",
    label: "interaction",
    issues,
  });

  const outcomes = node["outcomes"];
  if (outcomes === undefined) {
    return;
  }

  if (!Array.isArray(outcomes)) {
    issues.push(
      issue({
        code: "workflow_config.node_outcomes_invalid",
        path: `${nodePath}.outcomes`,
        message: "Workflow graph outcomes must be an array.",
      }),
    );
    return;
  }

  outcomes.forEach((outcome, outcomeIndex) => {
    validateGraphOutcome(
      outcome,
      `${nodePath}.outcomes.${outcomeIndex}`,
      nodeIds,
      stateKeys,
      interactionKeys,
      issues,
    );
  });
}

function validateGraphOutcome(
  outcome: unknown,
  outcomePath: string,
  nodeIds: Set<string>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(outcome)) {
    issues.push(
      issue({
        code: "workflow_config.outcome_invalid",
        path: outcomePath,
        message: "Workflow graph outcome must be an object.",
      }),
    );
    return;
  }

  readRequiredString(outcome, "outcome", `${outcomePath}.outcome`, issues);
  validateOptionalReference(outcome, "nextNodeId", outcomePath, nodeIds, {
    code: "workflow_config.outcome_next_node_missing",
    label: "graph node",
    issues,
  });
  validateOptionalReference(outcome, "nextState", outcomePath, stateKeys, {
    code: "workflow_config.outcome_next_state_missing",
    label: "state",
    issues,
  });
  validateOptionalReference(outcome, "nextStatus", outcomePath, knownWorkflowStatuses, {
    code: "workflow_config.outcome_next_status_unknown",
    label: "workflow status",
    issues,
  });
  validateOptionalReference(outcome, "nextInteraction", outcomePath, interactionKeys, {
    code: "workflow_config.outcome_next_interaction_missing",
    label: "interaction",
    issues,
  });

  const eventType = outcome["eventType"];
  if (eventType !== undefined) {
    if (typeof eventType !== "string" || !knownLedgerEventTypes.has(eventType)) {
      issues.push(
        issue({
          code: "workflow_config.outcome_event_type_unknown",
          path: `${outcomePath}.eventType`,
          message: `Outcome references unknown ledger event ${String(eventType)}.`,
        }),
      );
    }
  }
}

function workflowStateReferenceSet(
  states: Record<string, unknown> | undefined,
): Set<string> {
  return new Set([...knownWorkflowStates, ...Object.keys(states ?? {})]);
}

function validateTimeline(
  timeline: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (timeline === undefined) {
    return;
  }

  const businessEvents = readStringArray(
    timeline["businessEvents"],
    "timeline.businessEvents",
    issues,
  );
  const summaries = readRequiredRecord(
    timeline,
    "summaries",
    "timeline.summaries",
    issues,
  );

  if (businessEvents === undefined || summaries === undefined) {
    return;
  }

  businessEvents.forEach((eventType, eventIndex) => {
    const summary = summaries[eventType];

    if (typeof summary !== "string" || summary.trim() === "") {
      issues.push(
        issue({
          code: "workflow_config.timeline_summary_missing",
          path: `timeline.businessEvents.${eventIndex}`,
          message: `Business event ${eventType} needs a timeline summary.`,
        }),
      );
    }
  });
}

function validateOptionalReference(
  record: Record<string, unknown>,
  field: string,
  ownerPath: string,
  validValues: Set<string>,
  input: {
    code: string;
    label: string;
    issues: WorkflowValidationIssue[];
  },
): void {
  const value = record[field];

  if (value === undefined) {
    return;
  }

  const path = `${ownerPath}.${field}`;

  if (typeof value !== "string" || value.trim().length === 0) {
    input.issues.push(
      issue({
        code: "workflow_config.reference_invalid",
        path,
        message: `Workflow reference ${path} must be a non-empty string.`,
      }),
    );
    return;
  }

  if (!validValues.has(value)) {
    input.issues.push(
      issue({
        code: input.code,
        path,
        message: `Reference ${value} does not match a configured ${input.label}.`,
      }),
    );
  }
}

function readRequiredRecord(
  record: Record<string, unknown>,
  field: string,
  path: string,
  issues: WorkflowValidationIssue[],
): Record<string, unknown> | undefined {
  if (!hasOwnField(record, field)) {
    return undefined;
  }

  const value = record[field];
  if (!isRecord(value)) {
    issues.push(
      issue({
        code: "workflow_config.field_type_invalid",
        path,
        message: `Workflow config field ${path} must be an object.`,
      }),
    );
    return undefined;
  }

  return value;
}

function readRequiredString(
  record: Record<string, unknown>,
  field: string,
  path: string,
  issues: WorkflowValidationIssue[],
): string | undefined {
  if (!hasOwnField(record, field)) {
    return undefined;
  }

  const value = record[field];
  if (typeof value !== "string" || value.trim().length === 0) {
    issues.push(
      issue({
        code: "workflow_config.field_type_invalid",
        path,
        message: `Workflow config field ${path} must be a non-empty string.`,
      }),
    );
    return undefined;
  }

  return value;
}

function readRequiredBoolean(
  record: Record<string, unknown>,
  field: string,
  path: string,
  issues: WorkflowValidationIssue[],
): boolean | undefined {
  if (!hasOwnField(record, field)) {
    return undefined;
  }

  const value = record[field];
  if (typeof value !== "boolean") {
    issues.push(
      issue({
        code: "workflow_config.field_type_invalid",
        path,
        message: `Workflow config field ${path} must be a boolean.`,
      }),
    );
    return undefined;
  }

  return value;
}

function readStringArray(
  value: unknown,
  path: string,
  issues: WorkflowValidationIssue[],
): string[] | undefined {
  if (!Array.isArray(value)) {
    issues.push(
      issue({
        code: "workflow_config.field_type_invalid",
        path,
        message: `Workflow config field ${path} must be an array of strings.`,
      }),
    );
    return undefined;
  }

  const stringValues: string[] = [];

  value.forEach((item, itemIndex) => {
    if (typeof item !== "string" || item.trim().length === 0) {
      issues.push(
        issue({
          code: "workflow_config.field_type_invalid",
          path: `${path}.${itemIndex}`,
          message: `Workflow config field ${path}.${itemIndex} must be a non-empty string.`,
        }),
      );
      return;
    }

    stringValues.push(item);
  });

  return stringValues;
}

function issue(input: {
  severity?: WorkflowValidationSeverity;
  code: string;
  path: string;
  message: string;
}): WorkflowValidationIssue {
  return {
    severity: input.severity ?? "error",
    code: input.code,
    path: input.path,
    message: input.message,
  };
}

function hasOwnField(record: Record<string, unknown>, field: string): boolean {
  return Object.prototype.hasOwnProperty.call(record, field);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
