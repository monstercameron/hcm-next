import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
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
import { createCompensationDecisionExternalWriteClient } from "../../api/compensation-decision-client.js";
import type { AppDependencies } from "../../api/dependencies.js";
import type {
  ExecutorClient,
  ExecutorRequest,
  ExecutorResponse,
} from "../../api/executor-client.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import { createCompensationDecisionApiServer } from "../../third-party-apis/compensation-decision/server.js";
import {
  createWorkflowApiClient,
  type WorkflowApiClient,
} from "../support/workflow-api-client.js";

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  workflowApi: WorkflowApiClient;
  hrContext: ApiRequestContext;
  compensationAdminContext: ApiRequestContext;
  decisionServer: Server;
};

describe("employee.compensation.change E2E contract", () => {
  let harness: TestHarness | undefined;

  beforeEach(async () => {
    harness = await createHarness();
  });

  afterEach(async () => {
    if (harness !== undefined) {
      await harness.workflowApi.close();
      await closeServer(harness.decisionServer);
      harness = undefined;
    }
  });

  it("runs a compensation change through approval, vendor decision, projection, outbox, and ledger", async () => {
    const activeHarness = requireHarness(harness);
    const startedWorkflow = await activeHarness.workflowApi.startWorkflowIntent(
      activeHarness.hrContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
        subjectType: "worker",
        subjectId: DEMO_IDS.employeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }

    const workflowInstanceId = String(startedWorkflow.value["workflowInstanceId"]);
    const proposedCompensation = compensationFixture(98000);
    const submittedWorkflow = await activeHarness.workflowApi.transitionWorkflow(
      activeHarness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_comp_submit",
        expectedVersion: 1,
        input: {
          proposedCompensation,
          effectiveAt: "2026-06-01",
          businessReason: "retention_adjustment",
        },
      },
    );

    expect(submittedWorkflow.ok).toBe(true);
    expect(submittedWorkflow.ok && submittedWorkflow.value["state"]).toBe(
      "waiting_approval",
    );

    const tasks = await activeHarness.workflowApi.getTasks(
      activeHarness.compensationAdminContext,
    );
    expect(tasks.ok).toBe(true);
    const approvalTask = tasks.ok
      ? (tasks.value["tasks"] as Array<Record<string, unknown>>)[0]
      : undefined;
    const approvalTaskId = String(approvalTask?.["approvalTaskId"]);

    const approvedWorkflow = await activeHarness.workflowApi.transitionWorkflow(
      activeHarness.compensationAdminContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_comp_approve",
        expectedVersion: 2,
        input: {
          approvalTaskId,
          comment: "Compensation increase is in policy.",
        },
      },
    );

    expect(approvedWorkflow.ok).toBe(true);
    expect(approvedWorkflow.ok && approvedWorkflow.value["state"]).toBe("approved");

    const executedWorkflow = await activeHarness.workflowApi.transitionWorkflow(
      activeHarness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_comp_execute",
        expectedVersion: 3,
        input: {},
      },
    );

    expect(executedWorkflow.ok).toBe(true);
    expect(executedWorkflow.ok && executedWorkflow.value["state"]).toBe("executed");

    const executedInstance =
      activeHarness.repositories.store.workflowInstances.get(workflowInstanceId);
    const executedGraphRuntime = graphRuntimeFromInstance(executedInstance);
    expect(executedGraphRuntime?.["activeNodeId"]).toBe("completed");
    expect(graphCompletedNodeIds(executedGraphRuntime)).toEqual(
      expect.arrayContaining([
        "collect_compensation_input",
        "compensation_preflight",
        "compensation_approval",
        "plan_compensation_transaction",
        "vendor_compensation_decision",
        "apply_compensation_projection",
      ]),
    );

    const projection = activeHarness.repositories.store.employeeProjections.get(
      `${DEMO_IDS.tenantId}:${DEMO_IDS.employeeId}`,
    );
    expect(projection?.document.compensation.amount).toBe(98000);
    expect(projection?.document.compensation.currency).toBe("USD");

    const outboxRows = [...activeHarness.repositories.store.integrationOutbox.values()];
    expect(outboxRows).toHaveLength(1);
    expect(outboxRows[0]?.destination).toBe("third_party_compensation_decision");
    expect(outboxRows[0]?.operation).toBe("submitCompensationChange");
    expect(outboxRows[0]?.status).toBe("succeeded");
    expect(outboxRows[0]?.responsePayload?.["status"]).toBe("accepted");

    const timeline = await activeHarness.workflowApi.getTimeline(
      activeHarness.hrContext,
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
          "CompensationPreflighted",
          "ApprovalTaskCreated",
          "ApprovalGranted",
          "TransactionPlanCreated",
          "EmployeeCompensationUpdated",
          "ExternalWriteRequested",
          "ExternalWriteSucceeded",
          "WorkflowCompleted",
        ]),
      );
    }
  });

  it("stops execution when the third-party compensation decision rejects the payload", async () => {
    const activeHarness = requireHarness(harness);
    const startedWorkflow = await activeHarness.workflowApi.startWorkflowIntent(
      activeHarness.hrContext,
      {
        intent: WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
        subjectType: "worker",
        subjectId: DEMO_IDS.employeeId,
      },
    );

    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }

    const workflowInstanceId = String(startedWorkflow.value["workflowInstanceId"]);
    await submitAndApproveCompensationChange(
      activeHarness,
      workflowInstanceId,
      compensationFixture(125000),
    );

    const routedWorkflow = await activeHarness.workflowApi.transitionWorkflow(
      activeHarness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_comp_execute_rejected",
        expectedVersion: 3,
        input: {},
      },
    );

    expect(routedWorkflow.ok).toBe(true);
    expect(routedWorkflow.ok && routedWorkflow.value["state"]).toBe("waiting_repair");
    expect(routedWorkflow.ok && routedWorkflow.value["status"]).toBe("waiting_repair");

    const routedInstance =
      activeHarness.repositories.store.workflowInstances.get(workflowInstanceId);
    const routedGraphRuntime = graphRuntimeFromInstance(routedInstance);
    expect(routedGraphRuntime?.["activeNodeId"]).toBe("repair_vendor_decision");
    expect(graphCompletedNodeIds(routedGraphRuntime)).toEqual(
      expect.arrayContaining([
        "collect_compensation_input",
        "compensation_preflight",
        "compensation_approval",
        "plan_compensation_transaction",
        "vendor_compensation_decision",
      ]),
    );

    const projection = activeHarness.repositories.store.employeeProjections.get(
      `${DEMO_IDS.tenantId}:${DEMO_IDS.employeeId}`,
    );
    expect(projection?.document.compensation.amount).toBe(93000);
    expect(activeHarness.repositories.store.integrationOutbox.size).toBe(0);

    const auditTimeline = await activeHarness.workflowApi.getTimeline(
      activeHarness.hrContext,
      workflowInstanceId,
      "audit",
    );
    expect(auditTimeline.ok).toBe(true);
    if (auditTimeline.ok) {
      const eventTypes = (
        auditTimeline.value["events"] as Array<Record<string, unknown>>
      ).map((event) => event["eventType"]);

      expect(eventTypes).toEqual(
        expect.arrayContaining(["TransactionExecutionStarted", "ExternalWriteFailed"]),
      );
    }
  });
});

