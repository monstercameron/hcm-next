# Production UI composition

This package is the GoWebComponents composition layer for the production web
application. It accepts already-authorized presentation models and emits
deterministic server-rendered pages. It owns no workflow, authorization,
credential, persistence, or integration truth.

## Dependency direction

```text
HTTP / future Go-WASM adapter
           |
           v
      ViewProvider  <--- gRPC projection adapter or fixture provider
           |
           v
       PageRegistry
           |
           v
  AppShell + Feature Page + Shared Components
           |
           v
    GoWebComponents renderer
```

The page registry is the only canonical inventory of page identity, route,
title, navigation eligibility, ordering, and renderer. HTTP adapters resolve
routes through the registry instead of casting URL segments into page IDs.

## Source ownership

- `registry.go`: canonical page definitions and route resolution.
- `provider.go`: transport-neutral projection boundary.
- `shell.go`: header, navigation, page frame, and landmarks.
- `components.go`: reusable product primitives.
- `page_*.go`: one feature surface and its private subcomponents per file.
- `selectors.go`: pure filtering and selection over authorized models.
- `model.go`: presentation contracts and development fixtures.
- `styles.go`: semantic platform and component styles.
- `theme.go`: validated semantic token compiler and protected accessibility boundaries.
- `appearance.go`: closed customer palette, shape, density, glyph, and motion presets.
- `appearance_components.go`: storage-independent customer appearance editor.
- `render.go`: document assembly only.

## Adding a page

1. Add its stable `PageID` and one `PageDefinition`.
2. Implement its renderer in a dedicated `page_<feature>.go` file.
3. Accept only an already-authorized `View`; add typed view fields when needed.
4. Compose shared primitives instead of duplicating shell or surface markup.
5. Add route, deterministic-render, accessibility, and authorization tests.
6. Keep mutations behind a registered semantic action; pages never invent
   business authority.
