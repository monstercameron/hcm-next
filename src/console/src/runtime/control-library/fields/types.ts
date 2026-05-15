import type { CSSProperties, ReactElement } from "react";
import type { ControlRecord, FieldControlConfig } from "../../control-config";

export type FieldControlChangeHandler = (value: unknown) => void;

export type FieldControlCategory =
  | "basic"
  | "choice"
  | "toggle"
  | "structured"
  | "files"
  | "hcm"
  | "governance"
  | "simulation";

export type FieldControlBaseStyleProps = {
  className?: string;
  contentClassName?: string;
  controlClassName?: string;
  cssVariables?: CSSProperties;
  style?: CSSProperties;
};

export type FieldControlStyleProps = FieldControlBaseStyleProps & {
  brandingStyleProps?: FieldControlBaseStyleProps;
};

export type FieldControlProps = FieldControlStyleProps & {
  config: FieldControlConfig;
  value: unknown;
  onChange: FieldControlChangeHandler;
  options?: readonly ControlRecord[];
  items?: readonly ControlRecord[];
  rows?: readonly ControlRecord[];
  columns?: readonly ControlRecord[];
  groups?: readonly ControlRecord[];
};

export type FieldControlComponent = (props: FieldControlProps) => ReactElement | null;

export type FieldControlRegistryEntry = {
  type: string;
  label: string;
  category: FieldControlCategory;
  component: FieldControlComponent;
};
