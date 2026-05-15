import { DOCUMENT_STATUSES, LEDGER_EVENT_TYPES, PERMISSION_KEYS } from "../constants";
import type { RequestContext } from "../context";
import type {
  Actor,
  CreateDocumentRequest,
  DocumentRecord,
  JsonObject,
} from "../domain";
import { err, ok, permissionDeniedError, type Result } from "../result";
import {
  canViewEvidence,
  canViewWorkflowInstance,
  requireWorkflowPermission,
} from "../runtime/permissions";
import { parseDocumentRequest } from "../runtime/validation";
import type { WorkflowRuntimeStore } from "../storage/workflow-store";
import { buildWorkflowLedgerEvent } from "./ledger-event-builder";

export type DocumentService = {
  /** Creates fake V0 document metadata for a workflow instance. */
  createDocument(
    context: RequestContext,
    actor: Actor,
    body: unknown,
  ): Promise<Result<DocumentRecord>>;

  /** Returns permission-filtered V0 document metadata. */
  getDocument(
    context: RequestContext,
    actor: Actor,
    documentId: string,
  ): Promise<Result<DocumentRecord>>;
};

export function createDocumentService(store: WorkflowRuntimeStore): DocumentService {
  return new DefaultDocumentService(store);
}

class DefaultDocumentService implements DocumentService {
  constructor(private readonly store: WorkflowRuntimeStore) {}

  async createDocument(
    context: RequestContext,
    actor: Actor,
    body: unknown,
  ): Promise<Result<DocumentRecord>> {
    const requestResult = parseDocumentRequest(body);
    if (!requestResult.ok) {
      return requestResult;
    }

    return this.store.runInTransaction(async (store) => {
      const workflowResult = await store.findWorkflowInstanceById(
        context.tenantId,
        requestResult.value.workflowInstanceId,
      );
      if (!workflowResult.ok) {
        return workflowResult;
      }

      const permissionResult = requireWorkflowPermission(
        context,
        actor,
        PERMISSION_KEYS.LEGAL_NAME_PROVIDE_EVIDENCE,
        { workflowInstance: workflowResult.value },
      );
      if (!permissionResult.ok) {
        return permissionResult;
      }

      const documentCreateInput = buildDocumentCreateInput(
        context,
        actor,
        requestResult.value,
      );
      const documentResult = await store.createDocument(documentCreateInput);
      if (!documentResult.ok) {
        return documentResult;
      }

      const ledgerResult = await store.appendLedgerEvents([
        buildWorkflowLedgerEvent({
          context,
          workflowInstance: workflowResult.value,
          permissionSnapshot: permissionResult.value.snapshot,
          eventType: LEDGER_EVENT_TYPES.DOCUMENT_CREATED,
          subjectType: "document",
          subjectId: documentResult.value.documentId,
          changeRequestId: workflowResult.value.changeRequestId,
          payload: {
            documentId: documentResult.value.documentId,
            purpose: documentResult.value.purpose,
            classification: documentResult.value.classification,
            status: documentResult.value.status,
          },
        }),
      ]);
      if (!ledgerResult.ok) {
        return ledgerResult;
      }

      return ok(documentResult.value);
    });
  }

  async getDocument(
    context: RequestContext,
    actor: Actor,
    documentId: string,
  ): Promise<Result<DocumentRecord>> {
    const documentResult = await this.store.findDocumentById(
      context.tenantId,
      documentId,
    );
    if (!documentResult.ok) {
      return documentResult;
    }

    const workflowResult = await this.store.findWorkflowInstanceById(
      context.tenantId,
      documentResult.value.workflowInstanceId,
    );
    if (!workflowResult.ok) {
      return workflowResult;
    }

    if (!canViewWorkflowInstance(context, actor, workflowResult.value)) {
      return err(permissionDeniedError({ documentId }));
    }

    if (!canViewEvidence(context, actor, workflowResult.value)) {
      return err(permissionDeniedError({ documentId }));
    }

    return ok(documentResult.value);
  }
}

function buildDocumentCreateInput(
  context: RequestContext,
  actor: Actor,
  request: CreateDocumentRequest,
) {
  return {
    tenantId: context.tenantId,
    workflowInstanceId: request.workflowInstanceId,
    purpose: request.purpose,
    filename: request.filename,
    contentType: request.contentType,
    classification: request.classification,
    status: DOCUMENT_STATUSES.UPLOADED,
    uploadedByActorId: actor.actorId,
    metadata: (request.metadata ?? {}) as JsonObject,
  };
}
