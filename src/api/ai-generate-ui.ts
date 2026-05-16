import { createHash } from "node:crypto";
import {
  err,
  ok,
  permissionDeniedError,
  systemError,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  AiUiGenerationActor,
  AiUiGenerationRequest,
  AiUiGenerationSubject,
} from "@hcm-next/ai-client";
import type { EmployeeProjectionRecord } from "@hcm-next/data-store";
import type { AppDependencies } from "./dependencies.js";
import type { ApiRequestContext } from "./request-context.js";
import {
  computeWorkflowConfigHash,
  getWorkflowConfigByIntent,
  type WorkflowConfig,
} from "../workflows/registry/index.js";

const MAX_USER_PROMPT_LENGTH = 2000;
const SUBJECT_PICKER_OPTION_LIMIT = 10;

/**
 * Handles `POST /api/ai/generate-ui`. Loads the workflow config by intent,
 * validates inputs and the actor's permission to start that intent, resolves
 * the optional subject employee, then calls the configured `AiClient` to
 * generate a `PageDefinition` grounded in the workflow JSON.
 *
 * Returns the generated `page`, the `workflowConfigHash` used to ground the
 * response, and the provider metadata so callers can attribute usage.
 */
export async function handleAiGenerateUi(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  body: Record<string, unknown>,
): Promise<Result<Record<string, unknown>, AppError>> {
  const parseResult = parseGenerateUiBody(body);
  if (!parseResult.ok) {
    return parseResult;
  }
  const input = parseResult.value;

  const workflowConfigResult = getWorkflowConfigByIntent(
    dependencies.repositories,
    input.workflowIntent,
  );
  if (!workflowConfigResult.ok) {
    return workflowConfigResult;
  }
  const workflowConfig = workflowConfigResult.value;

  const subjectProjectionResult = loadSubjectProjection(
    dependencies,
    requestContext,
    input.subjectId,
  );
  if (!subjectProjectionResult.ok) {
    return subjectProjectionResult;
  }
  const subjectProjection = subjectProjectionResult.value;

  // PERM: the AI generator surfaces actions the user could trigger by starting
  // this intent. Gate the same way `startWorkflowIntent` does: the actor must
  // either satisfy `selfServiceStart` for their own record or hold one of the
  // configured `startActors` role tokens. Logic mirrors
  // `canStartConfiguredWorkflow` in src/workflows/runtime/permissions.ts;
  // duplicated inline here to avoid importing the runtime service layer into
  // the AI route surface.
  const permissionResult = checkCanStartIntent({
    workflowConfig,
    actor: requestContext.actor,
    subjectId: input.subjectId,
  });
  if (!permissionResult.ok) {
    return permissionResult;
  }

  if (dependencies.aiClient === undefined) {
    return err(systemError({ reason: "ai_client_not_configured" }));
  }

  const workflowConfigHash = computeWorkflowConfigHash(workflowConfig);
  const idempotencyKey = idempotencyKeyForRequest({
    workflowIntent: input.workflowIntent,
    actorId: requestContext.actor.actorId,
    userPrompt: input.userPrompt,
    currentState: input.currentState,
  });

  // Load the same picker candidate pool the chat tool uses so the standalone
  // generate-ui endpoint produces a comparable page (subject picker first,
  // workflow body below) regardless of which surface the AI was invoked from.
  // We also derive a `displayName-by-employeeId` map from the same pool so
  // every subject summary can render the manager's name instead of a bare id.
  const tenantProjections = loadTenantProjections(dependencies, requestContext);
  const managerLookup = buildManagerLookup(tenantProjections);
  const employeeOptions = tenantProjections
    .slice(0, SUBJECT_PICKER_OPTION_LIMIT)
    .map((record) => subjectFromProjection(record, managerLookup));

  const aiRequest: AiUiGenerationRequest = {
    workflow: workflowConfig,
    workflowConfigHash,
    ...(input.currentState !== undefined ? { currentState: input.currentState } : {}),
    ...(input.currentInteraction !== undefined
      ? { currentInteraction: input.currentInteraction }
      : {}),
    ...(subjectProjection !== undefined
      ? { subject: subjectFromProjection(subjectProjection, managerLookup) }
      : {}),
    employeeOptions,
    actor: actorSummary(requestContext.actor),
    userPrompt: input.userPrompt,
    correlationId: requestContext.correlationId,
    idempotencyKey,
  };

  dependencies.logger?.info("ai generate-ui started", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    correlationId: requestContext.correlationId,
    workflowIntent: input.workflowIntent,
    workflowConfigHash,
    hasSubject: subjectProjection !== undefined,
    currentState: input.currentState ?? null,
  });

  const generationResult = await dependencies.aiClient.generatePageDefinition(
    aiRequest,
    dependencies.logger,
  );
  if (!generationResult.ok) {
    dependencies.logger?.warn("ai generate-ui failed", {
      actorId: requestContext.actor.actorId,
      tenantId: requestContext.tenantId,
      correlationId: requestContext.correlationId,
      workflowIntent: input.workflowIntent,
      errorCode: generationResult.error.code,
    });
    return generationResult;
  }

  dependencies.logger?.info("ai generate-ui completed", {
    actorId: requestContext.actor.actorId,
    tenantId: requestContext.tenantId,
    correlationId: requestContext.correlationId,
    workflowIntent: input.workflowIntent,
    provider: generationResult.value.providerMetadata.provider,
    model: generationResult.value.providerMetadata.model,
    inputTokens: generationResult.value.providerMetadata.inputTokens,
    outputTokens: generationResult.value.providerMetadata.outputTokens,
  });

  return ok({
    page: generationResult.value.page,
    workflowConfigHash,
    providerMetadata: generationResult.value.providerMetadata,
  });
}

