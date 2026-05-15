# Dynamic UI And Branding Strategy

This document captures the product and architecture decisions for the HCM Next UI layer.

The core decision is that HCM Next cannot use fixed, hand-built screens as the primary model. Workflows are dynamic, permission-aware, tenant-specific, and often customer-branded. The UI must match that by rendering workflow pages from governed configuration, runtime data bindings, brand packs, and RBAC-aware widget contracts.

The UI layer should feel highly customizable to customers while remaining controlled by platform semantics, workflow state, permissions, accessibility rules, and audit requirements.

## Goals

- Render workflow-specific pages from configuration instead of hard-coded screens.
- Support full brand integration from customer-provided assets, websites, images, screenshots, or manual customization.
- Let admins compose workflow pages from controls and widgets that can be moved, reordered, resized, themed, prefilled, and API-backed.
- Support customer-level, workflow-level, persona-level, and limited employee-level personalization.
- Keep employee-data permissions, workflow transitions, validation, audit, and risk communication controlled by the platform.
- Support both HCM Next full-app experiences and embedded customer-owned experiences.
- Avoid building a full visual workflow canvas first. The UI builder composes workflow pages, not arbitrary workflow logic.

## Non-Goals

- Do not build a general website builder.
- Do not allow arbitrary JavaScript execution in customer-authored pages.
- Do not allow custom UI to bypass required workflow inputs, approvals, audit text, or server-side permission checks.
- Do not let brand or personalization rules alter risk severity, approval semantics, sensitive-field visibility, or material HR transaction behavior.
- Do not use the UI layer as the source of truth for workflow state. Runtime workflow APIs remain authoritative.

## Product Principle

The UI should be dynamic through semantics, not through uncontrolled code.

```text
Workflow config
  -> interaction contract
  -> page definition
  -> brand and experience resolver
  -> permission and data-binding resolver
  -> rendered UI
  -> workflow transition API
```

Workflow config should define what the user is doing. The brand and UI builder should define how that work is presented.

Examples:

- Workflow says: `approval action`, `compensation field`, `high risk`, `waiting_approval`.
- UI builder says: show these widgets in this layout for HRBP.
- Brand resolver says: use this tenant/entity brand pack and embedded mode.
- Permission resolver says: mask compensation for this actor unless they have the right scope.
- Renderer says: produce the page using governed widgets and tokens.

## First Product Surfaces

The first UI layer should prioritize the surfaces defined in the master plan:

1. Manager request form
2. HRBP / compensation approval view
3. Transaction simulation view
4. Audit / review timeline

These surfaces should be built as configurable page templates backed by reusable widgets.

### Manager Request Form

Purpose: let a manager or initiator propose a high-risk employee change without seeing irrelevant or unauthorized fields.

Expected widgets:

- Employee summary
- Current job/org/compensation context, permission-filtered
- Dynamic form sections
- Effective date picker
- Business reason selector
- Policy acknowledgement
- Missing data and policy findings
- AI change brief, once available
- Submit/cancel action bar

### Approval View

Purpose: let HRBP, compensation, finance, manager, or executive approvers make decisions with only the context they are allowed to see.

Expected widgets:

- Approval task header
- Current vs proposed diff
- Reviewer-specific context panels
- Approval routing explanation
- AI change brief and risk findings
- Comment and reason controls
- Approve, reject, request more info, delegate actions
- Timeline summary

### Transaction Simulation View

Purpose: preview exactly what will happen before execution.

Expected widgets:

- Current state
- Proposed state
- Field-level changes
- Effective-date impact
- Approval requirements
- Payroll impact
- Finance impact
- Identity/access impact
- Downstream systems touched
- Validation failures
- Reconciliation risks
- Internal writes preview
- External writes preview
- Execute readiness checklist

### Audit / Review Timeline

Purpose: show the ledger-derived history of a change.

Expected widgets:

