import { createHash } from "node:crypto";
import { canonicalJsonString } from "../shared/workflow-config-hash.js";
import type { WorkflowConfig } from "../shared/workflow-config.js";

/**
 * Produces a stable SHA-1 fingerprint of a workflow config so client caches
 * can key on a deterministic string. Uses canonical JSON (sorted object keys,
 * undefined values dropped) so equivalent configs with different key ordering
 * yield the same hash.
 *
 * Returned shape is `sha1:<hex>` so callers can distinguish the algorithm at
 * a glance and so it does not collide with the legacy SHA-256 hash used by
 * the admin registry.
 */
export function computeWorkflowConfigHash(workflowConfig: WorkflowConfig): string {
  const canonicalJson = canonicalJsonString(workflowConfig);
  const hashHex = createHash("sha1").update(canonicalJson).digest("hex");

  return `sha1:${hashHex}`;
}
