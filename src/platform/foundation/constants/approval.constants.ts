import type { ValueOf } from "../domain";

export const APPROVAL_TASK_STATUSES = {
  PENDING: "pending",
  APPROVED: "approved",
  REJECTED: "rejected",
  DELEGATED: "delegated",
  EXPIRED: "expired",
  CANCELED: "canceled",
  SKIPPED: "skipped",
} as const;

export type ApprovalTaskStatus = ValueOf<typeof APPROVAL_TASK_STATUSES>;
