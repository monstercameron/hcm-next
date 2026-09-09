import { createRepositories, createSeededDemoStore } from "@human-capital-management-suite/data-store";
import { createStructuredLogger } from "@human-capital-management-suite/foundation";
import { createNullAiClient, createOpenAiClient } from "@human-capital-management-suite/ai-client";
import type { AppDependencies } from "../workflows/shared/runtime-dependencies.js";
import { createCompensationDecisionExternalWriteClient } from "./compensation-decision-client.js";
import { createHttpExecutorClient } from "./executor-client.js";

export type { AppDependencies } from "../workflows/shared/runtime-dependencies.js";

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
    runtimeConfig: {
      allowFilesystemWorkflowImports: process.env["NODE_ENV"] !== "production",
    },
  };
}
