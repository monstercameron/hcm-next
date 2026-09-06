# HCM Next interaction design corpus

Status: design proposal for review, September 5, 2026. The approved Home remains
the visual reference. These boards and contracts specify behavior; they do not
claim implemented functionality or accessibility conformance.

## Authority and use

Read with [page inventory](page-design-inventory.md),
[frontend plan](../specs/production-frontend-and-page-composition.md) and
[product slice alignment](../specs/default-product-slice-alignment.md).
Existing runtime release gates remain controlling. This corpus supplies the
designs needed to implement shared components and the admitted Promotion
journey; later HR families validate the patterns before their own release.

The numbered boards are visual references. This document controls behavior and
fixture semantics when raster text is ambiguous. The local walkthrough is a
design review artifact; it neither authenticates nor sends business requests.

## Corrected scenario

All new boards use synthetic scenario time September 5, 2026. Prior concept
images remain historical visual explorations; their inconsistent dates,
counts and reporting lines must not become implementation fixtures.

| Record            | Contract                                                                                                                                            |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Northstar Group   | Fictional tenant; authorized North America population is 238 workers                                                                                |
| Maya Chen         | HR business partner with People Operations scope; not automatically payroll administrator or medical-case specialist                                |
| Alex Morgan       | Strategy manager; requester of Jordan's promotion                                                                                                   |
| Jordan Lee        | Current Senior Analyst, Strategy, New York; manager Alex Morgan; annual base USD 92,000                                                             |
| PR1042 revision 3 | Proposed Senior Manager, annual base USD 112,000, effective September 15, 2026; HR and compensation approvals required                              |
| Avery Patel       | Product Designer, Product; manager Elena Ruiz; coverage request for September 8; general users see coverage only                                    |
| Noah Williams     | Payroll Specialist, Chicago, manager Maya Chen; employee persona for clock, PTO, statement and preferences                                          |
| REQ301            | Open HR Operations Lead position; candidate Sam Rivera in interview stage                                                                           |
| OP204             | One reconciliation operation with two field differences; never counted as two transactions                                                          |
| Insights example  | 26 distinct transactions: 18 complete, 6 in progress, 2 requiring reconciliation; mutually exclusive reporting buckets defined for this report only |

Approval screens show revision 3 still proposed. Tracking examples explicitly
advance the scenario after approvals. A future-effective locally committed
change can be scheduled while external verification remains pending. Current
worker facts remain effective-as-of facts, not the contents of a proposal.

## Foundations

Use [Visual language and UI craft](visual-language.md) for the refined visual
tokens, type scale, geometry, density, focus treatment and page-family hierarchy.
Its specifications supersede the exploratory sizes below. Boards 18 and 19
are the visual-craft references; the behavior tables here remain controlling.

Proposed tokens: brand/action green `#006B57`, hover `#005344`, ink `#102238`,
canvas `#FAFAF7`, card `#FFFFFF`, selection `#EAF3EF`, border `#D5DDD8`, muted text
`#526171`, warning text `#925400`, error `#B42318`. Verify actual foreground and
background pairs in implementation; decorative borders are not text contrast.

Use a self-hosted humanist sans-serif such as Source Sans 3 if licensed and
admitted, with a system sans-serif fallback. Type sizes: 14 metadata, 16 body,
20 section, 28 page title, 36 public headline baseline; relative units and user
zoom remain effective. Use a 4px spacing grid with 8/12/16/24/32px common gaps,
8px controls and 12px containers. Desktop sidebar starts at 240px and collapses
when space requires. Comfortable controls target 44px height; dense tables
retain usable separate action targets.

Selection uses sage plus outline or indicator. Warning is a status, not a
selected-row color. Green brand/action styling does not imply a completed
transaction. Statuses always include text and an icon when useful.

