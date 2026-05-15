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
  type Repositories,
} from "@hcm-next/data-store";
import { createApiServer } from "../../api/server.js";
import type { AppDependencies } from "../../api/dependencies.js";
import type {
  ExecutorClient,
  ExecutorRequest,
  ExecutorResponse,
} from "../../api/executor-client.js";

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  apiServer: Server;
  apiOrigin: string;
};

type JsonResponse = {
  status: number;
  body: Record<string, unknown>;
};

describe("workflow admin registry E2E contract", () => {
  let harness: TestHarness | undefined;

  beforeEach(async () => {
    harness = await createHarness();
  });

  afterEach(async () => {
    if (harness !== undefined) {
      await closeServer(harness.apiServer);
      harness = undefined;
    }
  });

  it("imports filesystem workflow configs, lists registry metadata, and treats repeat imports as idempotent", async () => {
    const activeHarness = requireHarness(harness);
    const firstImport = await adminPost(
      activeHarness,
      "/admin/workflows/import-from-files",
    );

    expect(firstImport.status).toBe(200);
    expect(numberField(firstImport.body, "imported")).toBeGreaterThan(0);
    expect(numberField(firstImport.body, "rejected")).toBe(0);

    const listResponse = await adminGet(activeHarness, "/admin/workflows");
    expect(listResponse.status).toBe(200);
    const legalNameImport = requireWorkflowVersionForIntent(
      listResponse.body,
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    expect(legalNameImport["workflowVersionId"]).toBeTruthy();
    expect(legalNameImport["configHash"]).toBeTruthy();
    expect(legalNameImport["status"]).toBe("published");
    expect(
      workflowDefinitionForIntent(listResponse.body, legalNameImport["intent"]),
    ).toEqual(
      expect.objectContaining({
        currentVersionId: legalNameImport["workflowVersionId"],
      }),
    );

    const secondImport = await adminPost(
      activeHarness,
      "/admin/workflows/import-from-files",
    );
    expect(secondImport.status).toBe(200);
    expect(numberField(secondImport.body, "imported")).toBe(0);
    expect(numberField(secondImport.body, "skipped")).toBeGreaterThan(0);
    expect(numberField(secondImport.body, "rejected")).toBe(0);
  });

  it("pins a newly started legal-name workflow instance to the imported registry version", async () => {
    const activeHarness = requireHarness(harness);
    const importResponse = await adminPost(
      activeHarness,
      "/admin/workflows/import-from-files",
    );
    expect(importResponse.status).toBe(200);

    const listResponse = await adminGet(activeHarness, "/admin/workflows");
    expect(listResponse.status).toBe(200);
    const importedLegalNameVersion = requireWorkflowVersionForIntent(
      listResponse.body,
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const startResponse = await employeePost(activeHarness, "/workflow-intents", {
      intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
      subjectType: "worker",
      subjectId: DEMO_IDS.employeeId,
    });

    expect(startResponse.status).toBe(200);
    expect(startResponse.body["workflowVersionId"]).toBe(
      importedLegalNameVersion.workflowVersionId,
    );
    expect(startResponse.body["workflowDefinitionId"]).toBe(
      importedLegalNameVersion.workflowDefinitionId,
    );

    const workflowInstance = activeHarness.repositories.store.workflowInstances.get(
      String(startResponse.body["workflowInstanceId"]),
    );
    expect(workflowInstance?.workflowVersionId).toBe(
      importedLegalNameVersion.workflowVersionId,
    );
    expect(workflowInstance?.workflowDefinitionId).toBe(
      importedLegalNameVersion.workflowDefinitionId,
    );

    const intentStartedEvent = activeHarness.repositories.store.ledgerEvents.find(
      (event) => {
        return (
          event.workflowInstanceId === startResponse.body["workflowInstanceId"] &&
          event.eventType === "WorkflowIntentStarted"
        );
      },
    );
    expect(intentStartedEvent?.workflowVersionId).toBe(
      importedLegalNameVersion.workflowVersionId,
    );
  });

  it("runs compensation after import and records business ledger events against the imported version", async () => {
    const activeHarness = requireHarness(harness);
    const importResponse = await adminPost(
      activeHarness,
      "/admin/workflows/import-from-files",
    );
    expect(importResponse.status).toBe(200);

    const listResponse = await adminGet(activeHarness, "/admin/workflows");
    expect(listResponse.status).toBe(200);
    const importedCompensationVersion = requireWorkflowVersionForIntent(
      listResponse.body,
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const startedWorkflow = await hrPost(activeHarness, "/workflow-intents", {
      intent: WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
      subjectType: "worker",
      subjectId: DEMO_IDS.employeeId,
    });
    expect(startedWorkflow.status).toBe(200);

    const workflowInstanceId = String(startedWorkflow.body["workflowInstanceId"]);
    expect(startedWorkflow.body["workflowVersionId"]).toBe(
      importedCompensationVersion.workflowVersionId,
    );

    const submittedWorkflow = await hrPost(
      activeHarness,
      `/workflow-instances/${workflowInstanceId}/transitions`,
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_admin_registry_comp_submit",
        expectedVersion: 1,
        input: {
          proposedCompensation: compensationFixture(98000),
          effectiveAt: "2026-06-01",
          businessReason: "retention_adjustment",
        },
      },
    );
    expect(submittedWorkflow.status).toBe(200);

    const approvalTask = [
      ...activeHarness.repositories.store.approvalTasks.values(),
    ].find((task) => task.workflowInstanceId === workflowInstanceId);
    expect(approvalTask).toBeDefined();

    const approvedWorkflow = await compensationAdminPost(
      activeHarness,
      `/workflow-instances/${workflowInstanceId}/transitions`,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_admin_registry_comp_approve",
        expectedVersion: 2,
        input: {
          approvalTaskId: approvalTask?.approvalTaskId,
          comment: "Imported compensation workflow remains executable.",
        },
      },
    );
    expect(approvedWorkflow.status).toBe(200);

    const executedWorkflow = await hrPost(
      activeHarness,
      `/workflow-instances/${workflowInstanceId}/transitions`,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_admin_registry_comp_execute",
        expectedVersion: 3,
        input: {},
      },
    );
    expect(executedWorkflow.status).toBe(200);

    const eventTypes = activeHarness.repositories.store.ledgerEvents
      .filter((event) => event.workflowInstanceId === workflowInstanceId)
      .map((event) => event.eventType);

    expect(eventTypes).toEqual(
      expect.arrayContaining([
        "WorkflowIntentStarted",
        "ChangeRequestCreated",
        "CompensationPreflighted",
        "ApprovalTaskCreated",
        "ApprovalGranted",
        "TransactionPlanCreated",
        "EmployeeProjectionUpdated",
        "ExternalWriteRequested",
        "WorkflowCompleted",
      ]),
    );
    expect(
      activeHarness.repositories.store.ledgerEvents
        .filter((event) => event.workflowInstanceId === workflowInstanceId)
        .every((event) => {
          return (
            event.workflowVersionId === importedCompensationVersion.workflowVersionId
          );
        }),
    ).toBe(true);
  });
});

