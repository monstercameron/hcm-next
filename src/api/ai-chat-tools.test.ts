import { describe, expect, it } from "vitest";
import {
  createRepositories,
  createSeededDemoStore,
  DEMO_IDS,
} from "@hcm-next/data-store";
import { createNullAiClient } from "@hcm-next/ai-client";
import type { AppDependencies } from "./dependencies.js";
import type { ApiRequestContext } from "./request-context.js";
import {
  AI_CHAT_TOOL_DEFINITIONS,
  executeToolCall,
} from "./ai-chat-tools.js";

type Harness = {
  dependencies: AppDependencies;
  requestContext: ApiRequestContext;
};

function buildHarness(actorIdOverride?: string): Harness {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const actorId = actorIdOverride ?? DEMO_IDS.hrActorId;
  const actorResult = repositories.actors.findById(actorId);
  if (!actorResult.ok) {
    throw new Error(`actor not seeded: ${actorId}`);
  }
  const dependencies: AppDependencies = {
    repositories,
    executorClient: {
      executeBlock<TOutput>() {
        return Promise.resolve({
          ok: true as const,
          value: {
            status: "succeeded" as const,
            output: { valid: true, riskLevel: "low" } as unknown as TOutput,
            proposedEvents: [],
            externalCallRequests: [],
            logs: [],
            metrics: {},
          },
        });
      },
    },
    aiClient: createNullAiClient(),
  };
  const requestContext: ApiRequestContext = {
    actor: actorResult.value,
    tenantId: actorResult.value.tenantId,
    environmentId: DEMO_IDS.environmentId,
    requestId: "req_test",
    correlationId: "corr_test",
  };
  return { dependencies, requestContext };
}

const CONTACT_INFO_INTENT = "employee.contact_info.update";

describe("AI_CHAT_TOOL_DEFINITIONS catalog", () => {
  it("exposes the three tools the agent loop needs", () => {
    const names = AI_CHAT_TOOL_DEFINITIONS.map((tool) => tool.name);
    expect(names).toContain("list_workflows");
    expect(names).toContain("search_employees");
    expect(names).toContain("generate_ui_page");
  });

  it("registers the workflow-lifecycle tools", () => {
    const names = AI_CHAT_TOOL_DEFINITIONS.map((tool) => tool.name);
    expect(names).toContain("get_employee");
    expect(names).toContain("get_workflow_config");
    expect(names).toContain("start_workflow");
    expect(names).toContain("list_workflow_instances");
    expect(names).toContain("get_workflow_instance");
    expect(names).toContain("get_workflow_timeline");
    expect(names).toContain("get_available_actions");
    expect(names).toContain("transition_workflow");
    expect(names).toContain("get_my_tasks");
  });
});

describe("executeToolCall: list_workflows", () => {
  it("returns the registered workflow intents with metadata", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "list_workflows",
      arguments: {},
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      workflows: Array<{ intent: string }>;
    };
    expect(body.workflows.length).toBeGreaterThan(0);
    expect(body.workflows.some((w) => w.intent === "employee.termination")).toBe(
      true,
    );
  });
});

describe("executeToolCall: search_employees", () => {
  it("returns the first 5 employees when no query is supplied", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "search_employees",
      arguments: {},
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      employees: Array<{ id: string; displayName: string }>;
    };
    expect(body.employees.length).toBeGreaterThan(0);
    expect(body.employees.length).toBeLessThanOrEqual(5);
    for (const employee of body.employees) {
      expect(typeof employee.id).toBe("string");
      expect(typeof employee.displayName).toBe("string");
    }
  });

  it("filters by case-insensitive display name substring", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "search_employees",
      arguments: { query: "jane" },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      employees: Array<{ displayName: string }>;
    };
    for (const employee of body.employees) {
      expect(employee.displayName.toLowerCase()).toContain("jane");
    }
  });
});

