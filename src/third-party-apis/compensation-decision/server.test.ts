import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it } from "vitest";
import { createCompensationDecisionApiServer } from "./server.js";

let activeServer: Server | undefined;

describe("simulated compensation decision API", () => {
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
    expect(body["service"]).toBe("simulated-compensation-decision-api");
  });

  it("returns a good decision for a sane compensation raise", async () => {
    const origin = await startTestServer();
    const response = await postDecision(origin, {
      currentCompensation: compensationFixture(93000),
      proposedCompensation: compensationFixture(98000),
    });
    const body = (await response.json()) as Record<string, unknown>;

    expect(response.status).toBe(200);
    expect(body["decision"]).toBe("good");
    expect(body["status"]).toBe("accepted");
    expect(body["reasonCodes"]).toEqual([]);
  });

  it("returns a bad decision for an excessive compensation raise", async () => {
    const origin = await startTestServer();
    const response = await postDecision(origin, {
      currentCompensation: compensationFixture(93000),
      proposedCompensation: compensationFixture(125000),
    });
    const body = (await response.json()) as Record<string, unknown>;

    expect(response.status).toBe(200);
    expect(body["decision"]).toBe("bad");
    expect(body["status"]).toBe("rejected");
    expect(body["reasonCodes"]).toEqual(
      expect.arrayContaining(["compensation.increase_over_twenty_percent"]),
    );
  });
});

function startTestServer(): Promise<string> {
  activeServer = createCompensationDecisionApiServer();

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

function postDecision(
  origin: string,
  compensation: {
    currentCompensation: Record<string, unknown>;
    proposedCompensation: Record<string, unknown>;
  },
) {
  return fetch(`${origin}/v1/compensation-decisions`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      workerId: "emp_123",
      changeRequestId: "chg_123",
      currentCompensation: compensation.currentCompensation,
      proposedCompensation: compensation.proposedCompensation,
      effectiveAt: "2026-06-01",
      businessReason: "retention_adjustment",
    }),
  });
}

function compensationFixture(amount: number): Record<string, unknown> {
  return {
    amount,
    currency: "USD",
    payFrequency: "annual",
    bonusTargetPercent: 5,
    effectiveDate: "2026-06-01",
  };
}
