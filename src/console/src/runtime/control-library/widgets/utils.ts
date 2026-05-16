import type { CSSProperties } from "react";
import type {
  WidgetBrandingStyleProps,
  WidgetDensity,
  WidgetRecord,
  WidgetStyleProps,
  WidgetTone,
  WidgetVariant,
} from "./types";

const styleVariableKeys = {
  accentColor: "--ui-control-accent",
  surfaceColor: "--ui-control-surface",
  mutedSurfaceColor: "--ui-control-surface-muted",
  activeSurfaceColor: "--ui-control-surface-active",
  borderColor: "--ui-control-border",
  borderStrongColor: "--ui-control-border-strong",
  textColor: "--ui-control-text",
  mutedTextColor: "--ui-control-text-muted",
  radius: "--ui-radius-control",
  controlHeight: "--ui-control-height",
  shadow: "--ui-shadow-control",
  hoverLift: "--ui-motion-translate-hover",
  hoverScale: "--ui-motion-scale-hover",
  motionFast: "--ui-motion-fast",
  motionMedium: "--ui-motion-medium",
} as const;

export const widgetStyleKeys = [
  "className",
  "style",
  "tone",
  "variant",
  "density",
  "accentColor",
  "surfaceColor",
  "mutedSurfaceColor",
  "activeSurfaceColor",
  "borderColor",
  "borderStrongColor",
  "textColor",
  "mutedTextColor",
  "radius",
  "controlHeight",
  "shadow",
  "hoverLift",
  "hoverScale",
  "motionFast",
  "motionMedium",
] as const;

export const isWidgetRecord = (value: unknown): value is WidgetRecord =>
  typeof value === "object" && value !== null && !Array.isArray(value);

export const stringValue = (value: unknown, fallback = ""): string =>
  typeof value === "string" ? value : fallback;

export const numberValue = (value: unknown, fallback: number): number =>
  typeof value === "number" && Number.isFinite(value) ? value : fallback;

export const booleanValue = (value: unknown): boolean =>
  typeof value === "boolean" ? value : false;

export const objectValue = (value: unknown): WidgetRecord =>
  isWidgetRecord(value) ? value : {};

export const recordsValue = (value: unknown): readonly WidgetRecord[] =>
  Array.isArray(value) ? value.filter(isWidgetRecord) : [];

export const stringArrayValue = (value: unknown): readonly string[] =>
  Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];

export const valueToText = (value: unknown): string => {
  if (typeof value === "string") {
    return value;
  }

  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }

  if (value === undefined || value === null) {
    return "Not set";
  }

  return JSON.stringify(value);
};

export const optionValue = (option: WidgetRecord): string =>
  stringValue(option.value, stringValue(option.id, stringValue(option.label)));

export const optionLabel = (option: WidgetRecord): string =>
  stringValue(option.label, optionValue(option));

export const optionGroup = (option: WidgetRecord): string =>
  stringValue(option.group, "Other");

const readTone = (value: unknown): WidgetTone | undefined => {
  if (
    value === "neutral" ||
    value === "info" ||
    value === "success" ||
    value === "warning" ||
    value === "danger"
  ) {
    return value;
  }

  return undefined;
};

const readVariant = (value: unknown): WidgetVariant | undefined => {
  if (
    value === "default" ||
    value === "subtle" ||
    value === "outlined" ||
    value === "filled"
  ) {
    return value;
  }

  return undefined;
};

const readDensity = (value: unknown): WidgetDensity | undefined => {
  if (value === "compact" || value === "default" || value === "comfortable") {
    return value;
  }

  return undefined;
};

const assignString = (
  styleProps: WidgetStyleProps,
  key: keyof WidgetStyleProps,
  value: unknown,
): void => {
  if (typeof value === "string" && value.length > 0) {
    Object.assign(styleProps, { [key]: value });
  }
};

