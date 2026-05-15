import {
  idempotencyConflictError,
  ok,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import {
  makeId,
  nowIso,
  type ActorRecord,
  type WorkflowTransitionAttemptRecord,
} from "@hcm-next/data-store";

export type WorkflowTransitionAttemptBody = {
  transition: string;
  idempotencyKey: string;
  expectedVersion: number;
  input: Record<string, unknown>;
};

type WorkflowTransitionAttemptContext = {
  tenantId: string;
  actor: Pick<ActorRecord, "actorId">;
};

/**
 * Replays a completed transition attempt for idempotent workflow retries.
 */
export function replayTransitionAttempt(
  attempt: WorkflowTransitionAttemptRecord,
): Result<Record<string, unknown>, AppError> {
  if (attempt.status === "completed" && attempt.responsePayload !== undefined) {
    return ok({
      ...attempt.responsePayload,
      idempotentReplay: true,
    });
  }

  return {
    ok: false,
    error: idempotencyConflictError({
      workflowInstanceId: attempt.workflowInstanceId,
      idempotencyKey: attempt.idempotencyKey,
      status: attempt.status,
    }),
  };
}

/**
 * Creates the persisted idempotency attempt envelope before a transition runs.
 */
export function createTransitionAttempt(
  requestContext: WorkflowTransitionAttemptContext,
  workflowInstanceId: string,
  transitionBody: WorkflowTransitionAttemptBody,
): WorkflowTransitionAttemptRecord {
  return {
    workflowTransitionAttemptId: makeId("wfta"),
    tenantId: requestContext.tenantId,
    workflowInstanceId,
    transition: transitionBody.transition,
    idempotencyKey: transitionBody.idempotencyKey,
    expectedVersion: transitionBody.expectedVersion,
    actorId: requestContext.actor.actorId,
    requestPayload: transitionBody.input,
    status: "started",
    createdAt: nowIso(),
  };
}

/**
 * Captures the terminal result of a workflow transition attempt.
 */
export function completeTransitionAttempt(
  attempt: WorkflowTransitionAttemptRecord,
  result: Result<Record<string, unknown>, AppError>,
): WorkflowTransitionAttemptRecord {
  if (result.ok) {
    return {
      ...attempt,
      responsePayload: result.value,
      status: "completed",
      completedAt: nowIso(),
    };
  }

  return {
    ...attempt,
    status: "failed",
    error: {
      code: result.error.code,
      message: result.error.safeMessage,
      details: result.error.details ?? {},
    },
    completedAt: nowIso(),
  };
}
