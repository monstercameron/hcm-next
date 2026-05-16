import {
  ACTOR_ROLES,
  err,
  notFoundError,
  ok,
  permissionDeniedError,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  makeId,
  nowIso,
  type ActorRecord,
  type Repositories,
  type WorkflowDefinitionRecord,
  type WorkflowVersionRecord,
} from "@hcm-next/data-store";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import type { WorkflowConfig } from "../shared/workflow-config.js";
import {
  cloneWorkflowConfig,
  isWorkflowConfigRecord,
  listFilesystemWorkflowConfigs,
  workflowConfigFromGraphDefinition,
  workflowConfigHash,
} from "../shared/workflow-config-registry.js";
import {
  validateWorkflowConfig,
  type WorkflowValidationIssue,
} from "../shared/workflow-config-validation.js";

const administrativeRoles = new Set<string>([
  ACTOR_ROLES.HR_ADMIN,
  ACTOR_ROLES.FINANCE_ADMIN,
  ACTOR_ROLES.COMPENSATION_ADMIN,
  ACTOR_ROLES.SYSTEM,
]);

type WorkflowValidationOutput = {
  valid: boolean;
  errors: WorkflowValidationIssue[];
  warnings: WorkflowValidationIssue[];
  configHash: string;
};

/**
 * Lists workflow definitions and their registered versions for admin review.
 */
export function listWorkflowAdminDefinitions(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    dependencies.logger?.warn("workflow admin list authorization denied", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      errorCode: authorizationResult.error.code,
    });
    return authorizationResult;
  }

  return ok({
    workflows: workflowRegistrySummaries(
      dependencies.repositories,
      requestContext.tenantId,
    ),
  });
}

/**
 * Imports checked-in workflow JSON configs into the registry.
 */
export function importWorkflowConfigsFromFiles(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  body: Record<string, unknown> = {},
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    dependencies.logger?.warn("workflow configs file import authorization denied", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      errorCode: authorizationResult.error.code,
    });
    return authorizationResult;
  }

  if (process.env["NODE_ENV"] === "production") {
    dependencies.logger?.warn("workflow configs file import blocked in production", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
    });
    return err(
      validationFailedError({
        route: "POST /admin/workflows/import-from-files",
        reason: "Filesystem workflow imports are disabled in production.",
      }),
    );
  }

  const summary = createImportSummary();
  const shouldPublishImportedVersions = booleanField(body, "publish") ?? true;

  dependencies.logger?.info("workflow configs file import started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    publish: shouldPublishImportedVersions,
  });

  for (const workflowConfig of listFilesystemWorkflowConfigs()) {
    const importResult = importWorkflowConfigIntoRegistry({
      repositories: dependencies.repositories,
      tenantId: requestContext.tenantId,
      actorId: requestContext.actor.actorId,
      workflowConfig,
      source: "files",
      publishCurrent: shouldPublishImportedVersions,
    });

    if (!importResult.ok) {
      dependencies.logger?.warn("workflow config file import rejected", {
        intent: workflowConfig.intent,
        errorCode: importResult.error.code,
      });
      summary.rejected.push({
        intent: workflowConfig.intent,
        reason: importResult.error.safeMessage,
      });
      continue;
    }

    recordImportOutcome(summary, importResult.value);
  }

  dependencies.logger?.info("workflow configs file import completed", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    imported: summary.imported,
    skipped: summary.skipped,
    published: summary.published,
    rejected: summary.rejected.length,
  });

  return ok(summary);
}

/**
 * Imports one admin-provided workflow config payload into the registry.
 */