describe("executeToolCall: generate_ui_page", () => {
  it("returns a confirmation payload and a renderPage side effect on success", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "generate_ui_page",
      arguments: {
        intent: "employee.termination",
        subjectId: DEMO_IDS.employeeId,
      },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.renderPage).toBeDefined();
    const body = result.value.toolResultJson as {
      ok: boolean;
      pageId: string;
      regions: number;
      widgets: number;
    };
    expect(body.ok).toBe(true);
    expect(body.regions).toBeGreaterThan(0);
    expect(typeof body.pageId).toBe("string");
  });

  it("renders form.subjectPicker as the first widget when a subjectId is supplied", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "generate_ui_page",
      arguments: {
        intent: "employee.termination",
        subjectId: DEMO_IDS.employeeId,
      },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const renderPage = result.value.renderPage;
    expect(renderPage).toBeDefined();
    const firstWidget = renderPage?.regions[0]?.widgets[0];
    expect(firstWidget?.type).toBe("form.subjectPicker");
    expect(firstWidget?.props?.["defaultSubjectId"]).toBe(DEMO_IDS.employeeId);
    const options = firstWidget?.props?.["options"] as
      | Array<{ id: string }>
      | undefined;
    expect(options?.length).toBeGreaterThan(0);
    // Picker is capped at 10 candidates from the tenant, plus the active
    // subject if it wasn't already in that slice.
    expect(options?.length).toBeLessThanOrEqual(11);
    expect(options?.some((option) => option.id === DEMO_IDS.employeeId)).toBe(true);
  });

  it("renders form.subjectPicker with options but no default when subjectId is omitted", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "generate_ui_page",
      arguments: { intent: "employee.termination" },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const renderPage = result.value.renderPage;
    expect(renderPage).toBeDefined();
    const firstWidget = renderPage?.regions[0]?.widgets[0];
    expect(firstWidget?.type).toBe("form.subjectPicker");
    expect(firstWidget?.props?.["defaultSubjectId"]).toBeUndefined();
    const options = firstWidget?.props?.["options"] as
      | Array<{ id: string }>
      | undefined;
    expect(Array.isArray(options)).toBe(true);
    expect(options?.length).toBeGreaterThan(0);
  });

  it("returns a serialised tool error when the intent is missing", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "generate_ui_page",
      arguments: { subjectId: DEMO_IDS.employeeId },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("intent_required");
  });

  it("returns a serialised tool error when the workflow is unknown", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "generate_ui_page",
      arguments: { intent: "no.such.intent", subjectId: DEMO_IDS.employeeId },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("workflow_not_found");
  });

  it("returns a serialised tool error when the actor lacks permission", async () => {
    // The plain employee actor cannot start an employee.termination workflow
    // (selfServiceStart is false and the actor does not hold hr_admin).
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    const result = await executeToolCall({
      name: "generate_ui_page",
      arguments: {
        intent: "employee.termination",
        subjectId: DEMO_IDS.employeeId,
      },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("permission_denied");
  });
});

describe("executeToolCall: unknown tool", () => {
  it("returns a serialised tool error", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "definitely_not_a_tool",
      arguments: {},
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("unknown_tool");
  });
});

describe("executeToolCall: get_employee", () => {
  it("returns the employee profile when the actor can read the projection", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_employee",
      arguments: { employeeId: DEMO_IDS.employeeId },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      employee: { id: string; displayName: string; employmentStatus: string };
    };
    expect(body.ok).toBe(true);
    expect(body.employee.id).toBe(DEMO_IDS.employeeId);
    expect(typeof body.employee.displayName).toBe("string");
    expect(typeof body.employee.employmentStatus).toBe("string");
  });

  it("returns a not_found tool error when the employee id is unknown", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_employee",
      arguments: { employeeId: "emp_does_not_exist" },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("not_found");
  });

  it("returns an invalid_arguments tool error when employeeId is missing", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_employee",
      arguments: {},
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("invalid_arguments");
  });
});

