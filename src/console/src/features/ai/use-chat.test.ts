import { describe, expect, it, vi } from "vitest";
import { ERROR_CODES, type AppError } from "@hcm-next/foundation";
import type { PageDefinition } from "@hcm-next/ui-contracts";
import { useMutation, type UseMutationResult } from "@tanstack/react-query";
import {
  runChatTurn,
  useChat,
  type ChatTurnInput,
  type ChatTurnOutput,
  type RunChatTurnDeps,
} from "./use-chat.js";

const buildPage = (id: string): PageDefinition => ({
  id,
  title: `Page ${id}`,
  description: "Generated for tests.",
  workflowTypes: ["employee.termination"],
  surfaceModes: ["full_app"],
  regions: [{ id: "main", layout: "stack", width: "content", widgets: [] }],
});

const jsonResponse = (status: number, body: unknown): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

const baseInput: ChatTurnInput = {
  messages: [{ role: "user", content: "hi" }],
};

const makeDeps = (
  fetchImpl: typeof fetch,
): { deps: RunChatTurnDeps; fetchMock: ReturnType<typeof vi.fn> } => {
  const fetchMock = vi.fn(fetchImpl);
  const deps: RunChatTurnDeps = {
    fetch: fetchMock as unknown as typeof fetch,
  };
  return { deps, fetchMock };
};

describe("runChatTurn: success path", () => {
  it("returns the assistant message and side effects when the server responds 200", async () => {
    const page = buildPage("fresh-1");
    const fetchImpl: typeof fetch = async () =>
      jsonResponse(200, {
        assistantMessage: { content: "Hi there." },
        sideEffects: { renderPage: page, workflowConfigHash: "h1" },
        providerMetadata: {
          provider: "openai",
          model: "gpt-4o",
          inputTokens: 1,
          outputTokens: 2,
        },
      });
    const { deps, fetchMock } = makeDeps(fetchImpl);

    const result = await runChatTurn(baseInput, deps);

    expect(result.assistantMessage).toEqual({
      role: "assistant",
      content: "Hi there.",
    });
    expect(result.sideEffects.renderPage).toEqual(page);
    expect(result.sideEffects.workflowConfigHash).toBe("h1");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/ai/chat");
  });

  it("sends the messages array in the request body and includes the actor id header when supplied", async () => {
    const fetchImpl: typeof fetch = async () =>
      jsonResponse(200, {
        assistantMessage: { content: "ok" },
      });
    const { deps, fetchMock } = makeDeps(fetchImpl);

    await runChatTurn({ messages: baseInput.messages, actorId: "actor_42" }, deps);

    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(JSON.parse(String(init.body))).toEqual({
      messages: [{ role: "user", content: "hi" }],
    });
    expect((init.headers as Record<string, string>)["x-demo-actor-id"]).toBe(
      "actor_42",
    );
  });
});

describe("runChatTurn: HTTP error mapping", () => {
  const expectReject = async (
    status: number,
    expectedCode: string,
  ): Promise<AppError> => {
    const fetchImpl: typeof fetch = async () =>
      jsonResponse(status, {
        error: { code: "ignored", message: "Safe message", details: { x: 1 } },
      });
    const { deps } = makeDeps(fetchImpl);

    const promise = runChatTurn(baseInput, deps);
    await expect(promise).rejects.toMatchObject({ code: expectedCode });
    return promise.then(
      (): AppError => {
        throw new Error("expected rejection");
      },
      (error: AppError) => error,
    );
  };

  it("maps HTTP 400 to validationFailedError", async () => {
    const error = await expectReject(400, ERROR_CODES.VALIDATION_FAILED);
    expect(error.safeMessage).toBe("Safe message");
  });

  it("maps HTTP 500 to systemError", async () => {
    const error = await expectReject(500, ERROR_CODES.SYSTEM_ERROR);
    expect(error.details).toMatchObject({ httpStatus: 500 });
  });
});

describe("runChatTurn: failure modes", () => {
  it("rejects with systemError when fetch throws", async () => {
    const fetchImpl: typeof fetch = async () => {
      throw new TypeError("network down");
    };
    const { deps } = makeDeps(fetchImpl);

    await expect(runChatTurn(baseInput, deps)).rejects.toMatchObject({
      code: ERROR_CODES.SYSTEM_ERROR,
      details: { reason: "network_request_failed" },
    });
  });

  it("rejects with systemError when the response shape is invalid", async () => {
    const fetchImpl: typeof fetch = async () => jsonResponse(200, { unexpected: true });
    const { deps } = makeDeps(fetchImpl);

    await expect(runChatTurn(baseInput, deps)).rejects.toMatchObject({
      code: ERROR_CODES.SYSTEM_ERROR,
      details: { reason: "response_shape_invalid" },
    });
  });
});

describe("useChat: hook surface", () => {
  it("exposes a UseMutationResult parameterised on the chat types", () => {
    const hookRef: () => UseMutationResult<ChatTurnOutput, AppError, ChatTurnInput> =
      useChat;
    expect(typeof hookRef).toBe("function");
    expect(typeof useMutation).toBe("function");
  });
});
