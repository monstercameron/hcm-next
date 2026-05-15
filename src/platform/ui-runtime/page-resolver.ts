import type {
  BoundValue,
  PageDefinition,
  PageRegionDefinition,
  WidgetDefinition,
  WidgetInstance,
} from "@hcm-next/ui-contracts";
import { brandTokensToCssVariables } from "./brand";
import { resolveWidgetBindings } from "./bindings";
import type { UiRuntimeContext } from "./context";
import { createDefaultWidgetRegistry, findWidgetDefinition } from "./registry";
import { evaluateRuleSet } from "./rules";

export type ResolvedWidget = {
  instance: WidgetInstance;
  definition: WidgetDefinition | undefined;
  bindings: Readonly<Record<string, BoundValue>>;
};

export type ResolvedRegion = {
  region: PageRegionDefinition;
  widgets: readonly ResolvedWidget[];
};

export type ResolvedPage = {
  page: PageDefinition;
  visible: boolean;
  cssVariables: Readonly<Record<string, string>>;
  regions: readonly ResolvedRegion[];
};

const isWidgetAllowedForSurface = (
  definition: WidgetDefinition | undefined,
  context: UiRuntimeContext,
): boolean =>
  definition === undefined
    ? false
    : definition.allowedSurfaces.includes(context.surfaceMode);

const resolveWidget = (
  widget: WidgetInstance,
  context: UiRuntimeContext,
  registry: readonly WidgetDefinition[],
): ResolvedWidget | undefined => {
  const definition = findWidgetDefinition(registry, widget.type);

  if (!isWidgetAllowedForSurface(definition, context)) {
    return undefined;
  }

  const bindings = resolveWidgetBindings(widget, context);
  const isVisible =
    evaluateRuleSet(widget.visibility, context, bindings) &&
    evaluateRuleSet(widget.permissions, context, bindings);

  if (!isVisible) {
    return undefined;
  }

  return {
    instance: widget,
    definition,
    bindings,
  };
};

export const resolvePage = (
  page: PageDefinition,
  context: UiRuntimeContext,
  registry: readonly WidgetDefinition[] = createDefaultWidgetRegistry(),
): ResolvedPage => {
  const visible =
    page.surfaceModes.includes(context.surfaceMode) &&
    evaluateRuleSet(page.visibility, context);

  const regions = visible
    ? page.regions.map((region) => ({
        region,
        widgets: region.widgets
          .map((widget) => resolveWidget(widget, context, registry))
          .filter((widget): widget is ResolvedWidget => widget !== undefined),
      }))
    : [];

  return {
    page,
    visible,
    cssVariables: brandTokensToCssVariables(context.brand.tokens),
    regions,
  };
};
