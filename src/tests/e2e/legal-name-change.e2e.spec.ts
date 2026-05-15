import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  ok,
  WORKFLOW_INTENTS,
  WORKFLOW_TRANSITIONS,
  type Result,
  type AppError,
} from "@hcm-next/foundation";
import {
  createRepositories,
  createSeededDemoStore,
  DEMO_IDS,
  type ActorRecord,
  type Repositories,
} from "@hcm-next/data-store";
import type {
  ExecutorClient,
  ExecutorRequest,
  ExecutorResponse,
} from "../../api/executor-client.js";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import {
  createWorkflowApiClient,
  type WorkflowApiClient,
} from "../support/workflow-api-client.js";

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  workflowApi: WorkflowApiClient;
  employeeContext: ApiRequestContext;
  hrContext: ApiRequestContext;
};

describe("employee.legal_name.change E2E contract", () => {
  let harness: TestHarness;

  beforeEach(async () => {
    harness = await createHarness();
  });

  afterEach(async () => {
    await harness.workflowApi.close();
  });

  it("denies an employee starting another worker's legal-name request", async () => {
    const startedWorkflow = await harness.workflowApi.startWorkflowIntent(
      harness.employeeContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
        subjectType: "worker",
        subjectId: DEMO_IDS.secondEmployeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(false);
    expect(!startedWorkflow.ok && startedWorkflow.error.code).toBe("PERMISSION_DENIED");
  });

  it("runs the legal-name workflow through approval, execution, projection, outbox, and ledger", async () => {
    const startedWorkflow = await harness.workflowApi.startWorkflowIntent(
      harness.employeeContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
        subjectType: "worker",
        subjectId: DEMO_IDS.employeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }

    const workflowInstanceId = String(startedWorkflow.value["workflowInstanceId"]);

    const submittedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.employeeContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_submit",
        expectedVersion: 1,
        input: {
          newLegalName: {
            first: "Jane",
            middle: null,
            last: "Rivera",
          },
          effectiveAt: "2026-06-01",
          businessReason: "legal_name_change",
        },
      },
    );

    expect(submittedWorkflow.ok).toBe(true);
    expect(submittedWorkflow.ok && submittedWorkflow.value["state"]).toBe(
      "collecting_evidence",
    );

    const documentResult = await harness.workflowApi.createDocument(
      harness.employeeContext,
      {
        workflowInstanceId,
        filename: "court-order.pdf",
        contentType: "application/pdf",
      },
    );

    expect(documentResult.ok).toBe(true);
    if (!documentResult.ok) {
      return;
    }

    const document = documentResult.value["document"] as Record<string, unknown>;
    const documentId = String(document["documentId"]);

    const evidenceWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.employeeContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE,
        idempotencyKey: "idem_evidence",
        expectedVersion: 2,
        input: {
          documentId,
        },
      },
    );

    expect(evidenceWorkflow.ok).toBe(true);
    expect(evidenceWorkflow.ok && evidenceWorkflow.value["state"]).toBe(
      "waiting_approval",
    );

    const employeeApprovalAttempt = await harness.workflowApi.transitionWorkflow(
      harness.employeeContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_employee_approve_denied",
        expectedVersion: 3,
        input: {
          approvalTaskId: "not_visible",
        },
      },
    );

    expect(employeeApprovalAttempt.ok).toBe(false);
    expect(!employeeApprovalAttempt.ok && employeeApprovalAttempt.error.code).toBe(
      "PERMISSION_DENIED",
    );

    const taskList = await harness.workflowApi.getTasks(harness.hrContext);

    expect(taskList.ok).toBe(true);
    if (!taskList.ok) {
      return;
    }

    const tasks = taskList.value["tasks"] as Array<Record<string, unknown>>;
    expect(tasks).toHaveLength(1);

    const approvalTaskId = String(tasks[0]?.["approvalTaskId"]);

    const approvedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_approve",
        expectedVersion: 3,
        input: {
          approvalTaskId,
          comment: "Evidence matches the requested legal name change.",
        },
      },
    );

    expect(approvedWorkflow.ok).toBe(true);
    expect(approvedWorkflow.ok && approvedWorkflow.value["state"]).toBe("approved");

    const staleExecute = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_execute_stale",
        expectedVersion: 3,
        input: {},
      },
    );

    expect(staleExecute.ok).toBe(false);
    expect(!staleExecute.ok && staleExecute.error.code).toBe("VERSION_CONFLICT");

    const executedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_execute",
        expectedVersion: 4,
        input: {},
      },
    );

    expect(executedWorkflow.ok).toBe(true);
    expect(executedWorkflow.ok && executedWorkflow.value["state"]).toBe("executed");

    const replayedExecution = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_execute",
        expectedVersion: 4,
        input: {},
      },
    );

    expect(replayedExecution.ok).toBe(true);
    expect(replayedExecution.ok && replayedExecution.value["idempotentReplay"]).toBe(
      true,
    );

    const projection = harness.repositories.store.employeeProjections.get(
      `${DEMO_IDS.tenantId}:${DEMO_IDS.employeeId}`,
    );
    expect(projection?.document.person.legalName.last).toBe("Rivera");
    expect(harness.repositories.store.integrationOutbox.size).toBe(1);

    const transactionPlan = [
      ...harness.repositories.store.transactionPlans.values(),
    ][0];
    expect(transactionPlan?.projectionPatches).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          projection: "employee",
          operation: "replace",
          path: "/person/legalName",
        }),
        expect.objectContaining({
          projection: "employee",
          operation: "replace",
          path: "/person/displayName",
          value: "Jane Rivera",
        }),
      ]),
    );

    const timeline = await harness.workflowApi.getTimeline(
      harness.hrContext,
      workflowInstanceId,
    );

    expect(timeline.ok).toBe(true);
    expect(timeline.ok && timeline.value["view"]).toBe("business");
    expect(timeline.ok && timeline.value["totalLedgerEventCount"]).toBeGreaterThan(18);
    expect(timeline.ok && (timeline.value["events"] as unknown[]).length).toBe(10);
    if (timeline.ok) {
      const eventTypes = (
        timeline.value["events"] as Array<Record<string, unknown>>
      ).map((event) => event["eventType"]);

      expect(eventTypes).toEqual(
        expect.arrayContaining([
          "WorkflowIntentStarted",
          "ChangeRequestCreated",
          "NameChangePreflighted",
          "EvidenceProvided",
          "ApprovalTaskCreated",
          "ApprovalGranted",
          "TransactionPlanCreated",
          "PersonLegalNameChanged",
          "ExternalWriteRequested",
          "WorkflowCompleted",
        ]),
      );
      expect(eventTypes).not.toContain("WorkflowTransitionSubmitted");
      expect(eventTypes).not.toContain("WorkflowStateChanged");
    }

    const auditTimeline = await harness.workflowApi.getTimeline(
      harness.hrContext,
      workflowInstanceId,
      "audit",
    );
    expect(auditTimeline.ok).toBe(true);
    expect(
      auditTimeline.ok && (auditTimeline.value["events"] as unknown[]).length,
    ).toBeGreaterThan(18);

    const debugTimeline = await harness.workflowApi.getTimeline(
      harness.hrContext,
      workflowInstanceId,
      "debug",
    );
    expect(debugTimeline.ok).toBe(true);
    if (debugTimeline.ok) {
      const debugEventTypes = (
        debugTimeline.value["events"] as Array<Record<string, unknown>>
      ).map((event) => event["eventType"]);
      expect(debugEventTypes).toEqual(
        expect.arrayContaining(["WorkflowTransitionSubmitted", "WorkflowStateChanged"]),
      );
      expect(debugEventTypes).not.toContain("ApprovalGranted");
    }
  });

  it("does not create HR approval before evidence is provided", async () => {
    const startedWorkflow = await harness.workflowApi.startWorkflowIntent(
      harness.employeeContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
        subjectType: "worker",
        subjectId: DEMO_IDS.employeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }

    const workflowInstanceId = String(startedWorkflow.value["workflowInstanceId"]);
    const submittedWorkflow = await submitValidLegalNameInput(
      harness,
      workflowInstanceId,
      "idem_missing_evidence_submit",
    );
    const taskList = await harness.workflowApi.getTasks(harness.hrContext);
    const approvalAttempt = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_missing_evidence_approve",
        expectedVersion: 2,
        input: {
          approvalTaskId: "missing_evidence_task",
        },
      },
    );
    const workflowInstance =
      harness.repositories.store.workflowInstances.get(workflowInstanceId);

    expect(submittedWorkflow.ok).toBe(true);
    expect(submittedWorkflow.ok && submittedWorkflow.value["state"]).toBe(
      "collecting_evidence",
    );
    expect(taskList.ok).toBe(true);
    expect(taskList.ok && (taskList.value["tasks"] as unknown[])).toHaveLength(0);
    expect(approvalAttempt.ok).toBe(false);
    expect(workflowInstance?.state).toBe("collecting_evidence");
  });

  it("does not execute a rejected legal-name workflow", async () => {
    const startedWorkflow = await harness.workflowApi.startWorkflowIntent(
      harness.employeeContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
        subjectType: "worker",
        subjectId: DEMO_IDS.employeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }

    const workflowInstanceId = String(startedWorkflow.value["workflowInstanceId"]);
    const submittedWorkflow = await submitValidLegalNameInput(
      harness,
      workflowInstanceId,
      "idem_reject_submit",
    );
    const documentId = await createEvidenceDocumentId(harness, workflowInstanceId);
    const evidenceWorkflow = await provideLegalNameEvidence(
      harness,
      workflowInstanceId,
      documentId,
      "idem_reject_evidence",
    );
    const taskList = await harness.workflowApi.getTasks(harness.hrContext);

    expect(submittedWorkflow.ok).toBe(true);
    expect(evidenceWorkflow.ok).toBe(true);
    expect(taskList.ok).toBe(true);
    if (!taskList.ok) {
      return;
    }

    const tasks = taskList.value["tasks"] as Array<Record<string, unknown>>;
    const approvalTaskId = String(tasks[0]?.["approvalTaskId"]);
    const rejectedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.REJECT,
        idempotencyKey: "idem_reject",
        expectedVersion: 3,
        input: {
          approvalTaskId,
          reason: "evidence_mismatch",
          comment: "Evidence does not match the requested legal name.",
        },
      },
    );
    const executeRejectedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_execute_rejected",
        expectedVersion: 4,
        input: {},
      },
    );

    expect(rejectedWorkflow.ok).toBe(true);
    expect(rejectedWorkflow.ok && rejectedWorkflow.value["state"]).toBe("rejected");
    expect(executeRejectedWorkflow.ok).toBe(false);
  });

  it("rejects unchanged legal-name input without creating a change request", async () => {
    const startedWorkflow = await harness.workflowApi.startWorkflowIntent(
      harness.employeeContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
        subjectType: "worker",
        subjectId: DEMO_IDS.employeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }

    const workflowInstanceId = String(startedWorkflow.value["workflowInstanceId"]);
    const submitResult = await harness.workflowApi.transitionWorkflow(
      harness.employeeContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_unchanged",
        expectedVersion: 1,
        input: {
          newLegalName: {
            first: "Jane",
            middle: null,
            last: "Doe",
          },
          effectiveAt: "2026-06-01",
          businessReason: "legal_name_change",
        },
      },
    );

    expect(submitResult.ok).toBe(false);
    expect(harness.repositories.store.changeRequests.size).toBe(0);
  });
});

