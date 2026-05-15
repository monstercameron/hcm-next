import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { beforeEach, describe, expect, it } from "vitest";
import {
  WORKFLOW_TRANSITIONS,
  ok,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  createRepositories,
  createSeededDemoStore,
  DEMO_IDS,
  type ActorRecord,
  type EmployeeProjectionDocument,
  type EmployeeProjectionRecord,
  type Repositories,
} from "@hcm-next/data-store";
import type {
  ExecutorClient,
  ExecutorRequest,
  ExecutorResponse,
} from "../../api/executor-client.js";
import { createApiServer } from "../../api/server.js";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ApiRequestContext } from "../../api/request-context.js";
import * as employeeAccessService from "../../workflows/legal-name-change/service.js";

const contactInfoUpdateIntent = "employee.contact_info.update";
const maskedValueKeys = ["masked", "redacted", "restricted"];

type EmployeeListService = typeof employeeAccessService & {
  listEmployeeProjections?: (
    dependencies: AppDependencies,
    requestContext: ApiRequestContext,
  ) => Result<Record<string, unknown>, AppError>;
};

type TestHarness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  primaryHrContext: ApiRequestContext;
  managerContext: ApiRequestContext;
  employeeContext: ApiRequestContext;
  compensationAdminContext: ApiRequestContext;
  secondHrContext: ApiRequestContext;
};

