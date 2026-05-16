import { createHash, randomUUID } from "node:crypto";
import {
  err,
  ok,
  systemError,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  AiChatMessage,
  AiChatToolCall,
  AiChatTurnRequest,
  AiChatTurnResponse,
  AiGeneratedPageDefinition,
} from "@hcm-next/ai-client";
import type { AppDependencies } from "./dependencies.js";
import type { ApiRequestContext } from "./request-context.js";
import {
  AI_CHAT_TOOL_DEFINITIONS,
  executeToolCall,
  type ToolExecutionSideEffects,
} from "./ai-chat-tools.js";

const MAX_AGENT_ITERATIONS = 10;

/**
 * System prompt that anchors the assistant's persona and rules. Verbatim text
 * is part of the contract — changes ripple through the tests.
 */
export const AI_CHAT_SYSTEM_PROMPT = `You are the HCM Next assistant. You help HR coordinators complete employee
workflows (terminations, org changes, contact updates, approvals).

Capabilities — you can:
- List the workflows available for the user.
- Look up employees by name or ID.
- Show what's currently pending — workflows in progress and tasks assigned to the user.
- Inspect a specific workflow: its current state, its history, and what actions are available next.
- Render the intake form for a workflow when the user is ready to start.
- Submit a workflow on the user's behalf when they've given you the required inputs.
- Move a workflow forward — approve, reject, or execute the next step — after confirming with the user.

Behaviour rules:
- Always confirm destructive actions (start, approve, reject, execute) with the user BEFORE calling the tool.
- When the user names a workflow or employee, look it up first to confirm before taking action.
- If a tool returns ok: false, explain the problem to the user in plain English and suggest a next step.
- When the form has rendered in the main area, tell the user it's there and stop — let them complete it.
- When you start or transition a workflow, tell the user the new state and the next step they (or someone else) needs to take.
- When the user asks to start a workflow, call generate_ui_page immediately even if they haven't named a specific employee. Do NOT ask them to pick by chat — the form includes an employee picker at the top that they'll use visually.
- Only call search_employees when the user has explicitly named someone and you need to disambiguate (e.g. they said "Jane" and there are multiple Janes).

Rules:
- You must use tools to look up real data. Never invent employee names, IDs,
  or workflow details from memory.
- When the user is ready to act on a workflow, call generate_ui_page. The
  form will appear in the main area of the screen — tell the user to fill it
  in. The page includes an employee picker at the top, so a specific subject
  is optional.
- Ask clarifying questions when the request is ambiguous. Do not assume.
- Voice: practical and brief. Talk like a colleague, not a chatbot.
- Never mention JSON, tools, models, or implementation details.`;

/**
 * Handles `POST /api/ai/chat`. Runs the OpenAI tool-calling agent loop on the
 * server: each iteration calls the configured `AiClient.runChatTurn`, executes
 * any tool calls, appends the results as `tool` messages, and loops until the
 * assistant returns a final text reply or the iteration budget is exhausted.
 *
 * Returns the final assistant text, any side effects accrued during the loop
 * (notably a `renderPage` PageDefinition the panel pins in the main area),
 * and the provider metadata for billing attribution.
 */
export async function handleAiChat(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Promise<Result<Record<string, unknown>, AppError>> {
  const parseResult = parseChatRequest(body);
  if (!parseResult.ok) {
    return parseResult;
  }
  const initialMessages = parseResult.value;

  if (dependencies.aiClient === undefined) {
    return err(systemError({ reason: "ai_client_not_configured" }));
  }
  const aiClient = dependencies.aiClient;

  dependencies.logger?.info("ai chat turn requested", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    correlationId: requestContext.correlationId,
    messageCount: initialMessages.length,
  });

  const idempotencyKey = idempotencyKeyForChatRequest({
    actorId: requestContext.actor.actorId,
    messages: initialMessages,
  });

  const workingMessages: AiChatMessage[] = [...initialMessages];
  const accumulatedSideEffects: {
    renderPage?: AiGeneratedPageDefinition;
    workflowConfigHash?: string;
  } = {};
  let lastResponse: AiChatTurnResponse | undefined;

  for (let iteration = 0; iteration < MAX_AGENT_ITERATIONS; iteration++) {
    const turnRequest: AiChatTurnRequest = {
      systemPrompt: AI_CHAT_SYSTEM_PROMPT,
      messages: workingMessages,
      tools: AI_CHAT_TOOL_DEFINITIONS,
      correlationId: requestContext.correlationId,
      idempotencyKey: `${idempotencyKey}:${iteration}`,
    };

    const turnResult = await aiClient.runChatTurn(turnRequest, dependencies.logger);
    if (!turnResult.ok) {
      return turnResult;
    }
    lastResponse = turnResult.value;
    const assistantMessage = turnResult.value.assistantMessage;
    const toolCalls = assistantMessage.toolCalls ?? [];

    if (toolCalls.length === 0) {
      // Final assistant text — break out.
      workingMessages.push({
        role: "assistant",
        content: assistantMessage.content,
      });
      return ok(
        buildChatResponseBody({
          assistantContent: assistantMessage.content ?? "",
          sideEffects: accumulatedSideEffects,
          providerMetadata: turnResult.value.providerMetadata,
        }),
      );
    }

    // Record the assistant turn that requested the tool calls so the next
    // provider call sees the same conversation history we'll have locally.
    workingMessages.push({
      role: "assistant",
      content: assistantMessage.content,
      toolCalls,
    });

    for (const toolCall of toolCalls) {
      const execution = await executeToolCall({
        name: toolCall.name,
        arguments: toolCall.arguments,
        dependencies,
        requestContext,
      });
      if (!execution.ok) {
        return execution;
      }
      mergeSideEffects(accumulatedSideEffects, execution.value);
      workingMessages.push(toolMessageForResult(toolCall, execution.value));
    }
  }

  dependencies.logger?.warn("ai chat agent iteration budget exhausted", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    correlationId: requestContext.correlationId,
    iterations: MAX_AGENT_ITERATIONS,
  });

  return ok(
    buildChatResponseBody({
      assistantContent:
        "I'm having trouble finishing this thought. Could you rephrase what you need?",
      sideEffects: accumulatedSideEffects,
      providerMetadata: lastResponse?.providerMetadata ?? {
        provider: "unknown",
        model: "unknown",
        inputTokens: 0,
        outputTokens: 0,
      },
    }),
  );
}

