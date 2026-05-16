import {
  err,
  ok,
  validationFailedError,
  type AppError,
  type RequestContext,
  type Result,
} from "@hcm-next/foundation";
import type {
  ActorRecord,
  EmployeeProjectionRecord,
  Repositories,
} from "@hcm-next/data-store";
import type {
  ApiRequestContext,
  AppDependencies,
} from "../../shared/runtime-dependencies.js";
import { previewWorkflowInputMappings } from "../authoring/input-mapping-preview.js";
import {
  exportWorkflowJsonForEditor,
  importWorkflowJsonForEditor,
  validateWorkflowJsonForEditor,
} from "../authoring/contracts.js";
import { listWorkflowBlockCatalogEntries } from "../block-catalog/catalog.js";
import { diffWorkflowConfigs } from "../diff/workflow-diff.js";
import { runPublishGuardrails } from "../guardrails/publish-guardrails.js";
import { validateIntegrationBindings } from "../integrations/integration-bindings.js";
import {
  archiveWorkflowFamily,
  createWorkflowDraftFromConfig,
  ensureWorkflowFamilyForConfig,
  publishWorkflowDraft,
  rejectWorkflowDraft,
  requestWorkflowDraftReview,
  rollbackWorkflowFamilyToVersion,
  saveWorkflowDraftConfig,
} from "../lifecycle/workflow-admin-lifecycle.js";
import {
  clonePublishedWorkflowVersionIntoDraft,
  cloneWorkflowTemplateIntoDraft,
  seedDefaultWorkflowTemplates,
} from "../lifecycle/workflow-admin-templates.js";
import { previewWorkflowPermissions } from "../permissions/permission-preview.js";
import { buildWorkflowMermaidPreview } from "../preview/mermaid.js";
import { previewWorkflowInteraction } from "../preview/interaction-preview.js";
import {
  getWorkflowRegistryDetail,
  listWorkflowRegistry,
  listWorkflowTemplates,
} from "../registry/workflow-admin-registry.js";
import { simulateWorkflow } from "../simulation/workflow-simulator.js";
import { requireWorkflowAdmin } from "./admin-permissions.js";
import {
  cloneWorkflowConfig,
  workflowConfigFromGraphDefinition,
  type WorkflowConfig,
} from "../../shared/workflow-config.js";

/**
 * Lists database-backed workflow admin registry entries.
 */
export function listWorkflowRegistryForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const registryResult = listWorkflowRegistry({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
  });
  if (!registryResult.ok) {
    return registryResult;
  }

  return ok(withAdminMetadata(requestContext, { workflows: registryResult.value }));
}

/**
 * Reads one workflow registry detail entry.
 */
export function getWorkflowRegistryDetailForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowFamilyId: string,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const registryResult = getWorkflowRegistryDetail({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
    workflowFamilyId,
  });
  if (!registryResult.ok) {
    return registryResult;
  }

  return ok(withAdminMetadata(requestContext, { workflow: registryResult.value }));
}

/**
 * Seeds checked-in workflow JSON files as reusable admin templates.
 */
export function seedWorkflowTemplatesForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const seedResult = seedDefaultWorkflowTemplates(dependencies.repositories);
  if (!seedResult.ok) {
    return seedResult;
  }

  return ok(withAdminMetadata(requestContext, seedResult.value));
}

/**
 * Lists active workflow templates available for cloning.
 */
export function listWorkflowTemplatesForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const templatesResult = listWorkflowTemplates({
    repositories: dependencies.repositories,
  });
  if (!templatesResult.ok) {
    return templatesResult;
  }

  return ok(withAdminMetadata(requestContext, { templates: templatesResult.value }));
}

/**
 * Clones one workflow template into an editable tenant draft.
 */
export function cloneWorkflowTemplateForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowTemplateId: string,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const cloneResult = cloneWorkflowTemplateIntoDraft({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
    workflowTemplateId,
    overrideMetadata: objectField(body, "overrideMetadata"),
  });
  if (!cloneResult.ok) {
    return cloneResult;
  }

  return ok(withAdminMetadata(requestContext, cloneResult.value));
}

/**
 * Clones a published admin workflow version into a new draft.
 */
export function clonePublishedWorkflowVersionForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowVersionRecordId: string,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const cloneResult = clonePublishedWorkflowVersionIntoDraft({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
    workflowVersionRecordId,
    overrideMetadata: objectField(body, "overrideMetadata"),
  });
  if (!cloneResult.ok) {
    return cloneResult;
  }

  return ok(withAdminMetadata(requestContext, cloneResult.value));
}

