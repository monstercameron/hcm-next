import { err, ok, type Result } from "./result";

/**
 * Converts a throwing async system boundary into a Result.
 */
export async function fromPromise<TValue, TError>(
  operation: () => Promise<TValue>,
  mapError: (error: unknown) => TError,
): Promise<Result<TValue, TError>> {
  try {
    const value = await operation();

    return ok(value);
  } catch (error) {
    process.stderr.write(
      `${JSON.stringify({
        timestamp: new Date().toISOString(),
        level: "error",
        message: "unhandled exception at async boundary",
        exception:
          error instanceof Error
            ? { name: error.name, message: error.message }
            : String(error),
      })}\n`,
    );
    return err(mapError(error));
  }
}
