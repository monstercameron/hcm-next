import { describe, expect, it } from "vitest";
import {
  ERROR_CODES,
  WORKFLOW_INTENTS,
} from "@human-capital-management-suite/foundation";
import {
  createEmptyStore,
  createRepositories,
  type WorkflowVersionRecord,
} from "@human-capital-management-suite/data-store";
import {
  cloneWorkflowConfig,
  findFilesystemWorkflowConfigByIntent,
} from "../shared/workflow-config-registry.js";
import type { WorkflowConfig } from "../shared/workflow-config.js";
import { computeWorkflowConfigHash } from "./workflow-config-fingerprint.js";
import { getWorkflowConfigByIntent } from "./workflow-config-loader.js";

const tenantId = "tenant_registry_test";

function workflowVersionRecord(input: {
  workflowVersionId: string;
  workflowDefinitionId: string;
  versionNumber: number;
  status: string;
  graphDefinition: Record<string, unknown>;
}): WorkflowVersionRecord {
  return {
    workflowVersionId: input.workflowVersionId,
    tenantId,
    workflowDefinitionId: input.workflowDefinitionId,
    versionNumber: input.versionNumber,
    graphDefinition: input.graphDefinition,
    inputSchema: {},
    outputSchema: {},
    validationRules: [],
    approvalRules: [],
    aiReviewScope: {},
    status: input.status,
  };
}

function loadTerminationFilesystemConfig(): WorkflowConfig {
  const filesystemResult = findFilesystemWorkflowConfigByIntent(
    WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
  );

  expect(filesystemResult.ok).toBe(true);
  if (!filesystemResult.ok) {
    throw new Error("Termination workflow config not found in filesystem registry.");
  }

  return filesystemResult.value;
}

describe("getWorkflowConfigByIntent", () => {
  it("returns the workflow config for employee.termination from the filesystem registry", () => {
    const repositories = createRepositories(createEmptyStore());

    const result = getWorkflowConfigByIntent(
      repositories,
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }

    expect(result.value.intent).toBe(WORKFLOW_INTENTS.EMPLOYEE_TERMINATION);
    expect(result.value.subjectType).toBe("worker");
    expect(result.value.states["collecting_input"]).toBeDefined();
  });

  it("returns notFoundError for an unknown intent", () => {
    const repositories = createRepositories(createEmptyStore());

    const result = getWorkflowConfigByIntent(repositories, "employee.nonexistent");

    expect(result.ok).toBe(false);
    if (result.ok) {
      return;
    }

    expect(result.error.code).toBe(ERROR_CODES.NOT_FOUND);
    expect(result.error.details).toMatchObject({ intent: "employee.nonexistent" });
  });

  it("returns the highest published version when multiple published versions exist", () => {
    const store = createEmptyStore();
    const repositories = createRepositories(store);
    const filesystemConfig = loadTerminationFilesystemConfig();
    const olderConfig = cloneWorkflowConfig(filesystemConfig);
    const newerConfig = cloneWorkflowConfig(filesystemConfig);

    olderConfig.metadata = {
      ...(olderConfig.metadata ?? {}),
      title: "Termination (older)",
    };
    newerConfig.metadata = {
      ...(newerConfig.metadata ?? {}),
      title: "Termination (newer)",
    };

    store.workflowVersions.set(
      "workflow_version_termination_v1",
      workflowVersionRecord({
        workflowVersionId: "workflow_version_termination_v1",
        workflowDefinitionId: "workflow_definition_termination",
        versionNumber: 1,
        status: "published",
        graphDefinition: olderConfig as unknown as Record<string, unknown>,
      }),
    );
    store.workflowVersions.set(
      "workflow_version_termination_v2",
      workflowVersionRecord({
        workflowVersionId: "workflow_version_termination_v2",
        workflowDefinitionId: "workflow_definition_termination",
        versionNumber: 2,
        status: "published",
        graphDefinition: newerConfig as unknown as Record<string, unknown>,
      }),
    );

    const result = getWorkflowConfigByIntent(
      repositories,
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }

    expect(result.value.metadata?.title).toBe("Termination (newer)");
  });

  it("ignores non-published versions when picking the highest version", () => {
    const store = createEmptyStore();
    const repositories = createRepositories(store);
    const filesystemConfig = loadTerminationFilesystemConfig();
    const publishedConfig = cloneWorkflowConfig(filesystemConfig);
    const draftConfig = cloneWorkflowConfig(filesystemConfig);

    publishedConfig.metadata = {
      ...(publishedConfig.metadata ?? {}),
      title: "Termination (published v1)",
    };
    draftConfig.metadata = {
      ...(draftConfig.metadata ?? {}),
      title: "Termination (draft v2)",
    };

    store.workflowVersions.set(
      "workflow_version_termination_published",
      workflowVersionRecord({
        workflowVersionId: "workflow_version_termination_published",
        workflowDefinitionId: "workflow_definition_termination",
        versionNumber: 1,
        status: "published",
        graphDefinition: publishedConfig as unknown as Record<string, unknown>,
      }),
    );
    store.workflowVersions.set(
      "workflow_version_termination_draft",
      workflowVersionRecord({
        workflowVersionId: "workflow_version_termination_draft",
        workflowDefinitionId: "workflow_definition_termination",
        versionNumber: 99,
        status: "draft",
        graphDefinition: draftConfig as unknown as Record<string, unknown>,
      }),
    );

    const result = getWorkflowConfigByIntent(
      repositories,
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }

    expect(result.value.metadata?.title).toBe("Termination (published v1)");
  });

  it("falls back to the filesystem registry when a published version carries only a legacy seed graph", () => {
    const store = createEmptyStore();
    const repositories = createRepositories(store);

    store.workflowVersions.set(
      "workflow_version_termination_legacy",
      workflowVersionRecord({
        workflowVersionId: "workflow_version_termination_legacy",
        workflowDefinitionId: "workflow_definition_termination",
        versionNumber: 1,
        status: "published",
        graphDefinition: {
          intent: WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
          initialState: "collecting_input",
        },
      }),
    );

    const result = getWorkflowConfigByIntent(
      repositories,
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }

    expect(result.value.intent).toBe(WORKFLOW_INTENTS.EMPLOYEE_TERMINATION);
    expect(result.value.submit.preflightBlock).toBeDefined();
  });
});

