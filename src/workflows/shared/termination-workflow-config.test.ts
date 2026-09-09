import { describe, expect, it } from "vitest";
import { WORKFLOW_INTENTS } from "@human-capital-management-suite/foundation";
import { findFilesystemWorkflowConfigByIntent } from "./workflow-config-registry.js";
import { validateWorkflowConfig } from "./workflow-config-validation.js";

describe("employee termination workflow config", () => {
  it("loads the termination workflow config by intent", () => {
    const result = findFilesystemWorkflowConfigByIntent(
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(result.ok).toBe(true);
    if (!result.ok) return;

    expect(result.value.intent).toBe("employee.termination");
    expect(result.value.subjectType).toBe("worker");
    expect(result.value.selfServiceStart).toBe(false);
  });

  it("passes workflow config validation with no errors", () => {
    const configResult = findFilesystemWorkflowConfigByIntent(
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(configResult.ok).toBe(true);
    if (!configResult.ok) return;

    const validationReport = validateWorkflowConfig(configResult.value);

    expect(validationReport.errors).toHaveLength(0);
  });

  it("includes an ai_review node in the graph", () => {
    const configResult = findFilesystemWorkflowConfigByIntent(
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(configResult.ok).toBe(true);
    if (!configResult.ok) return;

    const aiReviewNodes = configResult.value.graph?.nodes.filter(
      (node) => node.type === "ai_review",
    );

    expect(aiReviewNodes).toBeDefined();
    expect(aiReviewNodes?.length).toBeGreaterThan(0);

    const firstAiReview = aiReviewNodes?.[0];
    expect(firstAiReview?.aiReview?.changeType).toBe("employee.termination");
    expect(firstAiReview?.aiReview?.failurePolicy).toBe("continue");
    expect(firstAiReview?.aiReview?.visibleFields.length).toBeGreaterThan(0);
  });

  it("ai_review node has outcomes leading to the approval node", () => {
    const configResult = findFilesystemWorkflowConfigByIntent(
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(configResult.ok).toBe(true);
    if (!configResult.ok) return;

    const aiReviewNode = configResult.value.graph?.nodes.find(
      (node) => node.type === "ai_review",
    );

    expect(aiReviewNode?.outcomes?.some((o) => o.routeKey === "completed")).toBe(true);
    const completedOutcome = aiReviewNode?.outcomes?.find(
      (o) => o.routeKey === "completed",
    );
    expect(completedOutcome?.nextNodeId).toBe("hr_termination_approval");
  });

  it("has the correct preflight block reference", () => {
    const configResult = findFilesystemWorkflowConfigByIntent(
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(configResult.ok).toBe(true);
    if (!configResult.ok) return;

    expect(configResult.value.submit.preflightBlock.name).toBe(
      "system.employee_data.termination.preflight",
    );
    expect(configResult.value.submit.preflightBlock.version).toBe("1.0.0");
  });

  it("requires hr_admin as the start actor", () => {
    const configResult = findFilesystemWorkflowConfigByIntent(
      WORKFLOW_INTENTS.EMPLOYEE_TERMINATION,
    );

    expect(configResult.ok).toBe(true);
    if (!configResult.ok) return;

    expect(configResult.value.startActors).toContain("hr_admin");
    expect(configResult.value.selfServiceStart).toBe(false);
  });
});
