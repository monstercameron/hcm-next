import {
  DEFAULT_COMPENSATION_MARKET_API_PORT,
  createCompensationMarketApiServer,
} from "./server.js";

const configuredPort = process.env["THIRD_PARTY_COMPENSATION_API_PORT"];
const port =
  configuredPort === undefined
    ? DEFAULT_COMPENSATION_MARKET_API_PORT
    : Number.parseInt(configuredPort, 10);
const server = createCompensationMarketApiServer();

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(
    JSON.stringify({
      service: "simulated-compensation-market-api",
      origin: `http://127.0.0.1:${port}`,
    }),
  );
  process.stdout.write("\n");
});
