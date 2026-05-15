import type { AppError } from "../errors";
import { goExecutorError } from "../errors";
import { fromPromise, type Result } from "../result";
import {
  GO_EXECUTOR_STATUS,
  type GoExecutorErrorResponse,
  type GoExecutorRequest,
  type GoExecutorResponse,
} from "./executor-contract";

export type ExecutorHttpResponse = {
  ok: boolean;
  status: number;
  json: () => Promise<unknown>;
};

export type ExecutorFetch = (
  url: string,
  init: {
    method: "POST";
    headers: Record<string, string>;
    body: string;
  },
) => Promise<ExecutorHttpResponse>;

export type GoExecutorClientConfig = {
  baseUrl: string;
  fetchFunction?: ExecutorFetch;
};

export type GoExecutorClient = {
  /**
   * Executes a deterministic Go block and maps transport or contract failures to AppError.
   */
  executeBlock: <Input, Output>(
    request: GoExecutorRequest<Input>,
  ) => Promise<Result<GoExecutorResponse<Output>, AppError>>;
};

class ExecutorClientFailure extends Error {
  readonly details: Record<string, unknown>;
  readonly executorError: GoExecutorErrorResponse | undefined;

  constructor(
    message: string,
    details: Record<string, unknown>,
    executorError?: GoExecutorErrorResponse,
  ) {
    super(message);
    this.name = "ExecutorClientFailure";
    this.details = details;
    this.executorError = executorError;
  }
}

/**
 * Creates a Result-returning client for the Go executor HTTP API.
 */
export function createGoExecutorClient(
  config: GoExecutorClientConfig,
): GoExecutorClient {
  const normalizedBaseUrl = config.baseUrl.replace(/\/+$/, "");
  const fetchFunction = resolveFetchFunction(config.fetchFunction);

  return {
    executeBlock: async <Input, Output>(
      request: GoExecutorRequest<Input>,
    ): Promise<Result<GoExecutorResponse<Output>, AppError>> => {
      return fromPromise(
        async () => {
          const httpResponse = await fetchFunction(
            `${normalizedBaseUrl}/execute-block`,
            {
              method: "POST",
              headers: {
                "content-type": "application/json",
                "x-correlation-id": request.context.correlationId,
              },
              body: JSON.stringify(request),
            },
          );

          const responseBody = await httpResponse.json();

          if (!isGoExecutorResponse(responseBody)) {
            throw new ExecutorClientFailure(
              "Go executor returned an invalid response contract.",
              {
                responseStatus: httpResponse.status,
                responseBody,
              },
            );
          }

          if (!httpResponse.ok || responseBody.status === GO_EXECUTOR_STATUS.FAILED) {
            throw new ExecutorClientFailure(
              "Go executor returned a failed block response.",
              {
                responseStatus: httpResponse.status,
                executorStatus: responseBody.status,
              },
              responseBody.error,
            );
          }

          return responseBody as GoExecutorResponse<Output>;
        },
        (error) =>
          goExecutorError(
            toExecutorErrorDetails(error, normalizedBaseUrl, request),
            error,
          ),
      );
    },
  };
}

function resolveFetchFunction(fetchFunction?: ExecutorFetch): ExecutorFetch {
  if (fetchFunction) {
    return fetchFunction;
  }

  const runtimeFetch = (
    globalThis as typeof globalThis & {
      fetch?: ExecutorFetch;
    }
  ).fetch;

  if (!runtimeFetch) {
    throw new Error("A fetch implementation is required for Go executor calls.");
  }

  return runtimeFetch;
}

function toExecutorErrorDetails<Input>(
  error: unknown,
  executorBaseUrl: string,
  request: GoExecutorRequest<Input>,
): Record<string, unknown> {
  const baseDetails: Record<string, unknown> = {
    executorBaseUrl,
    blockName: request.block.name,
    blockVersion: request.block.version,
    workflowInstanceId: request.workflowInstanceId,
    workflowVersionId: request.workflowVersionId,
    correlationId: request.context.correlationId,
  };

  if (error instanceof ExecutorClientFailure) {
    return {
      ...baseDetails,
      ...error.details,
      executorError: error.executorError,
      message: error.message,
    };
  }

  if (error instanceof Error) {
    return {
      ...baseDetails,
      message: error.message,
      name: error.name,
    };
  }

  return {
    ...baseDetails,
    message: String(error),
  };
}

function isGoExecutorResponse(value: unknown): value is GoExecutorResponse {
  if (!isRecord(value)) {
    return false;
  }

  if (
    value.status !== GO_EXECUTOR_STATUS.SUCCEEDED &&
    value.status !== GO_EXECUTOR_STATUS.FAILED
  ) {
    return false;
  }

  if (
    !Array.isArray(value.proposedEvents) ||
    !Array.isArray(value.externalCallRequests) ||
    !Array.isArray(value.logs)
  ) {
    return false;
  }

  if (!isRecord(value.metrics) || typeof value.metrics.durationMs !== "number") {
    return false;
  }

  if (value.error !== undefined && !isExecutorErrorResponse(value.error)) {
    return false;
  }

  return true;
}

function isExecutorErrorResponse(value: unknown): value is GoExecutorErrorResponse {
  return (
    isRecord(value) &&
    typeof value.code === "string" &&
    typeof value.message === "string" &&
    typeof value.safeMessage === "string"
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
