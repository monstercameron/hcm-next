// Load .env files before any downstream module reads process.env. Searches
// src/api/.env (cwd) first, then the repo-root .env. Missing files are
// silently skipped. `getAppDependencies()` is called lazily inside
// `createApiServer()` below, so OPENAI_API_KEY is in place before the AI
// client constructor runs.
import { config as loadDotenv } from "dotenv";
import { resolve } from "node:path";
loadDotenv({ path: resolve(process.cwd(), ".env") });
loadDotenv({ path: resolve(process.cwd(), "../../.env") });

import { createApiServer } from "./server.js";

const defaultPort = 3000;
const port = Number(process.env["PORT"] ?? defaultPort);
const server = createApiServer();

server.listen(port, () => {
  process.stdout.write(`HCM Next API listening on http://localhost:${port}\n`);
});
