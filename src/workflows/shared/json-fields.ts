/**
 * Reads a required-ish string field from a JSON record and trims blank input away.
 */
export function stringField(
  object: Record<string, unknown>,
  fieldName: string,
): string | undefined {
  const value = object[fieldName];

  if (typeof value !== "string") {
    return undefined;
  }

  const trimmedValue = value.trim();

  if (trimmedValue.length === 0) {
    return undefined;
  }

  return trimmedValue;
}

/**
 * Reads a number field from a JSON record without coercion.
 */
export function numberField(
  object: Record<string, unknown>,
  fieldName: string,
): number | undefined {
  const value = object[fieldName];

  return typeof value === "number" ? value : undefined;
}

/**
 * Reads a boolean field from a JSON record without coercion.
 */
export function booleanField(
  object: Record<string, unknown>,
  fieldName: string,
): boolean | undefined {
  const value = object[fieldName];

  return typeof value === "boolean" ? value : undefined;
}

/**
 * Reads a plain object field from a JSON record.
 */
export function objectField(
  object: Record<string, unknown>,
  fieldName: string,
): Record<string, unknown> | undefined {
  const value = object[fieldName];

  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined;
  }

  return value as Record<string, unknown>;
}

/**
 * Reads a string array field and removes blank or non-string values.
 */
export function stringArrayField(
  object: Record<string, unknown>,
  fieldName: string,
): string[] {
  const value = object[fieldName];

  if (!Array.isArray(value)) {
    return [];
  }

  return value.filter((item): item is string => {
    return typeof item === "string" && item.trim().length > 0;
  });
}

/**
 * Reads a nested value from a dot-delimited object path.
 */
export function valueAtDotPath(source: Record<string, unknown>, path: string): unknown {
  const pathSegments = path.split(".").filter((segment) => segment.length > 0);
  let currentValue: unknown = source;

  for (const pathSegment of pathSegments) {
    if (typeof currentValue !== "object" || currentValue === null) {
      return undefined;
    }

    currentValue = (currentValue as Record<string, unknown>)[pathSegment];
  }

  return currentValue;
}

/**
 * Clones JSON-compatible data so callers can safely mutate local copies.
 */
export function cloneJsonValue<TValue>(value: TValue): TValue {
  return JSON.parse(JSON.stringify(value)) as TValue;
}
