import { closeDatabasePool, createDatabasePool } from "../client";
import { runDemoSeed } from "../seeds/seed-runner";

async function main(): Promise<void> {
  const pool = createDatabasePool();
  const seedResult = await runDemoSeed(pool);
  const closeResult = await closeDatabasePool(pool);

  if (!seedResult.ok) {
    process.stderr.write(`${JSON.stringify(seedResult.error)}\n`);
    process.exitCode = 1;
    return;
  }

  if (!closeResult.ok) {
    process.stderr.write(`${JSON.stringify(closeResult.error)}\n`);
    process.exitCode = 1;
    return;
  }

  process.stdout.write(`${JSON.stringify(seedResult.value, null, 2)}\n`);
}

void main();