export function importWorkflowConfigPayload(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    dependencies.logger?.warn("workflow config payload import authorization denied", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      errorCode: authorizationResult.error.code,
    });
    return authorizationResult;
  }

  const workflowConfigResult = readWorkflowConfigPayload(body);
  if (!workflowConfigResult.ok) {
    dependencies.logger?.warn("workflow config payload parse failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      errorCode: workflowConfigResult.error.code,
    });
    return workflowConfigResult;
  }

  const publish = booleanField(body, "publish") ?? false;
  dependencies.logger?.info("workflow config payload import started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    intent: workflowConfigResult.value.intent,
    publish,
  });

  const importResult = importWorkflowConfigIntoRegistry({
    repositories: dependencies.repositories,
    tenantId: requestContext.tenantId,
    actorId: requestContext.actor.actorId,
    workflowConfig: workflowConfigResult.value,
    source: "api",
    publishCurrent: publish,
  });
  if (!importResult.ok) {
    dependencies.logger?.warn("workflow config payload import failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      intent: workflowConfigResult.value.intent,
      errorCode: importResult.error.code,
    });
    return importResult;
  }

  dependencies.logger?.info("workflow config payload import completed", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    intent: workflowConfigResult.value.intent,
    workflowVersionId: importResult.value.workflowVersionId,
    workflowDefinitionId: importResult.value.workflowDefinitionId,
    imported: importResult.value.imported,
    skipped: importResult.value.skipped,
    published: importResult.value.published,
  });

  return ok({
    imported: importResult.value.imported ? 1 : 0,
    skipped: importResult.value.skipped ? 1 : 0,
    published: importResult.value.published ? 1 : 0,
    rejected: 0,
    workflowDefinitionId: importResult.value.workflowDefinitionId,
    workflowVersionId: importResult.value.workflowVersionId,
    configHash: importResult.value.configHash,
  });
}

/**
 * Validates a workflow version and persists the validation result on the version.
 */
export function validateWorkflowVersion(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowVersionId: string,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    dependencies.logger?.warn("workflow version validation authorization denied", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowVersionId,
      errorCode: authorizationResult.error.code,
    });
    return authorizationResult;
  }

  dependencies.logger?.info("workflow version validation started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowVersionId,
  });

  const workflowVersionResult = findWorkflowVersion(
    dependencies.repositories,
    requestContext.tenantId,
    workflowVersionId,
  );
  if (!workflowVersionResult.ok) {
    dependencies.logger?.warn("workflow version not found for validation", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowVersionId,
      errorCode: workflowVersionResult.error.code,
    });
    return workflowVersionResult;
  }

  const validation = validateWorkflowConfigRecord(
    workflowVersionResult.value.graphDefinition,
  );
  const updatedVersion: WorkflowVersionRecord = {
    ...workflowVersionResult.value,
    status: validation.valid ? workflowVersionResult.value.status : "invalid",
  };
  const updateResult = dependencies.repositories.workflows.updateVersionValidation({
    tenantId: requestContext.tenantId,
    workflowVersionId,
    validationStatus: validation.valid ? "valid" : "invalid",
    validationResult: toValidationResultRecord(
      validation,
      requestContext.actor.actorId,
    ),
    actorId: requestContext.actor.actorId,
  });
  if (!updateResult.ok) {
    return updateResult;
  }

  if (!validation.valid) {
    const statusUpdateResult = dependencies.repositories.workflows.updateVersionStatus({
      tenantId: requestContext.tenantId,
      workflowVersionId,
      status: updatedVersion.status,
      actorId: requestContext.actor.actorId,
    });
    if (!statusUpdateResult.ok) {
      return statusUpdateResult;
    }
  }

  dependencies.logger?.info("workflow version validation completed", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowVersionId,
    valid: validation.valid,
    errorCount: validation.errors.length,
    warningCount: validation.warnings.length,
  });

  return ok({
    validated: validation.valid ? 1 : 0,
    rejected: validation.valid ? 0 : 1,
    workflowVersionId,
    validation,
  });
}

/**
 * Publishes a valid workflow version and makes it current for its definition.
 */
