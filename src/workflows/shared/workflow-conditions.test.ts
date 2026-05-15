import { describe, expect, it } from "vitest";
import { workflowConditionMatches } from "./workflow-conditions.js";

describe("workflowConditionMatches", () => {
  const sources = {
    input: {
      compensation: {
        increasePercent: 12,
        reason: "market_adjustment",
      },
    },
    externalWriteResponse: {
      status: "accepted",
      score: 91,
    },
  };

  it("matches simple comparison operators", () => {
    expect(
      workflowConditionMatches(
        {
          $source: "externalWriteResponse",
          path: "status",
          equals: "accepted",
        },
        sources,
      ),
    ).toBe(true);
    expect(
      workflowConditionMatches(
        {
          $source: "input",
          path: "compensation.reason",
          notEquals: "correction",
        },
        sources,
      ),
    ).toBe(true);
    expect(
      workflowConditionMatches(
        {
          $source: "input",
          path: "compensation.reason",
          in: ["market_adjustment", "promotion"],
        },
        sources,
      ),
    ).toBe(true);
    expect(
      workflowConditionMatches(
        {
          $source: "input",
          path: "compensation.missingField",
          exists: false,
        },
        sources,
      ),
    ).toBe(true);
  });

  it("matches composite all any and not operators", () => {
    expect(
      workflowConditionMatches(
        {
          all: [
            {
              $source: "externalWriteResponse",
              path: "status",
              equals: "accepted",
            },
            {
              any: [
                {
                  $source: "input",
                  path: "compensation.reason",
                  equals: "promotion",
                },
                {
                  $source: "input",
                  path: "compensation.reason",
                  equals: "market_adjustment",
                },
              ],
            },
            {
              not: {
                $source: "input",
                path: "compensation.reason",
                equals: "correction",
              },
            },
          ],
        },
        sources,
      ),
    ).toBe(true);
  });
});
