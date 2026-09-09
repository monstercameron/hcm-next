# Human Capital Management Suite visual language and UI craft

Status: proposed visual specification, September 5, 2026. Preserves the approved
green Home direction. This document refines visual decisions; it does not
reopen the product architecture or claim usability validation.

Start with [Visual language board](mockups/interaction-v1/18-visual-language.png)
and [Workbench craft board](mockups/interaction-v1/19-workbench-craft.png).
Use [the gallery](review.html) for the full collection and
[interaction contracts](interaction-design-corpus.md) for behavior.

## Design character

Customer adaptation is first-class: see [Customer branding, reorganization and
styling](customer-branding-and-layout.md). The values here are the default HCM
Next theme; customer accents, shape presets and page organization are resolved
through that contract rather than hard-coded into components.

Calm, capable, warm, precise. The interface should feel comfortable on first use
and efficient after months of daily work. Brand character comes from emerald,
humanist typography and consistent geometry, not decoration on every surface.

Use generous space around decisions and tighter rhythm within related facts.
A page should communicate its purpose, current context and next useful action
before any chart or decorative element competes for attention.

## Color: emerald as a deliberate accent

| Role                 | Token                  | Use                                                                 |
| -------------------- | ---------------------- | ------------------------------------------------------------------- |
| Primary action       | `#006B57`              | Primary buttons, active navigation marker, permitted action links   |
| Action hover/pressed | `#005344` / `#004536`  | Darker flat fill; do not resize the control                         |
| Canvas               | `#FAFAF7`              | Warm neutral application background                                 |
| Surface              | `#FFFFFF`              | Forms, work areas, menus                                            |
| Selected surface     | `#EAF3EF`              | Current row, selected navigation, active option                     |
| Strong text          | `#102238`              | Titles, values, body copy                                           |
| Supporting text      | `#526171`              | Labels, help, metadata that still needs reading                     |
| Decorative separator | `#D5DDD8`              | Card boundaries and quiet dividers, not the sole control identifier |
| Control boundary     | `#73847C`              | Visible input boundary where needed to identify a control           |
| Warning              | `#925400` on `#FFF4DB` | Review needed, waiting condition                                    |
| Error                | `#B42318` on `#FFF0EE` | Invalid entry, failed operation, material problem                   |
| Neutral status       | `#526171` on `#F0F2F1` | Draft, informational lifecycle state                                |

Keep most of the working surface neutral. A large emerald area is reserved for
an intentional brand moment, not a default dashboard header on every page.
Buttons have flat fills: raster mottling, glossy gradients and green texture
are generation artifacts, not design tokens.

Selection and status are separate: a selected row is sage even when its status
is amber or red. Green is not a generic sign that a workflow has executed.
Pair lifecycle colors with words; use consistent icons where they clarify.

Focus uses a two-layer indicator: a 2px white separator plus a 2px deep-ink
outer ring. Preserve visibility on both emerald and white. Error borders do
not replace keyboard focus. Respect system high-contrast colors.

### Calculated token checks

Computed from the exact sRGB token pairs below, not sampled from generated
images. Ratios are rounded for display only.

| Pair              | Foreground / background | Contrast |
| ----------------- | ----------------------- | -------- |
| Primary label     | `#FFFFFF` / `#006B57`   | 6.48:1   |
| Body              | `#102238` / `#FAFAF7`   | 15.35:1  |
| Supporting copy   | `#526171` / `#FFFFFF`   | 6.35:1   |
| Selected text     | `#006B57` / `#EAF3EF`   | 5.73:1   |
| Warning text      | `#925400` / `#FFFFFF`   | 6.02:1   |
| Error text        | `#B42318` / `#FFFFFF`   | 6.57:1   |
| Decorative border | `#D5DDD8` / `#FFFFFF`   | 1.38:1   |
| Control boundary  | `#73847C` / `#FFFFFF`   | 3.95:1   |

These text pairs exceed 4.5:1 on the listed backgrounds. The decorative border
does not; it must not be used as the only visible control boundary.
The normal-text threshold and the distinction for large text are documented
in [W3C contrast guidance](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html).
Recheck all real states, tinted backgrounds and customer themes; this is not
a whole-interface conformance claim.

## Geometry, space and depth

Use a 4px base rhythm. Typical gaps: 4 within an icon-label group, 8 between a
label and its control, 12 within a small cluster, 16 between related controls,
24 between page sections, 32 around a major content group. Preserve grouping
when text wraps instead of forcing fixed heights.

| Element                  | Starting specification                                            |
| ------------------------ | ----------------------------------------------------------------- |
| Input/button corner      | 8px radius                                                        |
| Card/panel corner        | 12px radius                                                       |
| Menu/popover corner      | 12px radius                                                       |
| Small status tag         | 6px radius; compact text-plus-icon shape                          |
| Avatar                   | Circle, 32px compact / 40px standard                              |
| Comfortable input/button | 44px minimum height, 16px horizontal padding                      |
| Comfortable task row     | 56px minimum; grows for wrapped text                              |
| Compact task row         | 40px minimum; explicit density preference                         |
| Desktop sidebar          | 240px; stable labels and bottom Help/Settings group               |
| Page gutter              | 24–32px desktop, 16px narrow                                      |
| Detail panel             | Approximately 360–440px when space permits; full page when narrow |

Avoid excessive pill shapes and nested rounded containers. A card should group
a meaningful task or subject, not wrap every label/value. Use whitespace and
section headings before adding another box.

