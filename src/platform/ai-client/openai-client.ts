import OpenAI from "openai";
import {
  aiReviewError,
  fromPromise,
  fromThrowable,
  ok,
  type StructuredLogger,
} from "@hcm-next/foundation";
import type {
  AiChangeReview,
  AiChangeReviewRequest,
  AiClient,
  AiRiskLevel,
} from "./ai-client.js";

export type OpenAiClientConfig = {
  apiKey: string;
  model?: string;
  /** Organization ID for API billing attribution. */
  organization?: string;
};

const REVIEW_JSON_SCHEMA = {
  type: "object",
  properties: {
    summary: {
      type: "string",
      description: "Plain-English summary of what is changing and why it matters.",
    },
    riskLevel: {
      type: "string",
      enum: ["low", "medium", "high"],
      description: "Overall risk level of this change.",
    },
    missingData: {
      type: "array",
      items: { type: "string" },
      description:
        "Field names or data points that are absent but required for a complete review.",
    },
    policyFlags: {
      type: "array",
      items: { type: "string" },
      description: "Policy rules or constraints that this change may conflict with.",
    },
    downstreamImpact: {
      type: "array",
      items: { type: "string" },
      description:
        "Systems, records, or processes that will be affected by this change.",
    },
    recommendedNextAction: {
      type: "string",
      description: "The single most important next step for the approver or requester.",
    },
  },
  required: [
    "summary",
    "riskLevel",
    "missingData",
    "policyFlags",
    "downstreamImpact",
    "recommendedNextAction",
  ],
  additionalProperties: false,
} as const;

const SYSTEM_PROMPT =
  "You are an AI assistant specialized in reviewing proposed HR data changes for enterprise organizations. " +
  "You analyze only the data provided — you have no access to external systems. " +
  "Be objective, precise, and concise. Flag real risks; do not invent concerns.";

function buildUserPrompt(request: AiChangeReviewRequest): string {
  return (
    `Change type: ${request.changeType}\n` +
    `Workflow instance: ${request.workflowInstanceId}\n\n` +
    `Current state:\n${JSON.stringify(request.currentState, null, 2)}\n\n` +
    `Proposed state:\n${JSON.stringify(request.proposedState, null, 2)}\n\n` +
    `Authorized visible fields: ${request.visibleFields.join(", ")}`
  );
}

function validateRiskLevel(value: unknown): AiRiskLevel {
  if (value === "low" || value === "medium" || value === "high") {
    return value;
  }
  return "medium";
}

/**
 * Creates an AiClient backed by the OpenAI Responses API.
 * Structured output is enforced via json_schema so the response is always
 * parseable without prompt-engineering fragility.
 */
export function createOpenAiClient(config: OpenAiClientConfig): AiClient {
  const openai = new OpenAI({
    apiKey: config.apiKey,
    ...(config.organization !== undefined ? { organization: config.organization } : {}),
  });
  const model = config.model ?? "gpt-4o";

  return {
    async generateChangeReview(
      request: AiChangeReviewRequest,
      logger?: StructuredLogger,
    ) {
      const startMs = Date.now();

      logger?.info("ai change review started", {
        changeType: request.changeType,
        workflowInstanceId: request.workflowInstanceId,
        changeRequestId: request.changeRequestId,
        visibleFieldCount: request.visibleFields.length,
        model,
        provider: "openai",
      });

      const responseResult = await fromPromise(
        () =>
          openai.responses.create({
            model,
            input: [
              { role: "system", content: SYSTEM_PROMPT },
              { role: "user", content: buildUserPrompt(request) },
            ],
            text: {
              format: {
                type: "json_schema",
                name: "ai_change_review",
                strict: true,
                schema: REVIEW_JSON_SCHEMA,
              },
            },
          }),
        (cause) =>
          aiReviewError({ model, provider: "openai", stage: "api_call" }, cause),
      );

      if (!responseResult.ok) {
        logger?.error("ai change review api call failed", {
          changeType: request.changeType,
          workflowInstanceId: request.workflowInstanceId,
          errorCode: responseResult.error.code,
          durationMs: Date.now() - startMs,
        });
        return responseResult;
      }

      const rawText = responseResult.value.output_text;

      const parseResult = fromThrowable(
        () => JSON.parse(rawText) as Record<string, unknown>,
        (cause) =>
          aiReviewError({ model, provider: "openai", stage: "response_parse" }, cause),
      );

      if (!parseResult.ok) {
        logger?.error("ai change review response parse failed", {
          changeType: request.changeType,
          workflowInstanceId: request.workflowInstanceId,
          errorCode: parseResult.error.code,
          durationMs: Date.now() - startMs,
        });
        return parseResult;
      }

      const parsed = parseResult.value;
      const usage = responseResult.value.usage;

      const review: AiChangeReview = {
        summary: String(parsed["summary"] ?? ""),
        riskLevel: validateRiskLevel(parsed["riskLevel"]),
        missingData: Array.isArray(parsed["missingData"])
          ? (parsed["missingData"] as string[])
          : [],
        policyFlags: Array.isArray(parsed["policyFlags"])
          ? (parsed["policyFlags"] as string[])
          : [],
        downstreamImpact: Array.isArray(parsed["downstreamImpact"])
          ? (parsed["downstreamImpact"] as string[])
          : [],
        recommendedNextAction: String(parsed["recommendedNextAction"] ?? ""),
        providerMetadata: {
          provider: "openai",
          model,
          inputTokens: usage?.input_tokens ?? 0,
          outputTokens: usage?.output_tokens ?? 0,
        },
      };

      logger?.info("ai change review completed", {
        changeType: request.changeType,
        workflowInstanceId: request.workflowInstanceId,
        riskLevel: review.riskLevel,
        inputTokens: review.providerMetadata.inputTokens,
        outputTokens: review.providerMetadata.outputTokens,
        durationMs: Date.now() - startMs,
      });

      return ok(review);
    },
  };
}
