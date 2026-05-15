import type { BrandPack, SurfaceMode } from "@hcm-next/ui-contracts";

export type UiRuntimeActor = {
  id: string;
  displayName: string;
  roles: readonly string[];
  permissions: readonly string[];
};

export type UiRuntimeWorkflow = {
  id: string;
  type: string;
  title: string;
  state: string;
  status: string;
  input: Readonly<Record<string, unknown>>;
  context: Readonly<Record<string, unknown>>;
  config: Readonly<Record<string, unknown>>;
};

export type UiRuntimeEmployee = Readonly<Record<string, unknown>>;

export type UiRuntimeTenant = {
  id: string;
  name: string;
  config: Readonly<Record<string, unknown>>;
};

export type UiRuntimeContext = {
  actor: UiRuntimeActor;
  workflow: UiRuntimeWorkflow;
  employee: UiRuntimeEmployee;
  tenant: UiRuntimeTenant;
  brand: BrandPack;
  surfaceMode: SurfaceMode;
  apiData: Readonly<Record<string, unknown>>;
  integrationData: Readonly<Record<string, unknown>>;
  previousWorkflowResponses: Readonly<Record<string, unknown>>;
  manualValues: Readonly<Record<string, unknown>>;
  uploadedAssets: Readonly<Record<string, unknown>>;
  nowIso: string;
};
