import {
  ACTOR_ROLES,
  ACTOR_TYPES,
  APPROVAL_TASK_STATUSES,
  DOCUMENT_STATUSES,
  INTEGRATION_OUTBOX_STATUSES,
  TRANSITION_ATTEMPT_STATUSES,
  WORKFLOW_INTENTS,
} from "../constants";
import type { RequestContext } from "../context";
import type {
  Actor,
  ApprovalTask,
  ChangeRequest,
  DemoTenantEnvironment,
  DocumentRecord,
  EmployeeProjection,
  IntegrationOutboxRecord,
  JsonObject,
  LedgerEvent,
  LedgerEventInput,
  ProposedChange,
  TransactionPlan,
  WorkflowDefinitionVersion,
  WorkflowInstance,
  WorkflowInstanceDocument,
  WorkflowTransitionAttempt,
} from "../domain";
import { notFoundError, ok, workflowNotFoundError, type Result } from "../result";
import { createReadableId } from "../runtime/ids";
import { LEGAL_NAME_WORKFLOW_DEFINITION } from "../runtime/legal-name-workflow";
import { nowIso } from "../runtime/time";
import type {
  ApprovalTaskCreateInput,
  ChangeRequestCreateInput,
  DocumentCreateInput,
  IntegrationOutboxCreateInput,
  ProposedChangeCreateInput,
  TransitionAttemptCreateInput,
  TransactionPlanCreateInput,
  WorkflowRuntimeStore,
} from "./workflow-store";

type StoreState = {
  tenantEnvironment: DemoTenantEnvironment;
  actors: Map<string, Actor>;
  workflowDefinitions: Map<string, WorkflowDefinitionVersion>;
  workflowInstances: Map<string, WorkflowInstance>;
  transitionAttempts: Map<string, WorkflowTransitionAttempt>;
  employeeProjections: Map<string, EmployeeProjection>;
  changeRequests: Map<string, ChangeRequest>;
  proposedChanges: Map<string, ProposedChange>;
  documents: Map<string, DocumentRecord>;
  workflowInstanceDocuments: Map<string, WorkflowInstanceDocument>;
  approvalTasks: Map<string, ApprovalTask>;
  transactionPlans: Map<string, TransactionPlan>;
  ledgerEvents: LedgerEvent[];
  integrationOutbox: Map<string, IntegrationOutboxRecord>;
  nextLedgerSequence: number;
};

/** Creates a V0 demo store for local API use until the Postgres adapter is wired. */
export function createInMemoryWorkflowStore(): WorkflowRuntimeStore {
  return new InMemoryWorkflowStore(createSeedState());
}

class InMemoryWorkflowStore implements WorkflowRuntimeStore {
  constructor(private readonly state: StoreState) {}

  async runInTransaction<T>(
    operation: (store: WorkflowRuntimeStore) => Promise<Result<T>>,
  ): Promise<Result<T>> {
    return operation(this);
  }

  async getDefaultTenantEnvironment(): Promise<Result<DemoTenantEnvironment>> {
    return ok(clone(this.state.tenantEnvironment));
  }

  async findActorById(tenantId: string, actorId: string): Promise<Result<Actor>> {
    const actor = this.state.actors.get(actorId);
    if (!actor || actor.tenantId !== tenantId) {
      return notFoundResult("Actor", { actorId });
    }

    return ok(clone(actor));
  }

  async getActiveWorkflowVersionByIntent(
    tenantId: string,
    intent: string,
  ): Promise<Result<WorkflowDefinitionVersion>> {
    const workflowDefinition = this.state.workflowDefinitions.get(intent);
    if (!workflowDefinition || tenantId !== this.state.tenantEnvironment.tenantId) {
      return notFoundResult("Workflow definition", { intent });
    }

    return ok(clone(workflowDefinition));
  }