describe("RBAC employee access E2E contract", () => {
  let harness: TestHarness;

  beforeEach(() => {
    harness = createHarness();
  });

  it("allows the primary HR admin to see the broad employee list", () => {
    const employees = listEmployees(harness.dependencies, harness.primaryHrContext);

    expect(employees.length).toBe(harness.repositories.store.employeeProjections.size);
    expect(employeeIds(employees)).toEqual(
      expect.arrayContaining(
        [...harness.repositories.store.employeeProjections.values()].map(
          (projection) => projection.employeeId,
        ),
      ),
    );
  });

  it("seeds org-aware RBAC graph records alongside legacy access grants", () => {
    expect(harness.repositories.store.organizationUnits.size).toBeGreaterThan(0);
    expect(harness.repositories.store.organizationRelationships.size).toBeGreaterThan(
      0,
    );
    expect(harness.repositories.store.workerAssignments.size).toBeGreaterThan(
      harness.repositories.store.employeeProjections.size,
    );
    expect(harness.repositories.store.roleBindings.size).toBeGreaterThan(
      harness.repositories.store.accessGrants.size,
    );

    const employeeAssignments =
      harness.repositories.workerAssignments.findActiveForEmployee(
        DEMO_IDS.tenantId,
        DEMO_IDS.employeeId,
      );
    const hrRoleBindings = harness.repositories.roleBindings.findActiveForActor(
      DEMO_IDS.tenantId,
      DEMO_IDS.hrActorId,
    );

    expect(employeeAssignments.ok).toBe(true);
    expect(hrRoleBindings.ok).toBe(true);

    if (!employeeAssignments.ok || !hrRoleBindings.ok) {
      return;
    }

    expect(
      employeeAssignments.value.map((assignment) => assignment.assignmentType),
    ).toEqual(
      expect.arrayContaining([
        "legal_employer",
        "primary_team",
        "work_location",
        "cost_center",
      ]),
    );
    expect(hrRoleBindings.value.some((binding) => binding.scopeType === "global")).toBe(
      true,
    );
  });

  it("limits a manager to self and direct reports only", () => {
    const managerEmployeeId = harness.managerContext.actor.linkedWorkerId;

    expect(managerEmployeeId).toBeDefined();
    const employees = listEmployees(harness.dependencies, harness.managerContext);

    expect(employees.length).toBeGreaterThan(0);
    expect(
      employees.every((employee) => {
        return isManagerSelfOrDirectReport(employee, managerEmployeeId);
      }),
    ).toBe(true);
  });

  it("limits an employee to their own employee projection", () => {
    const employees = listEmployees(harness.dependencies, harness.employeeContext);

    expect(employeeIds(employees)).toEqual([
      harness.employeeContext.actor.linkedWorkerId,
    ]);
  });

  it("allows a compensation admin to see compensation fields for scoped employees", () => {
    const employees = listEmployees(
      harness.dependencies,
      harness.compensationAdminContext,
    );

    expect(employees.length).toBeGreaterThan(0);
    expect(
      employees.every((employee) => {
        const compensation = compensationFor(employee);
        return (
          compensation !== undefined &&
          !isMasked(compensation) &&
          typeof compensation["amount"] === "number" &&
          typeof compensation["currency"] === "string" &&
          typeof compensation["effectiveDate"] === "string"
        );
      }),
    ).toBe(true);
  });

  it("masks compensation and contact data for limited actors", () => {
    const employee = readEmployeeProjection(
      harness.dependencies,
      harness.managerContext,
      DEMO_IDS.employeeId,
    );
    const sourceProjection = unwrapResult(
      harness.repositories.employeeProjections.findByEmployeeId(
        harness.managerContext.tenantId,
        DEMO_IDS.employeeId,
      ),
    );

    expect(isMasked(compensationFor(employee))).toBe(true);
    expect(isMaskedContact(contactFor(employee))).toBe(true);
    expect(JSON.stringify(employee)).not.toContain(
      sourceProjection.document.contact.personalEmail ?? "",
    );
    expect(JSON.stringify(employee)).not.toContain(
      sourceProjection.document.contact.mobilePhone ?? "",
    );
    expect(JSON.stringify(employee)).not.toContain(
      String(compensationFor(sourceProjection.document)?.["amount"] ?? ""),
    );
  });

  it("serves the RBAC-filtered employee list through the API route", async () => {
    const apiResponse = await readApiJson(
      harness.dependencies,
      "/employees",
      DEMO_IDS.managerActorId,
    );
    const employees = apiResponse.body["employees"] as EmployeeProjectionRecord[];

    expect(apiResponse.status).toBe(200);
    expect(employees.length).toBeGreaterThan(0);
    expect(
      employees.every((employee) => {
        return isManagerSelfOrDirectReport(employee, DEMO_IDS.managerEmployeeId);
      }),
    ).toBe(true);
  });

  it("denies out-of-scope employee projection reads through the API route", async () => {
    const apiResponse = await readApiJson(
      harness.dependencies,
      `/employees/${DEMO_IDS.managerEmployeeId}`,
      DEMO_IDS.employeeActorId,
    );
    const error = apiResponse.body["error"] as Record<string, unknown>;

    expect(apiResponse.status).toBe(403);
    expect(error["code"]).toBe("PERMISSION_DENIED");
  });

  it("allows a second HR admin to receive an approval task for an existing workflow", async () => {
    const startedWorkflow = employeeAccessService.startWorkflowIntent(
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

    const submittedWorkflow = await employeeAccessService.transitionWorkflow(
      harness.dependencies,
      harness.employeeContext,
      String(startedWorkflow.value["workflowInstanceId"]),
      {
        transition: WORKFLOW_TRANSITIONS.SUBMIT_INPUT,
        idempotencyKey: "idem_rbac_second_admin_submit",
        expectedVersion: 1,
        input: {
          proposedContactInfo: proposedContactInfoFixture(),
          effectiveAt: "2026-06-01",
          businessReason: "relocation",
        },
      },
    );

    expect(submittedWorkflow.ok).toBe(true);
    expect(submittedWorkflow.ok && submittedWorkflow.value["state"]).toBe(
      "waiting_approval",
    );

    const tasksResult = employeeAccessService.getTasks(
      harness.dependencies,
      harness.secondHrContext,
    );

    expect(tasksResult.ok).toBe(true);
    if (!tasksResult.ok) {
      return;
    }

    const tasks = tasksResult.value["tasks"] as Array<Record<string, unknown>>;
    expect(tasks).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          workflowInstanceId: startedWorkflow.value["workflowInstanceId"],
          assigneeActorId: harness.secondHrContext.actor.actorId,
          status: "pending",
        }),
      ]),
    );
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
    primaryHrContext: createRequestContext(mustActor(repositories, DEMO_IDS.hrActorId)),
    managerContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.managerActorId),
    ),
    employeeContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.employeeActorId),
    ),
    compensationAdminContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.compensationAdminActorId),
    ),
    secondHrContext: createRequestContext(
      mustActor(repositories, DEMO_IDS.secondAdminActorId),
    ),
  };
}

