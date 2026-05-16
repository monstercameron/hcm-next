import {
  LEDGER_EVENT_TYPES,
  PERMISSION_KEYS,
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
  jsonPath: string;
  message: string;
};

export type WorkflowValidationReport = {
  valid: boolean;
  configHash: string;
  errors: WorkflowValidationIssue[];
  warnings: WorkflowValidationIssue[];
};

const requiredTopLevelFields = [
  "schemaVersion",
  "intent",
  "subjectType",
  "selfServiceStart",
  "interactions",
  "states",
  "graph",
  "submit",
  "approval",
  "plan",
  "ledger",
  "projection",
  "timeline",
] as const;

export const SUPPORTED_WORKFLOW_SCHEMA_VERSIONS = ["v0.3", "v0.4"] as const;

const knownLedgerEventTypes = new Set<string>(Object.values(LEDGER_EVENT_TYPES));
const knownPermissionKeys = new Set<string>(Object.values(PERMISSION_KEYS));
const knownWorkflowStates = new Set<string>(Object.values(WORKFLOW_STATES));
const knownWorkflowStatuses = new Set<string>(Object.values(WORKFLOW_STATUSES));
const knownGraphNodeTypes = new Set([
  "interaction",
  "block",
  "policy_check",
  "approval",
  "approval_gate",
  "transaction_plan",
  "data_write",
  "external_write",
  "projection_write",
  "ledger_event",
  "manual_repair",
  "ai_review",
  "terminal",
]);
const sideEffectingNodeTypes = new Set([
  "data_write",
  "external_write",
  "projection_write",
]);
const knownSagaModes = new Set(["orchestrated", "choreographed"]);
const knownSagaBoundaries = new Set([
  "workflow_instance",
  "change_request",
  "transaction_plan",
  "node",
]);
const knownSagaFailurePolicies = new Set([
  "fail_fast",
  "retry_then_repair",
  "compensate_then_repair",
  "compensate_then_fail",
]);
const knownIdempotencyScopes = new Set([
  "workflow_instance",
  "transaction_plan",
  "node",
  "external_operation",
]);
const knownCompensationOrders = new Set([
  "reverse_completed_steps",
  "configured_order",
]);
const knownReversibilityValues = new Set([
  "none",
  "retryable",
  "reversible",
  "compensatable",
  "manual_repair",
  "irreversible",
]);
const knownFailurePolicies = new Set([
  "fail_fast",
  "retry",
  "retry_then_repair",
  "retry_then_compensate",
  "compensate_then_repair",
  "compensate_then_fail",
  "manual_repair",
  "ignore_with_warning",
]);
const knownRetryBackoffs = new Set(["none", "fixed", "linear", "exponential"]);
const knownDuplicateBehaviors = new Set([
  "read_existing_result",
  "reject_duplicate",
  "allow_if_same_payload",
  "manual_review_if_payload_differs",
]);
const knownLedgerEventSources = new Set([
  "runtime",
  "node",
  "block",
  "approval_gate",
  "integration",
]);
const knownApprovalGateFailurePolicyTypes = new Set([
  "stop_workflow",
  "send_to_repair",
  "continue_until_threshold_impossible",
  "require_all_responses",
  "veto_only",
  "escalate_on_timeout",
]);

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
  validateAdminMetadataFields(workflowConfigRecord, issues);

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
  const metadata = readOptionalRecord(
    workflowConfigRecord,
    "metadata",
    "metadata",
    issues,
  );
  const ui = readOptionalRecord(workflowConfigRecord, "ui", "ui", issues);
  const saga = readOptionalRecord(workflowConfigRecord, "saga", "saga", issues);
  const ledger = readOptionalRecord(workflowConfigRecord, "ledger", "ledger", issues);
  const submit = readRequiredRecord(workflowConfigRecord, "submit", "submit", issues);
  const approval = readRequiredRecord(
    workflowConfigRecord,
    "approval",
    "approval",
    issues,
  );
  const plan = readRequiredRecord(workflowConfigRecord, "plan", "plan", issues);

  readRequiredString(workflowConfigRecord, "intent", "intent", issues);
  readRequiredString(workflowConfigRecord, "subjectType", "subjectType", issues);
  readOptionalString(workflowConfigRecord, "schemaVersion", "schemaVersion", issues);
  readRequiredBoolean(
    workflowConfigRecord,
    "selfServiceStart",
    "selfServiceStart",
    issues,
  );

  validateMetadata(metadata, issues);
  validateSubmit(submit, issues);
  validateApproval(approval, issues);
  validatePlan(plan, issues);
  validateInteractions(interactions, issues);
  validateStates(states, interactions, issues);
  validateUi(ui, states, interactions, issues);
  validateProjection(projection, issues);
  const graphReferences = validateGraph(
    workflowConfigRecord["graph"],
    states,
    interactions,
    approval,
    issues,
  );
  validateSaga(saga, issues);
  validateLedger(ledger, graphReferences.nodeIds, issues);
  validateTimeline(timeline, issues);
  validateConfigLintRules(workflowConfigRecord, issues);

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

function validateAdminMetadataFields(
  workflowConfig: Record<string, unknown>,
  issues: WorkflowValidationIssue[],
): void {
  const schemaVersion = readRequiredString(
    workflowConfig,
    "schemaVersion",
    "schemaVersion",
    issues,
  );
  if (
    schemaVersion !== undefined &&
    !(SUPPORTED_WORKFLOW_SCHEMA_VERSIONS as readonly string[]).includes(schemaVersion)
  ) {
    issues.push(
      issue({
        code: "workflow_config.schema_version_unsupported",
        path: "schemaVersion",
        message: `Workflow schema version ${schemaVersion} is not supported.`,
      }),
    );
  }

  const metadata = isRecord(workflowConfig["metadata"])
    ? workflowConfig["metadata"]
    : undefined;
  const topLevelName = stringValue(workflowConfig["name"]);
  const metadataTitle =
    metadata === undefined ? undefined : stringValue(metadata["title"]);
  const topLevelDescription = stringValue(workflowConfig["description"]);
  const metadataDescription =
    metadata === undefined ? undefined : stringValue(metadata["description"]);

  if (topLevelName === undefined && metadataTitle === undefined) {
    issues.push(
      issue({
        severity: "warning",
        code: "workflow_config.name_missing",
        path: "name",
        message:
          "Workflow config should define name or metadata.title for admin lists.",
      }),
    );
  }

  if (topLevelDescription === undefined && metadataDescription === undefined) {
    issues.push(
      issue({
        severity: "warning",
        code: "workflow_config.description_missing",
        path: "description",
        message:
          "Workflow config should define description or metadata.description for admin review.",
      }),
    );
  }

  if (!isRecord(workflowConfig["permissions"])) {
    issues.push(
      issue({
        severity: "warning",
        code: "workflow_config.permissions_section_missing",
        path: "permissions",
        message:
          "Workflow config does not define an admin permissions section; referenced permissions will still be validated.",
      }),
    );
  }
}

