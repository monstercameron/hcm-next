import { readdirSync, readFileSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  WORKFLOW_TRANSITIONS,
  ok,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import {
  createRepositories,
  createSeededDemoStore,
  DEMO_IDS,
  type ActorRecord,
  type Repositories,
} from "@human-capital-management-suite/data-store";
import type { AppDependencies } from "../../api/dependencies.js";
import type {
  ExecutorClient,
  ExecutorRequest,
  ExecutorResponse,
} from "../../api/executor-client.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import contactInfoWorkflowConfigJson from "../../workflows/configs/employee-contact-info-update.workflow.json";
import {
  createWorkflowApiClient,
  type WorkflowApiClient,
} from "../support/workflow-api-client.js";

const genericContactInfoIntent = "employee.generic_contact_info_test.update";

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  workflowApi: WorkflowApiClient;
  employeeContext: ApiRequestContext;
  hrContext: ApiRequestContext;
};

describe("generic workflow runtime E2E contract", () => {
  let harness: TestHarness;

  beforeEach(async () => {
    harness = await createHarness();
  });

  afterEach(async () => {
    await harness.workflowApi.close();
  });

  it("keeps e2e workflows on public API routes instead of workflow service imports", () => {
    expect(workflowServiceImportOffenders()).toEqual([]);
  });

  it("imports and runs a new configured workflow intent without a TypeScript service edit", async () => {
    const workflowConfig = genericContactInfoWorkflowConfig();

    expect(workflowServiceSources()).not.toContain(genericContactInfoIntent);

    const importResult = await harness.workflowApi.postAdminWorkflowConfig(
      harness.hrContext,
      {
        workflowConfig,
        publish: true,
      },
    );
    expect(importResult.ok).toBe(true);
    if (!importResult.ok) {
      return;
    }
    expect(importResult.value["imported"]).toBe(1);
    expect(importResult.value["published"]).toBe(1);

    const startedWorkflow = await harness.workflowApi.startWorkflowIntent(
      harness.employeeContext,
      {
        intent: genericContactInfoIntent,
        subjectType: "worker",
        subjectId: DEMO_IDS.employeeId,
      },
    );
    expect(startedWorkflow.ok).toBe(true);
    if (!startedWorkflow.ok) {
      return;
    }
    expect(startedWorkflow.value["workflowVersionId"]).toBe(
      importResult.value["workflowVersionId"],
    );

    const workflowInstanceId = String(startedWorkflow.value["workflowInstanceId"]);
    const proposedContactInfo = genericContactInfoFixture();
    const submittedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.employeeContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_generic_contact_submit",
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

    const tasks = await harness.workflowApi.getTasks(harness.hrContext);
    expect(tasks.ok).toBe(true);
    const approvalTask = tasks.ok
      ? (tasks.value["tasks"] as Array<Record<string, unknown>>)[0]
      : undefined;

    const approvedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.APPROVE,
        idempotencyKey: "idem_generic_contact_approve",
        expectedVersion: 2,
        input: {
          approvalTaskId: String(approvalTask?.["approvalTaskId"]),
          comment: "Generic config workflow approved.",
        },
      },
    );
    expect(approvedWorkflow.ok).toBe(true);
    expect(approvedWorkflow.ok && approvedWorkflow.value["state"]).toBe("approved");

    const executedWorkflow = await harness.workflowApi.transitionWorkflow(
      harness.hrContext,
      workflowInstanceId,
      {
        transition: WORKFLOW_TRANSITIONS.EXECUTE,
        idempotencyKey: "idem_generic_contact_execute",
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
      "jane.generic.personal@example.com",
    );
    expect(harness.repositories.store.integrationOutbox.size).toBe(1);

    const timeline = await harness.workflowApi.getTimeline(
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
          "ApprovalGranted",
          "TransactionPlanCreated",
          "EmployeeContactInfoUpdated",
          "WorkflowCompleted",
        ]),
      );
    }
  });
});

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
      unwrapResult(repositories.actors.findById(DEMO_IDS.secondAdminActorId)),
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
    executeBlock<TOutput>(
      request: ExecutorRequest,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      const response = request.block.name.endsWith(".preflight")
        ? createGenericContactPreflightResponse()
        : createGenericContactPlanResponse(request);

      return Promise.resolve(ok(response as ExecutorResponse<TOutput>));
    },
  };
}

function createGenericContactPreflightResponse(): ExecutorResponse {
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

function createGenericContactPlanResponse(request: ExecutorRequest): ExecutorResponse {
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
          idempotencyKey: `fake_hris_generic_contact_${String(
            input["changeRequestId"],
          )}`,
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

function genericContactInfoWorkflowConfig(): Record<string, unknown> {
  return {
    ...(JSON.parse(JSON.stringify(contactInfoWorkflowConfigJson)) as Record<
      string,
      unknown
    >),
    intent: genericContactInfoIntent,
  };
}

function genericContactInfoFixture(): Record<string, unknown> {
  return {
    personalEmail: "jane.generic.personal@example.com",
    mobilePhone: "+15551112222",
    homeAddress: {
      line1: "300 Main St",
      line2: null,
      city: "Cambridge",
      region: "MA",
      postalCode: "02139",
      country: "US",
    },
  };
}

function workflowServiceImportOffenders(): string[] {
  const testRoot = resolve(process.cwd(), "src/tests");
  const serviceImportPattern =
    /(?:from\s+["'][^"']*workflows\/(?!configs\/)[^"']*\/service\.js["']|import\(["'][^"']*workflows\/(?!configs\/)[^"']*\/service\.js["']\))/;

  return sourceFiles(testRoot)
    .filter((filePath) => serviceImportPattern.test(readFileSync(filePath, "utf8")))
    .map((filePath) => relative(process.cwd(), filePath).replace(/\\/g, "/"));
}

function workflowServiceSources(): string {
  const workflowsRoot = resolve(process.cwd(), "src/workflows");

  return sourceFiles(workflowsRoot)
    .filter((filePath) => filePath.endsWith("service.ts"))
    .map((filePath) => readFileSync(filePath, "utf8"))
    .join("\n");
}

function sourceFiles(root: string): string[] {
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = join(root, entry.name);

    if (entry.isDirectory()) {
      return sourceFiles(entryPath);
    }

    return entry.isFile() && entry.name.endsWith(".ts") ? [entryPath] : [];
  });
}