- Business timeline
- Audit event stream
- Debug/runtime trace, admin only
- Approval history
- Permission decision history
- AI visibility disclosure
- External call log
- Failure and retry history
- Evidence/document history
- Export audit packet

## Brand Integration

Brand integration is not only logo and color customization. It is a runtime experience layer that adapts pages to the customer, business unit, workflow, actor, and embedded surface.

### Brand Sources

Brand packs may be created from:

- Uploaded logos
- Uploaded brand images
- Website URLs
- Customer portal screenshots
- Uploaded design reference images
- Existing brand guideline documents
- Manual admin customization
- Platform-provided presets

Extraction from these sources should produce suggested design tokens and experience settings. It must not directly inject arbitrary CSS into workflow pages.

### Brand Extraction Output

The brand extractor should suggest:

- Primary and secondary colors
- Neutral surface colors
- Status color candidates
- Logo variants
- Typography feel
- Density preference
- Border radius preference
- Button treatment
- Image style
- Icon style
- Tone hints for labels and helper text

All extracted output should be normalized into platform-governed semantic tokens.

### Semantic Brand Tokens

Brand packs should use semantic tokens rather than arbitrary style properties.

Example token groups:

```text
surface.base
surface.subtle
surface.raised
surface.inverse

text.primary
text.secondary
text.muted
text.inverse

border.default
border.strong
border.focus

action.primary.background
action.primary.text
action.secondary.background
action.danger.background

status.success
status.warning
status.error
status.info

risk.low
risk.medium
risk.high
risk.critical

approval.pending
approval.approved
approval.rejected

field.restricted
field.masked
timeline.event
timeline.systemEvent
```

The renderer maps workflow semantics to these tokens. For example, a high-risk compensation warning uses `risk.high`, not a hard-coded red.

### Brand Pack Shape

A first-pass brand pack can be modeled as:

```ts
type BrandPack = {
  brandPackId: string;
  tenantId: string;
  name: string;
  status: "draft" | "active" | "archived";
  source: {
    kind: "manual" | "logo_upload" | "website" | "screenshot" | "preset";
    sourceRefs: string[];
  };
  identity: {
    displayName: string;
    logoUrl?: string;
    logoMarkUrl?: string;
    faviconUrl?: string;
  };
  tokens: {
    color: Record<string, string>;
    typography: Record<string, string>;
    radius: Record<string, string>;
    spacing: Record<string, string>;
    shadow: Record<string, string>;
  };
  experience: {
    density: "compact" | "comfortable" | "spacious";
    chrome: "hcm_next" | "customer" | "embedded" | "minimal";
    buttonStyle: "filled" | "tonal" | "outline";
    cardStyle: "flat" | "outlined" | "raised";
  };
  validation: {
    contrastPassed: boolean;
    warnings: Record<string, unknown>[];
    errors: Record<string, unknown>[];
  };
  version: number;
};
```

### Brand Scope Resolution

The active brand pack should be resolved at runtime.

Preferred priority:

```text
explicit workflow/page brand override
-> legal entity brand
-> business unit brand
-> location or region brand
-> tenant default brand
-> HCM Next default brand
```

This supports enterprise reality: subsidiaries, acquired companies, regional operating brands, clinics, stores, franchises, and internal shared-services groups.

### Surface Modes

The same workflow page may render in different host contexts:

- HCM Next full application
- Customer internal portal
- Embedded manager widget
- Approval-only widget
- Slack/Teams-like compact approval
- Admin preview
- Audit/export view
- Mobile approval view
- Kiosk or shared-device mode

Surface mode affects chrome, density, navigation, action placement, and widget availability. It must not affect permissions or workflow legality.

## UI Builder Model

The UI builder should compose workflow pages from governed widgets. Admins can arrange and configure widgets, but widget behavior is constrained by the platform.

```text
Page
  -> regions
  -> widgets
  -> data bindings
  -> visibility rules
  -> permission rules
  -> action contracts
  -> brand tokens
  -> responsive rules
```

