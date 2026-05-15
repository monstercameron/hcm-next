import type { RequestContext } from "../context";
import type { JsonObject, LegalName } from "../domain";
import { fromPromise, goExecutorError, ok, type Result } from "../result";
import { valuesAreEqual } from "../runtime/json";

export const LEGAL_NAME_PREFLIGHT_BLOCK = {
  name: "system.employee_data.legal_name.preflight",
  version: "1.0.0",
} as const;

export const LEGAL_NAME_PLAN_TRANSACTION_BLOCK = {
  name: "system.employee_data.legal_name.plan_transaction",
  version: "1.0.0",
} as const;

export type ExecuteBlockRequest = {
  tenantId: string;
  environmentId: string;
  changeRequestId?: string;
  workflowInstanceId: string;
  workflowVersionId: string;
  block: {
    name: string;
    version: string;
  };
  input: JsonObject;
  context: {
    actorId: string;
    effectiveAt?: string;
    permissions: JsonObject;
    correlationId: string;
    idempotencyKey: string;
  };
};

export type ExecuteBlockResponse = {
  status: "succeeded" | "failed";
  output: JsonObject;
  proposedEvents: JsonObject[];
  externalCallRequests: JsonObject[];
  logs: JsonObject[];
  metrics: {
    durationMs: number;
  };
  error?: JsonObject;
};

export type ExecutorClient = {
  /** Executes a deterministic workflow block and returns a typed Result. */
  executeBlock(request: ExecuteBlockRequest): Promise<Result<ExecuteBlockResponse>>;
};

export type LegalNamePreflightOutput = {
  valid: boolean;
  riskLevel: "low" | "medium" | "high";
  requiresEvidence: boolean;
  requiresApproval: boolean;
  warnings: string[];
  errors: string[];
};

export type LegalNamePlanOutput = {
  internalWrites: JsonObject[];
  externalCallRequests: JsonObject[];
};

/** Creates the HTTP boundary client for the Go executor service. */
export function createHttpGoExecutorClient(baseUrl: string): ExecutorClient {
  return {
    async executeBlock(request) {
      const responseResult = await fromPromise(
        async () => {
          const response = await fetch(`${baseUrl}/execute-block`, {
            method: "POST",
            headers: {
              "content-type": "application/json",
              "x-correlation-id": request.context.correlationId,
            },
            body: JSON.stringify(request),
          });

          if (!response.ok) {
            throw new Error(`Go executor returned HTTP ${response.status}`);
          }

          return (await response.json()) as ExecuteBlockResponse;
        },
        (error: unknown) => goExecutorError({ block: request.block.name }, error),
      );

      if (!responseResult.ok) {
        return responseResult;
      }

      const validationResult = validateExecutorResponse(responseResult.value);
      if (!validationResult.ok) {
        return validationResult;
      }

      return ok(responseResult.value);
    },
  };
}

/** Provides deterministic local block behavior when the Go service is not wired yet. */
export function createLocalLegalNameExecutorClient(): ExecutorClient {
  return {
    async executeBlock(request) {
      if (request.block.name === LEGAL_NAME_PREFLIGHT_BLOCK.name) {
        return ok({
          status: "succeeded",
          output: runLocalPreflight(request.input),
          proposedEvents: [],
          externalCallRequests: [],
          logs: [],
          metrics: { durationMs: 0 },
        });
      }

      if (request.block.name === LEGAL_NAME_PLAN_TRANSACTION_BLOCK.name) {
        return ok({
          status: "succeeded",
          output: runLocalTransactionPlan(request.input),
          proposedEvents: [],
          externalCallRequests: runLocalTransactionPlan(request.input)
            .externalCallRequests,
          logs: [],
          metrics: { durationMs: 0 },
        });
      }

      return {
        ok: false,
        error: goExecutorError({ block: request.block.name }),
      };
    },
  };
}

export function buildExecutorRequest(input: {
  context: RequestContext;
  workflowInstanceId: string;
  workflowVersionId: string;
  changeRequestId?: string;
  block: ExecuteBlockRequest["block"];
  payload: JsonObject;
  effectiveAt?: string;
  idempotencyKey: string;
  permissions: JsonObject;
}): ExecuteBlockRequest {
  const executorContext: ExecuteBlockRequest["context"] = {
    actorId: input.context.actorId,
    permissions: input.permissions,
    correlationId: input.context.correlationId,
    idempotencyKey: input.idempotencyKey,
  };
  if (input.effectiveAt !== undefined) {
    executorContext.effectiveAt = input.effectiveAt;
  }

  return {
    tenantId: input.context.tenantId,
    environmentId: input.context.environmentId,
    workflowInstanceId: input.workflowInstanceId,
    workflowVersionId: input.workflowVersionId,
    changeRequestId: input.changeRequestId,
    block: input.block,
    input: input.payload,
    context: executorContext,
  };
}

function validateExecutorResponse(
  response: ExecuteBlockResponse,
): Result<ExecuteBlockResponse> {
  if (!response || (response.status !== "succeeded" && response.status !== "failed")) {
    return {
      ok: false,
      error: goExecutorError({ reason: "Invalid executor response" }),
    };
  }

  return ok(response);
}

function runLocalPreflight(input: JsonObject): JsonObject {
  const currentLegalName = input.currentLegalName as LegalName;
  const proposedLegalName = input.proposedLegalName as LegalName;
  const errors: string[] = [];

  if (!proposedLegalName?.first) {
    errors.push("First name is required.");
  }

  if (!proposedLegalName?.last) {
    errors.push("Last name is required.");
  }

  if (!input.businessReason) {
    errors.push("Business reason is required.");
  }

  if (!input.effectiveAt) {
    errors.push("Effective date is required.");
  }

  if (
    currentLegalName &&
    proposedLegalName &&
    valuesAreEqual(currentLegalName, proposedLegalName)
  ) {
    errors.push("New legal name must differ from the current legal name.");
  }

  return {
    valid: errors.length === 0,
    riskLevel: "low",
    requiresEvidence: true,
    requiresApproval: true,
    warnings: [],
    errors,
  };
}

function runLocalTransactionPlan(input: JsonObject): LegalNamePlanOutput {
  const changeRequestId = String(input.changeRequestId);
  const workerId = String(input.workerId);
  const proposedLegalName = input.proposedLegalName as JsonObject;
  const effectiveAt = String(input.effectiveAt);

  return {
    internalWrites: [
      {
        eventType: "PersonLegalNameChanged",
        subjectType: "worker",
        subjectId: workerId,
        effectiveAt,
        payload: {
          personId: input.personId ?? null,
          previousLegalName: (input.currentLegalName as JsonObject | undefined) ?? {},
          newLegalName: proposedLegalName,
        },
      },
    ],
    externalCallRequests: [
      {
        connectionId: "fake_hris",
        operation: "updateLegalName",
        idempotencyKey: `fake_hris_legal_name_${changeRequestId}`,
        payload: {
          workerId,
          legalName: proposedLegalName,
          effectiveAt,
        },
      },
    ],
  };
}
