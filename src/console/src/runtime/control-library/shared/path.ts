import { controlErr, controlOk, type ControlResult } from "./result";
import { isControlRecord } from "./guards";
import type { ControlRecord } from "./types";

const CONTROL_PATH_PATTERN = /^[A-Za-z0-9_$-]+(\.[A-Za-z0-9_$-]+)*$/;

export const isValidControlPath = (path: string): boolean =>
  path.length > 0 && CONTROL_PATH_PATTERN.test(path);

/**
 * Reads a dot-delimited path from arbitrary workflow/API data without throwing.
 */
export const readControlPath = (
  record: ControlRecord,
  path: string | undefined,
): unknown => {
  if (path === undefined || path.length === 0) {
    return undefined;
  }

  return path.split(".").reduce<unknown>((currentValue, pathKey) => {
    if (!isControlRecord(currentValue)) {
      return undefined;
    }

    return currentValue[pathKey];
  }, record);
};

/**
 * Validates and reads a path when the caller needs explicit invalid-path errors.
 */
export const parseControlPathValue = (
  record: ControlRecord,
  path: string,
): ControlResult<unknown> => {
  if (!isValidControlPath(path)) {
    return controlErr("CONTROL_CONFIG_INVALID_PATH", "Invalid control data path.", {
      path,
    });
  }

  return controlOk(readControlPath(record, path));
};
