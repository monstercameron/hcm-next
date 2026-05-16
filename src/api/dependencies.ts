import {
  createRepositories,
  createSeededDemoStore,
  type Repositories,
} from "@hcm-next/data-store";
import { createStructuredLogger, type StructuredLogger } from "@hcm-next/foundation";
import {
  createNullAiClient,
  createOpenAiClient,
  type AiClient,
} from "@hcm-next/ai-client";
import { createCompensationDecisionExternalWriteClient } from "./compensation-decision-client.js";
import { createHttpExecutorClient, type ExecutorClient } from "./executor-client.js";
import type { ExternalWriteClient } from "./external-write-client.js";

export type AppDependencies = {
  repositories: Repositories;
  executorClient: ExecutorClient;
  aiClient?: AiClient;
  externalWriteClients?: Record<string, ExternalWriteClient>;
  logger?: StructuredLogger;
};

export function createDefaultDependencies(): AppDependencies {
  const executorUrl = process.env["GO_EXECUTOR_URL"] ?? "http://localhost:7001";
  const compensationDecisionUrl =
    process.env["THIRD_PARTY_COMPENSATION_DECISION_URL"] ?? "http://localhost:4302";
  const openAiApiKey = process.env["OPENAI_API_KEY"];
  const store = createSeededDemoStore();

  return {
    repositories: createRepositories(store),
    executorClient: createHttpExecutorClient(executorUrl),
    aiClient:
      openAiApiKey !== undefined
        ? createOpenAiClient({
            apiKey: openAiApiKey,
            model: process.env["OPENAI_MODEL"] ?? "gpt-4o",
          })
        : createNullAiClient(),
    externalWriteClients: {
      third_party_compensation_decision: createCompensationDecisionExternalWriteClient(
        compensationDecisionUrl,
      ),
    },
    logger: createStructuredLogger({ service: "api" }),
  };
}
