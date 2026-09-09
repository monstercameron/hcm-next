import type { Result } from "@human-capital-management-suite/foundation";

import type { DatabaseClient } from "../client";
import { findOne, requireMutationRow, runQuery } from "./repository-utils";
import type { DatabaseJson, TransactionPlanRecord } from "./repository-types";

export type CreateTransactionPlanInput = {
  transactionPlanId?: string;
  tenantId: string;
  changeRequestId: string;
  status: string;
  planVersion?: number;
  steps?: readonly unknown[];
  internalWrites?: readonly unknown[];
  projectionPatches?: readonly unknown[];
  externalWrites?: readonly unknown[];
  rollbackPlan?: DatabaseJson;
  compensationPlan?: DatabaseJson;
  idempotencyKeys?: DatabaseJson;
  simulationResult?: DatabaseJson;
  executionResult?: DatabaseJson;
  reconciliationResult?: DatabaseJson;
  createdBy: string;
};

export type TransactionPlanRepository = {
  /** Creates an auditable transaction plan after approval. */
  create: (input: CreateTransactionPlanInput) => Promise<Result<TransactionPlanRecord>>;
  /** Finds the transaction plan for a change request. */
  findByChangeRequestId: (
    tenantId: string,
    changeRequestId: string,
  ) => Promise<Result<TransactionPlanRecord>>;
};

/**
 * Creates repository methods for transaction plans.
 */
export function createTransactionPlanRepository(
  database: DatabaseClient,
): TransactionPlanRepository {
  return {
    async create(input) {
      const rowsResult = await runQuery<TransactionPlanRecord>(
        database,
        `
          INSERT INTO transaction_plans (
            transaction_plan_id,
            tenant_id,
            change_request_id,
            status,
            plan_version,
            steps,
            internal_writes,
            projection_patches,
            external_writes,
            rollback_plan,
            compensation_plan,
            idempotency_keys,
            simulation_result,
            execution_result,
            reconciliation_result,
            created_by,
            updated_by
          )
          VALUES (
            COALESCE($1::uuid, gen_random_uuid()),
            $2,
            $3,
            $4,
            COALESCE($5::integer, 1),
            $6::jsonb,
            $7::jsonb,
            $8::jsonb,
            $9::jsonb,
            $10::jsonb,
            $11::jsonb,
            $12::jsonb,
            $13::jsonb,
            $14::jsonb,
            $15::jsonb,
            $16,
            $16
          )
          RETURNING *
        `,
        [
          input.transactionPlanId ?? null,
          input.tenantId,
          input.changeRequestId,
          input.status,
          input.planVersion ?? 1,
          input.steps ?? [],
          input.internalWrites ?? [],
          input.projectionPatches ?? [],
          input.externalWrites ?? [],
          input.rollbackPlan ?? {},
          input.compensationPlan ?? {},
          input.idempotencyKeys ?? {},
          input.simulationResult ?? {},
          input.executionResult ?? {},
          input.reconciliationResult ?? {},
          input.createdBy,
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "TransactionPlan", input);
    },
    findByChangeRequestId(tenantId, changeRequestId) {
      return findOne<TransactionPlanRecord>(
        database,
        `
          SELECT *
          FROM transaction_plans
          WHERE tenant_id = $1
            AND change_request_id = $2
          ORDER BY created_at DESC
          LIMIT 1
        `,
        [tenantId, changeRequestId],
        "TransactionPlan",
        { tenantId, changeRequestId },
      );
    },
  };
}
