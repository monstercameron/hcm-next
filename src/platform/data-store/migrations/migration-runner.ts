import { readdir, readFile } from "node:fs/promises";
import path from "node:path";

import {
  fromPromise,
  mapUnknownToDatabaseError,
  mapUnknownToSystemError,
  ok,
  type Result,
} from "@human-capital-management-suite/foundation";

import { executeInTransaction, type DatabaseClient } from "../client";
import type { Pool } from "pg";

export type MigrationSummary = {
  applied: readonly string[];
  skipped: readonly string[];
};

const MIGRATIONS_DIRECTORY = path.resolve(__dirname);

async function ensureMigrationTable(database: DatabaseClient): Promise<Result<void>> {
  const result = await fromPromise(
    async () =>
      database.query(`
        CREATE TABLE IF NOT EXISTS schema_migrations (
          migration_name text PRIMARY KEY,
          applied_at timestamptz NOT NULL DEFAULT now()
        )
      `),
    mapUnknownToDatabaseError,
  );

  if (!result.ok) {
    return result;
  }

  return ok(undefined);
}

async function hasMigrationApplied(
  database: DatabaseClient,
  migrationName: string,
): Promise<Result<boolean>> {
  const result = await fromPromise(
    async () =>
      database.query<{ migration_name: string }>(
        "SELECT migration_name FROM schema_migrations WHERE migration_name = $1",
        [migrationName],
      ),
    mapUnknownToDatabaseError,
  );

  if (!result.ok) {
    return result;
  }

  return ok((result.value.rowCount ?? 0) > 0);
}

async function markMigrationApplied(
  database: DatabaseClient,
  migrationName: string,
): Promise<Result<void>> {
  const result = await fromPromise(
    async () =>
      database.query(
        `
          INSERT INTO schema_migrations (migration_name)
          VALUES ($1)
          ON CONFLICT (migration_name) DO NOTHING
        `,
        [migrationName],
      ),
    mapUnknownToDatabaseError,
  );

  if (!result.ok) {
    return result;
  }

  return ok(undefined);
}

/**
 * Applies pending SQL migrations in lexical filename order.
 */
export async function runPendingMigrations(
  pool: Pool,
  migrationsDirectory: string = MIGRATIONS_DIRECTORY,
): Promise<Result<MigrationSummary>> {
  const ensureResult = await ensureMigrationTable(pool);

  if (!ensureResult.ok) {
    return ensureResult;
  }

  const fileNamesResult = await fromPromise(
    async () => readdir(migrationsDirectory),
    mapUnknownToSystemError,
  );

  if (!fileNamesResult.ok) {
    return fileNamesResult;
  }

  const migrationFileNames = fileNamesResult.value
    .filter((fileName) => /^\d{4}_.+\.sql$/.test(fileName))
    .sort();
  const applied: string[] = [];
  const skipped: string[] = [];

  for (const migrationFileName of migrationFileNames) {
    const appliedResult = await hasMigrationApplied(pool, migrationFileName);

    if (!appliedResult.ok) {
      return appliedResult;
    }

    if (appliedResult.value) {
      skipped.push(migrationFileName);
      continue;
    }

    const migrationPath = path.join(migrationsDirectory, migrationFileName);
    const migrationSqlResult = await fromPromise(
      async () => readFile(migrationPath, "utf8"),
      mapUnknownToSystemError,
    );

    if (!migrationSqlResult.ok) {
      return migrationSqlResult;
    }

    const transactionResult = await executeInTransaction(pool, async (client) => {
      const migrationResult = await fromPromise(
        async () => client.query(migrationSqlResult.value),
        mapUnknownToDatabaseError,
      );

      if (!migrationResult.ok) {
        return migrationResult;
      }

      const markResult = await markMigrationApplied(client, migrationFileName);

      if (!markResult.ok) {
        return markResult;
      }

      return ok(undefined);
    });

    if (!transactionResult.ok) {
      return transactionResult;
    }

    applied.push(migrationFileName);
  }

  return ok({
    applied,
    skipped,
  });
}
