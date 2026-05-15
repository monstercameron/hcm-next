export const isRecord = (value: unknown): value is Readonly<Record<string, unknown>> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

export const readPath = (source: unknown, path: string | undefined): unknown => {
  if (path === undefined || path.length === 0) {
    return source;
  }

  let current = source;

  for (const segment of path.split(".")) {
    if (!isRecord(current)) {
      return undefined;
    }

    current = current[segment];
  }

  return current;
};
