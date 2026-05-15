import {
  err,
  notFoundError,
  ok,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  employeeProjectionKey,
  makeId,
  nowIso,
  transitionAttemptKey,
  type HcmNextStore,
} from "./store.js";
import type {
  ActorRecord,
  ApprovalTaskRecord,
  ChangeRequestRecord,
  DocumentRecord,
  EmployeeProjectionDocument,
  EmployeeProjectionRecord,
  IntegrationOutboxRecord,
  LedgerEventRecord,
  ProposedChangeRecord,
  TransactionPlanRecord,
  WorkflowInstanceDocumentRecord,
  WorkflowInstanceRecord,
  WorkflowTransitionAttemptRecord,
} from "./types.js";

export type Repositories = ReturnType<typeof createRepositories>;

export function createRepositories(store: HcmNextStore) {
  return {
    actors: createActorRepository(store),
    workflows: createWorkflowRepository(store),
    ledger: createLedgerRepository(store),
    changeRequests: createChangeRequestRepository(store),
    proposedChanges: createProposedChangeRepository(store),
    documents: createDocumentRepository(store),
    approvals: createApprovalRepository(store),
    transactionPlans: createTransactionPlanRepository(store),
    employeeProjections: createEmployeeProjectionRepository(store),
    integrationOutbox: createIntegrationOutboxRepository(store),
    store,
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
  };
}

function createWorkflowRepository(store: HcmNextStore) {
  return {
    findVersionByIntent(intent: string): Result<
      {
        workflowDefinitionId: string;
        workflowVersionId: string;
      },
      AppError
    > {
      for (const workflowVersion of store.workflowVersions.values()) {
        if (workflowVersion.graphDefinition["intent"] === intent) {
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
        return (
          task.status === "pending" &&
          (task.assigneeActorId === actor.actorId ||
            (actor.roles.includes("hr_admin") && task.assigneeRole === "hr_admin"))
        );
      });

      return ok(tasks);
    },

    findPendingByWorkflow(
      workflowInstanceId: string,
    ): Result<ApprovalTaskRecord | undefined, AppError> {
      const task = [...store.approvalTasks.values()].find((approvalTask) => {
        return (
          approvalTask.workflowInstanceId === workflowInstanceId &&
          approvalTask.status === "pending"
        );
      });

      return ok(task);
    },
  };
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

function createIntegrationOutboxRepository(store: HcmNextStore) {
  return {
    create(record: IntegrationOutboxRecord): Result<IntegrationOutboxRecord, AppError> {
      store.integrationOutbox.set(record.outboxId, record);
      return ok(record);
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
