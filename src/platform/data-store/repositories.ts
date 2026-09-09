import {
  err,
  notFoundError,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import {
  employeeProjectionKey,
  makeId,
  nowIso,
  transitionAttemptKey,
  type HcmNextStore,
} from "./store.js";
import { createWorkflowAdminRepository } from "./repositories/in-memory-workflow-admin-repository.js";
import type {
  AccessGrantRecord,
  ActorRecord,
  ApprovalGroupRecord,
  ApprovalTaskRecord,
  ChangeRequestRecord,
  DocumentRecord,
  EmployeeProjectionDocument,
  EmployeeProjectionRecord,
  IntegrationOutboxRecord,
  LedgerEventRecord,
  OrganizationRelationshipRecord,
  OrganizationUnitRecord,
  ProposedChangeRecord,
  RoleBindingRecord,
  TransactionPlanRecord,
  WorkerAssignmentRecord,
  WorkflowDefinitionRecord,
  WorkflowIntegrationBindingRecord,
  WorkflowInstanceDocumentRecord,
  WorkflowInstanceRecord,
  WorkflowTransitionAttemptRecord,
  WorkflowVersionRecord,
  WorkflowVersionValidationResultRecord,
  WorkflowVersionValidationStatus,
} from "./types.js";

const workflowDefinitionStatuses = {
  ACTIVE: "active",
  DEPRECATED: "deprecated",
  ARCHIVED: "archived",
} as const;

const workflowVersionStatuses = {
  PUBLISHED: "published",
  DEPRECATED: "deprecated",
} as const;

export type Repositories = ReturnType<typeof createRepositories>;

export function createRepositories(store: HcmNextStore) {
  return {
    actors: createActorRepository(store),
    accessGrants: createAccessGrantRepository(store),
    organizationUnits: createOrganizationUnitRepository(store),
    organizationRelationships: createOrganizationRelationshipRepository(store),
    workerAssignments: createWorkerAssignmentRepository(store),
    roleBindings: createRoleBindingRepository(store),
    workflows: createWorkflowRepository(store),
    workflowAdmin: createWorkflowAdminRepository(store),
    workflowIntegrationBindings: createWorkflowIntegrationBindingRepository(store),
    ledger: createLedgerRepository(store),
    changeRequests: createChangeRequestRepository(store),
    proposedChanges: createProposedChangeRepository(store),
    documents: createDocumentRepository(store),
    approvalGroups: createApprovalGroupRepository(store),
    approvals: createApprovalRepository(store),
    transactionPlans: createTransactionPlanRepository(store),
    employeeProjections: createEmployeeProjectionRepository(store),
    integrationOutbox: createIntegrationOutboxRepository(store),
    store,
  };
}

function createWorkflowIntegrationBindingRepository(store: HcmNextStore) {
  return {
    list(input: {
      tenantId: string;
      environmentId?: string | undefined;
    }): Result<WorkflowIntegrationBindingRecord[], AppError> {
      return ok(
        [...store.workflowIntegrationBindings.values()]
          .filter((binding) => {
            return (
              binding.tenantId === input.tenantId &&
              (input.environmentId === undefined ||
                binding.environmentId === input.environmentId)
            );
          })
          .sort((left, right) =>
            left.abstractConnectionId.localeCompare(right.abstractConnectionId),
          ),
      );
    },

    upsert(
      input: Omit<
        WorkflowIntegrationBindingRecord,
        "workflowIntegrationBindingId" | "createdAt" | "updatedAt"
      > & {
        workflowIntegrationBindingId?: string | undefined;
      },
    ): Result<WorkflowIntegrationBindingRecord, AppError> {
      const existingBinding = [...store.workflowIntegrationBindings.values()].find(
        (binding) => {
          return (
            binding.tenantId === input.tenantId &&
            binding.environmentId === input.environmentId &&
            binding.abstractConnectionId === input.abstractConnectionId
          );
        },
      );
      const timestamp = nowIso();
      const record: WorkflowIntegrationBindingRecord = {
        ...(existingBinding ?? {}),
        ...input,
        workflowIntegrationBindingId:
          existingBinding?.workflowIntegrationBindingId ??
          input.workflowIntegrationBindingId ??
          makeId("workflow_integration_binding"),
        createdAt: existingBinding?.createdAt ?? timestamp,
        updatedAt: timestamp,
      };

      store.workflowIntegrationBindings.set(
        record.workflowIntegrationBindingId,
        record,
      );

      return ok(record);
    },
  };
}

function createActorRepository(store: HcmNextStore) {
  return {
    findById(actorId: string): Result<ActorRecord, AppError> {
      const actor = store.actors.get(actorId);

      if (!actor) {
        return err(notFoundError("Actor", { actorId }));
      }

      return ok(actor);
    },

    findByLinkedWorkerId(
      tenantId: string,
      linkedWorkerId: string,
    ): Result<ActorRecord, AppError> {
      const actor = [...store.actors.values()].find((candidate) => {
        return (
          candidate.tenantId === tenantId &&
          candidate.linkedWorkerId === linkedWorkerId &&
          candidate.status === "active"
        );
      });

      if (actor === undefined) {
        return err(notFoundError("Actor", { linkedWorkerId }));
      }

      return ok(actor);
    },

    findFirstActiveByRole(
      tenantId: string,
      role: string,
    ): Result<ActorRecord, AppError> {
      const actor = [...store.actors.values()].find((candidate) => {
        return (
          candidate.tenantId === tenantId &&
          candidate.status === "active" &&
          candidate.roles.includes(role)
        );
      });

      if (actor === undefined) {
        return err(notFoundError("Actor", { role }));
      }

      return ok(actor);
    },
  };
}

function createAccessGrantRepository(store: HcmNextStore) {
  return {
    findActiveForActor(
      tenantId: string,
      actorId: string,
    ): Result<AccessGrantRecord[], AppError> {
      const now = nowIso();

      return ok(
        [...store.accessGrants.values()].filter((grant) => {
          return (
            grant.tenantId === tenantId &&
            grant.actorId === actorId &&
            grant.status === "active" &&
            grant.startsAt <= now &&
            (grant.expiresAt === undefined || grant.expiresAt > now)
          );
        }),
      );
    },
  };
}

function createOrganizationUnitRepository(store: HcmNextStore) {
  return {
    findById(
      tenantId: string,
      orgUnitId: string,
    ): Result<OrganizationUnitRecord, AppError> {
      const organizationUnit = store.organizationUnits.get(orgUnitId);

      if (organizationUnit === undefined || organizationUnit.tenantId !== tenantId) {
        return err(notFoundError("Organization unit", { orgUnitId }));
      }

      return ok(organizationUnit);
    },

    findByTenant(tenantId: string): Result<OrganizationUnitRecord[], AppError> {
      return ok(
        [...store.organizationUnits.values()].filter((organizationUnit) => {
          return organizationUnit.tenantId === tenantId;
        }),
      );
    },

    findByKey(
      tenantId: string,
      unitKey: string,
    ): Result<OrganizationUnitRecord, AppError> {
      const organizationUnit = [...store.organizationUnits.values()].find((unit) => {
        return unit.tenantId === tenantId && unit.unitKey === unitKey;
      });

      if (organizationUnit === undefined) {
        return err(notFoundError("Organization unit", { unitKey }));
      }

      return ok(organizationUnit);
    },
  };
}

function createOrganizationRelationshipRepository(store: HcmNextStore) {
  return {
    findByTenant(tenantId: string): Result<OrganizationRelationshipRecord[], AppError> {
      return ok(
        [...store.organizationRelationships.values()].filter((relationship) => {
          return relationship.tenantId === tenantId;
        }),
      );
    },

    findActiveFromUnit(
      tenantId: string,
      fromOrgUnitId: string,
    ): Result<OrganizationRelationshipRecord[], AppError> {
      const now = nowIso();

      return ok(
        [...store.organizationRelationships.values()].filter((relationship) => {
          return (
            relationship.tenantId === tenantId &&
            relationship.fromOrgUnitId === fromOrgUnitId &&
            relationship.status === "active" &&
            relationship.effectiveStart <= now &&
            (relationship.effectiveEnd === undefined || relationship.effectiveEnd > now)
          );
        }),
      );
    },
  };
}

function createWorkerAssignmentRepository(store: HcmNextStore) {
  return {
    create(record: WorkerAssignmentRecord): Result<WorkerAssignmentRecord, AppError> {
      store.workerAssignments.set(record.workerAssignmentId, record);
      return ok(record);
    },

    update(record: WorkerAssignmentRecord): Result<WorkerAssignmentRecord, AppError> {
      if (!store.workerAssignments.has(record.workerAssignmentId)) {
        return err(
          notFoundError("Worker assignment", {
            workerAssignmentId: record.workerAssignmentId,
          }),
        );
      }

      store.workerAssignments.set(record.workerAssignmentId, {
        ...record,
        updatedAt: nowIso(),
      });
      return ok(store.workerAssignments.get(record.workerAssignmentId)!);
    },

    findById(workerAssignmentId: string): Result<WorkerAssignmentRecord, AppError> {
      const workerAssignment = store.workerAssignments.get(workerAssignmentId);

      if (workerAssignment === undefined) {
        return err(notFoundError("Worker assignment", { workerAssignmentId }));
      }

      return ok(workerAssignment);
    },

    findActiveByTenant(tenantId: string): Result<WorkerAssignmentRecord[], AppError> {
      const now = nowIso();

      return ok(
        [...store.workerAssignments.values()].filter((assignment) => {
          return (
            assignment.tenantId === tenantId &&
            assignment.status === "active" &&
            assignment.effectiveStart <= now &&
            (assignment.effectiveEnd === undefined || assignment.effectiveEnd > now)
          );
        }),
      );
    },

    findActiveForEmployee(
      tenantId: string,
      employeeId: string,
    ): Result<WorkerAssignmentRecord[], AppError> {
      const now = nowIso();

      return ok(
        [...store.workerAssignments.values()].filter((assignment) => {
          return (
            assignment.tenantId === tenantId &&
            assignment.employeeId === employeeId &&
            assignment.status === "active" &&
            assignment.effectiveStart <= now &&
            (assignment.effectiveEnd === undefined || assignment.effectiveEnd > now)
          );
        }),
      );
    },

    findActiveForEmployeeByTypes(
      tenantId: string,
      employeeId: string,
      assignmentTypes: string[],
    ): Result<WorkerAssignmentRecord[], AppError> {
      const now = nowIso();

      return ok(
        [...store.workerAssignments.values()].filter((assignment) => {
          return (
            assignment.tenantId === tenantId &&
            assignment.employeeId === employeeId &&
            assignment.status === "active" &&
            assignmentTypes.includes(assignment.assignmentType) &&
            assignment.effectiveStart <= now &&
            (assignment.effectiveEnd === undefined || assignment.effectiveEnd > now)
          );
        }),
      );
    },

    findBySourceWorkflow(
      tenantId: string,
      sourceWorkflowInstanceId: string,
    ): Result<WorkerAssignmentRecord[], AppError> {
      return ok(
        [...store.workerAssignments.values()].filter((assignment) => {
          return (
            assignment.tenantId === tenantId &&
            assignment.sourceWorkflowInstanceId === sourceWorkflowInstanceId
          );
        }),
      );
    },

    findActiveForOrgUnit(
      tenantId: string,
      orgUnitId: string,
    ): Result<WorkerAssignmentRecord[], AppError> {
      const now = nowIso();

      return ok(
        [...store.workerAssignments.values()].filter((assignment) => {
          return (
            assignment.tenantId === tenantId &&
            assignment.orgUnitId === orgUnitId &&
            assignment.status === "active" &&
            assignment.effectiveStart <= now &&
            (assignment.effectiveEnd === undefined || assignment.effectiveEnd > now)
          );
        }),
      );
    },
  };
}

function createRoleBindingRepository(store: HcmNextStore) {
  return {
    create(record: RoleBindingRecord): Result<RoleBindingRecord, AppError> {
      store.roleBindings.set(record.roleBindingId, record);
      return ok(record);
    },

    update(record: RoleBindingRecord): Result<RoleBindingRecord, AppError> {
      if (!store.roleBindings.has(record.roleBindingId)) {
        return err(
          notFoundError("Role binding", { roleBindingId: record.roleBindingId }),
        );
      }

      store.roleBindings.set(record.roleBindingId, {
        ...record,
        updatedAt: nowIso(),
      });
      return ok(store.roleBindings.get(record.roleBindingId)!);
    },

    findActiveForActor(
      tenantId: string,
      actorId: string,
    ): Result<RoleBindingRecord[], AppError> {
      const now = nowIso();

      return ok(
        [...store.roleBindings.values()].filter((roleBinding) => {
          return (
            roleBinding.tenantId === tenantId &&
            roleBinding.actorId === actorId &&
            roleBinding.status === "active" &&
            roleBinding.effectiveStart <= now &&
            (roleBinding.effectiveEnd === undefined || roleBinding.effectiveEnd > now)
          );
        }),
      );
    },

    findActiveByActorRoleAndScope(
      tenantId: string,
      actorId: string,
      roleKey: string,
      scopeType: string,
    ): Result<RoleBindingRecord | undefined, AppError> {
      const now = nowIso();
      const roleBinding = [...store.roleBindings.values()].find((candidate) => {
        return (
          candidate.tenantId === tenantId &&
          candidate.actorId === actorId &&
          candidate.roleKey === roleKey &&
          candidate.scopeType === scopeType &&
          candidate.status === "active" &&
          candidate.effectiveStart <= now &&
          (candidate.effectiveEnd === undefined || candidate.effectiveEnd > now)
        );
      });

      return ok(roleBinding);
    },

    findBySourceWorkflow(
      tenantId: string,
      sourceWorkflowInstanceId: string,
    ): Result<RoleBindingRecord[], AppError> {
      return ok(
        [...store.roleBindings.values()].filter((roleBinding) => {
          return (
            roleBinding.tenantId === tenantId &&
            roleBinding.sourceWorkflowInstanceId === sourceWorkflowInstanceId
          );
        }),
      );
    },
  };
}

function createWorkflowRepository(store: HcmNextStore) {
  return {
    /** Lists workflow definitions for tenant-scoped admin registry screens. */
    listDefinitions(tenantId: string): Result<WorkflowDefinitionRecord[], AppError> {
      return ok(
        [...store.workflowDefinitions.values()].filter((definition) => {
          return definition.tenantId === tenantId;
        }),
      );
    },

    /** Lists workflow versions for a tenant, optionally scoped to one definition. */
    listVersions(
      tenantId: string,
      workflowDefinitionId?: string,
    ): Result<WorkflowVersionRecord[], AppError> {
      return ok(
        [...store.workflowVersions.values()].filter((version) => {
          return (
            version.tenantId === tenantId &&
            (workflowDefinitionId === undefined ||
              version.workflowDefinitionId === workflowDefinitionId)
          );
        }),
      );
    },

    /** Finds the current published workflow version for a tenant and public intent. */
    findCurrentPublishedVersionByIntent(
      tenantId: string,
      intent: string,
    ): Result<
      {
        definition: WorkflowDefinitionRecord;
        version: WorkflowVersionRecord;
      },
      AppError
    > {
      for (const definition of store.workflowDefinitions.values()) {
        if (
          definition.tenantId !== tenantId ||
          !isWorkflowDefinitionActive(definition)
        ) {
          continue;
        }

        const currentVersion = definition.currentVersionId
          ? store.workflowVersions.get(definition.currentVersionId)
          : undefined;

        if (
          currentVersion !== undefined &&
          isCurrentPublishedVersionForIntent(definition, currentVersion, intent)
        ) {
          return ok({
            definition,
            version: currentVersion,
          });
        }
      }

      return err(notFoundError("Workflow", { tenantId, intent }));
    },

    /** Finds one workflow version by tenant and version ID. */
    findVersionById(
      tenantId: string,
      workflowVersionId: string,
    ): Result<WorkflowVersionRecord, AppError> {
      return findWorkflowVersionById(store, tenantId, workflowVersionId);
    },

    /** Finds a workflow version by definition and deterministic config hash. */
    findVersionByHash(input: {
      tenantId: string;
      workflowDefinitionId: string;
      configHash: string;
    }): Result<WorkflowVersionRecord | undefined, AppError> {
      const workflowVersion = [...store.workflowVersions.values()].find((version) => {
        return (
          version.tenantId === input.tenantId &&
          version.workflowDefinitionId === input.workflowDefinitionId &&
          version.configHash === input.configHash
        );
      });

      return ok(workflowVersion);
    },

    /** Creates a workflow definition record from an imported workflow config. */
    createDefinition(
      record: WorkflowDefinitionRecord,
    ): Result<WorkflowDefinitionRecord, AppError> {
      const timestamp = nowIso();
      const workflowDefinition: WorkflowDefinitionRecord = {
        ...record,
        createdAt: record.createdAt ?? timestamp,
        updatedAt: record.updatedAt ?? timestamp,
        metadata: record.metadata ?? {},
      };

      store.workflowDefinitions.set(
        workflowDefinition.workflowDefinitionId,
        workflowDefinition,
      );
      return ok(workflowDefinition);
    },

    /** Creates a workflow version record from an imported workflow config. */
    createVersion(
      record: WorkflowVersionRecord,
    ): Result<WorkflowVersionRecord, AppError> {
      const timestamp = nowIso();
      const workflowVersion: WorkflowVersionRecord = {
        ...record,
        validationStatus: record.validationStatus ?? "not_validated",
        createdAt: record.createdAt ?? timestamp,
        updatedAt: record.updatedAt ?? timestamp,
        metadata: record.metadata ?? {},
      };

      store.workflowVersions.set(workflowVersion.workflowVersionId, workflowVersion);
      return ok(workflowVersion);
    },

    /** Persists workflow version validation output and validation status. */
    updateVersionValidation(input: {
      tenantId: string;
      workflowVersionId: string;
      validationStatus: WorkflowVersionValidationStatus;
      validationResult: WorkflowVersionValidationResultRecord;
      actorId?: string;
    }): Result<WorkflowVersionRecord, AppError> {
      const workflowVersionResult = findWorkflowVersionById(
        store,
        input.tenantId,
        input.workflowVersionId,
      );
      if (!workflowVersionResult.ok) {
        return workflowVersionResult;
      }

      const updatedVersion: WorkflowVersionRecord = {
        ...workflowVersionResult.value,
        validationStatus: input.validationStatus,
        validationResult: input.validationResult,
        updatedByActorId: input.actorId,
        updatedAt: nowIso(),
      };

      store.workflowVersions.set(updatedVersion.workflowVersionId, updatedVersion);
      return ok(updatedVersion);
    },

    /** Updates workflow version lifecycle status without publishing it as current. */
    updateVersionStatus(input: {
      tenantId: string;
      workflowVersionId: string;
      status: string;
      actorId?: string;
    }): Result<WorkflowVersionRecord, AppError> {
      const workflowVersionResult = findWorkflowVersionById(
        store,
        input.tenantId,
        input.workflowVersionId,
      );
      if (!workflowVersionResult.ok) {
        return workflowVersionResult;
      }

      const updatedVersion: WorkflowVersionRecord = {
        ...workflowVersionResult.value,
        status: input.status,
        updatedByActorId: input.actorId,
        updatedAt: nowIso(),
      };

      store.workflowVersions.set(updatedVersion.workflowVersionId, updatedVersion);
      return ok(updatedVersion);
    },

    /** Publishes a workflow version and makes it the definition's current version. */
    publishVersionAsCurrent(input: {
      tenantId: string;
      workflowVersionId: string;
      actorId?: string;
    }): Result<
      {
        definition: WorkflowDefinitionRecord;
        version: WorkflowVersionRecord;
      },
      AppError
    > {
      const workflowVersionResult = findWorkflowVersionById(
        store,
        input.tenantId,
        input.workflowVersionId,
      );
      if (!workflowVersionResult.ok) {
        return workflowVersionResult;
      }

      const workflowDefinition = store.workflowDefinitions.get(
        workflowVersionResult.value.workflowDefinitionId,
      );
      if (
        workflowDefinition === undefined ||
        workflowDefinition.tenantId !== input.tenantId
      ) {
        return err(
          notFoundError("Workflow definition", {
            workflowDefinitionId: workflowVersionResult.value.workflowDefinitionId,
          }),
        );
      }

      const timestamp = nowIso();
      const updatedVersion: WorkflowVersionRecord = {
        ...workflowVersionResult.value,
        status: workflowVersionStatuses.PUBLISHED,
        publishedAt: workflowVersionResult.value.publishedAt ?? timestamp,
        publishedByActorId:
          workflowVersionResult.value.publishedByActorId ?? input.actorId,
        deprecatedAt: undefined,
        deprecatedByActorId: undefined,
        updatedByActorId: input.actorId,
        updatedAt: timestamp,
      };
      const updatedDefinition: WorkflowDefinitionRecord = {
        ...workflowDefinition,
        status: workflowDefinitionStatuses.ACTIVE,
        currentVersionId: updatedVersion.workflowVersionId,
        activatedAt: workflowDefinition.activatedAt ?? timestamp,
        updatedByActorId: input.actorId,
        updatedAt: timestamp,
      };

      store.workflowVersions.set(updatedVersion.workflowVersionId, updatedVersion);
      store.workflowDefinitions.set(
        updatedDefinition.workflowDefinitionId,
        updatedDefinition,
      );

      return ok({
        definition: updatedDefinition,
        version: updatedVersion,
      });
    },

    /** Deprecates a non-current workflow version. */
    deprecateVersion(input: {
      tenantId: string;
      workflowVersionId: string;
      actorId?: string;
    }): Result<WorkflowVersionRecord, AppError> {
      const workflowVersionResult = findWorkflowVersionById(
        store,
        input.tenantId,
        input.workflowVersionId,
      );
      if (!workflowVersionResult.ok) {
        return workflowVersionResult;
      }

      const workflowDefinition = store.workflowDefinitions.get(
        workflowVersionResult.value.workflowDefinitionId,
      );
      if (
        workflowDefinition !== undefined &&
        workflowDefinition.currentVersionId === input.workflowVersionId
      ) {
        return err(
          validationFailedError({
            workflowVersionId: input.workflowVersionId,
            reason: "current workflow version cannot be deprecated",
          }),
        );
      }

      const timestamp = nowIso();
      const updatedVersion: WorkflowVersionRecord = {
        ...workflowVersionResult.value,
        status: workflowVersionStatuses.DEPRECATED,
        deprecatedAt: workflowVersionResult.value.deprecatedAt ?? timestamp,
        deprecatedByActorId:
          workflowVersionResult.value.deprecatedByActorId ?? input.actorId,
        updatedByActorId: input.actorId,
        updatedAt: timestamp,
      };

      store.workflowVersions.set(updatedVersion.workflowVersionId, updatedVersion);
      return ok(updatedVersion);
    },

    /** Finds a workflow version by public intent for legacy runtime callers. */
    findVersionByIntent(intent: string): Result<
      {
        workflowDefinitionId: string;
        workflowVersionId: string;
      },
      AppError
    > {
      for (const workflowVersion of store.workflowVersions.values()) {
        if (
          workflowVersion.graphDefinition["intent"] === intent &&
          workflowVersion.status === workflowVersionStatuses.PUBLISHED
        ) {
          return ok({
            workflowDefinitionId: workflowVersion.workflowDefinitionId,
            workflowVersionId: workflowVersion.workflowVersionId,
          });
        }
      }

      return err(notFoundError("Workflow", { intent }));
    },

    createInstance(
      record: WorkflowInstanceRecord,
    ): Result<WorkflowInstanceRecord, AppError> {
      store.workflowInstances.set(record.workflowInstanceId, record);
      return ok(record);
    },

    findInstanceById(
      workflowInstanceId: string,
    ): Result<WorkflowInstanceRecord, AppError> {
      const workflowInstance = store.workflowInstances.get(workflowInstanceId);

      if (!workflowInstance) {
        return err(notFoundError("Workflow instance", { workflowInstanceId }));
      }

      return ok(workflowInstance);
    },

    /**
     * Lists workflow instances for a tenant. Sorted newest-first by
     * `startedAt` so list-style consumers (AI chat agent, dashboards) see
     * the most recent work first.
     */
    listInstancesByTenant(
      tenantId: string,
    ): Result<WorkflowInstanceRecord[], AppError> {
      const instances = [...store.workflowInstances.values()]
        .filter((instance) => instance.tenantId === tenantId)
        .sort((left, right) => right.startedAt.localeCompare(left.startedAt));
      return ok(instances);
    },

    updateInstance(
      workflowInstance: WorkflowInstanceRecord,
    ): Result<WorkflowInstanceRecord, AppError> {
      const existingWorkflowInstance = store.workflowInstances.get(
        workflowInstance.workflowInstanceId,
      );

      if (!existingWorkflowInstance) {
        return err(
          notFoundError("Workflow instance", {
            workflowInstanceId: workflowInstance.workflowInstanceId,
          }),
        );
      }

      store.workflowInstances.set(workflowInstance.workflowInstanceId, {
        ...workflowInstance,
        updatedAt: nowIso(),
      });
      return ok(store.workflowInstances.get(workflowInstance.workflowInstanceId)!);
    },

    findTransitionAttempt(
      tenantId: string,
      workflowInstanceId: string,
      idempotencyKey: string,
    ): Result<WorkflowTransitionAttemptRecord | undefined, AppError> {
      const attempt = store.transitionAttempts.get(
        transitionAttemptKey(tenantId, workflowInstanceId, idempotencyKey),
      );

      return ok(attempt);
    },

    saveTransitionAttempt(
      attempt: WorkflowTransitionAttemptRecord,
    ): Result<WorkflowTransitionAttemptRecord, AppError> {
      store.transitionAttempts.set(
        transitionAttemptKey(
          attempt.tenantId,
          attempt.workflowInstanceId,
          attempt.idempotencyKey,
        ),
        attempt,
      );
      return ok(attempt);
    },
  };
}

function findWorkflowVersionById(
  store: HcmNextStore,
  tenantId: string,
  workflowVersionId: string,
): Result<WorkflowVersionRecord, AppError> {
  const workflowVersion = store.workflowVersions.get(workflowVersionId);

  if (workflowVersion === undefined || workflowVersion.tenantId !== tenantId) {
    return err(notFoundError("Workflow version", { workflowVersionId }));
  }

  return ok(workflowVersion);
}

function isWorkflowDefinitionActive(definition: WorkflowDefinitionRecord): boolean {
  return definition.status === workflowDefinitionStatuses.ACTIVE;
}

function isCurrentPublishedVersionForIntent(
  definition: WorkflowDefinitionRecord,
  version: WorkflowVersionRecord,
  intent: string,
): boolean {
  return (
    version.tenantId === definition.tenantId &&
    version.workflowDefinitionId === definition.workflowDefinitionId &&
    version.status === workflowVersionStatuses.PUBLISHED &&
    (definition.intent === intent || workflowVersionIntent(version) === intent)
  );
}

function workflowVersionIntent(version: WorkflowVersionRecord): string | undefined {
  const intent = version.graphDefinition["intent"];

  return typeof intent === "string" && intent.trim().length > 0
    ? intent.trim()
    : undefined;
}

function createLedgerRepository(store: HcmNextStore) {
  return {
    append(
      event: Omit<
        LedgerEventRecord,
        "eventId" | "eventVersion" | "eventSequence" | "recordedAt"
      >,
    ): Result<LedgerEventRecord, AppError> {
      const ledgerEvent: LedgerEventRecord = {
        ...event,
        eventId: makeId("evt"),
        eventVersion: 1,
        eventSequence: store.nextLedgerSequence,
        recordedAt: nowIso(),
      };

      store.nextLedgerSequence += 1;
      store.ledgerEvents.push(ledgerEvent);
      return ok(ledgerEvent);
    },

    findTimelineForWorkflow(
      tenantId: string,
      workflowInstanceId: string,
      changeRequestId?: string,
    ): Result<LedgerEventRecord[], AppError> {
      const events = store.ledgerEvents
        .filter((event) => {
          const matchesWorkflow =
            event.tenantId === tenantId &&
            event.workflowInstanceId === workflowInstanceId;
          const matchesChangeRequest =
            changeRequestId !== undefined &&
            event.tenantId === tenantId &&
            event.changeRequestId === changeRequestId;

          return matchesWorkflow || matchesChangeRequest;
        })
        .sort((left, right) => left.eventSequence - right.eventSequence);

      return ok(events);
    },
  };
}

function createChangeRequestRepository(store: HcmNextStore) {
  return {
    create(record: ChangeRequestRecord): Result<ChangeRequestRecord, AppError> {
      store.changeRequests.set(record.changeRequestId, record);
      return ok(record);
    },

    findById(changeRequestId: string): Result<ChangeRequestRecord, AppError> {
      const changeRequest = store.changeRequests.get(changeRequestId);

      if (!changeRequest) {
        return err(notFoundError("Change request", { changeRequestId }));
      }

      return ok(changeRequest);
    },

    update(record: ChangeRequestRecord): Result<ChangeRequestRecord, AppError> {
      if (!store.changeRequests.has(record.changeRequestId)) {
        return err(
          notFoundError("Change request", {
            changeRequestId: record.changeRequestId,
          }),
        );
      }

      store.changeRequests.set(record.changeRequestId, {
        ...record,
        updatedAt: nowIso(),
      });
      return ok(store.changeRequests.get(record.changeRequestId)!);
    },
  };
}

function createProposedChangeRepository(store: HcmNextStore) {
  return {
    createMany(
      records: ProposedChangeRecord[],
    ): Result<ProposedChangeRecord[], AppError> {
      for (const record of records) {
        store.proposedChanges.set(record.proposedChangeId, record);
      }

      return ok(records);
    },

    findByChangeRequest(
      changeRequestId: string,
    ): Result<ProposedChangeRecord[], AppError> {
      return ok(
        [...store.proposedChanges.values()].filter(
          (record) => record.changeRequestId === changeRequestId,
        ),
      );
    },
  };
}

function createDocumentRepository(store: HcmNextStore) {
  return {
    create(record: DocumentRecord): Result<DocumentRecord, AppError> {
      store.documents.set(record.documentId, record);
      return ok(record);
    },

    findById(documentId: string): Result<DocumentRecord, AppError> {
      const document = store.documents.get(documentId);

      if (!document) {
        return err(notFoundError("Document", { documentId }));
      }

      return ok(document);
    },

    attachToWorkflow(
      record: WorkflowInstanceDocumentRecord,
    ): Result<WorkflowInstanceDocumentRecord, AppError> {
      store.workflowInstanceDocuments.set(record.workflowInstanceDocumentId, record);
      return ok(record);
    },
  };
}

function createApprovalGroupRepository(store: HcmNextStore) {
  return {
    /** Creates an approval gate-level group record. */
    create(record: ApprovalGroupRecord): Result<ApprovalGroupRecord, AppError> {
      store.approvalGroups.set(record.approvalGroupId, record);
      return ok(record);
    },

    /** Finds an approval group by its durable ID. */
    findById(approvalGroupId: string): Result<ApprovalGroupRecord, AppError> {
      const approvalGroup = store.approvalGroups.get(approvalGroupId);

      if (approvalGroup === undefined) {
        return err(notFoundError("Approval group", { approvalGroupId }));
      }

      return ok(approvalGroup);
    },

    /** Lists approval groups created for a workflow instance. */
    findByWorkflow(
      workflowInstanceId: string,
    ): Result<ApprovalGroupRecord[], AppError> {
      return ok(
        [...store.approvalGroups.values()].filter((approvalGroup) => {
          return approvalGroup.workflowInstanceId === workflowInstanceId;
        }),
      );
    },

    /** Finds the active approval group for a workflow instance, if one exists. */
    findActiveByWorkflow(
      workflowInstanceId: string,
    ): Result<ApprovalGroupRecord | undefined, AppError> {
      const approvalGroup = [...store.approvalGroups.values()].find((candidate) => {
        return (
          candidate.workflowInstanceId === workflowInstanceId &&
          candidate.status === "active"
        );
      });

      return ok(approvalGroup);
    },

    /** Updates an approval group and refreshes its modification timestamp. */
    update(record: ApprovalGroupRecord): Result<ApprovalGroupRecord, AppError> {
      if (!store.approvalGroups.has(record.approvalGroupId)) {
        return err(
          notFoundError("Approval group", {
            approvalGroupId: record.approvalGroupId,
          }),
        );
      }

      store.approvalGroups.set(record.approvalGroupId, {
        ...record,
        updatedAt: nowIso(),
      });
      return ok(store.approvalGroups.get(record.approvalGroupId)!);
    },
  };
}

function createApprovalRepository(store: HcmNextStore) {
  return {
    create(record: ApprovalTaskRecord): Result<ApprovalTaskRecord, AppError> {
      store.approvalTasks.set(record.approvalTaskId, record);
      return ok(record);
    },

    findById(approvalTaskId: string): Result<ApprovalTaskRecord, AppError> {
      const approvalTask = store.approvalTasks.get(approvalTaskId);

      if (!approvalTask) {
        return err(notFoundError("Approval task", { approvalTaskId }));
      }

      return ok(approvalTask);
    },

    update(record: ApprovalTaskRecord): Result<ApprovalTaskRecord, AppError> {
      if (!store.approvalTasks.has(record.approvalTaskId)) {
        return err(
          notFoundError("Approval task", {
            approvalTaskId: record.approvalTaskId,
          }),
        );
      }

      store.approvalTasks.set(record.approvalTaskId, record);
      return ok(record);
    },

    findPendingForActor(actor: ActorRecord): Result<ApprovalTaskRecord[], AppError> {
      const tasks = [...store.approvalTasks.values()].filter((task) => {
        const assignmentMode = task.metadata["assignmentMode"];
        const canUseRoleFallback =
          assignmentMode !== "actor" && actor.roles.includes(task.assigneeRole);

        return (
          task.status === "pending" &&
          (task.assigneeActorId === actor.actorId || canUseRoleFallback)
        );
      });

      return ok(tasks);
    },

    findByApprovalGroup(
      approvalGroupId: string,
    ): Result<ApprovalTaskRecord[], AppError> {
      return ok(
        [...store.approvalTasks.values()].filter((approvalTask) => {
          return approvalGroupIdForTask(approvalTask) === approvalGroupId;
        }),
      );
    },

    findPendingByApprovalGroup(
      approvalGroupId: string,
    ): Result<ApprovalTaskRecord[], AppError> {
      return ok(
        [...store.approvalTasks.values()].filter((approvalTask) => {
          return (
            approvalGroupIdForTask(approvalTask) === approvalGroupId &&
            approvalTask.status === "pending"
          );
        }),
      );
    },

    findPendingByWorkflow(
      workflowInstanceId: string,
    ): Result<ApprovalTaskRecord | undefined, AppError> {
      const pendingTasks = [...store.approvalTasks.values()].filter((approvalTask) => {
        return (
          approvalTask.workflowInstanceId === workflowInstanceId &&
          approvalTask.status === "pending"
        );
      });
      const legacyTask = pendingTasks.find((approvalTask) => {
        return approvalGroupIdForTask(approvalTask) === undefined;
      });

      return ok(legacyTask ?? pendingTasks[0]);
    },
  };
}

function approvalGroupIdForTask(approvalTask: ApprovalTaskRecord): string | undefined {
  if (approvalTask.approvalGroupId !== undefined) {
    return approvalTask.approvalGroupId;
  }

  const metadataApprovalGroupId = approvalTask.metadata["approvalGroupId"];

  return typeof metadataApprovalGroupId === "string"
    ? metadataApprovalGroupId
    : undefined;
}

function createTransactionPlanRepository(store: HcmNextStore) {
  return {
    create(record: TransactionPlanRecord): Result<TransactionPlanRecord, AppError> {
      store.transactionPlans.set(record.transactionPlanId, record);
      return ok(record);
    },

    findById(transactionPlanId: string): Result<TransactionPlanRecord, AppError> {
      const transactionPlan = store.transactionPlans.get(transactionPlanId);

      if (!transactionPlan) {
        return err(notFoundError("Transaction plan", { transactionPlanId }));
      }

      return ok(transactionPlan);
    },

    update(record: TransactionPlanRecord): Result<TransactionPlanRecord, AppError> {
      store.transactionPlans.set(record.transactionPlanId, {
        ...record,
        updatedAt: nowIso(),
      });
      return ok(store.transactionPlans.get(record.transactionPlanId)!);
    },
  };
}

function createEmployeeProjectionRepository(store: HcmNextStore) {
  return {
    findByTenant(tenantId: string): Result<EmployeeProjectionRecord[], AppError> {
      return ok(
        [...store.employeeProjections.values()].filter(
          (projection) => projection.tenantId === tenantId,
        ),
      );
    },

    findByEmployeeId(
      tenantId: string,
      employeeId: string,
    ): Result<EmployeeProjectionRecord, AppError> {
      const projection = store.employeeProjections.get(
        employeeProjectionKey(tenantId, employeeId),
      );

      if (!projection) {
        return err(notFoundError("Employee projection", { employeeId }));
      }

      return ok(projection);
    },

    updateDocument(
      tenantId: string,
      employeeId: string,
      document: EmployeeProjectionDocument,
      sourceEventId: string,
      sourceEventSequence: number,
    ): Result<EmployeeProjectionRecord, AppError> {
      const existingProjection = store.employeeProjections.get(
        employeeProjectionKey(tenantId, employeeId),
      );

      if (!existingProjection) {
        return err(notFoundError("Employee projection", { employeeId }));
      }

      const updatedProjection: EmployeeProjectionRecord = {
        ...existingProjection,
        projectionVersion: existingProjection.projectionVersion + 1,
        sourceEventId,
        sourceEventSequence,
        document,
        indexedFields: indexedFieldsForEmployeeDocument(document),
        updatedAt: nowIso(),
      };

      store.employeeProjections.set(
        employeeProjectionKey(tenantId, employeeId),
        updatedProjection,
      );
      return ok(updatedProjection);
    },
  };
}

function indexedFieldsForEmployeeDocument(
  document: EmployeeProjectionDocument,
): Record<string, unknown> {
  return {
    displayName: document.person.displayName,
    employmentStatus: document.employment.status,
    legalEntity: document.employment.legalEntity,
    businessUnit: document.organization.businessUnit,
    department: document.organization.department,
    team: document.organization.team,
    location: document.organization.location,
    costCenter: document.organization.costCenter,
    managerEmployeeId: document.manager.employeeId,
    jobCode: document.job.jobCode,
    jobLevel: document.job.level,
  };
}

function createIntegrationOutboxRepository(store: HcmNextStore) {
  return {
    create(record: IntegrationOutboxRecord): Result<IntegrationOutboxRecord, AppError> {
      store.integrationOutbox.set(record.outboxId, record);
      return ok(record);
    },

    findByIdempotencyKey(
      tenantId: string,
      idempotencyKey: string,
    ): Result<IntegrationOutboxRecord | undefined, AppError> {
      const outboxRow = [...store.integrationOutbox.values()].find((record) => {
        return record.tenantId === tenantId && record.idempotencyKey === idempotencyKey;
      });

      return ok(outboxRow);
    },

    findByChangeRequest(
      changeRequestId: string,
    ): Result<IntegrationOutboxRecord[], AppError> {
      return ok(
        [...store.integrationOutbox.values()].filter(
          (record) => record.changeRequestId === changeRequestId,
        ),
      );
    },
  };
}
