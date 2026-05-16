import { describe, expect, it } from "vitest";
import {
  controlClassName,
  controlStyleVariables,
  createFieldControlConfig,
  parseControlPathValue,
  parseControlRecords,
  parseControlRecordsFromSources,
  parseUnknownFieldControlConfig,
} from "./index";

describe("control library shared helpers", () => {
  it("parses generated field configs with style, behavior, validation, and data mapping", () => {
    const parseResult = parseUnknownFieldControlConfig({
      id: "employee_picker",
      label: "Employee",
      type: "permission_entity_picker",
      required: true,
      placeholder: "Search employees",
      help: "Select a permitted employee.",
      aiInstruction: "Use employees visible to the actor.",
      data: {
        source: "employees",
        path: "payload.rows",
        valuePath: "person.id",
        labelPath: "person.name",
      },
      display: {
        layout: "cards",
        showPreview: true,
      },
      style: {
        tone: "info",
        variant: "outlined",
        density: "comfortable",
        accentColor: "#2251ff",
      },
      behavior: {
        searchEnabled: true,
        clearable: true,
      },
      validation: {
        required: true,
        message: "Employee is required.",
      },
    });

    expect(parseResult.ok).toBe(true);

    if (!parseResult.ok) {
      return;
    }

    expect(parseResult.value.data.valuePath).toBe("person.id");
    expect(parseResult.value.display.layout).toBe("cards");
    expect(parseResult.value.style.tone).toBe("info");
    expect(parseResult.value.behavior.searchEnabled).toBe(true);
    expect(parseResult.value.validation.message).toBe("Employee is required.");
  });

  it("normalizes field aliases into canonical generated field types", () => {
    const emailConfig = createFieldControlConfig({
      id: "work_email",
      label: "Work email",
      type: "email",
    });
    const filterableConfig = createFieldControlConfig({
      id: "department",
      label: "Department",
      type: "filterable_dropdown",
    });

    expect(emailConfig.type).toBe("text");
    expect(emailConfig.raw.sourceType).toBe("email");
    expect(emailConfig.raw.inputType).toBe("email");
    expect(filterableConfig.type).toBe("combobox");
    expect(filterableConfig.behavior.searchEnabled).toBe(true);
  });

  it("returns explicit parse errors for invalid generated field configs", () => {
    const parseResult = parseUnknownFieldControlConfig("not-a-config");

    expect(parseResult.ok).toBe(false);

    if (parseResult.ok) {
      return;
    }

    expect(parseResult.error.code).toBe("CONTROL_CONFIG_INVALID_RECORD");
  });

  it("normalizes arbitrary API records through configured nested paths", () => {
    const parseResult = parseControlRecords(
      [
        {
          person: { id: "emp_1", name: "Avery Stone" },
          org: { unit: "Clinical Ops" },
          access: { scope: "manager_chain" },
        },
      ],
      {
        valuePath: "person.id",
        labelPath: "person.name",
        groupPath: "org.unit",
        scopePath: "access.scope",
      },
    );

    expect(parseResult.ok).toBe(true);

    if (!parseResult.ok) {
      return;
    }

    expect(parseResult.value[0]?.value).toBe("emp_1");
    expect(parseResult.value[0]?.label).toBe("Avery Stone");
    expect(parseResult.value[0]?.group).toBe("Clinical Ops");
    expect(parseResult.value[0]?.scope).toBe("manager_chain");
  });

  it("resolves records from named workflow data sources", () => {
    const parseResult = parseControlRecordsFromSources(
      {
        employees: {
          payload: {
            rows: [
              {
                employee: { id: "emp_2", displayName: "Morgan Lee" },
              },
            ],
          },
        },
      },
      {
        source: "employees",
        path: "payload.rows",
        valuePath: "employee.id",
        labelPath: "employee.displayName",
      },
    );

    expect(parseResult.ok).toBe(true);

    if (!parseResult.ok) {
      return;
    }

    expect(parseResult.value[0]?.value).toBe("emp_2");
    expect(parseResult.value[0]?.label).toBe("Morgan Lee");
  });

  it("keeps compatibility helpers stable for the current renderer", () => {
    const config = createFieldControlConfig({
      id: "risk_panel",
      label: "Risk Panel",
      type: "ai_review_panel",
      style: {
        tone: "warning",
        variant: "filled",
        density: "compact",
        accentColor: "#ad5900",
      },
    });
    const className = controlClassName(config, "workflow-field");
    const styleVariables = controlStyleVariables(config.style);

    expect(className).toContain("control-type-readonly");
    expect(config.raw.sourceType).toBe("ai_review_panel");
    expect(className).toContain("control-tone-warning");
    expect(styleVariables["--ui-control-accent" as keyof typeof styleVariables]).toBe(
      "#ad5900",
    );
  });

  it("validates explicit paths when callers need path-level failures", () => {
    const parseResult = parseControlPathValue(
      { employee: { id: "emp_3" } },
      "employee.id",
    );

    expect(parseResult).toEqual({ ok: true, value: "emp_3" });

    const invalidResult = parseControlPathValue({}, "employee..id");

    expect(invalidResult.ok).toBe(false);
  });
});
