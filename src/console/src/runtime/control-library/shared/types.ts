import type { CSSProperties } from "react";

export type ControlRecord = Readonly<Record<string, unknown>>;

export type ControlPrimitive = string | number | boolean | null;

export type ControlDataSources = Readonly<Record<string, unknown>>;

export type ControlTone = "neutral" | "info" | "success" | "warning" | "danger";

export type ControlVariant = "default" | "subtle" | "outlined" | "filled";

export type ControlDensity = "compact" | "default" | "comfortable";

export type ControlLayout = "stack" | "grid" | "inline" | "cards" | "table";

export type ControlOption = ControlRecord & {
  value: unknown;
  label: unknown;
  group: unknown;
  description: unknown;
  status: unknown;
  scope: unknown;
  permission: unknown;
  disabled: unknown;
  metadata: unknown;
};

export type ControlDataConfig = {
  source?: string | undefined;
  path?: string | undefined;
  itemsPath?: string | undefined;
  valuePath?: string | undefined;
  labelPath?: string | undefined;
  groupPath?: string | undefined;
  descriptionPath?: string | undefined;
  statusPath?: string | undefined;
  scopePath?: string | undefined;
  permissionPath?: string | undefined;
  disabledPath?: string | undefined;
  metadataPath?: string | undefined;
  emptyLabel?: string | undefined;
};

export type ControlDisplayConfig = {
  layout?: ControlLayout | undefined;
  showPreview?: boolean | undefined;
  showConstraints?: boolean | undefined;
  emptyLabel?: string | undefined;
};

export type ControlStyleConfig = {
  tone: ControlTone;
  variant: ControlVariant;
  density: ControlDensity;
  accentColor?: string | undefined;
  surfaceColor?: string | undefined;
  mutedSurfaceColor?: string | undefined;
  activeSurfaceColor?: string | undefined;
  borderColor?: string | undefined;
  borderStrongColor?: string | undefined;
  textColor?: string | undefined;
  mutedTextColor?: string | undefined;
  radius?: string | undefined;
  controlHeight?: string | undefined;
  shadow?: string | undefined;
  hoverLift?: string | undefined;
  hoverScale?: string | undefined;
  motionFast?: string | undefined;
  motionMedium?: string | undefined;
};

export type ControlBehaviorConfig = {
  searchEnabled?: boolean | undefined;
  multiSelect?: boolean | undefined;
  allowCustomValue?: boolean | undefined;
  clearable?: boolean | undefined;
  readonly?: boolean | undefined;
  disabled?: boolean | undefined;
};

export type ControlValidationConfig = {
  required?: boolean | undefined;
  min?: number | undefined;
  max?: number | undefined;
  minLength?: number | undefined;
  maxLength?: number | undefined;
  pattern?: string | undefined;
  message?: string | undefined;
};

export type FieldControlConfig = {
  id: string;
  label: string;
  type: string;
  required: boolean;
  placeholder: string;
  help: string;
  data: ControlDataConfig;
  display: ControlDisplayConfig;
  style: ControlStyleConfig;
  behavior: ControlBehaviorConfig;
  validation: ControlValidationConfig;
  aiInstruction?: string | undefined;
  raw: ControlRecord;
};

export type ControlStyleVariables = CSSProperties;

export type ControlStyleProps = Partial<ControlStyleConfig> & {
  className?: string | undefined;
  style?: CSSProperties | undefined;
};

export type ControlChangeHandler = (value: unknown) => void;

export type GeneratedControlProps = Readonly<{
  config: FieldControlConfig;
  value: unknown;
  onChange: ControlChangeHandler;
  options?: readonly ControlOption[] | undefined;
  records?: readonly ControlRecord[] | undefined;
  styleProps?: ControlStyleProps | undefined;
  className?: string | undefined;
}>;

export type ControlRecordMappingConfig = {
  valuePath?: string | undefined;
  labelPath?: string | undefined;
  groupPath?: string | undefined;
  descriptionPath?: string | undefined;
  statusPath?: string | undefined;
  scopePath?: string | undefined;
  permissionPath?: string | undefined;
  disabledPath?: string | undefined;
  metadataPath?: string | undefined;
};
