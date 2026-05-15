import {
  ok,
  type AppError,
  type RequestContext,
  type Result,
} from "@hcm-next/foundation";
import type {
  Repositories,
  WorkflowAdminDraftRecord,
  WorkflowAdminFamilyRecord,
  WorkflowAdminVersionRecord,
  WorkflowTemplateRecord,
} from "@hcm-next/data-store";

export type WorkflowRegistrySummary = {
  workflowFamilyId: string;
  tenantId: string;
  environmentId: string;
  intent: string;
  name: string;
  hcmDomain: string;
  ownerActorId: string;
  status: string;
  activeWorkflowVersionRecordId?: string | undefined;
  version: number;
  draftCount: number;
  publishedVersionCount: number;
  activePublishedVersion?: number | undefined;
  configSource: "database";
  updatedAt: string;
};

export type WorkflowRegistryDetail = WorkflowRegistrySummary & {
  family: WorkflowAdminFamilyRecord;
  drafts: WorkflowAdminDraftRecord[];
  versions: WorkflowAdminVersionRecord[];
};

/**
 * Lists workflow families with version counts for admin registry screens.
 */
export function listWorkflowRegistry(input: {
  repositories: Repositories;
  context: RequestContext;
}): Result<WorkflowRegistrySummary[], AppError> {
  const familiesResult = input.repositories.workflowAdmin.listFamilies({
    tenantId: input.context.tenantId,
    environmentId: input.context.environmentId,
  });
  if (!familiesResult.ok) {
    return familiesResult;
  }

  const summaries: WorkflowRegistrySummary[] = [];
  for (const family of familiesResult.value) {
    const detailResult = workflowRegistrySummaryForFamily(input.repositories, family);
    if (!detailResult.ok) {
      return detailResult;
    }

    summaries.push(detailResult.value);
  }

  return ok(summaries);
}

/**
 * Reads one workflow family with drafts and published versions.
 */
export function getWorkflowRegistryDetail(input: {
  repositories: Repositories;
  context: RequestContext;
  workflowFamilyId: string;
}): Result<WorkflowRegistryDetail, AppError> {
  const familyResult = input.repositories.workflowAdmin.findFamilyById({
    tenantId: input.context.tenantId,
    workflowFamilyId: input.workflowFamilyId,
  });
  if (!familyResult.ok) {
    return familyResult;
  }

  const draftsResult = input.repositories.workflowAdmin.listDraftsByFamily({
    tenantId: input.context.tenantId,
    workflowFamilyId: input.workflowFamilyId,
  });
  if (!draftsResult.ok) {
    return draftsResult;
  }

  const versionsResult = input.repositories.workflowAdmin.listVersionsByFamily({
    tenantId: input.context.tenantId,
    workflowFamilyId: input.workflowFamilyId,
  });
  if (!versionsResult.ok) {
    return versionsResult;
  }

  const summaryResult = workflowRegistrySummaryForFamily(
    input.repositories,
    familyResult.value,
  );
  if (!summaryResult.ok) {
    return summaryResult;
  }

  return ok({
    ...summaryResult.value,
    family: familyResult.value,
    drafts: draftsResult.value,
    versions: versionsResult.value,
  });
}

/**
 * Lists active workflow templates available for cloning.
 */
export function listWorkflowTemplates(input: {
  repositories: Repositories;
}): Result<WorkflowTemplateRecord[], AppError> {
  return input.repositories.workflowAdmin.listTemplates({ status: "active" });
}

function workflowRegistrySummaryForFamily(
  repositories: Repositories,
  family: WorkflowAdminFamilyRecord,
): Result<WorkflowRegistrySummary, AppError> {
  const draftsResult = repositories.workflowAdmin.listDraftsByFamily({
    tenantId: family.tenantId,
    workflowFamilyId: family.workflowFamilyId,
  });
  if (!draftsResult.ok) {
    return draftsResult;
  }

  const versionsResult = repositories.workflowAdmin.listVersionsByFamily({
    tenantId: family.tenantId,
    workflowFamilyId: family.workflowFamilyId,
  });
  if (!versionsResult.ok) {
    return versionsResult;
  }

  const activeVersion = versionsResult.value.find((version) => version.isActive);

  return ok({
    workflowFamilyId: family.workflowFamilyId,
    tenantId: family.tenantId,
    environmentId: family.environmentId,
    intent: family.intent,
    name: family.name,
    hcmDomain: family.hcmDomain,
    ownerActorId: family.ownerActorId,
    status: family.status,
    activeWorkflowVersionRecordId: family.activeWorkflowVersionRecordId,
    version: family.version,
    draftCount: draftsResult.value.length,
    publishedVersionCount: versionsResult.value.length,
    activePublishedVersion: activeVersion?.publishedVersion,
    configSource: "database",
    updatedAt: family.updatedAt,
  });
}