type ParsedGenerateUiInput = {
  workflowIntent: string;
  currentState?: string;
  currentInteraction?: string;
  subjectId?: string;
  userPrompt: string;
};

function parseGenerateUiBody(
  body: Record<string, unknown>,
): Result<ParsedGenerateUiInput, AppError> {
  const workflowIntent = optionalString(body, "workflowIntent");
  if (workflowIntent === undefined || workflowIntent.trim().length === 0) {
    return err(
      validationFailedError({
        field: "workflowIntent",
        reason: "workflow_intent_required",
      }),
    );
  }

  const userPrompt = optionalString(body, "userPrompt");
  if (userPrompt === undefined || userPrompt.trim().length === 0) {
    return err(
      validationFailedError({
        field: "userPrompt",
        reason: "user_prompt_required",
      }),
    );
  }

  if (userPrompt.length > MAX_USER_PROMPT_LENGTH) {
    return err(
      validationFailedError({
        field: "userPrompt",
        reason: "user_prompt_too_long",
        maxLength: MAX_USER_PROMPT_LENGTH,
        actualLength: userPrompt.length,
      }),
    );
  }

  const currentState = optionalString(body, "currentState");
  const currentInteraction = optionalString(body, "currentInteraction");
  const subjectId = optionalString(body, "subjectId");

  return ok({
    workflowIntent,
    userPrompt,
    ...(currentState !== undefined ? { currentState } : {}),
    ...(currentInteraction !== undefined ? { currentInteraction } : {}),
    ...(subjectId !== undefined ? { subjectId } : {}),
  });
}

function optionalString(
  body: Record<string, unknown>,
  field: string,
): string | undefined {
  const value = body[field];
  return typeof value === "string" ? value : undefined;
}

function loadTenantProjections(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
): readonly EmployeeProjectionRecord[] {
  const projectionsResult = dependencies.repositories.employeeProjections.findByTenant(
    requestContext.tenantId,
  );
  if (!projectionsResult.ok) {
    return [];
  }
  return projectionsResult.value;
}

function buildManagerLookup(
  projections: readonly EmployeeProjectionRecord[],
): ReadonlyMap<string, string> {
  const lookup = new Map<string, string>();
  for (const record of projections) {
    lookup.set(record.employeeId, record.document.person.displayName);
  }
  return lookup;
}

function loadSubjectProjection(
  dependencies: AppDependencies,
  requestContext: ApiRequestContext,
  subjectId: string | undefined,
): Result<EmployeeProjectionRecord | undefined, AppError> {
  if (subjectId === undefined) {
    return ok(undefined);
  }

  const projectionResult =
    dependencies.repositories.employeeProjections.findByEmployeeId(
      requestContext.tenantId,
      subjectId,
    );
  if (!projectionResult.ok) {
    return projectionResult;
  }

  return ok(projectionResult.value);
}

function checkCanStartIntent(input: {
  workflowConfig: WorkflowConfig;
  actor: ApiRequestContext["actor"];
  subjectId: string | undefined;
}): Result<true, AppError> {
  const { workflowConfig, actor, subjectId } = input;

  if (
    workflowConfig.selfServiceStart &&
    subjectId !== undefined &&
    actor.linkedWorkerId !== undefined &&
    actor.linkedWorkerId === subjectId
  ) {
    return ok(true);
  }

  const startActors = Array.isArray(workflowConfig.startActors)
    ? workflowConfig.startActors.map(String)
    : [];

  for (const actorToken of startActors) {
    if (actorMatchesRoleToken(actor.roles, actorToken)) {
      return ok(true);
    }
  }

  return err(
    permissionDeniedError({
      intent: workflowConfig.intent,
      ...(subjectId !== undefined ? { subjectId } : {}),
    }),
  );
}

function actorMatchesRoleToken(roles: readonly string[], actorToken: string): boolean {
  if (actorToken.includes("_or_")) {
    return actorToken.split("_or_").some((role) => roles.includes(role));
  }
  return roles.includes(actorToken);
}

function subjectFromProjection(
  projection: EmployeeProjectionRecord,
  managerLookup?: ReadonlyMap<string, string>,
): AiUiGenerationSubject {
  const document = projection.document;
  const jobTitle = document.job.title;
  const department = document.organization.department;
  const managerEmployeeId = document.manager.employeeId;
  const manager =
    managerEmployeeId !== null
      ? (managerLookup?.get(managerEmployeeId) ?? undefined)
      : undefined;
  return {
    id: projection.employeeId,
    displayName: document.person.displayName,
    ...(jobTitle !== undefined && jobTitle.length > 0 ? { jobTitle } : {}),
    ...(department !== undefined && department.length > 0 ? { department } : {}),
    ...(manager !== undefined && manager.length > 0 ? { manager } : {}),
  };
}

function actorSummary(actor: ApiRequestContext["actor"]): AiUiGenerationActor {
  return {
    id: actor.actorId,
    displayName: actor.displayName,
    roles: actor.roles,
  };
}

function idempotencyKeyForRequest(input: {
  workflowIntent: string;
  actorId: string;
  userPrompt: string;
  currentState: string | undefined;
}): string {
  // Identical requests (same intent, actor, prompt, and state) collapse onto
  // the same key so the AI client can short-circuit duplicate work.
  const canonical = JSON.stringify([
    input.workflowIntent,
    input.actorId,
    input.userPrompt,
    input.currentState ?? "",
  ]);
  const hashHex = createHash("sha1").update(canonical).digest("hex");
  return `ai-generate-ui:${hashHex}`;
}
