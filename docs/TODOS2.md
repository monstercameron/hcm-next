# TODO: Frontend UI Foundation

## Target Capability

Build the first frontend foundation for HCM Next:

```text
workflow API/runtime data
  -> UI contracts
  -> page definitions
  -> brand tokens
  -> widget registry
  -> workflow page renderer
  -> first console surfaces
```

This effort is not the full visual builder yet. It creates the codebase structure and runtime seams required for dynamic, branded, permission-aware workflow pages.

## Workstreams

| Workstream                 | Scope                                                                                                            | Primary Files                                                                                |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| 1. Workspace And Contracts | Add frontend workspaces, shared UI contract package, page/widget/brand/binding types, and first page definitions | `package.json`, `tsconfig*.json`, `src/platform/ui-contracts/*`, `src/platform/ui-runtime/*` |
| 2. Runtime And Widgets     | Add brand resolution, binding resolution, rule evaluation, widget registry, page resolver, and unit tests        | `src/platform/ui-runtime/*`                                                                  |
| 3. Console App             | Add the Vite React console shell, workflow page renderer, first operational pages, brand styling, and demo data  | `src/console/*`                                                                              |
| 4. Verification And Docs   | Wire scripts, ensure typecheck/build/tests pass, and document the first frontend slice                           | `docs/TODOS2.md`, `docs/README.md`, package scripts                                          |

## Workstream 1: Workspace And Contracts

Goal: create the frontend package boundaries without coupling the renderer contracts to the app shell.

- [x] Add `src/console` to npm workspaces.
- [x] Add `@hcm-next/ui-contracts` package.
- [x] Add `@hcm-next/ui-runtime` package.
- [x] Add TypeScript project references for the new packages.
- [x] Add path aliases for the new packages.
- [x] Define surface mode types.
- [x] Define brand pack and semantic token types.
- [x] Define page definition types.
- [x] Define page region and layout types.
- [x] Define widget instance types.
- [x] Define widget definition metadata types.
- [x] Define widget trust tiers.
- [x] Define data binding types.
- [x] Define bound-value provenance types.
- [x] Define dynamic rule types.
- [x] Define action binding types.
- [x] Export all public contracts from a package index.

## Workstream 2: Runtime And Widgets

Goal: make page definitions executable by a pure runtime before building the full editor.

- [x] Add a default HCM Next brand pack.
- [x] Add brand token CSS variable conversion.
- [x] Add deterministic brand-pack merge behavior.
- [x] Add a default widget registry.
- [x] Register layout widgets.
- [x] Register governed workflow widgets.
- [x] Register benign content widgets.
- [x] Register media widgets.
- [x] Register audit and approval widgets.
- [x] Add runtime binding context shape.
- [x] Add constant binding resolution.
- [x] Add workflow binding resolution.
- [x] Add actor binding resolution.
- [x] Add employee binding resolution.
- [x] Add tenant binding resolution.
- [x] Add brand binding resolution.
- [x] Add uploaded asset binding resolution.
- [x] Add fallback handling for unresolved bindings.
- [x] Add visibility rule evaluation.
- [x] Add permission rule evaluation.
- [x] Add surface-mode rule evaluation.
- [x] Add page resolver that filters widgets by rules.
- [x] Add page resolver that resolves widget bindings.
- [x] Add checked-in page definitions for the first frontend slice.
- [x] Add runtime unit tests.

## Workstream 3: Console App

Goal: provide a usable first frontend shell for workflow operators and admin preview.

- [x] Add `@hcm-next/console` package.
- [x] Add Vite React configuration.
- [x] Add app HTML entrypoint.
- [x] Add React application entrypoint.
- [x] Add TanStack Query provider.
- [x] Add browser router.
- [x] Add app shell navigation.
- [x] Add brand token provider.
- [x] Add global styles using semantic tokens.
- [x] Add demo runtime context.
- [x] Add workflow page renderer.
- [x] Render layout widgets.
- [x] Render employee summary widget.
- [x] Render dynamic form field group widget.
- [x] Render current-versus-proposed diff widget.
- [x] Render approval decision panel widget.
- [x] Render simulation result widget.
- [x] Render audit timeline widget.
- [x] Render task/request queue widget.
- [x] Render markdown, sanitized HTML, image, audio, video, and PDF content widgets.
- [x] Add Change Request Hub route.
- [x] Add Manager Request route.
- [x] Add Approval Review route.
- [x] Add Simulation route.
- [x] Add Audit Timeline route.
- [x] Add Admin Preview route.
- [x] Add API client foundation for existing workflow endpoints.
- [x] Add responsive layout behavior.
- [x] Add accessible button and form semantics.

## Workstream 4: Verification And Docs

Goal: keep the first frontend slice buildable and documented.

- [x] Add console scripts to root package scripts.
- [x] Ensure root build includes the new workspaces.
- [x] Run `npm install` to update workspace lockfile entries.
- [x] Run frontend/runtime tests.
- [x] Run typecheck for new workspaces.
- [x] Run console build.
- [x] Run root typecheck.
- [x] Run root build.
- [x] Add the frontend TODO file to the docs map if missing.
- [x] Mark completed TODOs only after implementation and verification.

## Completion Criteria

- [x] `npm --workspace @hcm-next/ui-runtime run test` passes.
- [x] `npm --workspace @hcm-next/ui-contracts run typecheck` passes.
- [x] `npm --workspace @hcm-next/ui-runtime run typecheck` passes.
- [x] `npm --workspace @hcm-next/console run typecheck` passes.
- [x] `npm --workspace @hcm-next/console run build` passes.
- [x] `npm run typecheck` passes.
- [x] `npm run build` passes.
- [x] All frontend foundation TODOs in this file are complete.
