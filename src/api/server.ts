import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { randomUUID } from "node:crypto";
import { URL } from "node:url";
import {
  createStructuredLogger,
  fromPromise,
  fromThrowable,
  err,
  notFoundError,
  ok,
  systemError,
  validationFailedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import { getAppDependencies } from "./dependency-container.js";
import type { AppDependencies } from "./dependencies.js";
import {
  buildApiRequestContext,
  readJsonObject,
  type ApiRequestContext,
  type HeaderReader,
} from "./request-context.js";
import { toJsonHttpResponse, type JsonHttpResponse } from "./response.js";
import {
  deprecateWorkflowVersion,
  importWorkflowConfigPayload,
  importWorkflowConfigsFromFiles,
  listWorkflowAdminDefinitions,
  publishWorkflowVersion,
  validateWorkflowVersion,
} from "../workflows/admin/service.js";
import {
  archiveWorkflowFamilyForApi,
  blockCatalogForApi,
  clonePublishedWorkflowVersionForApi,
  cloneWorkflowTemplateForApi,
  createWorkflowDraftForApi,
  diffWorkflowConfigsForApi,
  exportWorkflowJsonForApi,
  getWorkflowRegistryDetailForApi,
  inputMappingPreviewForApi,
  integrationBindingValidationForApi,
  interactionPreviewForApi,
  importWorkflowJsonForApi,
  listIntegrationBindingsForApi,
  listWorkflowRegistryForApi,
  listWorkflowTemplatesForApi,
  mermaidPreviewForApi,
  permissionPreviewForApi,
  publishGuardrailsForApi,
  publishWorkflowDraftForApi,
  rejectWorkflowDraftForApi,
  requestWorkflowDraftReviewForApi,
  rollbackWorkflowFamilyForApi,
  saveWorkflowDraftForApi,
  seedWorkflowTemplatesForApi,
  simulateWorkflowForApi,
  upsertIntegrationBindingForApi,
  validateWorkflowJsonForApi,
} from "../workflows/admin/contracts/admin-route-handlers.js";
import {
  getWorkflowRuntimeDebugger,
  submitWorkflowAdminRepairAction,
} from "../workflows/admin/debugger/index.js";
import {
  createDocument,
  getAvailableActions,
  getDocument,
  getEmployeeProjection,
  getTasks,
  getTimeline,
  getWorkflowInstance,
  listEmployeeProjections,
  startWorkflowIntent,
  transitionWorkflow,
} from "../workflows/runtime/service.js";
import { handleAiGenerateUi } from "./ai-generate-ui.js";
import { handleAiChat } from "./ai-chat.js";

type HttpMethod = "DELETE" | "GET" | "PATCH" | "POST" | "PUT" | "OPTIONS" | "HEAD";

type RouteContext = {
  dependencies: AppDependencies;
  request: IncomingMessage;
  method: HttpMethod;
  segments: string[];
  searchParams: URLSearchParams;
};

/**
 * Creates the API HTTP server without framework-specific route nesting.
 */
export function createApiServer(dependencies = getAppDependencies()) {
  return createServer((request, response) => {
    void sendHandledRequest(dependencies, request, response);
  });
}

async function sendHandledRequest(
  dependencies: AppDependencies,
  request: IncomingMessage,
  response: ServerResponse,
): Promise<void> {
  const startMs = Date.now();
  const requestId =
    (request.headers["x-request-id"] as string | undefined) ?? randomUUID();
  const correlationId =
    (request.headers["x-correlation-id"] as string | undefined) ?? requestId;
  const requestLogger = createStructuredLogger({
    service: "api",
    requestId,
    correlationId,
  });
  const requestDependencies = { ...dependencies, logger: requestLogger };
  const requestPath = new URL(request.url ?? "/", requestOrigin(request)).pathname;

  requestLogger.info("request received", {
    method: request.method ?? "UNKNOWN",
    path: requestPath,
  });

  const handledResponseResult = await fromPromise(
    async () => handleApiRequest(requestDependencies, request),
    (error) => systemError({ routeBoundary: true }, error),
  );
  const handledResponse = handledResponseResult.ok
    ? handledResponseResult.value
    : toJsonHttpResponse(handledResponseResult);

  const durationMs = Date.now() - startMs;
  const completionLevel =
    handledResponse.status >= 500
      ? "error"
      : handledResponse.status >= 400
        ? "warn"
        : "info";
  requestLogger[completionLevel]("request completed", {
    method: request.method ?? "UNKNOWN",
    path: requestPath,
    status: handledResponse.status,
    durationMs,
  });

  writeJsonResponse(response, handledResponse);
}

async function handleApiRequest(
  dependencies: AppDependencies,
  request: IncomingMessage,
): Promise<JsonHttpResponse> {
  const requestUrl = new URL(request.url ?? "/", requestOrigin(request));
  const method = normalizeMethod(request.method);
  const routeContext: RouteContext = {
    dependencies,
    request,
    method,
    segments: pathSegments(requestUrl.pathname),
    searchParams: requestUrl.searchParams,
  };

  if (method === "GET" && routeContext.segments[0] === "health") {
    return toJsonHttpResponse(ok({ status: "ok" }));
  }

  return routeWorkflowRequest(routeContext);
}

async function routeWorkflowRequest(
  routeContext: RouteContext,
): Promise<JsonHttpResponse> {
  const segments = routeContext.segments;
  const method = routeContext.method;

  if (
    method === "GET" &&
    segments.length === 2 &&
    segments[0] === "admin" &&
    segments[1] === "workflows"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      listWorkflowAdminDefinitions(routeContext.dependencies, requestContext),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "import-from-files"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      importWorkflowConfigsFromFiles(routeContext.dependencies, requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 2 &&
    segments[0] === "admin" &&
    segments[1] === "workflows"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      importWorkflowConfigPayload(routeContext.dependencies, requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-versions" &&
    segments[3] === "validate"
  ) {
    return handleJsonCommand(routeContext, async (requestContext) =>
      validateWorkflowVersion(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-versions" &&
    segments[3] === "publish"
  ) {
    return handleJsonCommand(routeContext, async (requestContext) =>
      publishWorkflowVersion(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-versions" &&
    segments[3] === "deprecate"
  ) {
    return handleJsonCommand(routeContext, async (requestContext) =>
      deprecateWorkflowVersion(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
      ),
    );
  }

  if (
    method === "GET" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-instances" &&
    segments[3] === "debug"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      getWorkflowRuntimeDebugger(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-instances" &&
    segments[3] === "repair-actions"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      submitWorkflowAdminRepairAction(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
        body,
      ),
    );
  }

  if (
    method === "GET" &&
    segments.length === 2 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-families"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      listWorkflowRegistryForApi(routeContext.dependencies, requestContext),
    );
  }

  if (
    method === "GET" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-families"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      getWorkflowRegistryDetailForApi(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-families" &&
    segments[3] === "rollback"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      rollbackWorkflowFamilyForApi(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
        body,
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-families" &&
    segments[3] === "archive"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      archiveWorkflowFamilyForApi(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
        body,
      ),
    );
  }

  if (
    method === "GET" &&
    segments.length === 2 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-integration-bindings"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      listIntegrationBindingsForApi(routeContext.dependencies, requestContext),
    );
  }

  if (
    method === "POST" &&
    segments.length === 2 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-integration-bindings"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      upsertIntegrationBindingForApi(routeContext.dependencies, requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-templates" &&
    segments[2] === "seed"
  ) {
    return handleJsonCommand(routeContext, async (requestContext) =>
      seedWorkflowTemplatesForApi(routeContext.dependencies, requestContext),
    );
  }

  if (
    method === "GET" &&
    segments.length === 2 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-templates"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      listWorkflowTemplatesForApi(routeContext.dependencies, requestContext),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-templates" &&
    segments[3] === "clone"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      cloneWorkflowTemplateForApi(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
        body,
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "published-workflow-versions" &&
    segments[3] === "clone"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      clonePublishedWorkflowVersionForApi(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
        body,
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 2 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-drafts"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      createWorkflowDraftForApi(routeContext.dependencies, requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-drafts" &&
    segments[3] === "save"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      saveWorkflowDraftForApi(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
        body,
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-drafts" &&
    segments[3] === "request-review"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      requestWorkflowDraftReviewForApi(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
        body,
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-drafts" &&
    segments[3] === "reject"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      rejectWorkflowDraftForApi(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
        body,
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-drafts" &&
    segments[3] === "publish"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      publishWorkflowDraftForApi(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
        body,
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "validate-json"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      validateWorkflowJsonForApi(requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "export-json"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      exportWorkflowJsonForApi(requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "import-json"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      importWorkflowJsonForApi(requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "mermaid-preview"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      mermaidPreviewForApi(requestContext, body),
    );
  }

  if (
    method === "GET" &&
    segments.length === 2 &&
    segments[0] === "admin" &&
    segments[1] === "workflow-blocks"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      blockCatalogForApi(requestContext),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "input-mapping-preview"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      inputMappingPreviewForApi(requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "interaction-preview"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      interactionPreviewForApi(requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "permission-preview"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      permissionPreviewForApi(
        routeContext.dependencies.repositories,
        requestContext,
        body,
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "simulate"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      simulateWorkflowForApi(
        routeContext.dependencies.repositories,
        requestContext,
        body,
      ),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "diff"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      diffWorkflowConfigsForApi(requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 4 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "integration-bindings" &&
    segments[3] === "validate"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      integrationBindingValidationForApi(requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "admin" &&
    segments[1] === "workflows" &&
    segments[2] === "publish-guardrails"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      publishGuardrailsForApi(requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 1 &&
    segments[0] === "workflow-intents"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      startWorkflowIntent(routeContext.dependencies, requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 2 &&
    segments[0] === "ai" &&
    segments[1] === "generate-ui"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      handleAiGenerateUi(routeContext.dependencies, requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 2 &&
    segments[0] === "ai" &&
    segments[1] === "chat"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      handleAiChat(routeContext.dependencies, requestContext, body),
    );
  }

  if (
    method === "POST" &&
    segments.length === 3 &&
    segments[0] === "workflow-instances" &&
    segments[2] === "transitions"
  ) {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      transitionWorkflow(
        routeContext.dependencies,
        requestContext,
        segments[1] ?? "",
        body,
      ),
    );
  }

  if (
    method === "GET" &&
    segments.length === 2 &&
    segments[0] === "workflow-instances"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      getWorkflowInstance(routeContext.dependencies, requestContext, segments[1] ?? ""),
    );
  }

  if (
    method === "GET" &&
    segments.length === 3 &&
    segments[0] === "workflow-instances" &&
    segments[2] === "available-actions"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      getAvailableActions(routeContext.dependencies, requestContext, segments[1] ?? ""),
    );
  }

  if (
    method === "GET" &&
    segments.length === 3 &&
    segments[0] === "workflow-instances" &&
    segments[2] === "timeline"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      getTimeline(
        routeContext.dependencies,
        requestContext,
        segments[1] ?? "",
        routeContext.searchParams.get("view") ?? undefined,
      ),
    );
  }

  if (method === "POST" && segments.length === 1 && segments[0] === "documents") {
    return handleJsonCommand(routeContext, async (requestContext, body) =>
      createDocument(routeContext.dependencies, requestContext, body),
    );
  }

  if (method === "GET" && segments.length === 2 && segments[0] === "documents") {
    return handleQuery(routeContext, (requestContext) =>
      getDocument(routeContext.dependencies, requestContext, segments[1] ?? ""),
    );
  }

  if (method === "GET" && segments.length === 1 && segments[0] === "tasks") {
    return handleQuery(routeContext, (requestContext) =>
      getTasks(routeContext.dependencies, requestContext),
    );
  }

  if (method === "GET" && segments.length === 1 && segments[0] === "employees") {
    return handleQuery(routeContext, (requestContext) =>
      listEmployeeProjections(routeContext.dependencies, requestContext),
    );
  }

  if (method === "GET" && segments.length === 2 && segments[0] === "employees") {
    return handleQuery(routeContext, (requestContext) =>
      getEmployeeProjection(
        routeContext.dependencies,
        requestContext,
        segments[1] ?? "",
      ),
    );
  }

  if (
    method === "GET" &&
    segments.length === 3 &&
    segments[0] === "demo" &&
    segments[1] === "employee-projections"
  ) {
    return handleQuery(routeContext, (requestContext) =>
      getEmployeeProjection(
        routeContext.dependencies,
        requestContext,
        segments[2] ?? "",
      ),
    );
  }

  return toJsonHttpResponse(
    err(
      notFoundError("Route", {
        method,
        path: `/${segments.join("/")}`,
      }),
    ),
  );
}

async function handleJsonCommand(
  routeContext: RouteContext,
  handler: (
    requestContext: ApiRequestContext,
    body: Record<string, unknown>,
  ) =>
    | Result<Record<string, unknown>, AppError>
    | Promise<Result<Record<string, unknown>, AppError>>,
): Promise<JsonHttpResponse> {
  const contextResult = requestContextForRoute(routeContext);
  if (!contextResult.ok) {
    return toJsonHttpResponse(contextResult);
  }

  const bodyResult = await readJsonRequestBody(routeContext.request);
  if (!bodyResult.ok) {
    return toJsonHttpResponse(bodyResult);
  }

  const result = await handler(contextResult.value, bodyResult.value);
  return toJsonHttpResponse(result);
}

function handleQuery(
  routeContext: RouteContext,
  handler: (
    requestContext: ApiRequestContext,
  ) => Result<Record<string, unknown>, AppError>,
): JsonHttpResponse {
  const contextResult = requestContextForRoute(routeContext);
  if (!contextResult.ok) {
    return toJsonHttpResponse(contextResult);
  }

  return toJsonHttpResponse(handler(contextResult.value));
}

function requestContextForRoute(
  routeContext: RouteContext,
): Result<ApiRequestContext, AppError> {
  return buildApiRequestContext(
    {
      headers: headerReaderForRequest(routeContext.request),
    },
    routeContext.dependencies.repositories,
  );
}

async function readJsonRequestBody(
  request: IncomingMessage,
): Promise<Result<Record<string, unknown>, AppError>> {
  const bodyTextResult = await fromPromise(
    async () => {
      let bodyText = "";

      for await (const chunk of request) {
        bodyText += Buffer.isBuffer(chunk) ? chunk.toString("utf8") : String(chunk);
      }

      return bodyText;
    },
    () => validationFailedError({ body: "Unable to read request body." }),
  );
  if (!bodyTextResult.ok) {
    return bodyTextResult;
  }

  if (bodyTextResult.value.trim().length === 0) {
    return ok({});
  }

  const parsedJsonResult = fromThrowable(
    () => JSON.parse(bodyTextResult.value) as unknown,
    () => validationFailedError({ body: "Invalid JSON body." }),
  );
  if (!parsedJsonResult.ok) {
    return parsedJsonResult;
  }

  return readJsonObject(parsedJsonResult.value);
}

function writeJsonResponse(
  response: ServerResponse,
  handledResponse: JsonHttpResponse,
): void {
  response.statusCode = handledResponse.status;
  response.setHeader("content-type", "application/json; charset=utf-8");
  response.end(JSON.stringify(handledResponse.body));
}

function headerReaderForRequest(request: IncomingMessage): HeaderReader {
  return {
    get(name: string) {
      const headerValue = request.headers[name.toLowerCase()];

      if (Array.isArray(headerValue)) {
        return headerValue[0] ?? null;
      }

      return headerValue ?? null;
    },
  };
}

function requestOrigin(request: IncomingMessage): string {
  const host = request.headers.host ?? "localhost";
  return `http://${host}`;
}

function normalizeMethod(method: string | undefined): HttpMethod {
  const methodValue = method?.toUpperCase();

  if (
    methodValue === "DELETE" ||
    methodValue === "GET" ||
    methodValue === "PATCH" ||
    methodValue === "POST" ||
    methodValue === "PUT" ||
    methodValue === "OPTIONS" ||
    methodValue === "HEAD"
  ) {
    return methodValue;
  }

  return "GET";
}

function pathSegments(pathname: string): string[] {
  const apiPath =
    pathname === "/api"
      ? "/"
      : pathname.startsWith("/api/")
        ? pathname.slice("/api".length)
        : pathname;

  return apiPath
    .split("/")
    .filter((segment) => segment.length > 0)
    .map((segment) => decodeURIComponent(segment));
}