| Component         | Required states and behavior                                                                                                                                       |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Button            | default, hover, keyboard focus, pressed, unavailable with reason, pending; keep label stable while pending                                                         |
| Text/select field | label, helper, required, optional, empty, value, error, read-only; absent value differs from zero                                                                  |
| Money/date field  | explicit currency and annual/hourly basis; date picker plus typed entry; local effective date distinct from timestamp                                              |
| Table             | sort, filter, selection, empty, loading, permission reduction; selection survives only while record remains authorized                                             |
| Tabs              | content navigation with stable URLs where appropriate; workflow steps are a separate component                                                                     |
| Drawer            | preview only or full task explicitly identified; close restores focus to launching row                                                                             |
| Dialog            | labelled purpose, initial focus, contained keyboard navigation, Escape behavior, return focus; no implicit destructive Enter                                       |
| Notification      | informational versus action-required; transient toast never the only receipt                                                                                       |
| Form save         | unchanged: Save unavailable; dirty: Save enabled; saving: prevent duplicate request; success: saved version/time; failure: preserve entered values and offer retry |
| Status            | Proposed, Awaiting review, Approved, Scheduled, Committed, Verified, Needs attention; domain-specific dimensions remain separate                                   |

## Role and navigation design

Employee Home prioritizes shift, timecard, PTO, pay, personal profile and assigned
work. Manager Home prioritizes team coverage, decisions, staffing and team
changes. Specialist Home uses queues and exceptions. The same shell filters
destinations and actions for the principal; hidden areas do not leave numerical
badges or empty navigation headings.

Payroll and recruiting workbenches need directly pinnable destinations within
the permitted shell. Admin owns their configuration, not the default entry for
daily payroll or recruiting operations. Test whether users find these through
Home shortcuts, My Work saved views and the Action Finder before freezing IA.

Search supports people, work and permitted actions with labelled result types,
keyboard navigation, recent authorized results and no-result recovery. Back
from a result restores search query and position. A selected worker opens a
preview; View profile opens the canonical object route. Deep links reauthorize
before rendering and preserve the intended return location after sign-in.

Changing organization or delegated context clears sensitive view state,
rechecks permission, warns about unsaved work, and refetches content. Search,
counts and notification previews are scoped using the same context.

## Promotion journey

| Stage    | Person sees and does                                                      | Durable/semantic outcome                                              | Recovery                                                                    |
| -------- | ------------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| Discover | Search Jordan or choose Promote worker                                    | Authorized query; no mutation                                         | No matches: change query; unavailable: safe generic message                 |
| Profile  | Current job and proposed requests in separate regions                     | Effective-as-of facts with freshness                                  | Stale required facts block new simulation until refreshed                   |
| Draft    | Enter title, annual base, effective date, reason                          | Saved draft revision; no domain change                                | Keep entered values on validation/save failure                              |
| Simulate | Diff, policy findings, uncertainty, approvals and source times            | Simulation result bound to exact inputs; no material business effects | Findings link to fields; changed source requires new simulation             |
| Review   | Exact proposal, consequences and required reviewers                       | Submit creates immutable proposal revision and workflow admission     | Editing material values invalidates prior review and simulation             |
| Decision | Assigned reviewer sees exact revision and evidence                        | Approval/return/rejection receipt bound to proposal                   | Changed revision requires new review; another claimant makes item read-only |
| Track    | Decision, execution, effective time, external consistency and obligations | Current authorized lifecycle projection                               | Unknown outcome prompts status lookup, not blind resubmission               |
| Repair   | Expected/observed diff and targeted repair proposal                       | Separate governed repair with approval and verification               | Unresolved mismatch retains attention and owner                             |

Draft footer: Save draft, Simulate change, Cancel. Review footer: Back to
details, Submit for approval. Decision footer: Approve proposal, Return for
changes, Reject. Return requires actionable explanation; rejection requires a
reason. Neither action appears in list-row bulk actions. Approval confirmation
repeats person, proposal revision, amount/basis and date. A reviewer cannot
approve their own request when the controlling policy forbids it.

Work-item claiming is explicit when exclusive ownership is needed. Before claim,
show availability; after claim, show owner and release action where supported.
Keep personal Drafts separate from Assigned counts; overlapping queue tabs must
be labelled as views rather than added together as independent totals.

Tracking never compresses approval, local commit, external acceptance and
verified outcome into one green Complete badge. The page explains the remaining
work and responsible party in business language. Technical evidence IDs belong
in an optional details panel when useful for support.

## Recovery and access state matrix