/** Converts AI/customer style config into widget styling props. */
export const stylePropsFromConfig = (value: unknown): WidgetStyleProps => {
  const styleConfig = objectValue(value);
  const styleProps: WidgetStyleProps = {};
  const tone = readTone(styleConfig.tone);
  const variant = readVariant(styleConfig.variant);
  const density = readDensity(styleConfig.density);

  if (tone !== undefined) {
    styleProps.tone = tone;
  }

  if (variant !== undefined) {
    styleProps.variant = variant;
  }

  if (density !== undefined) {
    styleProps.density = density;
  }

  assignString(styleProps, "className", styleConfig.className);
  assignString(styleProps, "accentColor", styleConfig.accentColor);
  assignString(styleProps, "surfaceColor", styleConfig.surfaceColor);
  assignString(styleProps, "mutedSurfaceColor", styleConfig.mutedSurfaceColor);
  assignString(styleProps, "activeSurfaceColor", styleConfig.activeSurfaceColor);
  assignString(styleProps, "borderColor", styleConfig.borderColor);
  assignString(styleProps, "borderStrongColor", styleConfig.borderStrongColor);
  assignString(styleProps, "textColor", styleConfig.textColor);
  assignString(styleProps, "mutedTextColor", styleConfig.mutedTextColor);
  assignString(styleProps, "radius", styleConfig.radius);
  assignString(styleProps, "controlHeight", styleConfig.controlHeight);
  assignString(styleProps, "shadow", styleConfig.shadow);
  assignString(styleProps, "hoverLift", styleConfig.hoverLift);
  assignString(styleProps, "hoverScale", styleConfig.hoverScale);
  assignString(styleProps, "motionFast", styleConfig.motionFast);
  assignString(styleProps, "motionMedium", styleConfig.motionMedium);

  return styleProps;
};

export const mergeWidgetStyleProps = (
  brandingStyleProps: WidgetStyleProps | undefined,
  localStyleProps: WidgetStyleProps | undefined,
): WidgetStyleProps => ({
  ...brandingStyleProps,
  ...localStyleProps,
  className: [brandingStyleProps?.className, localStyleProps?.className]
    .filter((className): className is string => typeof className === "string")
    .join(" "),
  style: {
    ...brandingStyleProps?.style,
    ...localStyleProps?.style,
  },
});

export const widgetStylePropsFromElementProps = (
  props: WidgetStyleProps,
): WidgetStyleProps => {
  const styleProps: WidgetStyleProps = {};

  for (const styleKey of widgetStyleKeys) {
    const value = props[styleKey];

    if (value !== undefined) {
      Object.assign(styleProps, { [styleKey]: value });
    }
  }

  return styleProps;
};

export const resolveWidgetStyleProps = (
  props: WidgetBrandingStyleProps & WidgetStyleProps,
): WidgetStyleProps => {
  const directStyleProps = widgetStylePropsFromElementProps(props);
  const localStyleProps = mergeWidgetStyleProps(props.styleProps, directStyleProps);

  return mergeWidgetStyleProps(props.brandingStyleProps, localStyleProps);
};

/** Creates CSS variables used by the generic console control styles. */
export const widgetStyleVariables = (
  styleProps: WidgetStyleProps | undefined,
): CSSProperties => {
  const cssVariables: Record<string, string> = {};

  if (styleProps === undefined) {
    return cssVariables as CSSProperties;
  }

  for (const [propKey, cssKey] of Object.entries(styleVariableKeys)) {
    const value = styleProps[propKey as keyof typeof styleVariableKeys];

    if (typeof value === "string" && value.length > 0) {
      cssVariables[cssKey] = value;
    }
  }

  return {
    ...cssVariables,
    ...styleProps.style,
  } as CSSProperties;
};

/** Generates generic class names that styling/branding can target later. */
export const widgetClassName = (
  baseClassName: string,
  styleProps: WidgetStyleProps | undefined,
): string =>
  [
    baseClassName,
    "control-library-widget",
    `widget-tone-${styleProps?.tone ?? "neutral"}`,
    `widget-variant-${styleProps?.variant ?? "default"}`,
    `widget-density-${styleProps?.density ?? "default"}`,
    styleProps?.className ?? "",
  ]
    .filter((part) => part.length > 0)
    .join(" ");

export const sanitizeHtmlSubset = (html: string): string =>
  html
    .replace(/<script[\s\S]*?<\/script>/gi, "")
    .replace(/\son[a-z]+="[^"]*"/gi, "")
    .replace(/\son[a-z]+='[^']*'/gi, "")
    .replace(/javascript:/gi, "");