function validateMetadata(
  metadata: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (metadata === undefined) {
    return;
  }

  readOptionalString(metadata, "schemaVersion", "metadata.schemaVersion", issues);
  readOptionalString(metadata, "title", "metadata.title", issues);
  readOptionalString(metadata, "description", "metadata.description", issues);
  readOptionalString(metadata, "domain", "metadata.domain", issues);
  readOptionalString(metadata, "owner", "metadata.owner", issues);

  if (metadata["tags"] !== undefined) {
    readStringArray(metadata["tags"], "metadata.tags", issues);
  }

  const deterministicBlockRefs = readOptionalRecord(
    metadata,
    "deterministicBlockRefs",
    "metadata.deterministicBlockRefs",
    issues,
  );
  if (deterministicBlockRefs === undefined) {
    return;
  }

  for (const [blockKey, blockReference] of Object.entries(deterministicBlockRefs)) {
    validateBlockReference(
      blockReference,
      `metadata.deterministicBlockRefs.${blockKey}`,
      issues,
    );
  }
}

function validateSubmit(
  submit: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (submit === undefined) {
    return;
  }

  validateBlockReference(submit["preflightBlock"], "submit.preflightBlock", issues);

  const additionalEvents = submit["additionalEvents"];
  if (additionalEvents === undefined) {
    return;
  }

  const eventTypes = readStringArray(
    additionalEvents,
    "submit.additionalEvents",
    issues,
  );
  for (const [eventIndex, eventType] of (eventTypes ?? []).entries()) {
    validateKnownLedgerEventType(
      eventType,
      `submit.additionalEvents.${eventIndex}`,
      issues,
    );
  }
}

function validateApproval(
  approval: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (approval === undefined) {
    return;
  }

  const assigneeActorId = approval["assigneeActorId"];
  if (assigneeActorId !== undefined && typeof assigneeActorId !== "string") {
    issues.push(
      issue({
        code: "workflow_config.field_type_invalid",
        path: "approval.assigneeActorId",
        message: "Workflow config field approval.assigneeActorId must be a string.",
      }),
    );
  }
  readRequiredString(approval, "assigneeRole", "approval.assigneeRole", issues);
  readRequiredString(approval, "approvalType", "approval.approvalType", issues);
}

function validatePlan(
  plan: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (plan === undefined) {
    return;
  }

  validateBlockReference(plan["block"], "plan.block", issues);
  readRequiredRecord(plan, "input", "plan.input", issues);
}

function validateUi(
  ui: Record<string, unknown> | undefined,
  states: Record<string, unknown> | undefined,
  interactions: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (ui === undefined) {
    return;
  }

  const stateKeys = workflowStateReferenceSet(states);
  const interactionKeys = new Set(Object.keys(interactions ?? {}));

  validateOptionalReference(ui, "defaultInteraction", "ui", interactionKeys, {
    code: "workflow_config.ui_default_interaction_missing",
    label: "interaction",
    issues,
  });

  const stateInteractions = readOptionalRecord(
    ui,
    "stateInteractions",
    "ui.stateInteractions",
    issues,
  );
  if (stateInteractions !== undefined) {
    for (const [state, interaction] of Object.entries(stateInteractions)) {
      if (!stateKeys.has(state)) {
        issues.push(
          issue({
            code: "workflow_config.ui_state_unknown",
            path: `ui.stateInteractions.${state}`,
            message: `UI state interaction ${state} does not match a workflow state.`,
          }),
        );
      }

      if (typeof interaction !== "string" || !interactionKeys.has(interaction)) {
        issues.push(
          issue({
            code: "workflow_config.ui_state_interaction_missing",
            path: `ui.stateInteractions.${state}`,
            message: `UI state interaction ${String(interaction)} is not configured.`,
          }),
        );
      }
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
    validateInteractionEmployeeContext(interaction, interactionPath, issues);
  }
}

function validateInteractionEmployeeContext(
  interaction: Record<string, unknown>,
  interactionPath: string,
  issues: WorkflowValidationIssue[],
): void {
  const employeeContext = interaction["employeeContext"];
  if (employeeContext === undefined) {
    return;
  }

  if (!Array.isArray(employeeContext)) {
    issues.push(
      issue({
        code: "workflow_config.interaction_employee_context_invalid",
        path: `${interactionPath}.employeeContext`,
        message: "Workflow interaction employeeContext must be an array.",
      }),
    );
    return;
  }

  employeeContext.forEach((contextItem, contextIndex) => {
    const contextPath = `${interactionPath}.employeeContext.${contextIndex}`;
    if (!isRecord(contextItem)) {
      issues.push(
        issue({
          code: "workflow_config.interaction_employee_context_item_invalid",
          path: contextPath,
          message: "Workflow interaction employeeContext entries must be objects.",
        }),
      );
      return;
    }

    readRequiredString(contextItem, "outputKey", `${contextPath}.outputKey`, issues);
    readRequiredString(contextItem, "path", `${contextPath}.path`, issues);

    const visibility = readOptionalRecord(
      contextItem,
      "visibility",
      `${contextPath}.visibility`,
      issues,
    );
    validatePermissionArray(
      visibility?.["permissions"],
      `${contextPath}.visibility.permissions`,
      issues,
    );
  });
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
  topLevelApproval: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): { nodeIds: Set<string> } {
  if (graph === undefined) {
    issues.push(
      issue({
        severity: "warning",
        code: "workflow_config.graph_missing",
        path: "graph",
        message: "Workflow config does not define a graph.",
      }),
    );
    return { nodeIds: new Set() };
  }

  if (!isRecord(graph)) {
    issues.push(
      issue({
        code: "workflow_config.graph_invalid",
        path: "graph",
        message: "Workflow graph must be an object.",
      }),
    );
    return { nodeIds: new Set() };
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
    return { nodeIds: new Set() };
  }

  const nodeIds = collectGraphNodeIds(nodes, issues);
  const outcomeRouteKeysByNodeId = new Map<string, Set<string>>();

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
      topLevelApproval,
      outcomeRouteKeysByNodeId,
      issues,
    );
  });

  validateGraphEdges(
    graph["edges"],
    nodeIds,
    outcomeRouteKeysByNodeId,
    stateKeys,
    interactionKeys,
    issues,
  );

  return { nodeIds };
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
  topLevelApproval: Record<string, unknown> | undefined,
  outcomeRouteKeysByNodeId: Map<string, Set<string>>,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(node)) {
    return;
  }

  const nodeId = readRequiredString(node, "nodeId", `${nodePath}.nodeId`, issues);
  const nodeType = readRequiredString(node, "type", `${nodePath}.type`, issues);
  readRequiredString(node, "title", `${nodePath}.title`, issues);
  readOptionalString(node, "description", `${nodePath}.description`, issues);

  if (nodeType !== undefined && !knownGraphNodeTypes.has(nodeType)) {
    issues.push(
      issue({
        code: "workflow_config.node_type_unknown",
        path: `${nodePath}.type`,
        message: `Workflow graph node type ${nodeType} is not known.`,
      }),
    );
  }

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

  validateGraphNodeShape(
    node,
    nodePath,
    nodeType,
    topLevelApproval,
    nodeIds,
    stateKeys,
    interactionKeys,
    issues,
  );

  const outcomes = node["outcomes"];
  if (outcomes === undefined) {
    if (nodeType !== "terminal") {
      issues.push(
        issue({
          code: "workflow_config.node_outcomes_missing",
          path: `${nodePath}.outcomes`,
          message: "Non-terminal workflow graph nodes need configured outcomes.",
        }),
      );
    }
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

  if (nodeType !== "terminal" && outcomes.length === 0) {
    issues.push(
      issue({
        code: "workflow_config.node_outcomes_missing",
        path: `${nodePath}.outcomes`,
        message: "Non-terminal workflow graph nodes need at least one outcome.",
      }),
    );
  }

  const routeKeys = new Set<string>();
  if (nodeId !== undefined) {
    outcomeRouteKeysByNodeId.set(nodeId, routeKeys);
  }

  outcomes.forEach((outcome, outcomeIndex) => {
    validateGraphOutcome(
      outcome,
      `${nodePath}.outcomes.${outcomeIndex}`,
      nodeIds,
      stateKeys,
      interactionKeys,
      routeKeys,
      issues,
    );
  });
}