async function createHarness(): Promise<TestHarness> {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const server = createCompensationDecisionApiServer();
  const decisionApiOrigin = await startTestServer(server);
  const dependencies: AppDependencies = {
    repositories,
    executorClient: createFakeExecutorClient(),
    externalWriteClients: {
      third_party_compensation_decision:
        createCompensationDecisionExternalWriteClient(decisionApiOrigin),
    },
  };
  const hrActor = mustActor(repositories, DEMO_IDS.hrActorId);
  const compensationAdminActor = mustActor(
    repositories,
    DEMO_IDS.compensationAdminActorId,
  );

  return {
    dependencies,
    repositories,
    workflowApi: await createWorkflowApiClient(dependencies),
    hrContext: createRequestContext(hrActor),
    compensationAdminContext: createRequestContext(compensationAdminActor),
    decisionServer: server,
  };
}

async function submitAndApproveCompensationChange(
  harness: TestHarness,
  workflowInstanceId: string,
  proposedCompensation: Record<string, unknown>,
): Promise<void> {
  const submittedWorkflow = await harness.workflowApi.transitionWorkflow(
    harness.hrContext,
    workflowInstanceId,
    {
      transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
      idempotencyKey: `idem_comp_submit_${String(proposedCompensation["amount"])}`,
      expectedVersion: 1,
      input: {
        proposedCompensation,
        effectiveAt: "2026-06-01",
        businessReason: "retention_adjustment",
      },
    },
  );
  expect(submittedWorkflow.ok).toBe(true);

  const tasks = await harness.workflowApi.getTasks(harness.compensationAdminContext);
  const approvalTask = tasks.ok
    ? (tasks.value["tasks"] as Array<Record<string, unknown>>)[0]
    : undefined;
  const approvedWorkflow = await harness.workflowApi.transitionWorkflow(
    harness.compensationAdminContext,
    workflowInstanceId,
    {
      transition: WORKFLOW_TRANSITIONS.APPROVE,
      idempotencyKey: `idem_comp_approve_${String(proposedCompensation["amount"])}`,
      expectedVersion: 2,
      input: {
        approvalTaskId: String(approvalTask?.["approvalTaskId"]),
        comment: "Compensation increase sent for vendor decision.",
      },
    },
  );
  expect(approvedWorkflow.ok).toBe(true);
}

