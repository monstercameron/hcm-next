import { describe, expect, it } from "vitest";
import { WORKFLOW_INTENTS } from "@hcm-next/foundation";
import {
  findFilesystemWorkflowConfigByIntent,
  type WorkflowConfig,
} from "../../shared/workflow-config.js";
import { previewWorkflowInputMappings } from "./input-mapping-preview.js";

describe("workflow input mapping preview", () => {
  it("resolves workflow input and employee projection mappings for a configured node", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const previewResult = previewWorkflowInputMappings({
      workflowConfig,
      nodeId: "legal_name_preflight",
      sources: {
        workflowInput: {
          newLegalName: {
            first: "Jane",
            last: "Rivera",
          },
          effectiveAt: "2026-06-01",
          businessReason: "legal_name_change",
        },
        employeeProjection: {
          person: {
            legalName: {
              first: "Jane",
              last: "Smith",
            },
          },
        },
      },
    });

    expect(previewResult.ok).toBe(true);
    if (!previewResult.ok) {
      return;
    }

    const proposedNameMapping = previewResult.value.entries.find((entry) => {
      return entry.targetPath === "proposedLegalName";
    });

    expect(proposedNameMapping?.resolved).toBe(true);
    expect(proposedNameMapping?.sourceType).toBe("workflow_input");
    expect(proposedNameMapping?.valuePreview).toEqual({
      first: "Jane",
      last: "Rivera",
    });
    expect(previewResult.value.unresolved).toEqual([]);
  });

  it("redacts sensitive compensation fields", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_COMPENSATION_CHANGE,
    );
    const previewResult = previewWorkflowInputMappings({
      workflowConfig,
      nodeId: "compensation_preflight",
      sources: {
        workflowInput: {
          proposedCompensation: {
            amount: 125000,
            currency: "USD",
          },
          effectiveAt: "2026-06-01",
          businessReason: "merit_increase",
        },
        employeeProjection: {
          compensation: {
            amount: 115000,
            currency: "USD",
          },
          job: { level: "L4" },
          organization: { costCenter: "CC-100" },
        },
      },
    });

    expect(previewResult.ok).toBe(true);
    if (!previewResult.ok) {
      return;
    }

    const compensationMapping = previewResult.value.entries.find((entry) => {
      return entry.targetPath === "proposedCompensation";
    });

    expect(compensationMapping?.redacted).toBe(true);
    expect(compensationMapping?.valuePreview).toBe("[redacted]");
  });

  it("uses fallbacks and defaults for preview-only mapping objects", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const previewResult = previewWorkflowInputMappings({
      workflowConfig,
      mappingObject: {
        workerId: {
          $source: "workflow",
          path: "missing",
          fallbackPaths: [{ $source: "employee", path: "worker.workerId" }],
        },
        businessReason: {
          $source: "input",
          path: "missingReason",
          defaultValue: "correction",
        },
      },
      sources: {
        employeeProjection: {
          worker: {
            workerId: "worker_123",
          },
        },
      },
    });

    expect(previewResult.ok).toBe(true);
    if (!previewResult.ok) {
      return;
    }

    expect(previewResult.value.entries).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          targetPath: "workerId",
          valuePreview: "worker_123",
          fallbackUsed: true,
        }),
        expect.objectContaining({
          targetPath: "businessReason",
          valuePreview: "correction",
          defaultUsed: true,
        }),
      ]),
    );
  });

  it("reports missing required mappings", () => {
    const workflowConfig = loadWorkflowConfig(
      WORKFLOW_INTENTS.EMPLOYEE_LEGAL_NAME_CHANGE,
    );
    const previewResult = previewWorkflowInputMappings({
      workflowConfig,
      mappingObject: {
        workerId: {
          $source: "workflow",
          path: "subjectId",
        },
      },
      sources: {},
    });

    expect(previewResult.ok).toBe(true);
    if (!previewResult.ok) {
      return;
    }

    expect(previewResult.value.unresolved[0]?.errors[0]?.code).toBe(
      "workflow_mapping.required_value_unresolved",
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