### Page Definition

```ts
type WorkflowPageDefinition = {
  pageId: string;
  workflowIntent: string;
  workflowState?: string;
  surfaceMode: string;
  title: string;
  regions: PageRegion[];
  widgets: WidgetInstance[];
  brandBinding?: BrandBinding;
  version: number;
};
```

### Region

```ts
type PageRegion = {
  regionId: string;
  role:
    | "header"
    | "primary"
    | "secondary"
    | "sidebar"
    | "actionBar"
    | "footer"
    | "modal"
    | "drawer";
  layout: "grid" | "stack" | "tabs" | "split" | "fixed";
  responsive: Record<string, unknown>;
};
```

### Widget Instance

```ts
type WidgetInstance = {
  widgetId: string;
  widgetType: string;
  regionId: string;
  layout: {
    x?: number;
    y?: number;
    width?: number;
    height?: number;
    minWidth?: number;
    minHeight?: number;
  };
  props: Record<string, unknown>;
  dataBindings: Record<string, DataBinding>;
  visibilityRules: VisibilityRule[];
  permissionRules: PermissionRule[];
  validationRules?: ValidationRule[];
  actionBindings?: ActionBinding[];
  auditBehavior?: AuditBehavior;
  responsive?: Record<string, unknown>;
};
```

### Layout Rules

The initial builder should use constrained layout primitives instead of freeform pixel positioning.

Recommended primitives:

- Grid
- Stack
- Split pane
- Sidebar
- Sticky action bar
- Tabs
- Accordion
- Modal
- Drawer
- Wizard/stepper

The builder can allow moving, resizing, reordering, and scaling, but within grid constraints:

```text
x: 0
y: 2
width: 6
height: 3
minWidth: 3
maxWidth: 12
allowedSurfaces: ["desktop", "tablet"]
```

This keeps generated pages responsive and testable.

## Widget Trust Tiers

Widgets should be classified by trust and authority.

### Tier 1: Governed Workflow Widgets

These widgets are tied directly to workflow semantics, permissions, validation, audit, or transitions.

Examples:

- Dynamic workflow form
- Employee summary
- Current vs proposed diff
- Approval decision panel
- Simulation preview
- Transaction plan preview
- Audit timeline
- Repair panel

Tier 1 widgets can submit workflow transitions only through the approved runtime API.

### Tier 2: Benign Content Widgets

These widgets display static, dynamic, or API-backed content but do not mutate workflow state directly.

Examples:

- Text
- Markdown
- Image
- Audio player
- Video player
- Policy snippet
- Link list
- PDF preview
- KPI tile

Tier 2 widgets can be highly customizable, but they cannot override workflow state, required fields, permissions, or audit.

### Tier 3: External Embed Widgets

These widgets render external or semi-external content.

Examples:

- iframe
- Hosted BI dashboard
- External help center article
- Map embed
- Calendar embed
- Video provider embed
- Internal portal embed

Tier 3 widgets need strict allowlists, sandboxing, content security policy, and explicit data-sharing controls.

## Widget Registry

The product should maintain a registry of available widgets. Each widget declares its allowed data bindings, allowed actions, permission behavior, audit behavior, responsive behavior, and brand token usage.

```ts
type WidgetDefinition = {
  widgetType: string;
  displayName: string;
  tier: "governed_workflow" | "benign_content" | "external_embed";
  category: string;
  allowedSurfaces: string[];
  propsSchema: Record<string, unknown>;
  bindingsSchema: Record<string, unknown>;
  actionSchema?: Record<string, unknown>;
  permissionBehavior: "inherits_page" | "requires_field_permissions" | "custom";
  auditBehavior: "none" | "rendered" | "input_changed" | "action_submitted";
  supportsResize: boolean;
  supportsDataBinding: boolean;
  supportsPersonalization: boolean;
};
```

## Widget Categories

### Layout Widgets

Layout widgets structure the page.

