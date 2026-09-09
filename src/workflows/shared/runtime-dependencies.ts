import type { AiClient } from "@human-capital-management-suite/ai-client";
import type { ActorRecord, Repositories } from "@human-capital-management-suite/data-store";
import type { AppError, Result, StructuredLogger } from "@human-capital-management-suite/foundation";

export type ApiRequestContext = {
  actor: ActorRecord;
  tenantId: string;
  environmentId: string;
  requestId: string;
  correlationId: string;
};

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

export type ExternalWriteClientResponse = {
  rawResponse: Record<string, unknown>;
  reasonCodes?: string[];
};

export type ExternalWriteClient = {
  submit(
    payload: Record<string, unknown>,
    idempotencyKey: string,
    logger?: StructuredLogger,
  ): Promise<Result<ExternalWriteClientResponse, AppError>>;
};

export type AppRuntimeConfig = {
  allowFilesystemWorkflowImports?: boolean;
};

export type AppDependencies = {
  repositories: Repositories;
  executorClient: ExecutorClient;
  aiClient?: AiClient;
  externalWriteClients?: Record<string, ExternalWriteClient>;
  logger?: StructuredLogger;
  runtimeConfig?: AppRuntimeConfig;
};