function mustActor(repositories: Repositories, actorId: string): ActorRecord {
  const actorResult = repositories.actors.findById(actorId);

  if (!actorResult.ok) {
    throw new Error(`Missing seeded actor ${actorId}`);
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

function listEmployees(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): EmployeeProjectionRecord[] {
  const listEmployeeProjections = (employeeAccessService as EmployeeListService)
    .listEmployeeProjections;

  if (listEmployeeProjections === undefined) {
    throw new Error(
      "Missing service export listEmployeeProjections(dependencies, requestContext).",
    );
  }

  const result = listEmployeeProjections(dependencies, requestContext);
  const value = unwrapResult(result);

  return value["employees"] as EmployeeProjectionRecord[];
}

function readEmployeeProjection(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  employeeId: string,
): EmployeeProjectionDocument {
  const result = employeeAccessService.getEmployeeProjection(
    dependencies,
    requestContext,
    employeeId,
  );
  const value = unwrapResult(result);
  const projection = value["projection"] as { document?: EmployeeProjectionDocument };

  return projection.document ?? (projection as unknown as EmployeeProjectionDocument);
}

function unwrapResult<TValue>(result: Result<TValue, AppError>): TValue {
  if (!result.ok) {
    throw new Error(result.error.message);
  }

  return result.value;
}

function employeeIds(employees: EmployeeProjectionRecord[]): string[] {
  return employees.map((employee) => employee.employeeId).sort();
}

function indexedManagerId(employee: EmployeeProjectionRecord): string | undefined {
  const indexedManagerEmployeeId = employee.indexedFields["managerEmployeeId"];

  return typeof indexedManagerEmployeeId === "string"
    ? indexedManagerEmployeeId
    : undefined;
}

function isManagerSelfOrDirectReport(
  employee: EmployeeProjectionRecord,
  managerEmployeeId: string | undefined,
): boolean {
  return (
    managerEmployeeId !== undefined &&
    (employee.employeeId === managerEmployeeId ||
      indexedManagerId(employee) === managerEmployeeId)
  );
}

function contactFor(
  employee: EmployeeProjectionDocument | EmployeeProjectionRecord,
): unknown {
  return employeeDocument(employee).contact;
}

function compensationFor(
  employee: EmployeeProjectionDocument | EmployeeProjectionRecord,
): Record<string, unknown> | undefined {
  const compensation = (
    employeeDocument(employee) as unknown as Record<string, unknown>
  )["compensation"];

  if (compensation === undefined || compensation === null) {
    return undefined;
  }

  if (typeof compensation !== "object" || Array.isArray(compensation)) {
    return { value: compensation };
  }

  return compensation as Record<string, unknown>;
}

function employeeDocument(
  employee: EmployeeProjectionDocument | EmployeeProjectionRecord,
): EmployeeProjectionDocument {
  if ("document" in employee) {
    return employee.document;
  }

  return employee;
}

function isMasked(value: unknown): boolean {
  if (value === undefined || value === null) {
    return true;
  }

  if (typeof value === "string") {
    const normalizedValue = value.toLowerCase();
    return normalizedValue.includes("mask") || normalizedValue.includes("restricted");
  }

  if (typeof value !== "object" || Array.isArray(value)) {
    return false;
  }

  const record = value as Record<string, unknown>;
  return (
    maskedValueKeys.some((key) => record[key] === true) ||
    Object.values(record).some((recordValue) => {
      return isMasked(recordValue);
    })
  );
}

function isMaskedContact(value: unknown): boolean {
  if (isMasked(value)) {
    return true;
  }

  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }

  const contact = value as {
    personalEmail?: unknown;
    mobilePhone?: unknown;
    homeAddress?: { line1?: unknown };
  };

  return (
    contact.personalEmail === null &&
    contact.mobilePhone === null &&
    isMasked(contact.homeAddress?.line1)
  );
}

type ApiJsonResponse = {
  status: number;
  body: Record<string, unknown>;
};

async function readApiJson(
  dependencies: AppDependencies,
  path: string,
  actorId: string,
): Promise<ApiJsonResponse> {
  const server = createApiServer(dependencies);
  const origin = await listenOnEphemeralPort(server);
  const response = await fetch(`${origin}${path}`, {
    headers: {
      "x-demo-actor-id": actorId,
      "x-request-id": `req_api_${actorId}`,
      "x-correlation-id": `corr_api_${actorId}`,
    },
  });
  const body = (await response.json()) as Record<string, unknown>;

  await closeServer(server);

  return {
    status: response.status,
    body,
  };
}

function listenOnEphemeralPort(server: Server): Promise<string> {
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
      warnings: [],
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
      externalCallRequests: [],
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
