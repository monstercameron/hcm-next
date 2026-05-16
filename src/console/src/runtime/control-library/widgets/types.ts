import type { CSSProperties, ComponentType, ReactNode } from "react";
import type { ResolvedWidget } from "@hcm-next/ui-runtime";

export type WidgetRecord = Readonly<Record<string, unknown>>;

export type WidgetTone = "neutral" | "info" | "success" | "warning" | "danger";

export type WidgetVariant = "default" | "subtle" | "outlined" | "filled";

export type WidgetDensity = "compact" | "default" | "comfortable";

export type WidgetStyleProps = {
  className?: string;
  style?: CSSProperties;
  tone?: WidgetTone;
  variant?: WidgetVariant;
  density?: WidgetDensity;
  accentColor?: string;
  surfaceColor?: string;
  mutedSurfaceColor?: string;
  activeSurfaceColor?: string;
  borderColor?: string;
  borderStrongColor?: string;
  textColor?: string;
  mutedTextColor?: string;
  radius?: string;
  controlHeight?: string;
  shadow?: string;
  hoverLift?: string;
  hoverScale?: string;
  motionFast?: string;
  motionMedium?: string;
};

export type WidgetBrandingStyleProps = {
  styleProps?: WidgetStyleProps | undefined;
  brandingStyleProps?: WidgetStyleProps | undefined;
};

export type WidgetComponentProps<TConfig> = WidgetBrandingStyleProps & {
  config: TConfig;
};

export type WidgetComponent<TConfig> = ComponentType<WidgetComponentProps<TConfig>>;

export type WidgetFactoryEntry = {
  type: string;
  componentName: string;
  render: (widget: ResolvedWidget) => JSX.Element;
};

export type WidgetFactoryRegistry = Readonly<Record<string, WidgetFactoryEntry>>;

export type WidgetLayoutProps = WidgetStyleProps &
  WidgetBrandingStyleProps & {
    children?: ReactNode | undefined;
    title?: string | undefined;
    description?: string | undefined;
  };

export type StatusTone = WidgetTone | "error" | "low" | "medium" | "high";

export type StatusBadgeConfig = {
  status: string;
  label?: string;
};

export type LinkItem = {
  label: string;
  href: string;
  description?: string;
};

export type LabelValueItem = {
  label: string;
  value: unknown;
  detail?: string;
};
