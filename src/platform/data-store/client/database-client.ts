import { Pool, type PoolConfig, type QueryResult, type QueryResultRow } from "pg";

import {
  CONFIG_DEFAULTS,
  fromPromise,
  mapUnknownToDatabaseError,
  ok,
  type Result,
} from "@human-capital-management-suite/foundation";

export type DatabaseClient = {
  query: <TRow extends QueryResultRow = QueryResultRow>(
    text: string,
    values?: readonly unknown[],
  ) => Promise<QueryResult<TRow>>;
};

/**
 * Resolves the Postgres connection string from explicit config or environment.
 */
export function resolveDatabaseUrl(
  databaseUrl: string | undefined = process.env.DATABASE_URL,
): string {
  return databaseUrl ?? CONFIG_DEFAULTS.DATABASE_URL;
}

/**
 * Creates a Postgres pool for repository and script boundaries.
 */
export function createDatabasePool(config: PoolConfig = {}): Pool {
  return new Pool({
    ...config,
    connectionString: resolveDatabaseUrl(config.connectionString),
  });
}

/**
 * Closes a Postgres pool and returns a checked Result.
 */
export async function closeDatabasePool(pool: Pool): Promise<Result<void>> {
  const closeResult = await fromPromise(
    async () => pool.end(),
    mapUnknownToDatabaseError,
  );

  if (!closeResult.ok) {
    return closeResult;
  }

  return ok(undefined);
}
