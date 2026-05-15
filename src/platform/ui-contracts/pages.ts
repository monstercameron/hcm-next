import type { SurfaceMode } from "./brand";
import type { RuleSet } from "./rules";
import type { WidgetInstance } from "./widgets";

export type PageRegionLayout = "stack" | "grid" | "split" | "sidebar" | "sticky_rail";

export type PageRegionWidth = "narrow" | "content" | "wide" | "full";

export type PageRegionDefinition = {
  id: string;
  title?: string;
  layout: PageRegionLayout;
  width: PageRegionWidth;
  widgets: readonly WidgetInstance[];
};

export type PageDefinition = {
  id: string;
  title: string;
  description: string;
  workflowTypes: readonly string[];
  surfaceModes: readonly SurfaceMode[];
  supportedStates?: readonly string[];
  actorRoles?: readonly string[];
  visibility?: RuleSet;
  regions: readonly PageRegionDefinition[];
};
