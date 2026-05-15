import {
  LEDGER_EVENT_TYPES,
  PERMISSION_KEYS,
  ok,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  WorkflowConfig,
  WorkflowGraphNodeConfig,
} from "../../shared/workflow-config.js";
import { validateWorkflowConfig } from "../../shared/workflow-config-validation.js";
import { diffWorkflowConfigs, type WorkflowDiffReview } from "../diff/workflow-diff.js";
import {
  validateIntegrationBindings,
  type IntegrationBindingValidationRequest,
  type IntegrationBindingValidationResult,
} from "../integrations/integration-bindings.js";
import {
  validateApprovalGateAdminConfig,
  type ApprovalGateResolverFixture,
  type ApprovalGateValidationResult,
} from "../safety/approval-gate-validation.js";
import {
  adminError,
  adminWarning,
  workflowAdminValidationReport,
  type WorkflowAdminIssue,
  type WorkflowAdminValidationReport,
} from "../safety/admin-issues.js";
import type { WorkflowSimulationResult } from "../simulation/workflow-simulator.js";

export type PublishGuardrailBlockReference = {
  name: string;
  version: string;
};

export type PublishGuardrailRequest = {
  workflowConfig: WorkflowConfig;
  publishedConfig?: WorkflowConfig | undefined;
  availableBlocks?: PublishGuardrailBlockReference[] | undefined;
  integrationBindings?:
    | Omit<IntegrationBindingValidationRequest, "workflowConfig">
    | undefined;
  approvalGateResolverFixture?: ApprovalGateResolverFixture | undefined;
  simulationResult?: WorkflowSimulationResult | undefined;
  adminApproval?:
    | {
        approved: boolean;
        approverActorId: string;
      }
    | undefined;
  requireSimulationForHighRisk?: boolean | undefined;
};

export type PublishGuardrailResult = WorkflowAdminValidationReport & {
  canPublish: boolean;
  diff?: WorkflowDiffReview;
  integrationValidation?: IntegrationBindingValidationResult;
  approvalGateValidations: ApprovalGateValidationResult[];
};

const knownPermissionKeys = new Set<string>(Object.values(PERMISSION_KEYS));
const knownLedgerEventTypes = new Set<string>(Object.values(LEDGER_EVENT_TYPES));
const sensitiveFieldHints = [
  "compensation",
  "salary",
  "bonus",
  "contact",
  "emergency",
  "medical",
  "nationalid",
  "governmentid",
] as const;

/**
 * Aggregates all publish blockers into one review result for Workflow Admin.
 */
export function runPublishGuardrails(
  input: PublishGuardrailRequest,
): Result<PublishGuardrailResult, AppError> {
  const issues: WorkflowAdminIssue[] = [];
  const schemaValidation = validateWorkflowConfig(input.workflowConfig);
  const diffReviewResult =
    input.publishedConfig === undefined
      ? undefined
      : diffWorkflowConfigs({
          draftConfig: input.workflowConfig,
          publishedConfig: input.publishedConfig,
        });
  const diffResult =
    diffReviewResult !== undefined && diffReviewResult.ok
      ? diffReviewResult.value
      : undefined;

  for (const schemaIssue of schemaValidation.errors) {
    issues.push(
      adminError({
        code: schemaIssue.code,
        path: schemaIssue.path,
        message: schemaIssue.message,
        suggestedRepair: "Fix the workflow config schema error before publishing.",
      }),
    );
  }

  for (const schemaIssue of schemaValidation.warnings) {
    issues.push(
      adminWarning({
        code: schemaIssue.code,
        path: schemaIssue.path,
        message: schemaIssue.message,
        suggestedRepair: "Review this workflow config warning before publishing.",
      }),
    );
  }

  validateBlockReferences({
    workflowConfig: input.workflowConfig,
    availableBlocks: input.availableBlocks,
    issues,
  });
  validateGraphSafety(input.workflowConfig, issues);
  validatePermissionReferences(input.workflowConfig, issues);
  validateFieldVisibility(input.workflowConfig, issues);
  validateLedgerMappings(input.workflowConfig, issues);
  validateAiContextExpansion(diffResult, issues);

  const approvalGateValidations = validateApprovalGatesForPublish({
    workflowConfig: input.workflowConfig,
    resolverFixture: input.approvalGateResolverFixture,
    issues,
  });
  const integrationValidation = validateIntegrationsForPublish({
    input,
    issues,
  });

  validateHighRiskPolicy({
    diff: diffResult,
    simulationResult: input.simulationResult,
    adminApproval: input.adminApproval,
    requireSimulationForHighRisk: input.requireSimulationForHighRisk ?? true,
    issues,
  });

  const report = workflowAdminValidationReport(issues);

  return ok({
    ...report,
    canPublish: report.valid,
    ...(diffResult !== undefined ? { diff: diffResult } : {}),
    ...(integrationValidation !== undefined ? { integrationValidation } : {}),
    approvalGateValidations,
  });
}

