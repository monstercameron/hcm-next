import {
  fromPromise,
  fromThrowable,
  idempotencyConflictError,
  notFoundError,
  permissionDeniedError,
  systemError,
  validationFailedError,
  type AppError,
} from "@hcm-next/foundation";
import { useMutation, type UseMutationResult } from "@tanstack/react-query";

/**
 * Input contract for the start-workflow mutation. `subjectType` is optional
 * because some intents (HR-driven flows on `worker` subjects) can be derived
 * server-side from the workflow config; pass it when the renderer knows it,
 * otherwise the server-side service will validate it against the config.
 */
export type StartWorkflowInput = {
  intent: string;
  subjectId: string;
  subjectType?: string;
  input: Record<string, unknown>;
  actorId?: string;
  idempotencyKey?: string;
};

export type StartWorkflowOutput = {
  workflowInstanceId: string;
  currentState: string;
  currentInteraction?: string;
};

export type RunStartWorkflowDeps = {
  fetch: typeof fetch;
  apiBaseUrl?: string;
  /**
   * Returns a SHA-1 hex digest for the given canonical input string. Defaults
   * to the browser's Web Crypto API; tests pass a deterministic stub.
   */
  computeSha1?: (canonical: string) => Promise<string>;
};

type ApiErrorEnvelope = {
  error: {
    code?: unknown;
    message?: unknown;
    details?: unknown;
  };
};

type ApiWorkflowInstanceBody = {
  workflowInstanceId?: unknown;
  state?: unknown;
  currentInteraction?: unknown;
};

const DEFAULT_API_BASE_URL = "/api";
const WORKFLOW_INTENTS_PATH = "/workflow-intents";

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
          : status === 409
            ? (detailRecord: Record<string, unknown>) =>
                idempotencyConflictError(detailRecord)
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

const toHex = (bytes: ArrayBuffer): string => {
  const view = new Uint8Array(bytes);
  let hex = "";
  for (let i = 0; i < view.byteLength; i += 1) {
    const byte = view[i] ?? 0;
    hex += byte.toString(16).padStart(2, "0");
  }
  return hex;
};

/**
 * Default SHA-1 implementation. Pulled out so React tests can inject a stub
 * without polyfilling the global crypto object.
 */
async function defaultSha1(canonical: string): Promise<string> {
  const subtle = globalThis.crypto?.subtle;
  if (subtle === undefined) {
    return Promise.reject(systemError({ reason: "crypto_subtle_unavailable" }));
  }
  const encoded = new TextEncoder().encode(canonical);
  const digestResult = await fromPromise(
    () => subtle.digest("SHA-1", encoded),
    (cause) => systemError({ reason: "sha1_digest_failed" }, cause),
  );
  if (!digestResult.ok) {
    return Promise.reject(digestResult.error);
  }
  return toHex(digestResult.value);
}

const canonicalIdempotencyInput = (input: StartWorkflowInput): string =>
  `${input.intent}|${input.subjectId}|${JSON.stringify(input.input)}`;

/**
 * Pure async core of the hook. Computes the idempotency key, posts the body
 * to `/api/workflow-intents`, and resolves the workflow instance summary. All
 * boundary failures flow through `Promise.reject` as typed `AppError` values.
 */
export async function runStartWorkflow(
  input: StartWorkflowInput,
  deps: RunStartWorkflowDeps,
): Promise<StartWorkflowOutput> {
  const apiBaseUrl = deps.apiBaseUrl ?? DEFAULT_API_BASE_URL;
  const sha1 = deps.computeSha1 ?? defaultSha1;

  const requestHeaders: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (input.actorId !== undefined) {
    requestHeaders["x-demo-actor-id"] = input.actorId;
  }

  const idempotencyKeyResult =
    input.idempotencyKey !== undefined
      ? { ok: true as const, value: input.idempotencyKey }
      : await fromPromise(
          () => sha1(canonicalIdempotencyInput(input)),
          (cause) => systemError({ reason: "idempotency_key_failed" }, cause),
        );
  if (!idempotencyKeyResult.ok) {
    return Promise.reject(idempotencyKeyResult.error);
  }

  const requestBody: Record<string, unknown> = {
    intent: input.intent,
    subjectId: input.subjectId,
    input: input.input,
    idempotencyKey: idempotencyKeyResult.value,
  };
  if (input.subjectType !== undefined) {
    requestBody.subjectType = input.subjectType;
  }

  const responseResult = await fromPromise(
    () =>
      deps.fetch(`${apiBaseUrl}${WORKFLOW_INTENTS_PATH}`, {
        method: "POST",
        headers: requestHeaders,
        body: JSON.stringify(requestBody),
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

  const body = parsedBodyResult.value as ApiWorkflowInstanceBody;
  const workflowInstanceId =
    typeof body.workflowInstanceId === "string" ? body.workflowInstanceId : "";
  const currentState = typeof body.state === "string" ? body.state : "";

  if (workflowInstanceId.length === 0 || currentState.length === 0) {
    return Promise.reject(
      systemError({
        reason: "response_shape_invalid",
        httpStatus: response.status,
      }),
    );
  }

  const output: StartWorkflowOutput = {
    workflowInstanceId,
    currentState,
  };

  if (typeof body.currentInteraction === "object" && body.currentInteraction !== null) {
    const interaction = body.currentInteraction as Record<string, unknown>;
    const interactionKey =
      typeof interaction["interactionKey"] === "string"
        ? (interaction["interactionKey"] as string)
        : typeof interaction["type"] === "string"
          ? (interaction["type"] as string)
          : undefined;
    if (interactionKey !== undefined) {
      output.currentInteraction = interactionKey;
    }
  }

  return output;
}

/**
 * React Query mutation around the workflow-intent start endpoint. Mirrors the
 * `useChat` mutation pattern so callers can read `isPending`, `error`, and
 * `data` without bringing their own state machine.
 */
export function useStartWorkflow(): UseMutationResult<
  StartWorkflowOutput,
  AppError,
  StartWorkflowInput
> {
  return useMutation<StartWorkflowOutput, AppError, StartWorkflowInput>({
    mutationFn: (input) =>
      runStartWorkflow(input, {
        fetch: (...args) => fetch(...args),
      }),
  });
}
