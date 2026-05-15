import { err, systemError, type AppError, type Result } from "@hcm-next/foundation";
import { getAppDependencies } from "./dependency-container.js";
import type { AppDependencies } from "./dependencies.js";
import { buildApiRequestContext, type ApiRequestContext } from "./request-context.js";
import { toJsonHttpResponse, type JsonHttpResponse } from "./response.js";

type RouteHandlerInput<TParams extends Record<string, string>> = {
  dependencies: AppDependencies;
  requestContext: ApiRequestContext;
  params: TParams;
};

type JsonRouteHandlerInput<TParams extends Record<string, string>> =
  RouteHandlerInput<TParams> & {
    body: Record<string, unknown>;
  };

type RequestLike = Parameters<typeof buildApiRequestContext>[0];

/**
 * Runs a query route with shared dependency/context construction.
 */
export function handleQueryRoute<TParams extends Record<string, string>>(
  request: RequestLike,
  params: TParams,
  handler: (
    input: RouteHandlerInput<TParams>,
  ) => Result<Record<string, unknown>, AppError>,
): JsonHttpResponse {
  const dependencies = getAppDependencies();
  const contextResult = buildApiRequestContext(request, dependencies.repositories);

  if (!contextResult.ok) {
    return toJsonHttpResponse(contextResult);
  }

  const result = handler({
    dependencies,
    requestContext: contextResult.value,
    params,
  });

  return toJsonHttpResponse(result);
}

/**
 * Runs a JSON command route with shared dependency/context construction.
 */
export async function handleJsonCommandRoute<TParams extends Record<string, string>>(
  request: RequestLike,
  params: TParams,
  body: Record<string, unknown>,
  handler: (
    input: JsonRouteHandlerInput<TParams>,
  ) =>
    | Result<Record<string, unknown>, AppError>
    | Promise<Result<Record<string, unknown>, AppError>>,
): Promise<JsonHttpResponse> {
  const dependencies = getAppDependencies();
  const contextResult = buildApiRequestContext(request, dependencies.repositories);

  if (!contextResult.ok) {
    return toJsonHttpResponse(contextResult);
  }

  const result = await handler({
    dependencies,
    requestContext: contextResult.value,
    params,
    body,
  });

  return toJsonHttpResponse(result);
}

/**
 * Converts unexpected route boundary failures into a typed API response.
 */
export function routeBoundaryError(error: unknown): JsonHttpResponse {
  return toJsonHttpResponse(err(systemError({ routeBoundary: true }, error)));
}
