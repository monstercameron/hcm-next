import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import {
  ERROR_CODES,
  WORKFLOW_INTENTS,
  err,
  ok,
  aiUiGenerationError,
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
import type {
  AiChangeReview,
  AiClient,
  AiUiGenerationRequest,
  AiUiGenerationResult,
} from "@human-capital-management-suite/ai-client";
import type { AppDependencies } from "../../api/dependencies.js";
import type { ExecutorClient, ExecutorResponse } from "../../api/executor-client.js";
import { createApiServer } from "../../api/server.js";
import type { Server } from "node:http";
import type { AddressInfo } from "node:net";

type StubAiClient = AiClient & {
  readonly calls: AiUiGenerationRequest[];
};

type ApiUnderTest = {
  origin: string;
  close: () => Promise<void>;
};

type Harness = {
  dependencies: AppDependencies;
  repositories: Repositories;
  aiClient: StubAiClient;
  hrActor: ActorRecord;
  employeeActor: ActorRecord;
  api: ApiUnderTest;
};

const TERMINATION_INTENT = WORKFLOW_INTENTS.EMPLOYEE_TERMINATION;

const STUB_AI_RESULT: AiUiGenerationResult = {
  page: {
    id: "ai-generated-employee.termination",
    title: "Employee Termination",
    description: "Assistant view for Employee Termination.",
    workflowTypes: [TERMINATION_INTENT],
    surfaceModes: ["full_app"],
    regions: [
      {
        id: "main",
        layout: "stack",
        width: "wide",
        widgets: [
          {
            id: "submit-fields-callout",
            type: "content.callout",
            title: "Required information",
          },
        ],
      },
    ],
  },
  providerMetadata: {
    provider: "stub",
    model: "stub-model",
    inputTokens: 100,
    outputTokens: 250,
  },
};

describe("POST /api/ai/generate-ui", () => {
  let harness: Harness;

  beforeEach(async () => {
    harness = await createHarness();
  });

  afterEach(async () => {
    await harness.api.close();
  });

  it("returns 400 when workflowIntent is empty", async () => {
    const response = await postGenerateUi(harness, harness.hrActor, {
      workflowIntent: "",
      userPrompt: "Start a termination for Jane.",
    });

    expect(response.status).toBe(400);
    expect(errorCode(response)).toBe(ERROR_CODES.VALIDATION_FAILED);
    expect(harness.aiClient.calls.length).toBe(0);
  });

  it("returns 400 when userPrompt is empty", async () => {
    const response = await postGenerateUi(harness, harness.hrActor, {
      workflowIntent: TERMINATION_INTENT,
      userPrompt: "   ",
    });

    expect(response.status).toBe(400);
    expect(errorCode(response)).toBe(ERROR_CODES.VALIDATION_FAILED);
    expect(harness.aiClient.calls.length).toBe(0);
  });

  it("returns 400 when userPrompt exceeds 2000 chars", async () => {
    const response = await postGenerateUi(harness, harness.hrActor, {
      workflowIntent: TERMINATION_INTENT,
      userPrompt: "x".repeat(2001),
    });

    expect(response.status).toBe(400);
    expect(errorCode(response)).toBe(ERROR_CODES.VALIDATION_FAILED);
    expect(harness.aiClient.calls.length).toBe(0);
  });

  it("returns 404 when workflow intent does not exist", async () => {
    const response = await postGenerateUi(harness, harness.hrActor, {
      workflowIntent: "employee.nonexistent",
      userPrompt: "Generate something useful.",
    });

    expect(response.status).toBe(404);
    expect(errorCode(response)).toBe(ERROR_CODES.NOT_FOUND);
    expect(harness.aiClient.calls.length).toBe(0);
  });

  it("returns 403 when actor lacks intent-start permission", async () => {
    const response = await postGenerateUi(harness, harness.employeeActor, {
      workflowIntent: TERMINATION_INTENT,
      subjectId: DEMO_IDS.employeeId,
      userPrompt: "Generate the termination view for me.",
    });

    expect(response.status).toBe(403);
    expect(errorCode(response)).toBe(ERROR_CODES.PERMISSION_DENIED);
    expect(harness.aiClient.calls.length).toBe(0);
  });

  it("returns 200 with the generated page when the AI client succeeds", async () => {
    const response = await postGenerateUi(harness, harness.hrActor, {
      workflowIntent: TERMINATION_INTENT,
      subjectId: DEMO_IDS.employeeId,
      currentState: "collecting_input",
      userPrompt: "Start a termination for Jane Rivera.",
    });

    expect(response.status).toBe(200);
    const body = response.body as Record<string, unknown>;
    expect(body).toMatchObject({
      page: { id: "ai-generated-employee.termination" },
      providerMetadata: { provider: "stub", model: "stub-model" },
    });
    expect(typeof body["workflowConfigHash"]).toBe("string");
    expect(String(body["workflowConfigHash"])).toMatch(/^sha1:[a-f0-9]{40}$/);

    expect(harness.aiClient.calls.length).toBe(1);
    const call = harness.aiClient.calls[0]!;
    expect(call.workflow.intent).toBe(TERMINATION_INTENT);
    expect(call.workflowConfigHash).toBe(body["workflowConfigHash"]);
    expect(call.currentState).toBe("collecting_input");
    expect(call.subject?.id).toBe(DEMO_IDS.employeeId);
    expect(call.subject?.displayName.length).toBeGreaterThan(0);
    expect(call.actor.id).toBe(harness.hrActor.actorId);
    expect(call.actor.roles).toContain("hr_admin");
    expect(call.userPrompt).toBe("Start a termination for Jane Rivera.");
    expect(call.idempotencyKey).toMatch(/^ai-generate-ui:[a-f0-9]{40}$/);
    expect(call.correlationId.length).toBeGreaterThan(0);
  });

  it("derives identical idempotency keys for identical requests", async () => {
    await postGenerateUi(harness, harness.hrActor, {
      workflowIntent: TERMINATION_INTENT,
      subjectId: DEMO_IDS.employeeId,
      currentState: "collecting_input",
      userPrompt: "Start a termination for Jane Rivera.",
    });
    await postGenerateUi(harness, harness.hrActor, {
      workflowIntent: TERMINATION_INTENT,
      subjectId: DEMO_IDS.employeeId,
      currentState: "collecting_input",
      userPrompt: "Start a termination for Jane Rivera.",
    });

    expect(harness.aiClient.calls.length).toBe(2);
    expect(harness.aiClient.calls[0]!.idempotencyKey).toBe(
      harness.aiClient.calls[1]!.idempotencyKey,
    );
  });

  it("returns a system error when the AI client returns Result.err", async () => {
    harness.aiClient.generatePageDefinition = vi.fn(async () =>
      err(aiUiGenerationError({ stage: "api_call" })),
    );

    const response = await postGenerateUi(harness, harness.hrActor, {
      workflowIntent: TERMINATION_INTENT,
      subjectId: DEMO_IDS.employeeId,
      userPrompt: "Start a termination.",
    });

    expect(response.status).toBe(500);
    expect(errorCode(response)).toBe(ERROR_CODES.AI_UI_GENERATION_API_CALL_FAILED);
  });

  it("returns a system error when aiClient is not configured", async () => {
    await harness.api.close();
    const dependenciesWithoutAi: AppDependencies = {
      ...harness.dependencies,
    };
    delete dependenciesWithoutAi.aiClient;
    harness.api = await startApiServer(dependenciesWithoutAi);

    const response = await postGenerateUi(harness, harness.hrActor, {
      workflowIntent: TERMINATION_INTENT,
      subjectId: DEMO_IDS.employeeId,
      userPrompt: "Start a termination.",
    });

    expect(response.status).toBe(500);
    expect(errorCode(response)).toBe(ERROR_CODES.SYSTEM_ERROR);
  });
});

function createStubAiClient(
  result: AiUiGenerationResult = STUB_AI_RESULT,
): StubAiClient {
  const calls: AiUiGenerationRequest[] = [];
  const stub: StubAiClient = {
    calls,
    async generatePageDefinition(
      request: AiUiGenerationRequest,
    ): Promise<Result<AiUiGenerationResult, AppError>> {
      calls.push(request);
      return ok(result);
    },
    async generateChangeReview(): Promise<Result<AiChangeReview, AppError>> {
      return ok({
        summary: "stub",
        riskLevel: "low",
        missingData: [],
        policyFlags: [],
        downstreamImpact: [],
        recommendedNextAction: "noop",
        providerMetadata: {
          provider: "stub",
          model: "stub-model",
          inputTokens: 0,
          outputTokens: 0,
        },
      });
    },
    async runChatTurn() {
      return ok({
        assistantMessage: { content: "stub" },
        providerMetadata: {
          provider: "stub",
          model: "stub-model",
          inputTokens: 0,
          outputTokens: 0,
        },
      });
    },
  };
  return stub;
}

function createFakeExecutorClient(): ExecutorClient {
  return {
    executeBlock<TOutput>(): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      return Promise.resolve(
        ok({
          status: "succeeded",
          proposedEvents: [],
          externalCallRequests: [],
          logs: [],
          metrics: {},
        } as ExecutorResponse<TOutput>),
      );
    },
  };
}