Normal cards use a 1px separator and no shadow. Floating menus use a restrained
shadow such as 0 8px 24px with ink at 12% opacity. Dialogs use one higher layer
with a quiet scrim; do not stack multiple modal tasks.

## Typography and icon craft

Use the established humanist sans direction; select one licensed, self-hosted
font family with an appropriate fallback. Proposed baseline: Source Sans 3,
then system UI. Validate its rendering with real numbers and long names before
freezing the choice.

| Purpose                             | Size / line-height | Weight                |
| ----------------------------------- | ------------------ | --------------------- |
| Page title / large display specimen | 32 / 40px          | 600                   |
| Major section heading               | 24 / 32px          | 600                   |
| Card/detail heading                 | 20 / 28px          | 600                   |
| Body/form value                     | 16 / 24px          | 400                   |
| Label/table/metadata                | 14 / 20px          | 400 or 600 by purpose |

These refine earlier exploratory type specimens. Product page titles should
not switch arbitrarily between 24, 28 and 44px. Use relative units, flexible
containers and content wrapping; never shrink important text to fit a card.
Monetary values and comparable numeric columns use tabular numerals. Align
numbers right, text left, and include currency and units in the header or value.

Use one consistent line-icon family, approximately 20px with matching optical
weight. Icons accompany unfamiliar actions with text. Reserve circular icon
backgrounds for identity or a meaningful category; do not turn every row into
a decorative badge. Essential text is never hidden solely in tooltips.

## Page families: different jobs, shared language

| Family               | Visual hierarchy                                       | Helpful features                                                                  |
| -------------------- | ------------------------------------------------------ | --------------------------------------------------------------------------------- |
| Employee Home        | One immediate task, personal shortcuts, useful context | Shift/action card, personal requests, recent pay, clear next step                 |
| Manager Home         | Decisions first, then team coverage and changes        | Review queue, coverage strip, scoped team view                                    |
| Specialist workbench | Toolbar, dense readable queue, contextual detail       | Saved views, filters, sortable columns, column chooser, safe bulk actions         |
| Worker profile       | Identity, current facts, related changes               | Stable tabs, action discovery, timeline, sensitive-field treatment                |
| Guided request       | Step identity, focused fields, reviewable summary      | Save state, validation summary, typed dates, current/proposed comparison          |
| Review/decision      | Change, evidence, outcome choice                       | Exact revision, explanation, reason field when required, clear confirmation       |
| Insights/report      | Question, metric definition, useful comparison         | Date/population filters, readable labels, data table, source freshness            |
| Page/workflow studio | Outline, canvas, inspector                             | Selection, preview, keyboard reorder, validation, version/diff and publish review |
| Access/recovery      | One explanation and one safe next action               | Neutral wording, sign-in recovery, clear retry or status check                    |

Do not make every destination a four-KPI-card dashboard. Home helps someone
start; a workbench helps them finish a queue; a profile helps them understand
a person; a form helps them make one accurate request.

The refined workbench board demonstrates the target balance: the queue remains
the main surface, the side panel answers what the selected item is, and the
primary action is visually obvious without multiple competing green buttons.
Filters stay visible as applied chips; empty filtered results offer Clear filters.

## Interaction polish

- Buttons: one primary action per task region. Use verbs with objects, stable
  width during pending state, immediate press feedback and a durable receipt.
- Inputs: labels above values, helpful examples outside the field, units nearby,
  inline errors plus a focusable summary. Read-only fields look different from
  disabled controls.
- Lists: hover is subtle, selection is persistent and visible, keyboard focus
  is separate from selection. Preserve position when opening and closing details.
- Details: small previews use a panel; long or sensitive work uses a full page.
  Provide Open full page instead of cramming a complex form into a narrow drawer.
- Filters: show count and applied values. Support saved personal views without
  implying access to a larger population. Preview result changes before bulk action.
- Search: labeled people/work/action groups, clear matching, keyboard access,
  recent authorized items and a useful no-result state.
- Feedback: small non-blocking success notice plus updated state. Persistent
  errors stay next to the affected object. Do not toast a critical failure away.
- Motion: proposed 120–160ms state transitions and 180–220ms panel transitions;
  no springy motion, decorative parallax or moving table rows during a decision.
  Honor reduced motion and do not animate status as a substitute for text.
- Loading: reserve space to prevent jumps. Skeletons echo the real layout;
  avoid showing zeros or completion badges before data is known.
- Notifications: readable priority, source, time and next action. Group repeated
  alerts without hiding unresolved work.
- Customization: allow layout, approved accent choices, density, navigation
  favorites and component options. Protect contrast, focus, status semantics,
  required disclosure and the primary action hierarchy.

Comfortable touch controls target 44px as a product choice. WCAG 2.2 AA's
minimum target-size criterion is 24px with defined exceptions; compact views
must still satisfy sizing or spacing requirements, not merely look dense.
See [W3C target-size guidance](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html).

Modal dialogs contain keyboard focus and return it appropriately when closed;
full-page views do not trap focus. Initial focus depends on content and risk,
not a blanket rule to focus the approval button.
See [W3C modal dialog pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/).

## Visual review gate

Before treating a page as implementation-ready, review it at normal size, narrow
width, large text and both densities. Check hierarchy, alignment, readable
contrast, action discoverability, long content, every control state and realistic
empty/error conditions. Then test task comprehension with representative users.

Boards 18 and 19 are the strongest references for visual craft. The earlier
boards explore layout and feature coverage; their remaining raster wording,
fixture details, active navigation, colors and proportions are not exact
implementation instructions. The interaction contract and this visual spec
control the build. See [review notes](design-review-notes.md) for explicit gaps.