/**
 * Creates or finds a workflow family and creates an editable draft from config.
 */
export function createWorkflowDraftForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const workflowConfigResult = workflowConfigFromBody(body);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const familyResult = ensureWorkflowFamilyForConfig({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
    workflowConfig: workflowConfigResult.value,
    ownerActorId: stringField(body, "ownerActorId"),
  });
  if (!familyResult.ok) {
    return familyResult;
  }

  const draftResult = createWorkflowDraftFromConfig({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
    workflowFamilyId: familyResult.value.workflowFamilyId,
    workflowConfig: workflowConfigResult.value,
    overrideMetadata: objectField(body, "overrideMetadata"),
  });
  if (!draftResult.ok) {
    return draftResult;
  }

  return ok(withAdminMetadata(requestContext, draftResult.value));
}

/**
 * Saves workflow draft JSON using expected-version concurrency.
 */
export function saveWorkflowDraftForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowDraftId: string,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const expectedVersion = numberField(body, "expectedVersion");
  const workflowConfigResult = workflowConfigFromBody(body);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  if (expectedVersion === undefined) {
    return err(validationFailedError({ expectedVersion }));
  }

  const saveResult = saveWorkflowDraftConfig({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
    workflowDraftId,
    expectedVersion,
    workflowConfig: workflowConfigResult.value,
    overrideMetadata: objectField(body, "overrideMetadata"),
  });
  if (!saveResult.ok) {
    return saveResult;
  }

  return ok(withAdminMetadata(requestContext, { draft: saveResult.value }));
}

/**
 * Requests review for a workflow draft.
 */
export function requestWorkflowDraftReviewForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowDraftId: string,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  return draftLifecycleActionForApi(
    dependencies,
    requestContext,
    workflowDraftId,
    body,
    requestWorkflowDraftReview,
    "draft",
  );
}

/**
 * Rejects a workflow draft.
 */
export function rejectWorkflowDraftForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowDraftId: string,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  return draftLifecycleActionForApi(
    dependencies,
    requestContext,
    workflowDraftId,
    body,
    rejectWorkflowDraft,
    "draft",
  );
}

/**
 * Publishes a workflow draft into an immutable admin version.
 */
export function publishWorkflowDraftForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowDraftId: string,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  return draftLifecycleActionForApi(
    dependencies,
    requestContext,
    workflowDraftId,
    body,
    publishWorkflowDraft,
    "publish",
  );
}

/**
 * Rolls back a workflow family to a prior published admin version.
 */
export function rollbackWorkflowFamilyForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowFamilyId: string,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const targetWorkflowVersionRecordId = stringField(
    body,
    "targetWorkflowVersionRecordId",
  );
  if (targetWorkflowVersionRecordId === undefined) {
    return err(validationFailedError({ targetWorkflowVersionRecordId }));
  }

  const rollbackResult = rollbackWorkflowFamilyToVersion({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
    workflowFamilyId,
    targetWorkflowVersionRecordId,
    expectedFamilyVersion: numberField(body, "expectedFamilyVersion"),
  });
  if (!rollbackResult.ok) {
    return rollbackResult;
  }

  return ok(withAdminMetadata(requestContext, rollbackResult.value));
}

/**
 * Archives a workflow family.
 */
export function archiveWorkflowFamilyForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowFamilyId: string,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const archiveResult = archiveWorkflowFamily({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
    workflowFamilyId,
    expectedVersion: numberField(body, "expectedVersion"),
  });
  if (!archiveResult.ok) {
    return archiveResult;
  }

  return ok(withAdminMetadata(requestContext, { family: archiveResult.value }));
}

/**
 * Validates unsaved workflow JSON for the admin editor.
 */
export function validateWorkflowJsonForApi(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const validationResult = validateWorkflowJsonForEditor({
    workflowConfig: body["workflowConfig"] ?? body,
  });
  if (!validationResult.ok) {
    return validationResult;
  }

  return ok(withAdminMetadata(requestContext, validationResult.value));
}

/**
 * Exports workflow JSON with stable formatting.
 */
export function exportWorkflowJsonForApi(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const exportResult = exportWorkflowJsonForEditor({
    workflowConfig: body["workflowConfig"] ?? body,
  });
  if (!exportResult.ok) {
    return exportResult;
  }

  return ok(withAdminMetadata(requestContext, exportResult.value));
}

