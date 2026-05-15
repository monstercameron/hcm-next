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
    return err(mapError(error));
  }
}
