import { canonicalWidgetTypeIds, type PageDefinition } from "@hcm-next/ui-contracts";
import { describe, expect, it } from "vitest";
import { brandTokensToCssVariables, defaultBrandPack } from "./brand";
import { resolveBinding } from "./bindings";
import type { UiRuntimeContext } from "./context";
import { resolvePage } from "./page-resolver";
import {
  createDefaultWidgetRegistry,
  findWidgetDefinition,
  findWidgetTypeAlias,
  getCanonicalWidgetDefinitions,
  normalizeWidgetInstance,
  resolveCanonicalWidgetType,
} from "./registry";
import { evaluateRuleSet } from "./rules";

const context: UiRuntimeContext = {
  actor: {
    id: "actor_hrbp",
    displayName: "Riley HRBP",
    roles: ["hrbp"],
    permissions: ["workflow.approve", "employee.view"],
  },
  workflow: {
    id: "wf_1",
    type: "employee.org_transfer_compensation_change",
    title: "Org transfer and compensation change",
    state: "waiting_approval",
    status: "active",
    input: {
      proposed: {
        department: "Cambridge Nursing",
      },
    },
    context: {
      current: {
        department: "Somerville Nursing",
      },
    },
    config: {},
  },
  employee: {
    id: "emp_1",
    displayName: "Jane Rivera",
    jobTitle: "Registered Nurse",
  },
  tenant: {
    id: "tenant_harborcare",
    name: "HarborCare",
    config: {},
  },
  brand: defaultBrandPack,
  surfaceMode: "full_app",
  apiData: {},
  integrationData: {},
  previousWorkflowResponses: {},
  manualValues: {},
  uploadedAssets: {},
  nowIso: "2026-05-15T12:00:00.000Z",
};

