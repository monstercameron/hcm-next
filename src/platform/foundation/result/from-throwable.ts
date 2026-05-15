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
    process.stderr.write(
      `${JSON.stringify({
        timestamp: new Date().toISOString(),
        level: "error",
        message: "unhandled exception at sync boundary",
        exception:
          error instanceof Error
            ? { name: error.name, message: error.message }
            : String(error),
      })}\n`,
    );
    return err(mapError(error));
  }
}