- Section
- Grid
- Stack
- Split pane
- Sidebar
- Header bar
- Footer action bar
- Sticky action rail
- Tabs
- Stepper
- Accordion
- Drawer
- Modal
- Divider
- Spacer
- Card/container
- Responsive breakpoint rule
- Embedded widget shell
- Print/export layout

Layout widgets should not directly access sensitive data or workflow transitions.

### Basic Form Controls

These collect standard workflow input.

- Text input
- Textarea
- Number input
- Money input
- Percent input
- Date picker
- Effective date picker
- Date range picker
- Time picker
- Select
- Multi-select
- Searchable lookup
- Radio group
- Checkbox
- Toggle
- Slider
- Stepper control
- File upload
- Evidence upload
- Repeating list
- Table editor
- Address editor
- Phone editor
- Email editor
- URL input
- Policy acknowledgement checkbox
- E-signature / attestation

### HCM Domain Form Controls

These controls understand HCM semantics.

- Employee picker
- Manager picker
- Org unit picker
- Department picker
- Legal entity picker
- Cost center picker
- Location picker
- Job profile picker
- Position picker
- Pay band picker
- Compensation editor
- Job change editor
- Manager/org change editor
- Worker assignment editor
- Role binding editor
- Effective-dated fact editor
- Payroll cutoff picker

Domain controls should always expose their data provenance and permission behavior.

### Read-Only Context Widgets

These display current state and actor-specific context.

- Employee profile header
- Employee summary
- Current legal name panel
- Current contact info panel
- Current emergency contacts panel
- Current employment status
- Current job panel
- Current manager panel
- Current organization placement
- Current compensation
- Current worker assignments
- Current access/role bindings
- Direct reports summary
- Team/org context
- Position summary
- Cost center summary
- Location summary
- Payroll cutoff summary
- Existing open requests for employee
- Related workflow history

These widgets must use permission-filtered projections.

### Change Review Widgets

These help users understand what is changing.

- Current vs proposed diff
- Field-level diff table
- Changed fields summary
- Effective-date impact
- Business reason summary
- Required approvals list
- Approval routing explanation
- Policy findings
- Missing data findings
- Risk score
- Risk reasons
- Downstream impact summary
- Payroll impact
- Finance impact
- Identity/access impact
- Reconciliation risk
- Reversibility / rollback summary

### Approval Widgets

These support human review.

- Approval task list
- Approval decision panel
- Approve/reject/request-info buttons
- Approval comment box
- Required rejection reason control
- Delegation control
- Approval chain visual
- Sequential approval tracker
- Parallel approval quorum tracker
- Veto-holder indicator
- SLA/due date indicator
- Escalation status
- Reviewer-specific visible context
- Approval history summary

Approval widgets may only call allowed workflow transitions.

### Transaction And Execution Widgets

These support simulation, execution, and repair.

- Transaction plan preview
- Simulation result
- Execution readiness checklist
- Internal writes preview
- Projection patch preview
- External writes preview
- Integration outbox status
- Execute button
- Dry-run output
- Retry panel
- Repair action panel
- Manual reconciliation form
- Roll-forward repair panel
- Cancel/supersede panel
- Reversibility summary
- Compensation plan summary

### Audit And Timeline Widgets

These render ledger-derived history.

- Business timeline
- Audit ledger stream
- Debug/runtime trace
- Actor/action/event list
- Approval history
- Permission decision viewer
- AI visibility viewer
- External call log
- Failure/retry history
- Document/evidence history
- Export audit packet
- Correlation ID display
- Idempotency key display
- Workflow version display

### AI Review Widgets

AI widgets are review aids. They are not sources of authority.

- AI change brief
- AI risk explanation
- AI missing-data summary
- AI policy conflict summary
- AI downstream impact summary
- Suggested next actions
- Draft approval note
- Draft audit review
- Human-edited AI output
- AI source/visibility disclosure
- AI model/provider metadata, admin/audit only

