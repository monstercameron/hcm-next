import { describe, expect, it } from "vitest";
import {
  applyStyleLabShadowPreset,
  createDefaultStyleLabConfig,
  mergeStyleLabCssVariables,
  styleLabConfigToFieldStyleProps,
  styleLabConfigToWidgetStyleProps,
  styleLabConfigToCssVariables,
} from "./style-lab-vars";

const brand = {
  id: "test-brand",
  name: "Test Brand",
  version: 1,
  status: "published" as const,
  density: "default" as const,
  radius: "soft" as const,
  typography: {
    bodyFontFamily: "Body",
    headingFontFamily: "Heading",
    monoFontFamily: "Mono",
  },
  assets: [],
  tokens: {
    "surface.base": "#f4f6fb",
    "surface.raised": "#ffffff",
    "surface.subtle": "#edf1f7",
    "text.primary": "#101827",
    "text.secondary": "#344054",
    "text.muted": "#667085",
    "ui.control.surface": "#fafafa",
    "ui.control.surface.active": "#eef6ff",
    "ui.control.border": "#cccccc",
    "ui.control.border.strong": "#999999",
    "action.primary.background": "#1357d8",
  },
};

describe("style lab variables", () => {
  it("creates brand-seeded default values", () => {
    const config = createDefaultStyleLabConfig(brand.tokens);

    expect(config.controlSurface).toBe("#fafafa");
    expect(config.pageSurface).toBe("#f4f6fb");
    expect(config.raisedSurface).toBe("#ffffff");
    expect(config.mutedSurface).toBe("#edf1f7");
    expect(config.activeSurface).toBe("#eef6ff");
    expect(config.border).toBe("#cccccc");
    expect(config.strongBorder).toBe("#999999");
    expect(config.textPrimary).toBe("#101827");
    expect(config.accent).toBe("#1357d8");
  });

  it("converts style state into runtime control variables", () => {
    const config = createDefaultStyleLabConfig(brand.tokens);
    const variables = styleLabConfigToCssVariables({
      ...config,
      radiusPx: 12,
      controlHeightPx: 44,
      motionSpeedMs: 200,
      hoverLiftPx: -2,
      hoverScale: 1.01,
    });

    expect(variables["--surface-base"]).toBe("#f4f6fb");
    expect(variables["--surface-raised"]).toBe("#ffffff");
    expect(variables["--surface-subtle"]).toBe("#edf1f7");
    expect(variables["--text-primary"]).toBe("#101827");
    expect(variables["--ui-radius-control"]).toBe("12px");
    expect(variables["--ui-control-height"]).toBe("44px");
    expect(variables["--ui-motion-fast"]).toBe("200ms");
    expect(variables["--ui-motion-translate-hover"]).toBe("-2px");
    expect(variables["--ui-motion-scale-hover"]).toBe("1.01");
  });

  it("merges brand variables before live style overrides", () => {
    const config = createDefaultStyleLabConfig(brand.tokens);
    const cssProperties = mergeStyleLabCssVariables({
      brand,
      styleConfig: {
        ...config,
        accent: "#00a676",
      },
    }) as Record<string, string>;

    expect(cssProperties["--action-primary-background"]).toBe("#00a676");
    expect(cssProperties["--font-body"]).toBe("Body");
  });

  it("applies shadow presets without changing other values", () => {
    const config = createDefaultStyleLabConfig(brand.tokens);
    const updatedConfig = applyStyleLabShadowPreset(config, "strong");

    expect(updatedConfig.shadowPreset).toBe("strong");
    expect(updatedConfig.accent).toBe(config.accent);
    expect(updatedConfig.controlShadow).not.toBe(config.controlShadow);
  });

  it("creates prop objects for generated field and widget components", () => {
    const config = createDefaultStyleLabConfig(brand.tokens);
    const fieldStyleProps = styleLabConfigToFieldStyleProps(config);
    const widgetStyleProps = styleLabConfigToWidgetStyleProps(config);

    expect(
      fieldStyleProps.cssVariables?.[
        "--ui-control-accent" as keyof typeof fieldStyleProps.cssVariables
      ],
    ).toBe("#1357d8");
    expect(fieldStyleProps.className).toBe("brand-generated-field-control");
    expect(widgetStyleProps.accentColor).toBe("#1357d8");
    expect(
      widgetStyleProps.style?.[
        "--ui-control-accent" as keyof typeof widgetStyleProps.style
      ],
    ).toBe("#1357d8");
  });
});
