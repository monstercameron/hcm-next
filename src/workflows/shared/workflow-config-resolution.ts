import {
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  Repositories,
  WorkflowInstanceRecord,
  WorkflowVersionRecord,
} from "@hcm-next/data-store";
import { stringField } from "./json-fields.js";
import type { WorkflowConfig } from "./workflow-config.js";
import {
  findFilesystemWorkflowConfigByIntent,
  workflowConfigFromGraphDefinition,
} from "./workflow-config-registry.js";

export type ResolvedWorkflowConfig = {
  workflowDefinitionId: string;
  workflowVersionId: string;
  workflowConfig: WorkflowConfig;
};

/**
 * Resolves the current published config for a tenant and workflow intent.
 */
export function resolveCurrentPublishedWorkflowConfig(
  repositories: Repositories,
  tenantId: string,
  intent: string,
): Result<ResolvedWorkflowConfig, AppError> {
  const workflowVersionResult =
    repositories.workflows.findCurrentPublishedVersionByIntent(tenantId, intent);
  if (!workflowVersionResult.ok) {
    return workflowVersionResult;
  }

  const workflowConfigResult = resolveWorkflowConfigFromVersion(
    workflowVersionResult.value.version,
    intent,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  return ok({
    workflowDefinitionId: workflowVersionResult.value.definition.workflowDefinitionId,
    workflowVersionId: workflowVersionResult.value.version.workflowVersionId,
    workflowConfig: workflowConfigResult.value,
  });
}

/**
 * Resolves the executable config pinned to an existing workflow instance.
 */
export function resolvePinnedWorkflowConfig(
  repositories: Repositories,
  workflowInstance: WorkflowInstanceRecord,
): Result<WorkflowConfig, AppError> {
  const workflowVersionResult = repositories.workflows.findVersionById(
    workflowInstance.tenantId,
    workflowInstance.workflowVersionId,
  );
  if (!workflowVersionResult.ok) {
    return workflowVersionResult;
  }

  if (
    workflowVersionResult.value.workflowDefinitionId !==
    workflowInstance.workflowDefinitionId
  ) {
    return err(
      validationFailedError({
        workflowInstanceId: workflowInstance.workflowInstanceId,
        workflowDefinitionId: workflowInstance.workflowDefinitionId,
        workflowVersionDefinitionId: workflowVersionResult.value.workflowDefinitionId,
      }),
    );
  }

  return resolveWorkflowConfigFromVersion(
    workflowVersionResult.value,
    workflowInstance.intent,
  );
}

/**
 * Detects whether workflow start needs employee projection context.
 */
export function shouldLoadEmployeeProjectionForStart(
  workflowConfig: WorkflowConfig,
): boolean {
  const inputInteraction = workflowConfig.interactions["input"];

  return (
    workflowConfig.subjectType === "worker" ||
    (inputInteraction?.employeeContext?.length ?? 0) > 0
  );
}

function resolveWorkflowConfigFromVersion(
  workflowVersion: WorkflowVersionRecord,
  intent: string,
): Result<WorkflowConfig, AppError> {
  const persistedConfigResult = workflowConfigFromGraphDefinition(
    workflowVersion.graphDefinition,
  );

  if (persistedConfigResult.ok) {
    if (persistedConfigResult.value.intent !== intent) {
      return err(
        validationFailedError({
          workflowVersionId: workflowVersion.workflowVersionId,
          expectedIntent: intent,
          actualIntent: persistedConfigResult.value.intent,
        }),
      );
    }

    return ok(persistedConfigResult.value);
  }

  if (isLegacySeededPartialWorkflowConfig(workflowVersion.graphDefinition, intent)) {
    const workflowConfigResult = findFilesystemWorkflowConfigByIntent(intent);
    if (!workflowConfigResult.ok) {
      return workflowConfigResult;
    }

    return ok(workflowConfigResult.value);
  }

  return persistedConfigResult;
}

function isLegacySeededPartialWorkflowConfig(
  graphDefinition: Record<string, unknown>,
  intent: string,
): boolean {
  const legacySeedKeys = new Set(["intent", "initialState"]);

  return (
    stringField(graphDefinition, "intent") === intent &&
    stringField(graphDefinition, "initialState") !== undefined &&
    Object.keys(graphDefinition).every((key) => legacySeedKeys.has(key))
  );
}
