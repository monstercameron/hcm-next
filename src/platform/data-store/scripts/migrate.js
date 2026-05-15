import { readFileSync } from "node:fs";
import { join } from "node:path";

const migrationPath = join(
  process.cwd(),
  "src/platform/data-store/migrations/001_initial.sql",
);
const migration = readFileSync(migrationPath, "utf8");

process.stdout.write(
  `Migration file is ready at ${migrationPath} (${migration.length} bytes).\n`,
);
process.stdout.write(
  "V0 uses the in-memory repository for tests/demo unless DATABASE_URL is wired later.\n",
);