| Condition                   | Presentation                                                  | Allowed next action                                        |
| --------------------------- | ------------------------------------------------------------- | ---------------------------------------------------------- |
| Initial loading             | Bounded skeleton plus loading announcement                    | Cancel navigation; no business action                      |
| Empty assigned queue        | All caught up, scope visible                                  | Browse permitted actions                                   |
| Filter yields nothing       | No matches, selected filters visible                          | Clear filters                                              |
| Partial panel failure       | Failed panel explanation; other data retains freshness labels | Retry affected query                                       |
| Network lost                | Last confirmed update time; unsaved draft state explicit      | Reconnect and refetch                                      |
| Submit response lost        | Outcome not yet confirmed                                     | Check request status using same request identity           |
| Proposal changed            | New revision and material diff                                | Review current revision                                    |
| Permission revoked          | Remove protected content; generic explanation                 | Return to authorized work                                  |
| Another person claimed task | Read-only state with safe ownership explanation               | Return to queue                                            |
| Session nearing expiry      | Accessible warning with work preservation explanation         | Extend session if allowed                                  |
| Session expired             | Saved/unsaved status distinguished                            | Sign in and reauthorize; never automatically replay action |
| Unsaved navigation          | Save, discard or continue editing                             | Explicit selection                                         |
| Provider unavailable        | Neutral sign-in error and support path                        | Retry approved identity path                               |

Organization discovery uses configured tenant routing, not a public membership
lookup. Organization ID is an alternate configured route. MFA happens through
the admitted identity provider; step-up names the protected action. Recovery
must work with password managers, paste, keyboard and assistive technology.

## Responsive, accessibility and localization

At narrow widths the shell becomes a labelled Menu control, list-detail becomes
list-to-full-detail navigation, form summaries stack below the current step,
and sticky actions reserve space so they never obscure content. User-entered
values survive viewport and enhancement changes.

Tables retain necessary two-dimensional scrolling with clear headers and an
object-detail alternative. Organization chart has an equivalent outline with
relationship type, parent and effective-date semantics. Drag interactions have
Move up/down and choose-parent alternatives. Focus is predictable after
validation, navigation, modal close and live updates. Live refresh never steals
focus or silently reorders the row being acted on.

Design fixtures include 320px reflow, 390px mobile, 768px tablet, wide desktop,
enlarged text, 200/400% zoom, long names, long translations, RTL, mixed scripts,
decimal/currency formats and time-zone boundaries. Reduced motion affects
animation only. Screen-reader announcements summarize results without reading
every background refresh. Image boards do not prove these behaviors; inspect
them in a semantic browser prototype before release.

## Customer composition

Experience Studio has an outline, live preview and inspector. An author chooses
purpose, audience, floorplan, allowed data binding and optional content. Required
identity, scope, security, confirmation and error regions are locked. Optional
regions can be reordered through keyboard controls. Customer pages inherit a
versioned platform definition; inheritance and local overrides are visible.

Preview covers role, tenant scope, mobile and empty/error states using synthetic
fixtures. Preview-as never grants access to another person's live data. Publish
requires validation, any mandated review and an immutable version; adoption and
rollback affect definitions, not previously executed business transactions.
An upgrade conflict opens a before/after diff and a repair task. Personal
preferences cannot override required fields, authority or evidence.

## Page data and action contracts

| Surface           | Reads                                                                 | Commands                                           | Confirmation visible to user                         |
| ----------------- | --------------------------------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------- |
| Home/My Work      | Authorized attention and work summaries, counts, watermarks           | Claim/open delegated to owning services            | Current owner and task state                         |
| Worker profile    | People-owned effective revisions, relationships and permitted actions | Start intent only through public capability        | Draft identity and current stage                     |
| Draft/simulation  | Draft revision, typed inputs, source versions, policy findings        | Save draft, simulate, submit                       | Saved revision, simulation context, proposal receipt |
| Decision          | Work item, exact proposal and safe evidence                           | Claim, approve, return, reject                     | Decision receipt and remaining reviews               |
| Tracker/repair    | Lifecycle dimensions, operation observations, differences             | Propose/approve/execute targeted repair            | Local result separate from external verification     |
| Report            | Semantic definition, authorized population, time window, lineage      | Preview, generate, schedule/export when authorized | Artifact/delivery state and retained access scope    |
| Experience Studio | Definition versions, safe bindings and preview fixtures               | Validate, request review, publish                  | Version, effective scope and publication result      |

