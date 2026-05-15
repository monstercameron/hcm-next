import type { ValueOf } from "../domain";

export const WORKFLOW_INTENTS = {
  EMPLOYEE_LEGAL_NAME_CHANGE: "employee.legal_name.change",
  EMPLOYEE_EMERGENCY_CONTACT_UPDATE: "employee.emergency_contact.update",
} as const;

export type WorkflowIntent = ValueOf<typeof WORKFLOW_INTENTS>;

export const WORKFLOW_STATES = {
  COLLECTING_INPUT: "collecting_input",
  COLLECTING_EVIDENCE: "collecting_evidence",
  WAITING_APPROVAL: "waiting_approval",
  APPROVED: "approved",
  EXECUTING: "executing",
  EXECUTED: "executed",
  REJECTED: "rejected",
  CANCELED: "canceled",
  FAILED: "failed",
  WAITING_REPAIR: "waiting_repair",
} as const;

export type WorkflowState = ValueOf<typeof WORKFLOW_STATES>;

export const WORKFLOW_STATUSES = {
  ACTIVE: "active",
  WAITING: "waiting",
  COMPLETED: "completed",
  REJECTED: "rejected",
  CANCELED: "canceled",
  FAILED: "failed",
  SUPERSEDED: "superseded",
  WAITING_REPAIR: "waiting_repair",
} as const;

export type WorkflowStatus = ValueOf<typeof WORKFLOW_STATUSES>;

export const WORKFLOW_TRANSITIONS = {
  SUBMIT_INPUT: "submit_input",
  PROVIDE_EVIDENCE: "provide_evidence",
  APPROVE: "approve",
  REJECT: "reject",
  REQUEST_MORE_INFO: "request_more_info",
  CANCEL: "cancel",
  EXECUTE: "execute",
} as const;

export type WorkflowTransition = ValueOf<typeof WORKFLOW_TRANSITIONS>;
