import { ERROR_CODES, type AppError, type Result } from "@hcm-next/foundation";

export type JsonHttpResponse = {
  status: number;
  body: unknown;
};

export function toJsonHttpResponse<T>(result: Result<T, AppError>): JsonHttpResponse {
  if (result.ok) {
    return {
      status: 200,
      body: result.value,
    };
  }

  return {
    status: statusForError(result.error),
    body: {
      error: {
        code: result.error.code,
        message: result.error.safeMessage,
        details: result.error.details,
      },
    },
  };
}

export function statusForError(error: AppError): number {
  if (error.code === ERROR_CODES.PERMISSION_DENIED) {
    return 403;
  }
  if (
    error.code === ERROR_CODES.NOT_FOUND ||
    error.code === ERROR_CODES.WORKFLOW_NOT_FOUND
  ) {
    return 404;
  }
  if (
    error.code === ERROR_CODES.VERSION_CONFLICT ||
    error.code === ERROR_CODES.IDEMPOTENCY_CONFLICT
  ) {
    return 409;
  }
  if (
    error.code === ERROR_CODES.VALIDATION_FAILED ||
    error.code === ERROR_CODES.INVALID_WORKFLOW_TRANSITION
  ) {
    return 400;
  }
  return 500;
}
