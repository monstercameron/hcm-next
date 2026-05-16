import {
  DateInputControl,
  NumberInputControl,
  ReadOnlyValueControl,
  TextareaControl,
  TextInputControl,
  TimeInputControl,
} from "./basic-inputs";
import {
  CheckboxControl,
  ComboboxControl,
  MultiSelectControl,
  RadioGroupControl,
  SelectControl,
} from "./choice-controls";
import { FileUploadControl, SignatureInputControl } from "./file-signature-controls";
import { SensitiveRevealControl } from "./governance-controls";
import { EntityPickerControl, TreePickerControl } from "./hcm-controls";
import {
  MatrixInputControl,
  RepeaterControl,
  TableInputControl,
} from "./structured-controls";
import { SliderControl, ToggleControl } from "./toggle-slider-controls";
import type {
  FieldControlCategory,
  FieldControlComponent,
  FieldControlProps,
  FieldControlRegistryEntry,
} from "./types";
import {
  canonicalFieldControlTypeFor,
  normalizeFieldControlConfig,
} from "../shared/field-types";

const basicControlTypes = [
  "text",
  "textarea",
  "number",
  "date",
  "time",
  "readonly",
] as const;

const choiceControlTypes = [
  "select",
  "combobox",
  "multi_select",
  "radio_group",
  "checkbox",
] as const;

const toggleControlTypes = ["toggle", "slider"] as const;

const structuredControlTypes = ["repeater", "table", "matrix"] as const;

const fileControlTypes = ["file_upload", "signature"] as const;

const hcmControlTypes = ["entity_picker", "tree_picker"] as const;

const governanceControlTypes = ["sensitive_reveal"] as const;

const simulationControlTypes = [] as const;

const labelFromType = (type: string): string =>
  type
    .split("_")
    .map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(" ");

const entry = (
  type: string,
  category: FieldControlCategory,
  component: FieldControlComponent,
): FieldControlRegistryEntry => ({
  type,
  label: labelFromType(type),
  category,
  component,
});

export const fieldControlRegistryEntries: readonly FieldControlRegistryEntry[] = [
  entry("text", "basic", TextInputControl),
  entry("textarea", "basic", TextareaControl),
  entry("number", "basic", NumberInputControl),
  entry("date", "basic", DateInputControl),
  entry("time", "basic", TimeInputControl),
  entry("select", "choice", SelectControl),
  entry("combobox", "choice", ComboboxControl),
  entry("multi_select", "choice", MultiSelectControl),
  entry("radio_group", "choice", RadioGroupControl),
  entry("checkbox", "choice", CheckboxControl),
  entry("toggle", "toggle", ToggleControl),
  entry("slider", "toggle", SliderControl),
  entry("repeater", "structured", RepeaterControl),
  entry("table", "structured", TableInputControl),
  entry("matrix", "structured", MatrixInputControl),
  entry("entity_picker", "hcm", EntityPickerControl),
  entry("tree_picker", "hcm", TreePickerControl),
  entry("file_upload", "files", FileUploadControl),
  entry("signature", "files", SignatureInputControl),
  entry("sensitive_reveal", "governance", SensitiveRevealControl),
  entry("readonly", "basic", ReadOnlyValueControl),
];

export const fieldControlRegistry: Readonly<Record<string, FieldControlComponent>> =
  Object.fromEntries(
    fieldControlRegistryEntries.map((entry) => [entry.type, entry.component]),
  );

/** Returns the registered component for a generated field control type. */
export const getFieldControlComponent = (type: string): FieldControlComponent =>
  fieldControlRegistry[canonicalFieldControlTypeFor(type)] ?? ReadOnlyValueControl;

/** Renders a generated field control using the registry component map. */
export const FieldControlFactory = (props: FieldControlProps) => {
  const config = normalizeFieldControlConfig(props.config);
  const Component = getFieldControlComponent(config.type);

  return <Component {...props} config={config} />;
};

export const fieldControlTypesByCategory: Readonly<
  Record<FieldControlCategory, readonly string[]>
> = {
  basic: basicControlTypes,
  choice: choiceControlTypes,
  toggle: toggleControlTypes,
  structured: structuredControlTypes,
  files: fileControlTypes,
  hcm: hcmControlTypes,
  governance: governanceControlTypes,
  simulation: simulationControlTypes,
};
