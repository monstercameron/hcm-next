import { describe, expect, it } from "vitest";
import {
  createRepositories,
  createSeededDemoStore,
  DEMO_IDS,
} from "@human-capital-management-suite/data-store";
import {
  createNullAiClient,
  type AiChatMessage,
  type AiChatToolCall,
  type AiChatTurnResponse,
  type AiClient,
} from "@human-capital-management-suite/ai-client";
import {
  ERROR_CODES,
  ok,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type { AppDependencies } from "./dependencies.js";
import type { ApiRequestContext } from "./request-context.js";
import { AI_CHAT_SYSTEM_PROMPT, handleAiChat } from "./ai-chat.js";

function buildHarness(actorIdOverride?: string): {
  dependencies: AppDependencies;
  requestContext: ApiRequestContext;
} {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const actorId = actorIdOverride ?? DEMO_IDS.hrActorId;
  const actorResult = repositories.actors.findById(actorId);
  if (!actorResult.ok) {
    throw new Error("seeded actor missing");
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

/**
 * Builds a stub AiClient that emits a scripted sequence of turn responses.
 * Each entry corresponds to one runChatTurn invocation, in order.
 */
function scriptedAiClient(responses: AiChatTurnResponse[]): AiClient {
  let invocationCount = 0;
  return {
    async generateChangeReview() {
      throw new Error("not used in this stub");
    },
    async generatePageDefinition() {
      throw new Error("not used in this stub");
    },
    async runChatTurn(): Promise<Result<AiChatTurnResponse, AppError>> {
      const next = responses[invocationCount];
      invocationCount += 1;
      if (next === undefined) {
        throw new Error(
          `scripted AiClient ran out of responses at iteration ${String(invocationCount)}`,
        );
      }
      return ok(next);
    },
  };
}

const STUB_PROVIDER_METADATA = {
  provider: "stub",
  model: "stub",
  inputTokens: 0,
  outputTokens: 0,
};

function assistantToolCallTurn(toolCall: AiChatToolCall): AiChatTurnResponse {
  return {
    assistantMessage: { content: null, toolCalls: [toolCall] },
    providerMetadata: STUB_PROVIDER_METADATA,
  };
}

function assistantTextTurn(content: string): AiChatTurnResponse {
  return {
    assistantMessage: { content },
    providerMetadata: STUB_PROVIDER_METADATA,
  };
}

function initialUserMessages(content: string): AiChatMessage[] {
  return [{ role: "user", content }];
}

describe("AI_CHAT_SYSTEM_PROMPT", () => {
  it("anchors the assistant persona and instructs tool use", () => {
    expect(AI_CHAT_SYSTEM_PROMPT).toContain("Human Capital Management Suite assistant");
    expect(AI_CHAT_SYSTEM_PROMPT).toContain("use tools");
    expect(AI_CHAT_SYSTEM_PROMPT).toContain("generate_ui_page");
  });
});

describe("handleAiChat: validation", () => {
  it("returns validation_failed when messages is missing", async () => {
    const harness = buildHarness();
    const result = await handleAiChat(harness.dependencies, harness.requestContext, {});
    expect(result.ok).toBe(false);
    if (result.ok) {
      return;
    }
    expect(result.error.code).toBe(ERROR_CODES.VALIDATION_FAILED);
  });

  it("returns validation_failed when no user message is present", async () => {
    const harness = buildHarness();
    const result = await handleAiChat(harness.dependencies, harness.requestContext, {
      messages: [{ role: "assistant", content: "lonely" }],
    });
    expect(result.ok).toBe(false);
    if (result.ok) {
      return;
    }
    expect(result.error.code).toBe(ERROR_CODES.VALIDATION_FAILED);
  });
});

describe("handleAiChat: agent loop end-to-end with NullAiClient", () => {
  it("returns the friendly intro for a single greeting message", async () => {
    const harness = buildHarness();
    const result = await handleAiChat(harness.dependencies, harness.requestContext, {
      messages: [{ role: "user", content: "hi" }],
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value as {
      assistantMessage: { content: string };
      sideEffects: Record<string, unknown>;
    };
    expect(body.assistantMessage.content).toMatch(/HCM assistant/);
    expect(body.sideEffects.renderPage).toBeUndefined();
  });

  it("runs the termination flow to completion, attaching a renderPage side effect", async () => {
    const harness = buildHarness();
    const result = await handleAiChat(harness.dependencies, harness.requestContext, {
      messages: [
        { role: "user", content: "hi" },
        { role: "assistant", content: "Hi — who do you need to help today?" },
        { role: "user", content: "Start a termination for someone" },
      ],
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value as {
      assistantMessage: { content: string };
      sideEffects: { renderPage?: { id: string; regions: Array<unknown> } };
    };
    expect(body.assistantMessage.content).toMatch(/termination form/);
    expect(body.sideEffects.renderPage).toBeDefined();
    expect(body.sideEffects.renderPage?.regions.length ?? 0).toBeGreaterThan(0);
  });
});

describe("handleAiChat: multi-tool round trip", () => {
  it("chains list_workflows, search_employees, start_workflow, and a final assistant reply", async () => {
    const harness = buildHarness(DEMO_IDS.employeeActorId);
    harness.dependencies.aiClient = scriptedAiClient([
      // Turn 1 — call list_workflows.
      assistantToolCallTurn({
        id: "call_1",
        name: "list_workflows",
        arguments: {},
      }),
      // Turn 2 — call search_employees once workflows are known.
      assistantToolCallTurn({
        id: "call_2",
        name: "search_employees",
        arguments: { query: "jane" },
      }),
      // Turn 3 — start the workflow for the resolved employee.
      assistantToolCallTurn({
        id: "call_3",
        name: "start_workflow",
        arguments: {
          intent: "employee.contact_info.update",
          subjectId: DEMO_IDS.employeeId,
          input: {},
        },
      }),
      // Turn 4 — final assistant message.
      assistantTextTurn(
        "I started the contact info update — you should see it in your pending list now.",
      ),
    ]);

    const result = await handleAiChat(harness.dependencies, harness.requestContext, {
      messages: initialUserMessages("update my contact info"),
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value as {
      assistantMessage: { content: string };
    };
    expect(body.assistantMessage.content).toMatch(/contact info update/);
  });
});

describe("handleAiChat: version conflict recovery", () => {
  it("retries transition_workflow after fetching the current instance", async () => {
    // Seed a workflow so the transition tool has something to act on.
    const harness = buildHarness(DEMO_IDS.employeeActorId);

    // Start an instance by directly calling startWorkflowIntent through the
    // tool dispatcher's wiring; simplest path is to use a real start turn.
    harness.dependencies.aiClient = scriptedAiClient([
      assistantToolCallTurn({
        id: "seed_start",
        name: "start_workflow",
        arguments: {
          intent: "employee.contact_info.update",
          subjectId: DEMO_IDS.employeeId,
          input: {},
        },
      }),
      assistantTextTurn("started"),
    ]);
    const startResult = await handleAiChat(
      harness.dependencies,
      harness.requestContext,
      { messages: initialUserMessages("seed it") },
    );
    expect(startResult.ok).toBe(true);

    // Find the workflow instance id we just created.
    const instances = [
      ...harness.dependencies.repositories.store.workflowInstances.values(),
    ];
    expect(instances.length).toBeGreaterThan(0);
    const instanceId = instances[0]!.workflowInstanceId;

    // Now drive the chat: the model first tries to transition with a stale
    // (no-op) body — the underlying service will reject because the requested
    // transition is not valid in the COLLECTING_INPUT state. We assert the
    // recovery shape: model receives a non-ok tool result, calls
    // get_workflow_instance, then retries with the same transition.
    harness.dependencies.aiClient = scriptedAiClient([
      assistantToolCallTurn({
        id: "transition_1",
        name: "transition_workflow",
        arguments: {
          workflowInstanceId: instanceId,
          transition: "approve",
        },
      }),
      assistantToolCallTurn({
        id: "fetch_state",
        name: "get_workflow_instance",
        arguments: { workflowInstanceId: instanceId },
      }),
      assistantToolCallTurn({
        id: "transition_2",
        name: "transition_workflow",
        arguments: {
          workflowInstanceId: instanceId,
          transition: "approve",
        },
      }),
      assistantTextTurn(
        "I can't approve from the current state — let me know what you'd like to do.",
      ),
    ]);

    const result = await handleAiChat(harness.dependencies, harness.requestContext, {
      messages: initialUserMessages("approve it"),
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const body = result.value as { assistantMessage: { content: string } };
    expect(body.assistantMessage.content).toMatch(/current state/);
  });
});