  async findWorkflowInstanceById(
    tenantId: string,
    workflowInstanceId: string,
  ): Promise<Result<WorkflowInstance>> {
    const workflowInstance = this.state.workflowInstances.get(workflowInstanceId);
    if (!workflowInstance || workflowInstance.tenantId !== tenantId) {
      return { ok: false, error: workflowNotFoundError({ workflowInstanceId }) };
    }

    return ok(clone(workflowInstance));
  }

  async findEmployeeProjectionById(
    tenantId: string,
    employeeId: string,
  ): Promise<Result<EmployeeProjection>> {
    const projection = this.state.employeeProjections.get(employeeId);
    if (!projection || projection.tenantId !== tenantId) {
      return notFoundResult("Employee projection", { employeeId });
    }

    return ok(clone(projection));
  }

  async findChangeRequestById(
    tenantId: string,
    changeRequestId: string,
  ): Promise<Result<ChangeRequest>> {
    const changeRequest = this.state.changeRequests.get(changeRequestId);
    if (!changeRequest || changeRequest.tenantId !== tenantId) {
      return notFoundResult("Change request", { changeRequestId });
    }

    return ok(clone(changeRequest));
  }

  async listProposedChangesByChangeRequestId(
    tenantId: string,
    changeRequestId: string,
  ): Promise<Result<ProposedChange[]>> {
    return ok(
      [...this.state.proposedChanges.values()]
        .filter(
          (change) =>
            change.tenantId === tenantId && change.changeRequestId === changeRequestId,
        )
        .map(clone),
    );
  }

  async findTransitionAttemptByIdempotencyKey(
    tenantId: string,
    workflowInstanceId: string,
    idempotencyKey: string,
  ): Promise<Result<WorkflowTransitionAttempt | undefined>> {
    const key = transitionAttemptKey(tenantId, workflowInstanceId, idempotencyKey);
    const attempt = this.state.transitionAttempts.get(key);
    return ok(attempt ? clone(attempt) : undefined);
  }

  async findDocumentById(
    tenantId: string,
    documentId: string,
  ): Promise<Result<DocumentRecord>> {
    const document = this.state.documents.get(documentId);
    if (!document || document.tenantId !== tenantId) {
      return notFoundResult("Document", { documentId });
    }

    return ok(clone(document));
  }

  async findWorkflowInstanceDocument(
    tenantId: string,
    workflowInstanceId: string,
    documentId: string,
  ): Promise<Result<WorkflowInstanceDocument | undefined>> {
    const link = this.state.workflowInstanceDocuments.get(
      workflowDocumentKey(tenantId, workflowInstanceId, documentId),
    );
    return ok(link ? clone(link) : undefined);
  }

  async findApprovalTaskById(
    tenantId: string,
    approvalTaskId: string,
  ): Promise<Result<ApprovalTask>> {
    const task = this.state.approvalTasks.get(approvalTaskId);
    if (!task || task.tenantId !== tenantId) {
      return notFoundResult("Approval task", { approvalTaskId });
    }

    return ok(clone(task));
  }

  async findPendingApprovalTaskByWorkflowInstanceId(
    tenantId: string,
    workflowInstanceId: string,
  ): Promise<Result<ApprovalTask | undefined>> {
    const task = [...this.state.approvalTasks.values()].find(
      (approvalTask) =>
        approvalTask.tenantId === tenantId &&
        approvalTask.workflowInstanceId === workflowInstanceId &&
        approvalTask.status === APPROVAL_TASK_STATUSES.PENDING,
    );

    return ok(task ? clone(task) : undefined);
  }

  async listApprovalTasks(
    context: RequestContext,
    status?: string,
  ): Promise<Result<ApprovalTask[]>> {
    if (!context.roles.includes(ACTOR_ROLES.HR_ADMIN)) {
      return ok([]);
    }

    return ok(
      [...this.state.approvalTasks.values()]
        .filter((task) => task.tenantId === context.tenantId)
        .filter((task) => !status || task.status === status)
        .map(clone),
    );
  }

