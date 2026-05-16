import { describe, expect, it } from "vitest";
import type { PageDefinition } from "@hcm-next/ui-contracts";
import {
  assistantViewChipLabel,
  shouldRenderAssistantView,
} from "./console-shell-helpers.js";

const samplePage: PageDefinition = {
  id: "demo-page",
  title: "Demo",
  description: "Sample assistant-generated page used in unit tests.",
  workflowTypes: ["employee.termination"],
  surfaceModes: ["full_app"],
  regions: [
    {
      id: "main",
      layout: "stack",
      width: "content",
      widgets: [],
    },
  ],
};

describe("shouldRenderAssistantView", () => {
  it("returns true when the user is authenticated and a generated page is present", () => {
    expect(
      shouldRenderAssistantView({
        aiGeneratedPage: samplePage,
        isAuthenticated: true,
      }),
    ).toBe(true);
  });

  it("returns false when authenticated but no generated page is present", () => {
    expect(
      shouldRenderAssistantView({
        aiGeneratedPage: undefined,
        isAuthenticated: true,
      }),
    ).toBe(false);
  });

  it("returns false when a generated page exists but the user is not authenticated", () => {
    expect(
      shouldRenderAssistantView({
        aiGeneratedPage: samplePage,
        isAuthenticated: false,
      }),
    ).toBe(false);
  });

  it("returns false when neither input is set", () => {
    expect(
      shouldRenderAssistantView({
        aiGeneratedPage: undefined,
        isAuthenticated: false,
      }),
    ).toBe(false);
  });
});

describe("assistantViewChipLabel", () => {
  it("appends the reused-last-time hint when the source is a cache hit", () => {
    expect(assistantViewChipLabel({ source: "cache" })).toBe(
      "Assistant view · reused last time",
    );
  });

  it("returns the plain label when the source is a fresh generation", () => {
    expect(assistantViewChipLabel({ source: "fresh" })).toBe("Assistant view");
  });

  it("returns the plain label when no source is known yet", () => {
    expect(assistantViewChipLabel({ source: undefined })).toBe("Assistant view");
  });
});