export function publishWorkflowVersion(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowVersionId: string,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    dependencies.logger?.warn("workflow version publish authorization denied", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowVersionId,
      errorCode: authorizationResult.error.code,
    });
    return authorizationResult;
  }

  dependencies.logger?.info("workflow version publish started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowVersionId,
  });

  const workflowVersionResult = findWorkflowVersion(
    dependencies.repositories,
    requestContext.tenantId,
    workflowVersionId,
  );
  if (!workflowVersionResult.ok) {
    dependencies.logger?.warn("workflow version not found for publish", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowVersionId,
      errorCode: workflowVersionResult.error.code,
    });
    return workflowVersionResult;
  }

  const validation = validateWorkflowConfigRecord(
    workflowVersionResult.value.graphDefinition,
  );
  if (!validation.valid) {
    dependencies.logger?.warn("workflow version publish validation failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowVersionId,
      errorCount: validation.errors.length,
    });
    return err(validationFailedError({ workflowVersionId, validation }));
  }

  const workflowDefinitionResult = findWorkflowDefinition(
    dependencies.repositories,
    requestContext.tenantId,
    workflowVersionResult.value.workflowDefinitionId,
  );
  if (!workflowDefinitionResult.ok) {
    return workflowDefinitionResult;
  }

  const validationUpdateResult =
    dependencies.repositories.workflows.updateVersionValidation({
      tenantId: requestContext.tenantId,
      workflowVersionId,
      validationStatus: "valid",
      validationResult: toValidationResultRecord(
        validation,
        requestContext.actor.actorId,
      ),
      actorId: requestContext.actor.actorId,
    });
  if (!validationUpdateResult.ok) {
    return validationUpdateResult;
  }

  const publishResult = dependencies.repositories.workflows.publishVersionAsCurrent({
    tenantId: requestContext.tenantId,
    workflowVersionId,
    actorId: requestContext.actor.actorId,
  });
  if (!publishResult.ok) {
    return publishResult;
  }

  dependencies.logger?.info("workflow version published", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowVersionId,
    workflowDefinitionId: workflowDefinitionResult.value.workflowDefinitionId,
    intent: workflowVersionResult.value.graphDefinition["intent"],
  });

  return ok({
    published: 1,
    rejected: 0,
    workflowDefinitionId: workflowDefinitionResult.value.workflowDefinitionId,
    workflowVersionId,
  });
}

/**
 * Deprecates a non-current workflow version.
 */
export function deprecateWorkflowVersion(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowVersionId: string,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    dependencies.logger?.warn("workflow version deprecate authorization denied", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowVersionId,
      errorCode: authorizationResult.error.code,
    });
    return authorizationResult;
  }

  dependencies.logger?.info("workflow version deprecate started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowVersionId,
  });

  const workflowVersionResult = findWorkflowVersion(
    dependencies.repositories,
    requestContext.tenantId,
    workflowVersionId,
  );
  if (!workflowVersionResult.ok) {
    dependencies.logger?.warn("workflow version not found for deprecate", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowVersionId,
      errorCode: workflowVersionResult.error.code,
    });
    return workflowVersionResult;
  }

  const workflowDefinitionResult = findWorkflowDefinition(
    dependencies.repositories,
    requestContext.tenantId,
    workflowVersionResult.value.workflowDefinitionId,
  );
  if (!workflowDefinitionResult.ok) {
    return workflowDefinitionResult;
  }

  if (workflowDefinitionResult.value.currentVersionId === workflowVersionId) {
    dependencies.logger?.warn(
      "workflow version deprecate rejected — version is current",
      {
        actorId: requestContext.actor.actorId,
        tenantId: requestContext.tenantId,
        workflowVersionId,
        workflowDefinitionId: workflowDefinitionResult.value.workflowDefinitionId,
      },
    );
    return err(
      validationFailedError({
        workflowVersionId,
        reason: "Cannot deprecate the current workflow version.",
      }),
    );
  }

  const deprecateResult = dependencies.repositories.workflows.deprecateVersion({
    tenantId: requestContext.tenantId,
    workflowVersionId,
    actorId: requestContext.actor.actorId,
  });
  if (!deprecateResult.ok) {
    return deprecateResult;
  }

  dependencies.logger?.info("workflow version deprecated", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowVersionId,
    workflowDefinitionId: workflowDefinitionResult.value.workflowDefinitionId,
  });

  return ok({
    deprecated: 1,
    rejected: 0,
    workflowDefinitionId: workflowDefinitionResult.value.workflowDefinitionId,
    workflowVersionId,
  });
}

function requireWorkflowAdmin(actor: ActorRecord): Result<true, AppError> {
  const isAllowed = actor.roles.some((role) => administrativeRoles.has(role));

  if (!isAllowed) {
    return err(permissionDeniedError({ permission: "workflow_admin" }));
  }

  return ok(true);
}

function importWorkflowConfigIntoRegistry(input: {
  repositories: Repositories;
  tenantId: string;
  actorId: string;
  workflowConfig: WorkflowConfig;
  source: "api" | "files";
  publishCurrent: boolean;
}): Result<
  {
    imported: boolean;
    skipped: boolean;
    published: boolean;
    workflowDefinitionId: string;
    workflowVersionId: string;
    configHash: string;
  },
  AppError
