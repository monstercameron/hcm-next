import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it } from "vitest";
import { createCompensationMarketApiServer } from "./server.js";

let activeServer: Server | undefined;

describe("simulated compensation market API", () => {
  afterEach(async () => {
    if (activeServer !== undefined) {
      await closeServer(activeServer);
      activeServer = undefined;
    }
  });

  it("serves health checks from the standalone third-party API", async () => {
    const origin = await startTestServer();
    const response = await fetch(`${origin}/health`);
    const body = (await response.json()) as Record<string, unknown>;

    expect(response.status).toBe(200);
    expect(body["service"]).toBe("simulated-compensation-market-api");
  });

  it("returns market pricing for a compensation raise workflow lookup", async () => {
    const origin = await startTestServer();
    const response = await fetch(
      `${origin}/v1/market-pricing?jobCode=ENG-SWE3&level=P3&payZone=US-WEST`,
    );
    const body = (await response.json()) as {
      record?: {
        jobCode?: string;
        annualBaseSalary?: { p50?: number };
      };
    };

    expect(response.status).toBe(200);
    expect(body.record?.jobCode).toBe("ENG-SWE3");
    expect(body.record?.annualBaseSalary?.p50).toBe(171000);
  });

  it("returns vendor-style not-found errors for missing market data", async () => {
    const origin = await startTestServer();
    const response = await fetch(
      `${origin}/v1/market-pricing?jobCode=NOPE&level=P1&payZone=US-WEST`,
    );
    const body = (await response.json()) as {
      error?: {
        code?: string;
      };
    };

    expect(response.status).toBe(404);
    expect(body.error?.code).toBe("MARKET_RECORD_NOT_FOUND");
  });
});

function startTestServer(): Promise<string> {
  activeServer = createCompensationMarketApiServer();

  return new Promise((resolve, reject) => {
    activeServer?.once("error", reject);
    activeServer?.listen(0, "127.0.0.1", () => {
      const address = activeServer?.address() as AddressInfo;
      resolve(`http://127.0.0.1:${address.port}`);
    });
  });
}

function closeServer(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) {
        reject(error);
        return;
      }

      resolve();
    });
  });
}
