export {
  ERROR_CODES,
  databaseError,
  err,
  fromPromise,
  fromThrowable,
  goExecutorError,
  idempotencyConflictError,
  integrationError,
  invalidWorkflowTransitionError,
  notFoundError,
  ok,
  permissionDeniedError,
  systemError,
  validationFailedError,
  versionConflictError,
  workflowNotFoundError,
} from "@human-capital-management-suite/foundation";

export type {
  AppError,
  ErrorCode,
  Result,
} from "@human-capital-management-suite/foundation";
