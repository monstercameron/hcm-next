import { ERROR_CODES, type AppError } from "@hcm-next/foundation";
import { describe, expect, it, vi, type Mock } from "vitest";
import { runStartWorkflow, type RunStartWorkflowDeps } from "./use-start-workflow.js";

type FetchCall = {
  url: string;
  init: RequestInit;
};

const okResponse = (body: Record<string, unknown>): Response =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
  });

const errorResponse = (status: number, body: Record<string, unknown>): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });

const baseDeps = (fetchImpl: Mock): RunStartWorkflowDeps => ({
  fetch: fetchImpl as unknown as RunStartWorkflowDeps["fetch"],
  computeSha1: async () => "stub-idempotency-key",
});

const fetchCalls = (mock: Mock): readonly FetchCall[] =>
  mock.mock.calls.map((args) => {
    const url = String(args[0] as string | URL);
    const init = (args[1] ?? {}) as RequestInit;
    return { url, init };
  });

const baseInput = {
  intent: "employee.termination",
  subjectId: "emp_1",
  input: { terminationType: "voluntary" },
};

describe("runStartWorkflow", () => {
  it("posts the workflow intent body and returns the workflow summary", async () => {
    const fetchMock = vi.fn(async () =>
      okResponse({
        workflowInstanceId: "wf_abc",
        state: "waiting_approval",
        currentInteraction: { type: "waiting", interactionKey: "waitingApproval" },
      }),
    );

    const result = await runStartWorkflow(
      { ...baseInput, actorId: "actor_1" },
      baseDeps(fetchMock),
    );

    expect(result).toEqual({
      workflowInstanceId: "wf_abc",
      currentState: "waiting_approval",
      currentInteraction: "waitingApproval",
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const calls = fetchCalls(fetchMock);
    const call = calls[0]!;
    expect(call.url).toBe("/api/workflow-intents");
    expect(call.init.method).toBe("POST");
    const headers = call.init.headers as Record<string, string>;
    expect(headers["Content-Type"]).toBe("application/json");
    expect(headers["x-demo-actor-id"]).toBe("actor_1");
    const body = JSON.parse(call.init.body as string) as Record<string, unknown>;
    expect(body).toEqual({
      intent: "employee.termination",
      subjectId: "emp_1",
      input: { terminationType: "voluntary" },
      idempotencyKey: "stub-idempotency-key",
    });
  });

  it("rejects with a system AppError when the network throws", async () => {
    const fetchMock = vi.fn(async () => {
      throw new Error("offline");
    });

    const error = (await runStartWorkflow(baseInput, baseDeps(fetchMock)).then(
      () => undefined,
      (e: AppError) => e,
    )) as AppError;

    expect(error.code).toBe(ERROR_CODES.SYSTEM_ERROR);
    expect(
      (error.details as { reason?: string } | undefined)?.reason,
    ).toBe("network_request_failed");
  });

  it("maps HTTP 400 to validation_failed and surfaces the safeMessage", async () => {
    const fetchMock = vi.fn(async () =>
      errorResponse(400, {
        error: {
          code: "validation_failed",
          message: "Termination type is required.",
          details: { field: "terminationType" },
        },
      }),
    );

    const error = (await runStartWorkflow(baseInput, baseDeps(fetchMock)).then(
      () => undefined,
      (e: AppError) => e,
    )) as AppError;

    expect(error.code).toBe(ERROR_CODES.VALIDATION_FAILED);
    expect(error.safeMessage).toBe("Termination type is required.");
  });

  it("maps HTTP 403 to permission_denied", async () => {
    const fetchMock = vi.fn(async () =>
      errorResponse(403, {
        error: { code: "permission_denied", message: "Not allowed." },
      }),
    );

    const error = (await runStartWorkflow(baseInput, baseDeps(fetchMock)).then(
      () => undefined,
      (e: AppError) => e,
    )) as AppError;

    expect(error.code).toBe(ERROR_CODES.PERMISSION_DENIED);
  });

  it("maps HTTP 409 to idempotency_conflict", async () => {
    const fetchMock = vi.fn(async () =>
      errorResponse(409, {
        error: { code: "idempotency_conflict", message: "Already submitted." },
      }),
    );

    const error = (await runStartWorkflow(baseInput, baseDeps(fetchMock)).then(
      () => undefined,
      (e: AppError) => e,
    )) as AppError;

    expect(error.code).toBe(ERROR_CODES.IDEMPOTENCY_CONFLICT);
  });

  it("maps HTTP 500 to system_error", async () => {
    const fetchMock = vi.fn(async () =>
      errorResponse(500, {
        error: { code: "system_error", message: "Boom." },
      }),
    );

    const error = (await runStartWorkflow(baseInput, baseDeps(fetchMock)).then(
      () => undefined,
      (e: AppError) => e,
    )) as AppError;

    expect(error.code).toBe(ERROR_CODES.SYSTEM_ERROR);
  });

  it("rejects when the response shape is missing required fields", async () => {
    const fetchMock = vi.fn(async () =>
      okResponse({ workflowInstanceId: "wf_abc" }),
    );

    const error = (await runStartWorkflow(baseInput, baseDeps(fetchMock)).then(
      () => undefined,
      (e: AppError) => e,
    )) as AppError;

    expect(error.code).toBe(ERROR_CODES.SYSTEM_ERROR);
    expect(
      (error.details as { reason?: string } | undefined)?.reason,
    ).toBe("response_shape_invalid");
  });

  it("includes subjectType in the body when provided", async () => {
    const fetchMock = vi.fn(async () =>
      okResponse({ workflowInstanceId: "wf_abc", state: "collecting_input" }),
    );

    await runStartWorkflow(
      { ...baseInput, subjectType: "worker" },
      baseDeps(fetchMock),
    );

    const call = fetchCalls(fetchMock)[0]!;
    const body = JSON.parse(call.init.body as string) as Record<string, unknown>;
    expect(body.subjectType).toBe("worker");
  });
});
