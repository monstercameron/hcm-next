import { createApiServer } from "./server.js";

const defaultPort = 3000;
const port = Number(process.env["PORT"] ?? defaultPort);
const server = createApiServer();

server.listen(port, () => {
  process.stdout.write(`HCM Next API listening on http://localhost:${port}\n`);
});
