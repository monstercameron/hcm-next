import {
  WORKFLOW_ADMIN_DRAFT_STATUSES,
  WORKFLOW_ADMIN_FAMILY_STATUSES,
  WORKFLOW_ADMIN_PUBLISH_ACTIONS,
  WORKFLOW_ADMIN_TEMPLATE_STATUSES,
  WORKFLOW_ADMIN_VERSION_STATUSES,
  err,
  notFoundError,
  ok,
  validationFailedError,
  versionConflictError,
  type AppError,
  type Result,
  type WorkflowAdminDraftStatus,
  type WorkflowAdminFamilyStatus,
} from "@human-capital-management-suite/foundation";
import { makeId, nowIso, type HcmNextStore } from "../store.js";
import type {
  WorkflowAdminDraftRecord,
  WorkflowAdminFamilyRecord,
  WorkflowAdminVersionRecord,
  WorkflowPublishHistoryRecord,
  WorkflowTemplateRecord,
} from "../types.js";

export type CreateWorkflowFamilyInput = {
  tenantId: string;
  environmentId: string;
  intent: string;
  name: string;
  hcmDomain: string;
  ownerActorId: string;
  description?: string | undefined;
  metadata?: Record<string, unknown> | undefined;
};

export type CreateWorkflowDraftInput = {
  tenantId: string;
  environmentId: string;
  workflowFamilyId: string;
  configJson: Record<string, unknown>;
  configChecksum: string;
  actorId: string;
  sourceTemplateId?: string | undefined;
  sourceWorkflowVersionRecordId?: string | undefined;
  sourceMetadata?: Record<string, unknown> | undefined;
  overrideMetadata?: Record<string, unknown> | undefined;
};

export type PublishWorkflowDraftResult = {
  family: WorkflowAdminFamilyRecord;
  draft: WorkflowAdminDraftRecord;
  publishedVersion: WorkflowAdminVersionRecord;
  previousActiveVersion?: WorkflowAdminVersionRecord | undefined;
  history: WorkflowPublishHistoryRecord;
};

export type RollbackWorkflowFamilyResult = {
  family: WorkflowAdminFamilyRecord;
  activeVersion: WorkflowAdminVersionRecord;
  previousActiveVersion?: WorkflowAdminVersionRecord | undefined;
  history: WorkflowPublishHistoryRecord;
};

export type WorkflowAdminRepository = ReturnType<typeof createWorkflowAdminRepository>;

/**
 * Creates the in-memory repository for workflow admin lifecycle records.
 */