function validateGraphNodeShape(
  node: Record<string, unknown>,
  nodePath: string,
  nodeType: string | undefined,
  topLevelApproval: Record<string, unknown> | undefined,
  nodeIds: Set<string>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (nodeType === undefined) {
    return;
  }

  if (nodeType === "block" || nodeType === "transaction_plan") {
    validateBlockReference(node["block"], `${nodePath}.block`, issues);
  }

  if (nodeType === "approval") {
    const approval = node["approval"];
    if (approval !== undefined) {
      validateApprovalNode(approval, `${nodePath}.approval`, interactionKeys, issues);
    } else if (topLevelApproval === undefined) {
      issues.push(
        issue({
          code: "workflow_config.approval_node_config_missing",
          path: `${nodePath}.approval`,
          message:
            "Approval graph nodes need node approval config or top-level approval config.",
        }),
      );
    }
  }

  if (nodeType === "approval_gate") {
    validateApprovalGate(
      node["approvalGate"],
      `${nodePath}.approvalGate`,
      nodeIds,
      stateKeys,
      interactionKeys,
      issues,
    );
  }

  if (nodeType === "policy_check") {
    readRequiredRecord(node, "policy", `${nodePath}.policy`, issues);
  }

  if (nodeType === "data_write" || nodeType === "external_write") {
    readRequiredString(node, "operation", `${nodePath}.operation`, issues);
  }

  if (nodeType === "external_write") {
    readRequiredString(node, "connectionId", `${nodePath}.connectionId`, issues);
  }

  if (sideEffectingNodeTypes.has(nodeType)) {
    const transaction = readRequiredRecord(
      node,
      "transaction",
      `${nodePath}.transaction`,
      issues,
    );
    validateTransactionBehavior(
      transaction,
      `${nodePath}.transaction`,
      nodeIds,
      stateKeys,
      interactionKeys,
      issues,
    );
  } else if (node["transaction"] !== undefined) {
    const transaction = readOptionalRecord(
      node,
      "transaction",
      `${nodePath}.transaction`,
      issues,
    );
    validateTransactionBehavior(
      transaction,
      `${nodePath}.transaction`,
      nodeIds,
      stateKeys,
      interactionKeys,
      issues,
    );
  }

  const failurePolicy = readOptionalRecord(
    node,
    "failurePolicy",
    `${nodePath}.failurePolicy`,
    issues,
  );
  validateFailurePolicy(
    failurePolicy,
    `${nodePath}.failurePolicy`,
    nodeIds,
    stateKeys,
    interactionKeys,
    issues,
  );

  const compensation = readOptionalRecord(
    node,
    "compensation",
    `${nodePath}.compensation`,
    issues,
  );
  validateCompensation(compensation, `${nodePath}.compensation`, nodeIds, issues);
}

function validateApprovalNode(
  approval: unknown,
  approvalPath: string,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(approval)) {
    issues.push(
      issue({
        code: "workflow_config.approval_node_invalid",
        path: approvalPath,
        message: "Approval node config must be an object.",
      }),
    );
    return;
  }

  readRequiredString(approval, "taskKey", `${approvalPath}.taskKey`, issues);
  readRequiredString(approval, "approvalType", `${approvalPath}.approvalType`, issues);
  validateOptionalReference(approval, "interaction", approvalPath, interactionKeys, {
    code: "workflow_config.approval_node_interaction_missing",
    label: "interaction",
    issues,
  });

  const resolver = readRequiredRecord(
    approval,
    "resolver",
    `${approvalPath}.resolver`,
    issues,
  );
  if (resolver === undefined) {
    return;
  }

  readRequiredString(resolver, "type", `${approvalPath}.resolver.type`, issues);
  readOptionalString(resolver, "actorId", `${approvalPath}.resolver.actorId`, issues);
  readOptionalString(resolver, "role", `${approvalPath}.resolver.role`, issues);
  readOptionalString(
    resolver,
    "fieldPath",
    `${approvalPath}.resolver.fieldPath`,
    issues,
  );
  readOptionalString(
    resolver,
    "permission",
    `${approvalPath}.resolver.permission`,
    issues,
  );
  validatePermissionValue(
    resolver["permission"],
    `${approvalPath}.resolver.permission`,
    issues,
  );
}

function validateApprovalGate(
  approvalGate: unknown,
  approvalGatePath: string,
  nodeIds: Set<string>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(approvalGate)) {
    issues.push(
      issue({
        code: "workflow_config.approval_gate_invalid",
        path: approvalGatePath,
        message: "Approval gate config must be an object.",
      }),
    );
    return;
  }

  readRequiredString(approvalGate, "gateId", `${approvalGatePath}.gateId`, issues);
  validateKnownStringField(approvalGate, "mode", `${approvalGatePath}.mode`, issues, {
    validValues: new Set(["sequential", "parallel"]),
    code: "workflow_config.approval_gate_mode_unknown",
    label: "approval gate mode",
  });
  validateOptionalReference(
    approvalGate,
    "interaction",
    approvalGatePath,
    interactionKeys,
    {
      code: "workflow_config.approval_gate_interaction_missing",
      label: "interaction",
      issues,
    },
  );

  const approverResolvers = approvalGate["approverResolvers"];
  if (!Array.isArray(approverResolvers) || approverResolvers.length === 0) {
    issues.push(
      issue({
        code: "workflow_config.approval_gate_resolvers_invalid",
        path: `${approvalGatePath}.approverResolvers`,
        message: "Approval gates need at least one approver resolver.",
      }),
    );
  } else {
    approverResolvers.forEach((resolver, resolverIndex) => {
      validateApprovalGateResolver(
        resolver,
        `${approvalGatePath}.approverResolvers.${resolverIndex}`,
        issues,
      );
    });
  }

  readRequiredRecord(approvalGate, "passRule", `${approvalGatePath}.passRule`, issues);

  const failurePolicies = approvalGate["failurePolicies"];
  if (Array.isArray(failurePolicies)) {
    failurePolicies.forEach((failurePolicy, failurePolicyIndex) => {
      validateApprovalGateFailurePolicy(
        failurePolicy,
        `${approvalGatePath}.failurePolicies.${failurePolicyIndex}`,
        nodeIds,
        stateKeys,
        interactionKeys,
        issues,
      );
    });
  } else {
    issues.push(
      issue({
        code: "workflow_config.approval_gate_failure_policies_invalid",
        path: `${approvalGatePath}.failurePolicies`,
        message: "Approval gate failure policies must be an array.",
      }),
    );
  }

  const events = readRequiredRecord(
    approvalGate,
    "events",
    `${approvalGatePath}.events`,
    issues,
  );
  if (events === undefined) {
    return;
  }

  for (const [eventKey, eventType] of Object.entries(events)) {
    if (eventType === undefined) {
      continue;
    }

    if (typeof eventType !== "string") {
      issues.push(
        issue({
          code: "workflow_config.approval_gate_event_invalid",
          path: `${approvalGatePath}.events.${eventKey}`,
          message: "Approval gate event values must be ledger event names.",
        }),
      );
      continue;
    }

    validateKnownLedgerEventType(
      eventType,
      `${approvalGatePath}.events.${eventKey}`,
      issues,
    );
  }
}