/**
 * Imports workflow JSON text and validates it for editor use.
 */
export function importWorkflowJsonForApi(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const importBody = stringField(body, "body");
  if (importBody === undefined) {
    return err(validationFailedError({ body: "missing" }));
  }

  const importResult = importWorkflowJsonForEditor({ body: importBody });
  if (!importResult.ok) {
    return importResult;
  }

  return ok(withAdminMetadata(requestContext, importResult.value));
}

/**
 * Generates Mermaid preview text from workflow config.
 */
export function mermaidPreviewForApi(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const workflowConfigResult = workflowConfigFromBody(body);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const mermaidResult = buildWorkflowMermaidPreview({
    workflowConfig: workflowConfigResult.value,
    direction: stringField(body, "direction") === "TD" ? "TD" : "LR",
  });
  if (!mermaidResult.ok) {
    return mermaidResult;
  }

  return ok(withAdminMetadata(requestContext, mermaidResult.value));
}

/**
 * Returns the deterministic Go block catalog.
 */
export function blockCatalogForApi(
  requestContext: ApiRequestContext,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  return ok(
    withAdminMetadata(requestContext, {
      blocks: listWorkflowBlockCatalogEntries(),
    }),
  );
}

/**
 * Previews input mappings for a selected workflow node.
 */
export function inputMappingPreviewForApi(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const workflowConfigResult = workflowConfigFromBody(body);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const previewResult = previewWorkflowInputMappings({
    workflowConfig: workflowConfigResult.value,
    nodeId: stringField(body, "nodeId"),
    mappingObject: objectField(body, "mappingObject"),
    sources: objectField(body, "sources"),
  });
  if (!previewResult.ok) {
    return previewResult;
  }

  return ok(withAdminMetadata(requestContext, previewResult.value));
}

/**
 * Previews a configured form/approval/repair interaction.
 */
export function interactionPreviewForApi(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const workflowConfigResult = workflowConfigFromBody(body);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const previewResult = previewWorkflowInteraction({
    workflowConfig: workflowConfigResult.value,
    state: stringField(body, "state"),
    interactionKey: stringField(body, "interactionKey"),
    actorFixture: objectField(body, "actorFixture"),
    employeeFixture: objectField(body, "employeeFixture"),
  });
  if (!previewResult.ok) {
    return previewResult;
  }

  return ok(withAdminMetadata(requestContext, previewResult.value));
}

/**
 * Previews permissions for one actor, employee, workflow config, and state.
 */
export function permissionPreviewForApi(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const workflowConfigResult = workflowConfigFromBody(body);
  const actorResult = actorFromBody(repositories, body, requestContext.actor);
  const employeeResult = employeeFromBody(repositories, requestContext, body);
  const workflowState = stringField(body, "workflowState");
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }
  if (!actorResult.ok) {
    return actorResult;
  }
  if (!employeeResult.ok) {
    return employeeResult;
  }
  if (workflowState === undefined) {
    return err(validationFailedError({ workflowState }));
  }

  const previewResult = previewWorkflowPermissions({
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
    actor: actorResult.value,
    employeeProjection: employeeResult.value,
    workflowConfig: workflowConfigResult.value,
    workflowState,
    workflowInput: objectField(body, "workflowInput"),
  });
  if (!previewResult.ok) {
    return previewResult;
  }

  return ok(withAdminMetadata(requestContext, previewResult.value));
}

/**
 * Runs workflow simulation against fixture data only.
 */
export function simulateWorkflowForApi(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const workflowConfigResult = workflowConfigFromBodyOrDraft(
    repositories,
    requestContext,
    body,
  );
  const actorResult = actorFromBody(repositories, body, requestContext.actor);
  const employeeResult = employeeFromBody(repositories, requestContext, body);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }
  if (!actorResult.ok) {
    return actorResult;
  }
  if (!employeeResult.ok) {
    return employeeResult;
  }

  const simulationResult = simulateWorkflow({
    workflowConfig: workflowConfigResult.value,
    actor: actorResult.value,
    employeeProjection: employeeResult.value.document,
    workflowInput: objectField(body, "workflowInput") ?? {},
    effectiveAt: stringField(body, "effectiveAt"),
    fakeIntegrationResponses: objectField(body, "fakeIntegrationResponses"),
    fakeIntegrationErrors: objectField(body, "fakeIntegrationErrors"),
    approvalDecisions: stringRecordField(body, "approvalDecisions"),
    nodeOutcomeOverrides: stringRecordField(body, "nodeOutcomeOverrides"),
  });
  if (!simulationResult.ok) {
    return simulationResult;
  }

  return ok(withAdminMetadata(requestContext, simulationResult.value));
}

