import type { Result } from "@hcm-next/foundation";

import type { DatabaseClient } from "../client";
import { runQuery } from "./repository-utils";
import type { ProposedChangeRecord } from "./repository-types";

export type CreateProposedChangeInput = {
  tenantId: string;
  changeRequestId: string;
  targetObjectType: string;
  targetObjectId: string;
  fieldPath: string;
  currentValue?: unknown;
  proposedValue: unknown;
  effectiveAt?: Date | string | null;
  reasonCode?: string | null;
  validationStatus: string;
  riskLevel: string;
  metadata?: Record<string, unknown>;
};

export type ProposedChangeRepository = {
  /** Creates one or more proposed field-level changes for a change request. */
  createMany: (
    inputs: readonly CreateProposedChangeInput[],
  ) => Promise<Result<ProposedChangeRecord[]>>;
  /** Lists proposed changes for a change request. */
  listForChangeRequest: (
    tenantId: string,
    changeRequestId: string,
  ) => Promise<Result<ProposedChangeRecord[]>>;
};

/**
 * Creates repository methods for proposed changes.
 */
export function createProposedChangeRepository(
  database: DatabaseClient,
): ProposedChangeRepository {
  return {
    async createMany(inputs) {
      const createdRows: ProposedChangeRecord[] = [];

      for (const input of inputs) {
        const rowsResult = await runQuery<ProposedChangeRecord>(
          database,
          `
            INSERT INTO proposed_changes (
              tenant_id,
              change_request_id,
              target_object_type,
              target_object_id,
              field_path,
              current_value,
              proposed_value,
              effective_at,
              reason_code,
              validation_status,
              risk_level,
              metadata
            )
            VALUES (
              $1,
              $2,
              $3,
              $4,
              $5,
              $6::jsonb,
              $7::jsonb,
              $8,
              $9,
              $10,
              $11,
              $12::jsonb
            )
            RETURNING *
          `,
          [
            input.tenantId,
            input.changeRequestId,
            input.targetObjectType,
            input.targetObjectId,
            input.fieldPath,
            input.currentValue ?? null,
            input.proposedValue,
            input.effectiveAt ?? null,
            input.reasonCode ?? null,
            input.validationStatus,
            input.riskLevel,
            input.metadata ?? {},
          ],
        );

        if (!rowsResult.ok) {
          return rowsResult;
        }

        const createdRow = rowsResult.value[0];

        if (createdRow !== undefined) {
          createdRows.push(createdRow);
        }
      }

      return {
        ok: true,
        value: createdRows,
      };
    },
    listForChangeRequest(tenantId, changeRequestId) {
      return runQuery<ProposedChangeRecord>(
        database,
        `
          SELECT *
          FROM proposed_changes
          WHERE tenant_id = $1
            AND change_request_id = $2
          ORDER BY created_at ASC
        `,
        [tenantId, changeRequestId],
      );
    },
  };
}
