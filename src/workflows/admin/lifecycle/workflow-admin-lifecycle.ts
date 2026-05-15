import {
  ERROR_CODES,
  LEDGER_EVENT_TYPES,
  WORKFLOW_CONFIG_SOURCES,
  err,
  ok,
  validationFailedError,
  type AppError,
  type RequestContext,
  type Result,
} from "@hcm-next/foundation";
import type {
  Repositories,
  WorkflowAdminDraftRecord,
  WorkflowAdminFamilyRecord,
  WorkflowAdminVersionRecord,
  WorkflowPublishHistoryRecord,
} from "@hcm-next/data-store";
import {
  cloneWorkflowConfig,
  workflowConfigFromGraphDefinition,
  workflowConfigHash,
} from "../../shared/workflow-config-registry.js";
import { validateWorkflowConfig } from "../../shared/workflow-config-validation.js";
import type { WorkflowConfig } from "../../shared/workflow-config.js";

export type WorkflowAdminLifecycleInput = {
  repositories: Repositories;
  context: RequestContext;
};

export type CreateWorkflowFamilyFromConfigInput = WorkflowAdminLifecycleInput & {
  workflowConfig: WorkflowConfig;
  ownerActorId?: string | undefined;
};

export type WorkflowDraftLifecycleResult = {
  family: WorkflowAdminFamilyRecord;
  draft: WorkflowAdminDraftRecord;
};

export type WorkflowPublishLifecycleResult = {
  family: WorkflowAdminFamilyRecord;
  draft: WorkflowAdminDraftRecord;
  publishedVersion: WorkflowAdminVersionRecord;
  previousActiveVersion?: WorkflowAdminVersionRecord | undefined;
  history: WorkflowPublishHistoryRecord;
};

export type WorkflowRollbackLifecycleResult = {
  family: WorkflowAdminFamilyRecord;
  activeVersion: WorkflowAdminVersionRecord;
  previousActiveVersion?: WorkflowAdminVersionRecord | undefined;
  history: WorkflowPublishHistoryRecord;
};

/**
 * Creates a workflow family for a full executable config, or returns the existing one.
 */
