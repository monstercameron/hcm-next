import { createHash } from "node:crypto";

/**
 * Stable SHA-1 idempotency key derived from a tagged set of inputs.
 *
 * Used by the AI chat tool dispatcher to collapse identical workflow
 * commands (start, transition) onto a single attempt. Identical inputs
 * — including object key order, since we canonicalise via JSON.stringify
 * over a structured payload — produce the same key.
 */
export function sha1IdempotencyKey(prefix: string, parts: readonly unknown[]): string {
  const canonical = JSON.stringify(parts);
  const hashHex = createHash("sha1").update(canonical).digest("hex");
  return `${prefix}:${hashHex}`;
}
