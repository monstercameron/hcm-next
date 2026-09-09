import { describe, expect, it } from "vitest";
import { WORKFLOW_INTENTS } from "@human-capital-management-suite/foundation";
import {
  cloneWorkflowConfig,
  findFilesystemWorkflowConfigByIntent,
  type WorkflowConfig,
} from "../../shared/workflow-config.js";
import { buildWorkflowMermaidPreview } from "./mermaid.js";

describe("workflow Mermaid preview", () => {
  it("renders all graph nodes and configured edges as flowchart LR", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const previewResult = buildWorkflowMermaidPreview({ workflowConfig });

    expect(previewResult.ok).toBe(true);
    if (!previewResult.ok) {
      return;
    }

    expect(previewResult.value.mermaid).toContain("flowchart LR");
    expect(previewResult.value.mermaid).toContain(
      'collect_legal_name_input["interaction: Collect legal name input"]',
    );
    expect(previewResult.value.mermaid).toContain(
      'collect_legal_name_input -->|"submitted"| legal_name_preflight',
    );
    expect(previewResult.value.metadata.nodeCount).toBe(
      workflowConfig.graph?.nodes.length,
    );
    expect(previewResult.value.metadata.edgeCount).toBe(
      workflowConfig.graph?.edges?.length,
    );
    expect(previewResult.value.metadata.terminalCount).toBe(4);
  });

  it("escapes labels and produces deterministic output", () => {
    const workflowConfig = cloneWorkflowConfig(
      loadWorkflowConfig(WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE),
    );
    const firstNode = workflowConfig.graph?.nodes[0];

    expect(firstNode).toBeDefined();
    if (firstNode === undefined) {
      return;
    }

    firstNode.title = 'Collect "quoted" input';

    const firstPreviewResult = buildWorkflowMermaidPreview({ workflowConfig });
    const secondPreviewResult = buildWorkflowMermaidPreview({ workflowConfig });

    expect(firstPreviewResult.ok).toBe(true);
    expect(secondPreviewResult.ok).toBe(true);
    if (!firstPreviewResult.ok || !secondPreviewResult.ok) {
      return;
    }

    expect(firstPreviewResult.value.mermaid).toContain('\\"quoted\\"');
    expect(firstPreviewResult.value.mermaid).toBe(secondPreviewResult.value.mermaid);
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
