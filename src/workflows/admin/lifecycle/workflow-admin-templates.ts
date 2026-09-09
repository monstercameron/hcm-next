import {
  WORKFLOW_ADMIN_TEMPLATE_STATUSES,
  WORKFLOW_CONFIG_SOURCES,
  ok,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type {
  Repositories,
  WorkflowAdminDraftRecord,
  WorkflowAdminFamilyRecord,
  WorkflowTemplateRecord,
} from "@human-capital-management-suite/data-store";
import type { RequestContext } from "@human-capital-management-suite/foundation";
import {
  cloneWorkflowConfig,
  listFilesystemWorkflowConfigEntries,
  workflowConfigFromGraphDefinition,
  workflowConfigHash,
} from "../../shared/workflow-config-registry.js";
import type { WorkflowConfig } from "../../shared/workflow-config.js";
import {
  createWorkflowDraftFromConfig,
  ensureWorkflowFamilyForConfig,
} from "./workflow-admin-lifecycle.js";

export type WorkflowTemplateSeedResult = {
  seeded: number;
  templates: WorkflowTemplateRecord[];
};

export type WorkflowTemplateCloneResult = {
  family: WorkflowAdminFamilyRecord;
  draft: WorkflowAdminDraftRecord;
  template: WorkflowTemplateRecord;
};

export type WorkflowPublishedVersionCloneResult = {
  family: WorkflowAdminFamilyRecord;
  draft: WorkflowAdminDraftRecord;
};

/**
 * Seeds reusable workflow templates from the checked-in development configs.
 */
export function seedDefaultWorkflowTemplates(
  repositories: Repositories,
): Result<WorkflowTemplateSeedResult, AppError> {
  const templates: WorkflowTemplateRecord[] = [];

  for (const entry of listFilesystemWorkflowConfigEntries()) {
    const workflowConfig = entry.workflowConfig;
    const templateResult = repositories.workflowAdmin.upsertTemplate({
      name: workflowConfig.metadata?.title ?? workflowConfig.intent,
      intent: workflowConfig.intent,
      hcmDomain: workflowConfig.metadata?.domain ?? inferredDomain(workflowConfig),
      description: workflowConfig.metadata?.description,
      templateVersion: 1,
      configJson: cloneConfigRecord(workflowConfig),
      configChecksum: workflowConfigHash(workflowConfig),
      status: WORKFLOW_ADMIN_TEMPLATE_STATUSES.ACTIVE,
      metadata: {
        source: WORKFLOW_CONFIG_SOURCES.FILESYSTEM,
        fileName: entry.fileName,
        subjectType: workflowConfig.subjectType,
        schemaVersion:
          workflowConfig.schemaVersion ?? workflowConfig.metadata?.schemaVersion,
      },
    });
    if (!templateResult.ok) {
      return templateResult;
    }

    templates.push(templateResult.value);
  }

  return ok({
    seeded: templates.length,
    templates,
  });
}

/**
 * Clones a template into a tenant/environment-scoped editable draft.
 */
export function cloneWorkflowTemplateIntoDraft(input: {
  repositories: Repositories;
  context: RequestContext;
  workflowTemplateId: string;
  overrideMetadata?: Record<string, unknown> | undefined;
}): Result<WorkflowTemplateCloneResult, AppError> {
  const templateResult = input.repositories.workflowAdmin.findTemplateById(
    input.workflowTemplateId,
  );
  if (!templateResult.ok) {
    return templateResult;
  }

  const workflowConfigResult = workflowConfigFromGraphDefinition(
    templateResult.value.configJson,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const familyResult = ensureWorkflowFamilyForConfig({
    repositories: input.repositories,
    context: input.context,
    workflowConfig: workflowConfigResult.value,
  });
  if (!familyResult.ok) {
    return familyResult;
  }

  const draftResult = createWorkflowDraftFromConfig({
    repositories: input.repositories,
    context: input.context,
    workflowFamilyId: familyResult.value.workflowFamilyId,
    workflowConfig: workflowConfigResult.value,
    sourceTemplateId: templateResult.value.workflowTemplateId,
    sourceMetadata: {
      source: WORKFLOW_CONFIG_SOURCES.TEMPLATE,
      templateName: templateResult.value.name,
      templateVersion: templateResult.value.templateVersion,
    },
    overrideMetadata: input.overrideMetadata,
  });
  if (!draftResult.ok) {
    return draftResult;
  }

  return ok({
    family: draftResult.value.family,
    draft: draftResult.value.draft,
    template: templateResult.value,
  });
}

/**
 * Clones an existing published workflow version into a new editable draft.
 */
export function clonePublishedWorkflowVersionIntoDraft(input: {
  repositories: Repositories;
  context: RequestContext;
  workflowVersionRecordId: string;
  overrideMetadata?: Record<string, unknown> | undefined;
}): Result<WorkflowPublishedVersionCloneResult, AppError> {
  const versionResult = input.repositories.workflowAdmin.findVersionById({
    tenantId: input.context.tenantId,
    workflowVersionRecordId: input.workflowVersionRecordId,
  });
  if (!versionResult.ok) {
    return versionResult;
  }

  const workflowConfigResult = workflowConfigFromGraphDefinition(
    versionResult.value.configJson,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }

  const familyResult = input.repositories.workflowAdmin.findFamilyById({
    tenantId: input.context.tenantId,
    workflowFamilyId: versionResult.value.workflowFamilyId,
  });
  if (!familyResult.ok) {
    return familyResult;
  }

  const draftResult = createWorkflowDraftFromConfig({
    repositories: input.repositories,
    context: input.context,
    workflowFamilyId: familyResult.value.workflowFamilyId,
    workflowConfig: workflowConfigResult.value,
    sourceWorkflowVersionRecordId: versionResult.value.workflowVersionRecordId,
    sourceMetadata: {
      source: WORKFLOW_CONFIG_SOURCES.PUBLISHED_VERSION,
      publishedVersion: versionResult.value.publishedVersion,
      configChecksum: versionResult.value.configChecksum,
    },
    overrideMetadata: input.overrideMetadata,
  });
  if (!draftResult.ok) {
    return draftResult;
  }

  return ok({
    family: draftResult.value.family,
    draft: draftResult.value.draft,
  });
}

function inferredDomain(workflowConfig: WorkflowConfig): string {
  return workflowConfig.intent.startsWith("position.") ? "position" : "employee";
}

function cloneConfigRecord(workflowConfig: WorkflowConfig): Record<string, unknown> {
  return cloneWorkflowConfig(workflowConfig) as unknown as Record<string, unknown>;
}
