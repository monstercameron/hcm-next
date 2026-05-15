import type { ValueOf } from "../domain";

export const WORKFLOW_INTENTS = {
  EMPLOYEE_LEGAL_NAME_CHANGE: "employee.legal_name.change",
  EMPLOYEE_EMERGENCY_CONTACT_UPDATE: "employee.emergency_contact.update",
  EMPLOYEE_CONTACT_INFO_UPDATE: "employee.contact_info.update",
  EMPLOYEE_COMPENSATION_CHANGE: "employee.compensation.change",
  EMPLOYEE_ORG_TRANSFER_COMPENSATION_CHANGE:
    "employee.org_transfer_compensation_change",
  POSITION_HEADCOUNT_REQUISITION_APPROVAL: "position.headcount_requisition.approval",
} as const;

export type WorkflowIntent = ValueOf<typeof WORKFLOW_INTENTS>;

export const WORKFLOW_STATES = {
  COLLECTING_INPUT: "collecting_input",
  COLLECTING_EVIDENCE: "collecting_evidence",
  WAITING_APPROVAL: "waiting_approval",
  WAITING_SOURCE_MANAGER_APPROVAL: "waiting_source_manager_approval",
  WAITING_DESTINATION_MANAGER_APPROVAL: "waiting_destination_manager_approval",
  WAITING_FINANCE_APPROVAL: "waiting_finance_approval",
  WAITING_COMPENSATION_APPROVAL: "waiting_compensation_approval",
  WAITING_MEDICAL_DIRECTOR_APPROVAL: "waiting_medical_director_approval",
  WAITING_SYNC_APPROVAL: "waiting_sync_approval",
  WAITING_ASYNC_APPROVAL: "waiting_async_approval",
  APPROVAL_GATE_PASSED: "approval_gate_passed",
  APPROVAL_GATE_FAILED: "approval_gate_failed",
  WAITING_APPROVAL_REPAIR: "waiting_approval_repair",
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
