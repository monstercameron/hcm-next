import type { Result } from "@hcm-next/foundation";

import type { DatabaseClient } from "../client";
import { findOne, requireMutationRow, runQuery } from "./repository-utils";
import type { DatabaseJson, WorkflowInstanceRecord } from "./repository-types";

export type CreateWorkflowInstanceInput = {
  workflowInstanceId?: string;
  tenantId: string;
  environmentId: string;
  workflowDefinitionId: string;
  workflowVersionId: string;
  intent: string;
  subjectType: string;
  subjectId: string;
  status: string;
  state: string;
  requesterActorId: string;
  currentInteraction: DatabaseJson;
  context: DatabaseJson;
  correlationId: string;
  metadata?: DatabaseJson;
};

export type UpdateWorkflowInstanceStateInput = {
  tenantId: string;
  workflowInstanceId: string;
  status: string;
  state: string;
  expectedVersion: number;
  currentInteraction: DatabaseJson;
  changeRequestId?: string | null;
  metadata?: DatabaseJson;
};

export type WorkflowInstanceRepository = {
  /** Creates a durable workflow runtime record. */
  create: (
    input: CreateWorkflowInstanceInput,
  ) => Promise<Result<WorkflowInstanceRecord>>;
  /** Finds a workflow runtime record by primary identifier. */
  findById: (
    tenantId: string,
    workflowInstanceId: string,
  ) => Promise<Result<WorkflowInstanceRecord>>;
  /** Updates state and increments the optimistic workflow version. */
  updateState: (
    input: UpdateWorkflowInstanceStateInput,
  ) => Promise<Result<WorkflowInstanceRecord>>;
};

/**
 * Creates repository methods for workflow instance persistence.
 */
export function createWorkflowInstanceRepository(
  database: DatabaseClient,
): WorkflowInstanceRepository {
  return {
    async create(input) {
      const rowsResult = await runQuery<WorkflowInstanceRecord>(
        database,
        `
          INSERT INTO workflow_instances (
            workflow_instance_id,
            tenant_id,
            environment_id,
            workflow_definition_id,
            workflow_version_id,
            intent,
            subject_type,
            subject_id,
            status,
            state,
            requester_actor_id,
            current_interaction,
            context,
            correlation_id,
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
            $10,
            $11,
            $12::jsonb,
            $13::jsonb,
            $14,
            $15::jsonb
          )
          RETURNING *
        `,
        [
          input.workflowInstanceId ?? null,
          input.tenantId,
          input.environmentId,
          input.workflowDefinitionId,
          input.workflowVersionId,
          input.intent,
          input.subjectType,
          input.subjectId,
          input.status,
          input.state,
          input.requesterActorId,
          input.currentInteraction,
          input.context,
          input.correlationId,
          input.metadata ?? {},
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "WorkflowInstance", input);
    },
    findById(tenantId, workflowInstanceId) {
      return findOne<WorkflowInstanceRecord>(
        database,
        `
          SELECT *
          FROM workflow_instances
          WHERE tenant_id = $1
            AND workflow_instance_id = $2
        `,
        [tenantId, workflowInstanceId],
        "WorkflowInstance",
        { tenantId, workflowInstanceId },
      );
    },
    async updateState(input) {
      const rowsResult = await runQuery<WorkflowInstanceRecord>(
        database,
        `
          UPDATE workflow_instances
          SET status = $3,
              state = $4,
              version = version + 1,
              current_interaction = $6::jsonb,
              change_request_id = COALESCE($7::uuid, change_request_id),
              metadata = metadata || $8::jsonb,
              updated_at = now()
          WHERE tenant_id = $1
            AND workflow_instance_id = $2
            AND version = $5
          RETURNING *
        `,
        [
          input.tenantId,
          input.workflowInstanceId,
          input.status,
          input.state,
          input.expectedVersion,
          input.currentInteraction,
          input.changeRequestId ?? null,
          input.metadata ?? {},
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "WorkflowInstance", input);
    },
  };
}
