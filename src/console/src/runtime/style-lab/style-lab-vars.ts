import { brandTokensToCssVariables } from "@hcm-next/ui-runtime";
import type { CSSProperties } from "react";
import type { FieldControlBaseStyleProps, WidgetStyleProps } from "../control-library";
import type {
  StyleLabControlStyle,
  StyleLabCssVariableMap,
  StyleLabCssVariableMergeInput,
  StyleLabDensity,
  StyleLabShadowPreset,
} from "./types";

const DENSITY_HEIGHTS: Record<StyleLabDensity, number> = {
  compact: 36,
  default: 42,
  comfortable: 48,
};

const SHADOW_PRESETS: Record<
  StyleLabShadowPreset,
  Pick<StyleLabControlStyle, "controlShadow" | "activeShadow">
> = {
  none: {
    controlShadow: "none",
    activeShadow: "0 0 0 3px color-mix(in srgb, #2456d6 16%, transparent)",
  },
  soft: {
    controlShadow: "0 1px 2px rgb(15 23 42 / 7%)",
    activeShadow: "0 0 0 3px color-mix(in srgb, #2456d6 18%, transparent)",
  },
  raised: {
    controlShadow: "0 10px 24px rgb(15 23 42 / 11%)",
    activeShadow:
      "0 14px 30px rgb(15 23 42 / 14%), 0 0 0 3px color-mix(in srgb, #2456d6 20%, transparent)",
  },
  strong: {
    controlShadow: "0 18px 42px rgb(15 23 42 / 18%)",
    activeShadow:
      "0 22px 50px rgb(15 23 42 / 22%), 0 0 0 3px color-mix(in srgb, #2456d6 24%, transparent)",
  },
};

const cssVariableRecord = (variables: Record<string, string>): StyleLabCssVariableMap =>
  variables;

const tokenValue = (
  tokens: Readonly<Record<string, string>>,
  tokenName: string,
  fallback: string,
): string => tokens[tokenName] ?? fallback;

const cssColorMix = (left: string, leftPercent: number, right: string): string =>
  `color-mix(in srgb, ${left} ${leftPercent}%, ${right})`;

/** Returns the default visual tuning values for a brand-aware style lab session. */
export const createDefaultStyleLabConfig = (
  tokens: Readonly<Record<string, string>> = {},
): StyleLabControlStyle => {
  const shadowPreset = "soft";
  const shadowValues = SHADOW_PRESETS[shadowPreset];
  const raisedSurface = tokenValue(tokens, "surface.raised", "#ffffff");
  const pageSurface = tokenValue(tokens, "surface.base", "#f6f7f9");
  const mutedSurface = tokenValue(tokens, "surface.subtle", "#eef1f5");
  const textPrimary = tokenValue(tokens, "text.primary", "#17202f");
  const textSecondary = tokenValue(tokens, "text.secondary", "#42526a");
  const textMuted = tokenValue(tokens, "text.muted", "#6b778c");
  const border = tokenValue(tokens, "ui.control.border", "#d8dee8");
  const strongBorder = tokenValue(tokens, "ui.control.border.strong", "#aeb8c8");
  const accent = tokenValue(tokens, "action.primary.background", "#2456d6");

  return {
    pageSurface,
    raisedSurface,
    mutedSurface,
    controlSurface: tokenValue(tokens, "ui.control.surface", raisedSurface),
    activeSurface: tokenValue(tokens, "ui.control.surface.active", "#edf2ff"),
    border,
    strongBorder,
    textPrimary,
    textSecondary,
    textMuted,
    accent,
    radiusPx: 8,
    density: "default",
    controlHeightPx: DENSITY_HEIGHTS.default,
    motionSpeedMs: 180,
    hoverLiftPx: -1,
    hoverScale: 1.005,
    shadowPreset,
    controlShadow: tokenValue(tokens, "ui.shadow.control", shadowValues.controlShadow),
    activeShadow: tokenValue(tokens, "ui.shadow.active", shadowValues.activeShadow),
  };
};

/** Applies a preset shadow while preserving the rest of the current control style. */
export const applyStyleLabShadowPreset = (
  currentStyle: StyleLabControlStyle,
  shadowPreset: StyleLabShadowPreset,
): StyleLabControlStyle => ({
  ...currentStyle,
  shadowPreset,
  ...SHADOW_PRESETS[shadowPreset],
});

/** Returns the recommended control height for a density option. */
export const styleLabControlHeightForDensity = (density: StyleLabDensity): number =>
  DENSITY_HEIGHTS[density];

