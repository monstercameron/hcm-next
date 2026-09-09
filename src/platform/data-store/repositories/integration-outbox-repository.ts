import type { Result } from "@human-capital-management-suite/foundation";

import type { DatabaseClient } from "../client";
import { requireMutationRow, runQuery } from "./repository-utils";
import type { DatabaseJson, IntegrationOutboxRecord } from "./repository-types";

export type CreateIntegrationOutboxInput = {
  outboxId?: string;
  tenantId: string;
  integrationConnectionId?: string | null;
  changeRequestId?: string | null;
  transactionPlanId?: string | null;
  ledgerEventId?: string | null;
  destination: string;
  operation: string;
  requestPayload: DatabaseJson;
  status: string;
  maxAttempts?: number;
  nextAttemptAt?: Date | string | null;
  idempotencyKey: string;
};

export type IntegrationOutboxRepository = {
  /** Creates an outbox row for later integration side-effect execution. */
  create: (
    input: CreateIntegrationOutboxInput,
  ) => Promise<Result<IntegrationOutboxRecord>>;
  /** Lists pending outbox work in processing order. */
  listPending: (
    tenantId: string,
    limit: number,
  ) => Promise<Result<IntegrationOutboxRecord[]>>;
};

/**
 * Creates repository methods for integration outbox rows.
 */
export function createIntegrationOutboxRepository(
  database: DatabaseClient,
): IntegrationOutboxRepository {
  return {
    async create(input) {
      const rowsResult = await runQuery<IntegrationOutboxRecord>(
        database,
        `
          INSERT INTO integration_outbox (
            outbox_id,
            tenant_id,
            integration_connection_id,
            change_request_id,
            transaction_plan_id,
            ledger_event_id,
            destination,
            operation,
            request_payload,
            status,
            max_attempts,
            next_attempt_at,
            idempotency_key
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
            $9::jsonb,
            $10,
            COALESCE($11::integer, 5),
            $12,
            $13
          )
          RETURNING *
        `,
        [
          input.outboxId ?? null,
          input.tenantId,
          input.integrationConnectionId ?? null,
          input.changeRequestId ?? null,
          input.transactionPlanId ?? null,
          input.ledgerEventId ?? null,
          input.destination,
          input.operation,
          input.requestPayload,
          input.status,
          input.maxAttempts ?? 5,
          input.nextAttemptAt ?? null,
          input.idempotencyKey,
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "IntegrationOutbox", input);
    },
    listPending(tenantId, limit) {
      return runQuery<IntegrationOutboxRecord>(
        database,
        `
          SELECT *
          FROM integration_outbox
          WHERE tenant_id = $1
            AND status IN ('pending', 'failed')
          ORDER BY next_attempt_at NULLS FIRST, created_at ASC
          LIMIT $2
        `,
        [tenantId, limit],
      );
    },
  };
}
