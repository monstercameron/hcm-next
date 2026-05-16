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

const tokenNumberValue = (
  tokens: Readonly<Record<string, string>>,
  tokenName: string,
  fallback: number,
): number => {
  const value = tokens[tokenName];

  if (value === undefined) {
    return fallback;
  }

  const parsedValue = Number.parseFloat(value);
  return Number.isFinite(parsedValue) ? parsedValue : fallback;
};

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
    gapPx: tokenNumberValue(tokens, "ui.gap.md", 14),
    sectionGapPx: tokenNumberValue(tokens, "ui.gap.section", 20),
    panelPaddingPx: tokenNumberValue(tokens, "ui.panel.padding.default", 22),
    cardPaddingPx: tokenNumberValue(tokens, "ui.card.padding.default", 18),
    controlPaddingXPx: tokenNumberValue(tokens, "ui.control.padding.x.default", 12),
    controlPaddingYPx: tokenNumberValue(tokens, "ui.control.padding.y.default", 10),
    radiusPx: 8,
    density: "default",
    controlHeightPx: tokenNumberValue(
      tokens,
      "ui.control.height.default",
      DENSITY_HEIGHTS.default,
    ),
    borderWidthPx: tokenNumberValue(tokens, "ui.border.width", 1),
    motionSpeedMs: 180,
    hoverLiftPx: -1,
    hoverScale: 1.005,
    hoverOpacity: tokenNumberValue(tokens, "ui.hover.opacity", 1),
    disabledOpacity: tokenNumberValue(tokens, "ui.disabled.opacity", 0.6),
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
    "--ui-gap-md": `${styleConfig.gapPx}px`,
    "--ui-density-gap": `${styleConfig.gapPx}px`,
    "--ui-gap-section": `${styleConfig.sectionGapPx}px`,
    "--ui-section-gap": `${styleConfig.sectionGapPx}px`,
    "--ui-region-gap": `${styleConfig.sectionGapPx}px`,
    "--ui-panel-padding": `${styleConfig.panelPaddingPx}px`,
    "--ui-page-padding": `${Math.round(styleConfig.panelPaddingPx * 1.25)}px`,
    "--ui-card-padding": `${styleConfig.cardPaddingPx}px`,
    "--ui-control-padding-x": `${styleConfig.controlPaddingXPx}px`,
    "--ui-control-padding-y": `${styleConfig.controlPaddingYPx}px`,
    "--ui-radius-control": `${styleConfig.radiusPx}px`,
    "--ui-radius-surface": `${Math.max(styleConfig.radiusPx, 6)}px`,
    "--ui-control-height": `${styleConfig.controlHeightPx}px`,
    "--ui-border-width": `${styleConfig.borderWidthPx}px`,
    "--ui-divider-width": `${styleConfig.borderWidthPx}px`,
    "--ui-shadow-card": styleConfig.controlShadow,
    "--ui-shadow-control": styleConfig.controlShadow,
    "--ui-shadow-active": styleConfig.activeShadow,
    "--ui-active-shadow": styleConfig.activeShadow,
    "--ui-focus-border": styleConfig.accent,
    "--ui-card-border": cssColorMix(styleConfig.border, 82, styleConfig.textPrimary),
    "--ui-hover-surface": cssColorMix(
      styleConfig.activeSurface,
      58,
      styleConfig.raisedSurface,
    ),
    "--ui-hover-border": styleConfig.strongBorder,
    "--ui-hover-shadow": styleConfig.controlShadow,
    "--ui-hover-opacity": String(styleConfig.hoverOpacity),
    "--ui-active-surface": styleConfig.activeSurface,
    "--ui-active-border": styleConfig.accent,
    "--ui-disabled-opacity": String(styleConfig.disabledOpacity),
    "--ui-drag-surface": cssColorMix(
      styleConfig.activeSurface,
      42,
      styleConfig.raisedSurface,
    ),
    "--ui-drop-surface": cssColorMix(styleConfig.accent, 10, styleConfig.raisedSurface),
    "--ui-subtle-tint": cssColorMix(
      styleConfig.activeSurface,
      28,
      styleConfig.raisedSurface,
    ),
    "--ui-selected-tint": cssColorMix(
      styleConfig.activeSurface,
      70,
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