function validateApprovalGateResolver(
  resolver: unknown,
  resolverPath: string,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(resolver)) {
    issues.push(
      issue({
        code: "workflow_config.approval_gate_resolver_invalid",
        path: resolverPath,
        message: "Approval gate approver resolvers must be objects.",
      }),
    );
    return;
  }

  readRequiredString(resolver, "resolverId", `${resolverPath}.resolverId`, issues);
  readRequiredString(resolver, "type", `${resolverPath}.type`, issues);
  readRequiredString(resolver, "taskKey", `${resolverPath}.taskKey`, issues);
  readRequiredString(resolver, "approvalType", `${resolverPath}.approvalType`, issues);
  readRequiredString(resolver, "permission", `${resolverPath}.permission`, issues);
  validatePermissionValue(resolver["permission"], `${resolverPath}.permission`, issues);
}

function validateApprovalGateFailurePolicy(
  failurePolicy: unknown,
  policyPath: string,
  nodeIds: Set<string>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(failurePolicy)) {
    issues.push(
      issue({
        code: "workflow_config.approval_gate_failure_policy_invalid",
        path: policyPath,
        message: "Approval gate failure policy must be an object.",
      }),
    );
    return;
  }

  validateKnownStringField(failurePolicy, "type", `${policyPath}.type`, issues, {
    validValues: knownApprovalGateFailurePolicyTypes,
    code: "workflow_config.approval_gate_failure_policy_type_unknown",
    label: "approval gate failure policy",
  });
  validateOptionalReference(failurePolicy, "nextNodeId", policyPath, nodeIds, {
    code: "workflow_config.approval_gate_failure_next_node_missing",
    label: "graph node",
    issues,
  });
  validateOptionalReference(failurePolicy, "nextState", policyPath, stateKeys, {
    code: "workflow_config.approval_gate_failure_next_state_missing",
    label: "state",
    issues,
  });
  validateOptionalReference(
    failurePolicy,
    "nextStatus",
    policyPath,
    knownWorkflowStatuses,
    {
      code: "workflow_config.approval_gate_failure_next_status_unknown",
      label: "workflow status",
      issues,
    },
  );
  validateOptionalReference(
    failurePolicy,
    "nextInteraction",
    policyPath,
    interactionKeys,
    {
      code: "workflow_config.approval_gate_failure_next_interaction_missing",
      label: "interaction",
      issues,
    },
  );
}

function validateGraphOutcome(
  outcome: unknown,
  outcomePath: string,
  nodeIds: Set<string>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  routeKeys: Set<string>,
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

  const outcomeKey = readRequiredString(
    outcome,
    "outcome",
    `${outcomePath}.outcome`,
    issues,
  );
  const routeKey =
    readOptionalString(outcome, "routeKey", `${outcomePath}.routeKey`, issues) ??
    outcomeKey;

  if (routeKey !== undefined) {
    if (routeKeys.has(routeKey)) {
      issues.push(
        issue({
          code: "workflow_config.outcome_route_key_duplicated",
          path: `${outcomePath}.routeKey`,
          message: `Outcome route key ${routeKey} is duplicated for this node.`,
        }),
      );
    }

    routeKeys.add(routeKey);
  }

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
    validateKnownLedgerEventValue(eventType, `${outcomePath}.eventType`, issues);
  }

  const ledgerEvents = outcome["ledgerEvents"];
  if (ledgerEvents === undefined) {
    return;
  }

  if (!Array.isArray(ledgerEvents)) {
    issues.push(
      issue({
        code: "workflow_config.outcome_ledger_events_invalid",
        path: `${outcomePath}.ledgerEvents`,
        message: "Outcome ledger events must be an array.",
      }),
    );
    return;
  }

  ledgerEvents.forEach((ledgerEvent, ledgerEventIndex) => {
    validateLedgerEventMapping(
      ledgerEvent,
      `${outcomePath}.ledgerEvents.${ledgerEventIndex}`,
      nodeIds,
      issues,
    );
  });
}

function validateGraphEdges(
  edges: unknown,
  nodeIds: Set<string>,
  outcomeRouteKeysByNodeId: Map<string, Set<string>>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (edges === undefined) {
    return;
  }

  if (!Array.isArray(edges)) {
    issues.push(
      issue({
        code: "workflow_config.graph_edges_invalid",
        path: "graph.edges",
        message: "Workflow graph edges must be an array.",
      }),
    );
    return;
  }

  const edgeIds = new Set<string>();
  edges.forEach((edge, edgeIndex) => {
    validateGraphEdge(
      edge,
      `graph.edges.${edgeIndex}`,
      edgeIds,
      nodeIds,
      outcomeRouteKeysByNodeId,
      stateKeys,
      interactionKeys,
      issues,
    );
  });
}