function validateFieldVisibility(
  workflowConfig: WorkflowConfig,
  issues: WorkflowAdminIssue[],
): void {
  for (const [interactionKey, interaction] of Object.entries(
    workflowConfig.interactions,
  )) {
    for (const contextItem of interaction.employeeContext ?? []) {
      const path = contextItem.path.toLowerCase();
      const isSensitive = sensitiveFieldHints.some((hint) => path.includes(hint));
      const visibility = contextItem.visibility;
      const hasVisibilityRule =
        (visibility?.permissions?.length ?? 0) > 0 ||
        (visibility?.fieldGroups?.length ?? 0) > 0 ||
        (visibility?.actors?.length ?? 0) > 0;

      if (isSensitive && !hasVisibilityRule) {
        issues.push(
          adminError({
            code: "publish_guardrail.field_visibility_missing",
            path: `interactions.${interactionKey}.employeeContext.${contextItem.outputKey}`,
            message: `Sensitive employee context path ${contextItem.path} has no visibility rule.`,
            suggestedRepair:
              "Add fieldGroups, permissions, or actor visibility to the interaction context item.",
          }),
        );
      }
    }
  }
}

function validateBlockReferences(input: {
  workflowConfig: WorkflowConfig;
  availableBlocks: PublishGuardrailBlockReference[] | undefined;
  issues: WorkflowAdminIssue[];
}): void {
  if (input.availableBlocks === undefined) {
    input.issues.push(
      adminWarning({
        code: "publish_guardrail.block_catalog_missing",
        path: "blocks",
        message:
          "Publish guardrails could not verify block references without a block catalog.",
        suggestedRepair: "Pass the current block catalog into publish guardrails.",
      }),
    );
    return;
  }

  const availableBlockKeys = new Set(
    input.availableBlocks.map((block) => `${block.name}@${block.version}`),
  );

  for (const blockRef of collectBlockReferences(input.workflowConfig)) {
    const blockKey = `${blockRef.name}@${blockRef.version}`;

    if (!availableBlockKeys.has(blockKey)) {
      input.issues.push(
        adminError({
          code: "publish_guardrail.block_missing",
          path: blockRef.path,
          message: `Workflow references unavailable block ${blockKey}.`,
          suggestedRepair: "Register the block version or update the workflow config.",
        }),
      );
    }
  }
}

function validateGraphSafety(
  workflowConfig: WorkflowConfig,
  issues: WorkflowAdminIssue[],
): void {
  const graph = workflowConfig.graph;

  if (graph === undefined) {
    issues.push(
      adminError({
        code: "publish_guardrail.graph_missing",
        path: "graph",
        message: "Workflow publish requires a graph.",
        suggestedRepair: "Add graph.startNodeId and graph.nodes.",
      }),
    );
    return;
  }

  const nodeIds = new Set(graph.nodes.map((node) => node.nodeId));
  const reachableNodeIds = reachableNodesFrom(workflowConfig);
  const requiredUnreachableNodes = graph.nodes
    .filter((node) => node.type !== "terminal")
    .filter((node) => !reachableNodeIds.has(node.nodeId));
  const terminalReachable = graph.nodes.some((node) => {
    return node.type === "terminal" && reachableNodeIds.has(node.nodeId);
  });

  for (const node of requiredUnreachableNodes) {
    issues.push(
      adminError({
        code: "publish_guardrail.required_node_unreachable",
        path: `graph.nodes.${node.nodeId}`,
        message: `Required node ${node.nodeId} is unreachable from the start node.`,
        suggestedRepair: "Add a route to this node or remove it from the graph.",
      }),
    );
  }

  if (!terminalReachable) {
    issues.push(
      adminError({
        code: "publish_guardrail.terminal_route_missing",
        path: "graph",
        message: "Workflow graph does not have a reachable terminal route.",
        suggestedRepair:
          "Route at least one successful or failed path to a terminal node.",
      }),
    );
  }

  for (const node of graph.nodes) {
    for (const outcome of node.outcomes ?? []) {
      if (outcome.nextNodeId !== undefined && !nodeIds.has(outcome.nextNodeId)) {
        issues.push(
          adminError({
            code: "publish_guardrail.route_target_missing",
            path: `graph.nodes.${node.nodeId}.outcomes.${outcome.outcome}`,
            message: `Route from ${node.nodeId} points to missing node ${outcome.nextNodeId}.`,
            suggestedRepair: "Point the route to an existing node or terminal outcome.",
          }),
        );
      }
    }
  }
}