export function ensureWorkflowFamilyForConfig(
  input: CreateWorkflowFamilyFromConfigInput,
): Result<WorkflowAdminFamilyRecord, AppError> {
  const existingFamilyResult = input.repositories.workflowAdmin.findFamilyByIntent({
    tenantId: input.context.tenantId,
    environmentId: input.context.environmentId,
    intent: input.workflowConfig.intent,
  });

  if (existingFamilyResult.ok) {
    return existingFamilyResult;
  }

  if (existingFamilyResult.error.code !== ERROR_CODES.NOT_FOUND) {
    return existingFamilyResult;
  }

  const validationResult = requireValidWorkflowConfig(input.workflowConfig);
  if (!validationResult.ok) {
    return validationResult;
  }

  const familyResult = input.repositories.workflowAdmin.createFamily({
    tenantId: input.context.tenantId,
    environmentId: input.context.environmentId,
    intent: input.workflowConfig.intent,
    name: workflowConfigTitle(input.workflowConfig),
    description: input.workflowConfig.metadata?.description,
    hcmDomain: workflowConfigDomain(input.workflowConfig),
    ownerActorId: input.ownerActorId ?? input.context.actorId,
    metadata: {
      source: WORKFLOW_CONFIG_SOURCES.DATABASE,
      subjectType: input.workflowConfig.subjectType,
      schemaVersion:
        input.workflowConfig.schemaVersion ??
        input.workflowConfig.metadata?.schemaVersion,
    },
  });
  if (!familyResult.ok) {
    return familyResult;
  }

  const ledgerResult = appendWorkflowAdminLedgerEvent(input, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_FAMILY_CREATED,
    subjectId: familyResult.value.workflowFamilyId,
    payload: {
      workflowFamilyId: familyResult.value.workflowFamilyId,
      intent: familyResult.value.intent,
      environmentId: familyResult.value.environmentId,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return familyResult;
}

/**
 * Creates an editable draft from a full workflow config.
 */
export function createWorkflowDraftFromConfig(
  input: WorkflowAdminLifecycleInput & {
    workflowFamilyId: string;
    workflowConfig: WorkflowConfig;
    sourceMetadata?: Record<string, unknown> | undefined;
    sourceTemplateId?: string | undefined;
    sourceWorkflowVersionRecordId?: string | undefined;
    overrideMetadata?: Record<string, unknown> | undefined;
  },
): Result<WorkflowDraftLifecycleResult, AppError> {
  const validationResult = requireValidWorkflowConfig(input.workflowConfig);
  if (!validationResult.ok) {
    return validationResult;
  }

  const familyResult = input.repositories.workflowAdmin.findFamilyById({
    tenantId: input.context.tenantId,
    workflowFamilyId: input.workflowFamilyId,
  });
  if (!familyResult.ok) {
    return familyResult;
  }

  const draftResult = input.repositories.workflowAdmin.createDraft({
    tenantId: input.context.tenantId,
    environmentId: input.context.environmentId,
    workflowFamilyId: input.workflowFamilyId,
    configJson: cloneConfigRecord(input.workflowConfig),
    configChecksum: workflowConfigHash(input.workflowConfig),
    actorId: input.context.actorId,
    sourceTemplateId: input.sourceTemplateId,
    sourceWorkflowVersionRecordId: input.sourceWorkflowVersionRecordId,
    sourceMetadata: input.sourceMetadata,
    overrideMetadata: input.overrideMetadata,
  });
  if (!draftResult.ok) {
    return draftResult;
  }

  const ledgerResult = appendWorkflowAdminLedgerEvent(input, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_DRAFT_CREATED,
    subjectId: draftResult.value.workflowDraftId,
    payload: {
      workflowFamilyId: input.workflowFamilyId,
      workflowDraftId: draftResult.value.workflowDraftId,
      sourceTemplateId: draftResult.value.sourceTemplateId,
      sourceWorkflowVersionRecordId: draftResult.value.sourceWorkflowVersionRecordId,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok({
    family: familyResult.value,
    draft: draftResult.value,
  });
}

/**
 * Saves draft JSON with optimistic concurrency and executable-config validation.
 */
export function saveWorkflowDraftConfig(
  input: WorkflowAdminLifecycleInput & {
    workflowDraftId: string;
    expectedVersion: number;
    workflowConfig: WorkflowConfig;
    overrideMetadata?: Record<string, unknown> | undefined;
  },
): Result<WorkflowAdminDraftRecord, AppError> {
  const validationResult = requireValidWorkflowConfig(input.workflowConfig);
  if (!validationResult.ok) {
    return validationResult;
  }

  const draftResult = input.repositories.workflowAdmin.saveDraft({
    tenantId: input.context.tenantId,
    workflowDraftId: input.workflowDraftId,
    expectedVersion: input.expectedVersion,
    configJson: cloneConfigRecord(input.workflowConfig),
    configChecksum: workflowConfigHash(input.workflowConfig),
    actorId: input.context.actorId,
    overrideMetadata: input.overrideMetadata,
  });
  if (!draftResult.ok) {
    return draftResult;
  }

  const ledgerResult = appendWorkflowAdminLedgerEvent(input, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_DRAFT_UPDATED,
    subjectId: draftResult.value.workflowDraftId,
    payload: {
      workflowFamilyId: draftResult.value.workflowFamilyId,
      workflowDraftId: draftResult.value.workflowDraftId,
      version: draftResult.value.version,
      configChecksum: draftResult.value.configChecksum,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return draftResult;
}

/**
 * Moves a draft into the ready-for-review lifecycle status.
 */
export function requestWorkflowDraftReview(
  input: WorkflowAdminLifecycleInput & {
    workflowDraftId: string;
    expectedVersion: number;
  },
): Result<WorkflowAdminDraftRecord, AppError> {
  const draftResult = input.repositories.workflowAdmin.markDraftReadyForReview({
    tenantId: input.context.tenantId,
    workflowDraftId: input.workflowDraftId,
    expectedVersion: input.expectedVersion,
    actorId: input.context.actorId,
  });
  if (!draftResult.ok) {
    return draftResult;
  }

  const ledgerResult = appendWorkflowAdminLedgerEvent(input, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_DRAFT_REVIEW_REQUESTED,
    subjectId: draftResult.value.workflowDraftId,
    payload: {
      workflowFamilyId: draftResult.value.workflowFamilyId,
      workflowDraftId: draftResult.value.workflowDraftId,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return draftResult;
}

/**
 * Rejects a draft while preserving it for audit and future cloning.
 */
export function rejectWorkflowDraft(
  input: WorkflowAdminLifecycleInput & {
    workflowDraftId: string;
    expectedVersion: number;
  },
): Result<WorkflowAdminDraftRecord, AppError> {
  const draftResult = input.repositories.workflowAdmin.rejectDraft({
    tenantId: input.context.tenantId,
    workflowDraftId: input.workflowDraftId,
    expectedVersion: input.expectedVersion,
    actorId: input.context.actorId,
  });
  if (!draftResult.ok) {
    return draftResult;
  }

  const ledgerResult = appendWorkflowAdminLedgerEvent(input, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_DRAFT_REJECTED,
    subjectId: draftResult.value.workflowDraftId,
    payload: {
      workflowFamilyId: draftResult.value.workflowFamilyId,
      workflowDraftId: draftResult.value.workflowDraftId,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return draftResult;
}

/**
 * Publishes a draft as an immutable active workflow config version.
 */
export function publishWorkflowDraft(
  input: WorkflowAdminLifecycleInput & {
    workflowDraftId: string;
    expectedVersion: number;
  },
): Result<WorkflowPublishLifecycleResult, AppError> {
  const draftResult = input.repositories.workflowAdmin.findDraftById({
    tenantId: input.context.tenantId,
    workflowDraftId: input.workflowDraftId,
  });
  if (!draftResult.ok) {
    return draftResult;
  }

  const configResult = workflowConfigFromGraphDefinition(draftResult.value.configJson);
  if (!configResult.ok) {
    return configResult;
  }

  const validationResult = requireValidWorkflowConfig(configResult.value);
  if (!validationResult.ok) {
    return validationResult;
  }

  const publishResult = input.repositories.workflowAdmin.publishDraft({
    tenantId: input.context.tenantId,
    workflowDraftId: input.workflowDraftId,
    expectedVersion: input.expectedVersion,
    actorId: input.context.actorId,
  });
  if (!publishResult.ok) {
    return publishResult;
  }

  const ledgerResult = appendWorkflowAdminLedgerEvent(input, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_PUBLISHED,
    subjectId: publishResult.value.publishedVersion.workflowVersionRecordId,
    workflowDefinitionId: publishResult.value.family.workflowFamilyId,
    workflowVersionId: publishResult.value.publishedVersion.workflowVersionRecordId,
    payload: {
      workflowFamilyId: publishResult.value.family.workflowFamilyId,
      workflowDraftId: publishResult.value.draft.workflowDraftId,
      workflowVersionRecordId:
        publishResult.value.publishedVersion.workflowVersionRecordId,
      publishedVersion: publishResult.value.publishedVersion.publishedVersion,
      previousActiveWorkflowVersionRecordId:
        publishResult.value.previousActiveVersion?.workflowVersionRecordId,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(publishResult.value);
}

/**
 * Rolls active runtime config back to an older published version.
 */
export function rollbackWorkflowFamilyToVersion(
  input: WorkflowAdminLifecycleInput & {
    workflowFamilyId: string;
    targetWorkflowVersionRecordId: string;
    expectedFamilyVersion?: number | undefined;
  },
): Result<WorkflowRollbackLifecycleResult, AppError> {
  const rollbackResult = input.repositories.workflowAdmin.rollbackToVersion({
    tenantId: input.context.tenantId,
    workflowFamilyId: input.workflowFamilyId,
    targetWorkflowVersionRecordId: input.targetWorkflowVersionRecordId,
    actorId: input.context.actorId,
    expectedFamilyVersion: input.expectedFamilyVersion,
  });
  if (!rollbackResult.ok) {
    return rollbackResult;
  }

  const ledgerResult = appendWorkflowAdminLedgerEvent(input, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_ROLLED_BACK,
    subjectId: rollbackResult.value.activeVersion.workflowVersionRecordId,
    workflowDefinitionId: rollbackResult.value.family.workflowFamilyId,
    workflowVersionId: rollbackResult.value.activeVersion.workflowVersionRecordId,
    payload: {
      workflowFamilyId: rollbackResult.value.family.workflowFamilyId,
      targetWorkflowVersionRecordId:
        rollbackResult.value.activeVersion.workflowVersionRecordId,
      previousActiveWorkflowVersionRecordId:
        rollbackResult.value.previousActiveVersion?.workflowVersionRecordId,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return ok(rollbackResult.value);
}

/**
 * Archives a workflow family after a caller has checked admin permissions.
 */
export function archiveWorkflowFamily(
  input: WorkflowAdminLifecycleInput & {
    workflowFamilyId: string;
    expectedVersion?: number | undefined;
  },
): Result<WorkflowAdminFamilyRecord, AppError> {
  const familyResult = input.repositories.workflowAdmin.archiveFamily({
    tenantId: input.context.tenantId,
    workflowFamilyId: input.workflowFamilyId,
    expectedVersion: input.expectedVersion,
    actorId: input.context.actorId,
  });
  if (!familyResult.ok) {
    return familyResult;
  }

  const ledgerResult = appendWorkflowAdminLedgerEvent(input, {
    eventType: LEDGER_EVENT_TYPES.WORKFLOW_ARCHIVED,
    subjectId: familyResult.value.workflowFamilyId,
    workflowDefinitionId: familyResult.value.workflowFamilyId,
    payload: {
      workflowFamilyId: familyResult.value.workflowFamilyId,
      intent: familyResult.value.intent,
    },
  });
  if (!ledgerResult.ok) {
    return ledgerResult;
  }

  return familyResult;
}

function requireValidWorkflowConfig(
  workflowConfig: WorkflowConfig,
): Result<true, AppError> {
  const validation = validateWorkflowConfig(workflowConfig);

  if (!validation.valid) {
    return err(
      validationFailedError({
        workflowConfig: workflowConfig.intent,
        errors: validation.errors,
        warnings: validation.warnings,
      }),
    );
  }

  return ok(true);
}

function appendWorkflowAdminLedgerEvent(
  input: WorkflowAdminLifecycleInput,
  event: {
    eventType: string;
    subjectId: string;
    payload: Record<string, unknown>;
    workflowDefinitionId?: string | undefined;
    workflowVersionId?: string | undefined;
  },
): Result<true, AppError> {
  const appendResult = input.repositories.ledger.append({
    tenantId: input.context.tenantId,
    eventType: event.eventType,
    subjectType: "workflow_admin",
    subjectId: event.subjectId,
    occurredAt: new Date().toISOString(),
    actorType: input.context.actorType,
    actorId: input.context.actorId,
    actorRole: input.context.roles[0],
    relationshipContext: {},
    workflowDefinitionId: event.workflowDefinitionId,
    workflowVersionId: event.workflowVersionId,
    correlationId: input.context.correlationId,
    permissionSnapshot: {},
    aiVisibilitySnapshot: {},
    payload: event.payload,
  });
  if (!appendResult.ok) {
    return appendResult;
  }

  return ok(true);
}

function workflowConfigTitle(workflowConfig: WorkflowConfig): string {
  return workflowConfig.metadata?.title ?? workflowConfig.intent;
}

function workflowConfigDomain(workflowConfig: WorkflowConfig): string {
  return (
    workflowConfig.metadata?.domain ??
    (workflowConfig.intent.startsWith("position.") ? "position" : "employee")
  );
}

function cloneConfigRecord(workflowConfig: WorkflowConfig): Record<string, unknown> {
  return cloneWorkflowConfig(workflowConfig) as unknown as Record<string, unknown>;
}
