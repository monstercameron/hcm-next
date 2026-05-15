import { describe, expect, it } from "vitest";

import { ERROR_CODES, systemError } from "../errors";
import { err, fromPromise, fromThrowable, isErr, isOk, ok } from "./index";

describe("Result helpers", () => {
  it("creates successful results", () => {
    const result = ok({ value: "created" });

    expect(isOk(result)).toBe(true);

    if (!result.ok) {
      throw new Error("Expected ok result.");
    }

    expect(result.value.value).toBe("created");
  });

  it("creates failed results", () => {
    const result = err(systemError({ operation: "test" }));

    expect(isErr(result)).toBe(true);

    if (result.ok) {
      throw new Error("Expected error result.");
    }

    expect(result.error.code).toBe(ERROR_CODES.SYSTEM_ERROR);
  });

  it("maps rejected promises", async () => {
    const result = await fromPromise(
      async () => Promise.reject(new Error("database unavailable")),
      (error) => systemError({ operation: "promise" }, error),
    );

    expect(result.ok).toBe(false);

    if (result.ok) {
      throw new Error("Expected mapped error result.");
    }

    expect(result.error.code).toBe(ERROR_CODES.SYSTEM_ERROR);
  });

  it("maps thrown synchronous failures", () => {
    const result = fromThrowable(
      () => {
        throw new Error("invalid payload");
      },
      (error) => systemError({ operation: "sync" }, error),
    );

    expect(result.ok).toBe(false);

    if (result.ok) {
      throw new Error("Expected mapped error result.");
    }

    expect(result.error.details?.operation).toBe("sync");
  });
});