function validatePermissionReferences(
  workflowConfig: WorkflowConfig,
  issues: WorkflowAdminIssue[],
): void {
  for (const node of workflowConfig.graph?.nodes ?? []) {
    const approvalPermission = node.approval?.resolver.permission;

    if (
      approvalPermission !== undefined &&
      !knownPermissionKeys.has(approvalPermission)
    ) {
      issues.push(
        adminError({
          code: "publish_guardrail.permission_unknown",
          path: `graph.nodes.${node.nodeId}.approval.resolver.permission`,
          message: `Approval node ${node.nodeId} references unknown permission ${approvalPermission}.`,
          suggestedRepair:
            "Use a shared permission constant or add the permission to the platform.",
        }),
      );
    }

    for (const resolver of node.approvalGate?.approverResolvers ?? []) {
      if (!knownPermissionKeys.has(resolver.permission)) {
        issues.push(
          adminError({
            code: "publish_guardrail.permission_unknown",
            path: `graph.nodes.${node.nodeId}.approvalGate.${resolver.resolverId}.permission`,
            message: `Approval gate resolver ${resolver.resolverId} references unknown permission ${resolver.permission}.`,
            suggestedRepair:
              "Use a shared permission constant or add the permission to the platform.",
          }),
        );
      }
    }
  }
}

function validateLedgerMappings(
  workflowConfig: WorkflowConfig,
  issues: WorkflowAdminIssue[],
): void {
  const ledgerMappings = [
    ...Object.values(workflowConfig.ledger?.events ?? {}),
    ...(workflowConfig.ledger?.nodeEvents ?? []),
  ];

  for (const [mappingIndex, mapping] of ledgerMappings.entries()) {
    if (!knownLedgerEventTypes.has(mapping.eventType)) {
      issues.push(
        adminError({
          code: "publish_guardrail.ledger_event_unknown",
          path: `ledger.${mappingIndex}.eventType`,
          message: `Workflow references unknown ledger event ${mapping.eventType}.`,
          suggestedRepair: "Use a shared ledger event constant or add the event type.",
        }),
      );
    }
  }

  for (const node of workflowConfig.graph?.nodes ?? []) {
    for (const outcome of node.outcomes ?? []) {
      if (outcome.eventType === undefined) {
        continue;
      }

      if (!knownLedgerEventTypes.has(outcome.eventType)) {
        issues.push(
          adminError({
            code: "publish_guardrail.ledger_event_unknown",
            path: `graph.nodes.${node.nodeId}.outcomes.${outcome.outcome}.eventType`,
            message: `Outcome ${node.nodeId}.${outcome.outcome} references unknown ledger event ${outcome.eventType}.`,
            suggestedRepair:
              "Use a shared ledger event constant or add the event type.",
          }),
        );
      }
    }
  }
}

function validateAiContextExpansion(
  diff: WorkflowDiffReview | undefined,
  issues: WorkflowAdminIssue[],
): void {
  if (diff === undefined || diff.impact.aiSensitiveFieldExpansionCount === 0) {
    return;
  }

  issues.push(
    adminError({
      code: "publish_guardrail.unsafe_ai_context_expansion",
      path: "aiReviewScope",
      message: "Draft expands AI visibility to sensitive fields.",
      suggestedRepair:
        "Review field-level permissions and require explicit admin approval before publishing.",
      details: {
        aiSensitiveFieldExpansionCount: diff.impact.aiSensitiveFieldExpansionCount,
      },
    }),
  );
}

function validateApprovalGatesForPublish(input: {
  workflowConfig: WorkflowConfig;
  resolverFixture: ApprovalGateResolverFixture | undefined;
  issues: WorkflowAdminIssue[];
}): ApprovalGateValidationResult[] {
  const approvalGateValidations: ApprovalGateValidationResult[] = [];

  for (const node of input.workflowConfig.graph?.nodes ?? []) {
    if (node.approvalGate === undefined) {
      continue;
    }

    const validationResult = validateApprovalGateAdminConfig({
      gateConfig: node.approvalGate,
      ...(input.workflowConfig.graph !== undefined
        ? { graph: input.workflowConfig.graph }
        : {}),
      ...(input.resolverFixture !== undefined
        ? { resolverFixture: input.resolverFixture }
        : {}),
      path: `graph.nodes.${node.nodeId}.approvalGate`,
    });

    if (!validationResult.ok) {
      continue;
    }

    approvalGateValidations.push(validationResult.value);
    input.issues.push(
      ...validationResult.value.errors,
      ...validationResult.value.warnings,
    );
  }

  return approvalGateValidations;
}