function validateGraphEdge(
  edge: unknown,
  edgePath: string,
  edgeIds: Set<string>,
  nodeIds: Set<string>,
  outcomeRouteKeysByNodeId: Map<string, Set<string>>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(edge)) {
    issues.push(
      issue({
        code: "workflow_config.graph_edge_invalid",
        path: edgePath,
        message: "Workflow graph edge must be an object.",
      }),
    );
    return;
  }

  const edgeId = readOptionalString(edge, "edgeId", `${edgePath}.edgeId`, issues);
  if (edgeId !== undefined) {
    if (edgeIds.has(edgeId)) {
      issues.push(
        issue({
          code: "workflow_config.graph_duplicate_edge_id",
          path: `${edgePath}.edgeId`,
          message: `Graph edge ID ${edgeId} is duplicated.`,
        }),
      );
    }

    edgeIds.add(edgeId);
  }

  const fromNodeId = readRequiredString(
    edge,
    "fromNodeId",
    `${edgePath}.fromNodeId`,
    issues,
  );
  const routeKey = readRequiredString(edge, "routeKey", `${edgePath}.routeKey`, issues);
  validateOptionalReference(edge, "toNodeId", edgePath, nodeIds, {
    code: "workflow_config.edge_to_node_missing",
    label: "graph node",
    issues,
  });
  validateOptionalReference(edge, "nextState", edgePath, stateKeys, {
    code: "workflow_config.edge_next_state_missing",
    label: "state",
    issues,
  });
  validateOptionalReference(edge, "nextStatus", edgePath, knownWorkflowStatuses, {
    code: "workflow_config.edge_next_status_unknown",
    label: "workflow status",
    issues,
  });
  validateOptionalReference(edge, "nextInteraction", edgePath, interactionKeys, {
    code: "workflow_config.edge_next_interaction_missing",
    label: "interaction",
    issues,
  });

  if (fromNodeId !== undefined && !nodeIds.has(fromNodeId)) {
    issues.push(
      issue({
        code: "workflow_config.edge_from_node_missing",
        path: `${edgePath}.fromNodeId`,
        message: `Graph edge source node ${fromNodeId} is not defined.`,
      }),
    );
  }

  if (
    fromNodeId !== undefined &&
    routeKey !== undefined &&
    nodeIds.has(fromNodeId) &&
    !outcomeRouteKeysByNodeId.get(fromNodeId)?.has(routeKey)
  ) {
    issues.push(
      issue({
        code: "workflow_config.edge_route_key_missing",
        path: `${edgePath}.routeKey`,
        message: `Graph edge route key ${routeKey} is not an outcome for ${fromNodeId}.`,
      }),
    );
  }

  const eventType = edge["eventType"];
  if (eventType !== undefined) {
    validateKnownLedgerEventValue(eventType, `${edgePath}.eventType`, issues);
  }
}

function validateTransactionBehavior(
  transaction: Record<string, unknown> | undefined,
  transactionPath: string,
  nodeIds: Set<string>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (transaction === undefined) {
    return;
  }

  const sideEffect = readRequiredBoolean(
    transaction,
    "sideEffect",
    `${transactionPath}.sideEffect`,
    issues,
  );
  validateKnownStringField(
    transaction,
    "reversibility",
    `${transactionPath}.reversibility`,
    issues,
    {
      validValues: knownReversibilityValues,
      code: "workflow_config.transaction_reversibility_unknown",
      label: "transaction reversibility",
    },
  );
  validateOptionalReference(
    transaction,
    "compensatingNodeId",
    transactionPath,
    nodeIds,
    {
      code: "workflow_config.transaction_compensating_node_missing",
      label: "graph node",
      issues,
    },
  );

  const idempotency = readOptionalRecord(
    transaction,
    "idempotency",
    `${transactionPath}.idempotency`,
    issues,
  );
  validateIdempotency(idempotency, `${transactionPath}.idempotency`, issues);
  readOptionalString(
    transaction,
    "idempotencyKeyPath",
    `${transactionPath}.idempotencyKeyPath`,
    issues,
  );

  if (
    sideEffect === true &&
    idempotency === undefined &&
    typeof transaction["idempotencyKeyPath"] !== "string"
  ) {
    issues.push(
      issue({
        code: "workflow_config.transaction_idempotency_missing",
        path: transactionPath,
        message: "Side-effecting graph nodes need idempotency config.",
      }),
    );
  }

  const retryPolicy = readOptionalRecord(
    transaction,
    "retryPolicy",
    `${transactionPath}.retryPolicy`,
    issues,
  );
  validateRetryPolicy(retryPolicy, `${transactionPath}.retryPolicy`, issues);

  const reconciliation = readOptionalRecord(
    transaction,
    "reconciliation",
    `${transactionPath}.reconciliation`,
    issues,
  );
  validateReconciliation(
    reconciliation,
    `${transactionPath}.reconciliation`,
    nodeIds,
    stateKeys,
    interactionKeys,
    issues,
  );

  const failurePolicy = readOptionalRecord(
    transaction,
    "failurePolicy",
    `${transactionPath}.failurePolicy`,
    issues,
  );
  validateFailurePolicy(
    failurePolicy,
    `${transactionPath}.failurePolicy`,
    nodeIds,
    stateKeys,
    interactionKeys,
    issues,
  );
}

function validateIdempotency(
  idempotency: Record<string, unknown> | undefined,
  idempotencyPath: string,
  issues: WorkflowValidationIssue[],
): void {
  if (idempotency === undefined) {
    return;
  }

  validateKnownStringField(idempotency, "scope", `${idempotencyPath}.scope`, issues, {
    validValues: knownIdempotencyScopes,
    code: "workflow_config.idempotency_scope_unknown",
    label: "idempotency scope",
  });
  validateKnownStringField(
    idempotency,
    "onDuplicate",
    `${idempotencyPath}.onDuplicate`,
    issues,
    {
      validValues: knownDuplicateBehaviors,
      code: "workflow_config.idempotency_duplicate_behavior_unknown",
      label: "duplicate behavior",
    },
  );
  readOptionalString(
    idempotency,
    "keyTemplate",
    `${idempotencyPath}.keyTemplate`,
    issues,
  );
  readOptionalString(idempotency, "keyPath", `${idempotencyPath}.keyPath`, issues);

  if (
    typeof idempotency["keyTemplate"] !== "string" &&
    typeof idempotency["keyPath"] !== "string"
  ) {
    issues.push(
      issue({
        code: "workflow_config.idempotency_key_missing",
        path: idempotencyPath,
        message: "Idempotency config needs keyTemplate or keyPath.",
      }),
    );
  }
}

function validateRetryPolicy(
  retryPolicy: Record<string, unknown> | undefined,
  retryPolicyPath: string,
  issues: WorkflowValidationIssue[],
): void {
  if (retryPolicy === undefined) {
    return;
  }

  const maxAttempts = retryPolicy["maxAttempts"];
  if (typeof maxAttempts !== "number" || maxAttempts < 0) {
    issues.push(
      issue({
        code: "workflow_config.retry_max_attempts_invalid",
        path: `${retryPolicyPath}.maxAttempts`,
        message: "Retry maxAttempts must be a non-negative number.",
      }),
    );
  }

  validateKnownStringField(
    retryPolicy,
    "backoff",
    `${retryPolicyPath}.backoff`,
    issues,
    {
      validValues: knownRetryBackoffs,
      code: "workflow_config.retry_backoff_unknown",
      label: "retry backoff",
    },
  );

  if (retryPolicy["retryOn"] !== undefined) {
    readStringArray(retryPolicy["retryOn"], `${retryPolicyPath}.retryOn`, issues);
  }
}

