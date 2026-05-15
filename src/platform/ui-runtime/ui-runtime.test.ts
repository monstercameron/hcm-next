import type { PageDefinition } from "@hcm-next/ui-contracts";
import { describe, expect, it } from "vitest";
import { brandTokensToCssVariables, defaultBrandPack } from "./brand";
import { resolveBinding } from "./bindings";
import type { UiRuntimeContext } from "./context";
import { resolvePage } from "./page-resolver";
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
});
