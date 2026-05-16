import type { AppError, Result, StructuredLogger } from "@hcm-next/foundation";

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
