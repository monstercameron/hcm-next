import type {
  ControlDensity,
  ControlStyleConfig,
  ControlStyleVariables,
  ControlTone,
  ControlVariant,
  FieldControlConfig,
} from "./types";

export const readTone = (value: unknown): ControlTone => {
  if (
    value === "info" ||
    value === "success" ||
    value === "warning" ||
    value === "danger"
  ) {
    return value;
  }

  return "neutral";
};

export const readVariant = (value: unknown): ControlVariant => {
  if (value === "subtle" || value === "outlined" || value === "filled") {
    return value;
  }

  return "default";
};

export const readDensity = (value: unknown): ControlDensity => {
  if (value === "compact" || value === "comfortable") {
    return value;
  }

  return "default";
};

const cssVariable = (
  variables: Record<string, string>,
  key: string,
  value: string | undefined,
): void => {
  if (value !== undefined && value.length > 0) {
    variables[key] = value;
  }
};

/**
 * Converts per-control style config into inheritable CSS variables.
 */
export const controlStyleVariables = (
  style: ControlStyleConfig,
): ControlStyleVariables => {
  const variables: Record<string, string> = {};

  cssVariable(variables, "--ui-control-accent", style.accentColor);
  cssVariable(variables, "--ui-control-surface", style.surfaceColor);
  cssVariable(variables, "--ui-control-surface-muted", style.mutedSurfaceColor);
  cssVariable(variables, "--ui-control-surface-active", style.activeSurfaceColor);
  cssVariable(variables, "--ui-control-border", style.borderColor);
  cssVariable(variables, "--ui-control-border-strong", style.borderStrongColor);
  cssVariable(variables, "--ui-control-text", style.textColor);
  cssVariable(variables, "--ui-control-text-muted", style.mutedTextColor);
  cssVariable(variables, "--ui-radius-control", style.radius);
  cssVariable(variables, "--ui-control-height", style.controlHeight);
  cssVariable(variables, "--ui-shadow-control", style.shadow);
  cssVariable(variables, "--ui-motion-translate-hover", style.hoverLift);
  cssVariable(variables, "--ui-motion-scale-hover", style.hoverScale);
  cssVariable(variables, "--ui-motion-fast", style.motionFast);
  cssVariable(variables, "--ui-motion-medium", style.motionMedium);

  return variables as ControlStyleVariables;
};

/**
 * Produces stable class names so generated controls can be targeted generically.
 */
export const controlClassName = (
  config: FieldControlConfig,
  baseClassName: string,
): string =>
  [
    baseClassName,
    "control-component",
    `control-type-${config.type}`,
    `control-tone-${config.style.tone}`,
    `control-variant-${config.style.variant}`,
    `control-density-${config.style.density}`,
  ].join(" ");
