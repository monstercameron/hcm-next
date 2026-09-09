import { describe, expect, it } from "vitest";
import {
  ACTOR_ROLES,
  ACTOR_TYPES,
  LEDGER_EVENT_TYPES,
  WORKFLOW_INTENTS,
  type RequestContext,
} from "@human-capital-management-suite/foundation";
import {
  createInitialWorkflowInstance,
  createRepositories,
  createSeededDemoStore,
  DEMO_IDS,
} from "@human-capital-management-suite/data-store";
import {
  cloneWorkflowConfig,
  findFilesystemWorkflowConfigByIntent,
} from "../../shared/workflow-config-registry.js";
import {
  resolveCurrentPublishedWorkflowConfig,
  resolvePinnedWorkflowConfig,
} from "../../shared/workflow-config-resolution.js";
import type { WorkflowConfig } from "../../shared/workflow-config.js";
import {
  publishWorkflowDraft,
  rollbackWorkflowFamilyToVersion,
  saveWorkflowDraftConfig,
} from "./workflow-admin-lifecycle.js";
import {
  clonePublishedWorkflowVersionIntoDraft,
  cloneWorkflowTemplateIntoDraft,
  seedDefaultWorkflowTemplates,
} from "./workflow-admin-templates.js";

describe("workflow admin lifecycle services", () => {
  it("clones templates, publishes immutable DB configs, pins instances, and rolls back active versions", () => {
    const repositories = createRepositories(createSeededDemoStore());
    const context = adminContext();
    const templateSeed = unwrap(seedDefaultWorkflowTemplates(repositories));
    const legalNameTemplate = templateSeed.templates.find((template) => {
      return template.intent === WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE;
    });

    expect(legalNameTemplate).toBeDefined();

    const firstDraft = unwrap(
      cloneWorkflowTemplateIntoDraft({
        repositories,
        context,
        workflowTemplateId: legalNameTemplate?.workflowTemplateId ?? "",
      }),
    );
    const firstPublish = unwrap(
      publishWorkflowDraft({
        repositories,
        context,
        workflowDraftId: firstDraft.draft.workflowDraftId,
        expectedVersion: firstDraft.draft.version,
      }),
    );

    const currentAfterFirstPublish = unwrap(
      resolveCurrentPublishedWorkflowConfig(
        repositories,
        context.tenantId,
        context.environmentId,
        WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      ),
    );
    expect(currentAfterFirstPublish.workflowDefinitionId).toBe(
      firstPublish.family.workflowFamilyId,
    );
    expect(currentAfterFirstPublish.workflowVersionId).toBe(
      firstPublish.publishedVersion.workflowVersionRecordId,
    );

    const editableTemplateClone = unwrap(
      cloneWorkflowTemplateIntoDraft({
        repositories,
        context,
        workflowTemplateId: legalNameTemplate?.workflowTemplateId ?? "",
      }),
    );
    unwrap(
      saveWorkflowDraftConfig({
        repositories,
        context,
        workflowDraftId: editableTemplateClone.draft.workflowDraftId,
        expectedVersion: editableTemplateClone.draft.version,
        workflowConfig: legalNameWorkflowConfigWithTitle("Template clone edit"),
      }),
    );
    const templateAfterCloneEdit = unwrap(
      repositories.workflowAdmin.findTemplateById(
        legalNameTemplate?.workflowTemplateId ?? "",
      ),
    );
    expect(metadataTitle(templateAfterCloneEdit.configJson)).not.toBe(
      "Template clone edit",
    );

    const pinnedWorkflowInstance = createInitialWorkflowInstance({
      tenantId: context.tenantId,
      environmentId: context.environmentId,
      workflowDefinitionId: firstPublish.family.workflowFamilyId,
      workflowVersionId: firstPublish.publishedVersion.workflowVersionRecordId,
      intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      subjectType: "worker",
      subjectId: DEMO_IDS.employeeId,
      requesterActorId: context.actorId,
      currentInteraction: {},
      context: {},
      correlationId: context.correlationId,
      metadata: {},
    });
    const pinnedConfigBeforeRepublish = unwrap(
      resolvePinnedWorkflowConfig(repositories, pinnedWorkflowInstance),
    );

    const secondDraft = unwrap(
      clonePublishedWorkflowVersionIntoDraft({
        repositories,
        context,
        workflowVersionRecordId: firstPublish.publishedVersion.workflowVersionRecordId,
      }),
    );
    const modifiedConfig = legalNameWorkflowConfigWithTitle(
      "Employee Legal Name Change v2",
    );
    const savedSecondDraft = unwrap(
      saveWorkflowDraftConfig({
        repositories,
        context,
        workflowDraftId: secondDraft.draft.workflowDraftId,
        expectedVersion: secondDraft.draft.version,
        workflowConfig: modifiedConfig,
      }),
    );
    const secondPublish = unwrap(
      publishWorkflowDraft({
        repositories,
        context,
        workflowDraftId: savedSecondDraft.workflowDraftId,
        expectedVersion: savedSecondDraft.version,
      }),
    );

    const currentAfterSecondPublish = unwrap(
      resolveCurrentPublishedWorkflowConfig(
        repositories,
        context.tenantId,
        context.environmentId,
        WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      ),
    );
    const pinnedConfigAfterRepublish = unwrap(
      resolvePinnedWorkflowConfig(repositories, pinnedWorkflowInstance),
    );

    expect(currentAfterSecondPublish.workflowVersionId).toBe(
      secondPublish.publishedVersion.workflowVersionRecordId,
    );
    expect(currentAfterSecondPublish.workflowConfig.metadata?.title).toBe(
      "Employee Legal Name Change v2",
    );
    expect(pinnedConfigAfterRepublish.metadata?.title).toBe(
      pinnedConfigBeforeRepublish.metadata?.title,
    );

    const rollback = unwrap(
      rollbackWorkflowFamilyToVersion({
        repositories,
        context,
        workflowFamilyId: firstPublish.family.workflowFamilyId,
        targetWorkflowVersionRecordId:
          firstPublish.publishedVersion.workflowVersionRecordId,
      }),
    );
    const currentAfterRollback = unwrap(
      resolveCurrentPublishedWorkflowConfig(
        repositories,
        context.tenantId,
        context.environmentId,
        WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      ),
    );

    expect(rollback.activeVersion.workflowVersionRecordId).toBe(
      firstPublish.publishedVersion.workflowVersionRecordId,
    );
    expect(currentAfterRollback.workflowVersionId).toBe(
      firstPublish.publishedVersion.workflowVersionRecordId,
    );
    expect(workflowAdminLedgerEventTypes(repositories)).toEqual(
      expect.arrayContaining([
        LEDGER_EVENT_TYPES.WORKFLOW_FAMILY_CREATED,
        LEDGER_EVENT_TYPES.WORKFLOW_DRAFT_CREATED,
        LEDGER_EVENT_TYPES.WORKFLOW_DRAFT_UPDATED,
        LEDGER_EVENT_TYPES.WORKFLOW_PUBLISHED,
        LEDGER_EVENT_TYPES.WORKFLOW_ROLLED_BACK,
      ]),
    );
  });
});

