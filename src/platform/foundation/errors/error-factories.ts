import type { JsonRecord } from "../domain";
import type { AppError } from "./app-error";
import { ERROR_CODES } from "./error-codes";

type BuildAppErrorInput = {
  code: AppError["code"];
  message: string;
  safeMessage: string;
  details?: JsonRecord | undefined;
  cause?: unknown;
};

function buildAppError(input: BuildAppErrorInput): AppError {
  const appError: AppError = {
    code: input.code,
    message: input.message,
    safeMessage: input.safeMessage,
  };

  if (input.details !== undefined) {
    appError.details = input.details;
  }

  if (input.cause !== undefined) {
    appError.cause = input.cause;
  }

  return appError;
}

/**
 * Creates an error for invalid input or failed validation rules.
 */
export function validationFailedError(details?: JsonRecord): AppError {
  return buildAppError({
    code: ERROR_CODES.VALIDATION_FAILED,
    message: "Validation failed.",
    safeMessage: "Some fields are invalid. Review the request and try again.",
    details,
  });
}

/**
 * Creates an error for denied actor permissions.
 */
export function permissionDeniedError(details?: JsonRecord): AppError {
  return buildAppError({
    code: ERROR_CODES.PERMISSION_DENIED,
    message: "Permission denied.",
    safeMessage: "You do not have permission to perform this action.",
    details,
  });
}

/**
 * Creates an error for a missing resource.
 */
export function notFoundError(resource: string, details?: JsonRecord): AppError {
  return buildAppError({
    code: ERROR_CODES.NOT_FOUND,
    message: `${resource} was not found.`,
    safeMessage: "The requested resource was not found.",
    details,
  });
}

/**
 * Creates an error for a missing workflow instance or definition.
 */
export function workflowNotFoundError(details?: JsonRecord): AppError {
  return buildAppError({
    code: ERROR_CODES.WORKFLOW_NOT_FOUND,
    message: "Workflow was not found.",
    safeMessage: "The requested workflow was not found.",
    details,
  });
}

/**
 * Creates an error for an invalid workflow transition from the current state.
 */
export function invalidWorkflowTransitionError(details?: JsonRecord): AppError {
  return buildAppError({
    code: ERROR_CODES.INVALID_WORKFLOW_TRANSITION,
    message: "Invalid workflow transition.",
    safeMessage: "That workflow action is not available right now.",
    details,
  });
}

/**
 * Creates an error for stale expectedVersion checks.
 */
export function versionConflictError(details?: JsonRecord): AppError {
  return buildAppError({
    code: ERROR_CODES.VERSION_CONFLICT,
    message: "Version conflict.",
    safeMessage: "This workflow changed. Refresh and try again.",
    details,
  });
}

/**
 * Creates an error for reused or conflicting idempotency keys.
 */
export function idempotencyConflictError(details?: JsonRecord): AppError {
  return buildAppError({
    code: ERROR_CODES.IDEMPOTENCY_CONFLICT,
    message: "Idempotency conflict.",
    safeMessage: "This request conflicts with a previous request.",
    details,
  });
}

/**
 * Creates an error for database boundary failures.
 */
export function databaseError(details?: JsonRecord, cause?: unknown): AppError {
  return buildAppError({
    code: ERROR_CODES.DATABASE_ERROR,
    message: "Database operation failed.",
    safeMessage: "A storage error occurred. Try again later.",
    details,
    cause,
  });
}

/**
 * Creates an error for external integration failures.
 */
export function integrationError(details?: JsonRecord, cause?: unknown): AppError {
  return buildAppError({
    code: ERROR_CODES.INTEGRATION_ERROR,
    message: "Integration operation failed.",
    safeMessage: "An external system could not be reached.",
    details,
    cause,
  });
}

/**
 * Creates an error for Go executor failures.
 */
export function goExecutorError(details?: JsonRecord, cause?: unknown): AppError {
  return buildAppError({
    code: ERROR_CODES.GO_EXECUTOR_ERROR,
    message: "Go executor operation failed.",
    safeMessage: "A workflow execution block failed.",
    details,
    cause,
  });
}

/**
 * Creates an error for unexpected system failures at process boundaries.
 */
export function systemError(details?: JsonRecord, cause?: unknown): AppError {
  return buildAppError({
    code: ERROR_CODES.SYSTEM_ERROR,
    message: "Unexpected system error.",
    safeMessage: "Something went wrong. Try again later.",
    details,
    cause,
  });
}
