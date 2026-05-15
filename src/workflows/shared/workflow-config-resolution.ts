import {
  ERROR_CODES,
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
  environmentId: string,
  intent: string,
): Result<ResolvedWorkflowConfig, AppError> {
  const adminConfigResult = resolveCurrentAdminWorkflowConfig(
    repositories,
    tenantId,
    environmentId,
    intent,
  );
  if (adminConfigResult.ok) {
    return adminConfigResult;
  }
  if (adminConfigResult.error.code !== ERROR_CODES.NOT_FOUND) {
    return adminConfigResult;
  }

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
    if (workflowVersionResult.error.code !== ERROR_CODES.NOT_FOUND) {
      return workflowVersionResult;
    }

    return resolvePinnedAdminWorkflowConfig(repositories, workflowInstance);
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

function resolveCurrentAdminWorkflowConfig(
  repositories: Repositories,
  tenantId: string,
  environmentId: string,
  intent: string,
): Result<ResolvedWorkflowConfig, AppError> {
  const activeVersionResult = repositories.workflowAdmin.findActiveVersionByIntent({
    tenantId,
    environmentId,
    intent,
  });
  if (!activeVersionResult.ok) {
    return activeVersionResult;
  }

  const workflowConfigResult = workflowConfigFromGraphDefinition(
    activeVersionResult.value.version.configJson,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  return ok({
    workflowDefinitionId: activeVersionResult.value.family.workflowFamilyId,
    workflowVersionId: activeVersionResult.value.version.workflowVersionRecordId,
    workflowConfig: workflowConfigResult.value,
  });
}

function resolvePinnedAdminWorkflowConfig(
  repositories: Repositories,
  workflowInstance: WorkflowInstanceRecord,
): Result<WorkflowConfig, AppError> {
  const workflowVersionResult = repositories.workflowAdmin.findVersionById({
    tenantId: workflowInstance.tenantId,
    workflowVersionRecordId: workflowInstance.workflowVersionId,
  });
  if (!workflowVersionResult.ok) {
    return workflowVersionResult;
  }

  if (
    workflowVersionResult.value.workflowFamilyId !==
    workflowInstance.workflowDefinitionId
  ) {
    return err(
      validationFailedError({
        workflowInstanceId: workflowInstance.workflowInstanceId,
        workflowDefinitionId: workflowInstance.workflowDefinitionId,
        workflowVersionFamilyId: workflowVersionResult.value.workflowFamilyId,
      }),
    );
  }

  return workflowConfigFromGraphDefinition(workflowVersionResult.value.configJson);
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