function validateReconciliation(
  reconciliation: Record<string, unknown> | undefined,
  reconciliationPath: string,
  nodeIds: Set<string>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (reconciliation === undefined) {
    return;
  }

  readRequiredBoolean(
    reconciliation,
    "required",
    `${reconciliationPath}.required`,
    issues,
  );
  validateOptionalReference(
    reconciliation,
    "checkNodeId",
    reconciliationPath,
    nodeIds,
    {
      code: "workflow_config.reconciliation_check_node_missing",
      label: "graph node",
      issues,
    },
  );

  const onMismatch = readOptionalRecord(
    reconciliation,
    "onMismatch",
    `${reconciliationPath}.onMismatch`,
    issues,
  );
  if (onMismatch === undefined) {
    return;
  }

  validateOptionalReference(
    onMismatch,
    "nextNodeId",
    `${reconciliationPath}.onMismatch`,
    nodeIds,
    {
      code: "workflow_config.reconciliation_next_node_missing",
      label: "graph node",
      issues,
    },
  );
  validateOptionalReference(
    onMismatch,
    "nextState",
    `${reconciliationPath}.onMismatch`,
    stateKeys,
    {
      code: "workflow_config.reconciliation_next_state_missing",
      label: "state",
      issues,
    },
  );
  validateOptionalReference(
    onMismatch,
    "nextStatus",
    `${reconciliationPath}.onMismatch`,
    knownWorkflowStatuses,
    {
      code: "workflow_config.reconciliation_next_status_unknown",
      label: "workflow status",
      issues,
    },
  );
  validateOptionalReference(
    onMismatch,
    "nextInteraction",
    `${reconciliationPath}.onMismatch`,
    interactionKeys,
    {
      code: "workflow_config.reconciliation_next_interaction_missing",
      label: "interaction",
      issues,
    },
  );

  const eventType = onMismatch["eventType"];
  if (eventType !== undefined) {
    validateKnownLedgerEventValue(
      eventType,
      `${reconciliationPath}.onMismatch.eventType`,
      issues,
    );
  }
}

function validateFailurePolicy(
  failurePolicy: Record<string, unknown> | undefined,
  failurePolicyPath: string,
  nodeIds: Set<string>,
  stateKeys: Set<string>,
  interactionKeys: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (failurePolicy === undefined) {
    return;
  }

  validateKnownStringField(
    failurePolicy,
    "onFailure",
    `${failurePolicyPath}.onFailure`,
    issues,
    {
      validValues: knownFailurePolicies,
      code: "workflow_config.failure_policy_unknown",
      label: "failure policy",
    },
  );

  const retryPolicy = readOptionalRecord(
    failurePolicy,
    "retryPolicy",
    `${failurePolicyPath}.retryPolicy`,
    issues,
  );
  validateRetryPolicy(retryPolicy, `${failurePolicyPath}.retryPolicy`, issues);

  const afterRetriesExhausted = readOptionalRecord(
    failurePolicy,
    "afterRetriesExhausted",
    `${failurePolicyPath}.afterRetriesExhausted`,
    issues,
  );
  if (afterRetriesExhausted === undefined) {
    return;
  }

  validateOptionalReference(
    afterRetriesExhausted,
    "nextNodeId",
    `${failurePolicyPath}.afterRetriesExhausted`,
    nodeIds,
    {
      code: "workflow_config.failure_policy_next_node_missing",
      label: "graph node",
      issues,
    },
  );
  validateOptionalReference(
    afterRetriesExhausted,
    "nextState",
    `${failurePolicyPath}.afterRetriesExhausted`,
    stateKeys,
    {
      code: "workflow_config.failure_policy_next_state_missing",
      label: "state",
      issues,
    },
  );
  validateOptionalReference(
    afterRetriesExhausted,
    "nextStatus",
    `${failurePolicyPath}.afterRetriesExhausted`,
    knownWorkflowStatuses,
    {
      code: "workflow_config.failure_policy_next_status_unknown",
      label: "workflow status",
      issues,
    },
  );
  validateOptionalReference(
    afterRetriesExhausted,
    "nextInteraction",
    `${failurePolicyPath}.afterRetriesExhausted`,
    interactionKeys,
    {
      code: "workflow_config.failure_policy_next_interaction_missing",
      label: "interaction",
      issues,
    },
  );

  const eventType = afterRetriesExhausted["eventType"];
  if (eventType !== undefined) {
    validateKnownLedgerEventValue(
      eventType,
      `${failurePolicyPath}.afterRetriesExhausted.eventType`,
      issues,
    );
  }
}

function validateCompensation(
  compensation: Record<string, unknown> | undefined,
  compensationPath: string,
  nodeIds: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (compensation === undefined) {
    return;
  }

  validateOptionalReference(
    compensation,
    "compensatesNodeId",
    compensationPath,
    nodeIds,
    {
      code: "workflow_config.compensation_node_missing",
      label: "graph node",
      issues,
    },
  );
  readRequiredString(compensation, "trigger", `${compensationPath}.trigger`, issues);
  readOptionalString(
    compensation,
    "reasonPath",
    `${compensationPath}.reasonPath`,
    issues,
  );
  readOptionalString(
    compensation,
    "successOutcome",
    `${compensationPath}.successOutcome`,
    issues,
  );
  readOptionalString(
    compensation,
    "failureOutcome",
    `${compensationPath}.failureOutcome`,
    issues,
  );
}

function workflowStateReferenceSet(
  states: Record<string, unknown> | undefined,
): Set<string> {
  return new Set([...knownWorkflowStates, ...Object.keys(states ?? {})]);
}

function validateSaga(
  saga: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (saga === undefined) {
    return;
  }

  validateKnownStringField(saga, "mode", "saga.mode", issues, {
    validValues: knownSagaModes,
    code: "workflow_config.saga_mode_unknown",
    label: "saga mode",
  });
  validateKnownStringField(
    saga,
    "transactionBoundary",
    "saga.transactionBoundary",
    issues,
    {
      validValues: knownSagaBoundaries,
      code: "workflow_config.saga_boundary_unknown",
      label: "saga transaction boundary",
    },
  );
  validateKnownStringField(saga, "failurePolicy", "saga.failurePolicy", issues, {
    validValues: knownSagaFailurePolicies,
    code: "workflow_config.saga_failure_policy_unknown",
    label: "saga failure policy",
  });
  validateKnownStringField(saga, "idempotencyScope", "saga.idempotencyScope", issues, {
    validValues: knownIdempotencyScopes,
    code: "workflow_config.saga_idempotency_scope_unknown",
    label: "idempotency scope",
  });
  validateKnownStringField(
    saga,
    "compensationOrder",
    "saga.compensationOrder",
    issues,
    {
      validValues: knownCompensationOrders,
      code: "workflow_config.saga_compensation_order_unknown",
      label: "compensation order",
    },
  );
  readRequiredBoolean(
    saga,
    "reconciliationRequired",
    "saga.reconciliationRequired",
    issues,
  );
}

function validateLedger(
  ledger: Record<string, unknown> | undefined,
  nodeIds: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (ledger === undefined) {
    return;
  }

  const events = readOptionalRecord(ledger, "events", "ledger.events", issues);
  if (events !== undefined) {
    for (const [eventKey, eventMapping] of Object.entries(events)) {
      validateLedgerEventMapping(
        eventMapping,
        `ledger.events.${eventKey}`,
        nodeIds,
        issues,
      );
    }
  }

  const nodeEvents = ledger["nodeEvents"];
  if (nodeEvents === undefined) {
    return;
  }

  if (!Array.isArray(nodeEvents)) {
    issues.push(
      issue({
        code: "workflow_config.ledger_node_events_invalid",
        path: "ledger.nodeEvents",
        message: "Ledger node events must be an array.",
      }),
    );
    return;
  }

  nodeEvents.forEach((eventMapping, eventIndex) => {
    validateLedgerEventMapping(
      eventMapping,
      `ledger.nodeEvents.${eventIndex}`,
      nodeIds,
      issues,
    );
  });
}

