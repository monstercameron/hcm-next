import { closeDatabasePool, createDatabasePool } from "../client";
import { runPendingMigrations } from "../migrations/migration-runner";

async function main(): Promise<void> {
  const pool = createDatabasePool();
  const migrationResult = await runPendingMigrations(pool);
  const closeResult = await closeDatabasePool(pool);

  if (!migrationResult.ok) {
    process.stderr.write(`${JSON.stringify(migrationResult.error)}\n`);
    process.exitCode = 1;
    return;
  }

  if (!closeResult.ok) {
    process.stderr.write(`${JSON.stringify(closeResult.error)}\n`);
    process.exitCode = 1;
    return;
  }

  process.stdout.write(`${JSON.stringify(migrationResult.value, null, 2)}\n`);
}

void main();