async function submitValidLegalNameInput(
  harness: TestHarness,
  workflowInstanceId: string,
  idempotencyKey: string,
) {
  return harness.workflowApi.transitionWorkflow(
    harness.employeeContext,
    workflowInstanceId,
    {
      transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
      idempotencyKey,
      expectedVersion: 1,
      input: {
        newLegalName: {
          first: "Jane",
          middle: null,
          last: "Rivera",
        },
        effectiveAt: "2026-06-01",
        businessReason: "legal_name_change",
      },
    },
  );
}

async function createEvidenceDocumentId(
  harness: TestHarness,
  workflowInstanceId: string,
): Promise<string> {
  const documentResult = await harness.workflowApi.createDocument(
    harness.employeeContext,
    {
      workflowInstanceId,
      filename: "court-order.pdf",
      contentType: "application/pdf",
    },
  );

  if (!documentResult.ok) {
    throw new Error(documentResult.error.message);
  }

  const document = documentResult.value["document"] as Record<string, unknown>;
  return String(document["documentId"]);
}

async function provideLegalNameEvidence(
  harness: TestHarness,
  workflowInstanceId: string,
  documentId: string,
  idempotencyKey: string,
) {
  return harness.workflowApi.transitionWorkflow(
    harness.employeeContext,
    workflowInstanceId,
    {
      transition: WORKFLOW_TRANSITIONS.PROVIDE_EVIDENCE,
      idempotencyKey,
      expectedVersion: 2,
      input: {
        documentId,
      },
    },
  );
}

