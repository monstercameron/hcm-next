import {
  createRepositories,
  createSeededDemoStore,
  type Repositories,
} from "@hcm-next/data-store";
import { createHttpExecutorClient, type ExecutorClient } from "./executor-client.js";

export type AppDependencies = {
  repositories: Repositories;
  executorClient: ExecutorClient;
};

export function createDefaultDependencies(): AppDependencies {
  const executorUrl = process.env["GO_EXECUTOR_URL"] ?? "http://localhost:7001";
  const store = createSeededDemoStore();

  return {
    repositories: createRepositories(store),
    executorClient: createHttpExecutorClient(executorUrl),
  };
}