function createFakeExecutorClient(): ExecutorClient {
  return {
    async executeBlock<TOutput>(
      request: ExecutorRequest,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      if (request.block.name.endsWith(".preflight")) {
        return ok(createCompensationPreflightResponse() as ExecutorResponse<TOutput>);
      }

      return ok(createCompensationPlanResponse(request) as ExecutorResponse<TOutput>);
    },
  };
}

function createCompensationPreflightResponse(): ExecutorResponse {
  return {
    status: "succeeded",
    output: {
      valid: true,
      riskLevel: "medium",
      requiresEvidence: false,
      requiresApproval: true,
      warnings: [],
      errors: [],
    },
    proposedEvents: [],
    externalCallRequests: [],
    logs: [],
    metrics: {
      durationMs: 1,
    },
  };
}

function createCompensationPlanResponse(request: ExecutorRequest): ExecutorResponse {
  const input = request.input;
  const changeRequestId = String(input["changeRequestId"]);
  const proposedCompensation = input["proposedCompensation"] as Record<string, unknown>;

  return {
    status: "succeeded",
    output: {
      internalWrites: [
        {
          eventType: "EmployeeCompensationUpdated",
          subjectType: "worker",
          subjectId: input["workerId"],
          effectiveAt: input["effectiveAt"],
          payload: {
            previousCompensation: input["currentCompensation"],
            newCompensation: proposedCompensation,
            changedFields: ["compensation.amount"],
            increasePercent: 5.38,
          },
        },
      ],
      projectionPatches: [
        {
          projection: "employee",
          operation: "replace",
          path: "/compensation",
          value: proposedCompensation,
        },
      ],
      externalCallRequests: [
        {
          connectionId: "third_party_compensation_decision",
          operation: "submitCompensationChange",
          idempotencyKey: `third_party_comp_decision_${changeRequestId}`,
          payload: {
            workerId: input["workerId"],
            changeRequestId,
            currentCompensation: input["currentCompensation"],
            proposedCompensation,
            effectiveAt: input["effectiveAt"],
            businessReason: input["businessReason"],
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

function startTestServer(server: Server): Promise<string> {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address() as AddressInfo;
      resolve(`http://127.0.0.1:${address.port}`);
    });
  });
}

function closeServer(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) {
        reject(error);
        return;
      }

      resolve();
    });
  });
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

function requireHarness(value: TestHarness | undefined): TestHarness {
  if (value === undefined) {
    throw new Error("Missing test harness.");
  }

  return value;
}

function compensationFixture(amount: number): Record<string, unknown> {
  return {
    amount,
    currency: "USD",
    payFrequency: "annual",
    bonusTargetPercent: 5,
    effectiveDate: "2026-06-01",
  };
}

function graphRuntimeFromInstance(
  workflowInstance: { context: Record<string, unknown> } | undefined,
): Record<string, unknown> | undefined {
  const graphRuntime = workflowInstance?.context["graphRuntime"];

  return typeof graphRuntime === "object" && graphRuntime !== null
    ? (graphRuntime as Record<string, unknown>)
    : undefined;
}

function graphCompletedNodeIds(
  graphRuntime: Record<string, unknown> | undefined,
): string[] {
  const completedNodes = graphRuntime?.["completedNodes"];

  if (!Array.isArray(completedNodes)) {
    return [];
  }

  return completedNodes.flatMap((node) => {
    return typeof node === "object" &&
      node !== null &&
      typeof (node as Record<string, unknown>)["nodeId"] === "string"
      ? [String((node as Record<string, unknown>)["nodeId"])]
      : [];
  });
}
