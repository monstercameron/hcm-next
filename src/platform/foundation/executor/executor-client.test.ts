import { describe, expect, it } from "vitest";

import { ERROR_CODES } from "../errors";
import {
  GO_EXECUTOR_BLOCKS,
  GO_EXECUTOR_BLOCK_VERSION,
  GO_EXECUTOR_STATUS,
  type GoExecutorRequest,
} from "./executor-contract";
import { createGoExecutorClient, type ExecutorFetch } from "./executor-client";

describe("createGoExecutorClient", () => {
  it("returns successful executor responses", async () => {
    const fetchFunction: ExecutorFetch = () =>
      Promise.resolve({
        ok: true,
        status: 200,
        json: () =>
          Promise.resolve({
            status: GO_EXECUTOR_STATUS.SUCCEEDED,
            output: {
              valid: true,
              riskLevel: "low",
              requiresEvidence: true,
              requiresApproval: true,
              warnings: [],
              errors: [],
            },
            proposedEvents: [],
            externalCallRequests: [],
            logs: [],
            metrics: {
              durationMs: 1,
            },
          }),
      });

    const client = createGoExecutorClient({
      baseUrl: "http://executor.test/",
      fetchFunction,
    });

    const result = await client.executeBlock(requestFixture());

    expect(result.ok).toBe(true);

    if (!result.ok) {
      throw new Error("Expected ok result.");
    }

    expect(result.value.status).toBe(GO_EXECUTOR_STATUS.SUCCEEDED);
  });

  it("maps failed executor responses", async () => {
    const fetchFunction: ExecutorFetch = () =>
      Promise.resolve({
        ok: false,
        status: 404,
        json: () =>
          Promise.resolve({
            status: GO_EXECUTOR_STATUS.FAILED,
            output: {},
            proposedEvents: [],
            externalCallRequests: [],
            logs: [],
            metrics: {
              durationMs: 1,
            },
            error: {
              code: "unknown_block",
              message:
                "No registered executor block matches the requested name and version.",
              safeMessage: "The requested executor block is not available.",
            },
          }),
      });

    const client = createGoExecutorClient({
      baseUrl: "http://executor.test",
      fetchFunction,
    });

    const result = await client.executeBlock(requestFixture());

    expect(result.ok).toBe(false);

    if (result.ok) {
      throw new Error("Expected error result.");
    }

    expect(result.error.code).toBe(ERROR_CODES.GO_EXECUTOR_ERROR);
    expect(
      (result.error.details?.executorError as { code?: string } | undefined)?.code,
    ).toBe("unknown_block");
  });
});

function requestFixture(): GoExecutorRequest {
  return {
    tenantId: "tenant_demo",
    environmentId: "env_demo",
    workflowInstanceId: "wfi_test",
    workflowVersionId: "wfv_test",
    block: {
      name: GO_EXECUTOR_BLOCKS.LEGAL_NAME_PREFLIGHT,
      version: GO_EXECUTOR_BLOCK_VERSION.V1,
    },
    input: {
      currentLegalName: {
        first: "Jane",
        middle: null,
        last: "Doe",
      },
      proposedLegalName: {
        first: "Jane",
        middle: null,
        last: "Rivera",
      },
      effectiveAt: "2026-06-01",
      businessReason: "legal_name_change",
    },
    context: {
      actorId: "actor_employee_jane",
      effectiveAt: "2026-05-15",
      permissions: {},
      correlationId: "corr_test",
      idempotencyKey: "idem_test",
    },
  };
}