  async findTransactionPlanByChangeRequestId(
    tenantId: string,
    changeRequestId: string,
  ): Promise<Result<TransactionPlan>> {
    const plan = [...this.state.transactionPlans.values()].find(
      (transactionPlan) =>
        transactionPlan.tenantId === tenantId &&
        transactionPlan.changeRequestId === changeRequestId,
    );

    if (!plan) {
      return notFoundResult("Transaction plan", { changeRequestId });
    }

    return ok(clone(plan));
  }

  async listLedgerEventsForWorkflow(
    tenantId: string,
    workflowInstanceId: string,
    changeRequestId?: string,
  ): Promise<Result<LedgerEvent[]>> {
    return ok(
      this.state.ledgerEvents
        .filter((event) => event.tenantId === tenantId)
        .filter(
          (event) =>
            event.workflowInstanceId === workflowInstanceId ||
            (changeRequestId !== undefined &&
              event.changeRequestId === changeRequestId),
        )
        .sort((left, right) => left.sequence - right.sequence)
        .map(clone),
    );
  }

  async createWorkflowInstance(
    workflowInstance: WorkflowInstance,
  ): Promise<Result<WorkflowInstance>> {
    this.state.workflowInstances.set(
      workflowInstance.workflowInstanceId,
      clone(workflowInstance),
    );
    return ok(clone(workflowInstance));
  }

  async updateWorkflowInstance(
    workflowInstance: WorkflowInstance,
  ): Promise<Result<WorkflowInstance>> {
    this.state.workflowInstances.set(
      workflowInstance.workflowInstanceId,
      clone(workflowInstance),
    );
    return ok(clone(workflowInstance));
  }

  async createTransitionAttempt(
    input: TransitionAttemptCreateInput,
  ): Promise<Result<WorkflowTransitionAttempt>> {
    const now = nowIso();
    const attempt: WorkflowTransitionAttempt = {
      ...input,
      workflowTransitionAttemptId: createReadableId("wfta"),
      createdAt: now,
    };
    this.state.transitionAttempts.set(
      transitionAttemptKey(
        attempt.tenantId,
        attempt.workflowInstanceId,
        attempt.idempotencyKey,
      ),
      clone(attempt),
    );
    return ok(clone(attempt));
  }

  async completeTransitionAttempt(
    attempt: WorkflowTransitionAttempt,
    responsePayload: JsonObject,
  ): Promise<Result<WorkflowTransitionAttempt>> {
    const completedAttempt: WorkflowTransitionAttempt = {
      ...attempt,
      responsePayload: clone(responsePayload),
      status: TRANSITION_ATTEMPT_STATUSES.COMPLETED,
      completedAt: nowIso(),
    };
    this.state.transitionAttempts.set(
      transitionAttemptKey(
        completedAttempt.tenantId,
        completedAttempt.workflowInstanceId,
        completedAttempt.idempotencyKey,
      ),
      clone(completedAttempt),
    );
    return ok(clone(completedAttempt));
  }

  async failTransitionAttempt(
    attempt: WorkflowTransitionAttempt,
    error: JsonObject,
  ): Promise<Result<WorkflowTransitionAttempt>> {
    const failedAttempt: WorkflowTransitionAttempt = {
      ...attempt,
      error: clone(error),
      status: TRANSITION_ATTEMPT_STATUSES.FAILED,
      completedAt: nowIso(),
    };
    this.state.transitionAttempts.set(
      transitionAttemptKey(
        failedAttempt.tenantId,
        failedAttempt.workflowInstanceId,
        failedAttempt.idempotencyKey,
      ),
      clone(failedAttempt),
    );
    return ok(clone(failedAttempt));
  }

  async appendLedgerEvents(events: LedgerEventInput[]): Promise<Result<LedgerEvent[]>> {
    const persistedEvents = events.map((event) => ({
      ...event,
      eventId: createReadableId("evt"),
      sequence: this.state.nextLedgerSequence++,
    }));

    this.state.ledgerEvents.push(...persistedEvents.map(clone));
    return ok(persistedEvents.map(clone));
  }

