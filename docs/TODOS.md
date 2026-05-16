# AI UI Workstream Status

Status: completed for the current implementation slice on 2026-05-16.

This file tracks the four UI/AI workstreams that replaced the older component
inventory. The detailed implementation notes live in
`docs/ai-ui-generation.md`.

## Workstream 1: AI Backend And Workflow Generation

- [x] Added `POST /api/ai/chat` with provider tool-loop orchestration.
- [x] Added `POST /api/ai/generate-ui` for direct page-generation calls.
- [x] Added chat tools for workflow discovery, employee search, generated UI,
      workflow start, workflow instance inspection, actions, transitions,
      timelines, and task lists.
- [x] Added `AiClient.generatePageDefinition` to the provider interface.
- [x] Added canonical widget type constraints for generated pages.
- [x] Added OpenAI and null-client implementations.
- [x] Added workflow config lookup and stable config hashing.
- [x] Added idempotency helpers for AI-generated UI and AI-driven workflow
      operations.
- [x] Added API, provider, registry, and E2E tests for the backend surface.

## Workstream 2: Console Assistant Surface

- [x] Added the floating `AiChatPanel`.
- [x] Added default HR quick-action chips with intent filtering.
- [x] Added Markdown rendering for assistant responses.
- [x] Added `useChat` for `/api/ai/chat`.
- [x] Added `useStartWorkflow` for generated-page submits.
- [x] Added assistant-view state and clear behavior to `ConsoleShell`.
- [x] Added route-change cleanup so assistant-rendered pages do not leak across
      navigation.
- [x] Added panel, helper, hook, cache, and Playwright coverage.

## Workstream 3: Generated Page Runtime

- [x] Added `PageFormProvider` so generated forms can collect values across
      widgets.
- [x] Added generated-field id normalization to prevent duplicate or missing ids
      from collapsing form state.
- [x] Added a subject picker widget for employee-scoped workflows.
- [x] Wired workflow action bars to submit through the existing workflow intent
      API.
- [x] Added submit-result notices at the top of generated pages.
- [x] Extended the widget registry and generated UI contracts for the new
      subject picker.
- [x] Added unit coverage for field id normalization, page form state, widget
      registration, and app shell helpers.

## Workstream 4: Quality Gates

- [x] Added GitHub Actions for Node and Go tests on push, pull request, and
      manual dispatch.
- [x] Added `scripts/check-code-style.mjs`.
- [x] Added `scripts/check-go-style.mjs`.
- [x] Added root npm scripts for code-style and Go checks.
- [x] Expanded `test:all` to include code-style and Go checks.
- [x] Added pre-commit execution for lint-staged, code-style, Go checks, and
      the full test suite.
- [x] Added Go formatting through lint-staged.
- [x] Moved runtime dependency types out of the API layer so workflows and
      platform code do not import upward.
- [x] Replaced the high-signal workflow magic literals caught by the checker
      with typed constants.

## Follow-Up Backlog

- [ ] Decide whether the chat surface should use the existing `ui-cache` helper
      for explicit "reuse generated screen" behavior, or keep every chat turn
      dynamic.
- [ ] Add production field-level redaction before enabling provider-backed AI on
      customer tenants.
- [ ] Add a promotion workflow if generated pages should become canonical
      checked-in page definitions.
- [ ] Add streaming progress if generation latency becomes visible in real
      provider usage.
- [ ] Build the later agentic runtime that consumes workflow JSON without
      rendering a page.
