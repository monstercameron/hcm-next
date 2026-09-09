import { describe, expect, it } from "vitest";
import { ERROR_CODES } from "@human-capital-management-suite/foundation";
import { createRepositories } from "../repositories.js";
import { createEmptyStore } from "../store.js";

const tenantA = "tenant_a";
const tenantB = "tenant_b";
const environmentA = "env_a";
const environmentB = "env_b";
const actorId = "actor_admin";
const intent = "employee.legal_name.change";

describe("in-memory workflow admin repository", () => {
  it("scopes workflow families by tenant and environment", () => {
    const repositories = createRepositories(createEmptyStore());
    const createdFamily = repositories.workflowAdmin.createFamily({
      tenantId: tenantA,
      environmentId: environmentA,
      intent,
      name: "Legal name change",
      hcmDomain: "employee",
      ownerActorId: actorId,
    });

    expect(createdFamily.ok).toBe(true);

    const tenantAFamilies = repositories.workflowAdmin.listFamilies({
      tenantId: tenantA,
      environmentId: environmentA,
    });
    const tenantBFamilies = repositories.workflowAdmin.listFamilies({
      tenantId: tenantB,
      environmentId: environmentA,
    });

    expect(tenantAFamilies.ok && tenantAFamilies.value).toHaveLength(1);
    expect(tenantBFamilies.ok && tenantBFamilies.value).toHaveLength(0);

    const duplicateFamily = repositories.workflowAdmin.createFamily({
      tenantId: tenantA,
      environmentId: environmentA,
      intent,
      name: "Duplicate legal name change",
      hcmDomain: "employee",
      ownerActorId: actorId,
    });
    expect(duplicateFamily.ok).toBe(false);

    const sameIntentDifferentEnvironment = repositories.workflowAdmin.createFamily({
      tenantId: tenantA,
      environmentId: environmentB,
      intent,
      name: "Legal name change sandbox",
      hcmDomain: "employee",
      ownerActorId: actorId,
    });
    const sameIntentDifferentTenant = repositories.workflowAdmin.createFamily({
      tenantId: tenantB,
      environmentId: environmentA,
      intent,
      name: "Legal name change tenant B",
      hcmDomain: "employee",
      ownerActorId: actorId,
    });

    expect(sameIntentDifferentEnvironment.ok).toBe(true);
    expect(sameIntentDifferentTenant.ok).toBe(true);
  });

  it("enforces draft concurrency, immutable published drafts, publish activation, and rollback", () => {
    const repositories = createRepositories(createEmptyStore());
    const family = unwrap(
      repositories.workflowAdmin.createFamily({
        tenantId: tenantA,
        environmentId: environmentA,
        intent,
        name: "Legal name change",
        hcmDomain: "employee",
        ownerActorId: actorId,
      }),
    );
    const firstDraft = unwrap(
      repositories.workflowAdmin.createDraft({
        tenantId: tenantA,
        environmentId: environmentA,
        workflowFamilyId: family.workflowFamilyId,
        configJson: configRecord("first"),
        configChecksum: "hash_first",
        actorId,
      }),
    );

    const staleSave = repositories.workflowAdmin.saveDraft({
      tenantId: tenantA,
      workflowDraftId: firstDraft.workflowDraftId,
      expectedVersion: firstDraft.version + 1,
      configJson: configRecord("stale"),
      configChecksum: "hash_stale",
      actorId,
    });
    expect(staleSave.ok).toBe(false);
    expect(staleSave.ok ? undefined : staleSave.error.code).toBe(
      ERROR_CODES.VERSION_CONFLICT,
    );

    const firstPublish = unwrap(
      repositories.workflowAdmin.publishDraft({
        tenantId: tenantA,
        workflowDraftId: firstDraft.workflowDraftId,
        expectedVersion: firstDraft.version,
        actorId,
      }),
    );
    expect(firstPublish.publishedVersion.isActive).toBe(true);
    expect(firstPublish.publishedVersion.publishedVersion).toBe(1);

    const editPublishedDraft = repositories.workflowAdmin.saveDraft({
      tenantId: tenantA,
      workflowDraftId: firstDraft.workflowDraftId,
      expectedVersion: firstPublish.draft.version,
      configJson: configRecord("edited"),
      configChecksum: "hash_edited",
      actorId,
    });
    expect(editPublishedDraft.ok).toBe(false);

    const secondDraft = unwrap(
      repositories.workflowAdmin.createDraft({
        tenantId: tenantA,
        environmentId: environmentA,
        workflowFamilyId: family.workflowFamilyId,
        configJson: configRecord("second"),
        configChecksum: "hash_second",
        actorId,
      }),
    );
    const secondPublish = unwrap(
      repositories.workflowAdmin.publishDraft({
        tenantId: tenantA,
        workflowDraftId: secondDraft.workflowDraftId,
        expectedVersion: secondDraft.version,
        actorId,
      }),
    );

    const firstVersionAfterSecondPublish = unwrap(
      repositories.workflowAdmin.findVersionById({
        tenantId: tenantA,
        workflowVersionRecordId: firstPublish.publishedVersion.workflowVersionRecordId,
      }),
    );

    expect(firstVersionAfterSecondPublish.isActive).toBe(false);
    expect(secondPublish.publishedVersion.isActive).toBe(true);
    expect(secondPublish.previousActiveVersion?.workflowVersionRecordId).toBe(
      firstPublish.publishedVersion.workflowVersionRecordId,
    );

    const rollback = unwrap(
      repositories.workflowAdmin.rollbackToVersion({
        tenantId: tenantA,
        workflowFamilyId: family.workflowFamilyId,
        targetWorkflowVersionRecordId:
          firstPublish.publishedVersion.workflowVersionRecordId,
        actorId,
      }),
    );

    expect(rollback.activeVersion.workflowVersionRecordId).toBe(
      firstPublish.publishedVersion.workflowVersionRecordId,
    );
    expect(rollback.previousActiveVersion?.workflowVersionRecordId).toBe(
      secondPublish.publishedVersion.workflowVersionRecordId,
    );
  });
});

function configRecord(label: string): Record<string, unknown> {
  return {
    intent,
    subjectType: "worker",
    metadata: { title: label },
  };
}

function unwrap<TValue>(
  result: { ok: true; value: TValue } | { ok: false; error: unknown },
): TValue {
  if (!result.ok) {
    throw new Error(`Expected ok result: ${JSON.stringify(result.error)}`);
  }

  return result.value;
}
