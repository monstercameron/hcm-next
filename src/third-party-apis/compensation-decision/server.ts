import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { URL } from "node:url";

export const DEFAULT_COMPENSATION_DECISION_API_PORT = 4302;

type JsonHttpResponse = {
  status: number;
  body: Record<string, unknown>;
};

type CompensationDecisionPayload = {
  workerId: string;
  changeRequestId: string;
  currentCompensation: CompensationAmount;
  proposedCompensation: CompensationAmount;
  effectiveAt: string;
  businessReason?: string;
};

type CompensationAmount = {
  amount: number;
  currency: string;
  payFrequency: string;
  bonusTargetPercent?: number;
  effectiveDate?: string;
};

type ParsedJsonBody =
  | {
      ok: true;
      value: Record<string, unknown>;
    }
  | {
      ok: false;
      response: JsonHttpResponse;
    };

type ParsedDecisionPayload =
  | {
      ok: true;
      value: CompensationDecisionPayload;
    }
  | {
      ok: false;
      reasonCodes: string[];
    };

/**
 * Creates a standalone simulated third-party compensation decision server.
 */
export function createCompensationDecisionApiServer() {
  return createServer((request, response) => {
    void routeRequest(request).then(
      (jsonResponse) => {
        writeJsonResponse(response, jsonResponse);
      },
      () => {
        writeJsonResponse(
          response,
          errorResponse(500, "DECISION_SERVICE_ERROR", "Decision service failed."),
        );
      },
    );
  });
}

async function routeRequest(request: IncomingMessage): Promise<JsonHttpResponse> {
  const requestUrl = new URL(request.url ?? "/", requestOrigin(request));
  const method = request.method?.toUpperCase() ?? "GET";
  const pathname = requestUrl.pathname;

  if (method === "GET" && pathname === "/health") {
    return okResponse({
      status: "ok",
      service: "simulated-compensation-decision-api",
    });
  }

  if (method === "POST" && pathname === "/v1/compensation-decisions") {
    return compensationDecisionResponse(request);
  }

  return errorResponse(404, "ROUTE_NOT_FOUND", "Route not found.");
}

async function compensationDecisionResponse(
  request: IncomingMessage,
): Promise<JsonHttpResponse> {
  const rawBody = await readRequestBody(request);
  const bodyResult = await parseJsonObject(rawBody);

  if (!bodyResult.ok) {
    return bodyResult.response;
  }

  const payloadResult = parseCompensationDecisionPayload(bodyResult.value);
  const evaluation = payloadResult.ok
    ? evaluateCompensationDecision(payloadResult.value)
    : {
        decision: "bad",
        status: "rejected",
        reasonCodes: payloadResult.reasonCodes,
      };

  return okResponse({
    providerDecisionId: providerDecisionId(bodyResult.value),
    provider: "Meridian Compensation Controls",
    decision: evaluation.decision,
    status: evaluation.status,
    reasonCodes: evaluation.reasonCodes,
    receivedAt: new Date().toISOString(),
  });
}

function parseCompensationDecisionPayload(
  body: Record<string, unknown>,
): ParsedDecisionPayload {
  const currentCompensation = compensationAmountFromRecord(
    objectField(body, "currentCompensation"),
  );
  const proposedCompensation = compensationAmountFromRecord(
    objectField(body, "proposedCompensation"),
  );
  const workerId = stringField(body, "workerId");
  const changeRequestId = stringField(body, "changeRequestId");
  const effectiveAt = stringField(body, "effectiveAt");
  const businessReason = stringField(body, "businessReason");
  const reasonCodes: string[] = [];

  if (workerId === undefined) {
    reasonCodes.push("payload.worker_id_missing");
  }

  if (changeRequestId === undefined) {
    reasonCodes.push("payload.change_request_id_missing");
  }

  if (effectiveAt === undefined) {
    reasonCodes.push("payload.effective_at_missing");
  }

  if (currentCompensation === undefined) {
    reasonCodes.push("payload.current_compensation_invalid");
  }

  if (proposedCompensation === undefined) {
    reasonCodes.push("payload.proposed_compensation_invalid");
  }

  if (
    workerId === undefined ||
    changeRequestId === undefined ||
    effectiveAt === undefined ||
    currentCompensation === undefined ||
    proposedCompensation === undefined
  ) {
    return {
      ok: false,
      reasonCodes,
    };
  }

  return {
    ok: true,
    value: {
      workerId,
      changeRequestId,
      currentCompensation,
      proposedCompensation,
      effectiveAt,
      ...(businessReason !== undefined ? { businessReason } : {}),
    },
  };
}

function evaluateCompensationDecision(payload: CompensationDecisionPayload) {
  const reasonCodes = compensationDecisionReasonCodes(payload);
  const isGoodDecision = reasonCodes.length === 0;

  return {
    decision: isGoodDecision ? "good" : "bad",
    status: isGoodDecision ? "accepted" : "rejected",
    reasonCodes,
  };
}

