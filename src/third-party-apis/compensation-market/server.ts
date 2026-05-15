import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { URL } from "node:url";
import {
  findCompensationMarketRecord,
  listCompensationMarketRecords,
  type CompensationMarketLookup,
} from "./data.js";

export const DEFAULT_COMPENSATION_MARKET_API_PORT = 4301;

type JsonHttpResponse = {
  status: number;
  body: Record<string, unknown>;
};

type RouteRequest = {
  method: string;
  pathname: string;
  searchParams: URLSearchParams;
};

/**
 * Creates a standalone simulated third-party compensation datasource server.
 */
export function createCompensationMarketApiServer() {
  return createServer((request, response) => {
    const jsonResponse = routeRequest(request);
    writeJsonResponse(response, jsonResponse);
  });
}

function routeRequest(request: IncomingMessage): JsonHttpResponse {
  const requestUrl = new URL(request.url ?? "/", requestOrigin(request));
  const routeRequest: RouteRequest = {
    method: request.method?.toUpperCase() ?? "GET",
    pathname: requestUrl.pathname,
    searchParams: requestUrl.searchParams,
  };

  if (routeRequest.method === "GET" && routeRequest.pathname === "/health") {
    return okResponse({
      status: "ok",
      service: "simulated-compensation-market-api",
    });
  }

  if (routeRequest.method === "GET" && routeRequest.pathname === "/v1/market-pricing") {
    return marketPricingResponse(routeRequest.searchParams);
  }

  if (
    routeRequest.method === "GET" &&
    routeRequest.pathname === "/v1/market-pricing/search"
  ) {
    return marketPricingSearchResponse(routeRequest.searchParams);
  }

  return errorResponse(404, "ROUTE_NOT_FOUND", "Route not found.");
}

function marketPricingResponse(searchParams: URLSearchParams): JsonHttpResponse {
  const lookup = compensationLookupFromSearch(searchParams);
  const marketRecord = findCompensationMarketRecord(lookup);

  if (marketRecord === undefined) {
    return errorResponse(
      404,
      "MARKET_RECORD_NOT_FOUND",
      "No compensation market record matched the supplied dimensions.",
    );
  }

  return okResponse({
    providerRequestId: providerRequestIdForLookup(lookup),
    record: marketRecord,
  });
}

function marketPricingSearchResponse(searchParams: URLSearchParams): JsonHttpResponse {
  const lookup = compensationLookupFromSearch(searchParams);
  const records = listCompensationMarketRecords(lookup);

  return okResponse({
    providerRequestId: providerRequestIdForLookup(lookup),
    records,
    total: records.length,
  });
}

function compensationLookupFromSearch(
  searchParams: URLSearchParams,
): CompensationMarketLookup {
  const lookup: CompensationMarketLookup = {};
  const jobCode = optionalSearchValue(searchParams, "jobCode");
  const level = optionalSearchValue(searchParams, "level");
  const location = optionalSearchValue(searchParams, "location");
  const payZone = optionalSearchValue(searchParams, "payZone");

  if (jobCode !== undefined) {
    lookup.jobCode = jobCode;
  }

  if (level !== undefined) {
    lookup.level = level;
  }

  if (location !== undefined) {
    lookup.location = location;
  }

  if (payZone !== undefined) {
    lookup.payZone = payZone;
  }

  return lookup;
}

function optionalSearchValue(
  searchParams: URLSearchParams,
  key: string,
): string | undefined {
  const value = searchParams.get(key)?.trim();

  return value === undefined || value.length === 0 ? undefined : value;
}

function providerRequestIdForLookup(lookup: CompensationMarketLookup): string {
  const lookupParts = [
    lookup.jobCode ?? "any-job",
    lookup.level ?? "any-level",
    lookup.payZone ?? lookup.location ?? "any-market",
  ];

  return `sim_comp_${lookupParts.join("_").replace(/[^a-zA-Z0-9]+/g, "_")}`;
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