async function createHarness(): Promise<TestHarness> {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const dependencies: AppDependencies = {
    repositories,
    executorClient: createFakeExecutorClient(),
  };
  const apiServer = createApiServer(dependencies);
  const apiOrigin = await startTestServer(apiServer);

  return {
    dependencies,
    repositories,
    apiServer,
    apiOrigin,
  };
}

function createFakeExecutorClient(): ExecutorClient {
  return {
    async executeBlock<TOutput>(
      request: ExecutorRequest,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      if (request.block.name.endsWith(".preflight")) {
        return ok(createPreflightResponse() as ExecutorResponse<TOutput>);
      }

      return ok(createPlanResponse(request) as ExecutorResponse<TOutput>);
    },
  };
}

function createPreflightResponse(): ExecutorResponse {
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
    metrics: { durationMs: 1 },
  };
}

function createPlanResponse(request: ExecutorRequest): ExecutorResponse {
  const input = request.input;
  const proposedCompensation = input["proposedCompensation"] as
    | Record<string, unknown>
    | undefined;

  if (proposedCompensation !== undefined) {
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
        externalCallRequests: [],
      },
      proposedEvents: [],
      externalCallRequests: [],
      logs: [],
      metrics: { durationMs: 1 },
    };
  }

  return {
    status: "succeeded",
    output: {
      internalWrites: [],
      projectionPatches: [],
      externalCallRequests: [],
    },
    proposedEvents: [],
    externalCallRequests: [],
    logs: [],
    metrics: { durationMs: 1 },
  };
}

