import type { Result } from "@human-capital-management-suite/foundation";

import type { DatabaseClient } from "../client";
import { findOne, requireMutationRow, runQuery } from "./repository-utils";
import type { ChangeRequestRecord, DatabaseJson } from "./repository-types";

export type CreateChangeRequestInput = {
  changeRequestId?: string;
  tenantId: string;
  environmentId: string;
  changeType: string;
  targetWorkerId: string;
  requesterActorId: string;
  effectiveAt?: Date | string | null;
  businessReason?: string | null;
  status: string;
  currentSnapshot?: DatabaseJson;
  proposedSnapshot?: DatabaseJson;
  preflightResult?: DatabaseJson;
  workflowDefinitionId: string;
  workflowVersionId: string;
  createdBy: string;
  metadata?: DatabaseJson;
};

export type UpdateChangeRequestStatusInput = {
  tenantId: string;
  changeRequestId: string;
  status: string;
  updatedBy: string;
  metadata?: DatabaseJson;
};

export type ChangeRequestRepository = {
  /** Creates a business mutation record inside a workflow transition. */
  create: (input: CreateChangeRequestInput) => Promise<Result<ChangeRequestRecord>>;
  /** Finds a change request by primary identifier. */
  findById: (
    tenantId: string,
    changeRequestId: string,
  ) => Promise<Result<ChangeRequestRecord>>;
  /** Updates the change request status without transition business logic. */
  updateStatus: (
    input: UpdateChangeRequestStatusInput,
  ) => Promise<Result<ChangeRequestRecord>>;
};

/**
 * Creates repository methods for change request persistence.
 */
export function createChangeRequestRepository(
  database: DatabaseClient,
): ChangeRequestRepository {
  return {
    async create(input) {
      const rowsResult = await runQuery<ChangeRequestRecord>(
        database,
        `
          INSERT INTO change_requests (
            change_request_id,
            tenant_id,
            environment_id,
            change_type,
            target_worker_id,
            requester_actor_id,
            effective_at,
            business_reason,
            status,
            current_snapshot,
            proposed_snapshot,
            preflight_result,
            workflow_definition_id,
            workflow_version_id,
            created_by,
            updated_by,
            metadata
          )
          VALUES (
            COALESCE($1::uuid, gen_random_uuid()),
            $2,
            $3,
            $4,
            $5,
            $6,
            $7,
            $8,
            $9,
            $10::jsonb,
            $11::jsonb,
            $12::jsonb,
            $13,
            $14,
            $15,
            $15,
            $16::jsonb
          )
          RETURNING *
        `,
        [
          input.changeRequestId ?? null,
          input.tenantId,
          input.environmentId,
          input.changeType,
          input.targetWorkerId,
          input.requesterActorId,
          input.effectiveAt ?? null,
          input.businessReason ?? null,
          input.status,
          input.currentSnapshot ?? {},
          input.proposedSnapshot ?? {},
          input.preflightResult ?? {},
          input.workflowDefinitionId,
          input.workflowVersionId,
          input.createdBy,
          input.metadata ?? {},
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "ChangeRequest", input);
    },
    findById(tenantId, changeRequestId) {
      return findOne<ChangeRequestRecord>(
        database,
        `
          SELECT *
          FROM change_requests
          WHERE tenant_id = $1
            AND change_request_id = $2
        `,
        [tenantId, changeRequestId],
        "ChangeRequest",
        { tenantId, changeRequestId },
      );
    },
    async updateStatus(input) {
      const rowsResult = await runQuery<ChangeRequestRecord>(
        database,
        `
          UPDATE change_requests
          SET status = $3,
              updated_by = $4,
              metadata = metadata || $5::jsonb,
              version = version + 1,
              updated_at = now()
          WHERE tenant_id = $1
            AND change_request_id = $2
          RETURNING *
        `,
        [
          input.tenantId,
          input.changeRequestId,
          input.status,
          input.updatedBy,
          input.metadata ?? {},
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "ChangeRequest", input);
    },
  };
}
