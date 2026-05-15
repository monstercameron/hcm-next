import {
  fromPromise,
  goExecutorError,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";

export type ExecutorBlockRef = {
  name: string;
  version: string;
};

export type ExecutorRequest = {
  tenantId: string;
  environmentId: string;
  changeRequestId: string;
  workflowInstanceId: string;
  workflowVersionId: string;
  block: ExecutorBlockRef;
  input: Record<string, unknown>;
  context: {
    actorId: string;
    effectiveAt: string;
    permissions: Record<string, unknown>;
    correlationId: string;
    idempotencyKey: string;
  };
};

export type ExecutorResponse<TOutput = Record<string, unknown>> = {
  status: "succeeded" | "failed";
  output?: TOutput;
  proposedEvents: Record<string, unknown>[];
  externalCallRequests: Record<string, unknown>[];
  logs: Record<string, unknown>[];
  metrics: Record<string, unknown>;
  error?: {
    code: string;
    message: string;
    details?: Record<string, unknown>;
  };
};

export type ExecutorClient = {
  executeBlock<TOutput>(
    request: ExecutorRequest,
  ): Promise<Result<ExecutorResponse<TOutput>, AppError>>;
};

export function createHttpExecutorClient(baseUrl: string): ExecutorClient {
  return {
    async executeBlock<TOutput>(
      request: ExecutorRequest,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      const responseResult = await fromPromise(
        async () => {
          const response = await fetch(`${baseUrl}/execute-block`, {
            method: "POST",
            headers: { "content-type": "application/json" },
            body: JSON.stringify(request),
          });

          return response;
        },
        (error: unknown) => goExecutorError(undefined, error),
      );

      if (!responseResult.ok) {
        return responseResult;
      }

      const bodyResult = await fromPromise(
        async () => {
          return (await responseResult.value.json()) as ExecutorResponse<TOutput>;
        },
        (error: unknown) => goExecutorError(undefined, error),
      );

      if (!bodyResult.ok) {
        return bodyResult;
      }

      if (bodyResult.value.status !== "succeeded") {
        return {
          ok: false,
          error: validationFailedError({
            executorError: bodyResult.value.error,
          }),
        };
      }

      return bodyResult;
    },
  };
}
