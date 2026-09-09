export {
  ACTOR_ROLES,
  ACTOR_TYPES,
  APPROVAL_TASK_STATUSES,
  CHANGE_REQUEST_STATUSES,
  CHANGE_REQUEST_TYPES,
  DOCUMENT_CLASSIFICATIONS,
  DOCUMENT_STATUSES,
  INTEGRATION_OUTBOX_STATUSES,
  LEDGER_EVENT_TYPES,
  PERMISSION_KEYS,
  WORKFLOW_INTENTS,
  WORKFLOW_STATES,
  WORKFLOW_STATUSES,
  WORKFLOW_TRANSITIONS,
} from "@human-capital-management-suite/foundation";

export type {
  ActorRole,
  ActorType,
  ApprovalTaskStatus,
  ChangeRequestStatus,
  ChangeRequestType,
  DocumentClassification,
  DocumentStatus,
  IntegrationOutboxStatus,
  LedgerEventType,
  PermissionKey,
  WorkflowIntent,
  WorkflowState,
  WorkflowStatus,
  WorkflowTransition,
} from "@human-capital-management-suite/foundation";

export const TRANSITION_ATTEMPT_STATUSES = {
  IN_PROGRESS: "in_progress",
  COMPLETED: "completed",
  FAILED: "failed",
} as const;

export type TransitionAttemptStatus =
  (typeof TRANSITION_ATTEMPT_STATUSES)[keyof typeof TRANSITION_ATTEMPT_STATUSES];
