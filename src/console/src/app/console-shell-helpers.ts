import type { PageDefinition } from "@hcm-next/ui-contracts";

/**
 * Pure derivation: should the `ConsoleShell` swap the normal route content
 * for an AI-generated assistant view? True only when an authenticated user
 * has an assistant-generated page in state. Both inputs are required so the
 * caller cannot accidentally render the assistant view on the login screen.
 */
export function shouldRenderAssistantView(args: {
  aiGeneratedPage: PageDefinition | undefined;
  isAuthenticated: boolean;
}): boolean {
  return args.aiGeneratedPage !== undefined && args.isAuthenticated;
}

/**
 * Label text for the dismiss chip that surfaces while an assistant-generated
 * page is mounted. `"cache"` results communicate the reuse explicitly so the
 * user knows the screen was not re-generated.
 */
export function assistantViewChipLabel(args: {
  source: "cache" | "fresh" | undefined;
}): string {
  if (args.source === "cache") {
    return "Assistant view · reused last time";
  }
  return "Assistant view";
}