function adminContext(): RequestContext {
  return {
    tenantId: DEMO_IDS.tenantId,
    environmentId: DEMO_IDS.environmentId,
    actorId: DEMO_IDS.hrActorId,
    actorType: ACTOR_TYPES.HUMAN,
    roles: [ACTOR_ROLES.HR_ADMIN],
    requestId: "req_workflow_admin_lifecycle",
    correlationId: "corr_workflow_admin_lifecycle",
  };
}

function legalNameWorkflowConfigWithTitle(title: string): WorkflowConfig {
  const workflowConfig = unwrap(
    findFilesystemWorkflowConfigByIntent(WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE),
  );
  const clonedWorkflowConfig = cloneWorkflowConfig(workflowConfig);

  return {
    ...clonedWorkflowConfig,
    metadata: {
      ...clonedWorkflowConfig.metadata,
      title,
    },
  };
}

function workflowAdminLedgerEventTypes(
  repositories: ReturnType<typeof createRepositories>,
): string[] {
  return repositories.store.ledgerEvents
    .filter((event) => event.subjectType === "workflow_admin")
    .map((event) => event.eventType);
}

function metadataTitle(configJson: Record<string, unknown>): string | undefined {
  const metadata = configJson["metadata"];

  if (typeof metadata !== "object" || metadata === null) {
    return undefined;
  }

  const title = (metadata as Record<string, unknown>)["title"];

  return typeof title === "string" ? title : undefined;
}

function unwrap<TValue>(
  result: { ok: true; value: TValue } | { ok: false; error: unknown },
): TValue {
  if (!result.ok) {
    throw new Error(`Expected ok result: ${JSON.stringify(result.error)}`);
  }

  return result.value;
}
