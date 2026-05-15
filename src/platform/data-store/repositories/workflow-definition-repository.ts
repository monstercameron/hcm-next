import type { Result } from "@hcm-next/foundation";

import type { DatabaseClient } from "../client";
import { findOne } from "./repository-utils";
import type {
  WorkflowDefinitionRecord,
  WorkflowDefinitionWithVersionRecord,
  WorkflowVersionRecord,
} from "./repository-types";

type WorkflowDefinitionWithVersionRow = {
  definition: WorkflowDefinitionRecord;
  version: WorkflowVersionRecord;
};

export type WorkflowDefinitionRepository = {
  /** Finds the active workflow definition and current published version for an intent. */
  findActiveVersionByIntent: (
    tenantId: string,
    intent: string,
  ) => Promise<Result<WorkflowDefinitionWithVersionRecord>>;
};

/**
 * Creates repository methods for workflow definition reads.
 */
export function createWorkflowDefinitionRepository(
  database: DatabaseClient,
): WorkflowDefinitionRepository {
  return {
    async findActiveVersionByIntent(tenantId, intent) {
      const rowResult = await findOne<WorkflowDefinitionWithVersionRow>(
        database,
        `
          SELECT row_to_json(workflow_definitions.*) AS definition,
                 row_to_json(workflow_versions.*) AS version
          FROM workflow_definitions
          INNER JOIN workflow_versions
            ON workflow_versions.workflow_version_id = workflow_definitions.current_version_id
          WHERE workflow_definitions.tenant_id = $1
            AND workflow_definitions.name = $2
            AND workflow_definitions.status = 'active'
            AND workflow_versions.status = 'published'
        `,
        [tenantId, intent],
        "WorkflowDefinition",
        { tenantId, intent },
      );

      if (!rowResult.ok) {
        return rowResult;
      }

      return {
        ok: true,
        value: {
          definition: rowResult.value.definition,
          version: rowResult.value.version,
        },
      };
    },
  };
}