export function createWorkflowAdminRepository(store: HcmNextStore) {
  return {
    /** Creates a tenant/environment-scoped workflow family with unique intent. */
    createFamily(
      input: CreateWorkflowFamilyInput,
    ): Result<WorkflowAdminFamilyRecord, AppError> {
      const duplicateFamily = workflowFamilyByIntent(store, {
        tenantId: input.tenantId,
        environmentId: input.environmentId,
        intent: input.intent,
      });

      if (duplicateFamily !== undefined) {
        return err(
          validationFailedError({
            tenantId: input.tenantId,
            environmentId: input.environmentId,
            intent: input.intent,
            reason: "workflow family intent already exists",
          }),
        );
      }

      const timestamp = nowIso();
      const family: WorkflowAdminFamilyRecord = {
        workflowFamilyId: makeId("workflow_family"),
        tenantId: input.tenantId,
        environmentId: input.environmentId,
        intent: input.intent,
        name: input.name,
        ...(input.description !== undefined ? { description: input.description } : {}),
        hcmDomain: input.hcmDomain,
        ownerActorId: input.ownerActorId,
        status: WORKFLOW_ADMIN_FAMILY_STATUSES.ACTIVE,
        version: 1,
        createdAt: timestamp,
        updatedAt: timestamp,
        metadata: input.metadata ?? {},
      };

      store.workflowAdminFamilies.set(family.workflowFamilyId, family);
      return ok(family);
    },

    /** Lists workflow families scoped to one tenant/environment. */
    listFamilies(input: {
      tenantId: string;
      environmentId?: string | undefined;
      status?: WorkflowAdminFamilyStatus | undefined;
    }): Result<WorkflowAdminFamilyRecord[], AppError> {
      const families = [...store.workflowAdminFamilies.values()]
        .filter((family) => {
          return (
            family.tenantId === input.tenantId &&
            (input.environmentId === undefined ||
              family.environmentId === input.environmentId) &&
            (input.status === undefined || family.status === input.status)
          );
        })
        .sort(compareFamilies);

      return ok(families);
    },

    /** Finds a workflow family by durable ID. */
    findFamilyById(input: {
      tenantId: string;
      workflowFamilyId: string;
    }): Result<WorkflowAdminFamilyRecord, AppError> {
      const family = store.workflowAdminFamilies.get(input.workflowFamilyId);

      if (family === undefined || family.tenantId !== input.tenantId) {
        return err(
          notFoundError("Workflow family", {
            workflowFamilyId: input.workflowFamilyId,
          }),
        );
      }

      return ok(family);
    },

    /** Finds a workflow family by tenant, environment, and public intent. */
    findFamilyByIntent(input: {
      tenantId: string;
      environmentId: string;
      intent: string;
    }): Result<WorkflowAdminFamilyRecord, AppError> {
      const family = workflowFamilyByIntent(store, input);

      if (family === undefined) {
        return err(
          notFoundError("Workflow family", {
            tenantId: input.tenantId,
            environmentId: input.environmentId,
            intent: input.intent,
          }),
        );
      }

      return ok(family);
    },

    /** Archives a workflow family without deleting versions or drafts. */
    archiveFamily(input: {
      tenantId: string;
      workflowFamilyId: string;
      actorId: string;
      expectedVersion?: number | undefined;
    }): Result<WorkflowAdminFamilyRecord, AppError> {
      return updateFamilyStatus(store, {
        ...input,
        status: WORKFLOW_ADMIN_FAMILY_STATUSES.ARCHIVED,
      });
    },

    /** Restores an archived family for local/admin demos. */
    unarchiveFamily(input: {
      tenantId: string;
      workflowFamilyId: string;
      actorId: string;
      expectedVersion?: number | undefined;
    }): Result<WorkflowAdminFamilyRecord, AppError> {
      return updateFamilyStatus(store, {
        ...input,
        status: WORKFLOW_ADMIN_FAMILY_STATUSES.ACTIVE,
      });
    },

    /** Creates an editable draft for a workflow family. */
    createDraft(
      input: CreateWorkflowDraftInput,
    ): Result<WorkflowAdminDraftRecord, AppError> {
      const familyResult = findWorkflowFamily(store, {
        tenantId: input.tenantId,
        workflowFamilyId: input.workflowFamilyId,
      });
      if (!familyResult.ok) {
        return familyResult;
      }

      if (familyResult.value.environmentId !== input.environmentId) {
        return err(
          validationFailedError({
            workflowFamilyId: input.workflowFamilyId,
            expectedEnvironmentId: familyResult.value.environmentId,
            actualEnvironmentId: input.environmentId,
          }),
        );
      }

      const timestamp = nowIso();
      const draft: WorkflowAdminDraftRecord = {
        workflowDraftId: makeId("workflow_draft"),
        workflowFamilyId: input.workflowFamilyId,
        tenantId: input.tenantId,
        environmentId: input.environmentId,
        draftVersion: nextDraftVersion(store, input.workflowFamilyId),
        configJson: cloneJsonRecord(input.configJson),
        configChecksum: input.configChecksum,
        status: WORKFLOW_ADMIN_DRAFT_STATUSES.DRAFT,
        createdByActorId: input.actorId,
        updatedByActorId: input.actorId,
        ...(input.sourceTemplateId !== undefined
          ? { sourceTemplateId: input.sourceTemplateId }
          : {}),
        ...(input.sourceWorkflowVersionRecordId !== undefined
          ? { sourceWorkflowVersionRecordId: input.sourceWorkflowVersionRecordId }
          : {}),
        sourceMetadata: input.sourceMetadata ?? {},
        overrideMetadata: input.overrideMetadata ?? {},
        version: 1,
        createdAt: timestamp,
        updatedAt: timestamp,
      };

      store.workflowAdminDrafts.set(draft.workflowDraftId, draft);
      return ok(draft);
    },

    /** Finds a draft by durable ID. */
    findDraftById(input: {
      tenantId: string;
      workflowDraftId: string;
    }): Result<WorkflowAdminDraftRecord, AppError> {
      return findWorkflowDraft(store, input);
    },

    /** Lists drafts for one workflow family. */
    listDraftsByFamily(input: {
      tenantId: string;
      workflowFamilyId: string;
      status?: WorkflowAdminDraftStatus | undefined;
    }): Result<WorkflowAdminDraftRecord[], AppError> {
      const drafts = [...store.workflowAdminDrafts.values()]
        .filter((draft) => {
          return (
            draft.tenantId === input.tenantId &&
            draft.workflowFamilyId === input.workflowFamilyId &&
            (input.status === undefined || draft.status === input.status)
          );
        })
        .sort((left, right) => left.draftVersion - right.draftVersion);

      return ok(drafts);
    },

    /** Saves draft JSON after an optimistic concurrency check. */
    saveDraft(input: {
      tenantId: string;
      workflowDraftId: string;
      expectedVersion: number;
      configJson: Record<string, unknown>;
      configChecksum: string;
      actorId: string;
      overrideMetadata?: Record<string, unknown> | undefined;
    }): Result<WorkflowAdminDraftRecord, AppError> {
      const draftResult = findWorkflowDraft(store, input);
      if (!draftResult.ok) {
        return draftResult;
      }

      const versionResult = requireExpectedVersion(
        draftResult.value.version,
        input.expectedVersion,
        { workflowDraftId: input.workflowDraftId },
      );
      if (!versionResult.ok) {
        return versionResult;
      }

      if (draftResult.value.status === WORKFLOW_ADMIN_DRAFT_STATUSES.PUBLISHED) {
        return err(
          validationFailedError({
            workflowDraftId: input.workflowDraftId,
            reason: "published draft cannot be edited",
          }),
        );
      }

      const timestamp = nowIso();
      const savedDraft: WorkflowAdminDraftRecord = {
        ...draftResult.value,
        configJson: cloneJsonRecord(input.configJson),
        configChecksum: input.configChecksum,
        status: WORKFLOW_ADMIN_DRAFT_STATUSES.DRAFT,
        updatedByActorId: input.actorId,
        overrideMetadata: input.overrideMetadata ?? draftResult.value.overrideMetadata,
        version: draftResult.value.version + 1,
        updatedAt: timestamp,
      };

      store.workflowAdminDrafts.set(savedDraft.workflowDraftId, savedDraft);
      return ok(savedDraft);
    },

    /** Marks a draft ready for admin review. */
    markDraftReadyForReview(input: {
      tenantId: string;
      workflowDraftId: string;
      expectedVersion: number;
      actorId: string;
    }): Result<WorkflowAdminDraftRecord, AppError> {
      return updateDraftStatus(store, {
        ...input,
        status: WORKFLOW_ADMIN_DRAFT_STATUSES.READY_FOR_REVIEW,
      });
    },

    /** Rejects a draft and preserves its config for future review. */
    rejectDraft(input: {
      tenantId: string;
      workflowDraftId: string;
      expectedVersion: number;
      actorId: string;
    }): Result<WorkflowAdminDraftRecord, AppError> {
      return updateDraftStatus(store, {
        ...input,
        status: WORKFLOW_ADMIN_DRAFT_STATUSES.REJECTED,
      });
    },

    /** Publishes a draft as an immutable version and activates it. */
    publishDraft(input: {
      tenantId: string;
      workflowDraftId: string;
      expectedVersion: number;
      actorId: string;
    }): Result<PublishWorkflowDraftResult, AppError> {
      const draftResult = findWorkflowDraft(store, input);
      if (!draftResult.ok) {
        return draftResult;
      }

      const versionResult = requireExpectedVersion(
        draftResult.value.version,
        input.expectedVersion,
        { workflowDraftId: input.workflowDraftId },
      );
      if (!versionResult.ok) {
        return versionResult;
      }

      if (draftResult.value.status === WORKFLOW_ADMIN_DRAFT_STATUSES.PUBLISHED) {
        return err(
          validationFailedError({
            workflowDraftId: input.workflowDraftId,
            reason: "draft is already published",
          }),
        );
      }

      const familyResult = findWorkflowFamily(store, {
        tenantId: input.tenantId,
        workflowFamilyId: draftResult.value.workflowFamilyId,
      });
      if (!familyResult.ok) {
        return familyResult;
      }

      if (familyResult.value.status !== WORKFLOW_ADMIN_FAMILY_STATUSES.ACTIVE) {
        return err(
          validationFailedError({
            workflowFamilyId: familyResult.value.workflowFamilyId,
            status: familyResult.value.status,
          }),
        );
      }

      const previousActiveVersion = activeVersionForFamily(
        store,
        familyResult.value.workflowFamilyId,
      );
      const publishResult = publishAdminVersionInTransaction(store, {
        family: familyResult.value,
        draft: draftResult.value,
        actorId: input.actorId,
        previousActiveVersion,
      });

      return ok(publishResult);
    },

    /** Rolls the active family pointer back to an older published version. */
    rollbackToVersion(input: {
      tenantId: string;
      workflowFamilyId: string;
      targetWorkflowVersionRecordId: string;
      actorId: string;
      expectedFamilyVersion?: number | undefined;
    }): Result<RollbackWorkflowFamilyResult, AppError> {
      const familyResult = findWorkflowFamily(store, input);
      if (!familyResult.ok) {
        return familyResult;
      }

      const versionResult = findWorkflowAdminVersion(store, {
        tenantId: input.tenantId,
        workflowVersionRecordId: input.targetWorkflowVersionRecordId,
      });
      if (!versionResult.ok) {
        return versionResult;
      }

      if (
        versionResult.value.workflowFamilyId !== familyResult.value.workflowFamilyId
      ) {
        return err(
          validationFailedError({
            workflowFamilyId: familyResult.value.workflowFamilyId,
            targetWorkflowVersionRecordId: input.targetWorkflowVersionRecordId,
          }),
        );
      }

      if (input.expectedFamilyVersion !== undefined) {
        const versionCheckResult = requireExpectedVersion(
          familyResult.value.version,
          input.expectedFamilyVersion,
          { workflowFamilyId: input.workflowFamilyId },
        );
        if (!versionCheckResult.ok) {
          return versionCheckResult;
        }
      }

      const previousActiveVersion = activeVersionForFamily(
        store,
        familyResult.value.workflowFamilyId,
      );

      return ok(
        activateExistingVersionInTransaction(store, {
          family: familyResult.value,
          actorId: input.actorId,
          targetVersion: versionResult.value,
          previousActiveVersion,
        }),
      );
    },

    /** Lists published records for one family. */
    listVersionsByFamily(input: {
      tenantId: string;
      workflowFamilyId: string;
    }): Result<WorkflowAdminVersionRecord[], AppError> {
      const versions = [...store.workflowAdminVersions.values()]
        .filter((version) => {
          return (
            version.tenantId === input.tenantId &&
            version.workflowFamilyId === input.workflowFamilyId
          );
        })
        .sort((left, right) => left.publishedVersion - right.publishedVersion);

      return ok(versions);
    },

    /** Finds a published admin version by durable ID, active or inactive. */
    findVersionById(input: {
      tenantId: string;
      workflowVersionRecordId: string;
    }): Result<WorkflowAdminVersionRecord, AppError> {
      return findWorkflowAdminVersion(store, input);
    },

    /** Finds the active admin version for runtime config resolution. */
    findActiveVersionByIntent(input: {
      tenantId: string;
      environmentId: string;
      intent: string;
    }): Result<
      {
        family: WorkflowAdminFamilyRecord;
        version: WorkflowAdminVersionRecord;
      },
      AppError
    > {
      const family = workflowFamilyByIntent(store, input);

      if (family === undefined) {
        return err(
          notFoundError("Workflow family", {
            tenantId: input.tenantId,
            environmentId: input.environmentId,
            intent: input.intent,
          }),
        );
      }

      if (family.status !== WORKFLOW_ADMIN_FAMILY_STATUSES.ACTIVE) {
        return err(
          notFoundError("Workflow family", {
            tenantId: input.tenantId,
            environmentId: input.environmentId,
            intent: input.intent,
            status: family.status,
          }),
        );
      }

      const activeVersion = activeVersionForFamily(store, family.workflowFamilyId);
      if (activeVersion === undefined) {
        return err(
          notFoundError("Workflow admin version", {
            workflowFamilyId: family.workflowFamilyId,
            intent: input.intent,
          }),
        );
      }

      return ok({
        family,
        version: activeVersion,
      });
    },

    /** Records or updates a reusable workflow template. */
    upsertTemplate(
      record: Omit<
        WorkflowTemplateRecord,
        "workflowTemplateId" | "createdAt" | "updatedAt"
      > & {
        workflowTemplateId?: string | undefined;
      },
    ): Result<WorkflowTemplateRecord, AppError> {
      const existingTemplate = [...store.workflowTemplates.values()].find(
        (candidate) => {
          return (
            candidate.intent === record.intent &&
            candidate.templateVersion === record.templateVersion
          );
        },
      );
      const timestamp = nowIso();
      const templateId =
        record.workflowTemplateId ??
        existingTemplate?.workflowTemplateId ??
        makeId("workflow_template");
      const createdAt = existingTemplate?.createdAt ?? timestamp;
      const template: WorkflowTemplateRecord = {
        workflowTemplateId: templateId,
        name: record.name,
        intent: record.intent,
        hcmDomain: record.hcmDomain,
        ...(record.description !== undefined
          ? { description: record.description }
          : {}),
        templateVersion: record.templateVersion,
        configJson: cloneJsonRecord(record.configJson),
        configChecksum: record.configChecksum,
        status: record.status,
        createdAt,
        updatedAt: timestamp,
        metadata: record.metadata,
      };

      store.workflowTemplates.set(template.workflowTemplateId, template);
      return ok(template);
    },

    /** Lists reusable templates available for cloning. */
    listTemplates(input?: {
      status?:
        | typeof WORKFLOW_ADMIN_TEMPLATE_STATUSES.ACTIVE
        | typeof WORKFLOW_ADMIN_TEMPLATE_STATUSES.ARCHIVED;
    }): Result<WorkflowTemplateRecord[], AppError> {
      const templates = [...store.workflowTemplates.values()]
        .filter((template) => {
          return input?.status === undefined || template.status === input.status;
        })
        .sort((left, right) => {
          return (
            left.intent.localeCompare(right.intent) ||
            left.templateVersion - right.templateVersion
          );
        });

      return ok(templates);
    },

    /** Finds a reusable workflow template by durable ID. */
    findTemplateById(
      workflowTemplateId: string,
    ): Result<WorkflowTemplateRecord, AppError> {
      const template = store.workflowTemplates.get(workflowTemplateId);

      if (template === undefined) {
        return err(notFoundError("Workflow template", { workflowTemplateId }));
      }

      return ok(template);
    },
  };
}