  async createChangeRequest(
    input: ChangeRequestCreateInput,
  ): Promise<Result<ChangeRequest>> {
    const now = nowIso();
    const changeRequest: ChangeRequest = {
      ...input,
      changeRequestId: createReadableId("cr"),
      createdAt: now,
      updatedAt: now,
    };
    this.state.changeRequests.set(changeRequest.changeRequestId, clone(changeRequest));
    return ok(clone(changeRequest));
  }

  async updateChangeRequest(
    changeRequest: ChangeRequest,
  ): Promise<Result<ChangeRequest>> {
    const updatedChangeRequest = { ...changeRequest, updatedAt: nowIso() };
    this.state.changeRequests.set(
      updatedChangeRequest.changeRequestId,
      clone(updatedChangeRequest),
    );
    return ok(clone(updatedChangeRequest));
  }

  async createProposedChanges(
    changes: ProposedChangeCreateInput[],
  ): Promise<Result<ProposedChange[]>> {
    const now = nowIso();
    const persistedChanges = changes.map((change) => ({
      ...change,
      proposedChangeId: createReadableId("pc"),
      createdAt: now,
      updatedAt: now,
    }));

    for (const change of persistedChanges) {
      this.state.proposedChanges.set(change.proposedChangeId, clone(change));
    }

    return ok(persistedChanges.map(clone));
  }

  async createDocument(input: DocumentCreateInput): Promise<Result<DocumentRecord>> {
    const now = nowIso();
    const document: DocumentRecord = {
      ...input,
      documentId: createReadableId("doc"),
      status: input.status ?? DOCUMENT_STATUSES.UPLOADED,
      createdAt: now,
      updatedAt: now,
    };
    this.state.documents.set(document.documentId, clone(document));
    return ok(clone(document));
  }

  async attachDocumentToWorkflowInstance(
    link: WorkflowInstanceDocument,
  ): Promise<Result<WorkflowInstanceDocument>> {
    this.state.workflowInstanceDocuments.set(
      workflowDocumentKey(link.tenantId, link.workflowInstanceId, link.documentId),
      clone(link),
    );
    return ok(clone(link));
  }

  async createApprovalTask(
    input: ApprovalTaskCreateInput,
  ): Promise<Result<ApprovalTask>> {
    const task: ApprovalTask = {
      ...input,
      approvalTaskId: createReadableId("task"),
      createdAt: nowIso(),
    };
    this.state.approvalTasks.set(task.approvalTaskId, clone(task));
    return ok(clone(task));
  }

  async updateApprovalTask(task: ApprovalTask): Promise<Result<ApprovalTask>> {
    this.state.approvalTasks.set(task.approvalTaskId, clone(task));
    return ok(clone(task));
  }

  async createTransactionPlan(
    input: TransactionPlanCreateInput,
  ): Promise<Result<TransactionPlan>> {
    const now = nowIso();
    const plan: TransactionPlan = {
      ...input,
      transactionPlanId: createReadableId("tp"),
      createdAt: now,
      updatedAt: now,
    };
    this.state.transactionPlans.set(plan.transactionPlanId, clone(plan));
    return ok(clone(plan));
  }

  async updateTransactionPlan(plan: TransactionPlan): Promise<Result<TransactionPlan>> {
    const updatedPlan = { ...plan, updatedAt: nowIso() };
    this.state.transactionPlans.set(updatedPlan.transactionPlanId, clone(updatedPlan));
    return ok(clone(updatedPlan));
  }

  async updateEmployeeProjection(
    projection: EmployeeProjection,
  ): Promise<Result<EmployeeProjection>> {
    const updatedProjection = {
      ...projection,
      projectionVersion: projection.projectionVersion + 1,
      updatedAt: nowIso(),
    };
    this.state.employeeProjections.set(
      updatedProjection.employeeId,
      clone(updatedProjection),
    );
    return ok(clone(updatedProjection));
  }