function compensationDecisionReasonCodes(
  payload: CompensationDecisionPayload,
): string[] {
  const reasonCodes: string[] = [];
  const currentAmount = payload.currentCompensation.amount;
  const proposedAmount = payload.proposedCompensation.amount;
  const increasePercent =
    currentAmount > 0 ? ((proposedAmount - currentAmount) / currentAmount) * 100 : 0;

  if (payload.proposedCompensation.currency !== "USD") {
    reasonCodes.push("compensation.currency_unsupported");
  }

  if (payload.proposedCompensation.currency !== payload.currentCompensation.currency) {
    reasonCodes.push("compensation.currency_changed");
  }

  if (
    payload.proposedCompensation.payFrequency !==
    payload.currentCompensation.payFrequency
  ) {
    reasonCodes.push("compensation.pay_frequency_changed");
  }

  if (proposedAmount <= 0) {
    reasonCodes.push("compensation.proposed_amount_invalid");
  }

  if (proposedAmount <= currentAmount) {
    reasonCodes.push("compensation.not_a_raise");
  }

  if (increasePercent > 20) {
    reasonCodes.push("compensation.increase_over_twenty_percent");
  }

  if (proposedAmount > 300000) {
    reasonCodes.push("compensation.amount_exceeds_vendor_limit");
  }

  if ((payload.businessReason ?? "").trim().length === 0) {
    reasonCodes.push("compensation.business_reason_missing");
  }

  return reasonCodes;
}

function compensationAmountFromRecord(
  record: Record<string, unknown> | undefined,
): CompensationAmount | undefined {
  if (record === undefined) {
    return undefined;
  }

  const amount = numberField(record, "amount");
  const currency = stringField(record, "currency");
  const payFrequency = stringField(record, "payFrequency");
  const bonusTargetPercent = numberField(record, "bonusTargetPercent");
  const effectiveDate = stringField(record, "effectiveDate");

  if (amount === undefined || currency === undefined || payFrequency === undefined) {
    return undefined;
  }

  return {
    amount,
    currency,
    payFrequency,
    ...(bonusTargetPercent !== undefined ? { bonusTargetPercent } : {}),
    ...(effectiveDate !== undefined ? { effectiveDate } : {}),
  };
}

function readRequestBody(request: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];

    request.on("data", (chunk: Buffer) => {
      chunks.push(chunk);
    });
    request.on("end", () => {
      resolve(Buffer.concat(chunks).toString("utf8"));
    });
    request.on("error", reject);
  });
}

function parseJsonObject(body: string): Promise<ParsedJsonBody> {
  return Promise.resolve()
    .then(() => JSON.parse(body) as unknown)
    .then(
      (value) => {
        if (typeof value !== "object" || value === null || Array.isArray(value)) {
          return {
            ok: false,
            response: errorResponse(
              400,
              "INVALID_JSON_OBJECT",
              "Request body must be a JSON object.",
            ),
          };
        }

        return {
          ok: true,
          value: value as Record<string, unknown>,
        };
      },
      () => {
        return {
          ok: false,
          response: errorResponse(400, "INVALID_JSON", "Request body is invalid JSON."),
        };
      },
    );
}

function objectField(
  record: Record<string, unknown>,
  key: string,
): Record<string, unknown> | undefined {
  const value = record[key];

  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function stringField(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key];

  return typeof value === "string" && value.trim().length > 0
    ? value.trim()
    : undefined;
}

function numberField(record: Record<string, unknown>, key: string): number | undefined {
  const value = record[key];

  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function providerDecisionId(body: Record<string, unknown>): string {
  const workerId = stringField(body, "workerId") ?? "unknown_worker";
  const changeRequestId = stringField(body, "changeRequestId") ?? "unknown_change";
  const normalizedValue = `${workerId}_${changeRequestId}`.replace(
    /[^a-zA-Z0-9]+/g,
    "_",
  );

  return `sim_comp_decision_${normalizedValue}`;
}

function okResponse(body: Record<string, unknown>): JsonHttpResponse {
  return {
    status: 200,
    body,
  };
}

function errorResponse(
  status: number,
  code: string,
  message: string,
): JsonHttpResponse {
  return {
    status,
    body: {
      error: {
        code,
        message,
      },
    },
  };
}

function writeJsonResponse(
  response: ServerResponse,
  jsonResponse: JsonHttpResponse,
): void {
  response.statusCode = jsonResponse.status;
  response.setHeader("content-type", "application/json; charset=utf-8");
  response.end(JSON.stringify(jsonResponse.body));
}

function requestOrigin(request: IncomingMessage): string {
  const host = request.headers.host ?? "localhost";

  return `http://${Array.isArray(host) ? host[0] : host}`;
}
