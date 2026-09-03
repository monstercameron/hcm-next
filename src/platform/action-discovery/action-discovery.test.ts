import { describe, expect, it } from "vitest";
import {
  ActionRegistry,
  createSemanticActionId,
  discoverActions,
  type ActionDefinition,
} from "./index";

const action: ActionDefinition = {
  semanticId: createSemanticActionId("worker", "update-name", "worker.update"),
  label: "Update worker name",
  scope: "contextual",
  featureId: "worker",
  intentId: "update-name",
  capabilityId: "worker.update",
  versions: { feature: "1", intent: "2", capability: "3" },
  route: { method: "POST", path: "/v1/intents/worker/update-name", version: "2026-01" },
  permittedSubjectTypes: ["operator"],
  requiredInputs: [{ name: "displayName", type: "string", required: true }],
  risk: "medium",
  effect: "update",
  simulationAvailable: true,
};

describe("action discovery", () => {
  it("resolves one descriptor for contextual surfaces and preserves route metadata", () => {
    const registry = new ActionRegistry([action]);
    const result = discoverActions(registry, {
      context: { subjectType: "operator", contextType: "worker", contextId: "w-1" },
    });
    expect(result[0]).toMatchObject({
      semanticId: action.semanticId,
      available: true,
      route: action.route,
    });
  });

  it("returns a safe reason for unauthorized subjects", () => {
    const result = new ActionRegistry([action]).discover({
      context: { subjectType: "candidate" },
    });
    expect(result[0]).toMatchObject({
      available: false,
      unavailableReason: "unsupported_subject",
      availability: { reason: "unsupported_subject" },
    });
  });

  it("rejects duplicate semantic identities", () => {
    expect(() => new ActionRegistry([action, action])).toThrow(
      "Duplicate semantic action ID",
    );
  });

  it("TestTodo_UX_006_Browser", () => {
    const registry = new ActionRegistry([action]);
    const desktop = registry.discover({
      context: { subjectType: "operator", contextType: "worker", contextId: "w-1" },
    });
    const mobile = registry.discover({
      context: { subjectType: "operator", contextType: "worker", contextId: "w-1" },
    });
    expect(mobile[0]).toEqual(desktop[0]);
    expect(mobile[0]?.route).toEqual(action.route);
  });

  it("TestTodo_UX_006_Integration", () => {
    const resolved = new ActionRegistry([action]).discover({
      context: { subjectType: "operator", contextType: "worker", contextId: "w-1" },
    });
    expect(resolved[0]).toMatchObject({
      featureId: "worker",
      intentId: "update-name",
      capabilityId: "worker.update",
      versions: action.versions,
    });
  });

  it("TestTodo_UX_006_Mutation", () => {
    const discovered = new ActionRegistry([action]).discover({
      context: { subjectType: "operator", contextType: "worker", contextId: "w-1" },
    });
    expect(
      () => ((discovered[0] as unknown as { label: string }).label = "forged"),
    ).toThrow();
  });

  it("TestTodo_UX_006_Security", () => {
    const denied = new ActionRegistry([action]).discover({
      context: { subjectType: "operator", contextType: "worker", contextId: "w-1" },
      authorize: () => ({
        allowed: false,
        reason: "unauthorized",
        explanation: "policy=secret; subject=other-tenant",
      }),
    });
    expect(denied[0]).toMatchObject({
      available: false,
      unavailableReason: "unauthorized",
    });
    expect(denied[0]?.unavailableExplanation).toBe(
      "This action is not available for the current authorization.",
    );
  });
});