function workflowFamilyByIntent(
  store: HcmNextStore,
  input: {
    tenantId: string;
    environmentId: string;
    intent: string;
  },
): WorkflowAdminFamilyRecord | undefined {
  return [...store.workflowAdminFamilies.values()].find((family) => {
    return (
      family.tenantId === input.tenantId &&
      family.environmentId === input.environmentId &&
      family.intent === input.intent
    );
  });
}

function findWorkflowFamily(
  store: HcmNextStore,
  input: {
    tenantId: string;
    workflowFamilyId: string;
  },
): Result<WorkflowAdminFamilyRecord, AppError> {
  const family = store.workflowAdminFamilies.get(input.workflowFamilyId);

  if (family === undefined || family.tenantId !== input.tenantId) {
    return err(
      notFoundError("Workflow family", {
        workflowFamilyId: input.workflowFamilyId,
      }),
    );
  }

  return ok(family);
}

function findWorkflowDraft(
  store: HcmNextStore,
  input: {
    tenantId: string;
    workflowDraftId: string;
  },
): Result<WorkflowAdminDraftRecord, AppError> {
  const draft = store.workflowAdminDrafts.get(input.workflowDraftId);

  if (draft === undefined || draft.tenantId !== input.tenantId) {
    return err(
      notFoundError("Workflow draft", {
        workflowDraftId: input.workflowDraftId,
      }),
    );
  }

  return ok(draft);
}

