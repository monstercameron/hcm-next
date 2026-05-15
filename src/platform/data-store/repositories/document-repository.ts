import type { Result } from "@hcm-next/foundation";

import type { DatabaseClient } from "../client";
import { findOne, requireMutationRow, runQuery } from "./repository-utils";
import type {
  DatabaseJson,
  DocumentRecord,
  WorkflowInstanceDocumentRecord,
} from "./repository-types";

export type CreateDocumentInput = {
  documentId?: string;
  tenantId: string;
  environmentId: string;
  workflowInstanceId?: string | null;
  createdByActorId: string;
  purpose: string;
  filename: string;
  contentType: string;
  classification: string;
  status: string;
  storageKey?: string | null;
  metadata?: DatabaseJson;
};

export type LinkDocumentToWorkflowInput = {
  tenantId: string;
  workflowInstanceId: string;
  documentId: string;
  changeRequestId?: string | null;
  purpose: string;
  attachedByActorId: string;
};

export type DocumentRepository = {
  /** Creates document metadata without performing object storage side effects. */
  create: (input: CreateDocumentInput) => Promise<Result<DocumentRecord>>;
  /** Finds document metadata by primary identifier. */
  findById: (tenantId: string, documentId: string) => Promise<Result<DocumentRecord>>;
  /** Links a document to a workflow instance as evidence. */
  linkToWorkflowInstance: (
    input: LinkDocumentToWorkflowInput,
  ) => Promise<Result<WorkflowInstanceDocumentRecord>>;
};

/**
 * Creates repository methods for document metadata.
 */
export function createDocumentRepository(database: DatabaseClient): DocumentRepository {
  return {
    async create(input) {
      const rowsResult = await runQuery<DocumentRecord>(
        database,
        `
          INSERT INTO documents (
            document_id,
            tenant_id,
            environment_id,
            workflow_instance_id,
            created_by_actor_id,
            purpose,
            filename,
            content_type,
            classification,
            status,
            storage_key,
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
            $12::jsonb
          )
          RETURNING *
        `,
        [
          input.documentId ?? null,
          input.tenantId,
          input.environmentId,
          input.workflowInstanceId ?? null,
          input.createdByActorId,
          input.purpose,
          input.filename,
          input.contentType,
          input.classification,
          input.status,
          input.storageKey ?? null,
          input.metadata ?? {},
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "Document", input);
    },
    findById(tenantId, documentId) {
      return findOne<DocumentRecord>(
        database,
        `
          SELECT *
          FROM documents
          WHERE tenant_id = $1
            AND document_id = $2
        `,
        [tenantId, documentId],
        "Document",
        { tenantId, documentId },
      );
    },
    async linkToWorkflowInstance(input) {
      const rowsResult = await runQuery<WorkflowInstanceDocumentRecord>(
        database,
        `
          INSERT INTO workflow_instance_documents (
            tenant_id,
            workflow_instance_id,
            document_id,
            change_request_id,
            purpose,
            attached_by_actor_id
          )
          VALUES ($1, $2, $3, $4, $5, $6)
          ON CONFLICT (tenant_id, workflow_instance_id, document_id) DO UPDATE
          SET change_request_id = EXCLUDED.change_request_id,
              purpose = EXCLUDED.purpose
          RETURNING *
        `,
        [
          input.tenantId,
          input.workflowInstanceId,
          input.documentId,
          input.changeRequestId ?? null,
          input.purpose,
          input.attachedByActorId,
        ],
      );

      if (!rowsResult.ok) {
        return rowsResult;
      }

      return requireMutationRow(rowsResult.value[0], "WorkflowInstanceDocument", input);
    },
  };
}
