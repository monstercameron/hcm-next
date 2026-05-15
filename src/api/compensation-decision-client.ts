import {
  err,
  fromPromise,
  integrationError,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";

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
        return responseResult;
      }

      if (!responseResult.value.ok) {
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
