import type { Result } from "@human-capital-management-suite/foundation";

import type { DatabaseClient } from "../client";
import { requireMutationRow, runQuery } from "./repository-utils";
import type { DatabaseJson, LedgerEventRecord } from "./repository-types";

export type AppendLedgerEventInput = {
  tenantId: string;
  environmentId?: string | null;
  eventType: string;
  eventVersion?: number;
  subjectType: string;
  subjectId: string;
  effectiveAt?: Date | string | null;
  actorType: string;
  actorId?: string | null;
  actorRole?: string | null;
  relationshipContext?: DatabaseJson;
  workflowInstanceId?: string | null;
  workflowDefinitionId?: string | null;
  workflowVersionId?: string | null;
  changeRequestId?: string | null;
  transactionPlanId?: string | null;
  approvalTaskId?: string | null;
  blockName?: string | null;
  blockVersion?: string | null;
  correlationId: string;
  causationId?: string | null;
  idempotencyKey?: string | null;
  permissionSnapshot?: DatabaseJson;
  aiVisibilitySnapshot?: DatabaseJson;
  payload?: DatabaseJson;
  previousHash?: string | null;
  eventHash?: string | null;
};

export type LedgerRepository = {
  /** Appends one immutable business ledger event. */
  appendEvent: (input: AppendLedgerEventInput) => Promise<Result<LedgerEventRecord>>;
  /** Lists ledger events for a workflow instance in sequence order. */
  listByWorkflowInstance: (
    tenantId: string,
    workflowInstanceId: string,
  ) => Promise<Result<LedgerEventRecord[]>>;
};

/**
 * Creates repository methods for immutable ledger event access.
 */
export function createLedgerRepository(database: DatabaseClient): LedgerRepository {
  return {
    async appendEvent(input) {
      const rowsResult = await runQuery<LedgerEventRecord>(
        database,
        `
          INSERT INTO ledger_events (
            tenant_id,
            environment_id,
            event_type,
            event_version,
            subject_type,
            subject_id,
            effective_at,
            actor_type,
            actor_id,
            actor_role,
            relationship_context,
            workflow_instance_id,
            workflow_definition_id,
            workflow_version_id,
            change_request_id,
            transaction_plan_id,
            approval_task_id,
            block_name,
            block_version,
            correlation_id,
            causation_id,
            idempotency_key,
            permission_snapshot,
            ai_visibility_snapshot,
            payload,
            previous_hash,
            event_hash
          )
          VALUES (
            $1,
            $2,
            $3,
            COALESCE($4::integer, 1),
            $5,
            $6,
            $7,
            $8,
            $9,
            $10,
            $11::jsonb,
            $12,
            $13,
            $14,
            $15,
            $16,
            $17,
            $18,
            $19,
            $20,
            $21,
            $22,
            $23::jsonb,
            $24::jsonb,
            $25::jsonb,
            $26,
            $27
          )
          RETURNING *
        `,
        [
          input.tenantId,
          input.environmentId ?? null,
          input.eventType,
          input.eventVersion ?? 1,
          input.subjectType,
          input.subjectId,
          input.effectiveAt ?? null,
          input.actorType,
          input.actorId ?? null,
          input.actorRole ?? null,
          input.relationshipContext ?? {},
          input.workflowInstanceId ?? null,
          input.workflowDefinitionId ?? null,
          input.workflowVersionId ?? null,
          input.changeRequestId ?? null,
          input.transactionPlanId ?? null,
          input.approvalTaskId ?? null,
          input.blockName ?? null,
          input.blockVersion ?? null,
          input.correlationId,
          input.causationId ?? null,
          input.idempotencyKey ?? null,
          input.permissionSnapshot ?? {},
          input.aiVisibilitySnapshot ?? {},
          input.payload ?? {},
          input.previousHash ?? null,
          input.eventHash ?? null,
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "LedgerEvent", input);
    },
    listByWorkflowInstance(tenantId, workflowInstanceId) {
      return runQuery<LedgerEventRecord>(
        database,
        `
          SELECT *
          FROM ledger_events
          WHERE tenant_id = $1
            AND workflow_instance_id = $2
          ORDER BY event_sequence ASC
        `,
        [tenantId, workflowInstanceId],
      );
    },
  };
}