function findWorkflowAdminVersion(
  store: HcmNextStore,
  input: {
    tenantId: string;
    workflowVersionRecordId: string;
  },
): Result<WorkflowAdminVersionRecord, AppError> {
  const version = store.workflowAdminVersions.get(input.workflowVersionRecordId);

  if (version === undefined || version.tenantId !== input.tenantId) {
    return err(
      notFoundError("Workflow admin version", {
        workflowVersionRecordId: input.workflowVersionRecordId,
      }),
    );
  }

  return ok(version);
}

function updateFamilyStatus(
  store: HcmNextStore,
  input: {
    tenantId: string;
    workflowFamilyId: string;
    actorId: string;
    status: WorkflowAdminFamilyStatus;
    expectedVersion?: number | undefined;
  },
): Result<WorkflowAdminFamilyRecord, AppError> {
  const familyResult = findWorkflowFamily(store, input);
  if (!familyResult.ok) {
    return familyResult;
  }

  if (input.expectedVersion !== undefined) {
    const versionResult = requireExpectedVersion(
      familyResult.value.version,
      input.expectedVersion,
      { workflowFamilyId: input.workflowFamilyId },
    );
    if (!versionResult.ok) {
      return versionResult;
    }
  }

  const updatedFamily: WorkflowAdminFamilyRecord = {
    ...familyResult.value,
    status: input.status,
    version: familyResult.value.version + 1,
    updatedAt: nowIso(),
    metadata: {
      ...familyResult.value.metadata,
      lastUpdatedByActorId: input.actorId,
    },
  };

  store.workflowAdminFamilies.set(updatedFamily.workflowFamilyId, updatedFamily);
  return ok(updatedFamily);
}

