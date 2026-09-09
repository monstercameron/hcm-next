import {
  LEDGER_EVENT_TYPES,
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type {
  ActorRecord,
  EmployeeProjectionRecord,
  LedgerEventRecord,
} from "@human-capital-management-suite/data-store";
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
  const requiredFieldGroup = fieldGroupForTimelinePayload(event.payload);

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

  return genericPayloadExcerpt(event.payload);
}

function genericPayloadExcerpt(
  payload: Record<string, unknown>,
): Record<string, unknown> {
  const outboxRows = payload["outboxRows"];

  if (Array.isArray(outboxRows)) {
    return {
      outboxRequestCount: outboxRows.length,
    };
  }

  const externalWriteExecutions = payload["externalWriteExecutions"];

  if (Array.isArray(externalWriteExecutions)) {
    return {
      externalWriteSuccessCount: externalWriteExecutions.length,
    };
  }

  if (payload["documentId"] !== undefined) {
    return {
      documentId: payload["documentId"],
    };
  }

  const approvalTask = objectField(payload, "approvalTask");

  if (approvalTask !== undefined) {
    return {
      approvalTaskId: approvalTask?.["approvalTaskId"],
      assigneeRole: approvalTask?.["assigneeRole"],
    };
  }

  const excerpt: Record<string, unknown> = {};

  for (const key of genericExcerptKeys) {
    if (payload[key] !== undefined) {
      excerpt[key] = payload[key];
    }
  }

  return excerpt;
}

const genericExcerptKeys = [
  "valid",
  "riskLevel",
  "requiresEvidence",
  "requiresApproval",
  "projectionVersion",
  "terminalState",
] as const;

function fieldGroupForTimelinePayload(
  payload: Record<string, unknown>,
): EmployeeAccessFieldGroup | undefined {
  for (const key of Object.keys(payload)) {
    const fieldGroup = fieldGroupForPayloadKey(key);

    if (fieldGroup !== undefined) {
      return fieldGroup;
    }
  }

  return undefined;
}

function fieldGroupForPayloadKey(key: string): EmployeeAccessFieldGroup | undefined {
  const normalizedKey = key.toLowerCase();

  if (
    normalizedKey.includes("compensation") ||
    normalizedKey.includes("salary") ||
    normalizedKey.includes("bonus")
  ) {
    return "compensation";
  }

  if (normalizedKey.includes("emergency") || normalizedKey.includes("dependent")) {
    return "emergencyContacts";
  }

  if (
    normalizedKey.includes("contact") ||
    normalizedKey.includes("email") ||
    normalizedKey.includes("phone") ||
    normalizedKey.includes("address")
  ) {
    return "contact";
  }

  if (
    normalizedKey.includes("organization") ||
    normalizedKey.includes("orgunit") ||
    normalizedKey.includes("manager") ||
    normalizedKey.includes("location") ||
    normalizedKey.includes("costcenter") ||
    normalizedKey.includes("team")
  ) {
    return "organization";
  }

  if (
    normalizedKey.includes("legalname") ||
    normalizedKey.includes("person") ||
    normalizedKey.includes("profile")
  ) {
    return "profile";
  }

  return undefined;
}
