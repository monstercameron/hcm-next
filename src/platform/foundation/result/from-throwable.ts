import { err, ok, type Result } from "./result";

/**
 * Converts a throwing synchronous system boundary into a Result.
 */
export function fromThrowable<TValue, TError>(
  operation: () => TValue,
  mapError: (error: unknown) => TError,
): Result<TValue, TError> {
  try {
    return ok(operation());
  } catch (error) {
    return err(mapError(error));
  }
}