function updateDraftStatus(
  store: HcmNextStore,
  input: {
    tenantId: string;
    workflowDraftId: string;
    expectedVersion: number;
    actorId: string;
    status: WorkflowAdminDraftStatus;
  },
): Result<WorkflowAdminDraftRecord, AppError> {
  const draftResult = findWorkflowDraft(store, input);
  if (!draftResult.ok) {
    return draftResult;
  }

  const versionResult = requireExpectedVersion(
    draftResult.value.version,
    input.expectedVersion,
    { workflowDraftId: input.workflowDraftId },
  );
  if (!versionResult.ok) {
    return versionResult;
  }

  if (draftResult.value.status === WORKFLOW_ADMIN_DRAFT_STATUSES.PUBLISHED) {
    return err(
      validationFailedError({
        workflowDraftId: input.workflowDraftId,
        reason: "published draft status cannot be changed",
      }),
    );
  }

  const updatedDraft: WorkflowAdminDraftRecord = {
    ...draftResult.value,
    status: input.status,
    updatedByActorId: input.actorId,
    version: draftResult.value.version + 1,
    updatedAt: nowIso(),
  };

  store.workflowAdminDrafts.set(updatedDraft.workflowDraftId, updatedDraft);
  return ok(updatedDraft);
}

function publishAdminVersionInTransaction(
  store: HcmNextStore,
  input: {
    family: WorkflowAdminFamilyRecord;
    draft: WorkflowAdminDraftRecord;
    actorId: string;
    previousActiveVersion?: WorkflowAdminVersionRecord | undefined;
  },
): PublishWorkflowDraftResult {
  const timestamp = nowIso();
  const publishedVersion: WorkflowAdminVersionRecord = {
    workflowVersionRecordId: makeId("workflow_admin_version"),
    workflowFamilyId: input.family.workflowFamilyId,
    tenantId: input.family.tenantId,
    environmentId: input.family.environmentId,
    publishedVersion: nextPublishedVersion(store, input.family.workflowFamilyId),
    configJson: cloneJsonRecord(input.draft.configJson),
    configChecksum: input.draft.configChecksum,
    status: WORKFLOW_ADMIN_VERSION_STATUSES.PUBLISHED,
    isActive: true,
    publishedByActorId: input.actorId,
    publishedAt: timestamp,
    sourceWorkflowDraftId: input.draft.workflowDraftId,
    createdAt: timestamp,
    metadata: {
      sourceTemplateId: input.draft.sourceTemplateId,
      sourceWorkflowVersionRecordId: input.draft.sourceWorkflowVersionRecordId,
      sourceMetadata: input.draft.sourceMetadata,
      overrideMetadata: input.draft.overrideMetadata,
    },
  };
  const updatedDraft: WorkflowAdminDraftRecord = {
    ...input.draft,
    status: WORKFLOW_ADMIN_DRAFT_STATUSES.PUBLISHED,
    updatedByActorId: input.actorId,
    version: input.draft.version + 1,
    updatedAt: timestamp,
  };
  const updatedFamily: WorkflowAdminFamilyRecord = {
    ...input.family,
    activeWorkflowVersionRecordId: publishedVersion.workflowVersionRecordId,
    version: input.family.version + 1,
    updatedAt: timestamp,
  };
  const history = createPublishHistory({
    family: updatedFamily,
    action: WORKFLOW_ADMIN_PUBLISH_ACTIONS.PUBLISHED,
    actorId: input.actorId,
    publishedWorkflowVersionRecordId: publishedVersion.workflowVersionRecordId,
    sourceWorkflowDraftId: input.draft.workflowDraftId,
    previousActiveWorkflowVersionRecordId:
      input.previousActiveVersion?.workflowVersionRecordId,
    occurredAt: timestamp,
  });

  deactivateFamilyVersions(store, input.family.workflowFamilyId);
  store.workflowAdminVersions.set(
    publishedVersion.workflowVersionRecordId,
    publishedVersion,
  );
  store.workflowAdminDrafts.set(updatedDraft.workflowDraftId, updatedDraft);
  store.workflowAdminFamilies.set(updatedFamily.workflowFamilyId, updatedFamily);
  store.workflowPublishHistory.push(history);

  return {
    family: updatedFamily,
    draft: updatedDraft,
    publishedVersion,
    ...(input.previousActiveVersion !== undefined
      ? { previousActiveVersion: input.previousActiveVersion }
      : {}),
    history,
  };
}

