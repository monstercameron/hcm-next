import type { Result } from "@hcm-next/foundation";

import type { DatabaseClient } from "../client";
import { findOne, requireMutationRow, runQuery } from "./repository-utils";
import type { DatabaseJson, WorkflowTransitionAttemptRecord } from "./repository-types";

export type CreateWorkflowTransitionAttemptInput = {
  tenantId: string;
  workflowInstanceId: string;
  transition: string;
  idempotencyKey: string;
  expectedVersion: number;
  actorId: string;
  requestPayload: DatabaseJson;
  status: string;
};

export type CompleteWorkflowTransitionAttemptInput = {
  tenantId: string;
  workflowInstanceId: string;
  idempotencyKey: string;
  responsePayload?: DatabaseJson | null;
  error?: DatabaseJson | null;
  status: string;
};

export type WorkflowTransitionAttemptRepository = {
  /** Finds a transition attempt by workflow and idempotency key. */
  findByIdempotencyKey: (
    tenantId: string,
    workflowInstanceId: string,
    idempotencyKey: string,
  ) => Promise<Result<WorkflowTransitionAttemptRecord>>;
  /** Creates a transition attempt before handler dispatch. */
  createStarted: (
    input: CreateWorkflowTransitionAttemptInput,
  ) => Promise<Result<WorkflowTransitionAttemptRecord>>;
  /** Stores the final transition attempt response or error. */
  complete: (
    input: CompleteWorkflowTransitionAttemptInput,
  ) => Promise<Result<WorkflowTransitionAttemptRecord>>;
};

/**
 * Creates repository methods for transition idempotency records.
 */
export function createWorkflowTransitionAttemptRepository(
  database: DatabaseClient,
): WorkflowTransitionAttemptRepository {
  return {
    findByIdempotencyKey(tenantId, workflowInstanceId, idempotencyKey) {
      return findOne<WorkflowTransitionAttemptRecord>(
        database,
        `
          SELECT *
          FROM workflow_transition_attempts
          WHERE tenant_id = $1
            AND workflow_instance_id = $2
            AND idempotency_key = $3
        `,
        [tenantId, workflowInstanceId, idempotencyKey],
        "WorkflowTransitionAttempt",
        { tenantId, workflowInstanceId, idempotencyKey },
      );
    },
    async createStarted(input) {
      const rowsResult = await runQuery<WorkflowTransitionAttemptRecord>(
        database,
        `
          INSERT INTO workflow_transition_attempts (
            tenant_id,
            workflow_instance_id,
            transition,
            idempotency_key,
            expected_version,
            actor_id,
            request_payload,
            status
          )
          VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
          RETURNING *
        `,
        [
          input.tenantId,
          input.workflowInstanceId,
          input.transition,
          input.idempotencyKey,
          input.expectedVersion,
          input.actorId,
          input.requestPayload,
          input.status,
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(
        rowsResult.value[0],
        "WorkflowTransitionAttempt",
        input,
      );
    },
    async complete(input) {
      const rowsResult = await runQuery<WorkflowTransitionAttemptRecord>(
        database,
        `
          UPDATE workflow_transition_attempts
          SET response_payload = $4::jsonb,
              error = $5::jsonb,
              status = $6,
              completed_at = now()
          WHERE tenant_id = $1
            AND workflow_instance_id = $2
            AND idempotency_key = $3
          RETURNING *
        `,
        [
          input.tenantId,
          input.workflowInstanceId,
          input.idempotencyKey,
          input.responsePayload ?? null,
          input.error ?? null,
          input.status,
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(
        rowsResult.value[0],
        "WorkflowTransitionAttempt",
        input,
      );
    },
  };
}
