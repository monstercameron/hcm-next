import { beforeEach, describe, expect, it, vi } from "vitest";

import { ERROR_CODES } from "@hcm-next/foundation";
import type { StructuredLogger } from "@hcm-next/foundation";

import { createOpenAiClient } from "./openai-client.js";
import type { AiGeneratedPageDefinition, AiUiGenerationRequest } from "./ai-client.js";

const { responsesCreateMock, chatCompletionsCreateMock } = vi.hoisted(() => ({
  responsesCreateMock: vi.fn(),
  chatCompletionsCreateMock: vi.fn(),
}));

vi.mock("openai", () => {
  return {
    default: class OpenAIMock {
      public responses = {
        create: responsesCreateMock,
      };
      public chat = {
        completions: {
          create: chatCompletionsCreateMock,
        },
      };
    },
  };
});

function buildRequest(
  overrides: Partial<AiUiGenerationRequest> = {},
): AiUiGenerationRequest {
  const base: AiUiGenerationRequest = {
    workflow: {
      intent: "employee.termination",
      subjectType: "employee",
      submit: {
        proposedSnapshot: {
          terminationDate: { $source: "input", path: "terminationDate" },
          reasonCode: { $source: "input", path: "reasonCode" },
        },
      },
      states: {
        draft: {
          actions: [{ transition: "submit", label: "Submit termination" }],
        },
      },
    },
    workflowConfigHash: "hash-abc",
    currentState: "draft",
    currentInteraction: "termination_intake",
    subject: {
      id: "emp_42",
      displayName: "Jane Rivera",
      jobTitle: "Senior Engineer",
      department: "Platform",
    },
    actor: {
      id: "actor_hrbp",
      displayName: "Riley HRBP",
      roles: ["hr_admin"],
    },
    userPrompt: "Start a termination for Jane",
    correlationId: "corr_1",
    idempotencyKey: "idem_1",
  };
  return { ...base, ...overrides };
}

function validPageDefinitionJson(): AiGeneratedPageDefinition {
  return {
    id: "ai-generated-employee.termination",
    title: "Employee Termination",
    description: "Confirm and submit termination details.",
    workflowTypes: ["employee.termination"],
    surfaceModes: ["full_app"],
    regions: [
      {
        id: "main",
        layout: "stack",
        width: "wide",
        widgets: [
          {
            id: "subject",
            type: "data.recordSummary",
            title: "Jane Rivera",
          },
          {
            id: "actions",
            type: "workflow.actionBar",
            props: {
              actions: [{ transition: "submit", label: "Submit termination" }],
            },
          },
        ],
      },
    ],
  };
}

function buildLogger(): StructuredLogger & {
  info: ReturnType<typeof vi.fn>;
  error: ReturnType<typeof vi.fn>;
  warn: ReturnType<typeof vi.fn>;
  debug: ReturnType<typeof vi.fn>;
} {
  return {
    info: vi.fn(),
    error: vi.fn(),
    warn: vi.fn(),
    debug: vi.fn(),
  } as never;
}