Every displayed value declares canonical field, semantic owner, effective/as-of
context, source version, disclosure disposition and missing/stale behavior.
Actions bind capability, typed input, expected version, authorization context,
idempotency and exact completion/error response. Existing Protobuf/domain and
storage manifests supply these bindings; UI code does not name arbitrary SQL.
WebSocket hints trigger authorized refetch. SSR and enhanced interactions retain
equivalent semantics and form values.

## Time, PTO and scheduling

Clock-in confirms only after the server records the event. Connection loss
shows unrecorded status; duplicate retries use the same event identity. Any
offline capture mode requires its own admitted contract and explicit unverified
state. Show shift time zone, breaks and correction route without hiding
unresolved events behind green success.

Noah requests September 14–15, 2026: 16 hours against 40 available gives 24
remaining, using the selected work calendar. Submission is pending approval,
not a balance deduction presented as final. Partial days, holidays, overlapping
requests, concurrent balance changes and insufficient balance have field-level
explanations. Manager coverage shows coverage gaps and permitted availability;
medical reasons remain in the specialist case context.

## Recruiting and payroll

REQ301 candidate pipeline counts are 4 applied, 2 screening, 1 interview and 0
offer. Candidate profile separates evidence from reviewer assessment. Interview
invitation for Sam Rivera is September 10, 2026, 2pm Eastern / 1pm Central;
recipient review and Send are explicit. Stage changes retain history. Declines,
withdrawals, consent expiry and schedule conflicts require dedicated states.

Payroll PAY0915 has 238 workers, 236 ready and 2 exceptions. Resolve exceptions
before run approval when required; calculation, approval, transmission,
acceptance and settlement remain distinct. Large selections show scope and
excluded records before any batch action. Noah's illustrative statement:
gross USD3,000 less taxes600 and benefits150 equals net2,250. Delivery and
payment state are independent; correction preserves the original statement.

## Workflow and AI design

Template origin and automation mode are separate dimensions. A default template
can be copied into a custom definition; either may contain AI assistance under
the same permitted capability contracts. Workflow Studio separates definition
draft, simulation, publication and actual execution.

AI assistance shows source references, draft output, uncertainty when relevant
and a human edit/accept/reject path. Bounded autonomy names permitted actions,
scope, duration, limits, escalation and pause control. The example permits draft
summaries and review-task creation; it does not delegate approvals, pay changes,
candidate rejection or other employment decisions. Pausing prevents new
actions and reports already in-flight effects separately. Activity history
records actor, version, inputs, decisions and results.

## People analysis, reports and communications

Retention analysis begins with cohorts, measure definitions, time horizon,
source quality and disclosure thresholds. An individual support view may show
employee-stated growth interests and suggested conversations; it must not
present an unsupported personal flight-risk percentage as fact. Any future
individual prediction requires separate validation, purpose and access design.
Employee experience distinguishes reported feedback, observed process friction,
calculated measures and AI interpretation. DX is provisionally interpreted as
digital employee experience; the label remains unconfirmed.

The example voluntary exit rate is 6 exits divided by average headcount240,
or 2.5%, with population and interval disclosed. This denominator is distinct
from the current authorized directory count238. Report previews use semantic
fields, consistent totals and data-table alternatives. Export, schedules and
recipient authorization are separately reviewed; scheduled delivery rechecks
access at execution. Small cohorts show suppression rather than misleading zero.

Email composition distinguishes draft, AI draft, reviewed, queued, sent,
delivered and read where evidenced. Recipient, template, time zone and protected
content are reviewed before send. Notifications contain minimal safe attention
text and link to an authenticated object. Acknowledging an alert is not resolving
its cause. Quiet hours affect permitted attention delivery; mandatory notices
follow the owning policy. Failed delivery exposes owner and safe redrive route.

## Build readiness and review

The component set and Promotion journey can move into implementation after
review of semantic tokens, form behavior, role disclosure and stage transitions.
Use a clickable walkthrough to test finding Jordan, submitting a draft,
returning it for changes, reviewing a revised proposal and investigating a
pending external result. Observe employee, manager and specialist users; record
task success, wrong turns, terminology confusion and recovery success.

Before shipping, connect a semantic browser prototype to deterministic fixtures
and verify keyboard, focus, reflow, assistive technology, stale-state handling
and exact service outcomes. Time/PTO, recruiting, payroll, AI, reporting and
communications boards guide later product slices; each requires its own domain
validation. No checkbox or generated image substitutes for that evidence.
