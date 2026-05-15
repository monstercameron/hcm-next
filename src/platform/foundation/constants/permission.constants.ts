import type { ValueOf } from "../domain";

export const ACTOR_TYPES = {
  HUMAN: "human",
  SERVICE_ACCOUNT: "service_account",
  INTEGRATION: "integration",
  WORKFLOW: "workflow",
  AI_AGENT: "ai_agent",
  SYSTEM: "system",
} as const;

export type ActorType = ValueOf<typeof ACTOR_TYPES>;

export const ROLE_KEYS = {
  EMPLOYEE: "employee",
  HR_ADMIN: "hr_admin",
  SYSTEM: "system",
} as const;

export type RoleKey = ValueOf<typeof ROLE_KEYS>;

export const ACTOR_ROLES = ROLE_KEYS;

export type ActorRole = RoleKey;

export const PERMISSION_KEYS = {
  LEGAL_NAME_REQUEST: "employee_data_change.legal_name.request",
  LEGAL_NAME_PROVIDE_EVIDENCE: "employee_data_change.legal_name.provide_evidence",
  LEGAL_NAME_CANCEL_OWN: "employee_data_change.legal_name.cancel_own",
  LEGAL_NAME_APPROVE: "employee_data_change.legal_name.approve",
  LEGAL_NAME_REJECT: "employee_data_change.legal_name.reject",
  LEGAL_NAME_REQUEST_MORE_INFO: "employee_data_change.legal_name.request_more_info",
  LEGAL_NAME_EXECUTE: "employee_data_change.legal_name.execute",
  LEGAL_NAME_VIEW_EVIDENCE: "employee_data_change.legal_name.view_evidence",
  EMERGENCY_CONTACT_REQUEST: "employee_data_change.emergency_contact.request",
  EMERGENCY_CONTACT_CANCEL_OWN: "employee_data_change.emergency_contact.cancel_own",
  EMERGENCY_CONTACT_APPROVE: "employee_data_change.emergency_contact.approve",
  EMERGENCY_CONTACT_REJECT: "employee_data_change.emergency_contact.reject",
  EMERGENCY_CONTACT_EXECUTE: "employee_data_change.emergency_contact.execute",
} as const;

export type PermissionKey = ValueOf<typeof PERMISSION_KEYS>;
