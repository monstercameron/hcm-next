const redactedValue = "[redacted]";

const sensitiveKeyFragments = [
  "amount",
  "bonus",
  "compensation",
  "document",
  "homeaddress",
  "mobilephone",
  "pay",
  "personalemail",
  "salary",
  "secret",
  "ssn",
  "tax",
  "token",
];

/**
 * Redacts sensitive HR and integration values before admin debugger responses leave
 * the runtime boundary.
 */
export function redactSensitiveWorkflowDebugValue(value: unknown): unknown {
  return redactValueAtDepth(value, 0);
}

function redactValueAtDepth(value: unknown, depth: number): unknown {
  if (depth > 8) {
    return "[max_depth]";
  }

  if (Array.isArray(value)) {
    return value.map((item) => redactValueAtDepth(item, depth + 1));
  }

  if (typeof value !== "object" || value === null) {
    return value;
  }

  const redactedRecord: Record<string, unknown> = {};

  for (const [key, nestedValue] of Object.entries(value)) {
    redactedRecord[key] = isSensitiveKey(key)
      ? redactedValue
      : redactValueAtDepth(nestedValue, depth + 1);
  }

  return redactedRecord;
}

function isSensitiveKey(key: string): boolean {
  const normalizedKey = key.toLowerCase().replace(/[^a-z0-9]/g, "");

  return sensitiveKeyFragments.some((fragment) => {
    return normalizedKey.includes(fragment);
  });
}