describe("computeWorkflowConfigHash", () => {
  it("is stable across two reads of the same config", () => {
    const firstRead = loadTerminationFilesystemConfig();
    const secondRead = loadTerminationFilesystemConfig();

    const firstHash = computeWorkflowConfigHash(firstRead);
    const secondHash = computeWorkflowConfigHash(secondRead);

    expect(firstHash).toBe(secondHash);
    expect(firstHash).toMatch(/^sha1:[a-f0-9]{40}$/);
  });

  it("is stable across two computations of the same in-memory config", () => {
    const config = loadTerminationFilesystemConfig();

    expect(computeWorkflowConfigHash(config)).toBe(computeWorkflowConfigHash(config));
  });

  it("changes after a non-whitespace edit to the config", () => {
    const original = loadTerminationFilesystemConfig();
    const mutated = cloneWorkflowConfig(original);

    mutated.metadata = {
      ...(mutated.metadata ?? {}),
      title: "Termination (edited)",
    };

    expect(computeWorkflowConfigHash(mutated)).not.toBe(
      computeWorkflowConfigHash(original),
    );
  });

  it("ignores object key ordering since the hash uses canonical JSON", () => {
    const left = {
      intent: "test.workflow",
      subjectType: "worker",
      selfServiceStart: true,
      interactions: { input: { title: "Input", type: "form" } },
      states: {},
      submit: {},
      approval: {},
      plan: {},
      projection: { allowedPatchPaths: [] },
      timeline: { businessEvents: [], summaries: {} },
    } as unknown as WorkflowConfig;
    const right = {
      timeline: { summaries: {}, businessEvents: [] },
      projection: { allowedPatchPaths: [] },
      plan: {},
      approval: {},
      submit: {},
      states: {},
      interactions: { input: { type: "form", title: "Input" } },
      selfServiceStart: true,
      subjectType: "worker",
      intent: "test.workflow",
    } as unknown as WorkflowConfig;

    expect(computeWorkflowConfigHash(left)).toBe(computeWorkflowConfigHash(right));
  });
});
