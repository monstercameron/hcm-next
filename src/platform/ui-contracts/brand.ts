export type SurfaceMode =
  | "full_app"
  | "customer_portal"
  | "embedded_manager_widget"
  | "embedded_approval_widget"
  | "employee_self_service"
  | "hrbp_workbench"
  | "compensation_review"
  | "payroll_review"
  | "finance_review"
  | "admin_preview"
  | "audit_export"
  | "mobile_compact"
  | "email_summary";

export type BrandPackStatus = "draft" | "published" | "deprecated";

export type BrandDensity = "compact" | "default" | "comfortable";

export type BrandRadius = "square" | "soft" | "rounded";

export type BrandTokenMap = Readonly<Record<string, string>>;

export type BrandAssetKind =
  | "logo"
  | "compact_mark"
  | "favicon"
  | "image"
  | "audio"
  | "video"
  | "document";

export type BrandAsset = {
  id: string;
  kind: BrandAssetKind;
  src: string;
  altText: string;
  surfaceModes?: readonly SurfaceMode[];
};

export type BrandTypography = {
  headingFontFamily: string;
  bodyFontFamily: string;
  monoFontFamily: string;
};

export type BrandPack = {
  id: string;
  name: string;
  version: number;
  status: BrandPackStatus;
  tokens: BrandTokenMap;
  assets: readonly BrandAsset[];
  typography: BrandTypography;
  density: BrandDensity;
  radius: BrandRadius;
};