describe("createOpenAiClient.generatePageDefinition", () => {
  beforeEach(() => {
    responsesCreateMock.mockReset();
  });

  it("builds a user prompt containing workflow intent, current state, and subject name", async () => {
    responsesCreateMock.mockResolvedValueOnce({
      output_text: JSON.stringify(validPageDefinitionJson()),
      usage: { input_tokens: 100, output_tokens: 50 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });
    const request = buildRequest();

    const result = await client.generatePageDefinition(request);

    expect(result.ok).toBe(true);
    expect(responsesCreateMock).toHaveBeenCalledTimes(1);
    const callArgs = responsesCreateMock.mock.calls[0]?.[0] as {
      model: string;
      input: Array<{ role: string; content: string }>;
      text: { format: { type: string; name: string; strict: boolean } };
    };
    expect(callArgs.text.format.name).toBe("ai_page_definition");
    // Non-strict mode is intentional: widget `props` is an open-ended object
    // and strict mode requires `additionalProperties: false` on every nested
    // object, which would force enumeration of every widget's props schema.
    expect(callArgs.text.format.strict).toBe(false);
    const userMessage = callArgs.input.find((msg) => msg.role === "user")?.content;
    expect(userMessage).toContain("employee.termination");
    expect(userMessage).toContain("draft");
    expect(userMessage).toContain("Jane Rivera");
    expect(userMessage).toContain("Start a termination for Jane");
  });

  it("includes the employeeOptions list in the user prompt when provided", async () => {
    responsesCreateMock.mockResolvedValueOnce({
      output_text: JSON.stringify(validPageDefinitionJson()),
      usage: { input_tokens: 100, output_tokens: 50 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });
    const request = buildRequest({
      employeeOptions: [
        { id: "emp_1", displayName: "Alice North", jobTitle: "Recruiter" },
        { id: "emp_2", displayName: "Beto South", department: "Finance" },
      ],
    });

    const result = await client.generatePageDefinition(request);

    expect(result.ok).toBe(true);
    const callArgs = responsesCreateMock.mock.calls[0]?.[0] as {
      input: Array<{ role: string; content: string }>;
    };
    const userMessage =
      callArgs.input.find((msg) => msg.role === "user")?.content ?? "";
    expect(userMessage).toContain("Employee options");
    expect(userMessage).toContain("emp_1");
    expect(userMessage).toContain("Alice North");
    expect(userMessage).toContain("Beto South");
    const systemMessage =
      callArgs.input.find((msg) => msg.role === "system")?.content ?? "";
    expect(systemMessage).toContain("form.subjectPicker");
  });

  it("returns Result.err with ai_ui_generation_api_call_failed when OpenAI throws", async () => {
    responsesCreateMock.mockRejectedValueOnce(new Error("network down"));

    const client = createOpenAiClient({ apiKey: "test-key" });

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(false);
    if (result.ok) {
      return;
    }
    expect(result.error.code).toBe(ERROR_CODES.AI_UI_GENERATION_API_CALL_FAILED);
    expect(result.error.safeMessage).toBe(
      "We could not generate the screen. Try again or use the standard view.",
    );
  });

  it("returns Result.err with ai_ui_generation_response_parse_failed for unparseable JSON", async () => {
    responsesCreateMock.mockResolvedValueOnce({
      output_text: "this is not json",
      usage: { input_tokens: 10, output_tokens: 5 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(false);
    if (result.ok) {
      return;
    }
    expect(result.error.code).toBe(ERROR_CODES.AI_UI_GENERATION_RESPONSE_PARSE_FAILED);
  });

  it("returns Result.err with schema_validation when regions are missing", async () => {
    responsesCreateMock.mockResolvedValueOnce({
      output_text: JSON.stringify({
        id: "page",
        title: "Page",
        description: "Page",
        workflowTypes: ["x"],
        surfaceModes: ["full_app"],
        regions: [],
      }),
      usage: { input_tokens: 10, output_tokens: 5 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(false);
    if (result.ok) {
      return;
    }
    expect(result.error.code).toBe(
      ERROR_CODES.AI_UI_GENERATION_SCHEMA_VALIDATION_FAILED,
    );
  });

  it("returns Result.err with schema_validation when a widget uses an unknown type", async () => {
    responsesCreateMock.mockResolvedValueOnce({
      output_text: JSON.stringify({
        id: "page",
        title: "Page",
        description: "Page",
        workflowTypes: ["x"],
        surfaceModes: ["full_app"],
        regions: [
          {
            id: "main",
            layout: "stack",
            width: "wide",
            widgets: [{ id: "w1", type: "not.a.real.widget" }],
          },
        ],
      }),
      usage: { input_tokens: 10, output_tokens: 5 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(false);
    if (result.ok) {
      return;
    }
    expect(result.error.code).toBe(
      ERROR_CODES.AI_UI_GENERATION_SCHEMA_VALIDATION_FAILED,
    );
  });

  it("returns Result.ok with a valid PageDefinition", async () => {
    const page = validPageDefinitionJson();
    responsesCreateMock.mockResolvedValueOnce({
      output_text: JSON.stringify(page),
      usage: { input_tokens: 200, output_tokens: 75 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });

    const result = await client.generatePageDefinition(buildRequest());

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.page.id).toBe(page.id);
    expect(result.value.page.regions.length).toBe(1);
    expect(result.value.page.regions[0]?.widgets[0]?.type).toBe("data.recordSummary");
    expect(result.value.providerMetadata).toEqual({
      provider: "openai",
      model: "gpt-4o",
      inputTokens: 200,
      outputTokens: 75,
    });
  });

  it("logs token counts on success", async () => {
    responsesCreateMock.mockResolvedValueOnce({
      output_text: JSON.stringify(validPageDefinitionJson()),
      usage: { input_tokens: 333, output_tokens: 111 },
    });

    const logger = buildLogger();
    const client = createOpenAiClient({ apiKey: "test-key" });

    const result = await client.generatePageDefinition(buildRequest(), logger);

    expect(result.ok).toBe(true);
    const completionCall = logger.info.mock.calls.find(
      (call) => call[0] === "ai ui generation completed",
    );
    expect(completionCall).toBeDefined();
    const context = completionCall?.[1] as Record<string, unknown> | undefined;
    expect(context?.["inputTokens"]).toBe(333);
    expect(context?.["outputTokens"]).toBe(111);
  });
});

describe("createOpenAiClient.runChatTurn", () => {
  beforeEach(() => {
    chatCompletionsCreateMock.mockReset();
  });

  it("forwards system prompt, conversation history and tools to chat.completions.create", async () => {
    chatCompletionsCreateMock.mockResolvedValueOnce({
      choices: [{ message: { content: "Hi there", tool_calls: undefined } }],
      usage: { prompt_tokens: 42, completion_tokens: 7 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });
    const result = await client.runChatTurn({
      systemPrompt: "You are the HCM Next assistant.",
      messages: [{ role: "user", content: "hi" }],
      tools: [
        {
          name: "list_workflows",
          description: "list",
          parametersJsonSchema: {
            type: "object",
            properties: {},
            additionalProperties: false,
          },
        },
      ],
      correlationId: "corr_chat",
      idempotencyKey: "idem_chat",
    });

    expect(result.ok).toBe(true);
    expect(chatCompletionsCreateMock).toHaveBeenCalledTimes(1);
    const args = chatCompletionsCreateMock.mock.calls[0]?.[0] as {
      model: string;
      messages: Array<{ role: string; content?: string }>;
      tools?: Array<{ type: string; function: { name: string } }>;
      tool_choice?: string;
    };
    expect(args.model).toBe("gpt-4o");
    expect(args.messages[0]).toEqual({
      role: "system",
      content: "You are the HCM Next assistant.",
    });
    expect(args.messages[1]).toEqual({ role: "user", content: "hi" });
    expect(args.tools?.[0]?.function.name).toBe("list_workflows");
    expect(args.tool_choice).toBe("auto");
  });

  it("returns a final assistant text reply when there are no tool calls", async () => {
    chatCompletionsCreateMock.mockResolvedValueOnce({
      choices: [{ message: { content: "All set." } }],
      usage: { prompt_tokens: 5, completion_tokens: 3 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });
    const result = await client.runChatTurn({
      systemPrompt: "sp",
      messages: [{ role: "user", content: "hi" }],
      tools: [],
      correlationId: "c1",
      idempotencyKey: "i1",
    });

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.assistantMessage.content).toBe("All set.");
    expect(result.value.assistantMessage.toolCalls).toBeUndefined();
    expect(result.value.providerMetadata).toEqual({
      provider: "openai",
      model: "gpt-4o",
      inputTokens: 5,
      outputTokens: 3,
    });
  });

  it("parses tool_calls and converts JSON arguments to objects", async () => {
    chatCompletionsCreateMock.mockResolvedValueOnce({
      choices: [
        {
          message: {
            content: null,
            tool_calls: [
              {
                id: "call_1",
                type: "function",
                function: {
                  name: "search_employees",
                  arguments: JSON.stringify({ query: "Jane" }),
                },
              },
            ],
          },
        },
      ],
      usage: { prompt_tokens: 10, completion_tokens: 12 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });
    const result = await client.runChatTurn({
      systemPrompt: "sp",
      messages: [{ role: "user", content: "find jane" }],
      tools: [],
      correlationId: "c1",
      idempotencyKey: "i1",
    });

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    expect(result.value.assistantMessage.content).toBeNull();
    expect(result.value.assistantMessage.toolCalls).toEqual([
      {
        id: "call_1",
        name: "search_employees",
        arguments: { query: "Jane" },
      },
    ]);
  });

  it("returns Result.err with chat_turn stage when the provider throws", async () => {
    chatCompletionsCreateMock.mockRejectedValueOnce(new Error("network down"));

    const client = createOpenAiClient({ apiKey: "test-key" });
    const result = await client.runChatTurn({
      systemPrompt: "sp",
      messages: [{ role: "user", content: "hi" }],
      tools: [],
      correlationId: "c1",
      idempotencyKey: "i1",
    });

    expect(result.ok).toBe(false);
    if (result.ok) {
      return;
    }
    expect(result.error.details?.["stage"]).toBe("chat_turn");
  });

  it("round-trips assistant tool calls and tool results back to the wire format", async () => {
    chatCompletionsCreateMock.mockResolvedValueOnce({
      choices: [{ message: { content: "Done." } }],
      usage: { prompt_tokens: 1, completion_tokens: 1 },
    });

    const client = createOpenAiClient({ apiKey: "test-key" });
    await client.runChatTurn({
      systemPrompt: "sp",
      messages: [
        { role: "user", content: "hi" },
        {
          role: "assistant",
          content: null,
          toolCalls: [{ id: "call_a", name: "list_workflows", arguments: {} }],
        },
        {
          role: "tool",
          toolCallId: "call_a",
          content: JSON.stringify({ workflows: [] }),
        },
      ],
      tools: [],
      correlationId: "c1",
      idempotencyKey: "i1",
    });

    const args = chatCompletionsCreateMock.mock.calls[0]?.[0] as {
      messages: Array<Record<string, unknown>>;
    };
    expect(args.messages[2]).toMatchObject({
      role: "assistant",
      content: null,
      tool_calls: [
        {
          id: "call_a",
          type: "function",
          function: { name: "list_workflows", arguments: "{}" },
        },
      ],
    });
    expect(args.messages[3]).toEqual({
      role: "tool",
      tool_call_id: "call_a",
      content: JSON.stringify({ workflows: [] }),
    });
  });
});
