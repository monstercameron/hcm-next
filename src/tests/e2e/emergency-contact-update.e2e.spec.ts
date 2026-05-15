import { beforeEach, describe, expect, it } from "vitest";
import {
  ok,
  WORKFLOW_INTENTS,
  WORKFLOW_TRANSITIONS,
  type AppError,
  type Result,
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

describe("employee.emergency_contact.update E2E contract", () => {
  let harness: TestHarness;

  beforeEach(() => {
    harness = createHarness();
  });

  it("runs the emergency-contact workflow through approval, execution, projection, outbox, and ledger", async () => {
    const startedWorkflow = startWorkflowIntent(
      harness.dependencies,
      harness.employeeContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_EMERGENCY_CONTACT_UPDATE,
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
        idempotencyKey: "idem_emergency_contact_submit",
        expectedVersion: 1,
        input: {
          proposedEmergencyContact: {
            contactId: "ec_001",
            name: "Alex Doe",
            relationship: "spouse",
            phone: "+15557654321",
            email: "alex.doe@example.com",
            priority: 1,
          },
          effectiveAt: "2026-06-01",
          businessReason: "employee_self_service",
        },
      },
    );

    expect(submittedWorkflow.ok).toBe(true);
    expect(submittedWorkflow.ok && submittedWorkflow.value["state"]).toBe(
      "waiting_approval",
    );

    const employeeApprovalAttempt = await transitionWorkflow(
      harness.dependencies,
      harness.employeeContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_emergency_contact_employee_approve_denied",
        expectedVersion: 2,
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
        idempotencyKey: "idem_emergency_contact_approve",
        expectedVersion: 2,
        input: {
          approvalTaskId,
          comment: "Emergency contact update is complete and low risk.",
        },
      },
    );

    expect(approvedWorkflow.ok).toBe(true);
    expect(approvedWorkflow.ok && approvedWorkflow.value["state"]).toBe("approved");

    const executedWorkflow = await transitionWorkflow(
      harness.dependencies,
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_emergency_contact_execute",
        expectedVersion: 3,
        input: {},
      },
    );

    expect(executedWorkflow.ok).toBe(true);
    expect(executedWorkflow.ok && executedWorkflow.value["state"]).toBe("executed");

    const projection = harness.repositories.store.employeeProjections.get(
      `${DEMO_IDS.tenantId}:${DEMO_IDS.employeeId}`,
    );
    expect(projection?.document.emergencyContacts[0]?.phone).toBe("+15557654321");
    expect(harness.repositories.store.integrationOutbox.size).toBe(1);

    const transactionPlan = [
      ...harness.repositories.store.transactionPlans.values(),
    ][0];
    expect(transactionPlan?.projectionPatches).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          projection: "employee",
          operation: "replace",
          path: "/emergencyContacts",
        }),
      ]),
    );

    const timeline = getTimeline(
      harness.dependencies,
      harness.hrContext,
      workflowInstanceId,
    );

    expect(timeline.ok).toBe(true);
    expect(timeline.ok && timeline.value["totalLedgerEventCount"]).toBeGreaterThan(20);
    if (timeline.ok) {
      const eventTypes = (
        timeline.value["events"] as Array<Record<string, unknown>>
      ).map((event) => event["eventType"]);

      expect(eventTypes).toEqual(
        expect.arrayContaining([
          "WorkflowIntentStarted",
          "ChangeRequestCreated",
          "EmergencyContactPreflighted",
          "ApprovalTaskCreated",
          "ApprovalGranted",
          "TransactionPlanCreated",
          "EmployeeEmergencyContactUpdated",
          "ExternalWriteRequested",
          "WorkflowCompleted",
        ]),
      );
    }
  });

  it("rejects unchanged emergency-contact input without creating a change request", async () => {
    const startedWorkflow = startWorkflowIntent(
      harness.dependencies,
      harness.employeeContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_EMERGENCY_CONTACT_UPDATE,
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
        idempotencyKey: "idem_emergency_contact_unchanged",
        expectedVersion: 1,
        input: {
          proposedEmergencyContact: {
            contactId: "ec_001",
            name: "Alex Doe",
            relationship: "spouse",
            phone: "+15551234567",
            email: "alex.doe@example.com",
            priority: 1,
          },
          effectiveAt: "2026-06-01",
          businessReason: "employee_self_service",
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
        return ok(
          createEmergencyContactPreflightResponse(request) as ExecutorResponse<TOutput>,
        );
      }

      return ok(
        createEmergencyContactPlanResponse(request) as ExecutorResponse<TOutput>,
      );
    },
  };
}

function createEmergencyContactPreflightResponse(
  request: ExecutorRequest,
): ExecutorResponse {
  const input = request.input;
  const currentContacts = input["currentEmergencyContacts"] as Array<
    Record<string, unknown>
  >;
  const proposedContact = input["proposedEmergencyContact"] as Record<string, unknown>;
  const matchingContact = currentContacts.find((contact) => {
    return contact["contactId"] === proposedContact["contactId"];
  });
  const unchanged =
    matchingContact !== undefined &&
    matchingContact["name"] === proposedContact["name"] &&
    matchingContact["relationship"] === proposedContact["relationship"] &&
    matchingContact["phone"] === proposedContact["phone"] &&
    matchingContact["email"] === proposedContact["email"] &&
    matchingContact["priority"] === proposedContact["priority"];

  return {
    status: "succeeded",
    output: {
      valid: !unchanged,
      riskLevel: unchanged ? "high" : "low",
      requiresEvidence: false,
      requiresApproval: true,
      warnings: [],
      errors: unchanged
        ? [
            {
              code: "emergency_contact.unchanged",
              field: "proposedEmergencyContact",
              message: "Emergency contact must differ from the current contact.",
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

function createEmergencyContactPlanResponse(
  request: ExecutorRequest,
): ExecutorResponse {
  const input = request.input;
  const currentContacts = input["currentEmergencyContacts"] as Array<
    Record<string, unknown>
  >;
  const proposedContact = input["proposedEmergencyContact"] as Record<string, unknown>;
  const newEmergencyContacts = currentContacts.map((contact) => {
    if (contact["contactId"] === proposedContact["contactId"]) {
      return proposedContact;
    }

    return contact;
  });

  return {
    status: "succeeded",
    output: {
      internalWrites: [
        {
          eventType: "EmployeeEmergencyContactUpdated",
          subjectType: "worker",
          subjectId: input["workerId"],
          effectiveAt: input["effectiveAt"],
          payload: {
            previousEmergencyContacts: currentContacts,
            changedEmergencyContact: proposedContact,
            newEmergencyContacts,
          },
        },
      ],
      projectionPatches: [
        {
          projection: "employee",
          operation: "replace",
          path: "/emergencyContacts",
          value: newEmergencyContacts,
        },
      ],
      externalCallRequests: [
        {
          connectionId: "fake_hris",
          operation: "updateEmergencyContacts",
          idempotencyKey: `fake_hris_emergency_contact_${String(
            input["changeRequestId"],
          )}`,
          payload: {
            workerId: input["workerId"],
            emergencyContacts: newEmergencyContacts,
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
