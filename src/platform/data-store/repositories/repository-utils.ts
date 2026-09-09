import type { QueryResultRow } from "pg";

import {
  err,
  fromPromise,
  mapUnknownToDatabaseError,
  notFoundError,
  ok,
  type AppError,
  type JsonRecord,
  type Result,
} from "@human-capital-management-suite/foundation";

import type { DatabaseClient } from "../client";

/**
 * Runs a parameterized SQL statement through the repository boundary.
 */
export async function runQuery<TRow extends QueryResultRow>(
  database: DatabaseClient,
  sql: string,
  values: readonly unknown[] = [],
): Promise<Result<TRow[]>> {
  const queryResult = await fromPromise(
    async () => database.query<TRow>(sql, values),
    mapUnknownToDatabaseError,
  );

  if (!queryResult.ok) {
    return queryResult;
  }

  return ok(queryResult.value.rows);
}

/**
 * Runs a query that must return one row.
 */
export async function findOne<TRow extends QueryResultRow>(
  database: DatabaseClient,
  sql: string,
  values: readonly unknown[],
  resourceName: string,
  details?: JsonRecord,
): Promise<Result<TRow>> {
  const rowsResult = await runQuery<TRow>(database, sql, values);

  if (!rowsResult.ok) {
    return rowsResult;
  }

  const row = rowsResult.value[0];

  if (row === undefined) {
    return err(notFoundError(resourceName, details));
  }

  return ok(row);
}

/**
 * Converts a missing mutation row into a typed not-found error.
 */
export function requireMutationRow<TRow>(
  row: TRow | undefined,
  resourceName: string,
  details?: JsonRecord,
): Result<TRow, AppError> {
  if (row === undefined) {
    return err(notFoundError(resourceName, details));
  }

  return ok(row);
}