> {
  const validation = validateWorkflowConfigRecord(input.workflowConfig);
  if (!validation.valid) {
    return err(
      validationFailedError({ intent: input.workflowConfig.intent, validation }),
    );
  }

  const configHash = workflowConfigHash(input.workflowConfig);
  const existingVersion = findWorkflowVersionByHash(
    input.repositories,
    input.tenantId,
    input.workflowConfig.intent,
    configHash,
  );
  if (existingVersion !== undefined) {
    const publishResult = publishWorkflowVersionIfRequested({
      repositories: input.repositories,
      tenantId: input.tenantId,
      actorId: input.actorId,
      workflowVersionId: existingVersion.workflowVersionId,
      publishCurrent: input.publishCurrent,
    });
    if (!publishResult.ok) {
      return publishResult;
    }

    return ok({
      imported: false,
      skipped: true,
      published: publishResult.value,
      workflowDefinitionId: existingVersion.workflowDefinitionId,
      workflowVersionId: existingVersion.workflowVersionId,
      configHash,
    });
  }

  const workflowDefinitionResult = findOrCreateWorkflowDefinition(
    input.repositories,
    input.tenantId,
    input.workflowConfig,
  );
  if (!workflowDefinitionResult.ok) {
    return workflowDefinitionResult;
  }

  const workflowVersion = createWorkflowVersionRecord({
    repositories: input.repositories,
    tenantId: input.tenantId,
    actorId: input.actorId,
    workflowDefinition: workflowDefinitionResult.value,
    workflowConfig: input.workflowConfig,
    configHash,
    source: input.source,
  });

  const createVersionResult =
    input.repositories.workflows.createVersion(workflowVersion);
  if (!createVersionResult.ok) {
    return createVersionResult;
  }

  const publishResult = publishWorkflowVersionIfRequested({
    repositories: input.repositories,
    tenantId: input.tenantId,
    actorId: input.actorId,
    workflowVersionId: workflowVersion.workflowVersionId,
    publishCurrent: input.publishCurrent,
  });
  if (!publishResult.ok) {
    return publishResult;
  }

  return ok({
    imported: true,
    skipped: false,
    published: publishResult.value,
    workflowDefinitionId: workflowDefinitionResult.value.workflowDefinitionId,
    workflowVersionId: workflowVersion.workflowVersionId,
    configHash,
  });
}

function publishWorkflowVersionIfRequested(input: {
  repositories: Repositories;
  tenantId: string;
  actorId: string;
  workflowVersionId: string;
  publishCurrent: boolean;
}): Result<boolean, AppError> {
  if (!input.publishCurrent) {
    return ok(false);
  }

  const publishResult = input.repositories.workflows.publishVersionAsCurrent({
    tenantId: input.tenantId,
    workflowVersionId: input.workflowVersionId,
    actorId: input.actorId,
  });
  if (!publishResult.ok) {
    return publishResult;
  }

  return ok(true);
}

function createWorkflowVersionRecord(input: {
  repositories: Repositories;
  tenantId: string;
  actorId: string;
  workflowDefinition: WorkflowDefinitionRecord;
  workflowConfig: WorkflowConfig;
  configHash: string;
  source: "api" | "files";
}): WorkflowVersionRecord {
  const timestamp = nowIso();

  return {
    workflowVersionId: makeId("workflow_version"),
    tenantId: input.tenantId,
    workflowDefinitionId: input.workflowDefinition.workflowDefinitionId,
    versionNumber: nextWorkflowVersionNumber(
      input.repositories,
      input.tenantId,
      input.workflowDefinition.workflowDefinitionId,
    ),
    graphDefinition: cloneWorkflowConfig(input.workflowConfig) as unknown as Record<
      string,
      unknown
    >,
    inputSchema: {},
    outputSchema: {},
    validationRules: [],
    approvalRules: [],
    aiReviewScope: {},
    status: "draft",
    configHash: input.configHash,
    validationStatus: "valid",
    authorActorId: input.actorId,
    createdByActorId: input.actorId,
    updatedByActorId: input.actorId,
    createdAt: timestamp,
    updatedAt: timestamp,
    metadata: {
      importSource: input.source,
      importedAt: timestamp,
    },
  };
}

