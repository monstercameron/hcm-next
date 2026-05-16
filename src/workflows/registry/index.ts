/**
 * Workflow config registry — runtime-facing entry point.
 *
 * This module is intentionally narrow: it exposes the helpers the API server
 * needs to load a published workflow config by intent and to compute a stable
 * fingerprint for client-side caching. It does NOT pull in the workflow admin
 * surface (import / publish / validation) so server code that only needs to
 * read a published config stays free of admin transitive dependencies.
 */
export { getWorkflowConfigByIntent } from "./workflow-config-loader.js";
export { computeWorkflowConfigHash } from "./workflow-config-fingerprint.js";
export type { WorkflowConfig } from "../shared/workflow-config.js";
