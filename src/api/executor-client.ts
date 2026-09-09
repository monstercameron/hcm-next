import {
  fromPromise,
  goExecutorError,
  validationFailedError,
  type AppError,
  type Result,
  type StructuredLogger,
} from "@human-capital-management-suite/foundation";

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
    logger?: StructuredLogger,
  ): Promise<Result<ExecutorResponse<TOutput>, AppError>>;
};

export function createHttpExecutorClient(baseUrl: string): ExecutorClient {
  return {
    async executeBlock<TOutput>(
      request: ExecutorRequest,
      logger?: StructuredLogger,
    ): Promise<Result<ExecutorResponse<TOutput>, AppError>> {
      const blockRef = `${request.block.name}@${request.block.version}`;
      const startMs = Date.now();

      logger?.info("executor block started", {
        block: blockRef,
        workflowInstanceId: request.workflowInstanceId,
        changeRequestId: request.changeRequestId,
        correlationId: request.context.correlationId,
      });

      const responseResult = await fromPromise(
        async () => {
          const response = await fetch(`${baseUrl}/execute-block`, {
            method: "POST",
            headers: {
              "content-type": "application/json",
              "x-request-id": request.context.correlationId,
              "x-correlation-id": request.context.correlationId,
            },
            body: JSON.stringify(request),
          });

          return response;
        },
        (error: unknown) => goExecutorError(undefined, error),
      );

      if (!responseResult.ok) {
        logger?.error("executor request failed", {
          block: blockRef,
          workflowInstanceId: request.workflowInstanceId,
          errorCode: responseResult.error.code,
          durationMs: Date.now() - startMs,
        });
        return responseResult;
      }

      const bodyResult = await fromPromise(
        async () => {
          return (await responseResult.value.json()) as ExecutorResponse<TOutput>;
        },
        (error: unknown) => goExecutorError(undefined, error),
      );

      if (!bodyResult.ok) {
        logger?.error("executor response parse failed", {
          block: blockRef,
          workflowInstanceId: request.workflowInstanceId,
          errorCode: bodyResult.error.code,
          durationMs: Date.now() - startMs,
        });
        return bodyResult;
      }

      for (const { level: rawLevel, message: rawMessage, ...details } of bodyResult
        .value.logs) {
        const level = (rawLevel as string | undefined) ?? "info";
        const message = (rawMessage as string | undefined) ?? "executor log";
        if (level === "error") {
          logger?.error(message, { ...details, service: "executor" });
        } else if (level === "warn") {
          logger?.warn(message, { ...details, service: "executor" });
        } else {
          logger?.info(message, { ...details, service: "executor" });
        }
      }

      if (bodyResult.value.status !== "succeeded") {
        logger?.warn("executor block failed", {
          block: blockRef,
          workflowInstanceId: request.workflowInstanceId,
          errorCode: bodyResult.value.error?.code,
          durationMs: Date.now() - startMs,
        });
        return {
          ok: false,
          error: validationFailedError({
            executorError: bodyResult.value.error,
          }),
        };
      }

      logger?.info("executor block completed", {
        block: blockRef,
        workflowInstanceId: request.workflowInstanceId,
        durationMs: Date.now() - startMs,
      });

      return bodyResult;
    },
  };
}