async function createHarness(): Promise<Harness> {
  const store = createSeededDemoStore();
  const repositories = createRepositories(store);
  const aiClient = createStubAiClient();
  const dependencies: AppDependencies = {
    repositories,
    executorClient: createFakeExecutorClient(),
    aiClient,
  };
  const api = await startApiServer(dependencies);

  return {
    dependencies,
    repositories,
    aiClient,
    api,
    hrActor: mustActor(repositories, DEMO_IDS.hrActorId),
    employeeActor: mustActor(repositories, DEMO_IDS.employeeActorId),
  };
}

function mustActor(repositories: Repositories, actorId: string): ActorRecord {
  const actorResult = repositories.actors.findById(actorId);
  if (!actorResult.ok) {
    throw new Error(`Missing seeded actor ${actorId}`);
  }
  return actorResult.value;
}

async function startApiServer(dependencies: AppDependencies): Promise<ApiUnderTest> {
  const server = createApiServer(dependencies);
  const origin = await listenOnEphemeralPort(server);
  return {
    origin,
    close: () => closeServer(server),
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

type GenerateUiResponse = {
  status: number;
  body: unknown;
};

async function postGenerateUi(
  harness: Harness,
  actor: ActorRecord,
  body: Record<string, unknown>,
): Promise<GenerateUiResponse> {
  const response = await fetch(`${harness.api.origin}/api/ai/generate-ui`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-demo-actor-id": actor.actorId,
      "x-request-id": `req_${actor.actorId}_${Date.now()}`,
    },
    body: JSON.stringify(body),
  });
  const responseBody = (await response.json()) as unknown;
  return { status: response.status, body: responseBody };
}

function errorCode(response: GenerateUiResponse): string | undefined {
  if (typeof response.body !== "object" || response.body === null) {
    return undefined;
  }
  const errorBlock = (response.body as Record<string, unknown>)["error"];
  if (typeof errorBlock !== "object" || errorBlock === null) {
    return undefined;
  }
  const code = (errorBlock as Record<string, unknown>)["code"];
  return typeof code === "string" ? code : undefined;
}
