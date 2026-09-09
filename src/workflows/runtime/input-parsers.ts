import {
  WORKFLOW_TRANSITIONS,
  err,
  validationFailedError,
  type AppError,
  type Result,
  type WorkflowTransition,
} from "@human-capital-management-suite/foundation";
import { numberField, objectField, stringField } from "../shared/json-fields.js";
import type { ApprovalDecisionInput, EvidenceInput, TransitionBody } from "./types.js";

/**
 * Parses the standard workflow transition payload used by workflow-native APIs.
 */
export function parseTransitionBody(
  body: Record<string, unknown>,
): Result<TransitionBody, AppError> {
  const transition = stringField(body, "transition");
  const idempotencyKey = stringField(body, "idempotencyKey");
  const expectedVersion = numberField(body, "expectedVersion");
  const input = objectField(body, "input") ?? objectField(body, "payload") ?? {};
  const validTransitions = Object.values(WORKFLOW_TRANSITIONS);

  if (
    transition === undefined ||
    !validTransitions.includes(transition as WorkflowTransition) ||
    idempotencyKey === undefined ||
    expectedVersion === undefined
  ) {
    return err(
      validationFailedError({
        transition,
        idempotencyKey,
        expectedVersion,
      }),
    );
  }

  return {
    ok: true,
    value: {
      transition: transition as WorkflowTransition,
      idempotencyKey,
      expectedVersion,
      input,
    },
  };
}

/**
 * Parses evidence submission payloads for document-backed workflow steps.
 */
export function parseEvidenceInput(
  input: Record<string, unknown>,
): Result<EvidenceInput, AppError> {
  const documentId = stringField(input, "documentId");

  if (documentId === undefined) {
    return err(validationFailedError({ documentId }));
  }

  return {
    ok: true,
    value: { documentId },
  };
}

/**
 * Parses approval task decisions without relying on direct approval APIs.
 */
export function parseApprovalDecisionInput(
  input: Record<string, unknown>,
): Result<ApprovalDecisionInput, AppError> {
  const approvalTaskId = stringField(input, "approvalTaskId");
  const taskVersion = numberField(input, "taskVersion");
  const comment = stringField(input, "comment");
  const reason = stringField(input, "reason");

  if (approvalTaskId === undefined) {
    return err(validationFailedError({ approvalTaskId }));
  }

  return {
    ok: true,
    value: {
      approvalTaskId,
      ...(taskVersion !== undefined ? { taskVersion } : {}),
      ...(comment !== undefined ? { comment } : {}),
      ...(reason !== undefined ? { reason } : {}),
    },
  };
}
