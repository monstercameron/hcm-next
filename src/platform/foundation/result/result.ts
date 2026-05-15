import type { AppError } from "../errors/app-error";

export type Result<TValue, TError = AppError> =
  | {
      ok: true;
      value: TValue;
    }
  | {
      ok: false;
      error: TError;
    };

/**
 * Creates a successful Result for a completed checked operation.
 */
export function ok<TValue>(value: TValue): Result<TValue, never> {
  return {
    ok: true,
    value,
  };
}

/**
 * Creates a failed Result for an expected or mapped boundary failure.
 */
export function err<TError>(error: TError): Result<never, TError> {
  return {
    ok: false,
    error,
  };
}

/**
 * Narrows a Result to its successful branch.
 */
export function isOk<TValue, TError>(
  result: Result<TValue, TError>,
): result is { ok: true; value: TValue } {
  return result.ok;
}

/**
 * Narrows a Result to its failed branch.
 */
export function isErr<TValue, TError>(
  result: Result<TValue, TError>,
): result is { ok: false; error: TError } {
  return !result.ok;
}