AI widgets must only render content generated from fields the actor and AI scope were allowed to see.

### Benign Content Widgets

These are simple content and presentation blocks.

- Text block
- Rich text block
- Markdown viewer
- Sanitized HTML viewer
- Image
- Image gallery
- Logo block
- Avatar
- Icon
- Banner
- Callout / notice
- Help tooltip
- FAQ block
- Link list
- PDF/document preview
- Policy snippet
- Terms / compliance notice
- Custom footer
- Map/location preview
- Calendar/date summary
- QR code

### Media Widgets

Media widgets can support brand, training, accessibility, and employee-specific experiences.

- Hero image
- Department/location photo
- Employee avatar
- Org logo
- Training video
- Policy explainer video
- Audio player
- Accessibility audio prompt
- Uploaded evidence preview
- Before/after document preview

### Data Display Widgets

These can be API-backed but should remain display-first.

- KPI tile
- Status badge
- Progress bar
- Data table
- Mini chart
- Org breadcrumb
- Related requests list
- Recent activity list
- Announcement feed
- Checklist display
- SLA countdown
- Work queue count
- Integration health tile

### External Embed Widgets

External embed widgets should be optional and tightly controlled.

- iframe embed
- Hosted dashboard embed
- External help center article
- BI chart embed
- Calendar embed
- Map embed
- Internal portal embed
- Video provider embed

Rules:

- Use allowlisted domains.
- Use sandboxed iframes.
- Block arbitrary scripts.
- Block unapproved form posts.
- Do not pass sensitive workflow data directly into arbitrary embeds.
- Log external embed configuration changes.

## Data Binding

Every widget should support dynamic data bindings.

Supported source types:

- Constant / hard-coded value
- Workflow input
- Workflow context
- Workflow state
- Workflow config
- Employee projection
- Actor profile
- Tenant config
- Brand pack
- API query
- External integration
- Computed expression
- Previous workflow response
- Manual override
- Uploaded asset

Each bound value should carry provenance.

```ts
type BoundValue<T = unknown> = {
  value: T;
  source:
    | "constant"
    | "workflow_input"
    | "workflow_context"
    | "employee_projection"
    | "actor_profile"
    | "tenant_config"
    | "brand_pack"
    | "api"
    | "external_integration"
    | "computed"
    | "manual_override";
  sourcePath?: string;
  confidence: "authoritative" | "derived" | "suggested" | "unknown";
  editable: boolean;
  lastResolvedAt?: string;
};
```

### Binding Examples

Hard-coded:

```json
{
  "title": {
    "source": "constant",
    "value": "Request promotion"
  }
}
```

Employee projection:

```json
{
  "currentJob": {
    "source": "employee_projection",
    "path": "job"
  }
}
```

API-backed:

```json
{
  "payBand": {
    "source": "api",
    "method": "GET",
    "path": "/compensation-market/pay-bands",
    "params": {
      "jobCode": { "source": "workflow_input", "path": "proposedJob.jobCode" },
      "location": { "source": "employee_projection", "path": "organization.location" }
    }
  }
}
```

Computed:

```json
{
  "increasePercent": {
    "source": "computed",
    "expression": "(proposed.amount - current.amount) / current.amount * 100"
  }
}
```

Expressions should be evaluated by a constrained expression engine, not by arbitrary JavaScript.

## Dynamic Rules

Widgets need runtime rules.

Common rule types:

- Show/hide by actor role
- Show/hide by field permission
- Show/hide by workflow state
- Show/hide by risk level
- Show/hide by surface mode
- Read-only after approval
- Required only when a condition matches
- Mask sensitive values
- Collapse on mobile
- Switch to compact approval mode
- Use different data source by tenant/entity
- Require additional acknowledgement for high-risk changes

Example:

