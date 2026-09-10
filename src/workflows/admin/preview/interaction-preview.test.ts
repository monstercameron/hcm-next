import { describe, expect, it } from "vitest";
import {
  PERMISSION_KEYS,
  WORKFLOW_INTENTS,
} from "@human-capital-management-suite/foundation";
import {
  findFilesystemWorkflowConfigByIntent,
  type WorkflowConfig,
} from "../../shared/workflow-config.js";
import { previewWorkflowInteraction } from "./interaction-preview.js";

describe("workflow interaction preview", () => {
  it("previews legal name input fields and submit contract", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const previewResult = previewWorkflowInteraction({
      workflowConfig,
      state: "collecting_input",
      employeeFixture: {
        person: {
          legalName: {
            first: "Jane",
            last: "Smith",
          },
        },
      },
    });

    expect(previewResult.ok).toBe(true);
    if (!previewResult.ok) {
      return;
    }

    expect(previewResult.value.interactionKey).toBe("input");
    expect(previewResult.value.fields).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "newLegalName", required: true }),
        expect.objectContaining({ path: "newLegalName.first", required: true }),
        expect.objectContaining({ path: "effectiveAt", required: true }),
      ]),
    );
    expect(previewResult.value.submitAction?.transition).toBe("submit_input");
    expect(previewResult.value.summarySections[0]?.valuePreview).toEqual({
      first: "Jane",
      last: "Smith",
    });
  });

  it("redacts employee context when actor lacks required permission", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_ORG_TRANSFER_COMPENSATION_CHANGE,
    );
    const previewResult = previewWorkflowInteraction({
      workflowConfig,
      interactionKey: "input",
      actorFixture: {
        actorId: "actor_manager",
        permissions: [PERMISSION_KEYS.EMPLOYEE_VIEW_ORGANIZATION],
      },
      employeeFixture: {
        compensation: {
          amount: 110000,
          currency: "USD",
        },
        organization: {
          costCenter: "CC-100",
        },
      },
    });

    expect(previewResult.ok).toBe(true);
    if (!previewResult.ok) {
      return;
    }

    const compensationSummary = previewResult.value.summarySections.find((section) => {
      return section.outputKey === "currentCompensation";
    });

    expect(compensationSummary?.visible).toBe(false);
    expect(compensationSummary?.redacted).toBe(true);
    expect(compensationSummary?.deniedReasons[0]).toContain(
      PERMISSION_KEYS.EMPLOYEE_VIEW_COMPENSATION,
    );
  });

  it("previews compensation repair fields", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const previewResult = previewWorkflowInteraction({
      workflowConfig,
      state: "waiting_repair",
    });

    expect(previewResult.ok).toBe(true);
    if (!previewResult.ok) {
      return;
    }

    expect(previewResult.value.type).toBe("repair");
    expect(previewResult.value.fields).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "repairAction", required: true }),
      ]),
    );
    expect(previewResult.value.warnings).not.toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          code: "workflow_interaction.repair_input_missing",
        }),
      ]),
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