describe("ui runtime", () => {
  it("converts semantic brand tokens into css custom properties", () => {
    const cssVariables = brandTokensToCssVariables(defaultBrandPack.tokens);

    expect(cssVariables["--surface-base"]).toBe("#f6f7f9");
    expect(cssVariables["--action-primary-background"]).toBe("#2456d6");
    expect(cssVariables["--ui-gap-section"]).toBe("20px");
    expect(cssVariables["--ui-hover-shadow"]).toBe("0 8px 20px rgb(15 23 42 / 10%)");
    expect(cssVariables["--ui-disabled-opacity"]).toBe("0.6");
  });

  it("resolves nested workflow bindings with provenance", () => {
    const value = resolveBinding(
      {
        source: "workflow_input",
        path: "proposed.department",
      },
      context,
    );

    expect(value.value).toBe("Cambridge Nursing");
    expect(value.source).toBe("workflow_input");
    expect(value.resolved).toBe(true);
  });

  it("evaluates actor permission rules", () => {
    const result = evaluateRuleSet(
      {
        all: [
          {
            id: "can-approve",
            source: "actor",
            operator: "has_permission",
            value: "workflow.approve",
          },
        ],
      },
      context,
    );

    expect(result).toBe(true);
  });

  it("resolves visible widgets and filters hidden widgets", () => {
    const page: PageDefinition = {
      id: "test-page",
      title: "Test Page",
      description: "Test page",
      workflowTypes: ["*"],
      surfaceModes: ["full_app"],
      regions: [
        {
          id: "main",
          layout: "stack",
          width: "wide",
          widgets: [
            {
              id: "visible",
              type: "content.text",
              props: {
                body: "Visible",
              },
            },
            {
              id: "hidden",
              type: "content.text",
              visibility: {
                all: [
                  {
                    id: "hidden-rule",
                    source: "actor",
                    operator: "matches_role",
                    value: "finance",
                  },
                ],
              },
            },
          ],
        },
      ],
    };

    const resolved = resolvePage(page, context);
    const firstRegion = resolved.regions[0];

    expect(firstRegion?.widgets.map((widget) => widget.instance.id)).toEqual([
      "visible",
    ]);
  });

  it("exposes canonical first-class widget definitions", () => {
    const registry = createDefaultWidgetRegistry();
    const canonicalDefinitions = getCanonicalWidgetDefinitions(registry);
    const canonicalRegistryIds = canonicalDefinitions.map(
      (definition) => definition.widgetType,
    );

    expect([...canonicalRegistryIds].sort()).toEqual(
      [...canonicalWidgetTypeIds].sort(),
    );
    expect(
      canonicalDefinitions.every(
        (definition) => definition.canonicalType === definition.widgetType,
      ),
    ).toBe(true);
  });

  it("documents widget aliases and falls back to canonical definitions", () => {
    expect(resolveCanonicalWidgetType("queue.requestList")).toBe("data.queueList");
    expect(resolveCanonicalWidgetType("content.metricTile")).toBe("data.metricTile");
    expect(resolveCanonicalWidgetType("viz.lineChart")).toBe("viz.chart");

    const decisionAlias = findWidgetTypeAlias("approval.decisionPanel");

    expect(decisionAlias?.canonicalType).toBe("workflow.actionBar");
    expect(decisionAlias?.composition).toEqual([
      "workflow.actionBar",
      "workflow.reasonCapture",
    ]);
    expect(
      findWidgetDefinition(createDefaultWidgetRegistry(), "viz.futureChart")
        ?.widgetType,
    ).toBe("viz.chart");
  });

  it("normalizes widget aliases and generated fields without losing original type", () => {
    const page: PageDefinition = {
      id: "alias-page",
      title: "Alias Page",
      description: "Alias page",
      workflowTypes: ["*"],
      surfaceModes: ["full_app"],
      regions: [
        {
          id: "main",
          layout: "stack",
          width: "wide",
          widgets: [
            {
              id: "queue",
              type: "queue.requestList",
              props: {
                requests: [],
              },
            },
            {
              id: "fields",
              type: "form.dynamicFieldGroup",
              props: {
                fields: [
                  {
                    id: "email",
                    label: "Email",
                    type: "email",
                  },
                  {
                    id: "amount",
                    label: "Amount",
                    type: "money",
                  },
                  {
                    id: "manager",
                    label: "Manager",
                    type: "manager_picker",
                  },
                  {
                    id: "signature",
                    label: "Signature",
                    type: "signature_capture",
                  },
                ],
              },
            },
          ],
        },
      ],
    };

    const resolved = resolvePage(page, context);
    const widgets = resolved.regions[0]?.widgets ?? [];
    const queueWidget = widgets.find((widget) => widget.instance.id === "queue");
    const fieldsWidget = widgets.find((widget) => widget.instance.id === "fields");
    const fields = fieldsWidget?.normalizedInstance.props?.fields;

    expect(queueWidget?.instance.type).toBe("queue.requestList");
    expect(queueWidget?.originalType).toBe("queue.requestList");
    expect(queueWidget?.canonicalType).toBe("data.queueList");
    expect(queueWidget?.normalizedInstance.type).toBe("data.queueList");
    expect(queueWidget?.definition?.widgetType).toBe("queue.requestList");
    expect(queueWidget?.canonicalDefinition?.widgetType).toBe("data.queueList");
    expect(queueWidget?.compatibilityAlias?.canonicalType).toBe("data.queueList");

    expect(Array.isArray(fields)).toBe(true);
    expect(
      (fields as readonly Record<string, unknown>[]).map((field) => field.type),
    ).toEqual(["text", "number", "entity_picker", "signature"]);
    expect((fields as readonly Record<string, unknown>[])[0]?.originalType).toBe(
      "email",
    );
    expect((fields as readonly Record<string, unknown>[])[2]?.entityType).toBe(
      "employee",
    );
  });

  it("normalizes standalone widget instances", () => {
    const widget = normalizeWidgetInstance({
      id: "pdf",
      type: "media.pdf",
      title: "Packet",
      props: {
        src: "/packet.pdf",
      },
    });

    expect(widget.type).toBe("media.viewer");
    expect(widget.props?.mediaType).toBe("pdf");
    expect(widget.props?.src).toBe("/packet.pdf");
  });
});
