import { ok } from "@hcm-next/foundation";
import type { AiChangeReview, AiClient } from "./ai-client.js";

const NULL_REVIEW: AiChangeReview = {
  summary: "AI review is disabled in this environment.",
  riskLevel: "low",
  missingData: [],
  policyFlags: [],
  downstreamImpact: [],
  recommendedNextAction: "Proceed with manual review.",
  providerMetadata: {
    provider: "null",
    model: "none",
    inputTokens: 0,
    outputTokens: 0,
  },
};

/**
 * No-op AiClient for test environments and deployments where AI review
 * is intentionally disabled. Always returns a fixed placeholder review.
 */
export function createNullAiClient(): AiClient {
  return {
    async generateChangeReview() {
      return ok(NULL_REVIEW);
    },
  };
}