describe("executeToolCall: get_workflow_config", () => {
  it("summarises the workflow config when the intent is registered", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_workflow_config",
      arguments: { intent: CONTACT_INFO_INTENT },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      intent: string;
      subjectType: string;
      selfServiceStart: boolean;
      declaredInputs: Array<{ name: string }>;
      states: Array<{ id: string; actions: unknown[] }>;
      approvers: Array<{ assigneeRole: string }>;
    };
    expect(body.ok).toBe(true);
    expect(body.intent).toBe(CONTACT_INFO_INTENT);
    expect(body.subjectType).toBe("worker");
    expect(body.declaredInputs.length).toBeGreaterThan(0);
    expect(body.states.length).toBeGreaterThan(0);
  });

  it("returns a not_found tool error for an unknown intent", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_workflow_config",
      arguments: { intent: "no.such.intent" },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("not_found");
  });

  it("returns an invalid_arguments tool error when intent is missing", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_workflow_config",
      arguments: {},
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("invalid_arguments");
  });
});

describe("executeToolCall: start_workflow", () => {
  it("starts a contact-info update workflow for the employee actor", async () => {
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    const result = await executeToolCall({
      name: "start_workflow",
      arguments: {
        intent: CONTACT_INFO_INTENT,
        subjectId: DEMO_IDS.employeeId,
        input: {},
      },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      workflowInstanceId: string;
      currentState: string;
    };
    expect(body.ok).toBe(true);
    expect(body.workflowInstanceId.length).toBeGreaterThan(0);
    expect(typeof body.currentState).toBe("string");
  });

  it("returns a permission_denied error when the actor cannot start the intent", async () => {
    // employee.compensation.change is published in the demo seed but the plain
    // employee actor lacks the compensation_admin role required to start it.
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    const result = await executeToolCall({
      name: "start_workflow",
      arguments: {
        intent: "employee.compensation.change",
        subjectId: DEMO_IDS.employeeId,
        input: {},
      },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("permission_denied");
  });

  it("returns a not_found error for an unknown intent", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "start_workflow",
      arguments: {
        intent: "no.such.intent",
        subjectId: DEMO_IDS.employeeId,
        input: {},
      },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("not_found");
  });
});

async function seedStartedWorkflow(harness: Harness): Promise<string> {
  const startResult = await executeToolCall({
    name: "start_workflow",
    arguments: {
      intent: CONTACT_INFO_INTENT,
      subjectId: DEMO_IDS.employeeId,
      input: {},
    },
    ...harness,
  });
  if (!startResult.ok) {
    throw new Error("expected start_workflow tool call to succeed");
  }
  const body = startResult.value.toolResultJson as {
    ok: true;
    workflowInstanceId: string;
  };
  return body.workflowInstanceId;
}

describe("executeToolCall: list_workflow_instances", () => {
  it("returns the started instances visible to the actor", async () => {
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    const workflowInstanceId = await seedStartedWorkflow(harness);

    const result = await executeToolCall({
      name: "list_workflow_instances",
      arguments: {},
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      instances: Array<{ id: string; intent: string; subjectId: string }>;
    };
    expect(body.ok).toBe(true);
    expect(body.instances.some((entry) => entry.id === workflowInstanceId)).toBe(true);
  });

  it("returns an empty list when filtered to an unrelated intent", async () => {
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    await seedStartedWorkflow(harness);

    const result = await executeToolCall({
      name: "list_workflow_instances",
      arguments: { intent: "employee.compensation.change" },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      instances: Array<{ id: string }>;
    };
    expect(body.ok).toBe(true);
    expect(body.instances).toHaveLength(0);
  });

  it("returns an empty list for an actor with no workflows assigned", async () => {
    const harness = buildHarness(DEMO_IDS.managerActorId);
    const result = await executeToolCall({
      name: "list_workflow_instances",
      arguments: { assignedToMe: true },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      instances: Array<unknown>;
    };
    expect(body.ok).toBe(true);
    expect(Array.isArray(body.instances)).toBe(true);
  });
});

describe("executeToolCall: get_workflow_instance", () => {
  it("returns the instance summary for a workflow the actor can read", async () => {
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    const workflowInstanceId = await seedStartedWorkflow(harness);

    const result = await executeToolCall({
      name: "get_workflow_instance",
      arguments: { workflowInstanceId },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      id: string;
      intent: string;
      subjectId: string;
    };
    expect(body.ok).toBe(true);
    expect(body.id).toBe(workflowInstanceId);
    expect(body.intent).toBe(CONTACT_INFO_INTENT);
    expect(body.subjectId).toBe(DEMO_IDS.employeeId);
  });

  it("returns a not_found error for an unknown workflow instance", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_workflow_instance",
      arguments: { workflowInstanceId: "wfi_missing" },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("not_found");
  });

  it("returns invalid_arguments when workflowInstanceId is missing", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_workflow_instance",
      arguments: {},
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("invalid_arguments");
  });
});

describe("executeToolCall: get_workflow_timeline", () => {
  it("returns the business timeline for a workflow the actor can read", async () => {
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    const workflowInstanceId = await seedStartedWorkflow(harness);

    const result = await executeToolCall({
      name: "get_workflow_timeline",
      arguments: { workflowInstanceId },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      entries: Array<{ eventType: string; summary: string }>;
      truncated?: boolean;
    };
    expect(body.ok).toBe(true);
    expect(Array.isArray(body.entries)).toBe(true);
  });

  it("returns a not_found error for an unknown workflow instance", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_workflow_timeline",
      arguments: { workflowInstanceId: "wfi_missing" },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("not_found");
  });
});

describe("executeToolCall: get_available_actions", () => {
  it("returns transitions the actor can take", async () => {
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    const workflowInstanceId = await seedStartedWorkflow(harness);

    const result = await executeToolCall({
      name: "get_available_actions",
      arguments: { workflowInstanceId },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      actions: Array<{ transition: string; label: string }>;
    };
    expect(body.ok).toBe(true);
    expect(Array.isArray(body.actions)).toBe(true);
  });

  it("returns invalid_arguments when workflowInstanceId is missing", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "get_available_actions",
      arguments: {},
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("invalid_arguments");
  });
});

describe("executeToolCall: transition_workflow", () => {
  it("returns a not_found tool error when the workflow does not exist", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "transition_workflow",
      arguments: {
        workflowInstanceId: "wfi_missing",
        transition: "approve",
      },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("not_found");
  });

  it("returns invalid_arguments when transition is missing", async () => {
    const harness = buildHarness();
    const result = await executeToolCall({
      name: "transition_workflow",
      arguments: { workflowInstanceId: "wfi_anything" },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(body.error.code).toBe("invalid_arguments");
  });

  it("returns a permission_denied or invalid_transition error when the actor cannot drive it", async () => {
    // The employee actor cannot push approve on a workflow they just started
    // because the state machine has no approve action in the initial state.
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    const workflowInstanceId = await seedStartedWorkflow(harness);

    const result = await executeToolCall({
      name: "transition_workflow",
      arguments: {
        workflowInstanceId,
        transition: "approve",
      },
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: false;
      error: { code: string };
    };
    expect(body.ok).toBe(false);
    expect(["invalid_transition", "permission_denied"]).toContain(body.error.code);
  });
});

describe("executeToolCall: get_my_tasks", () => {
  it("returns the empty task list for actors with nothing pending", async () => {
    const harness = buildHarness(DEMO_IDS.managerActorId);
    const result = await executeToolCall({
      name: "get_my_tasks",
      arguments: {},
      ...harness,
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value.toolResultJson as {
      ok: true;
      tasks: Array<unknown>;
    };
    expect(body.ok).toBe(true);
    expect(Array.isArray(body.tasks)).toBe(true);
  });
});
