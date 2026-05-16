import type {
  BoundValue,
  PageDefinition,
  PageRegionDefinition,
  WidgetDefinition,
  WidgetInstance,
  WidgetTypeAliasDefinition,
} from "@hcm-next/ui-contracts";
import { brandTokensToCssVariables } from "./brand";
import { resolveWidgetBindings } from "./bindings";
import type { UiRuntimeContext } from "./context";
import {
  createDefaultWidgetRegistry,
  findCanonicalWidgetDefinition,
  findExactWidgetDefinition,
  findWidgetDefinition,
  getWidgetTypeCanonicalization,
  normalizeWidgetInstance,
} from "./registry";
import { evaluateRuleSet } from "./rules";

export type ResolvedWidget = {
  instance: WidgetInstance;
  definition: WidgetDefinition | undefined;
  canonicalDefinition: WidgetDefinition | undefined;
  normalizedInstance: WidgetInstance;
  originalType: string;
  canonicalType: string;
  compatibilityAlias?: WidgetTypeAliasDefinition;
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
  const canonicalization = getWidgetTypeCanonicalization(widget.type);
  const definition =
    findExactWidgetDefinition(registry, widget.type) ??
    findWidgetDefinition(registry, widget.type);
  const canonicalDefinition = findCanonicalWidgetDefinition(registry, widget.type);

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
    canonicalDefinition,
    normalizedInstance: normalizeWidgetInstance(widget),
    originalType: canonicalization.originalType,
    canonicalType: canonicalization.canonicalType,
    ...(canonicalization.alias === undefined
      ? {}
      : { compatibilityAlias: canonicalization.alias }),
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
