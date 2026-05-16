import { describe, expect, it } from "vitest";
import { AiChatPanel, defaultQuickActions } from "./AiChatPanel.js";
import {
  renderPromptTemplate,
  resolveAvailableChips,
  type QuickAction,
} from "./chat-panel-helpers.js";

describe("renderPromptTemplate", () => {
  it("substitutes {subjectName} when a value is provided", () => {
    const rendered = renderPromptTemplate("Start a termination for {subjectName}", {
      subjectName: "Jane Rivera",
    });
    expect(rendered).toBe("Start a termination for Jane Rivera");
  });

  it("falls back to 'this employee' when subjectName is undefined", () => {
    const rendered = renderPromptTemplate("Update contact info for {subjectName}", {});
    expect(rendered).toBe("Update contact info for this employee");
  });

  it("falls back when subjectName is an empty string", () => {
    const rendered = renderPromptTemplate("Update contact info for {subjectName}", {
      subjectName: "",
    });
    expect(rendered).toBe("Update contact info for this employee");
  });
});

describe("resolveAvailableChips", () => {
  it("returns the default chip catalog when no override is supplied", () => {
    const chips = resolveAvailableChips(undefined, [
      "employee.termination",
      "employee.contact_update",
      "employee.org_transfer_compensation_change",
    ]);
    expect(chips).toEqual(defaultQuickActions);
  });

  it("filters out chips whose intent is not in availableIntents", () => {
    const chips = resolveAvailableChips(undefined, ["employee.termination"]);
    expect(chips.map((chip) => chip.id)).toEqual(["start_termination"]);
  });

  it("honours a caller-supplied chip override", () => {
    const override: readonly QuickAction[] = [
      {
        id: "custom_chip",
        label: "Custom",
        workflowIntent: "employee.custom",
        promptTemplate: "Do the custom thing for {subjectName}",
      },
    ];
    const chips = resolveAvailableChips(override, ["employee.custom"]);
    expect(chips).toEqual(override);
  });

  it("returns an empty list when no intents are enabled", () => {
    expect(resolveAvailableChips(undefined, [])).toEqual([]);
  });
});

describe("AiChatPanel module surface", () => {
  it("exports the component and the default quick action catalog", () => {
    // Render-free smoke: confirms the module compiles and the public exports
    // are present without booting React (the console workspace has no DOM
    // test environment).
    expect(typeof AiChatPanel).toBe("function");
    expect(Array.isArray(defaultQuickActions)).toBe(true);
    expect(defaultQuickActions.length).toBeGreaterThan(0);
    expect(
      defaultQuickActions.every(
        (chip) =>
          typeof chip.id === "string" &&
          typeof chip.label === "string" &&
          typeof chip.workflowIntent === "string" &&
          typeof chip.promptTemplate === "string",
      ),
    ).toBe(true);
  });
});