function validateIntegrationsForPublish(input: {
  input: PublishGuardrailRequest;
  issues: WorkflowAdminIssue[];
}): IntegrationBindingValidationResult | undefined {
  const externalWriteNodes = (input.input.workflowConfig.graph?.nodes ?? []).filter(
    isExternalWriteNode,
  );

  if (externalWriteNodes.length === 0) {
    return undefined;
  }

  if (input.input.integrationBindings === undefined) {
    input.issues.push(
      adminError({
        code: "publish_guardrail.integration_bindings_missing",
        path: "integrations",
        message:
          "Workflow has external writes but no integration binding review was supplied.",
        suggestedRepair: "Run integration binding validation before publish.",
      }),
    );
    return undefined;
  }

  const integrationResult = validateIntegrationBindings({
    workflowConfig: input.input.workflowConfig,
    ...input.input.integrationBindings,
  });

  if (!integrationResult.ok) {
    return undefined;
  }

  input.issues.push(
    ...integrationResult.value.errors,
    ...integrationResult.value.warnings,
  );

  return integrationResult.value;
}

function validateHighRiskPolicy(input: {
  diff: WorkflowDiffReview | undefined;
  simulationResult: WorkflowSimulationResult | undefined;
  adminApproval: PublishGuardrailRequest["adminApproval"];
  requireSimulationForHighRisk: boolean;
  issues: WorkflowAdminIssue[];
}): void {
  const hasHighRiskDiff =
    input.diff !== undefined && input.diff.impact.riskLevel === "high";

  if (!hasHighRiskDiff) {
    return;
  }

  if (
    input.requireSimulationForHighRisk &&
    (input.simulationResult === undefined ||
      input.simulationResult.validationErrors.length > 0)
  ) {
    input.issues.push(
      adminError({
        code: "publish_guardrail.high_risk_simulation_required",
        path: "simulation",
        message: "High-risk workflow changes require a successful simulation.",
        suggestedRepair:
          "Run simulator fixtures and clear validation errors before publishing.",
      }),
    );
  }

  if (input.adminApproval?.approved !== true) {
    input.issues.push(
      adminError({
        code: "publish_guardrail.high_risk_admin_approval_required",
        path: "publishApproval",
        message: "High-risk workflow changes require admin approval.",
        suggestedRepair: "Record explicit publish approval from an authorized admin.",
      }),
    );
  }
}

function collectBlockReferences(
  workflowConfig: WorkflowConfig,
): Array<PublishGuardrailBlockReference & { path: string }> {
  const blockReferences: Array<PublishGuardrailBlockReference & { path: string }> = [
    { ...workflowConfig.submit.preflightBlock, path: "submit.preflightBlock" },
    { ...workflowConfig.plan.block, path: "plan.block" },
  ];

  for (const [blockKey, blockRef] of Object.entries(
    workflowConfig.metadata?.deterministicBlockRefs ?? {},
  )) {
    blockReferences.push({
      ...blockRef,
      path: `metadata.deterministicBlockRefs.${blockKey}`,
    });
  }

  for (const node of workflowConfig.graph?.nodes ?? []) {
    if (node.block !== undefined) {
      blockReferences.push({
        ...node.block,
        path: `graph.nodes.${node.nodeId}.block`,
      });
    }
  }

  return blockReferences;
}

function reachableNodesFrom(workflowConfig: WorkflowConfig): Set<string> {
  const graph = workflowConfig.graph;
  const reachableNodeIds = new Set<string>();

  if (graph === undefined) {
    return reachableNodeIds;
  }

  const nodesById = new Map(
    graph.nodes.map((node) => {
      return [node.nodeId, node];
    }),
  );
  const queue = [graph.startNodeId];

  while (queue.length > 0) {
    const nodeId = queue.shift();

    if (nodeId === undefined || reachableNodeIds.has(nodeId)) {
      continue;
    }

    reachableNodeIds.add(nodeId);
    const node = nodesById.get(nodeId);

    for (const outcome of node?.outcomes ?? []) {
      if (outcome.nextNodeId !== undefined) {
        queue.push(outcome.nextNodeId);
      }
    }
  }

  return reachableNodeIds;
}

function isExternalWriteNode(node: WorkflowGraphNodeConfig): boolean {
  return (
    node.type === "external_write" ||
    node.connectionId !== undefined ||
    node.operation !== undefined
  );
}
