import {
  err,
  notFoundError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type {
  Repositories,
  WorkflowVersionRecord,
} from "@human-capital-management-suite/data-store";
import type { WorkflowConfig } from "../shared/workflow-config.js";
import {
  findFilesystemWorkflowConfigByIntent,
  workflowConfigFromGraphDefinition,
} from "../shared/workflow-config-registry.js";

const PUBLISHED_STATUS = "published";

/**
 * Returns the published `WorkflowConfig` for the given public intent.
 *
 * Resolution order:
 *   1. Highest `versionNumber` published `WorkflowVersionRecord` whose
 *      `graphDefinition.intent` matches `intent` across all tenants/environments.
 *      The repository is the source of truth for the API runtime so any
 *      tenant-published version takes precedence.
 *   2. If the chosen record only carries a legacy seed graph definition
 *      (intent + initialState only), fall back to the checked-in filesystem
 *      workflow config for that intent so the runtime can still execute.
 *   3. If no published version exists in the repositories at all, fall back
 *      to the checked-in filesystem workflow config registered with the same
 *      intent. This keeps the demo store usable during local development and
 *      keeps tests that only seed the filesystem registry green.
 *
 * Returns `notFoundError("Workflow", { intent })` when neither a published
 * repository version nor a filesystem registry entry exists for the intent.
 */
export function getWorkflowConfigByIntent(
  repositories: Repositories,
  intent: string,
): Result<WorkflowConfig, AppError> {
  const highestPublishedVersion = findHighestPublishedVersionForIntent(
    repositories,
    intent,
  );

  if (highestPublishedVersion !== undefined) {
    const persistedConfigResult = workflowConfigFromGraphDefinition(
      highestPublishedVersion.graphDefinition,
    );
    if (persistedConfigResult.ok) {
      return persistedConfigResult;
    }

    // The persisted graph definition is a legacy seed record (e.g. only
    // `intent` + `initialState`). Fall back to the checked-in filesystem
    // workflow config which carries the full executable shape.
    const filesystemFallback = findFilesystemWorkflowConfigByIntent(intent);
    if (filesystemFallback.ok) {
      return filesystemFallback;
    }

    // The persisted record is unusable and no filesystem fallback exists.
    // Surface the original validation failure so callers see why.
    return persistedConfigResult;
  }

  const filesystemResult = findFilesystemWorkflowConfigByIntent(intent);
  if (filesystemResult.ok) {
    return filesystemResult;
  }

  return err(notFoundError("Workflow", { intent }));
}

function findHighestPublishedVersionForIntent(
  repositories: Repositories,
  intent: string,
): WorkflowVersionRecord | undefined {
  const allVersions = collectAllWorkflowVersions(repositories);
  let highestVersion: WorkflowVersionRecord | undefined;

  for (const candidate of allVersions) {
    if (!isPublishedVersionForIntent(candidate, intent)) {
      continue;
    }

    if (
      highestVersion === undefined ||
      candidate.versionNumber > highestVersion.versionNumber
    ) {
      highestVersion = candidate;
    }
  }

  return highestVersion;
}

function collectAllWorkflowVersions(
  repositories: Repositories,
): WorkflowVersionRecord[] {
  // The `Repositories.store` field exposes the underlying map directly so the
  // loader can search across every tenant without requiring callers to pass a
  // tenantId. The AI generator API runs at a process scope, not a tenant scope.
  const versionsMap = repositories.store.workflowVersions;

  return [...versionsMap.values()];
}

function isPublishedVersionForIntent(
  workflowVersion: WorkflowVersionRecord,
  intent: string,
): boolean {
  if (workflowVersion.status !== PUBLISHED_STATUS) {
    return false;
  }

  const versionIntent = workflowVersion.graphDefinition["intent"];

  return typeof versionIntent === "string" && versionIntent === intent;
}
