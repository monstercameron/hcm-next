import { describe, expect, it } from "vitest";
import { WORKFLOW_INTENTS } from "@human-capital-management-suite/foundation";
import {
  canonicalJsonString,
  cloneWorkflowConfig,
  findFilesystemWorkflowConfigByIntent,
  isWorkflowConfigRecord,
  listFilesystemWorkflowConfigs,
  workflowConfigFromGraphDefinition,
  workflowConfigHash,
} from "./workflow-config-registry.js";
import { validateWorkflowConfig } from "./workflow-config-validation.js";
import type { WorkflowConfig } from "./workflow-config.js";

describe("workflow config registry", () => {
  it("returns cloned filesystem configs so callers cannot mutate source imports", () => {
    const firstRead = listFilesystemWorkflowConfigs();
    const legalNameConfig = firstRead.find((workflowConfig) => {
      return workflowConfig.intent === WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE;
    });

    expect(legalNameConfig).toBeDefined();
    if (legalNameConfig === undefined) {
      return;
    }

    legalNameConfig.interactions["input"] = {
      type: "form",
      title: "Mutated by test",
    };

    const secondReadResult = findFilesystemWorkflowConfigByIntent(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );

    expect(secondReadResult.ok).toBe(true);
    expect(
      secondReadResult.ok && secondReadResult.value.interactions["input"]?.["title"],
    ).not.toBe("Mutated by test");
  });

  it("hashes equivalent object-key orderings to the same deterministic value", () => {
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

    expect(canonicalJsonString(left)).toBe(canonicalJsonString(right));
    expect(workflowConfigHash(left)).toBe(workflowConfigHash(right));
    expect(workflowConfigHash(left)).toMatch(/^sha256:[a-f0-9]{64}$/);
  });

  it("accepts full executable graph definitions and rejects partial seed records", () => {
    const workflowConfigResult = findFilesystemWorkflowConfigByIntent(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );

    expect(workflowConfigResult.ok).toBe(true);
    if (!workflowConfigResult.ok) {
      return;
    }

    const clonedConfig = cloneWorkflowConfig(workflowConfigResult.value);
    const fullConfigResult = workflowConfigFromGraphDefinition(clonedConfig);
    const partialSeedRecord = {
      intent: WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
      initialState: "collecting_input",
    };

    expect(isWorkflowConfigRecord(clonedConfig)).toBe(true);
    expect(fullConfigResult.ok).toBe(true);
    expect(workflowConfigFromGraphDefinition(partialSeedRecord).ok).toBe(false);
  });

  it("validates checked-in workflow configs without errors", () => {
    for (const workflowConfig of listFilesystemWorkflowConfigs()) {
      const validationReport = validateWorkflowConfig(workflowConfig);

      expect(validationReport.errors, workflowConfig.intent).toEqual([]);
      expect(validationReport.valid, workflowConfig.intent).toBe(true);
    }
  });

  it("reports invalid state, interaction, and graph node references", () => {
    const workflowConfigResult = findFilesystemWorkflowConfigByIntent(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );

    expect(workflowConfigResult.ok).toBe(true);
    if (!workflowConfigResult.ok) {
      return;
    }

    const workflowConfig = cloneWorkflowConfig(workflowConfigResult.value);
    const collectingInputActions =
      workflowConfig.states["collecting_input"]?.actions ?? [];
    const firstAction = collectingInputActions[0];
    const firstGraphOutcome = workflowConfig.graph?.nodes[0]?.outcomes?.[0];

    expect(firstAction).toBeDefined();
    if (firstAction === undefined) {
      return;
    }

    collectingInputActions[0] = {
      ...firstAction,
      nextState: "missing_state",
      nextInteraction: "missing_interaction",
    } as unknown as (typeof collectingInputActions)[number];

    if (firstGraphOutcome !== undefined) {
      firstGraphOutcome.nextNodeId = "missing_node";
    }

    const validationReport = validateWorkflowConfig(workflowConfig);
    const errorCodes = validationReport.errors.map((validationError) => {
      return validationError.code;
    });

    expect(validationReport.valid).toBe(false);
    expect(errorCodes).toEqual(
      expect.arrayContaining([
        "workflow_config.action_next_state_missing",
        "workflow_config.action_next_interaction_missing",
        "workflow_config.outcome_next_node_missing",
      ]),
    );
  });
});