async function createHarness(): Promise<TestHarness> {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const dependencies: AppDependencies = {
    repositories,
    executorClient: createFakeExecutorClient(),
  };

  return {
    dependencies,
    repositories,
    workflowApi: await createWorkflowApiClient(dependencies),
    employeeContext: createApiRequestContext(
      unwrapResult(repositories.actors.findById(DEMO_IDS.employeeActorId)),
    ),
    hrContext: createApiRequestContext(
      unwrapResult(repositories.actors.findById(DEMO_IDS.hrActorId)),
    ),
  };
}

function createApiRequestContext(actor: ActorRecord): ApiRequestContext {
  return {
    actor,
    tenantId: actor.tenantId,
    environmentId: DEMO_IDS.environmentId,
    requestId: `req_${actor.actorId}`,
    correlationId: `corr_${actor.actorId}`,
  };
}

function unwrapResult<TValue>(result: Result<TValue, AppError>): TValue {
  if (!result.ok) {
    throw new Error(result.error.message);
  }

  return result.value;
}

function createFakeExecutorClient(): ExecutorClient {
  return {
    async executeBlock<TOutput>(
      request: ExecutorRequest,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      if (request.block.name.endsWith(".preflight")) {
        return ok(createPreflightResponse(request) as ExecutorResponse<TOutput>);
      }

      return ok(createPlanResponse(request) as ExecutorResponse<TOutput>);
    },
  };
}

