import type { ValueOf } from "../domain";

export const LEDGER_EVENT_TYPES = {
  WORKFLOW_INTENT_STARTED: "WorkflowIntentStarted",
  WORKFLOW_TRANSITION_SUBMITTED: "WorkflowTransitionSubmitted",
  WORKFLOW_STATE_CHANGED: "WorkflowStateChanged",
  CHANGE_REQUEST_CREATED: "ChangeRequestCreated",
  PROPOSED_CHANGE_CREATED: "ProposedChangeCreated",
  NAME_CHANGE_PREFLIGHTED: "NameChangePreflighted",
  EMERGENCY_CONTACT_PREFLIGHTED: "EmergencyContactPreflighted",
  EVIDENCE_REQUESTED: "EvidenceRequested",
  DOCUMENT_CREATED: "DocumentCreated",
  EVIDENCE_PROVIDED: "EvidenceProvided",
  APPROVAL_TASK_CREATED: "ApprovalTaskCreated",
  CHANGE_REQUEST_SUBMITTED: "ChangeRequestSubmitted",
  APPROVAL_GRANTED: "ApprovalGranted",
  APPROVAL_REJECTED: "ApprovalRejected",
  MORE_INFORMATION_REQUESTED: "MoreInformationRequested",
  CHANGE_REQUEST_APPROVED: "ChangeRequestApproved",
  CHANGE_REQUEST_REJECTED: "ChangeRequestRejected",
  TRANSACTION_PLAN_CREATED: "TransactionPlanCreated",
  TRANSACTION_EXECUTION_STARTED: "TransactionExecutionStarted",
  PERSON_LEGAL_NAME_CHANGED: "PersonLegalNameChanged",
  EMPLOYEE_EMERGENCY_CONTACT_UPDATED: "EmployeeEmergencyContactUpdated",
  EMPLOYEE_PROJECTION_UPDATED: "EmployeeProjectionUpdated",
  EXTERNAL_WRITE_REQUESTED: "ExternalWriteRequested",
  TRANSACTION_EXECUTION_COMPLETED: "TransactionExecutionCompleted",
  WORKFLOW_COMPLETED: "WorkflowCompleted",
  WORKFLOW_CANCELED: "WorkflowCanceled",
  WORKFLOW_FAILED: "WorkflowFailed",
} as const;

export type LedgerEventType = ValueOf<typeof LEDGER_EVENT_TYPES>;
