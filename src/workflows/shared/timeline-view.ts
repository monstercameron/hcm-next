import {
  LEDGER_EVENT_TYPES,
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  ActorRecord,
  EmployeeProjectionRecord,
  LedgerEventRecord,
} from "@hcm-next/data-store";
import {
  canViewEmployee,
  type EmployeeAccessInput,
  type EmployeeFieldGroup as EmployeeAccessFieldGroup,
} from "./employee-access.js";
import { objectField } from "./json-fields.js";
import type { WorkflowConfig } from "./workflow-config.js";

export type TimelineView = "business" | "audit" | "debug";

export type TimelineVisibilityContext = {
  actor: ActorRecord;
  targetProjection: EmployeeProjectionRecord;
  access: EmployeeAccessInput;
};

const timelineViews = {
  BUSINESS: "business",
  AUDIT: "audit",
  DEBUG: "debug",
} as const satisfies Record<string, TimelineView>;

const debugTimelineEventTypes = new Set<string>([
  LEDGER_EVENT_TYPES.WORKFLOW_TRANSITION_SUBMITTED,
  LEDGER_EVENT_TYPES.WORKFLOW_STATE_CHANGED,
  LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_STARTED,
  LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_COMPLETED,
]);

/**
 * Parses a requested timeline view with a business-view default.
 */
export function parseTimelineView(view?: string): Result<TimelineView, AppError> {
  if (view === undefined || view.trim() === "") {
    return ok(timelineViews.BUSINESS);
  }

  const normalizedView = view.trim().toLowerCase();
  const allowedViews = Object.values(timelineViews);

  if (!allowedViews.includes(normalizedView as TimelineView)) {
    return err(
      validationFailedError({
        view,
        allowedViews,
      }),
    );
  }

  return ok(normalizedView as TimelineView);
}

/**
 * Builds a timeline view from immutable ledger events and actor visibility context.
 */
export function buildTimelineView(
  ledgerEvents: LedgerEventRecord[],
  view: TimelineView,
  workflowConfig: WorkflowConfig,
  visibilityContext?: TimelineVisibilityContext,
): Record<string, unknown>[] {
  if (view === timelineViews.AUDIT) {
    return ledgerEvents;
  }

  if (view === timelineViews.DEBUG) {
    return ledgerEvents
      .filter((event) => debugTimelineEventTypes.has(event.eventType))
      .map((event) => createDebugTimelineEntry(event));
  }

  const businessTimelineEventTypes = new Set<string>(
    workflowConfig.timeline.businessEvents,
  );

  return ledgerEvents
    .filter((event) => businessTimelineEventTypes.has(event.eventType))
    .map((event) =>
      createBusinessTimelineEntry(event, workflowConfig, visibilityContext),
    );
}

function createBusinessTimelineEntry(
  event: LedgerEventRecord,
  workflowConfig: WorkflowConfig,
  visibilityContext?: TimelineVisibilityContext,
): Record<string, unknown> {
  return {
    eventType: event.eventType,
    occurredAt: event.occurredAt,
    actorId: event.actorId,
    summary: businessSummaryForEvent(event, workflowConfig),
    payloadExcerpt: businessPayloadExcerpt(event, visibilityContext),
  };
}

function createDebugTimelineEntry(event: LedgerEventRecord): Record<string, unknown> {
  return {
    eventType: event.eventType,
    occurredAt: event.occurredAt,
    actorId: event.actorId,
    idempotencyKey: event.idempotencyKey,
    payloadExcerpt: event.payload,
  };
}

function businessSummaryForEvent(
  event: LedgerEventRecord,
  workflowConfig: WorkflowConfig,
): string {
  return workflowConfig.timeline.summaries[event.eventType] ?? event.eventType;
}

function businessPayloadExcerpt(
  event: LedgerEventRecord,
  visibilityContext?: TimelineVisibilityContext,
): Record<string, unknown> {
  const requiredFieldGroup = fieldGroupForTimelineEvent(event);

  if (
    requiredFieldGroup !== undefined &&
    visibilityContext !== undefined &&
    !canViewEmployee(
      visibilityContext.actor,
      visibilityContext.targetProjection.document,
      requiredFieldGroup,
      visibilityContext.access,
    ).ok
  ) {
    return {
      restricted: true,
      fieldGroup: requiredFieldGroup,
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.PERSON_LEGAL_NAME_CHANGED) {
    return {
      previousLegalName: event.payload["previousLegalName"],
      newLegalName: event.payload["newLegalName"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMPLOYEE_EMERGENCY_CONTACT_UPDATED) {
    return {
      changedEmergencyContact: event.payload["changedEmergencyContact"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMPLOYEE_COMPENSATION_UPDATED) {
    return {
      previousCompensation: event.payload["previousCompensation"],
      newCompensation: event.payload["newCompensation"],
      increasePercent: event.payload["increasePercent"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED) {
    const outboxRows = event.payload["outboxRows"];

    return {
      outboxRequestCount: Array.isArray(outboxRows) ? outboxRows.length : 0,
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EXTERNAL_WRITE_SUCCEEDED) {
    const externalWriteExecutions = event.payload["externalWriteExecutions"];

    return {
      externalWriteSuccessCount: Array.isArray(externalWriteExecutions)
        ? externalWriteExecutions.length
        : 0,
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EVIDENCE_PROVIDED) {
    return {
      documentId: event.payload["documentId"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED) {
    const approvalTask = objectField(event.payload, "approvalTask");

    return {
      approvalTaskId: approvalTask?.["approvalTaskId"],
      assigneeRole: approvalTask?.["assigneeRole"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.NAME_CHANGE_PREFLIGHTED) {
    return {
      valid: event.payload["valid"],
      riskLevel: event.payload["riskLevel"],
      requiresEvidence: event.payload["requiresEvidence"],
      requiresApproval: event.payload["requiresApproval"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMERGENCY_CONTACT_PREFLIGHTED) {
    return {
      valid: event.payload["valid"],
      riskLevel: event.payload["riskLevel"],
      requiresEvidence: event.payload["requiresEvidence"],
      requiresApproval: event.payload["requiresApproval"],
    };
  }

  if (event.eventType === LEDGER_EVENT_TYPES.COMPENSATION_PREFLIGHTED) {
    return {
      valid: event.payload["valid"],
      riskLevel: event.payload["riskLevel"],
      requiresApproval: event.payload["requiresApproval"],
    };
  }

  return {};
}

function fieldGroupForTimelineEvent(
  event: LedgerEventRecord,
): EmployeeAccessFieldGroup | undefined {
  if (event.eventType === LEDGER_EVENT_TYPES.PERSON_LEGAL_NAME_CHANGED) {
    return "profile";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMPLOYEE_EMERGENCY_CONTACT_UPDATED) {
    return "emergencyContacts";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMPLOYEE_CONTACT_INFO_UPDATED) {
    return "contact";
  }

  if (event.eventType === LEDGER_EVENT_TYPES.EMPLOYEE_COMPENSATION_UPDATED) {
    return "compensation";
  }

  if (
    event.eventType === LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED ||
    event.eventType === LEDGER_EVENT_TYPES.NAME_CHANGE_PREFLIGHTED ||
    event.eventType === LEDGER_EVENT_TYPES.EMERGENCY_CONTACT_PREFLIGHTED ||
    event.eventType === LEDGER_EVENT_TYPES.CONTACT_INFO_PREFLIGHTED ||
    event.eventType === LEDGER_EVENT_TYPES.COMPENSATION_PREFLIGHTED
  ) {
    return "workflow";
  }

  return undefined;
}
