import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { URL } from "node:url";
import {
  fromPromise,
  fromThrowable,
  err,
  notFoundError,
  ok,
  systemError,
  validationFailedError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
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
} from "../workflows/legal-name-change/service.js";

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
  const handledResponseResult = await fromPromise(
    async () => handleApiRequest(dependencies, request),
    (error) => systemError({ routeBoundary: true }, error),
  );
  const handledResponse = handledResponseResult.ok
    ? handledResponseResult.value
    : toJsonHttpResponse(handledResponseResult);

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
