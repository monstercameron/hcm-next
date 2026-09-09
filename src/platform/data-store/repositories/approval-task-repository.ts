import type { Result } from "@human-capital-management-suite/foundation";

import type { DatabaseClient } from "../client";
import { requireMutationRow, runQuery } from "./repository-utils";
import type { ApprovalTaskRecord, DatabaseJson } from "./repository-types";

export type CreateApprovalTaskInput = {
  approvalTaskId?: string;
  tenantId: string;
  changeRequestId: string;
  workflowInstanceId: string;
  assigneeActorId?: string | null;
  assigneeRole?: string | null;
  assigneeRelationship?: string | null;
  approvalType: string;
  status: string;
  dueAt?: Date | string | null;
  metadata?: DatabaseJson;
};

export type UpdateApprovalDecisionInput = {
  tenantId: string;
  approvalTaskId: string;
  status: string;
  decision: string;
  decisionReason?: string | null;
  comments?: string | null;
};

export type ApprovalTaskRepository = {
  /** Creates a human or system approval task. */
  create: (input: CreateApprovalTaskInput) => Promise<Result<ApprovalTaskRecord>>;
  /** Lists visible pending approval tasks by assignee role. */
  listPendingByRole: (
    tenantId: string,
    assigneeRole: string,
  ) => Promise<Result<ApprovalTaskRecord[]>>;
  /** Stores an approval decision. */
  updateDecision: (
    input: UpdateApprovalDecisionInput,
  ) => Promise<Result<ApprovalTaskRecord>>;
};

/**
 * Creates repository methods for approval tasks.
 */
export function createApprovalTaskRepository(
  database: DatabaseClient,
): ApprovalTaskRepository {
  return {
    async create(input) {
      const rowsResult = await runQuery<ApprovalTaskRecord>(
        database,
        `
          INSERT INTO approval_tasks (
            approval_task_id,
            tenant_id,
            change_request_id,
            workflow_instance_id,
            assignee_actor_id,
            assignee_role,
            assignee_relationship,
            approval_type,
            status,
            due_at,
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
            $11::jsonb
          )
          RETURNING *
        `,
        [
          input.approvalTaskId ?? null,
          input.tenantId,
          input.changeRequestId,
          input.workflowInstanceId,
          input.assigneeActorId ?? null,
          input.assigneeRole ?? null,
          input.assigneeRelationship ?? null,
          input.approvalType,
          input.status,
          input.dueAt ?? null,
          input.metadata ?? {},
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "ApprovalTask", input);
    },
    listPendingByRole(tenantId, assigneeRole) {
      return runQuery<ApprovalTaskRecord>(
        database,
        `
          SELECT *
          FROM approval_tasks
          WHERE tenant_id = $1
            AND assignee_role = $2
            AND status = 'pending'
          ORDER BY created_at ASC
        `,
        [tenantId, assigneeRole],
      );
    },
    async updateDecision(input) {
      const rowsResult = await runQuery<ApprovalTaskRecord>(
        database,
        `
          UPDATE approval_tasks
          SET status = $3,
              decision = $4,
              decision_reason = $5,
              comments = $6,
              decided_at = now()
          WHERE tenant_id = $1
            AND approval_task_id = $2
          RETURNING *
        `,
        [
          input.tenantId,
          input.approvalTaskId,
          input.status,
          input.decision,
          input.decisionReason ?? null,
          input.comments ?? null,
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "ApprovalTask", input);
    },
  };
}
