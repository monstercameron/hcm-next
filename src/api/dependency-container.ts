import type { AppDependencies } from "./dependencies.js";
import { createDefaultDependencies } from "./dependencies.js";

type HcmNextGlobal = typeof globalThis & {
  __hcmNextDependencies?: AppDependencies;
};

function hcmNextGlobal(): HcmNextGlobal {
  return globalThis as HcmNextGlobal;
}

/**
 * Returns the process-local dependency set used by Next route handlers.
 */
export function getAppDependencies(): AppDependencies {
  const runtimeGlobal = hcmNextGlobal();

  if (runtimeGlobal.__hcmNextDependencies === undefined) {
    runtimeGlobal.__hcmNextDependencies = createDefaultDependencies();
  }

  return runtimeGlobal.__hcmNextDependencies;
}

/**
 * Replaces dependencies for route-level tests without reaching into globals.
 */
export function setAppDependenciesForTest(dependencies: AppDependencies): void {
  hcmNextGlobal().__hcmNextDependencies = dependencies;
}

/**
 * Clears test dependencies so a new seeded demo store is created on demand.
 */
export function resetAppDependenciesForTest(): void {
  delete hcmNextGlobal().__hcmNextDependencies;
}
