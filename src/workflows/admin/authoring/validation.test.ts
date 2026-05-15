import { describe, expect, it } from "vitest";
import { WORKFLOW_INTENTS } from "@hcm-next/foundation";
import {
  cloneWorkflowConfig,
  findFilesystemWorkflowConfigByIntent,
  type WorkflowConfig,
} from "../../shared/workflow-config.js";
import {
  importWorkflowJsonForEditor,
  validateWorkflowJsonForEditor,
} from "./contracts.js";
import {
  listWorkflowConfigBlockReferences,
  validateWorkflowConfigForAuthoring,
} from "./validation.js";

describe("workflow admin authoring validation", () => {
  it("returns path-specific validation reports for checked-in configs", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const validationReport = validateWorkflowConfigForAuthoring(workflowConfig);

    expect(validationReport.valid).toBe(true);
    expect(validationReport.errors).toEqual([]);
    expect(validationReport.configHash).toMatch(/^sha256:/);
    expect(validationReport.warnings[0]?.jsonPath).toMatch(/^\$/);
  });

  it("reports missing block catalog entries", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const mutatedConfig = cloneWorkflowConfig(workflowConfig);
    const graphNode = mutatedConfig.graph?.nodes.find((node) => {
      return node.nodeId === "legal_name_preflight";
    });

    expect(graphNode).toBeDefined();
    if (graphNode === undefined || graphNode.block === undefined) {
      return;
    }

    graphNode.block = {
      ...graphNode.block,
      name: "customer.unknown.block",
    };

    const validationReport = validateWorkflowConfigForAuthoring(mutatedConfig);
    const errorCodes = validationReport.errors.map((error) => {
      return error.code;
    });

    expect(validationReport.valid).toBe(false);
    expect(errorCodes).toContain("workflow_config.block_reference_unknown");
  });

  it("lists block references with JSON paths", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const blockReferences = listWorkflowConfigBlockReferences(workflowConfig);
    const paths = blockReferences.map((reference) => {
      return reference.path;
    });

    expect(paths).toEqual(
      expect.arrayContaining([
        "$.submit.preflightBlock",
        "$.plan.block",
        "$.graph.nodes.1.block",
        "$.graph.nodes.3.block",
      ]),
    );
  });

  it("parses imported JSON and returns editor validation", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_CONTACT_INFO_UPDATE,
    );
    const importResult = importWorkflowJsonForEditor({
      body: JSON.stringify(workflowConfig),
    });

    expect(importResult.ok).toBe(true);
    expect(importResult.ok && importResult.value.validation.valid).toBe(true);
  });

  it("returns the canonical JSON schema with editor validation", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_CONTACT_INFO_UPDATE,
    );
    const validationResult = validateWorkflowJsonForEditor({ workflowConfig });

    expect(validationResult.ok).toBe(true);
    expect(validationResult.ok && validationResult.value.jsonSchema["title"]).toBe(
      "HCM Next Workflow Config",
    );
  });
});

function loadWorkflowConfig(intent: string): WorkflowConfig {
  const workflowConfigResult = findFilesystemWorkflowConfigByIntent(intent);

  expect(workflowConfigResult.ok).toBe(true);
  if (!workflowConfigResult.ok) {
    throw new Error(`Missing workflow config ${intent}.`);
  }

  return workflowConfigResult.value;
}
