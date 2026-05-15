import type { ValueOf } from "../domain";

export const CHANGE_REQUEST_TYPES = {
  HIRE: "hire",
  ONBOARDING: "onboarding",
  JOB_CHANGE: "job_change",
  MANAGER_ORG_CHANGE: "manager_org_change",
  COMPENSATION_CHANGE: "compensation_change",
  PROMOTION: "promotion",
  LEAVE: "leave",
  EMPLOYEE_DATA_CHANGE: "employee_data_change",
  HEADCOUNT_REQUISITION: "headcount_requisition",
  TERMINATION: "termination",
  REORG_BATCH: "reorg_batch",
  CUSTOM: "custom",
} as const;

export type ChangeRequestType = ValueOf<typeof CHANGE_REQUEST_TYPES>;

export const CHANGE_REQUEST_STATUSES = {
  DRAFT: "draft",
  NEEDS_DATA: "needs_data",
  PREFLIGHTED: "preflighted",
  SUBMITTED: "submitted",
  IN_APPROVAL: "in_approval",
  APPROVED: "approved",
  REJECTED: "rejected",
  CANCELED: "canceled",
  SIMULATED: "simulated",
  EXECUTING: "executing",
  EXECUTED: "executed",
  RECONCILING: "reconciling",
  RECONCILED: "reconciled",
  WAITING_REPAIR: "waiting_repair",
  CLOSED: "closed",
  SUPERSEDED: "superseded",
  FAILED: "failed",
} as const;

export type ChangeRequestStatus = ValueOf<typeof CHANGE_REQUEST_STATUSES>;
