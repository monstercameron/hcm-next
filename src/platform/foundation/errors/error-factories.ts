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
 * Creates an error for AI provider failures, timeouts, or unparseable responses.
 */
export function aiReviewError(details?: JsonRecord, cause?: unknown): AppError {
  return buildAppError({
    code: ERROR_CODES.AI_REVIEW_ERROR,
    message: "AI review operation failed.",
    safeMessage:
      "The AI review could not be completed. The workflow will continue without it.",
    details,
    cause,
  });
}

export type AiUiGenerationStage =
  | "api_call"
  | "response_parse"
  | "schema_validation"
  | "chat_turn";

const AI_UI_GENERATION_CODE_BY_STAGE: Record<
  AiUiGenerationStage,
  (typeof ERROR_CODES)[
    | "AI_UI_GENERATION_API_CALL_FAILED"
    | "AI_UI_GENERATION_RESPONSE_PARSE_FAILED"
    | "AI_UI_GENERATION_SCHEMA_VALIDATION_FAILED"
    | "AI_UI_GENERATION_CHAT_TURN_FAILED"]
> = {
  api_call: ERROR_CODES.AI_UI_GENERATION_API_CALL_FAILED,
  response_parse: ERROR_CODES.AI_UI_GENERATION_RESPONSE_PARSE_FAILED,
  schema_validation: ERROR_CODES.AI_UI_GENERATION_SCHEMA_VALIDATION_FAILED,
  chat_turn: ERROR_CODES.AI_UI_GENERATION_CHAT_TURN_FAILED,
};

const AI_UI_GENERATION_INTERNAL_MESSAGE_BY_STAGE: Record<AiUiGenerationStage, string> =
  {
    api_call: "AI UI generation provider call failed.",
    response_parse: "AI UI generation response could not be parsed as JSON.",
    schema_validation:
      "AI UI generation response did not match the expected page definition shape.",
    chat_turn: "AI chat turn could not be completed by the provider.",
  };

const AI_UI_GENERATION_SAFE_MESSAGE =
  "We could not generate the screen. Try again or use the standard view.";

/**
 * Creates an error for AI UI generation failures across each pipeline stage:
 * provider API call, JSON parse, or schema validation. Public message is
 * intentionally generic so the chat panel can render it directly.
 */
export function aiUiGenerationError(
  context: { stage: AiUiGenerationStage } & JsonRecord,
  cause?: unknown,
): AppError {
  const { stage, ...rest } = context;

  return buildAppError({
    code: AI_UI_GENERATION_CODE_BY_STAGE[stage],
    message: AI_UI_GENERATION_INTERNAL_MESSAGE_BY_STAGE[stage],
    safeMessage: AI_UI_GENERATION_SAFE_MESSAGE,
    details: { stage, ...rest },
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
