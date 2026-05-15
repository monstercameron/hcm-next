import { LEDGER_EVENT_TYPES, type LedgerEventType } from "../constants";
import type { RequestContext } from "../context";
import type { Actor, JsonObject, LedgerEvent, TimelineEntry } from "../domain";
import { err, ok, permissionDeniedError, type Result } from "../result";
import { canViewEvidence, canViewWorkflowInstance } from "../runtime/permissions";
import type { WorkflowRuntimeStore } from "../storage/workflow-store";

export type TimelineService = {
  /** Builds a business timeline from immutable workflow ledger events. */
  getWorkflowTimeline(
    context: RequestContext,
    actor: Actor,
    workflowInstanceId: string,
  ): Promise<Result<TimelineEntry[]>>;
};

export function createTimelineService(store: WorkflowRuntimeStore): TimelineService {
  return new DefaultTimelineService(store);
}

class DefaultTimelineService implements TimelineService {
  constructor(private readonly store: WorkflowRuntimeStore) {}

  async getWorkflowTimeline(
    context: RequestContext,
    actor: Actor,
    workflowInstanceId: string,
  ): Promise<Result<TimelineEntry[]>> {
    const workflowResult = await this.store.findWorkflowInstanceById(
      context.tenantId,
      workflowInstanceId,
    );
    if (!workflowResult.ok) {
      return workflowResult;
    }

    if (!canViewWorkflowInstance(context, actor, workflowResult.value)) {
      return err(permissionDeniedError({ workflowInstanceId }));
    }

    const eventsResult = await this.store.listLedgerEventsForWorkflow(
      context.tenantId,
      workflowInstanceId,
      workflowResult.value.changeRequestId,
    );
    if (!eventsResult.ok) {
      return eventsResult;
    }

    const canSeeEvidence = canViewEvidence(context, actor, workflowResult.value);
    return ok(
      eventsResult.value.map((event: LedgerEvent) =>
        mapLedgerEventToTimelineEntry(event, canSeeEvidence),
      ),
    );
  }
}

function mapLedgerEventToTimelineEntry(
  event: LedgerEvent,
  canSeeEvidence: boolean,
): TimelineEntry {
  const payload = shouldRedactPayload(event.eventType, canSeeEvidence)
    ? { redacted: true }
    : excerptPayload(event.payload);

  return {
    eventId: event.eventId,
    eventType: event.eventType,
    occurredAt: event.occurredAt,
    actorId: event.actorId,
    summary: summarizeLedgerEvent(event),
    payloadExcerpt: payload,
  };
}

function shouldRedactPayload(
  eventType: LedgerEventType,
  canSeeEvidence: boolean,
): boolean {
  return (
    !canSeeEvidence &&
    [
      LEDGER_EVENT_TYPES.DOCUMENT_CREATED,
      LEDGER_EVENT_TYPES.EVIDENCE_PROVIDED,
    ].includes(eventType)
  );
}

function excerptPayload(payload: JsonObject): JsonObject {
  const entries = Object.entries(payload).slice(0, 6);
  return Object.fromEntries(entries) as JsonObject;
}

function summarizeLedgerEvent(event: LedgerEvent): string {
  const summaries: Partial<Record<LedgerEventType, string>> = {
    [LEDGER_EVENT_TYPES.WORKFLOW_INTENT_STARTED]: "Workflow intent started.",
    [LEDGER_EVENT_TYPES.WORKFLOW_TRANSITION_SUBMITTED]:
      "Workflow transition submitted.",
    [LEDGER_EVENT_TYPES.WORKFLOW_STATE_CHANGED]: "Workflow state changed.",
    [LEDGER_EVENT_TYPES.CHANGE_REQUEST_CREATED]: "Change request created.",
    [LEDGER_EVENT_TYPES.PROPOSED_CHANGE_CREATED]: "Proposed change recorded.",
    [LEDGER_EVENT_TYPES.NAME_CHANGE_PREFLIGHTED]: "Legal name preflight completed.",
    [LEDGER_EVENT_TYPES.EMERGENCY_CONTACT_PREFLIGHTED]:
      "Emergency contact preflight completed.",
    [LEDGER_EVENT_TYPES.CONTACT_INFO_PREFLIGHTED]:
      "Contact information preflight completed.",
    [LEDGER_EVENT_TYPES.COMPENSATION_PREFLIGHTED]: "Compensation preflight completed.",
    [LEDGER_EVENT_TYPES.EVIDENCE_REQUESTED]: "Evidence requested.",
    [LEDGER_EVENT_TYPES.DOCUMENT_CREATED]: "Document metadata created.",
    [LEDGER_EVENT_TYPES.EVIDENCE_PROVIDED]: "Evidence provided.",
    [LEDGER_EVENT_TYPES.APPROVAL_TASK_CREATED]: "Approval task created.",
    [LEDGER_EVENT_TYPES.CHANGE_REQUEST_SUBMITTED]:
      "Change request submitted for approval.",
    [LEDGER_EVENT_TYPES.APPROVAL_GRANTED]: "Approval granted.",
    [LEDGER_EVENT_TYPES.APPROVAL_REJECTED]: "Approval rejected.",
    [LEDGER_EVENT_TYPES.MORE_INFORMATION_REQUESTED]: "More information requested.",
    [LEDGER_EVENT_TYPES.CHANGE_REQUEST_APPROVED]: "Change request approved.",
    [LEDGER_EVENT_TYPES.CHANGE_REQUEST_REJECTED]: "Change request rejected.",
    [LEDGER_EVENT_TYPES.TRANSACTION_PLAN_CREATED]: "Transaction plan created.",
    [LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_STARTED]:
      "Transaction execution started.",
    [LEDGER_EVENT_TYPES.PERSON_LEGAL_NAME_CHANGED]: "Legal name changed.",
    [LEDGER_EVENT_TYPES.EMPLOYEE_EMERGENCY_CONTACT_UPDATED]:
      "Emergency contact updated.",
    [LEDGER_EVENT_TYPES.EMPLOYEE_CONTACT_INFO_UPDATED]: "Contact information updated.",
    [LEDGER_EVENT_TYPES.EMPLOYEE_COMPENSATION_UPDATED]: "Compensation updated.",
    [LEDGER_EVENT_TYPES.EMPLOYEE_PROJECTION_UPDATED]: "Employee projection updated.",
    [LEDGER_EVENT_TYPES.EXTERNAL_WRITE_REQUESTED]: "External write requested.",
    [LEDGER_EVENT_TYPES.EXTERNAL_WRITE_SUCCEEDED]: "External write succeeded.",
    [LEDGER_EVENT_TYPES.EXTERNAL_WRITE_FAILED]: "External write failed.",
    [LEDGER_EVENT_TYPES.TRANSACTION_EXECUTION_COMPLETED]:
      "Transaction execution completed.",
    [LEDGER_EVENT_TYPES.WORKFLOW_COMPLETED]: "Workflow completed.",
    [LEDGER_EVENT_TYPES.WORKFLOW_CANCELED]: "Workflow canceled.",
    [LEDGER_EVENT_TYPES.WORKFLOW_FAILED]: "Workflow failed.",
  };

  return summaries[event.eventType] ?? event.eventType;
}
