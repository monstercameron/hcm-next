import { randomUUID } from "node:crypto";
import {
  err,
  fromThrowable,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import { DEMO_IDS, type ActorRecord, type Repositories } from "@hcm-next/data-store";

export type ApiRequestContext = {
  actor: ActorRecord;
  tenantId: string;
  environmentId: string;
  requestId: string;
  correlationId: string;
};

export type HeaderReader = {
  get: (name: string) => string | null;
};

export type RequestLike = {
  headers: HeaderReader;
};

/**
 * Builds the API request context from demo headers and actor records.
 */
export function buildApiRequestContext(
  request: RequestLike,
  repositories: Repositories,
): Result<ApiRequestContext, AppError> {
  const actorId = request.headers.get("x-demo-actor-id") ?? DEMO_IDS.employeeActorId;
  const requestId = request.headers.get("x-request-id") ?? randomUUID();
  const correlationId = request.headers.get("x-correlation-id") ?? requestId;
  const actorResult = repositories.actors.findById(actorId);

  if (!actorResult.ok) {
    return actorResult;
  }

  return ok({
    actor: actorResult.value,
    tenantId: actorResult.value.tenantId,
    environmentId: DEMO_IDS.environmentId,
    requestId,
    correlationId,
  });
}

/**
 * Validates an already-parsed JSON value as a command object.
 */
export function readJsonObject(
  value: unknown,
): Result<Record<string, unknown>, AppError> {
  const bodyResult = fromThrowable(
    () => value,
    () => validationFailedError({ body: "Invalid JSON body." }),
  );

  if (!bodyResult.ok) {
    return bodyResult;
  }

  if (
    typeof bodyResult.value !== "object" ||
    bodyResult.value === null ||
    Array.isArray(bodyResult.value)
  ) {
    return err(validationFailedError({ body: "Expected a JSON object." }));
  }

  return ok(bodyResult.value as Record<string, unknown>);
}
