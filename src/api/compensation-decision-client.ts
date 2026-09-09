import {
  err,
  fromPromise,
  integrationError,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type { ExternalWriteClient } from "./external-write-client.js";

export type CompensationDecisionResponse = {
  providerDecisionId: string;
  decision: "good" | "bad";
  status: "accepted" | "rejected";
  reasonCodes: string[];
  receivedAt: string;
  rawResponse: Record<string, unknown>;
};

export type CompensationDecisionClient = {
  submitCompensationChange(
    payload: Record<string, unknown>,
    idempotencyKey: string,
  ): Promise<Result<CompensationDecisionResponse, AppError>>;
};

/**
 * Creates the HTTP client for the simulated third-party compensation decision API.
 */
export function createHttpCompensationDecisionClient(
  baseUrl: string,
): CompensationDecisionClient {
  const normalizedBaseUrl = baseUrl.replace(/\/+$/, "");

  return {
    async submitCompensationChange(payload, idempotencyKey) {
      const responseResult = await fromPromise(
        async () => {
          return fetch(`${normalizedBaseUrl}/v1/compensation-decisions`, {
            method: "POST",
            headers: {
              "content-type": "application/json",
              "idempotency-key": idempotencyKey,
            },
            body: JSON.stringify(payload),
          });
        },
        (error: unknown) =>
          integrationError(
            {
              integration: "third_party_compensation_decision",
              operation: "submitCompensationChange",
            },
            error,
          ),
      );

      if (!responseResult.ok) {
        process.stderr.write(
          `${JSON.stringify({ timestamp: new Date().toISOString(), level: "error", message: "compensation decision HTTP request failed", integration: "third_party_compensation_decision" })}\n`,
        );
        return responseResult;
      }

      if (!responseResult.value.ok) {
        process.stderr.write(
          `${JSON.stringify({ timestamp: new Date().toISOString(), level: "error", message: "compensation decision HTTP error response", integration: "third_party_compensation_decision", httpStatus: responseResult.value.status })}\n`,
        );
        return err(
          integrationError({
            integration: "third_party_compensation_decision",
            operation: "submitCompensationChange",
            httpStatus: responseResult.value.status,
          }),
        );
      }

      const bodyResult = await fromPromise(
        async () => {
          return (await responseResult.value.json()) as Record<string, unknown>;
        },
        (error: unknown) =>
          integrationError(
            {
              integration: "third_party_compensation_decision",
              operation: "parseCompensationDecisionResponse",
            },
            error,
          ),
      );

      if (!bodyResult.ok) {
        return bodyResult;
      }

      return parseCompensationDecisionResponse(bodyResult.value);
    },
  };
}

/**
 * Adapts the demo compensation decision client to the generic external-write
 * client contract used by the workflow runtime.
 */
export function createCompensationDecisionExternalWriteClient(
  baseUrl: string,
): ExternalWriteClient {
  const compensationDecisionClient = createHttpCompensationDecisionClient(baseUrl);

  return {
    async submit(payload, idempotencyKey, logger) {
      logger?.info("compensation decision submit started", { idempotencyKey });

      const decisionResult = await compensationDecisionClient.submitCompensationChange(
        payload,
        idempotencyKey,
      );
      if (!decisionResult.ok) {
        logger?.error("compensation decision submit failed", {
          idempotencyKey,
          errorCode: decisionResult.error.code,
        });
        return decisionResult;
      }

      logger?.info("compensation decision submit completed", {
        idempotencyKey,
        decision: decisionResult.value.decision,
        status: decisionResult.value.status,
        reasonCodes: decisionResult.value.reasonCodes,
      });

      return ok({
        rawResponse: decisionResult.value.rawResponse,
        reasonCodes: decisionResult.value.reasonCodes,
      });
    },
  };
}

function parseCompensationDecisionResponse(
  body: Record<string, unknown>,
): Result<CompensationDecisionResponse, AppError> {
  const providerDecisionId = stringField(body, "providerDecisionId");
  const decision = stringField(body, "decision");
  const status = stringField(body, "status");
  const receivedAt = stringField(body, "receivedAt");
  const reasonCodes = stringArrayField(body, "reasonCodes") ?? [];

  if (
    providerDecisionId === undefined ||
    (decision !== "good" && decision !== "bad") ||
    (status !== "accepted" && status !== "rejected") ||
    receivedAt === undefined
  ) {
    return err(
      validationFailedError({
        providerDecisionId,
        decision,
        status,
        receivedAt,
      }),
    );
  }

  return ok({
    providerDecisionId,
    decision,
    status,
    reasonCodes,
    receivedAt,
    rawResponse: body,
  });
}

function stringField(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key];

  return typeof value === "string" && value.trim().length > 0
    ? value.trim()
    : undefined;
}

function stringArrayField(
  record: Record<string, unknown>,
  key: string,
): string[] | undefined {
  const value = record[key];

  if (!Array.isArray(value)) {
    return undefined;
  }

  return value.filter((item): item is string => typeof item === "string");
}
