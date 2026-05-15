import type { ValueOf } from "../domain";

export const DOCUMENT_STATUSES = {
  PENDING_UPLOAD: "pending_upload",
  UPLOADED: "uploaded",
  REJECTED: "rejected",
  DELETED: "deleted",
} as const;

export type DocumentStatus = ValueOf<typeof DOCUMENT_STATUSES>;

export const DOCUMENT_CLASSIFICATIONS = {
  PUBLIC: "public",
  INTERNAL: "internal",
  CONFIDENTIAL: "confidential",
  SENSITIVE_PERSON_IDENTITY: "sensitive_person_identity",
} as const;

export type DocumentClassification = ValueOf<typeof DOCUMENT_CLASSIFICATIONS>;