async function adminGet(harness: TestHarness, path: string): Promise<JsonResponse> {
  return httpJson(harness, "GET", path, DEMO_IDS.hrActorId);
}

async function adminPost(
  harness: TestHarness,
  path: string,
  body: Record<string, unknown> = {},
): Promise<JsonResponse> {
  return httpJson(harness, "POST", path, DEMO_IDS.hrActorId, body);
}

async function employeePost(
  harness: TestHarness,
  path: string,
  body: Record<string, unknown>,
): Promise<JsonResponse> {
  return httpJson(harness, "POST", path, DEMO_IDS.employeeActorId, body);
}

async function hrPost(
  harness: TestHarness,
  path: string,
  body: Record<string, unknown>,
): Promise<JsonResponse> {
  return httpJson(harness, "POST", path, DEMO_IDS.hrActorId, body);
}

async function compensationAdminPost(
  harness: TestHarness,
  path: string,
  body: Record<string, unknown>,
): Promise<JsonResponse> {
  return httpJson(harness, "POST", path, DEMO_IDS.compensationAdminActorId, body);
}

async function httpJson(
  harness: TestHarness,
  method: "GET" | "POST",
  path: string,
  actorId: string,
  body?: Record<string, unknown>,
): Promise<JsonResponse> {
  const response = await fetch(`${harness.apiOrigin}${path}`, {
    method,
    headers: {
      "content-type": "application/json",
      "x-demo-actor-id": actorId,
      "x-request-id": `req_${actorId}`,
      "x-correlation-id": `corr_${actorId}`,
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const responseBody = (await response.json()) as Record<string, unknown>;

  return {
    status: response.status,
    body: responseBody,
  };
}

function workflowVersionForIntent(
  body: Record<string, unknown>,
  intent: string,
): Record<string, unknown> | undefined {
  return workflowVersionsFromBody(body).find((version) => version["intent"] === intent);
}

function requireWorkflowVersionForIntent(
  body: Record<string, unknown>,
  intent: string,
): Record<string, unknown> {
  const workflowDefinition = workflowDefinitionForIntent(body, intent);
  const currentVersionId = workflowDefinition?.["currentVersionId"];
  const versions = workflowVersionsFromBody(body);
  const workflowVersion =
    versions.find((version) => {
      return (
        currentVersionId !== undefined &&
        version["workflowVersionId"] === currentVersionId
      );
    }) ?? workflowVersionForIntent(body, intent);

  if (workflowVersion === undefined) {
    throw new Error(`Missing imported workflow version for ${intent}.`);
  }

  return workflowVersion;
}

function workflowVersionsFromBody(
  body: Record<string, unknown>,
): Record<string, unknown>[] {
  for (const key of ["workflowVersions", "versions", "importedVersions"]) {
    const value = body[key];

    if (Array.isArray(value)) {
      return value as Record<string, unknown>[];
    }
  }

  const workflows = body["workflows"];
  if (!Array.isArray(workflows)) {
    return [];
  }

  return workflows.flatMap((workflow) => {
    if (typeof workflow !== "object" || workflow === null) {
      return [];
    }

    const versions = (workflow as Record<string, unknown>)["versions"];
    return Array.isArray(versions) ? (versions as Record<string, unknown>[]) : [];
  });
}

function workflowDefinitionForIntent(
  body: Record<string, unknown>,
  intent: unknown,
): Record<string, unknown> | undefined {
  const workflows = body["workflows"];

  if (!Array.isArray(workflows)) {
    return undefined;
  }

  return (workflows as Record<string, unknown>[]).find((workflow) => {
    if (workflow["intent"] === intent) {
      return true;
    }

    const versions = workflow["versions"];
    return (
      Array.isArray(versions) &&
      versions.some((version) => {
        return (
          typeof version === "object" &&
          version !== null &&
          (version as Record<string, unknown>)["intent"] === intent
        );
      })
    );
  });
}

function numberField(body: Record<string, unknown>, key: string): number {
  const value = body[key];

  return typeof value === "number" ? value : 0;
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

function requireHarness(value: TestHarness | undefined): TestHarness {
  if (value === undefined) {
    throw new Error("Missing test harness.");
  }

  return value;
}
