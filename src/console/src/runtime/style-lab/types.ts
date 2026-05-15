import type { BrandPack } from "@hcm-next/ui-contracts";
import type { CSSProperties, ReactNode } from "react";

export type StyleLabDensity = "compact" | "default" | "comfortable";

export type StyleLabShadowPreset = "none" | "soft" | "raised" | "strong";

export type StyleLabControlStyle = Readonly<{
  pageSurface: string;
  raisedSurface: string;
  mutedSurface: string;
  controlSurface: string;
  activeSurface: string;
  border: string;
  strongBorder: string;
  textPrimary: string;
  textSecondary: string;
  textMuted: string;
  accent: string;
  radiusPx: number;
  density: StyleLabDensity;
  controlHeightPx: number;
  motionSpeedMs: number;
  hoverLiftPx: number;
  hoverScale: number;
  shadowPreset: StyleLabShadowPreset;
  controlShadow: string;
  activeShadow: string;
}>;

export type StyleLabPreset = Readonly<{
  id: string;
  label: string;
  style: StyleLabControlStyle;
}>;

export type StyleLabCssVariableMap = Readonly<Record<string, string>>;

export type StyleLabCssVariableMergeInput = Readonly<{
  brand: BrandPack;
  styleConfig: StyleLabControlStyle;
  includeBrandTypography?: boolean;
  extraVariables?: StyleLabCssVariableMap;
}>;

export type StyleLabRailProps = Readonly<{
  value: StyleLabControlStyle;
  onChange: (nextValue: StyleLabControlStyle) => void;
  brand?: BrandPack;
  presets?: readonly StyleLabPreset[];
  title?: string;
  description?: string;
  className?: string;
  style?: CSSProperties;
}>;

export type StyleLabPreviewScopeProps = Readonly<{
  brand: BrandPack;
  styleConfig: StyleLabControlStyle;
  children: ReactNode;
  className?: string;
  style?: CSSProperties;
}>;

export type StyleLabWorkbenchProps = Readonly<{
  brand: BrandPack;
  value: StyleLabControlStyle;
  onChange: (nextValue: StyleLabControlStyle) => void;
  children: ReactNode;
  presets?: readonly StyleLabPreset[];
  title?: string;
  description?: string;
  className?: string;
  style?: CSSProperties;
}>;
