import {
  WORKFLOW_INTENTS,
  WORKFLOW_TRANSITIONS,
  type WorkflowIntent,
  type WorkflowTransition,
} from "../constants";
import type {
  CreateDocumentRequest,
  JsonObject,
  LegalName,
  WorkflowIntentRequest,
  WorkflowSubject,
  WorkflowTransitionRequest,
} from "../domain";
import { err, ok, validationFailedError, type Result } from "../result";
import { isJsonObject } from "./json";

export type LegalNameInputPayload = {
  newLegalName: LegalName;
  effectiveAt: string;
  businessReason: string;
};

export function parseWorkflowIntentRequest(
  body: unknown,
): Result<{ intent: WorkflowIntent; subject: WorkflowSubject }> {
  if (!isJsonObject(body)) {
    return err(validationFailedError({ field: "body" }));
  }

  const request = body as WorkflowIntentRequest;
  if (request.intent !== WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE) {
    return err(validationFailedError({ field: "intent" }));
  }

  if (request.subject?.type !== "worker" || !request.subject.id) {
    return err(validationFailedError({ field: "subject" }));
  }

  return ok({
    intent: WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    subject: { type: "worker", id: request.subject.id },
  });
}

export function parseTransitionRequest(
  body: unknown,
): Result<WorkflowTransitionRequest & { transition: WorkflowTransition }> {
  if (!isJsonObject(body)) {
    return err(validationFailedError({ field: "body" }));
  }

  const request = body as WorkflowTransitionRequest;
  if (
    !Object.values(WORKFLOW_TRANSITIONS).includes(
      request.transition as WorkflowTransition,
    )
  ) {
    return err(validationFailedError({ field: "transition" }));
  }

  if (!request.idempotencyKey || typeof request.idempotencyKey !== "string") {
    return err(validationFailedError({ field: "idempotencyKey" }));
  }

  if (!Number.isInteger(request.expectedVersion) || request.expectedVersion < 1) {
    return err(validationFailedError({ field: "expectedVersion" }));
  }

  if (!isJsonObject(request.payload)) {
    return err(validationFailedError({ field: "payload" }));
  }

  return ok({
    transition: request.transition as WorkflowTransition,
    idempotencyKey: request.idempotencyKey,
    expectedVersion: request.expectedVersion,
    payload: request.payload,
  });
}

export function parseLegalNameInputPayload(
  payload: JsonObject,
): Result<LegalNameInputPayload> {
  const newLegalName = payload.newLegalName;
  const validationErrors: Record<string, string> = {};

  if (!isJsonObject(newLegalName)) {
    validationErrors.newLegalName = "A new legal name is required.";
  }

  const firstName = isJsonObject(newLegalName) ? stringValue(newLegalName.first) : "";
  const middleName = isJsonObject(newLegalName)
    ? nullableStringValue(newLegalName.middle)
    : null;
  const lastName = isJsonObject(newLegalName) ? stringValue(newLegalName.last) : "";
  const effectiveAt = stringValue(payload.effectiveAt);
  const businessReason = stringValue(payload.businessReason);

  if (!firstName) {
    validationErrors["newLegalName.first"] = "First name is required.";
  }

  if (!lastName) {
    validationErrors["newLegalName.last"] = "Last name is required.";
  }

  if (!effectiveAt) {
    validationErrors.effectiveAt = "Effective date is required.";
  }

  if (!businessReason) {
    validationErrors.businessReason = "Business reason is required.";
  }

  if (Object.keys(validationErrors).length > 0) {
    return err(validationFailedError({ validationErrors }));
  }

  return ok({
    newLegalName: {
      first: firstName,
      middle: middleName,
      last: lastName,
    },
    effectiveAt,
    businessReason,
  });
}

export function parseDocumentRequest(body: unknown): Result<CreateDocumentRequest> {
  if (!isJsonObject(body)) {
    return err(validationFailedError({ field: "body" }));
  }

  const purpose = stringValue(body.purpose);
  const filename = stringValue(body.filename);
  const contentType = stringValue(body.contentType);
  const classification = stringValue(body.classification);
  const workflowInstanceId = stringValue(body.workflowInstanceId);
  const validationErrors: Record<string, string> = {};

  if (!purpose) {
    validationErrors.purpose = "Document purpose is required.";
  }

  if (!filename) {
    validationErrors.filename = "Filename is required.";
  }

  if (!contentType) {
    validationErrors.contentType = "Content type is required.";
  }

  if (!classification) {
    validationErrors.classification = "Classification is required.";
  }

  if (!workflowInstanceId) {
    validationErrors.workflowInstanceId = "Workflow instance ID is required.";
  }

  if (Object.keys(validationErrors).length > 0) {
    return err(validationFailedError({ validationErrors }));
  }

  return ok({
    purpose,
    filename,
    contentType,
    classification,
    workflowInstanceId,
    metadata: isJsonObject(body.metadata) ? body.metadata : {},
  });
}

export function requireStringPayloadField(
  payload: JsonObject,
  field: string,
): Result<string> {
  const value = stringValue(payload[field]);
  if (!value) {
    return err(validationFailedError({ field }));
  }

  return ok(value);
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function nullableStringValue(value: unknown): string | null {
  if (value === null || value === undefined) {
    return null;
  }

  const normalizedValue = stringValue(value);
  return normalizedValue.length > 0 ? normalizedValue : null;
}
