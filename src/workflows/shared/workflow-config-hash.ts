import { createHash } from "node:crypto";
import type { WorkflowConfig } from "./workflow-config.js";

/**
 * Produces a deterministic SHA-256 hash for a workflow config.
 */
export function workflowConfigHash(workflowConfig: WorkflowConfig): string {
  const canonicalJson = canonicalJsonString(workflowConfig);
  const hash = createHash("sha256").update(canonicalJson).digest("hex");

  return `sha256:${hash}`;
}

/**
 * Converts JSON-compatible values to a stable object-key-sorted string.
 */
export function canonicalJsonString(value: unknown): string {
  return JSON.stringify(canonicalizeJsonValue(value));
}

function canonicalizeJsonValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => {
      return canonicalizeJsonValue(item);
    });
  }

  if (isRecord(value)) {
    const sortedEntries = Object.entries(value)
      .filter((entry) => {
        return entry[1] !== undefined;
      })
      .sort(([leftKey], [rightKey]) => {
        return leftKey.localeCompare(rightKey);
      })
      .map(([key, entryValue]) => {
        return [key, canonicalizeJsonValue(entryValue)] as const;
      });

    return Object.fromEntries(sortedEntries);
  }

  if (typeof value === "number" && !Number.isFinite(value)) {
    return null;
  }

  return value;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