function activateExistingVersionInTransaction(
  store: HcmNextStore,
  input: {
    family: WorkflowAdminFamilyRecord;
    actorId: string;
    targetVersion: WorkflowAdminVersionRecord;
    previousActiveVersion?: WorkflowAdminVersionRecord | undefined;
  },
): RollbackWorkflowFamilyResult {
  const timestamp = nowIso();
  const activatedVersion: WorkflowAdminVersionRecord = {
    ...input.targetVersion,
    status: WORKFLOW_ADMIN_VERSION_STATUSES.PUBLISHED,
    isActive: true,
  };
  const updatedFamily: WorkflowAdminFamilyRecord = {
    ...input.family,
    activeWorkflowVersionRecordId: activatedVersion.workflowVersionRecordId,
    version: input.family.version + 1,
    updatedAt: timestamp,
  };
  const history = createPublishHistory({
    family: updatedFamily,
    action: WORKFLOW_ADMIN_PUBLISH_ACTIONS.ROLLED_BACK,
    actorId: input.actorId,
    publishedWorkflowVersionRecordId: activatedVersion.workflowVersionRecordId,
    previousActiveWorkflowVersionRecordId:
      input.previousActiveVersion?.workflowVersionRecordId,
    rollbackTargetWorkflowVersionRecordId: activatedVersion.workflowVersionRecordId,
    occurredAt: timestamp,
  });

  deactivateFamilyVersions(store, input.family.workflowFamilyId);
  store.workflowAdminVersions.set(
    activatedVersion.workflowVersionRecordId,
    activatedVersion,
  );
  store.workflowAdminFamilies.set(updatedFamily.workflowFamilyId, updatedFamily);
  store.workflowPublishHistory.push(history);

  return {
    family: updatedFamily,
    activeVersion: activatedVersion,
    ...(input.previousActiveVersion !== undefined
      ? { previousActiveVersion: input.previousActiveVersion }
      : {}),
    history,
  };
}