function findOrCreateWorkflowDefinition(
  repositories: Repositories,
  tenantId: string,
  workflowConfig: WorkflowConfig,
): Result<WorkflowDefinitionRecord, AppError> {
  const definitionsResult = repositories.workflows.listDefinitions(tenantId);
  const versionsResult = repositories.workflows.listVersions(tenantId);
  const existingDefinition =
    definitionsResult.ok && versionsResult.ok
      ? definitionsResult.value.find((definition) => {
          const currentVersion = versionsResult.value.find((version) => {
            return version.workflowVersionId === definition.currentVersionId;
          });

          return (
            currentVersion?.graphDefinition["intent"] === workflowConfig.intent ||
            definition.intent === workflowConfig.intent ||
            definition.name === workflowConfig.intent
          );
        })
      : undefined;

  if (existingDefinition !== undefined) {
    return ok(existingDefinition);
  }

  const workflowDefinition: WorkflowDefinitionRecord = {
    workflowDefinitionId: makeId("workflow_definition"),
    tenantId,
    intent: workflowConfig.intent,
    subjectType: workflowConfig.subjectType,
    name: workflowConfig.intent,
    workflowType: "employee_data_change",
    status: "draft",
    createdAt: nowIso(),
    updatedAt: nowIso(),
    metadata: {},
  };

  return repositories.workflows.createDefinition(workflowDefinition);
}

function findWorkflowVersionByHash(
  repositories: Repositories,
  tenantId: string,
  intent: string,
  configHash: string,
): WorkflowVersionRecord | undefined {
  const versionsResult = repositories.workflows.listVersions(tenantId);
  if (!versionsResult.ok) {
    return undefined;
  }

  return versionsResult.value.find((version) => {
    const versionHash = isWorkflowConfigRecord(version.graphDefinition)
      ? workflowConfigHash(version.graphDefinition)
      : version.configHash;

    return version.graphDefinition["intent"] === intent && versionHash === configHash;
  });
}

function findWorkflowVersion(
  repositories: Repositories,
  tenantId: string,
  workflowVersionId: string,
): Result<WorkflowVersionRecord, AppError> {
  return repositories.workflows.findVersionById(tenantId, workflowVersionId);
}

function findWorkflowDefinition(
  repositories: Repositories,
  tenantId: string,
  workflowDefinitionId: string,
): Result<WorkflowDefinitionRecord, AppError> {
  const definitionsResult = repositories.workflows.listDefinitions(tenantId);
  if (!definitionsResult.ok) {
    return definitionsResult;
  }

  const workflowDefinition = definitionsResult.value.find((definition) => {
    return definition.workflowDefinitionId === workflowDefinitionId;
  });

  if (workflowDefinition === undefined) {
    return err(notFoundError("Workflow definition", { workflowDefinitionId }));
  }

  return ok(workflowDefinition);
}

function nextWorkflowVersionNumber(
  repositories: Repositories,
  tenantId: string,
  workflowDefinitionId: string,
): number {
  const versionsResult = repositories.workflows.listVersions(
    tenantId,
    workflowDefinitionId,
  );

  const versionNumbers = versionsResult.ok
    ? versionsResult.value.map((version) => version.versionNumber)
    : [];

  return Math.max(0, ...versionNumbers) + 1;
}

function workflowRegistrySummaries(
  repositories: Repositories,
  tenantId: string,
): Array<Record<string, unknown>> {
  const definitionsResult = repositories.workflows.listDefinitions(tenantId);
  const versionsResult = repositories.workflows.listVersions(tenantId);

  if (!definitionsResult.ok || !versionsResult.ok) {
    return [];
  }

  return definitionsResult.value.map((definition) => {
    const versions = versionsResult.value
      .filter((version) => {
        return version.workflowDefinitionId === definition.workflowDefinitionId;
      })
      .sort((left, right) => left.versionNumber - right.versionNumber)
      .map(workflowVersionSummary);

    return {
      workflowDefinitionId: definition.workflowDefinitionId,
      intent: definition.intent,
      name: definition.name,
      workflowType: definition.workflowType,
      status: definition.status,
      currentVersionId: definition.currentVersionId,
      versions,
    };
  });
}

