import { renderToStaticMarkup } from "react-dom/server";
import type { CSSProperties } from "react";
import { describe, expect, it } from "vitest";
import { createFieldControlConfig } from "../shared";
import {
  FieldControlFactory,
  fieldControlRegistry,
  fieldControlRegistryEntries,
  getFieldControlComponent,
} from "./registry";
import {
  CheckboxControl,
  ComboboxControl,
  EntityPickerControl,
  FileUploadControl,
  NumberInputControl,
  ReadOnlyValueControl,
  RepeaterControl,
  SelectControl,
  SensitiveRevealControl,
  SignatureInputControl,
  SliderControl,
  TableInputControl,
  TextInputControl,
  ToggleControl,
  TreePickerControl,
} from "./index";
import type { FieldControlProps } from "./types";

const renderControl = (
  type: string,
  props: Partial<FieldControlProps> = {},
): { markup: string; configType: string } => {
  const config = createFieldControlConfig({
    id: `${type}_field`,
    label: "Demo field",
    type,
    placeholder: "Choose a value",
  });
  const markup = renderToStaticMarkup(
    <FieldControlFactory
      columns={[
        { label: "Name", value: "name" },
        { label: "Value", value: "value" },
      ]}
      config={config}
      groups={[
        { label: "Group A", value: "group-a" },
        { label: "Group B", value: "group-b" },
      ]}
      items={[
        { label: "Item A", value: "item-a", description: "First item" },
        { label: "Item B", value: "item-b", description: "Second item" },
      ]}
      onChange={() => undefined}
      options={[
        { label: "Option A", value: "a", group: "Group A" },
        { label: "Option B", value: "b", group: "Group B" },
      ]}
      rows={[
        { label: "Row A", value: "row-a" },
        { label: "Row B", value: "row-b" },
      ]}
      value=""
      {...props}
    />,
  );

  return { markup, configType: config.type };
};

describe("field control registry", () => {
  it("exposes canonical field type IDs as first-class registry entries", () => {
    expect(Object.keys(fieldControlRegistry)).toEqual([
      "text",
      "textarea",
      "number",
      "date",
      "time",
      "select",
      "combobox",
      "multi_select",
      "radio_group",
      "checkbox",
      "toggle",
      "slider",
      "repeater",
      "table",
      "matrix",
      "entity_picker",
      "tree_picker",
      "file_upload",
      "signature",
      "sensitive_reveal",
      "readonly",
    ]);
    expect(fieldControlRegistryEntries.map((entry) => entry.type)).not.toContain(
      "email",
    );
    expect(fieldControlRegistryEntries.map((entry) => entry.type)).not.toContain(
      "table_editor",
    );
  });

  it("maps canonical types and representative aliases to canonical controls", () => {
    expect(getFieldControlComponent("text")).toBe(TextInputControl);
    expect(getFieldControlComponent("email")).toBe(TextInputControl);
    expect(getFieldControlComponent("money")).toBe(NumberInputControl);
    expect(getFieldControlComponent("dropdown")).toBe(SelectControl);
    expect(getFieldControlComponent("filterable_dropdown")).toBe(ComboboxControl);
    expect(getFieldControlComponent("policy_acknowledgement")).toBe(CheckboxControl);
    expect(getFieldControlComponent("toggle_group")).toBe(ToggleControl);
    expect(getFieldControlComponent("slider_group")).toBe(SliderControl);
    expect(getFieldControlComponent("repeating_list")).toBe(RepeaterControl);
    expect(getFieldControlComponent("table_editor")).toBe(TableInputControl);
    expect(getFieldControlComponent("employee_picker")).toBe(EntityPickerControl);
    expect(getFieldControlComponent("manager_tree_picker")).toBe(TreePickerControl);
    expect(getFieldControlComponent("file")).toBe(FileUploadControl);
    expect(getFieldControlComponent("e_signature")).toBe(SignatureInputControl);
    expect(getFieldControlComponent("sensitive_field_reveal")).toBe(
      SensitiveRevealControl,
    );
    expect(getFieldControlComponent("ai_review_panel")).toBe(ReadOnlyValueControl);
  });

  it("renders aliases through canonical configs while preserving alias semantics", () => {
    const cases = [
      ["email", "text", 'type="email"'],
      ["money", "number", "field-library-affix"],
      ["date_range", "date", "field-library-date-range"],
      ["filterable_dropdown", "combobox", "field-library-filterable-select"],
      ["dropdown", "select", "field-library-select"],
      ["toggle_group", "toggle", "field-library-toggle-group"],
      ["slider_group", "slider", "field-library-slider-group"],
      ["repeating_list", "repeater", "field-library-repeating-list"],
      ["table_editor", "table", "field-library-table-editor"],
      ["employee_picker", "entity_picker", "field-library-entity-picker"],
      ["manager_tree_picker", "tree_picker", "field-library-tree-picker"],
      ["file", "file_upload", "field-library-file-upload"],
      ["signature_capture", "signature", "field-library-signature-capture"],
      ["sensitive_field_reveal", "sensitive_reveal", "field-library-sensitive-reveal"],
      ["ai_review_panel", "readonly", "field-library-readonly"],
    ] as const;

    for (const [sourceType, expectedType, expectedMarkup] of cases) {
      const { markup, configType } = renderControl(sourceType, {
        value:
          sourceType === "toggle_group" ||
          sourceType === "slider_group" ||
          sourceType === "date_range" ||
          sourceType === "sensitive_field_reveal"
            ? {}
            : [{ name: "Example", value: "example" }],
      });

      expect(configType).toBe(expectedType);
      expect(markup).toContain(expectedMarkup);
      expect(markup).toContain(`field-library-control-${expectedType}`);
    }
  });

  it("merges branding and local style props on canonical controls", () => {
    const config = createFieldControlConfig({
      id: "first_name",
      label: "First name",
      type: "text",
    });
    const markup = renderToStaticMarkup(
      <FieldControlFactory
        brandingStyleProps={{
          className: "tenant-brand",
          controlClassName: "tenant-control",
          cssVariables: {
            "--ui-control-accent": "#0044cc",
            "--ui-control-radius": "6px",
          } as CSSProperties,
          style: {
            backgroundColor: "rgb(240, 245, 255)",
            color: "rgb(10, 20, 30)",
          },
        }}
        className="local-field"
        config={config}
        controlClassName="local-control"
        cssVariables={
          {
            "--ui-control-radius": "4px",
          } as CSSProperties
        }
        onChange={() => undefined}
        style={{
          color: "rgb(30, 40, 50)",
        }}
        value=""
      />,
    );

    expect(markup).toContain("tenant-brand");
    expect(markup).toContain("local-field");
    expect(markup).toContain("tenant-control");
    expect(markup).toContain("local-control");
    expect(markup).toContain("--ui-control-accent:#0044cc");
    expect(markup).toContain("--ui-control-radius:4px");
    expect(markup).toContain("background-color:rgb(240, 245, 255)");
    expect(markup).toContain("color:rgb(30, 40, 50)");
  });
});