/**
 * Diffs draft and published workflow configs.
 */
export function diffWorkflowConfigsForApi(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const draftConfigResult = namedWorkflowConfigFromBody(body, "draftConfig");
  const publishedConfigResult = namedWorkflowConfigFromBody(body, "publishedConfig");
  if (!draftConfigResult.ok) {
    return draftConfigResult;
  }
  if (!publishedConfigResult.ok) {
    return publishedConfigResult;
  }

  const diffResult = diffWorkflowConfigs({
    draftConfig: draftConfigResult.value,
    publishedConfig: publishedConfigResult.value,
  });
  if (!diffResult.ok) {
    return diffResult;
  }

  return ok(withAdminMetadata(requestContext, diffResult.value));
}

/**
 * Validates integration bindings against workflow external write nodes.
 */
export function integrationBindingValidationForApi(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const workflowConfigResult = workflowConfigFromBody(body);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const bindingResult = validateIntegrationBindings({
    workflowConfig: workflowConfigResult.value,
    bindings: arrayField(body, "bindings") as never,
    targetEnvironment:
      stringField(body, "targetEnvironment") === "production"
        ? "production"
        : stringField(body, "targetEnvironment") === "staging"
          ? "staging"
          : "sandbox",
    availableSecretRefs: stringArrayField(body, "availableSecretRefs"),
  });
  if (!bindingResult.ok) {
    return bindingResult;
  }

  return ok(withAdminMetadata(requestContext, bindingResult.value));
}

/**
 * Runs publish guardrails against a workflow config.
 */
export function publishGuardrailsForApi(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const workflowConfigResult = workflowConfigFromBody(body);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const publishedConfigResult =
    body["publishedConfig"] === undefined
      ? ok(undefined)
      : namedWorkflowConfigFromBody(body, "publishedConfig");
  if (!publishedConfigResult.ok) {
    return publishedConfigResult;
  }

  const guardrailResult = runPublishGuardrails({
    workflowConfig: workflowConfigResult.value,
    ...(publishedConfigResult.value !== undefined
      ? { publishedConfig: publishedConfigResult.value }
      : {}),
    availableBlocks: listWorkflowBlockCatalogEntries().map((block) => {
      return {
        name: block.name,
        version: block.version,
      };
    }),
    requireSimulationForHighRisk: booleanField(body, "requireSimulationForHighRisk"),
  });
  if (!guardrailResult.ok) {
    return guardrailResult;
  }

  return ok(withAdminMetadata(requestContext, guardrailResult.value));
}

/**
 * Lists tenant/environment integration bindings used by workflow configs.
 */
export function listIntegrationBindingsForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    return authorizationResult;
  }

  const bindingsResult = dependencies.repositories.workflowIntegrationBindings.list({
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
  });
  if (!bindingsResult.ok) {
    return bindingsResult;
  }

  return ok(withAdminMetadata(requestContext, { bindings: bindingsResult.value }));
}

/**
 * Creates or updates one tenant/environment integration binding.
 */
export function upsertIntegrationBindingForApi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    dependencies.logger?.warn("integration binding upsert authorization denied", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      errorCode: authorizationResult.error.code,
    });
    return authorizationResult;
  }

  const bindingInputResult = integrationBindingRecordFromBody(requestContext, body);
  if (!bindingInputResult.ok) {
    dependencies.logger?.warn("integration binding upsert parse failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      errorCode: bindingInputResult.error.code,
    });
    return bindingInputResult;
  }

  dependencies.logger?.info("integration binding upsert started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
    abstractConnectionId: bindingInputResult.value.abstractConnectionId,
    connectorId: bindingInputResult.value.connectorId,
  });

  const upsertResult = dependencies.repositories.workflowIntegrationBindings.upsert(
    bindingInputResult.value,
  );
  if (!upsertResult.ok) {
    dependencies.logger?.warn("integration binding upsert failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      abstractConnectionId: bindingInputResult.value.abstractConnectionId,
      errorCode: upsertResult.error.code,
    });
    return upsertResult;
  }

  dependencies.logger?.info("integration binding upserted", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
    abstractConnectionId: bindingInputResult.value.abstractConnectionId,
    connectorId: bindingInputResult.value.connectorId,
  });

  return ok(withAdminMetadata(requestContext, { binding: upsertResult.value }));
}