/** Converts the live style lab state into the CSS variables consumed by controls. */
export const styleLabConfigToCssVariables = (
  styleConfig: StyleLabControlStyle,
): StyleLabCssVariableMap =>
  cssVariableRecord({
    "--surface-base": styleConfig.pageSurface,
    "--surface-raised": styleConfig.raisedSurface,
    "--surface-subtle": styleConfig.mutedSurface,
    "--text-primary": styleConfig.textPrimary,
    "--text-secondary": styleConfig.textSecondary,
    "--text-muted": styleConfig.textMuted,
    "--border-default": styleConfig.border,
    "--border-strong": styleConfig.strongBorder,
    "--border-focus": styleConfig.accent,
    "--ui-control-accent": styleConfig.accent,
    "--ui-control-surface": styleConfig.controlSurface,
    "--ui-control-surface-muted": styleConfig.mutedSurface,
    "--ui-control-surface-active": styleConfig.activeSurface,
    "--ui-control-border": styleConfig.border,
    "--ui-control-border-strong": styleConfig.strongBorder,
    "--ui-control-text": styleConfig.textPrimary,
    "--ui-control-text-muted": styleConfig.textMuted,
    "--ui-radius-control": `${styleConfig.radiusPx}px`,
    "--ui-radius-surface": `${Math.max(styleConfig.radiusPx, 6)}px`,
    "--ui-control-height": `${styleConfig.controlHeightPx}px`,
    "--ui-shadow-card": styleConfig.controlShadow,
    "--ui-shadow-control": styleConfig.controlShadow,
    "--ui-shadow-active": styleConfig.activeShadow,
    "--ui-card-border": cssColorMix(styleConfig.border, 82, styleConfig.textPrimary),
    "--ui-hover-surface": cssColorMix(
      styleConfig.activeSurface,
      58,
      styleConfig.raisedSurface,
    ),
    "--ui-motion-fast": `${styleConfig.motionSpeedMs}ms`,
    "--ui-motion-medium": `${Math.round(styleConfig.motionSpeedMs * 1.45)}ms`,
    "--ui-motion-slow": `${Math.round(styleConfig.motionSpeedMs * 2)}ms`,
    "--ui-motion-translate-hover": `${styleConfig.hoverLiftPx}px`,
    "--ui-motion-scale-hover": String(styleConfig.hoverScale),
    "--action-primary-background": styleConfig.accent,
    "--action-secondary-background": styleConfig.activeSurface,
    "--ui-focus-ring": `0 0 0 3px color-mix(in srgb, ${styleConfig.accent} 18%, transparent)`,
  });

/** Returns branding style props to pass into every generated field control. */
export const styleLabConfigToFieldStyleProps = (
  styleConfig: StyleLabControlStyle,
): FieldControlBaseStyleProps => ({
  className: "brand-generated-field-control",
  controlClassName: `brand-generated-field-density-${styleConfig.density}`,
  cssVariables: styleLabConfigToCssVariables(styleConfig),
});

/** Returns branding style props to pass into every generated widget component. */
export const styleLabConfigToWidgetStyleProps = (
  styleConfig: StyleLabControlStyle,
): WidgetStyleProps => ({
  className: "brand-generated-widget",
  density: styleConfig.density,
  accentColor: styleConfig.accent,
  surfaceColor: styleConfig.controlSurface,
  mutedSurfaceColor: styleConfig.mutedSurface,
  activeSurfaceColor: styleConfig.activeSurface,
  borderColor: styleConfig.border,
  borderStrongColor: styleConfig.strongBorder,
  textColor: styleConfig.textPrimary,
  mutedTextColor: styleConfig.textMuted,
  radius: `${styleConfig.radiusPx}px`,
  controlHeight: `${styleConfig.controlHeightPx}px`,
  shadow: styleConfig.controlShadow,
  hoverLift: `${styleConfig.hoverLiftPx}px`,
  hoverScale: String(styleConfig.hoverScale),
  motionFast: `${styleConfig.motionSpeedMs}ms`,
  motionMedium: `${Math.round(styleConfig.motionSpeedMs * 1.45)}ms`,
  style: styleLabConfigToCssVariables(styleConfig),
});

/** Merges brand CSS variables with live style overrides for a preview surface. */
export const mergeStyleLabCssVariables = ({
  brand,
  styleConfig,
  includeBrandTypography = true,
  extraVariables = {},
}: StyleLabCssVariableMergeInput): CSSProperties => {
  const brandVariables = brandTokensToCssVariables(brand.tokens);
  const styleLabVariables = styleLabConfigToCssVariables(styleConfig);
  const typographyVariables = includeBrandTypography
    ? {
        "--font-body": brand.typography.bodyFontFamily,
        "--font-heading": brand.typography.headingFontFamily,
        "--font-mono": brand.typography.monoFontFamily,
      }
    : {};

  return {
    ...brandVariables,
    ...typographyVariables,
    ...styleLabVariables,
    ...extraVariables,
  } as CSSProperties;
};
