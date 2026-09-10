import {
  ok,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type {
  WorkflowConfig,
  WorkflowGraphNodeConfig,
} from "../../shared/workflow-config.js";
import type { WorkflowAdminRiskLevel } from "../safety/admin-issues.js";

export type WorkflowDiffCategory =
  | "metadata"
  | "state"
  | "graph_node"
  | "route"
  | "approval_gate"
  | "permission"
  | "interaction"
  | "block"
  | "integration"
  | "ledger"
  | "transaction"
  | "ai_context";

export type WorkflowDiffChangeType = "added" | "removed" | "changed";

export type WorkflowDiffItem = {
  category: WorkflowDiffCategory;
  changeType: WorkflowDiffChangeType;
  path: string;
  riskLevel: WorkflowAdminRiskLevel;
  message: string;
  before?: unknown;
  after?: unknown;
  reasonCodes: string[];
};

export type WorkflowImpactSummary = {
  riskLevel: WorkflowAdminRiskLevel;
  highRiskCount: number;
  mediumRiskCount: number;
  lowRiskCount: number;
  newExternalCallCount: number;
  removedApprovalStepCount: number;
  sensitiveFieldExposureCount: number;
  compensationPathChangeCount: number;
  aiSensitiveFieldExpansionCount: number;
};

export type WorkflowDiffReview = {
  hasChanges: boolean;
  items: WorkflowDiffItem[];
  impact: WorkflowImpactSummary;
};

const sensitiveFieldHints = [
  "compensation",
  "salary",
  "bonus",
  "contact",
  "emergency",
  "medical",
  "nationalid",
  "governmentid",
];

/**
 * Compares a draft workflow config to a published config and classifies publish risk.
 */
export function diffWorkflowConfigs(input: {
  draftConfig: WorkflowConfig;
  publishedConfig: WorkflowConfig;
}): Result<WorkflowDiffReview, AppError> {
  const items = [
    ...diffTopLevelMetadata(input),
    ...diffRecordKeys({
      category: "state",
      pathPrefix: "states",
      before: input.publishedConfig.states,
      after: input.draftConfig.states,
      messageLabel: "workflow state",
    }),
    ...diffGraphNodes(input.publishedConfig, input.draftConfig),
    ...diffRoutes(input.publishedConfig, input.draftConfig),
    ...diffApprovalGates(input.publishedConfig, input.draftConfig),
    ...diffPermissions(input.publishedConfig, input.draftConfig),
    ...diffRecordKeys({
      category: "interaction",
      pathPrefix: "interactions",
      before: input.publishedConfig.interactions,
      after: input.draftConfig.interactions,
      messageLabel: "interaction",
    }),
    ...diffBlocks(input.publishedConfig, input.draftConfig),
    ...diffIntegrations(input.publishedConfig, input.draftConfig),
    ...diffLedgerMappings(input.publishedConfig, input.draftConfig),
    ...diffTransactions(input.publishedConfig, input.draftConfig),
    ...diffAiVisibility(input.publishedConfig, input.draftConfig),
  ];

  return ok({
    hasChanges: items.length > 0,
    items,
    impact: impactSummaryFrom(items),
  });
}

function diffTopLevelMetadata(input: {
  draftConfig: WorkflowConfig;
  publishedConfig: WorkflowConfig;
}): WorkflowDiffItem[] {
  const metadataPairs = [
    ["intent", input.publishedConfig.intent, input.draftConfig.intent],
    ["subjectType", input.publishedConfig.subjectType, input.draftConfig.subjectType],
    [
      "selfServiceStart",
      input.publishedConfig.selfServiceStart,
      input.draftConfig.selfServiceStart,
    ],
    ["startActors", input.publishedConfig.startActors, input.draftConfig.startActors],
    ["metadata", input.publishedConfig.metadata, input.draftConfig.metadata],
    ["saga", input.publishedConfig.saga, input.draftConfig.saga],
  ] as const;

  return metadataPairs.flatMap(([path, before, after]) =>
    valuesEqual(before, after)
      ? []
      : [
          diffItem({
            category: "metadata",
            changeType: "changed",
            path,
            before,
            after,
            message: `Workflow metadata changed at ${path}.`,
          }),
        ],
  );
}

function diffGraphNodes(
  publishedConfig: WorkflowConfig,
  draftConfig: WorkflowConfig,
): WorkflowDiffItem[] {
  const beforeNodes = nodesById(publishedConfig);
  const afterNodes = nodesById(draftConfig);
  const nodeIds = uniqueStrings([...beforeNodes.keys(), ...afterNodes.keys()]);
  const diffItems: WorkflowDiffItem[] = [];

  for (const nodeId of nodeIds) {
    const beforeNode = beforeNodes.get(nodeId);
    const afterNode = afterNodes.get(nodeId);
    const path = `graph.nodes.${nodeId}`;

    if (beforeNode === undefined && afterNode !== undefined) {
      diffItems.push(
        diffItem({
          category: "graph_node",
          changeType: "added",
          path,
          after: afterNode,
          message: `Workflow graph node ${nodeId} was added.`,
        }),
      );
      continue;
    }

    if (beforeNode !== undefined && afterNode === undefined) {
      diffItems.push(
        diffItem({
          category: "graph_node",
          changeType: "removed",
          path,
          before: beforeNode,
          message: `Workflow graph node ${nodeId} was removed.`,
        }),
      );
      continue;
    }

    if (!valuesEqual(beforeNode, afterNode)) {
      diffItems.push(
        diffItem({
          category: "graph_node",
          changeType: "changed",
          path,
          before: beforeNode,
          after: afterNode,
          message: `Workflow graph node ${nodeId} changed.`,
        }),
      );
    }
  }

  return diffItems;
}

function diffRoutes(
  publishedConfig: WorkflowConfig,
  draftConfig: WorkflowConfig,
): WorkflowDiffItem[] {
  return diffRecordKeys({
    category: "route",
    pathPrefix: "graph.routes",
    before: routesByKey(publishedConfig),
    after: routesByKey(draftConfig),
    messageLabel: "route",
  });
}

function diffApprovalGates(
  publishedConfig: WorkflowConfig,
  draftConfig: WorkflowConfig,
): WorkflowDiffItem[] {
  return diffRecordKeys({
    category: "approval_gate",
    pathPrefix: "graph.approvalGates",
    before: approvalGatesByNodeId(publishedConfig),
    after: approvalGatesByNodeId(draftConfig),
    messageLabel: "approval gate",
  });
}

function diffPermissions(
  publishedConfig: WorkflowConfig,
  draftConfig: WorkflowConfig,
): WorkflowDiffItem[] {
  return diffRecordKeys({
    category: "permission",
    pathPrefix: "permissions",
    before: permissionSummaryByKey(publishedConfig),
    after: permissionSummaryByKey(draftConfig),
    messageLabel: "permission reference",
  });
}

function diffBlocks(
  publishedConfig: WorkflowConfig,
  draftConfig: WorkflowConfig,
): WorkflowDiffItem[] {
  return diffRecordKeys({
    category: "block",
    pathPrefix: "blocks",
    before: blockRefsByKey(publishedConfig),
    after: blockRefsByKey(draftConfig),
    messageLabel: "block reference",
  });
}

function diffIntegrations(
  publishedConfig: WorkflowConfig,
  draftConfig: WorkflowConfig,
): WorkflowDiffItem[] {
  return diffRecordKeys({
    category: "integration",
    pathPrefix: "integrations",
    before: integrationRefsByKey(publishedConfig),
    after: integrationRefsByKey(draftConfig),
    messageLabel: "integration reference",
  });
}

function diffLedgerMappings(
  publishedConfig: WorkflowConfig,
  draftConfig: WorkflowConfig,
): WorkflowDiffItem[] {
  return diffRecordKeys({
    category: "ledger",
    pathPrefix: "ledger",
    before: ledgerRefsByKey(publishedConfig),
    after: ledgerRefsByKey(draftConfig),
    messageLabel: "ledger mapping",
  });
}

function diffTransactions(
  publishedConfig: WorkflowConfig,
  draftConfig: WorkflowConfig,
): WorkflowDiffItem[] {
  return diffRecordKeys({
    category: "transaction",
    pathPrefix: "graph.transactions",
    before: transactionRefsByKey(publishedConfig),
    after: transactionRefsByKey(draftConfig),
    messageLabel: "transaction settings",
  });
}

function diffAiVisibility(
  publishedConfig: WorkflowConfig,
  draftConfig: WorkflowConfig,
): WorkflowDiffItem[] {
  return diffRecordKeys({
    category: "ai_context",
    pathPrefix: "aiReviewScope",
    before: aiVisibilityByKey(publishedConfig),
    after: aiVisibilityByKey(draftConfig),
    messageLabel: "AI context visibility",
  });
}

function diffRecordKeys(input: {
  category: WorkflowDiffCategory;
  pathPrefix: string;
  before: Record<string, unknown>;
  after: Record<string, unknown>;
  messageLabel: string;
}): WorkflowDiffItem[] {
  const keys = uniqueStrings([
    ...Object.keys(input.before),
    ...Object.keys(input.after),
  ]);
  const diffItems: WorkflowDiffItem[] = [];

  for (const key of keys) {
    const before = input.before[key];
    const after = input.after[key];
    const path = `${input.pathPrefix}.${key}`;

    if (before === undefined && after !== undefined) {
      diffItems.push(
        diffItem({
          category: input.category,
          changeType: "added",
          path,
          after,
          message: `Workflow ${input.messageLabel} ${key} was added.`,
        }),
      );
      continue;
    }

    if (before !== undefined && after === undefined) {
      diffItems.push(
        diffItem({
          category: input.category,
          changeType: "removed",
          path,
          before,
          message: `Workflow ${input.messageLabel} ${key} was removed.`,
        }),
      );
      continue;
    }

    if (!valuesEqual(before, after)) {
      diffItems.push(
        diffItem({
          category: input.category,
          changeType: "changed",
          path,
          before,
          after,
          message: `Workflow ${input.messageLabel} ${key} changed.`,
        }),
      );
    }
  }

  return diffItems;
}

function diffItem(input: {
  category: WorkflowDiffCategory;
  changeType: WorkflowDiffChangeType;
  path: string;
  message: string;
  before?: unknown;
  after?: unknown;
}): WorkflowDiffItem {
  const reasonCodes = riskReasonCodes(input);
  const riskLevel = riskLevelFromReasonCodes(reasonCodes);

  return {
    category: input.category,
    changeType: input.changeType,
    path: input.path,
    riskLevel,
    message: input.message,
    ...(input.before !== undefined ? { before: input.before } : {}),
    ...(input.after !== undefined ? { after: input.after } : {}),
    reasonCodes,
  };
}

function riskReasonCodes(input: {
  category: WorkflowDiffCategory;
  changeType: WorkflowDiffChangeType;
  path: string;
  before?: unknown;
  after?: unknown;
}): string[] {
  const pathText = input.path.toLowerCase();
  const afterText = stableJsonString(input.after).toLowerCase();
  const beforeText = stableJsonString(input.before).toLowerCase();
  const searchableText = `${pathText} ${afterText} ${beforeText}`;
  const reasonCodes: string[] = [];

  if (input.category === "integration" && input.changeType === "added") {
    reasonCodes.push("new_external_call");
  }

  if (input.category === "approval_gate" && input.changeType === "removed") {
    reasonCodes.push("removed_approval_step");
  }

  if (
    input.category === "permission" &&
    input.changeType !== "removed" &&
    containsSensitiveHint(searchableText)
  ) {
    reasonCodes.push("sensitive_field_exposure");
  }

  if (input.category === "ai_context" && containsSensitiveHint(searchableText)) {
    reasonCodes.push("new_ai_visible_sensitive_field");
  }

  if (searchableText.includes("compensation")) {
    reasonCodes.push("compensation_path_changed");
  }

  if (input.category === "route" && input.changeType === "changed") {
    reasonCodes.push("route_target_changed");
  }

  if (reasonCodes.length === 0 && input.changeType === "changed") {
    reasonCodes.push("configuration_changed");
  }

  if (reasonCodes.length === 0) {
    reasonCodes.push(`configuration_${input.changeType}`);
  }

  return reasonCodes;
}

function riskLevelFromReasonCodes(reasonCodes: string[]): WorkflowAdminRiskLevel {
  const highRiskReasons = new Set([
    "new_external_call",
    "removed_approval_step",
    "sensitive_field_exposure",
    "new_ai_visible_sensitive_field",
    "compensation_path_changed",
  ]);

  if (reasonCodes.some((reasonCode) => highRiskReasons.has(reasonCode))) {
    return "high";
  }

  if (reasonCodes.includes("route_target_changed")) {
    return "medium";
  }

  return "low";
}

function impactSummaryFrom(items: WorkflowDiffItem[]): WorkflowImpactSummary {
  const highRiskCount = items.filter((item) => item.riskLevel === "high").length;
  const mediumRiskCount = items.filter((item) => item.riskLevel === "medium").length;
  const lowRiskCount = items.filter((item) => item.riskLevel === "low").length;

  return {
    riskLevel: highRiskCount > 0 ? "high" : mediumRiskCount > 0 ? "medium" : "low",
    highRiskCount,
    mediumRiskCount,
    lowRiskCount,
    newExternalCallCount: reasonCount(items, "new_external_call"),
    removedApprovalStepCount: reasonCount(items, "removed_approval_step"),
    sensitiveFieldExposureCount: reasonCount(items, "sensitive_field_exposure"),
    compensationPathChangeCount: reasonCount(items, "compensation_path_changed"),
    aiSensitiveFieldExpansionCount: reasonCount(
      items,
      "new_ai_visible_sensitive_field",
    ),
  };
}

function nodesById(
  workflowConfig: WorkflowConfig,
): Map<string, WorkflowGraphNodeConfig> {
  return new Map(
    (workflowConfig.graph?.nodes ?? []).map((node) => {
      return [node.nodeId, node];
    }),
  );
}

function routesByKey(workflowConfig: WorkflowConfig): Record<string, unknown> {
  const routeEntries: Array<[string, unknown]> = [];

  for (const node of workflowConfig.graph?.nodes ?? []) {
    for (const outcome of node.outcomes ?? []) {
      routeEntries.push([
        `${node.nodeId}.${outcome.routeKey ?? outcome.outcome}`,
        outcome,
      ]);
    }
  }

  for (const edge of workflowConfig.graph?.edges ?? []) {
    routeEntries.push([`${edge.fromNodeId}.${edge.routeKey}`, edge]);
  }

  return Object.fromEntries(routeEntries);
}

function approvalGatesByNodeId(
  workflowConfig: WorkflowConfig,
): Record<string, unknown> {
  return Object.fromEntries(
    (workflowConfig.graph?.nodes ?? [])
      .filter((node) => node.type === "approval_gate")
      .map((node) => [node.nodeId, node.approvalGate]),
  );
}

function permissionSummaryByKey(
  workflowConfig: WorkflowConfig,
): Record<string, unknown> {
  const entries: Array<[string, unknown]> = [];

  for (const [state, stateConfig] of Object.entries(workflowConfig.states)) {
    for (const action of stateConfig.actions) {
      entries.push([`states.${state}.${action.transition}`, action.actor]);
    }
  }

  for (const node of workflowConfig.graph?.nodes ?? []) {
    const approvalPermission = node.approval?.resolver.permission;
    if (approvalPermission !== undefined) {
      entries.push([
        `graph.nodes.${node.nodeId}.approval.permission`,
        approvalPermission,
      ]);
    }

    for (const resolver of node.approvalGate?.approverResolvers ?? []) {
      entries.push([
        `graph.nodes.${node.nodeId}.approvalGate.${resolver.resolverId}.permission`,
        resolver.permission,
      ]);
    }
  }

  return Object.fromEntries(entries);
}

function blockRefsByKey(workflowConfig: WorkflowConfig): Record<string, unknown> {
  const entries: Array<[string, unknown]> = [
    ["submit.preflightBlock", workflowConfig.submit.preflightBlock],
    ["plan.block", workflowConfig.plan.block],
  ];

  for (const [blockKey, blockRef] of Object.entries(
    workflowConfig.metadata?.deterministicBlockRefs ?? {},
  )) {
    entries.push([`metadata.deterministicBlockRefs.${blockKey}`, blockRef]);
  }

  for (const node of workflowConfig.graph?.nodes ?? []) {
    if (node.block !== undefined) {
      entries.push([`graph.nodes.${node.nodeId}.block`, node.block]);
    }
  }

  return Object.fromEntries(entries);
}

function integrationRefsByKey(workflowConfig: WorkflowConfig): Record<string, unknown> {
  return Object.fromEntries(
    (workflowConfig.graph?.nodes ?? [])
      .filter((node) => node.connectionId !== undefined || node.operation !== undefined)
      .map((node) => [
        node.nodeId,
        {
          connectionId: node.connectionId,
          operation: node.operation,
          transaction: node.transaction,
        },
      ]),
  );
}

function ledgerRefsByKey(workflowConfig: WorkflowConfig): Record<string, unknown> {
  const entries = Object.entries(workflowConfig.ledger?.events ?? {});

  for (const [eventIndex, eventMapping] of (
    workflowConfig.ledger?.nodeEvents ?? []
  ).entries()) {
    entries.push([`nodeEvents.${eventIndex}`, eventMapping]);
  }

  return Object.fromEntries(entries);
}

function transactionRefsByKey(workflowConfig: WorkflowConfig): Record<string, unknown> {
  return Object.fromEntries(
    (workflowConfig.graph?.nodes ?? [])
      .filter((node) => node.transaction !== undefined)
      .map((node) => [node.nodeId, node.transaction]),
  );
}

function aiVisibilityByKey(workflowConfig: WorkflowConfig): Record<string, unknown> {
  const configRecord = workflowConfig as unknown as Record<string, unknown>;
  const aiReviewScope = configRecord["aiReviewScope"];
  const aiContext = configRecord["aiContext"];

  if (isRecord(aiReviewScope)) {
    return aiReviewScope;
  }

  if (isRecord(aiContext)) {
    return aiContext;
  }

  return {};
}

function containsSensitiveHint(value: string): boolean {
  return sensitiveFieldHints.some((hint) => value.includes(hint));
}

function reasonCount(items: WorkflowDiffItem[], reasonCode: string): number {
  return items.filter((item) => item.reasonCodes.includes(reasonCode)).length;
}

function uniqueStrings(values: string[]): string[] {
  return [...new Set(values)].sort();
}

function valuesEqual(left: unknown, right: unknown): boolean {
  return stableJsonString(left) === stableJsonString(right);
}

function stableJsonString(value: unknown): string {
  if (value === undefined) {
    return "undefined";
  }

  if (Array.isArray(value)) {
    return `[${value.map((item) => stableJsonString(item)).join(",")}]`;
  }

  if (!isRecord(value)) {
    return JSON.stringify(value);
  }

  const entries = Object.keys(value)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableJsonString(value[key])}`);

  return `{${entries.join(",")}}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
