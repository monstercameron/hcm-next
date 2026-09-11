import {
  ok,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type {
  WorkflowConfig,
  WorkflowGraphNodeConfig,
} from "../../shared/workflow-config.js";
import {
  adminError,
  adminWarning,
  workflowAdminValidationReport,
  type WorkflowAdminIssue,
  type WorkflowAdminValidationReport,
} from "../safety/admin-issues.js";

export type IntegrationEnvironment = "sandbox" | "staging" | "production";

export type TenantConnectorBinding = {
  abstractConnectionId: string;
  connectorId: string;
  environment: IntegrationEnvironment;
  secretRef: string;
  enabled: boolean;
  allowedOperations: string[];
  timeoutMs: number;
  retryPolicy: {
    maxAttempts: number;
    backoff: "none" | "fixed" | "linear" | "exponential";
  };
  reconciliation: {
    required: boolean;
    expectedStatusPath?: string;
  };
  idempotencyScope:
    | "workflow_instance"
    | "transaction_plan"
    | "node"
    | "external_operation";
  allowSandboxInProduction?: boolean;
};

export type IntegrationBindingValidationRequest = {
  workflowConfig: WorkflowConfig;
  bindings: TenantConnectorBinding[];
  targetEnvironment: IntegrationEnvironment;
  availableSecretRefs: string[];
};

export type ExternalCallPreview = {
  nodeId: string;
  title: string;
  abstractConnectionId: string;
  connectorId?: string;
  environment?: IntegrationEnvironment;
  operation: string;
  enabled: boolean;
  timeoutMs?: number;
  retryMaxAttempts?: number;
  idempotencyScope?: TenantConnectorBinding["idempotencyScope"];
  secretRefStatus: "bound" | "missing" | "not_configured";
};

export type IntegrationBindingValidationResult = WorkflowAdminValidationReport & {
  requiredConnectionIds: string[];
  externalCallPreviews: ExternalCallPreview[];
};

/**
 * Validates that workflow external-write nodes are safely bound to tenant connectors.
 */
export function validateIntegrationBindings(
  input: IntegrationBindingValidationRequest,
): Result<IntegrationBindingValidationResult, AppError> {
  const issues: WorkflowAdminIssue[] = [];
  const bindingsByConnectionId = new Map(
    input.bindings.map((binding) => [binding.abstractConnectionId, binding]),
  );
  const externalWriteNodes = externalWriteNodesFrom(input.workflowConfig);
  const requiredConnectionIds = uniqueStrings(
    externalWriteNodes.flatMap((node) =>
      node.connectionId === undefined ? [] : [node.connectionId],
    ),
  );
  const externalCallPreviews = externalWriteNodes.map((node) =>
    validateExternalWriteNode({
      node,
      bindingsByConnectionId,
      targetEnvironment: input.targetEnvironment,
      availableSecretRefs: input.availableSecretRefs,
      issues,
    }),
  );

  return ok({
    ...workflowAdminValidationReport(issues),
    requiredConnectionIds,
    externalCallPreviews,
  });
}

function validateExternalWriteNode(input: {
  node: WorkflowGraphNodeConfig;
  bindingsByConnectionId: Map<string, TenantConnectorBinding>;
  targetEnvironment: IntegrationEnvironment;
  availableSecretRefs: string[];
  issues: WorkflowAdminIssue[];
}): ExternalCallPreview {
  const nodePath = `graph.nodes.${input.node.nodeId}`;
  const connectionId = input.node.connectionId;
  const operation = input.node.operation;

  if (connectionId === undefined || connectionId.trim().length === 0) {
    input.issues.push(
      adminError({
        code: "integration_binding.connection_id_missing",
        path: `${nodePath}.connectionId`,
        message: `External write node ${input.node.nodeId} needs a connection ID.`,
        suggestedRepair: "Set connectionId to an abstract workflow connection.",
      }),
    );
  }

  if (operation === undefined || operation.trim().length === 0) {
    input.issues.push(
      adminError({
        code: "integration_binding.operation_missing",
        path: `${nodePath}.operation`,
        message: `External write node ${input.node.nodeId} needs an operation.`,
        suggestedRepair: "Set operation to a connector capability.",
      }),
    );
  }

  const binding =
    connectionId === undefined
      ? undefined
      : input.bindingsByConnectionId.get(connectionId);

  if (connectionId !== undefined && binding === undefined) {
    input.issues.push(
      adminError({
        code: "integration_binding.missing",
        path: `${nodePath}.connectionId`,
        message: `Connection ${connectionId} has no tenant binding.`,
        suggestedRepair:
          "Create a tenant connector binding for this abstract connection.",
      }),
    );
  }

  if (binding !== undefined) {
    validateBindingForNode({
      binding,
      node: input.node,
      targetEnvironment: input.targetEnvironment,
      availableSecretRefs: input.availableSecretRefs,
      issues: input.issues,
    });
  }

  return {
    nodeId: input.node.nodeId,
    title: input.node.title,
    abstractConnectionId: connectionId ?? "missing",
    ...(binding !== undefined ? { connectorId: binding.connectorId } : {}),
    ...(binding !== undefined ? { environment: binding.environment } : {}),
    operation: operation ?? "missing",
    enabled: binding?.enabled ?? false,
    ...(binding !== undefined ? { timeoutMs: binding.timeoutMs } : {}),
    ...(binding !== undefined
      ? { retryMaxAttempts: binding.retryPolicy.maxAttempts }
      : {}),
    ...(binding !== undefined ? { idempotencyScope: binding.idempotencyScope } : {}),
    secretRefStatus: secretRefStatus(binding, input.availableSecretRefs),
  };
}

function validateBindingForNode(input: {
  binding: TenantConnectorBinding;
  node: WorkflowGraphNodeConfig;
  targetEnvironment: IntegrationEnvironment;
  availableSecretRefs: string[];
  issues: WorkflowAdminIssue[];
}): void {
  const bindingPath = `integrations.${input.binding.abstractConnectionId}`;

  if (!input.binding.enabled) {
    input.issues.push(
      adminError({
        code: "integration_binding.disabled",
        path: `${bindingPath}.enabled`,
        message: `Connector ${input.binding.connectorId} is disabled.`,
        suggestedRepair:
          "Enable the connector or bind this workflow to an enabled connector.",
      }),
    );
  }

  if (
    input.node.operation !== undefined &&
    !input.binding.allowedOperations.includes(input.node.operation)
  ) {
    input.issues.push(
      adminError({
        code: "integration_binding.operation_not_allowed",
        path: `${bindingPath}.allowedOperations`,
        message: `Connector ${input.binding.connectorId} does not allow ${input.node.operation}.`,
        suggestedRepair:
          "Add the operation capability or choose another connector binding.",
      }),
    );
  }

  if (!input.availableSecretRefs.includes(input.binding.secretRef)) {
    input.issues.push(
      adminError({
        code: "integration_binding.secret_missing",
        path: `${bindingPath}.secretRef`,
        message: `Secret reference ${input.binding.secretRef} is not available.`,
        suggestedRepair: "Create the secret reference before publishing.",
      }),
    );
  }

  if (
    input.targetEnvironment === "production" &&
    input.binding.environment === "sandbox" &&
    input.binding.allowSandboxInProduction !== true
  ) {
    input.issues.push(
      adminError({
        code: "integration_binding.production_points_to_sandbox",
        path: `${bindingPath}.environment`,
        message: "Production workflow publish cannot point to a sandbox connector.",
        suggestedRepair:
          "Bind to a production connector or explicitly allow sandbox binding for this demo.",
      }),
    );
  }

  if (input.binding.timeoutMs <= 0) {
    input.issues.push(
      adminError({
        code: "integration_binding.timeout_invalid",
        path: `${bindingPath}.timeoutMs`,
        message: "Connector timeout must be greater than zero.",
        suggestedRepair: "Set a positive timeout in milliseconds.",
      }),
    );
  }

  if (!input.binding.reconciliation.required) {
    input.issues.push(
      adminWarning({
        code: "integration_binding.reconciliation_not_required",
        path: `${bindingPath}.reconciliation.required`,
        message: "External writes should usually define reconciliation expectations.",
        suggestedRepair:
          "Set reconciliation.required to true when the external system can confirm state.",
      }),
    );
  }
}

function externalWriteNodesFrom(
  workflowConfig: WorkflowConfig,
): WorkflowGraphNodeConfig[] {
  return (workflowConfig.graph?.nodes ?? []).filter((node) => {
    return (
      node.type === "external_write" ||
      node.connectionId !== undefined ||
      node.operation !== undefined
    );
  });
}

function secretRefStatus(
  binding: TenantConnectorBinding | undefined,
  availableSecretRefs: string[],
): ExternalCallPreview["secretRefStatus"] {
  if (binding === undefined) {
    return "not_configured";
  }

  return availableSecretRefs.includes(binding.secretRef) ? "bound" : "missing";
}

function uniqueStrings(values: string[]): string[] {
  return [...new Set(values)].sort();
}