function draftLifecycleActionForApi<TValue extends Record<string, unknown>>(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  workflowDraftId: string,
  body: Record<string, unknown>,
  action: (input: {
    repositories: Repositories;
    context: RequestContext;
    workflowDraftId: string;
    expectedVersion: number;
  }) => Result<TValue, AppError>,
  responseKey: string,
): Result<Record<string, unknown>, AppError> {
  const authorizationResult = requireWorkflowAdmin(requestContext.actor);
  if (!authorizationResult.ok) {
    dependencies.logger?.warn("workflow draft lifecycle action authorization denied", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowDraftId,
      responseKey,
      errorCode: authorizationResult.error.code,
    });
    return authorizationResult;
  }

  const expectedVersion = numberField(body, "expectedVersion");
  if (expectedVersion === undefined) {
    return err(validationFailedError({ expectedVersion }));
  }

  dependencies.logger?.info("workflow draft lifecycle action started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowDraftId,
    action: responseKey,
    expectedVersion,
  });

  const actionResult = action({
    repositories: dependencies.repositories,
    context: adminContext(requestContext),
    workflowDraftId,
    expectedVersion,
  });
  if (!actionResult.ok) {
    dependencies.logger?.warn("workflow draft lifecycle action failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      workflowDraftId,
      action: responseKey,
      errorCode: actionResult.error.code,
    });
    return actionResult;
  }

  dependencies.logger?.info("workflow draft lifecycle action completed", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    workflowDraftId,
    action: responseKey,
  });

  return ok(withAdminMetadata(requestContext, { [responseKey]: actionResult.value }));
}

function workflowConfigFromBody(
  body: Record<string, unknown>,
): Result<WorkflowConfig, AppError> {
  return namedWorkflowConfigFromBody(body, "workflowConfig");
}

