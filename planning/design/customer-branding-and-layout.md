# Customer branding, reorganization and styling

Status: proposed customer-experience design contract, September 5, 2026.
Green is the shipped HCM Next identity, not a fixed customer requirement.

The [interactive workspace slice](hcm-brandable-workspace.html) now demonstrates
company identity, brand accent, shape, density, navigation grouping, section
order and navigation surface. These are local presentation controls only.

See [brand/layout variants](mockups/interaction-v1/20-brand-layout-variants.png),
[Brand & navigation studio](mockups/interaction-v1/21-branding-studio.png),
[visual language](visual-language.md) and
[governed page composition](../specs/production-frontend-and-page-composition.md).

## What customers can change

| Layer               | Customer options                                                                           | Invariants                                                                                    |
| ------------------- | ------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------- |
| Identity            | Company logo, display name, approved brand assets, tenant-resolved sign-in branding        | Unambiguous organization/acting context; neutral discovery before a tenant is safely resolved |
| Color               | Primary accent, supporting brand colors, surface tint, light/dark theme where fully tested | Valid text/control contrast; status and focus meanings remain stable                          |
| Typography          | Approved licensed heading/body font choices, bounded scale presets                         | Readability, fallback, zoom, long strings and stable control layout                           |
| Shapes              | Square, Balanced and Soft radius presets                                                   | Controls remain identifiable and usable; no clipped content                                   |
| Spacing             | Comfortable/Compact defaults, bounded section padding and gaps                             | Minimum usable targets, reflow, focus visibility and readable labels                          |
| Shell               | Navigation groups, order, localized labels, favorites, role landing destinations           | Stable route identity, authorized discovery, context/security controls and Help access        |
| Pages               | Reorder optional sections, choose columns, add approved blocks, create custom pages        | Required disclosures, attention and task controls remain reachable and visible                |
| Workbenches         | Saved filters, table columns/order, sort, density and detail placement                     | Field-level access, accurate data units, safe selection/bulk-action semantics                 |
| Content             | Help links, welcome text, approved announcements and templates                             | No impersonation, unsafe content or replacement of governed confirmation text                 |
| Personal preference | Favorites, permitted saved views, density, language, reduced motion                        | Cannot override tenant locks or authorization                                                 |

Customers may organize the same capabilities by department, lifecycle or daily
work. A navigation label is presentation, not the resource's identity.
Moving Payroll into Finance or renaming My Work to Requests must not break
bookmarks, audit references, search or return navigation.

## Design tokens, not hard-coded green

Separate brand tokens from semantic tokens. Primary action and active navigation
use the resolved customer accent. Success, warning, danger, focus and disabled
states have their own semantic roles. A blue customer theme must not turn
warning into blue or imply a different workflow outcome.

Ship complete tested theme presets, not one arbitrary accent replacement with
unreviewed hover, selected and disabled colors. Each theme defines text-on-accent,
hover/pressed fills, selected background/indicator, links, focus treatment and
every relevant surface pair. Customers can supply a brand color; the studio
checks or proposes an accessible operational variant without silently changing
the stored brand asset.

Radius presets: Square 4px controls/8px panels; Balanced 8px/12px; Soft 12px/16px.
Typography and density are independent from radius and brand. Do not force a
customer to accept a new layout merely to use their logo.

In the planned Go/web-component/WASM frontend, components consume semantic theme
tokens and documented styling hooks. Expose stable CSS custom properties,
approved component parts and composition slots as the design contract; avoid
customer selectors depending on private DOM or global CSS overriding internals.
The initial customer editor does not admit arbitrary JavaScript, unbounded CSS
or direct database queries. This is an architectural design constraint, not
new implementation work in this corpus.

## Configuration inheritance and ownership

| Precedence                | Owner and intent                                             |
| ------------------------- | ------------------------------------------------------------ |
| Product defaults          | Complete usable experience without configuration             |
| Tenant brand              | Shared identity, typography and shape defaults               |
| Tenant experience profile | Navigation/page arrangement for an admitted role or audience |
| Page override             | Bounded component placement and local presentation           |
| Personal preference       | Allowed density, favorites and saved views                   |

Resolve overrides only within each field's allowed ownership. Accessibility and
authorization constraints apply after composition; no later layer can undo them.
The studio shows inherited values, local overrides, lock owner and Reset to
inherited. Avoid unexplained disabled fields.

An audience selects who may receive a page; it never grants permission to the
page's records, widgets or actions. Preview-as uses synthetic or otherwise
authorized data and is visibly marked. It is not impersonation.

## Brand & navigation studio

Use an outline/settings rail, editing pane and live preview. Keep advanced
options progressive so a customer can upload a logo and choose an accent
without navigating a full page builder.

The design includes:

- Brand asset upload with fit/clear-space preview and light/dark alternatives.
- Accent and approved palette presets with text/control contrast feedback.
- Typography samples using long names, numbers and translated labels.
- Shape/density previews applied to actual inputs, tables and dialogs.
- Navigation tree with add group, rename, reorder, move up/down and restore.
- Page outline with named slots, optional blocks and clearly explained locks.
- Desktop/narrow preview and synthetic Employee/Manager/Specialist variants.
- Before/after review, inherited-versus-overridden indicators and validation.
- Draft save, authorized publication review, version history and rollback.

Dragging always has a keyboard equivalent. Reordering sections also reorders
logical reading/focus order; CSS placement must not create a different visual
and assistive-technology sequence.

## Publishing and updates

Changes begin as drafts and do not unexpectedly rearrange another user's
active task. Preview and validate navigation reachability, tokens, responsive
layout and required regions before publication. Publish to the intended
experience profile with an effective version and record who changed what.
Rollback restores a known published configuration, not business records.

Existing page sessions should be told a new layout is available and apply it
at a safe navigation boundary; do not move a primary button during a decision.
Preserve stable page IDs, URLs and bookmarks across cosmetic reorganizations.
Product upgrades migrate supported token/slot versions with a diff and a
fallback; silently discarding customer layout is not acceptable.

## Review examples and remaining work

Board 20 shows three different brands and navigation arrangements over the same
capabilities. Board 21 shows the editing experience. The examples are proposals;
a screenshot's lock icon does not establish a real immutable region.

Test one strongly contrasting customer brand, a long translated navigation
tree, Compact mode, narrow layout, keyboard reordering and a product upgrade
before treating extensibility as proven. Dark mode is an eventual complete
theme contract, not a claim that these light-mode boards already design it.