function createPublishHistory(input: {
  family: WorkflowAdminFamilyRecord;
  action:
    | typeof WORKFLOW_ADMIN_PUBLISH_ACTIONS.PUBLISHED
    | typeof WORKFLOW_ADMIN_PUBLISH_ACTIONS.ROLLED_BACK;
  actorId: string;
  publishedWorkflowVersionRecordId: string;
  sourceWorkflowDraftId?: string | undefined;
  previousActiveWorkflowVersionRecordId?: string | undefined;
  rollbackTargetWorkflowVersionRecordId?: string | undefined;
  occurredAt: string;
}): WorkflowPublishHistoryRecord {
  return {
    workflowPublishHistoryId: makeId("workflow_publish_history"),
    workflowFamilyId: input.family.workflowFamilyId,
    tenantId: input.family.tenantId,
    environmentId: input.family.environmentId,
    action: input.action,
    ...(input.sourceWorkflowDraftId !== undefined
      ? { sourceWorkflowDraftId: input.sourceWorkflowDraftId }
      : {}),
    publishedWorkflowVersionRecordId: input.publishedWorkflowVersionRecordId,
    ...(input.previousActiveWorkflowVersionRecordId !== undefined
      ? {
          previousActiveWorkflowVersionRecordId:
            input.previousActiveWorkflowVersionRecordId,
        }
      : {}),
    ...(input.rollbackTargetWorkflowVersionRecordId !== undefined
      ? {
          rollbackTargetWorkflowVersionRecordId:
            input.rollbackTargetWorkflowVersionRecordId,
        }
      : {}),
    actorId: input.actorId,
    occurredAt: input.occurredAt,
    metadata: {},
  };
}