function workflowConfigFromBodyOrDraft(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<WorkflowConfig, AppError> {
  const workflowDraftId = stringField(body, "workflowDraftId");
  if (workflowDraftId === undefined) {
    return workflowConfigFromBody(body);
  }

  const draftResult = repositories.workflowAdmin.findDraftById({
    tenantId: requestContext.tenantId,
    workflowDraftId,
  });
  if (!draftResult.ok) {
    return draftResult;
  }

  return workflowConfigFromGraphDefinition(draftResult.value.configJson);
}

function namedWorkflowConfigFromBody(
  body: Record<string, unknown>,
  fieldName: string,
): Result<WorkflowConfig, AppError> {
  const payload = body[fieldName] ?? body;

  if (!isRecord(payload)) {
    return err(
      validationFailedError({ [fieldName]: "Expected workflow config object." }),
    );
  }

  const workflowConfigResult = workflowConfigFromGraphDefinition(payload);
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  return ok(cloneWorkflowConfig(workflowConfigResult.value));
}

function actorFromBody(
  repositories: Repositories,
  body: Record<string, unknown>,
  fallbackActor: ActorRecord,
): Result<ActorRecord, AppError> {
  const actorId = stringField(body, "actorId") ?? fallbackActor.actorId;

  return repositories.actors.findById(actorId);
}

function employeeFromBody(
  repositories: Repositories,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Result<EmployeeProjectionRecord, AppError> {
  const employeeId = stringField(body, "employeeId");

  if (employeeId === undefined) {
    return err(validationFailedError({ employeeId }));
  }

  return repositories.employeeProjections.findByEmployeeId(
    requestContext.tenantId,
    employeeId,
  );
}

function adminContext(requestContext: ApiRequestContext): RequestContext {
  return {
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
    actorId: requestContext.actor.actorId,
    actorType: requestContext.actor.actorType as RequestContext["actorType"],
    roles: requestContext.actor.roles,
    requestId: requestContext.requestId,
    correlationId: requestContext.correlationId,
  };
}

function withAdminMetadata(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Record<string, unknown> {
  return {
    correlationId: requestContext.correlationId,
    generatedAt: new Date().toISOString(),
    ...body,
  };
}

function objectField(
  record: Record<string, unknown>,
  fieldName: string,
): Record<string, unknown> | undefined {
  const value = record[fieldName];

  return isRecord(value) ? value : undefined;
}

function stringField(
  record: Record<string, unknown>,
  fieldName: string,
): string | undefined {
  const value = record[fieldName];

  return typeof value === "string" && value.trim().length > 0
    ? value.trim()
    : undefined;
}

function numberField(
  record: Record<string, unknown>,
  fieldName: string,
): number | undefined {
  const value = record[fieldName];

  return typeof value === "number" ? value : undefined;
}

function booleanField(
  record: Record<string, unknown>,
  fieldName: string,
): boolean | undefined {
  const value = record[fieldName];

  return typeof value === "boolean" ? value : undefined;
}

function arrayField(record: Record<string, unknown>, fieldName: string): unknown[] {
  const value = record[fieldName];

  return Array.isArray(value) ? value : [];
}

function stringArrayField(
  record: Record<string, unknown>,
  fieldName: string,
): string[] {
  return arrayField(record, fieldName).filter((value): value is string => {
    return typeof value === "string" && value.trim().length > 0;
  });
}

function stringRecordField(
  record: Record<string, unknown>,
  fieldName: string,
): Record<string, string> | undefined {
  const object = objectField(record, fieldName);
  if (object === undefined) {
    return undefined;
  }

  return Object.fromEntries(
    Object.entries(object).flatMap(([key, value]) => {
      return typeof value === "string" ? [[key, value]] : [];
    }),
  );
}

function integrationBindingRecordFromBody(
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
) {
  const abstractConnectionId = stringField(body, "abstractConnectionId");
  const connectorId = stringField(body, "connectorId");
  const connectorEnvironment = integrationEnvironmentField(
    body,
    "connectorEnvironment",
  );
  const secretRef = stringField(body, "secretRef");
  const timeoutMs = numberField(body, "timeoutMs");
  const retryPolicy = objectField(body, "retryPolicy");
  const reconciliation = objectField(body, "reconciliation");
  const idempotencyScope = idempotencyScopeField(body, "idempotencyScope");

  if (
    abstractConnectionId === undefined ||
    connectorId === undefined ||
    connectorEnvironment === undefined ||
    secretRef === undefined ||
    timeoutMs === undefined ||
    retryPolicy === undefined ||
    reconciliation === undefined ||
    idempotencyScope === undefined
  ) {
    return err(
      validationFailedError({
        abstractConnectionId,
        connectorId,
        connectorEnvironment,
        secretRef,
        timeoutMs,
        retryPolicy: retryPolicy ?? "missing",
        reconciliation: reconciliation ?? "missing",
        idempotencyScope,
      }),
    );
  }

  const backoff = retryBackoffField(retryPolicy, "backoff");
  const reconciliationRequired = booleanField(reconciliation, "required");
  if (backoff === undefined || reconciliationRequired === undefined) {
    return err(validationFailedError({ retryPolicy, reconciliation }));
  }

  return ok({
    tenantId: requestContext.tenantId,
    environmentId: requestContext.environmentId,
    abstractConnectionId,
    connectorId,
    connectorEnvironment,
    secretRef,
    enabled: booleanField(body, "enabled") ?? true,
    allowedOperations: stringArrayField(body, "allowedOperations"),
    timeoutMs,
    retryPolicy: {
      maxAttempts: numberField(retryPolicy, "maxAttempts") ?? 1,
      backoff,
    },
    reconciliation: {
      required: reconciliationRequired,
      ...(stringField(reconciliation, "expectedStatusPath") === undefined
        ? {}
        : {
            expectedStatusPath: stringField(reconciliation, "expectedStatusPath"),
          }),
    },
    idempotencyScope,
    ...(booleanField(body, "allowSandboxInProduction") === undefined
      ? {}
      : {
          allowSandboxInProduction: booleanField(body, "allowSandboxInProduction"),
        }),
    metadata: objectField(body, "metadata") ?? {},
  });
}

function integrationEnvironmentField(
  record: Record<string, unknown>,
  fieldName: string,
): "sandbox" | "staging" | "production" | undefined {
  const value = stringField(record, fieldName);

  return value === "sandbox" || value === "staging" || value === "production"
    ? value
    : undefined;
}

function retryBackoffField(
  record: Record<string, unknown>,
  fieldName: string,
): "none" | "fixed" | "linear" | "exponential" | undefined {
  const value = stringField(record, fieldName);

  return value === "none" ||
    value === "fixed" ||
    value === "linear" ||
    value === "exponential"
    ? value
    : undefined;
}

function idempotencyScopeField(
  record: Record<string, unknown>,
  fieldName: string,
):
  | "workflow_instance"
  | "transaction_plan"
  | "node"
  | "external_operation"
  | undefined {
  const value = stringField(record, fieldName);

  return value === "workflow_instance" ||
    value === "transaction_plan" ||
    value === "node" ||
    value === "external_operation"
    ? value
    : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