function createPreflightResponse(request: ExecutorRequest): ExecutorResponse {
  const input = request.input;
  const currentName = input["currentLegalName"] as Record<string, unknown>;
  const proposedName = input["proposedLegalName"] as Record<string, unknown>;
  const unchanged =
    currentName["first"] === proposedName["first"] &&
    currentName["middle"] === proposedName["middle"] &&
    currentName["last"] === proposedName["last"];

  return {
    status: "succeeded",
    output: {
      valid: !unchanged,
      riskLevel: unchanged ? "high" : "low",
      requiresEvidence: true,
      requiresApproval: true,
      warnings: [],
      errors: unchanged
        ? [
            {
              code: "legal_name.unchanged",
              field: "proposedLegalName",
              message: "New legal name must differ from the current legal name.",
            },
          ]
        : [],
    },
    proposedEvents: [],
    externalCallRequests: [],
    logs: [],
    metrics: {
      durationMs: 1,
    },
  };
}

function createPlanResponse(request: ExecutorRequest): ExecutorResponse {
  const input = request.input;

  return {
    status: "succeeded",
    output: {
      internalWrites: [
        {
          eventType: "PersonLegalNameChanged",
          subjectType: "worker",
          subjectId: input["workerId"],
          effectiveAt: input["effectiveAt"],
          payload: {
            personId: input["personId"],
            previousLegalName: input["currentLegalName"],
            newLegalName: input["proposedLegalName"],
          },
        },
      ],
      projectionPatches: [
        {
          projection: "employee",
          operation: "replace",
          path: "/person/legalName",
          value: input["proposedLegalName"],
        },
        {
          projection: "employee",
          operation: "replace",
          path: "/person/displayName",
          value: displayNameFromLegalName(
            input["proposedLegalName"] as Record<string, unknown>,
          ),
        },
      ],
      externalCallRequests: [
        {
          connectionId: "fake_hris",
          operation: "updateLegalName",
          idempotencyKey: `fake_hris_legal_name_${String(input["changeRequestId"])}`,
          payload: {
            workerId: input["workerId"],
            legalName: input["proposedLegalName"],
            effectiveAt: input["effectiveAt"],
          },
          reconciliation: {},
        },
      ],
    },
    proposedEvents: [],
    externalCallRequests: [],
    logs: [],
    metrics: {
      durationMs: 1,
    },
  };
}

function displayNameFromLegalName(legalName: Record<string, unknown>): string {
  return [legalName["first"], legalName["middle"], legalName["last"]]
    .filter((value): value is string => {
      return typeof value === "string" && value.trim().length > 0;
    })
    .join(" ");
}