```json
{
  "visibilityRules": [
    {
      "when": {
        "source": "actor_profile",
        "path": "roles",
        "contains": "compensation_admin"
      },
      "effect": "show"
    }
  ],
  "permissionRules": [
    {
      "fieldGroup": "compensation",
      "fallback": "mask"
    }
  ]
}
```

## Action Binding

Actions must bind to workflow transitions, not arbitrary endpoints, unless the action is explicitly display-only or admin-only.

Allowed workflow actions include:

- Submit
- Save draft, once implemented
- Cancel
- Approve
- Reject
- Request more information
- Execute
- Retry
- Repair
- Delegate
- Export
- Add evidence
- Supersede
- Reopen repair

Example action:

```json
{
  "label": "Approve",
  "kind": "workflow_transition",
  "transition": "approve",
  "requiresConfirmation": true,
  "payloadBinding": {
    "approvalTaskId": {
      "source": "widget_state",
      "path": "selectedApprovalTaskId"
    },
    "comment": {
      "source": "widget_state",
      "path": "comment"
    }
  }
}
```

The server remains responsible for checking:

- Workflow state
- Expected version
- Idempotency key
- Actor permissions
- Approval task ownership
- Required payload fields

## Personalization

Personalization should exist at several layers.

### Tenant-Level

- Brand pack
- Default density
- Default app chrome
- Allowed widgets
- Default page templates
- Localization defaults

### Entity-Level

- Legal entity brand
- Business unit brand
- Location/region brand
- Workflow policy copy
- Local compliance notices

### Workflow-Level

- Page layout
- Form sections
- Required context widgets
- Approval panel shape
- Simulation widgets
- Timeline event emphasis
- Repair panel shape

### Actor/Persona-Level

- Manager layout
- HRBP layout
- Compensation layout
- Finance layout
- Payroll layout
- Executive compact approval layout
- System/admin debug layout

### Employee-Level

Employee-specific customization can exist, but should stay cosmetic or accessibility-related.

Allowed examples:

- Accent color
- Compact/comfortable density
- Large text
- Reduced motion
- Avatar/banner
- Preferred panel order
- Preferred language

Not allowed:

- Changing required fields
- Changing approval wording
- Changing audit wording
- Hiding risk severity
- Hiding required policy disclosures
- Revealing restricted fields
- Changing submit/approve/execute semantics

## Runtime Resolution Order

The renderer should resolve page behavior in this order:

```text
platform safety rules
-> workflow interaction contract
-> workflow state
-> permission/RBAC policy
-> customer/entity brand pack
-> workflow page definition
-> actor/persona layout overrides
-> employee display preferences
-> device and surface mode
```

This order means customer customization can affect presentation but cannot override safety or workflow correctness.

## Permissions And Security

The UI layer must assume the server is authoritative.

UI rules can hide or disable controls for usability, but server-side runtime APIs must enforce:

- Actor identity
- Role permissions
- Relationship access
- Attribute scopes
- Field-level permissions
- Action-level permissions
- Workflow state
- Approval task ownership
- Idempotency
- Expected version

### Sensitive Field Behavior

Possible display behaviors:

- Show
- Mask
- Redact
- Hide
- Show summary only
- Show derived value only

Examples:

- Manager sees operational impact but not unrelated compensation.
- Compensation approver sees pay data for the relevant request.
- HRBP sees assigned population only.
- Payroll sees payroll-impacting fields.
- Audit sees which fields AI could access.

### Benign Widget Safety

Markdown:

- Use a safe renderer.
- Disable raw HTML by default, or sanitize it before rendering.
- Strip scripts and unsafe links.

HTML:

- Sanitize aggressively.
- Block scripts.
- Block inline event handlers.
- Block unsafe iframes.
- Block external tracking pixels by default.
- Support a limited safe subset of tags.

Images:

- Proxy or scan uploaded images.
- Store approved asset references.
- Prevent image URLs from leaking sensitive query params.

Audio/video:

- Allow uploaded or allowlisted providers.
- Support captions/transcripts for accessibility where relevant.
- Do not autoplay by default.