function validateLedgerEventMapping(
  eventMapping: unknown,
  eventMappingPath: string,
  nodeIds: Set<string>,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(eventMapping)) {
    issues.push(
      issue({
        code: "workflow_config.ledger_event_mapping_invalid",
        path: eventMappingPath,
        message: "Ledger event mapping must be an object.",
      }),
    );
    return;
  }

  const eventType = readRequiredString(
    eventMapping,
    "eventType",
    `${eventMappingPath}.eventType`,
    issues,
  );
  if (eventType !== undefined) {
    validateKnownLedgerEventType(eventType, `${eventMappingPath}.eventType`, issues);
  }

  validateKnownStringField(
    eventMapping,
    "source",
    `${eventMappingPath}.source`,
    issues,
    {
      validValues: knownLedgerEventSources,
      code: "workflow_config.ledger_event_source_unknown",
      label: "ledger event source",
    },
  );
  validateOptionalReference(eventMapping, "nodeId", eventMappingPath, nodeIds, {
    code: "workflow_config.ledger_event_node_missing",
    label: "graph node",
    issues,
  });
  readOptionalString(eventMapping, "routeKey", `${eventMappingPath}.routeKey`, issues);
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
    validateKnownLedgerEventType(
      eventType,
      `timeline.businessEvents.${eventIndex}`,
      issues,
    );

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

function validateConfigLintRules(
  workflowConfig: Record<string, unknown>,
  issues: WorkflowValidationIssue[],
): void {
  const graph = isRecord(workflowConfig["graph"]) ? workflowConfig["graph"] : undefined;
  const interactions = isRecord(workflowConfig["interactions"])
    ? workflowConfig["interactions"]
    : undefined;
  const ledger = isRecord(workflowConfig["ledger"])
    ? workflowConfig["ledger"]
    : undefined;

  validateUnreachableGraphNodes(graph, issues);
  validateUnusedInteractions(workflowConfig, interactions, graph, issues);
  validateUnusedLedgerMappings(workflowConfig, ledger, graph, issues);
}

function validateUnreachableGraphNodes(
  graph: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  const nodes = Array.isArray(graph?.["nodes"]) ? graph["nodes"] : undefined;
  const startNodeId = stringValue(graph?.["startNodeId"]);

  if (nodes === undefined || startNodeId === undefined) {
    return;
  }

  const adjacency = graphAdjacency(nodes);
  const reachableNodeIds = reachableNodes(startNodeId, adjacency);

  nodes.forEach((node, nodeIndex) => {
    if (!isRecord(node)) {
      return;
    }

    const nodeId = stringValue(node["nodeId"]);
    if (nodeId !== undefined && !reachableNodeIds.has(nodeId)) {
      issues.push(
        issue({
          severity: "warning",
          code: "workflow_config.graph_node_unreachable",
          path: `graph.nodes.${nodeIndex}.nodeId`,
          message: `Graph node ${nodeId} is not reachable from the start node.`,
        }),
      );
    }
  });
}

function validateUnusedInteractions(
  workflowConfig: Record<string, unknown>,
  interactions: Record<string, unknown> | undefined,
  graph: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  if (interactions === undefined) {
    return;
  }

  const referencedInteractions = collectReferencedInteractions(workflowConfig, graph);
  for (const interactionKey of Object.keys(interactions)) {
    if (!referencedInteractions.has(interactionKey)) {
      issues.push(
        issue({
          severity: "warning",
          code: "workflow_config.interaction_unused",
          path: `interactions.${interactionKey}`,
          message: `Workflow interaction ${interactionKey} is not referenced by UI, states, or graph routes.`,
        }),
      );
    }
  }
}

