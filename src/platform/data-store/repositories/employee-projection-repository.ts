import type { Result } from "@human-capital-management-suite/foundation";

import type { DatabaseClient } from "../client";
import { findOne, requireMutationRow, runQuery } from "./repository-utils";
import type { DatabaseJson, EmployeeProjectionRecord } from "./repository-types";

export type UpsertEmployeeProjectionInput = {
  tenantId: string;
  employeeId: string;
  projectionVersion?: number;
  asOfEffectiveAt?: Date | string | null;
  sourceEventId?: string | null;
  sourceEventSequence?: string | number | null;
  document: DatabaseJson;
  indexedFields?: DatabaseJson;
};

export type EmployeeProjectionRepository = {
  /** Finds the current employee projection by worker identifier. */
  findByEmployeeId: (
    tenantId: string,
    employeeId: string,
  ) => Promise<Result<EmployeeProjectionRecord>>;
  /** Upserts the rebuildable employee projection document. */
  upsert: (
    input: UpsertEmployeeProjectionInput,
  ) => Promise<Result<EmployeeProjectionRecord>>;
};

/**
 * Creates repository methods for employee projections.
 */
export function createEmployeeProjectionRepository(
  database: DatabaseClient,
): EmployeeProjectionRepository {
  return {
    findByEmployeeId(tenantId, employeeId) {
      return findOne<EmployeeProjectionRecord>(
        database,
        `
          SELECT *
          FROM employee_projection
          WHERE tenant_id = $1
            AND employee_id = $2
        `,
        [tenantId, employeeId],
        "EmployeeProjection",
        { tenantId, employeeId },
      );
    },
    async upsert(input) {
      const rowsResult = await runQuery<EmployeeProjectionRecord>(
        database,
        `
          INSERT INTO employee_projection (
            tenant_id,
            employee_id,
            projection_version,
            as_of_effective_at,
            source_event_id,
            source_event_sequence,
            document,
            indexed_fields
          )
          VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb)
          ON CONFLICT (tenant_id, employee_id) DO UPDATE
          SET projection_version = EXCLUDED.projection_version,
              as_of_effective_at = EXCLUDED.as_of_effective_at,
              source_event_id = EXCLUDED.source_event_id,
              source_event_sequence = EXCLUDED.source_event_sequence,
              document = EXCLUDED.document,
              indexed_fields = EXCLUDED.indexed_fields,
              updated_at = now()
          RETURNING *
        `,
        [
          input.tenantId,
          input.employeeId,
          input.projectionVersion ?? 1,
          input.asOfEffectiveAt ?? null,
          input.sourceEventId ?? null,
          input.sourceEventSequence ?? null,
          input.document,
          input.indexedFields ?? {},
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "EmployeeProjection", input);
    },
  };
}
