import type { JsonRecord } from "../domain";
import type { AppError } from "./app-error";
import {
  databaseError,
  goExecutorError,
  integrationError,
  systemError,
} from "./error-factories";

/**
 * Converts an unknown thrown value into safe internal diagnostic details.
 */
export function unknownErrorDetails(error: unknown): JsonRecord {
  if (error instanceof Error) {
    return {
      name: error.name,
      message: error.message,
    };
  }

  return {
    message: "Unknown thrown value.",
  };
}

/**
 * Maps unknown database boundary failures into an AppError.
 */
export function mapUnknownToDatabaseError(error: unknown): AppError {
  return databaseError(unknownErrorDetails(error), error);
}

/**
 * Maps unknown integration boundary failures into an AppError.
 */
export function mapUnknownToIntegrationError(error: unknown): AppError {
  return integrationError(unknownErrorDetails(error), error);
}

/**
 * Maps unknown Go executor boundary failures into an AppError.
 */
export function mapUnknownToGoExecutorError(error: unknown): AppError {
  return goExecutorError(unknownErrorDetails(error), error);
}

/**
 * Maps unknown process boundary failures into an AppError.
 */
export function mapUnknownToSystemError(error: unknown): AppError {
  return systemError(unknownErrorDetails(error), error);
}
