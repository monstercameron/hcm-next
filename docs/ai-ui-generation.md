# AI-Generated Workflow UI

The HCM Next console includes a floating HR-assistant panel that turns a plain
English request into a workflow-specific screen. The screen is still rendered by
the existing `WorkflowPageRenderer`, so generated pages use the same field,
widget, permission, and workflow transition paths as authored pages.

## 1. Runtime Shape

The implemented console path is chat-first:

```text
AiChatPanel
  -> useChat()
  -> POST /api/ai/chat
  -> provider tool loop
  -> generate_ui_page tool when a page should be shown
  -> AiClient.generatePageDefinition()
  -> sideEffects.renderPage
  -> WorkflowPageRenderer
```

The standalone `POST /api/ai/generate-ui` route still exists. It is useful for
direct API tests, deterministic integrations, and future non-chat callers. The
chat route is the product surface users see.

## 2. Chat Route

Handler: `src/api/ai-chat.ts`.

Request body:

```json
{
  "messages": [
    { "role": "user", "content": "Start a termination for Jane Rivera" }
  ]
}
```

The console sends `x-demo-actor-id` when an actor is available so the backend
can resolve tenant, role, and permission context.

Success response:

```json
{
  "assistantMessage": {
    "role": "assistant",
    "content": "I opened a termination screen for Jane Rivera."
  },
  "sideEffects": {
    "renderPage": { "id": "ai.employee.termination.collecting_input" },
    "workflowConfigHash": "sha1:..."
  },
  "providerMetadata": {
    "provider": "openai",
    "model": "gpt-4o",
    "inputTokens": 2841,
    "outputTokens": 312
  }
}
```

The side effect is optional. The assistant can answer questions without
rendering a page, search employees before acting, start workflows after
confirmation, or inspect tasks and workflow instances.

## 3. Available Tools

The tool catalog lives in `src/api/ai-chat-tools.ts`:

- `list_workflows`
- `search_employees`
- `generate_ui_page`
- `get_employee`
- `get_workflow_config`
- `start_workflow`
- `list_workflow_instances`
- `get_workflow_instance`
- `get_workflow_timeline`
- `get_available_actions`
- `transition_workflow`
- `get_my_tasks`

Tool calls return plain serializable results so the provider can recover from
bad arguments, missing employees, missing workflows, or denied permissions
without crashing the route. The `generate_ui_page` tool is the bridge from chat
to screen rendering.

## 4. Direct Generate-UI Route

Handler: `src/api/ai-generate-ui.ts`.

Request body:

```json
{
  "workflowIntent": "employee.termination",
  "currentState": "collecting_input",
  "currentInteraction": "input",
  "subjectId": "emp_jane_rivera",
  "userPrompt": "Start a termination for Jane Rivera"
}
```

The route validates `workflowIntent` and `userPrompt`, loads the published
workflow config, resolves the optional subject, checks the same start gate as
`startWorkflowIntent`, and calls `AiClient.generatePageDefinition`.

Success response:

```json
{
  "page": {
    "id": "ai.employee.termination.collecting_input",
    "title": "Submit Jane Rivera's termination for HR approval",
    "workflowTypes": ["employee.termination"],
    "surfaceModes": ["assistant"],
    "regions": []
  },
  "workflowConfigHash": "sha1:...",
  "providerMetadata": {
    "provider": "openai",
    "model": "gpt-4o",
    "inputTokens": 2841,
    "outputTokens": 312
  }
}
```

## 5. Grounding And Permissions

The workflow JSON is the source of truth. The AI provider is asked to lay out a
page, not invent business fields. The OpenAI implementation uses a strict JSON
schema whose `widgets[].type` values come from
`src/platform/ai-client/canonical-widget-types.ts`.

Generation is permission-gated before the provider call:

- `selfServiceStart` allows the actor to start a workflow for their linked
  worker record.
- Otherwise the actor must hold one of the workflow's `startActors` role tokens.
- Subject context sent to the provider is limited to display-safe employee
  fields such as display name, job title, department, and manager name.

## 6. Console Surface

Key files:

- `src/console/src/features/ai/AiChatPanel.tsx`
- `src/console/src/features/ai/use-chat.ts`
- `src/console/src/features/ai/use-start-workflow.ts`
- `src/console/src/app/App.tsx`
- `src/console/src/runtime/page-form-context.tsx`
- `src/console/src/runtime/control-library/widgets/subject-picker-widget.tsx`

The panel exposes quick-action chips, a prompt textarea, visible user/assistant
turns, focus handling, and Markdown rendering for assistant responses. When a
chat response carries `sideEffects.renderPage`, `ConsoleShell` pins that page in
the main area until the user clears it or navigates away.

Generated pages can collect field values through `PageFormProvider`. The
workflow action bar uses `useStartWorkflow` to submit through
`POST /api/workflow-intents`, preserving the existing idempotency and workflow
runtime behavior.

## 7. Cache Utility

`src/console/src/features/ai/ui-cache.ts` provides a versioned `localStorage`
cache helper keyed by workflow config hash, state, actor role, and refinement
sequence. It is covered by unit tests and ready for direct generate-UI flows.

The current chat path intentionally does not use that cache. Chat turns are
dynamic tool-loop interactions, so `use-chat.ts` sends each turn to the server.

## 8. Environment Behavior

`src/api/main.ts` loads `src/api/.env` through `dotenv` before building
dependencies.

When `OPENAI_API_KEY` is absent, `createDefaultDependencies` uses
`createNullAiClient`. The null client returns deterministic generated pages from
workflow JSON, which keeps local development and browser tests free of provider
calls.

## 9. Testing

Unit and integration coverage:

- `src/api/ai-chat.test.ts`
- `src/api/ai-chat-tools.test.ts`
- `src/tests/e2e/ai-generate-ui.e2e.spec.ts`
- `src/platform/ai-client/null-client.test.ts`
- `src/platform/ai-client/openai-client.test.ts`
- `src/workflows/registry/workflow-config-loader.test.ts`
- `src/console/src/features/ai/use-chat.test.ts`
- `src/console/src/features/ai/use-start-workflow.test.ts`
- `src/console/src/features/ai/ui-cache.test.ts`
- `src/console/src/features/ai/AiChatPanel.test.tsx`
- `src/console/tests/ai-chat-panel.playwright.mjs`

Useful commands:

```bash
npm run test
npm run test:console:ux
npm run test:all
```

## 10. Out Of Scope

- Promoting generated pages to canonical checked-in page definitions.
- Persistent assistant memory beyond the current browser session and request
  payload.
- Streaming partial pages while generation is in flight.
- Production-grade field-level redaction for every workflow config field.
- A separate agentic runtime that traverses workflow JSON without rendering a
  page.