External embeds:

- Sandbox.
- Allowlist domains.
- Do not pass sensitive context unless mediated by a governed integration.

## Accessibility And Quality Requirements

Generated pages must pass minimum quality checks before publish.

Required checks:

- Color contrast
- Keyboard navigation
- Focus states
- Mobile layout
- Text overflow
- Required field visibility
- Required action visibility
- Error state visibility
- Masked field clarity
- Reduced motion support
- Screen reader labels for form controls
- No overlapping widgets
- No clipped action buttons

## Governance

Page templates, brand packs, and widget definitions should be versioned and publishable.

Recommended lifecycle:

```text
draft
-> validate
-> preview by role/surface
-> test with fixture workflow data
-> publish
-> runtime pins version
-> rollback if needed
```

Validation should include:

- Required workflow fields are represented.
- Required actions are visible.
- Sensitive fields are permission-filtered.
- No widget references unknown data bindings.
- No action invokes an unsupported transition.
- Brand tokens pass contrast checks.
- Embedded content is allowlisted.
- Mobile preview is usable.
- Audit and timeline views remain available to authorized actors.
- AI widgets only use configured AI-visible fields.

## Builder Preview Modes

The builder should support previews for:

- Manager requester
- Employee requester
- HRBP
- Compensation approver
- Finance approver
- Payroll reviewer
- Executive approver
- System/admin user
- Audit/compliance user

Surface previews:

- Desktop full app
- Mobile
- Embedded widget
- Approval-only widget
- Print/export

State previews:

- Draft
- Preflighted
- Submitted
- Waiting approval
- Approved
- Simulated
- Executed
- Reconciled
- Failed
- Waiting repair
- Canceled/rejected

## Suggested V0 UI Implementation

The first implementation should avoid building the entire builder UI. Start with a renderer plus a small set of checked-in page definitions.

### Phase 1: Renderer Foundation

- Create a frontend app shell.
- Define page definition schema.
- Define widget registry schema.
- Implement brand token provider.
- Implement permission-aware data binding resolver.
- Render static checked-in page definitions.
- Use existing workflow runtime APIs.

### Phase 2: First Workflow Pages

Build pages for one flagship flow:

- Manager request page
- Approval page
- Simulation/transaction plan page
- Audit timeline page

Recommended flagship:

```text
employee.org_transfer_compensation_change
```

This workflow already exercises job, manager/org, compensation, approval routing, external writes, repair, and audit concepts.

### Phase 3: Brand Pack Support

- Add HCM Next default brand.
- Add one customer brand pack.
- Support logo upload or static logo reference.
- Support semantic color tokens.
- Validate contrast.
- Render full app and embedded modes.

### Phase 4: Builder UI

- Page region editor
- Widget palette
- Move/reorder/resize controls
- Props editor
- Binding editor
- Role/surface/state preview
- Publish validation

### Phase 5: Extraction And Personalization

- Logo/image color extraction.
- Website brand extraction.
- Screenshot-based style suggestion.
- Manual override controls.
- Persona-level layouts.
- Safe employee display preferences.

## Open Product Questions

- Should HCM Next branding remain visible in embedded mode as a trust signal, or disappear behind customer branding?
- How much customer-authored HTML should be allowed in early versions?
- Should brand extraction be automatic, admin-reviewed, or both?
- Should employee-level personalization be enabled before enterprise admins are comfortable with governance?
- Should page definitions be stored as workflow-version-owned artifacts, tenant-wide templates, or both?
- Should the builder support arbitrary responsive layout editing, or only controlled templates at first?

## Summary

HCM Next UI should be a dynamic, brand-aware workflow rendering system.

The product boundary is:

```text
Customers control presentation, composition, branding, and benign content.
HCM Next controls workflow semantics, permissions, audit, safety, and transaction authority.
```

This gives customers a highly tailored experience without turning HR transaction workflows into unsupported custom code.
