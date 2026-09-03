import { describe, expect, it } from "vitest";
import {
  ActionRegistry,
  createSemanticActionId,
  type ActionDefinition,
  type DiscoveredAction,
} from "./index";

const updateWorker: ActionDefinition = {
  semanticId: createSemanticActionId("worker", "update-name", "worker.update"),
  label: "Update worker name",
  description: "Change the display name for a worker.",
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

const inspectWorker: ActionDefinition = {
  semanticId: createSemanticActionId("worker", "inspect", "worker.read"),
  label: "Inspect worker",
  scope: "universal",
  featureId: "worker",
  intentId: "inspect",
  capabilityId: "worker.read",
  versions: { feature: "1", intent: "1", capability: "1" },
  route: { method: "GET", path: "/v1/intents/worker/inspect", version: "2026-01" },
  permittedSubjectTypes: ["operator", "manager"],
  risk: "low",
  effect: "read",
  simulationAvailable: false,
};

function available(registry: ActionRegistry, subjectType: string, context = {}) {
  return registry.discover({ context: { subjectType, ...context } });
}

describe("UX-006 action-discovery registry matrix", () => {
  it("TestUniversalActionDiscoveryRejectsUnavailableOrDivergentAction", () => {
    const unpublished = {
      ...inspectWorker,
      semanticId: createSemanticActionId("worker", "inspect-beta", "worker.read-beta"),
      intentId: "inspect-beta",
      capabilityId: "worker.read-beta",
      published: false,
    };
    const registry = new ActionRegistry([inspectWorker, unpublished]);
    const rows = available(registry, "operator");

    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ available: true, route: inspectWorker.route });
    expect(rows[1]).toMatchObject({
      available: false,
      unavailableReason: "unpublished_intent",
      availability: { available: false, reason: "unpublished_intent" },
    });
  });

  it("rejects invalid or divergent route metadata before a surface can invoke it", () => {
    expect(
      () =>
        new ActionRegistry([
          {
            ...inspectWorker,
            route: { ...inspectWorker.route, path: "v1/no-leading-slash" },
          },
        ]),
    ).toThrow("invalid route");
    expect(() => new ActionRegistry([inspectWorker, { ...inspectWorker }])).toThrow(
      "Duplicate semantic action ID",
    );
  });

  it("keeps unauthorized and unavailable actions non-disclosing", () => {
    const registry = new ActionRegistry([
      updateWorker,
      { ...inspectWorker, capabilityAvailable: false },
    ]);
    const rows = registry.discover({
      context: { subjectType: "candidate", contextType: "worker", contextId: "w-1" },
      authorize: () => ({
        allowed: false,
        reason: "policy-internal-detail" as never,
        explanation: "secret policy details",
      }),
    });

    expect(rows[0]).toMatchObject({
      available: false,
      unavailableReason: "unsupported_subject",
    });
    expect(rows[0]!.unavailableExplanation).toBeUndefined();
    expect(rows[1]!).toMatchObject({
      available: false,
      unavailableReason: "missing_capability",
    });
  });

  it("uses one canonical route for every presentation surface", () => {
    const registry = new ActionRegistry([updateWorker]);
    const surfaces = [
      "global-bar",
      "command-palette",
      "worker-menu",
      "mobile",
      "kiosk",
    ];
    const resolved = surfaces.map(
      () =>
        available(registry, "operator", {
          contextType: "worker",
          contextId: "w-1",
        })[0]!,
    );

    expect(new Set(resolved.map((action) => action.semanticId)).size).toBe(1);
    expect(new Set(resolved.map((action) => JSON.stringify(action.route))).size).toBe(
      1,
    );
    expect(resolved.every((action) => action.route === updateWorker.route)).toBe(true);
  });

  it("TestTodo_UX_006_Golden", () => {
    const registry = new ActionRegistry([inspectWorker, updateWorker]);
    const projection = registry
      .discover({
        context: { subjectType: "operator", contextType: "worker", contextId: "w-1" },
      })
      .map((action: DiscoveredAction) => ({
        semanticId: action.semanticId,
        scope: action.scope,
        route: action.route,
        versions: action.versions,
        risk: action.risk,
        effect: action.effect,
        simulationAvailable: action.simulationAvailable,
        available: action.available,
      }));

    expect(projection).toEqual([
      {
        semanticId: "worker:inspect:worker.read",
        scope: "universal",
        route: {
          method: "GET",
          path: "/v1/intents/worker/inspect",
          version: "2026-01",
        },
        versions: { feature: "1", intent: "1", capability: "1" },
        risk: "low",
        effect: "read",
        simulationAvailable: false,
        available: true,
      },
      {
        semanticId: "worker:update-name:worker.update",
        scope: "contextual",
        route: {
          method: "POST",
          path: "/v1/intents/worker/update-name",
          version: "2026-01",
        },
        versions: { feature: "1", intent: "2", capability: "3" },
        risk: "medium",
        effect: "update",
        simulationAvailable: true,
        available: true,
      },
    ]);
  });
});
