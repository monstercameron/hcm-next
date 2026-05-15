import type { ValueOf } from "../domain";

export const INTEGRATION_OUTBOX_STATUSES = {
  PENDING: "pending",
  PROCESSING: "processing",
  SUCCEEDED: "succeeded",
  FAILED: "failed",
  DEAD_LETTER: "dead_letter",
  CANCELED: "canceled",
} as const;

export type IntegrationOutboxStatus = ValueOf<typeof INTEGRATION_OUTBOX_STATUSES>;