function buildChatResponseBody(input: {
  assistantContent: string;
  sideEffects: {
    renderPage?: AiGeneratedPageDefinition;
    workflowConfigHash?: string;
  };
  providerMetadata: AiChatTurnResponse["providerMetadata"];
}): Record<string, unknown> {
  const sideEffectsBody: Record<string, unknown> = {};
  if (input.sideEffects.renderPage !== undefined) {
    sideEffectsBody["renderPage"] = input.sideEffects.renderPage;
  }
  if (input.sideEffects.workflowConfigHash !== undefined) {
    sideEffectsBody["workflowConfigHash"] = input.sideEffects.workflowConfigHash;
  }
  return {
    assistantMessage: { content: input.assistantContent },
    sideEffects: sideEffectsBody,
    providerMetadata: input.providerMetadata,
  };
}

function mergeSideEffects(
  accumulator: {
    renderPage?: AiGeneratedPageDefinition;
    workflowConfigHash?: string;
  },
  next: ToolExecutionSideEffects,
): void {
  if (next.renderPage !== undefined) {
    accumulator.renderPage = next.renderPage;
  }
  if (next.workflowConfigHash !== undefined) {
    accumulator.workflowConfigHash = next.workflowConfigHash;
  }
}

function toolMessageForResult(
  toolCall: AiChatToolCall,
  sideEffects: ToolExecutionSideEffects,
): AiChatMessage {
  return {
    role: "tool",
    toolCallId: toolCall.id,
    content: JSON.stringify(sideEffects.toolResultJson ?? {}),
  };
}

/**
 * Parses the incoming chat request body. The wire format mirrors what the
 * frontend `useChat` hook sends.
 */
function parseChatRequest(
  body: Record<string, unknown>,
): Result<AiChatMessage[], AppError> {
  const rawMessages = body["messages"];
  if (!Array.isArray(rawMessages)) {
    return err(
      validationFailedError({ field: "messages", reason: "messages_array_required" }),
    );
  }

  const messages: AiChatMessage[] = [];
  for (let index = 0; index < rawMessages.length; index++) {
    const candidate = rawMessages[index];
    const messageResult = readWireChatMessage(candidate, index);
    if (!messageResult.ok) {
      return messageResult;
    }
    messages.push(messageResult.value);
  }

  const hasUserMessage = messages.some((message) => message.role === "user");
  if (!hasUserMessage) {
    return err(
      validationFailedError({
        field: "messages",
        reason: "at_least_one_user_message_required",
      }),
    );
  }

  return ok(messages);
}

function readWireChatMessage(
  candidate: unknown,
  index: number,
): Result<AiChatMessage, AppError> {
  if (typeof candidate !== "object" || candidate === null) {
    return err(
      validationFailedError({
        field: `messages[${index}]`,
        reason: "message_must_be_object",
      }),
    );
  }
  const record = candidate as Record<string, unknown>;
  const role = record["role"];

  if (role === "user" || role === "assistant") {
    const content = record["content"];
    if (typeof content !== "string") {
      return err(
        validationFailedError({
          field: `messages[${index}].content`,
          reason: "content_must_be_string",
        }),
      );
    }
    return ok({ role, content });
  }

  if (role === "system") {
    const content = record["content"];
    if (typeof content !== "string") {
      return err(
        validationFailedError({
          field: `messages[${index}].content`,
          reason: "content_must_be_string",
        }),
      );
    }
    return ok({ role: "system", content });
  }

  if (role === "tool") {
    const toolCallId = record["toolCallId"];
    const content = record["content"];
    if (typeof toolCallId !== "string" || typeof content !== "string") {
      return err(
        validationFailedError({
          field: `messages[${index}]`,
          reason: "tool_message_shape_invalid",
        }),
      );
    }
    return ok({ role: "tool", toolCallId, content });
  }

  return err(
    validationFailedError({
      field: `messages[${index}].role`,
      reason: "unknown_role",
    }),
  );
}

function idempotencyKeyForChatRequest(input: {
  actorId: string;
  messages: readonly AiChatMessage[];
}): string {
  const canonical = JSON.stringify({
    actorId: input.actorId,
    messages: input.messages.map((message) => ({
      role: message.role,
      content: "content" in message ? message.content : null,
    })),
    nonce: randomUUID(),
  });
  return createHash("sha1").update(canonical).digest("hex");
}
