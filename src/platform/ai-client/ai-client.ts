import type { AppError, Result, StructuredLogger } from "@hcm-next/foundation";

/**
 * Permission-gated input for an AI change review. The caller is responsible
 * for filtering currentState and proposedState to only include fields the
 * acting user is authorized to see before passing them here.
 */
export type AiChangeReviewRequest = {
  changeType: string;
  currentState: Record<string, unknown>;
  proposedState: Record<string, unknown>;
  /** Field names visible to the actor — logged for audit without values. */
  visibleFields: string[];
  workflowInstanceId: string;
  changeRequestId: string;
  correlationId: string;
  idempotencyKey: string;
};

export type AiRiskLevel = "low" | "medium" | "high";

export type AiChangeReview = {
  summary: string;
  riskLevel: AiRiskLevel;
  missingData: string[];
  policyFlags: string[];
  downstreamImpact: string[];
  recommendedNextAction: string;
  providerMetadata: {
    provider: string;
    model: string;
    inputTokens: number;
    outputTokens: number;
  };
};

/**
 * Provider-agnostic AI client. Implementations wrap a specific inference
 * provider (OpenAI, Anthropic, Azure OpenAI, etc.) behind this interface.
 * Callers never depend on provider SDK types.
 */
export type AiClient = {
  generateChangeReview(
    request: AiChangeReviewRequest,
    logger?: StructuredLogger,
  ): Promise<Result<AiChangeReview, AppError>>;
};