  async createIntegrationOutbox(
    input: IntegrationOutboxCreateInput,
  ): Promise<Result<IntegrationOutboxRecord>> {
    const now = nowIso();
    const outbox: IntegrationOutboxRecord = {
      ...input,
      status: input.status ?? INTEGRATION_OUTBOX_STATUSES.PENDING,
      outboxId: createReadableId("outbox"),
      createdAt: now,
      updatedAt: now,
    };
    this.state.integrationOutbox.set(outbox.outboxId, clone(outbox));
    return ok(clone(outbox));
  }
}

function createSeedState(): StoreState {
  const tenantEnvironment = {
    tenantId: "tenant_demo",
    environmentId: "env_demo",
  };
  const actors = new Map<string, Actor>([
    [
      "actor_employee_jane",
      {
        actorId: "actor_employee_jane",
        tenantId: tenantEnvironment.tenantId,
        actorType: ACTOR_TYPES.HUMAN,
        linkedWorkerId: "emp_123",
        displayName: "Jane Doe",
        roles: [ACTOR_ROLES.EMPLOYEE],
      },
    ],
    [
      "actor_hr_admin",
      {
        actorId: "actor_hr_admin",
        tenantId: tenantEnvironment.tenantId,
        actorType: ACTOR_TYPES.HUMAN,
        displayName: "HR Admin",
        roles: [ACTOR_ROLES.HR_ADMIN],
      },
    ],
    [
      "actor_system",
      {
        actorId: "actor_system",
        tenantId: tenantEnvironment.tenantId,
        actorType: ACTOR_TYPES.SYSTEM,
        displayName: "System",
        roles: [ACTOR_ROLES.SYSTEM],
      },
    ],
  ]);

  return {
    tenantEnvironment,
    actors,
    workflowDefinitions: new Map([
      [WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE, LEGAL_NAME_WORKFLOW_DEFINITION],
    ]),
    workflowInstances: new Map(),
    transitionAttempts: new Map(),
    employeeProjections: new Map([
      [
        "emp_123",
        {
          tenantId: tenantEnvironment.tenantId,
          employeeId: "emp_123",
          projectionVersion: 1,
          updatedAt: nowIso(),
          document: {
            employeeId: "emp_123",
            person: {
              personId: "person_123",
              legalName: {
                first: "Jane",
                middle: null,
                last: "Doe",
              },
              displayName: "Jane Doe",
              preferredName: null,
              workEmail: "jane.doe@example.com",
            },
            employment: {
              status: "active",
              legalEntity: "US-001",
            },
            manager: {
              employeeId: "emp_456",
            },
            custom: {},
          },
        },
      ],
    ]),
    changeRequests: new Map(),
    proposedChanges: new Map(),
    documents: new Map(),
    workflowInstanceDocuments: new Map(),
    approvalTasks: new Map(),
    transactionPlans: new Map(),
    ledgerEvents: [],
    integrationOutbox: new Map(),
    nextLedgerSequence: 1,
  };
}

function transitionAttemptKey(
  tenantId: string,
  workflowInstanceId: string,
  idempotencyKey: string,
): string {
  return `${tenantId}:${workflowInstanceId}:${idempotencyKey}`;
}

function workflowDocumentKey(
  tenantId: string,
  workflowInstanceId: string,
  documentId: string,
): string {
  return `${tenantId}:${workflowInstanceId}:${documentId}`;
}

function clone<T>(value: T): T {
  if (typeof value === "object" && value !== null) {
    return JSON.parse(JSON.stringify(value)) as T;
  }

  return value;
}

function notFoundResult<T>(
  resource: string,
  details: Record<string, unknown>,
): Result<T> {
  return { ok: false, error: notFoundError(resource, details) };
}
