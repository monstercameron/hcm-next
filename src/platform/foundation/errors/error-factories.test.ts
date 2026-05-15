import { describe, expect, it } from "vitest";

import { ERROR_CODES } from "./error-codes";
import {
  databaseError,
  goExecutorError,
  idempotencyConflictError,
  integrationError,
  invalidWorkflowTransitionError,
  notFoundError,
  permissionDeniedError,
  systemError,
  validationFailedError,
  versionConflictError,
  workflowNotFoundError,
} from "./error-factories";

describe("error factories", () => {
  it("creates safe validation errors", () => {
    const error = validationFailedError({ field: "lastName" });

    expect(error.code).toBe(ERROR_CODES.VALIDATION_FAILED);
    expect(error.safeMessage).not.toContain("lastName");
    expect(error.details?.field).toBe("lastName");
  });

  it("creates all core error codes", () => {
    const errors = [
      permissionDeniedError(),
      notFoundError("Actor"),
      workflowNotFoundError(),
      invalidWorkflowTransitionError(),
      versionConflictError(),
      idempotencyConflictError(),
      databaseError(),
      integrationError(),
      goExecutorError(),
      systemError(),
    ];

    expect(errors.map((error) => error.code)).toEqual([
      ERROR_CODES.PERMISSION_DENIED,
      ERROR_CODES.NOT_FOUND,
      ERROR_CODES.WORKFLOW_NOT_FOUND,
      ERROR_CODES.INVALID_WORKFLOW_TRANSITION,
      ERROR_CODES.VERSION_CONFLICT,
      ERROR_CODES.IDEMPOTENCY_CONFLICT,
      ERROR_CODES.DATABASE_ERROR,
      ERROR_CODES.INTEGRATION_ERROR,
      ERROR_CODES.GO_EXECUTOR_ERROR,
      ERROR_CODES.SYSTEM_ERROR,
    ]);
  });
});
