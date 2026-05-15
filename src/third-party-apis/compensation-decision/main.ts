import {
  DEFAULT_COMPENSATION_DECISION_API_PORT,
  createCompensationDecisionApiServer,
} from "./server.js";

const configuredPort = process.env["THIRD_PARTY_COMPENSATION_DECISION_API_PORT"];
const port =
  configuredPort === undefined
    ? DEFAULT_COMPENSATION_DECISION_API_PORT
    : Number.parseInt(configuredPort, 10);
const server = createCompensationDecisionApiServer();

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(
    JSON.stringify({
      service: "simulated-compensation-decision-api",
      origin: `http://127.0.0.1:${port}`,
    }),
  );
  process.stdout.write("\n");
});
