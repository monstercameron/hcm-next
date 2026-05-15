import {
  createRepositories,
  createSeededDemoStore,
  type Repositories,
} from "@hcm-next/data-store";
import {
  createHttpCompensationDecisionClient,
  type CompensationDecisionClient,
} from "./compensation-decision-client.js";
import { createHttpExecutorClient, type ExecutorClient } from "./executor-client.js";

export type AppDependencies = {
  repositories: Repositories;
  executorClient: ExecutorClient;
  compensationDecisionClient?: CompensationDecisionClient;
};

export function createDefaultDependencies(): AppDependencies {
  const executorUrl = process.env["GO_EXECUTOR_URL"] ?? "http://localhost:7001";
  const compensationDecisionUrl =
    process.env["THIRD_PARTY_COMPENSATION_DECISION_URL"] ?? "http://localhost:4302";
  const store = createSeededDemoStore();

  return {
    repositories: createRepositories(store),
    executorClient: createHttpExecutorClient(executorUrl),
    compensationDecisionClient: createHttpCompensationDecisionClient(
      compensationDecisionUrl,
    ),
  };
}