function activeVersionForFamily(
  store: HcmNextStore,
  workflowFamilyId: string,
): WorkflowAdminVersionRecord | undefined {
  return [...store.workflowAdminVersions.values()].find((version) => {
    return version.workflowFamilyId === workflowFamilyId && version.isActive;
  });
}

function deactivateFamilyVersions(store: HcmNextStore, workflowFamilyId: string): void {
  for (const version of store.workflowAdminVersions.values()) {
    if (version.workflowFamilyId !== workflowFamilyId || !version.isActive) {
      continue;
    }

    store.workflowAdminVersions.set(version.workflowVersionRecordId, {
      ...version,
      status: WORKFLOW_ADMIN_VERSION_STATUSES.INACTIVE,
      isActive: false,
    });
  }
}

function nextDraftVersion(store: HcmNextStore, workflowFamilyId: string): number {
  const versionNumbers = [...store.workflowAdminDrafts.values()]
    .filter((draft) => draft.workflowFamilyId === workflowFamilyId)
    .map((draft) => draft.draftVersion);

  return Math.max(0, ...versionNumbers) + 1;
}

function nextPublishedVersion(store: HcmNextStore, workflowFamilyId: string): number {
  const versionNumbers = [...store.workflowAdminVersions.values()]
    .filter((version) => version.workflowFamilyId === workflowFamilyId)
    .map((version) => version.publishedVersion);

  return Math.max(0, ...versionNumbers) + 1;
}

function requireExpectedVersion(
  actualVersion: number,
  expectedVersion: number,
  details: Record<string, unknown>,
): Result<true, AppError> {
  if (actualVersion !== expectedVersion) {
    return err(
      versionConflictError({
        ...details,
        expectedVersion,
        actualVersion,
      }),
    );
  }

  return ok(true);
}

function compareFamilies(
  left: WorkflowAdminFamilyRecord,
  right: WorkflowAdminFamilyRecord,
): number {
  return left.intent.localeCompare(right.intent);
}

function cloneJsonRecord(value: Record<string, unknown>): Record<string, unknown> {
  return JSON.parse(JSON.stringify(value)) as Record<string, unknown>;
}