function workflowVersionSummary(
  workflowVersion: WorkflowVersionRecord,
): Record<string, unknown> {
  const persistedConfigHash = isWorkflowConfigRecord(workflowVersion.graphDefinition)
    ? workflowConfigHash(workflowVersion.graphDefinition)
    : workflowVersion.configHash;

  return {
    workflowVersionId: workflowVersion.workflowVersionId,
    workflowDefinitionId: workflowVersion.workflowDefinitionId,
    versionNumber: workflowVersion.versionNumber,
    status: workflowVersion.status,
    intent: workflowVersion.graphDefinition["intent"],
    configHash: workflowVersion.configHash ?? persistedConfigHash,
    validationStatus: workflowVersion.validationStatus,
    validation: workflowVersion.validationResult,
    importedAt: workflowVersion.metadata?.["importedAt"],
    publishedAt: workflowVersion.publishedAt,
    deprecatedAt: workflowVersion.deprecatedAt,
  };
}

function readWorkflowConfigPayload(
  body: Record<string, unknown>,
): Result<WorkflowConfig, AppError> {
  const payload = body["workflowConfig"] ?? body;

  if (!isWorkflowConfigRecord(payload)) {
    return err(
      validationFailedError({
        workflowConfig: "Expected a full executable workflow config payload.",
      }),
    );
  }

  return ok(cloneWorkflowConfig(payload));
}

function booleanField(
  record: Record<string, unknown>,
  key: string,
): boolean | undefined {
  const value = record[key];

  if (typeof value === "boolean") {
    return value;
  }

  return undefined;
}

function validateWorkflowConfigRecord(config: unknown): WorkflowValidationOutput {
  if (!isWorkflowConfigRecord(config)) {
    return {
      valid: false,
      errors: [
        {
          severity: "error",
          code: "workflow_config.executable_shape_required",
          path: "",
          jsonPath: "$",
          message: "Workflow config must be a full executable workflow config.",
        },
      ],
      warnings: [],
      configHash: "invalid",
    };
  }

  const workflowConfigResult = workflowConfigFromGraphDefinition(config);
  if (!workflowConfigResult.ok) {
    return {
      valid: false,
      errors: [
        {
          severity: "error",
          code: "workflow_config.executable_shape_required",
          path: "",
          jsonPath: "$",
          message: workflowConfigResult.error.safeMessage,
        },
      ],
      warnings: [],
      configHash: "invalid",
    };
  }

  const validationReport = validateWorkflowConfig(workflowConfigResult.value);

  return {
    valid: validationReport.valid,
    errors: validationReport.errors,
    warnings: validationReport.warnings,
    configHash: validationReport.configHash,
  };
}

function toValidationResultRecord(
  validation: WorkflowValidationOutput,
  actorId: string,
) {
  return {
    status: validation.valid ? "valid" : "invalid",
    errors: validation.errors.map((issue) => {
      return { ...issue };
    }),
    warnings: validation.warnings.map((issue) => {
      return { ...issue };
    }),
    summary: {
      configHash: validation.configHash,
      errorCount: validation.errors.length,
      warningCount: validation.warnings.length,
    },
    validatedAt: nowIso(),
    validatedByActorId: actorId,
  } as const;
}

function createImportSummary(): {
  imported: number;
  skipped: number;
  published: number;
  rejected: Array<Record<string, unknown>>;
  workflowVersionIds: string[];
  skippedWorkflowVersionIds: string[];
  publishedWorkflowVersionIds: string[];
} {
  return {
    imported: 0,
    skipped: 0,
    published: 0,
    rejected: [],
    workflowVersionIds: [],
    skippedWorkflowVersionIds: [],
    publishedWorkflowVersionIds: [],
  };
}

function recordImportOutcome(
  summary: ReturnType<typeof createImportSummary>,
  outcome: {
    imported: boolean;
    skipped: boolean;
    published: boolean;
    workflowVersionId: string;
  },
): void {
  if (outcome.imported) {
    summary.imported += 1;
    summary.workflowVersionIds.push(outcome.workflowVersionId);
  }

  if (outcome.skipped) {
    summary.skipped += 1;
    summary.skippedWorkflowVersionIds.push(outcome.workflowVersionId);
  }

  if (outcome.published) {
    summary.published += 1;
    summary.publishedWorkflowVersionIds.push(outcome.workflowVersionId);
  }
}
