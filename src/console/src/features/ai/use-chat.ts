import {
  fromPromise,
  fromThrowable,
  notFoundError,
  permissionDeniedError,
  systemError,
  validationFailedError,
  type AppError,
} from "@hcm-next/foundation";
import type { PageDefinition } from "@hcm-next/ui-contracts";
import { useMutation, type UseMutationResult } from "@tanstack/react-query";

/**
 * One message in the visible chat history. The wire protocol can carry richer
 * structures (system / tool / assistant tool calls) but the UI only renders
 * the user/assistant flow — anything else is internal to the server-side agent
 * loop.
 */
export type ChatMessage = { role: "user" | "assistant"; content: string };

/**
 * Provider attribution returned alongside chat turns. Mirrors the shape the
 * server attaches to its envelope so callers can surface usage metrics.
 */
export type ChatProviderMetadata = {
  provider: string;
  model: string;
  inputTokens: number;
  outputTokens: number;
};

/**
 * Side effects that the agent may attach to its final response. The chat
 * panel pins `renderPage` in the main area via the existing
 * `onGenerated(page, "fresh")` callback; `workflowConfigHash` is preserved
 * for downstream auditability.
 */
export type ChatSideEffects = {
  renderPage?: PageDefinition;
  workflowConfigHash?: string;
};

export type ChatTurnInput = {
  messages: readonly ChatMessage[];
  actorId?: string;
};

export type ChatTurnOutput = {
  assistantMessage: ChatMessage;
  sideEffects: ChatSideEffects;
  providerMetadata?: ChatProviderMetadata;
};

/**
 * Boundary dependencies for `runChatTurn`. Surfaced so the network logic can
 * be exercised without the DOM or a real fetch implementation.
 */
export type RunChatTurnDeps = {
  fetch: typeof fetch;
  apiBaseUrl?: string;
};

type ApiErrorEnvelope = {
  error: {
    code?: unknown;
    message?: unknown;
    details?: unknown;
  };
};

type ApiChatSuccessBody = {
  assistantMessage: { content: string };
  sideEffects?: {
    renderPage?: PageDefinition;
    workflowConfigHash?: string;
  };
  providerMetadata?: ChatProviderMetadata;
};

const DEFAULT_API_BASE_URL = "/api";
const CHAT_PATH = "/ai/chat";

const isApiErrorEnvelope = (value: unknown): value is ApiErrorEnvelope => {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as { error?: unknown };
  if (typeof candidate.error !== "object" || candidate.error === null) {
    return false;
  }
  return true;
};

const isApiChatSuccessBody = (value: unknown): value is ApiChatSuccessBody => {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as Record<string, unknown>;
  const assistantMessage = candidate["assistantMessage"];
  if (
    typeof assistantMessage !== "object" ||
    assistantMessage === null ||
    typeof (assistantMessage as { content?: unknown })["content"] !== "string"
  ) {
    return false;
  }
  return true;
};

const appErrorFromResponse = (status: number, body: unknown): AppError => {
  const envelopeDetails =
    isApiErrorEnvelope(body) &&
    typeof body.error.details === "object" &&
    body.error.details !== null
      ? (body.error.details as Record<string, unknown>)
      : undefined;
  const envelopeMessage =
    isApiErrorEnvelope(body) && typeof body.error.message === "string"
      ? body.error.message
      : undefined;

  const details: Record<string, unknown> = {
    httpStatus: status,
    ...(envelopeDetails ?? {}),
  };

  const factory =
    status === 400
      ? validationFailedError
      : status === 403
        ? permissionDeniedError
        : status === 404
          ? (detailRecord: Record<string, unknown>) =>
              notFoundError("Resource", detailRecord)
          : systemError;

  const error = factory(details);
  if (envelopeMessage !== undefined) {
    return { ...error, safeMessage: envelopeMessage };
  }
  return error;
};

const parseJsonSafe = (text: string) =>
  fromThrowable(
    () => JSON.parse(text) as unknown,
    (cause) => cause,
  );

/**
 * Pure async core of the hook. Sends the chat history to the backend agent
 * route and resolves the assistant's reply plus any side effects (notably a
 * rendered `PageDefinition`). All boundary failures flow through `Promise.reject`
 * as typed `AppError` values.
 */
export async function runChatTurn(
  input: ChatTurnInput,
  deps: RunChatTurnDeps,
): Promise<ChatTurnOutput> {
  const apiBaseUrl = deps.apiBaseUrl ?? DEFAULT_API_BASE_URL;
  const requestHeaders: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (input.actorId !== undefined) {
    requestHeaders["x-demo-actor-id"] = input.actorId;
  }

  const responseResult = await fromPromise(
    () =>
      deps.fetch(`${apiBaseUrl}${CHAT_PATH}`, {
        method: "POST",
        headers: requestHeaders,
        body: JSON.stringify({ messages: input.messages }),
      }),
    (cause) => systemError({ reason: "network_request_failed" }, cause),
  );
  if (!responseResult.ok) {
    return Promise.reject(responseResult.error);
  }
  const response = responseResult.value;

  const bodyTextResult = await fromPromise(
    () => response.text(),
    (cause) => systemError({ reason: "response_read_failed" }, cause),
  );
  if (!bodyTextResult.ok) {
    return Promise.reject(bodyTextResult.error);
  }

  const parsedBodyResult = parseJsonSafe(bodyTextResult.value);

  if (!response.ok) {
    const body = parsedBodyResult.ok ? parsedBodyResult.value : undefined;
    return Promise.reject(appErrorFromResponse(response.status, body));
  }
  if (!parsedBodyResult.ok) {
    return Promise.reject(
      systemError(
        { reason: "response_parse_failed", httpStatus: response.status },
        parsedBodyResult.error,
      ),
    );
  }
  if (!isApiChatSuccessBody(parsedBodyResult.value)) {
    return Promise.reject(
      systemError({
        reason: "response_shape_invalid",
        httpStatus: response.status,
      }),
    );
  }

  const successBody = parsedBodyResult.value;
  const sideEffects: ChatSideEffects = {};
  const renderPage = successBody.sideEffects?.renderPage;
  if (renderPage !== undefined) {
    sideEffects.renderPage = renderPage;
  }
  const workflowConfigHash = successBody.sideEffects?.workflowConfigHash;
  if (workflowConfigHash !== undefined) {
    sideEffects.workflowConfigHash = workflowConfigHash;
  }

  const output: ChatTurnOutput = {
    assistantMessage: {
      role: "assistant",
      content: successBody.assistantMessage.content,
    },
    sideEffects,
  };
  if (successBody.providerMetadata !== undefined) {
    output.providerMetadata = successBody.providerMetadata;
  }
  return output;
}

/**
 * React Query mutation around the chat agent. Chat responses are dynamic by
 * nature so there is no client-side caching — every send hits the server.
 */
export function useChat(): UseMutationResult<
  ChatTurnOutput,
  AppError,
  ChatTurnInput
> {
  return useMutation<ChatTurnOutput, AppError, ChatTurnInput>({
    mutationFn: (input) =>
      runChatTurn(input, {
        fetch: (...args) => fetch(...args),
      }),
  });
}
