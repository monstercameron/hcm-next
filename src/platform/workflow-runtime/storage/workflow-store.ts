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
import type { Result } from "../result";

export type TransitionAttemptCreateInput = Omit<
  WorkflowTransitionAttempt,
  "workflowTransitionAttemptId" | "createdAt" | "completedAt"
>;

export type ChangeRequestCreateInput = Omit<
  ChangeRequest,
  "changeRequestId" | "createdAt" | "updatedAt"
>;

export type ProposedChangeCreateInput = Omit<
  ProposedChange,
  "proposedChangeId" | "createdAt" | "updatedAt"
>;

export type DocumentCreateInput = Omit<
  DocumentRecord,
  "documentId" | "createdAt" | "updatedAt"
>;

export type ApprovalTaskCreateInput = Omit<
  ApprovalTask,
  "approvalTaskId" | "createdAt" | "decidedAt"
>;

export type TransactionPlanCreateInput = Omit<
  TransactionPlan,
  "transactionPlanId" | "createdAt" | "updatedAt"
>;

export type IntegrationOutboxCreateInput = Omit<
  IntegrationOutboxRecord,
  "outboxId" | "createdAt" | "updatedAt"
>;

export type WorkflowRuntimeStore = WorkflowRuntimeQueries &
  WorkflowRuntimeCommands & {
    /** Runs workflow writes in one transactional boundary. */
    runInTransaction<T>(
      operation: (store: WorkflowRuntimeStore) => Promise<Result<T>>,
    ): Promise<Result<T>>;
  };

export type WorkflowRuntimeQueries = {
  /** Returns the default tenant and environment for local V0 demo requests. */
  getDefaultTenantEnvironment(): Promise<Result<DemoTenantEnvironment>>;

  /** Finds an actor by tenant-scoped actor ID. */
  findActorById(tenantId: string, actorId: string): Promise<Result<Actor>>;

  /** Loads the active workflow definition version for a public intent. */
  getActiveWorkflowVersionByIntent(
    tenantId: string,
    intent: string,
  ): Promise<Result<WorkflowDefinitionVersion>>;

  /** Loads a workflow instance by tenant and ID. */
  findWorkflowInstanceById(
    tenantId: string,
    workflowInstanceId: string,
  ): Promise<Result<WorkflowInstance>>;

  /** Loads an employee projection used by workflow permission and preflight checks. */
  findEmployeeProjectionById(
    tenantId: string,
    employeeId: string,
  ): Promise<Result<EmployeeProjection>>;

  /** Loads a change request by ID. */
  findChangeRequestById(
    tenantId: string,
    changeRequestId: string,
  ): Promise<Result<ChangeRequest>>;

  /** Loads proposed changes for a change request. */
  listProposedChangesByChangeRequestId(
    tenantId: string,
    changeRequestId: string,
  ): Promise<Result<ProposedChange[]>>;

  /** Finds a prior transition attempt for idempotency handling. */
  findTransitionAttemptByIdempotencyKey(
    tenantId: string,
    workflowInstanceId: string,
    idempotencyKey: string,
  ): Promise<Result<WorkflowTransitionAttempt | undefined>>;

  /** Loads document metadata by ID. */
  findDocumentById(
    tenantId: string,
    documentId: string,
  ): Promise<Result<DocumentRecord>>;

  /** Returns whether a document has already been attached to a workflow. */
  findWorkflowInstanceDocument(
    tenantId: string,
    workflowInstanceId: string,
    documentId: string,
  ): Promise<Result<WorkflowInstanceDocument | undefined>>;

  /** Finds an approval task by ID. */
  findApprovalTaskById(
    tenantId: string,
    approvalTaskId: string,
  ): Promise<Result<ApprovalTask>>;

  /** Finds the pending approval task for a workflow instance. */
  findPendingApprovalTaskByWorkflowInstanceId(
    tenantId: string,
    workflowInstanceId: string,
  ): Promise<Result<ApprovalTask | undefined>>;

  /** Lists approval tasks visible to an actor context. */
  listApprovalTasks(
    context: RequestContext,
    status?: string,
  ): Promise<Result<ApprovalTask[]>>;

  /** Finds the latest transaction plan for a change request. */
  findTransactionPlanByChangeRequestId(
    tenantId: string,
    changeRequestId: string,
  ): Promise<Result<TransactionPlan>>;

  /** Lists ledger events for a workflow timeline. */
  listLedgerEventsForWorkflow(
    tenantId: string,
    workflowInstanceId: string,
    changeRequestId?: string,
  ): Promise<Result<LedgerEvent[]>>;
};

export type WorkflowRuntimeCommands = {
  /** Creates a durable workflow instance. */
  createWorkflowInstance(
    workflowInstance: WorkflowInstance,
  ): Promise<Result<WorkflowInstance>>;

  /** Persists workflow state, interaction, status, and version changes. */
  updateWorkflowInstance(
    workflowInstance: WorkflowInstance,
  ): Promise<Result<WorkflowInstance>>;

  /** Creates a transition attempt before handler execution. */
  createTransitionAttempt(
    input: TransitionAttemptCreateInput,
  ): Promise<Result<WorkflowTransitionAttempt>>;

  /** Persists a completed transition attempt response for idempotent replay. */
  completeTransitionAttempt(
    attempt: WorkflowTransitionAttempt,
    responsePayload: JsonObject,
  ): Promise<Result<WorkflowTransitionAttempt>>;

  /** Persists a failed transition attempt for audit and idempotency conflicts. */
  failTransitionAttempt(
    attempt: WorkflowTransitionAttempt,
    error: JsonObject,
  ): Promise<Result<WorkflowTransitionAttempt>>;

  /** Appends immutable ledger events. */
  appendLedgerEvents(events: LedgerEventInput[]): Promise<Result<LedgerEvent[]>>;

  /** Creates a change request from a workflow transition. */
  createChangeRequest(input: ChangeRequestCreateInput): Promise<Result<ChangeRequest>>;

  /** Updates change request status and lifecycle timestamps. */
  updateChangeRequest(changeRequest: ChangeRequest): Promise<Result<ChangeRequest>>;

  /** Creates proposed field changes. */
  createProposedChanges(
    changes: ProposedChangeCreateInput[],
  ): Promise<Result<ProposedChange[]>>;

  /** Creates V0 document metadata. */
  createDocument(input: DocumentCreateInput): Promise<Result<DocumentRecord>>;

  /** Attaches document evidence to a workflow instance. */
  attachDocumentToWorkflowInstance(
    link: WorkflowInstanceDocument,
  ): Promise<Result<WorkflowInstanceDocument>>;

  /** Creates an approval task for HR review. */
  createApprovalTask(input: ApprovalTaskCreateInput): Promise<Result<ApprovalTask>>;

  /** Updates an approval task decision and status. */
  updateApprovalTask(task: ApprovalTask): Promise<Result<ApprovalTask>>;

  /** Creates a transaction plan after approval. */
  createTransactionPlan(
    input: TransactionPlanCreateInput,
  ): Promise<Result<TransactionPlan>>;

  /** Updates a transaction plan during execution. */
  updateTransactionPlan(plan: TransactionPlan): Promise<Result<TransactionPlan>>;

  /** Applies the approved legal-name projection update. */
  updateEmployeeProjection(
    projection: EmployeeProjection,
  ): Promise<Result<EmployeeProjection>>;

  /** Creates an integration outbox request for later worker execution. */
  createIntegrationOutbox(
    input: IntegrationOutboxCreateInput,
  ): Promise<Result<IntegrationOutboxRecord>>;
};
