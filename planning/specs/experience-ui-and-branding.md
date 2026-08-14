# Experience, Dynamic UI, and Branding Contract

## Purpose

HCM Next supports static product navigation, governed schema-driven pages, and
agent-assembled contextual workspaces through one presentation contract. The
server remains authoritative for state, actions, permissions, and business
effects.

```text
domain/workflow facts
        |
        +-- available actions
        +-- authorization scope
        +-- data classification
        +-- provenance
        v
typed PageDefinition
        |
        v
GoWebComponents renderer
        |
        v
human interaction
        |
        v
governed capability / workflow transition
```

Generated UI is data and configuration. It is never arbitrary generated
JavaScript, HTML, CSS, SQL, or direct database access.

## Presentation Authority Boundary

The experience layer may:

- Select registered widgets and constrained layouts.
- Bind authorized values and provenance into those widgets.
- Present currently available workflow actions.
- Apply semantic brand, locale, density, and accessibility settings.
- Save a generated page as a governed draft.

It may not:

- Invent authority or expose hidden fields.
- Bypass workflow transitions or invoke private mutations.
- Treat a cached page definition as current permission evidence.
- execute arbitrary customer or model-supplied code.
- Turn agent inference into canonical worker truth.

## Resolution Order

Presentation is resolved in a stable order:

```text
platform safety constraints
          |
workflow interaction contract
          |
current business state
          |
field/record/action authorization
          |
brand and locale
          |
published page definition
          |
persona and permitted preferences
          |
device / surface adaptation
```

Later layers may specialize appearance. They cannot weaken safety, workflow,
or authorization decisions from earlier layers.

## Page Definition

```text
PageDefinition

page_id
version
purpose
surface
input_schema
regions[]
actions[]
required_capabilities[]
data_domains[]
brand_scope
locale_policy
accessibility_profile
risk_class
status
  draft | validated | published | retired
```

Each region uses constrained layout primitives such as stack, grid, split,
tabs, summary rail, table, or task panel. Responsive behavior comes from the
renderer and semantic layout tokens, not arbitrary breakpoint code in the page
definition.

## Widget Registry and Trust Tiers

```text
WidgetDefinition

widget_type
version
input_schema
output_events[]
allowed_actions[]
supported_surfaces[]
classification_limit
accessibility_contract
render_component
```

Widgets have explicit trust tiers:

| Tier           | Use                                              | Rule                                                            |
| -------------- | ------------------------------------------------ | --------------------------------------------------------------- |
| Governed       | Workflow forms, approvals, money, legal evidence | Platform-owned component and strict typed contract              |
| Content        | Help, descriptions, non-sensitive summaries      | Sanitized content and constrained interaction                   |
| External embed | Approved third-party content                     | Sandboxed, allowlisted origins, explicit data and egress policy |

External embeds cannot receive workflow context, credentials, or sensitive
data by default.

## Provenance-Bearing Bindings

A widget binding carries more than a value:

```text
BoundValue

value
display_value
source_type
source_id
source_version
authority_class
classification
confidence?
effective_at?
recorded_at?
editable
masked
```

This lets the UI distinguish a canonical fact, external observation, manager
observation, and agent hypothesis instead of flattening all four into the same
visual certainty.

Sensitive fields resolve to one of:

```text
SHOW | MASK | REDACT | HIDE | SUMMARY_ONLY | DERIVED_ONLY
```

Filtering happens before data reaches the component tree, not through CSS or
client-only hiding.

## Brand Packs

Branding uses semantic tokens rather than raw page-specific values:

```text
BrandPack

brand_id
version
scope
  platform | tenant | company | organization | experience
tokens
  color | typography | spacing | radius | elevation | motion
assets
  logo | icon | illustration
surface_modes
  light | dark | high_contrast | print
effective_interval
status
```

Resolution follows scope inheritance, but accessibility and platform safety
constraints cannot be overridden. A custom brand must pass contrast, focus,
zoom, reduced-motion, high-contrast, and document-rendering checks.

## Actions

Page actions bind only to public semantic capabilities or available workflow
transitions:

```text
UI Action
   |
   +-- current action token
   +-- expected workflow/resource version
   +-- idempotency key
   +-- typed input
   v
Capability Gateway
   v
AuthZ + policy + revalidation
```

The client does not synthesize approval, execution, repair, or direct employee
mutation endpoints.

## Agent-Generated Workspaces

The documented legacy chat flow used a model tool loop to search permitted
workers, inspect workflow state, and emit a canonical page definition. The
target preserves the useful boundary:

```text
human intent
     |
agent capability discovery
     |
authorized display-safe results
     |
PageDefinition draft
     |
schema + widget + security validation
     |
ephemeral render or governed publication
```

Agent output passes:

- Page schema validation.
- Registered-widget and layout validation.
- Capability and field authorization.
- Data classification and DLP checks.
- URL/embed/content sanitization.
- Accessibility checks.
- Cost and risk limits.

A generated page does not become a reusable tenant asset until it is versioned,
previewed with fixtures, reviewed, and published.

## Content Safety

- Markdown uses an allowlisted parser and renderer.
- Raw HTML is prohibited unless a separately sandboxed component owns it.
- Images, audio, and attachments use approved object references and malware/
  content processing.
- Links and embeds pass scheme, origin, egress, and classification policy.
- Prompt, tool, and retrieved content is treated as untrusted input to the
  Agent Plane.

## Accessibility Contract

Every governed component and page must support:

- Keyboard-only completion and visible focus.
- Semantic names, roles, states, and relationships.
- Error identification connected to the relevant field.
- Zoom/reflow and responsive layout without lost actions.
- Screen-reader announcements for workflow state changes.
- Reduced motion and high-contrast modes.
- Accessible generated documents when the workflow requires them.

Accessibility is a compiler/publication gate, not a post-release visual review.

## Lifecycle and Preview

```text
draft
  |
validate schema/widgets/actions/security/accessibility
  |
preview
  +-- roles
  +-- workflow states
  +-- locales
  +-- brands
  +-- classifications
  +-- mobile/desktop
  |
publish immutable version
  |
pin consumers / progressive rollout
  |
observe and rollback
```

## GWC Implementation

Phase 1 implements one Promotion workspace using GWC/GoWebComponents. Legacy
React schemas and fixtures may inform offline conformance tests, but no React
renderer or TypeScript application ships beside the GWC implementation.

grpcbridge-generated clients consume the same Protobuf capability contracts as
internal Go callers. SchemaFlux compiles and exposes the selected page/widget
and API catalogs and their dependency graph. Neither tool may create a second
source of business semantics.

## Phase 1 Depth

**Implement:**

- Promotion request, approval, simulation, and repair/timeline workspace.
- Small governed widget registry.
- Semantic brand tokens and light/high-contrast rendering.
- Field filtering, provenance indicators, available-action binding.
- Keyboard, screen-reader, contrast, and browser-level conformance tests.

**Minimal contract:**

- Page draft/publish lifecycle.
- Agent-generated PageDefinition validation.
- Tenant/company brand scope.
- Secure external-content boundary.

**Deferred:**

- General visual page builder.
- Arbitrary customer widgets or code.
- Full multi-channel generated experiences.
- Pixel-for-pixel recreation of the legacy React console.
