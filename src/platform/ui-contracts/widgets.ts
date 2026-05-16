import type { DataBinding } from "./bindings";
import type { SurfaceMode } from "./brand";
import type {
  CanonicalWidgetTypeId,
  WidgetTypeAliasDefinition,
} from "./generated-types";
import type { RuleSet } from "./rules";

export type WidgetTrustTier = "governed_workflow" | "benign_content" | "external_embed";

export type WidgetCategory =
  | "layout"
  | "form"
  | "hcm_context"
  | "change_review"
  | "approval"
  | "transaction"
  | "audit"
  | "ai_review"
  | "content"
  | "media"
  | "data_display"
  | "external_embed";

export type WidgetSize = "compact" | "half" | "full" | "wide";

export type WidgetActionKind =
  | "save_draft"
  | "submit"
  | "cancel"
  | "approve"
  | "reject"
  | "request_more_information"
  | "delegate"
  | "simulate"
  | "dry_run"
  | "execute"
  | "retry"
  | "repair"
  | "supersede"
  | "export";

export type ActionBinding = {
  action: WidgetActionKind;
  label: string;
  transition?: string;
  command?: string;
  requiresReason?: boolean;
  variant?: "primary" | "secondary" | "danger" | "quiet";
};

export type WidgetDefinition = {
  widgetType: string;
  displayName: string;
  tier: WidgetTrustTier;
  category: WidgetCategory;
  allowedSurfaces: readonly SurfaceMode[];
  defaultSize: WidgetSize;
  supportsResize: boolean;
  supportsDataBinding: boolean;
  supportsPersonalization: boolean;
  canonicalType?: CanonicalWidgetTypeId;
  compatibilityAlias?: WidgetTypeAliasDefinition;
  requiredBindings?: readonly string[];
  allowedActions?: readonly WidgetActionKind[];
};

export type WidgetInstance = {
  id: string;
  type: string;
  title?: string;
  description?: string;
  size?: WidgetSize;
  props?: Readonly<Record<string, unknown>>;
  bindings?: Readonly<Record<string, DataBinding>>;
  visibility?: RuleSet;
  permissions?: RuleSet;
  actions?: readonly ActionBinding[];
};
