import { beforeEach, describe, expect, it } from "vitest";
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
  createDocument,
  getTasks,
  getTimeline,
  startWorkflowIntent,
  transitionWorkflow,
} from "../../workflows/legal-name-change/service.js";

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  employeeContext: ApiRequestContext;
  hrContext: ApiRequestContext;
};

describe("employee.legal_name.change E2E contract", () => {
  let harness: TestHarness;

  beforeEach(() => {
    harness = createHarness();
  });

  it("runs the legal-name workflow through approval, execution, projection, outbox, and ledger", async () => {
    const startedWorkflow = startWorkflowIntent(
      harness.dependencies,
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

    const submittedWorkflow = await transitionWorkflow(
      harness.dependencies,
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

    const documentResult = createDocument(
      harness.dependencies,
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

    const evidenceWorkflow = await transitionWorkflow(
      harness.dependencies,
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

    const employeeApprovalAttempt = await transitionWorkflow(
      harness.dependencies,
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

    const taskList = getTasks(harness.dependencies, harness.hrContext);

    expect(taskList.ok).toBe(true);
    if (!taskList.ok) {
      return;
    }

    const tasks = taskList.value["tasks"] as Array<Record<string, unknown>>;
    expect(tasks).toHaveLength(1);

    const approvalTaskId = String(tasks[0]?.["approvalTaskId"]);

    const approvedWorkflow = await transitionWorkflow(
      harness.dependencies,
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

    const staleExecute = await transitionWorkflow(
      harness.dependencies,
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

    const executedWorkflow = await transitionWorkflow(
      harness.dependencies,
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

    const replayedExecution = await transitionWorkflow(
      harness.dependencies,
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

    const timeline = getTimeline(
      harness.dependencies,
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

    const auditTimeline = getTimeline(
      harness.dependencies,
      harness.hrContext,
      workflowInstanceId,
      "audit",
    );
    expect(auditTimeline.ok).toBe(true);
    expect(
      auditTimeline.ok && (auditTimeline.value["events"] as unknown[]).length,
    ).toBeGreaterThan(18);

    const debugTimeline = getTimeline(
      harness.dependencies,
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

  it("rejects unchanged legal-name input without creating a change request", async () => {
    const startedWorkflow = startWorkflowIntent(
      harness.dependencies,
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
    const submitResult = await transitionWorkflow(
      harness.dependencies,
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

function createHarness(): TestHarness {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const dependencies: AppDependencies = {
    repositories,
    executorClient: createFakeExecutorClient(),
  };

  return {
    dependencies,
    repositories,
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
