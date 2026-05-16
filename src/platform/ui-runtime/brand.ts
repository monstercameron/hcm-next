import type { BrandPack, BrandTokenMap } from "@hcm-next/ui-contracts";

export const defaultBrandPack: BrandPack = {
  id: "hcm-next-default",
  name: "HCM Next",
  version: 1,
  status: "published",
  density: "default",
  radius: "soft",
  typography: {
    headingFontFamily:
      'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    bodyFontFamily:
      'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    monoFontFamily:
      '"SFMono-Regular", Consolas, "Liberation Mono", ui-monospace, monospace',
  },
  assets: [
    {
      id: "hcm-next-logo",
      kind: "logo",
      src: "/hcm-next-logo.svg",
      altText: "HCM Next",
    },
  ],
  tokens: {
    "surface.base": "#f6f7f9",
    "surface.subtle": "#eef1f5",
    "surface.raised": "#ffffff",
    "surface.inverse": "#14171f",
    "text.primary": "#17202f",
    "text.secondary": "#42526a",
    "text.muted": "#6b778c",
    "text.inverse": "#ffffff",
    "border.default": "#d8dee8",
    "border.strong": "#aeb8c8",
    "border.focus": "#2456d6",
    "action.primary.background": "#2456d6",
    "action.primary.text": "#ffffff",
    "action.secondary.background": "#edf2ff",
    "action.danger.background": "#b42318",
    "status.success": "#16794c",
    "status.warning": "#a15c07",
    "status.error": "#b42318",
    "status.info": "#2456d6",
    "risk.low": "#16794c",
    "risk.medium": "#a15c07",
    "risk.high": "#b42318",
    "ui.gap.xs": "6px",
    "ui.gap.sm": "10px",
    "ui.gap.md": "14px",
    "ui.gap.lg": "18px",
    "ui.gap.xl": "24px",
    "ui.gap.section": "20px",
    "ui.gap.region": "20px",
    "ui.gap.cluster": "12px",
    "ui.page.padding.default": "28px",
    "ui.panel.padding.default": "22px",
    "ui.card.padding.default": "18px",
    "ui.control.padding.x.default": "12px",
    "ui.control.padding.y.default": "10px",
    "ui.control.height.default": "42px",
    "ui.control.min.width": "160px",
    "ui.icon.size.default": "18px",
    "ui.sidebar.width": "320px",
    "ui.style.rail.width": "360px",
    "ui.content.max.width": "1180px",
    "ui.border.width": "1px",
    "ui.border.width.strong": "2px",
    "ui.divider.width": "1px",
    "ui.shadow.card": "0 18px 45px rgb(15 23 42 / 10%)",
    "ui.shadow.control": "0 1px 2px rgb(15 23 42 / 7%)",
    "ui.shadow.active": "0 0 0 3px color-mix(in srgb, #2456d6 22%, transparent)",
    "ui.focus.ring": "0 0 0 3px color-mix(in srgb, #2456d6 18%, transparent)",
    "ui.card.border": "color-mix(in srgb, #d8dee8 82%, #17202f 18%)",
    "ui.panel.shadow": "0 16px 36px rgb(15 23 42 / 7%), 0 1px 2px rgb(15 23 42 / 5%)",
    "ui.hover.surface": "color-mix(in srgb, #edf2ff 58%, #ffffff)",
    "ui.hover.shadow": "0 8px 20px rgb(15 23 42 / 10%)",
    "ui.hover.opacity": "1",
    "ui.disabled.opacity": "0.6",
    "ui.control.surface": "#ffffff",
    "ui.control.surface.muted": "#f6f7f9",
    "ui.control.surface.active": "#edf2ff",
    "ui.control.border": "#d8dee8",
    "ui.control.border.strong": "#aeb8c8",
    "ui.control.text": "#17202f",
    "ui.control.text.muted": "#6b778c",
    "ui.control.inner.shadow": "inset 0 1px 0 rgb(255 255 255 / 72%)",
    "ui.chart.accent.2": "#335f9e",
    "ui.chart.accent.3": "#9a5b13",
    "ui.scrollbar.size": "10px",
    "ui.motion.fast": "140ms",
    "ui.motion.medium": "220ms",
    "ui.motion.slow": "320ms",
    "ui.motion.ease": "cubic-bezier(0.2, 0, 0, 1)",
    "ui.motion.translate.hover": "-1px",
    "ui.motion.scale.hover": "1.005",
  },
};

export const mergeBrandPacks = (
  base: BrandPack,
  override: Partial<BrandPack>,
): BrandPack => ({
  ...base,
  ...override,
  tokens: {
    ...base.tokens,
    ...override.tokens,
  },
  assets: override.assets ?? base.assets,
  typography: {
    ...base.typography,
    ...override.typography,
  },
});

export const brandTokensToCssVariables = (
  tokens: BrandTokenMap,
): Readonly<Record<string, string>> => {
  const variables: Record<string, string> = {};

  for (const [tokenName, tokenValue] of Object.entries(tokens)) {
    variables[`--${tokenName.replaceAll(".", "-")}`] = String(tokenValue);
  }

  return variables;
};
