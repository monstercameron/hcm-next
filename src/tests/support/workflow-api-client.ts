import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { ERROR_CODES, err, ok, type AppError, type Result } from "@hcm-next/foundation";
import type { AppDependencies } from "../../api/dependencies.js";
import { createApiServer } from "../../api/server.js";
import type { ApiRequestContext } from "../../api/request-context.js";

export type WorkflowApiClient = {
  startWorkflowIntent(
    requestContext: ApiRequestContext,
    body: Record<string, unknown>,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  transitionWorkflow(
    requestContext: ApiRequestContext,
    workflowInstanceId: string,
    body: Record<string, unknown>,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  createDocument(
    requestContext: ApiRequestContext,
    body: Record<string, unknown>,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  getAvailableActions(
    requestContext: ApiRequestContext,
    workflowInstanceId: string,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  getTasks(
    requestContext: ApiRequestContext,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  getTimeline(
    requestContext: ApiRequestContext,
    workflowInstanceId: string,
    view?: string,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  getEmployeeProjection(
    requestContext: ApiRequestContext,
    employeeId: string,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  listEmployeeProjections(
    requestContext: ApiRequestContext,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  postAdminWorkflowImport(
    requestContext: ApiRequestContext,
    body?: Record<string, unknown>,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  postAdminWorkflowConfig(
    requestContext: ApiRequestContext,
    body: Record<string, unknown>,
  ): Promise<Result<Record<string, unknown>, AppError>>;
  close(): Promise<void>;
};

/**
 * Starts the real API router for e2e tests so workflow coverage uses public,
 * generic workflow routes instead of workflow-specific TypeScript service imports.
 */
export async function createWorkflowApiClient(
  dependencies: AppDependencies,
): Promise<WorkflowApiClient> {
  const apiServer = createApiServer(dependencies);
  const apiOrigin = await startTestServer(apiServer);

  return {
    startWorkflowIntent(requestContext, body) {
      return postJson(apiOrigin, requestContext, "/workflow-intents", body);
    },
    transitionWorkflow(requestContext, workflowInstanceId, body) {
      return postJson(
        apiOrigin,
        requestContext,
        `/workflow-instances/${workflowInstanceId}/transitions`,
        body,
      );
    },
    createDocument(requestContext, body) {
      return postJson(apiOrigin, requestContext, "/documents", body);
    },
    getAvailableActions(requestContext, workflowInstanceId) {
      return getJson(
        apiOrigin,
        requestContext,
        `/workflow-instances/${workflowInstanceId}/available-actions`,
      );
    },
    getTasks(requestContext) {
      return getJson(apiOrigin, requestContext, "/tasks");
    },
    getTimeline(requestContext, workflowInstanceId, view) {
      const search = view === undefined ? "" : `?view=${encodeURIComponent(view)}`;

      return getJson(
        apiOrigin,
        requestContext,
        `/workflow-instances/${workflowInstanceId}/timeline${search}`,
      );
    },
    getEmployeeProjection(requestContext, employeeId) {
      return getJson(apiOrigin, requestContext, `/employees/${employeeId}`);
    },
    listEmployeeProjections(requestContext) {
      return getJson(apiOrigin, requestContext, "/employees");
    },
    postAdminWorkflowImport(requestContext, body = {}) {
      return postJson(
        apiOrigin,
        requestContext,
        "/admin/workflows/import-from-files",
        body,
      );
    },
    postAdminWorkflowConfig(requestContext, body) {
      return postJson(apiOrigin, requestContext, "/admin/workflows", body);
    },
    close() {
      return closeServer(apiServer);
    },
  };
}

async function getJson(
  apiOrigin: string,
  requestContext: ApiRequestContext,
  path: string,
): Promise<Result<Record<string, unknown>, AppError>> {
  return sendJson(apiOrigin, requestContext, "GET", path);
}

async function postJson(
  apiOrigin: string,
  requestContext: ApiRequestContext,
  path: string,
  body: Record<string, unknown>,
): Promise<Result<Record<string, unknown>, AppError>> {
  return sendJson(apiOrigin, requestContext, "POST", path, body);
}

async function sendJson(
  apiOrigin: string,
  requestContext: ApiRequestContext,
  method: "GET" | "POST",
  path: string,
  body?: Record<string, unknown>,
): Promise<Result<Record<string, unknown>, AppError>> {
  const response = await fetch(`${apiOrigin}${path}`, {
    method,
    headers: {
      "content-type": "application/json",
      "x-demo-actor-id": requestContext.actor.actorId,
      "x-request-id": requestContext.requestId,
      "x-correlation-id": requestContext.correlationId,
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  const responseBody = (await response.json()) as unknown;

  if (response.ok && isRecord(responseBody)) {
    return ok(responseBody);
  }

  return err(appErrorFromResponse(response.status, responseBody));
}

function appErrorFromResponse(status: number, body: unknown): AppError {
  const errorBody = isRecord(body) && isRecord(body["error"]) ? body["error"] : {};
  const code =
    typeof errorBody["code"] === "string"
      ? (errorBody["code"] as AppError["code"])
      : ERROR_CODES.SYSTEM_ERROR;
  const safeMessage =
    typeof errorBody["message"] === "string"
      ? errorBody["message"]
      : `Workflow API request failed with status ${status}.`;
  const details = isRecord(errorBody["details"]) ? errorBody["details"] : undefined;

  return {
    code,
    message: safeMessage,
    safeMessage,
    ...(details === undefined ? {} : { details }),
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function startTestServer(server: Server): Promise<string> {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address() as AddressInfo;
      resolve(`http://127.0.0.1:${address.port}`);
    });
  });
}

function closeServer(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) {
        reject(error);
        return;
      }

      resolve();
    });
  });
}