function validateUnusedLedgerMappings(
  workflowConfig: Record<string, unknown>,
  ledger: Record<string, unknown> | undefined,
  graph: Record<string, unknown> | undefined,
  issues: WorkflowValidationIssue[],
): void {
  const ledgerEvents = isRecord(ledger?.["events"]) ? ledger["events"] : undefined;
  if (ledgerEvents === undefined) {
    return;
  }

  const referencedEventTypes = collectReferencedLedgerEvents(workflowConfig, graph);
  for (const eventType of Object.keys(ledgerEvents)) {
    if (!referencedEventTypes.has(eventType)) {
      issues.push(
        issue({
          severity: "warning",
          code: "workflow_config.ledger_mapping_unused",
          path: `ledger.events.${eventType}`,
          message: `Ledger mapping ${eventType} is not referenced by workflow actions, graph routes, or timeline summaries.`,
        }),
      );
    }
  }
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

function validateBlockReference(
  value: unknown,
  path: string,
  issues: WorkflowValidationIssue[],
): void {
  if (!isRecord(value)) {
    issues.push(
      issue({
        code: "workflow_config.block_reference_invalid",
        path,
        message: `Workflow block reference ${path} must be an object.`,
      }),
    );
    return;
  }

  readRequiredString(value, "name", `${path}.name`, issues);
  readRequiredString(value, "version", `${path}.version`, issues);

  const runtime = value["runtime"];
  if (runtime !== undefined && runtime !== "go") {
    issues.push(
      issue({
        code: "workflow_config.block_runtime_unknown",
        path: `${path}.runtime`,
        message: `Workflow block runtime ${String(runtime)} is not supported.`,
      }),
    );
  }
}

function validateKnownStringField(
  record: Record<string, unknown>,
  field: string,
  path: string,
  issues: WorkflowValidationIssue[],
  input: {
    validValues: Set<string>;
    code: string;
    label: string;
  },
): string | undefined {
  const value = readRequiredString(record, field, path, issues);
  if (value === undefined) {
    return undefined;
  }

  if (!input.validValues.has(value)) {
    issues.push(
      issue({
        code: input.code,
        path,
        message: `Workflow ${input.label} ${value} is not supported.`,
      }),
    );
  }

  return value;
}

function validateKnownLedgerEventValue(
  value: unknown,
  path: string,
  issues: WorkflowValidationIssue[],
): void {
  if (typeof value !== "string") {
    issues.push(
      issue({
        code: "workflow_config.ledger_event_type_invalid",
        path,
        message: `Ledger event reference ${path} must be a string.`,
      }),
    );
    return;
  }

  validateKnownLedgerEventType(value, path, issues);
}

function validateKnownLedgerEventType(
  eventType: string,
  path: string,
  issues: WorkflowValidationIssue[],
): void {
  if (knownLedgerEventTypes.has(eventType)) {
    return;
  }

  issues.push(
    issue({
      code: "workflow_config.ledger_event_type_unknown",
      path,
      message: `Workflow references unknown ledger event ${eventType}.`,
    }),
  );
}

function readOptionalRecord(
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

function readOptionalString(
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
    jsonPath: toJsonPath(input.path),
    message: input.message,
  };
}

function validatePermissionArray(
  value: unknown,
  path: string,
  issues: WorkflowValidationIssue[],
): void {
  if (value === undefined) {
    return;
  }

  if (!Array.isArray(value)) {
    issues.push(
      issue({
        code: "workflow_config.permissions_invalid",
        path,
        message: "Workflow permission references must be an array of permission keys.",
      }),
    );
    return;
  }

  value.forEach((permission, permissionIndex) => {
    validatePermissionValue(permission, `${path}.${permissionIndex}`, issues);
  });
}

function validatePermissionValue(
  value: unknown,
  path: string,
  issues: WorkflowValidationIssue[],
): void {
  if (value === undefined) {
    return;
  }

  if (typeof value !== "string" || value.trim().length === 0) {
    issues.push(
      issue({
        code: "workflow_config.permission_reference_invalid",
        path,
        message: "Workflow permission references must be non-empty strings.",
      }),
    );
    return;
  }

  if (value === "*" || value.endsWith(".*")) {
    issues.push(
      issue({
        severity: "warning",
        code: "workflow_config.permission_scope_overbroad",
        path,
        message: `Workflow permission reference ${value} is broader than expected for HR workflow admin.`,
      }),
    );
    return;
  }

  if (!knownPermissionKeys.has(value)) {
    issues.push(
      issue({
        code: "workflow_config.permission_reference_unknown",
        path,
        message: `Workflow references unknown permission ${value}.`,
      }),
    );
  }
}

function collectReferencedInteractions(
  workflowConfig: Record<string, unknown>,
  graph: Record<string, unknown> | undefined,
): Set<string> {
  const referencedInteractions = new Set<string>();
  const ui = isRecord(workflowConfig["ui"]) ? workflowConfig["ui"] : undefined;
  const states = isRecord(workflowConfig["states"]) ? workflowConfig["states"] : {};
  const nodes = Array.isArray(graph?.["nodes"]) ? graph["nodes"] : [];

  addStringIfPresent(referencedInteractions, ui?.["defaultInteraction"]);

  const stateInteractions = isRecord(ui?.["stateInteractions"])
    ? ui["stateInteractions"]
    : {};
  for (const value of Object.values(stateInteractions)) {
    addStringIfPresent(referencedInteractions, value);
  }

  for (const stateConfig of Object.values(states)) {
    if (!isRecord(stateConfig) || !Array.isArray(stateConfig["actions"])) {
      continue;
    }

    for (const action of stateConfig["actions"]) {
      if (isRecord(action)) {
        addStringIfPresent(referencedInteractions, action["nextInteraction"]);
      }
    }
  }

  for (const node of nodes) {
    if (!isRecord(node)) {
      continue;
    }

    addStringIfPresent(referencedInteractions, node["interaction"]);

    const outcomes = Array.isArray(node["outcomes"]) ? node["outcomes"] : [];
    for (const outcome of outcomes) {
      if (isRecord(outcome)) {
        addStringIfPresent(referencedInteractions, outcome["nextInteraction"]);
      }
    }

    const approvalGate = isRecord(node["approvalGate"])
      ? node["approvalGate"]
      : undefined;
    addStringIfPresent(referencedInteractions, approvalGate?.["interaction"]);
  }

  return referencedInteractions;
}

function collectReferencedLedgerEvents(
  workflowConfig: Record<string, unknown>,
  graph: Record<string, unknown> | undefined,
): Set<string> {
  const referencedEventTypes = new Set<string>();
  const submit = isRecord(workflowConfig["submit"]) ? workflowConfig["submit"] : {};
  const timeline = isRecord(workflowConfig["timeline"])
    ? workflowConfig["timeline"]
    : {};
  const nodes = Array.isArray(graph?.["nodes"]) ? graph["nodes"] : [];

  addStringArrayValues(referencedEventTypes, submit["additionalEvents"]);
  addStringArrayValues(referencedEventTypes, timeline["businessEvents"]);

  const summaries = isRecord(timeline["summaries"]) ? timeline["summaries"] : {};
  for (const eventType of Object.keys(summaries)) {
    referencedEventTypes.add(eventType);
  }

  for (const node of nodes) {
    if (!isRecord(node)) {
      continue;
    }

    const outcomes = Array.isArray(node["outcomes"]) ? node["outcomes"] : [];
    for (const outcome of outcomes) {
      if (isRecord(outcome)) {
        addStringIfPresent(referencedEventTypes, outcome["eventType"]);
      }
    }

    const approvalGate = isRecord(node["approvalGate"])
      ? node["approvalGate"]
      : undefined;
    const approvalGateEvents = isRecord(approvalGate?.["events"])
      ? approvalGate["events"]
      : {};
    for (const eventType of Object.values(approvalGateEvents)) {
      addStringIfPresent(referencedEventTypes, eventType);
    }
  }

  return referencedEventTypes;
}

function graphAdjacency(nodes: unknown[]): Map<string, string[]> {
  const adjacency = new Map<string, string[]>();

  for (const node of nodes) {
    if (!isRecord(node)) {
      continue;
    }

    const nodeId = stringValue(node["nodeId"]);
    if (nodeId === undefined) {
      continue;
    }

    const nextNodeIds: string[] = [];
    const outcomes = Array.isArray(node["outcomes"]) ? node["outcomes"] : [];

    for (const outcome of outcomes) {
      if (!isRecord(outcome)) {
        continue;
      }

      const nextNodeId = stringValue(outcome["nextNodeId"]);
      if (nextNodeId !== undefined) {
        nextNodeIds.push(nextNodeId);
      }
    }

    adjacency.set(nodeId, nextNodeIds);
  }

  return adjacency;
}

function reachableNodes(
  startNodeId: string,
  adjacency: Map<string, string[]>,
): Set<string> {
  const visitedNodeIds = new Set<string>();
  const pendingNodeIds = [startNodeId];

  while (pendingNodeIds.length > 0) {
    const nodeId = pendingNodeIds.pop();
    if (nodeId === undefined || visitedNodeIds.has(nodeId)) {
      continue;
    }

    visitedNodeIds.add(nodeId);

    for (const nextNodeId of adjacency.get(nodeId) ?? []) {
      pendingNodeIds.push(nextNodeId);
    }
  }

  return visitedNodeIds;
}

function addStringArrayValues(target: Set<string>, value: unknown): void {
  if (!Array.isArray(value)) {
    return;
  }

  for (const item of value) {
    addStringIfPresent(target, item);
  }
}

function addStringIfPresent(target: Set<string>, value: unknown): void {
  const text = stringValue(value);
  if (text !== undefined) {
    target.add(text);
  }
}

function stringValue(value: unknown): string | undefined {
  if (typeof value !== "string") {
    return undefined;
  }

  const trimmedValue = value.trim();
  return trimmedValue.length > 0 ? trimmedValue : undefined;
}

function toJsonPath(path: string): string {
  if (path === "$") {
    return "$";
  }

  return `$.${path}`;
}

function hasOwnField(record: Record<string, unknown>, field: string): boolean {
  return Object.prototype.hasOwnProperty.call(record, field);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
