import { beforeEach, describe, expect, it } from "vitest";
import {
  ok,
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

const contactInfoUpdateIntent = "employee.contact_info.update";

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  employeeContext: ApiRequestContext;
  hrContext: ApiRequestContext;
};

describe("employee.contact_info.update E2E contract", () => {
  let harness: TestHarness;

  beforeEach(() => {
    harness = createHarness();
  });

  it("runs the contact-info workflow through approval, execution, projection, outbox, and ledger", async () => {
    const startedWorkflow = startWorkflowIntent(
      harness.dependencies,
      harness.employeeContext,
      {
        intent: contactInfoUpdateIntent,
        subjectType: "worker",
        subjectId: DEMO_IDS.employeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }

    const workflowInstanceId = String(startedWorkflow.value["workflowInstanceId"]);
    const proposedContactInfo = proposedContactInfoFixture();
    const submittedWorkflow = await transitionWorkflow(
      harness.dependencies,
      harness.employeeContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_contact_info_submit",
        expectedVersion: 1,
        input: {
          proposedContactInfo,
          effectiveAt: "2026-06-01",
          businessReason: "relocation",
        },
      },
    );

    expect(submittedWorkflow.ok).toBe(true);
    expect(submittedWorkflow.ok && submittedWorkflow.value["state"]).toBe(
      "waiting_approval",
    );

    const tasks = getTasks(harness.dependencies, harness.hrContext);
    expect(tasks.ok).toBe(true);
    const approvalTask = tasks.ok
      ? (tasks.value["tasks"] as Array<Record<string, unknown>>)[0]
      : undefined;
    const approvalTaskId = String(approvalTask?.["approvalTaskId"]);

    const approvedWorkflow = await transitionWorkflow(
      harness.dependencies,
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_contact_info_approve",
        expectedVersion: 2,
        input: {
          approvalTaskId,
          comment: "Relocation contact update is complete.",
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
        idempotencyKey: "idem_contact_info_execute",
        expectedVersion: 3,
        input: {},
      },
    );

    expect(executedWorkflow.ok).toBe(true);
    expect(executedWorkflow.ok && executedWorkflow.value["state"]).toBe("executed");

    const projection = harness.repositories.store.employeeProjections.get(
      `${DEMO_IDS.tenantId}:${DEMO_IDS.employeeId}`,
    );
    expect(projection?.document.contact.personalEmail).toBe(
      "jane.rivera.personal@example.com",
    );
    expect(projection?.document.contact.mobilePhone).toBe("+15559998888");
    expect(projection?.document.contact.homeAddress.region).toBe("NY");
    expect(harness.repositories.store.integrationOutbox.size).toBe(1);

    const transactionPlan = [
      ...harness.repositories.store.transactionPlans.values(),
    ][0];
    expect(transactionPlan?.projectionPatches).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          projection: "employee",
          operation: "replace",
          path: "/contact/personalEmail",
        }),
        expect.objectContaining({
          projection: "employee",
          operation: "replace",
          path: "/contact/mobilePhone",
        }),
        expect.objectContaining({
          projection: "employee",
          operation: "replace",
          path: "/contact/homeAddress",
        }),
      ]),
    );

    const timeline = getTimeline(
      harness.dependencies,
      harness.hrContext,
      workflowInstanceId,
    );

    expect(timeline.ok).toBe(true);
    if (timeline.ok) {
      const eventTypes = (
        timeline.value["events"] as Array<Record<string, unknown>>
      ).map((event) => event["eventType"]);

      expect(eventTypes).toEqual(
        expect.arrayContaining([
          "WorkflowIntentStarted",
          "ChangeRequestCreated",
          "ContactInfoPreflighted",
          "ApprovalTaskCreated",
          "ApprovalGranted",
          "TransactionPlanCreated",
          "EmployeeContactInfoUpdated",
          "ExternalWriteRequested",
          "WorkflowCompleted",
        ]),
      );
      expect(eventTypes).not.toContain("WorkflowTransitionSubmitted");
      expect(eventTypes).not.toContain("WorkflowStateChanged");
    }
  });

  it("rejects unchanged contact information without creating a change request", async () => {
    const startedWorkflow = startWorkflowIntent(
      harness.dependencies,
      harness.employeeContext,
      {
        intent: contactInfoUpdateIntent,
        subjectType: "worker",
        subjectId: DEMO_IDS.employeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }

    const unchangedContactInfo = harness.repositories.store.employeeProjections.get(
      `${DEMO_IDS.tenantId}:${DEMO_IDS.employeeId}`,
    )?.document.contact;
    const submittedWorkflow = await transitionWorkflow(
      harness.dependencies,
      harness.employeeContext,
      String(startedWorkflow.value["workflowInstanceId"]),
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_contact_info_unchanged",
        expectedVersion: 1,
        input: {
          proposedContactInfo: unchangedContactInfo,
          effectiveAt: "2026-06-01",
          businessReason: "employee_self_service",
        },
      },
    );

    expect(submittedWorkflow.ok).toBe(false);
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
  const employeeActor = mustActor(repositories, DEMO_IDS.employeeActorId);
  const hrActor = mustActor(repositories, DEMO_IDS.hrActorId);

  return {
    dependencies,
    repositories,
    employeeContext: createRequestContext(employeeActor),
    hrContext: createRequestContext(hrActor),
  };
}

function mustActor(repositories: Repositories, actorId: string): ActorRecord {
  const actorResult = repositories.actors.findById(actorId);

  if (!actorResult.ok) {
    throw new Error(`Missing actor ${actorId}`);
  }

  return actorResult.value;
}

function createRequestContext(actor: ActorRecord): ApiRequestContext {
  return {
    tenantId: DEMO_IDS.tenantId,
    environmentId: DEMO_IDS.environmentId,
    actor,
    requestId: `req_${actor.actorId}`,
    correlationId: `corr_${actor.actorId}`,
  };
}

function createFakeExecutorClient(): ExecutorClient {
  return {
    executeBlock<TOutput>(
      request: ExecutorRequest,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      if (request.block.name.endsWith(".preflight")) {
        return Promise.resolve(
          ok(createContactInfoPreflightResponse(request) as ExecutorResponse<TOutput>),
        );
      }

      return Promise.resolve(
        ok(createContactInfoPlanResponse(request) as ExecutorResponse<TOutput>),
      );
    },
  };
}

function createContactInfoPreflightResponse(
  request: ExecutorRequest,
): ExecutorResponse {
  const input = request.input;
  const currentContactInfo = input["currentContactInfo"] as Record<string, unknown>;
  const proposedContactInfo = input["proposedContactInfo"] as Record<string, unknown>;
  const isUnchanged =
    JSON.stringify(currentContactInfo) === JSON.stringify(proposedContactInfo);

  return {
    status: "succeeded",
    output: {
      valid: !isUnchanged,
      riskLevel: isUnchanged ? "high" : "medium",
      requiresEvidence: false,
      requiresApproval: true,
      warnings: isUnchanged
        ? []
        : [
            {
              code: "contact_info.region_changed",
              field: "proposedContactInfo.homeAddress.region",
              message: "Region changed; local payroll or tax rules may be affected.",
            },
          ],
      errors: isUnchanged
        ? [
            {
              code: "contact_info.unchanged",
              field: "proposedContactInfo",
              message:
                "Contact information must differ from current contact information.",
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

function createContactInfoPlanResponse(request: ExecutorRequest): ExecutorResponse {
  const input = request.input;
  const proposedContactInfo = input["proposedContactInfo"] as Record<string, unknown>;

  return {
    status: "succeeded",
    output: {
      internalWrites: [
        {
          eventType: "EmployeeContactInfoUpdated",
          subjectType: "worker",
          subjectId: input["workerId"],
          effectiveAt: input["effectiveAt"],
          payload: {
            previousContactInfo: input["currentContactInfo"],
            newContactInfo: proposedContactInfo,
            changedFields: [
              "contact.personalEmail",
              "contact.mobilePhone",
              "contact.homeAddress",
            ],
          },
        },
      ],
      projectionPatches: [
        {
          projection: "employee",
          operation: "replace",
          path: "/contact/personalEmail",
          value: proposedContactInfo["personalEmail"],
        },
        {
          projection: "employee",
          operation: "replace",
          path: "/contact/mobilePhone",
          value: proposedContactInfo["mobilePhone"],
        },
        {
          projection: "employee",
          operation: "replace",
          path: "/contact/homeAddress",
          value: proposedContactInfo["homeAddress"],
        },
      ],
      externalCallRequests: [
        {
          connectionId: "fake_hris",
          operation: "updateContactInfo",
          idempotencyKey: `fake_hris_contact_info_${String(input["changeRequestId"])}`,
          payload: {
            workerId: input["workerId"],
            contactInfo: proposedContactInfo,
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

function proposedContactInfoFixture(): Record<string, unknown> {
  return {
    personalEmail: "jane.rivera.personal@example.com",
    mobilePhone: "+15559998888",
    homeAddress: {
      line1: "200 Park Ave",
      line2: "Apt 8",
      city: "New York",
      region: "NY",
      postalCode: "10017",
      country: "US",
    },
  };
}
