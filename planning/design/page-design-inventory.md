# HCM Next Page Design Inventory

## Interaction design corpus — September 2026

The [interactive workspace slice](hcm-brandable-workspace.html) is the current
HTML/CSS example for product styling and customer customization. Its
[source and validation notes](demo/README.md) describe the bounded behavior.

The [21 design boards](interaction-board-index.md),
[behavior and page contracts](interaction-design-corpus.md), and
[local review gallery](review.html) extend these concept pages with role variants,
the connected Promotion journey, recovery, responsive behavior, page composition,
HR workbenches, AI controls and communications. They are proposals for review;
they do not mark implementation or usability validation complete.

The corrected September 2026 fixture in the interaction contract is authoritative
for new design work. Existing concept images below are preserved as historical
visual references, including any old dates or inconsistent workflow labels.

## Purpose

This inventory sequences production page design from public discovery through
authenticated work and administration. It is a visual-design queue, not a new
route or capability authority. Every authenticated page must resolve through
the contracts in:

- [Production Frontend and Governed Page Composition](../specs/production-frontend-and-page-composition.md)
- [Default Product Slice Alignment](../specs/default-product-slice-alignment.md)
- [Experience, Dynamic UI and Branding](../specs/experience-ui-and-branding.md)

## Shared visual direction

- Calm, human and operational rather than ornamental.
- Deep ink typography on warm neutral surfaces with restrained sage/teal and
  semantic amber/red accents.
- Clear page identity, authority context, freshness and next action.
- Comfortable spacing for occasional users; an explicitly denser workbench
  mode for specialists.
- Current, proposed, simulated, observed and completed information remain
  visually distinct.
- Product truth is shown through realistic workflow states, not generic charts.
- Semantic HTML, keyboard order, zoom, contrast, reduced motion and narrow
  viewport behavior are designed with the desktop composition.

## Historical concept scenario — superseded for new designs

The initial product mockups used this synthetic scenario. For new boards, use
the corrected fixture in [the interaction corpus](interaction-design-corpus.md)
instead; do not copy the old medical disclosure or interpret pending changes as
already scheduled.

| Context                 | Canonical value                                                                                                                        |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| Organization            | Northstar Group                                                                                                                        |
| Principal               | Maya Chen, People Operations, North America, acting as herself                                                                         |
| Jordan Lee              | Senior Analyst in Strategy, manager Alex Morgan                                                                                        |
| Jordan's proposal       | Promotion to Senior Manager; `$92,000 -> $112,000`; effective July 1, 2025; ready for review; three policy checks passed; no conflicts |
| Avery Patel             | Product Designer; medical leave coverage decision; begins September 8, 2025                                                            |
| Payroll operation       | Payroll connector has two reconciliation differences requiring investigation                                                           |
| Noah Williams           | Payroll Specialist; worker-details draft; address update recently verified                                                             |
| Elena Ruiz              | VP, Product; manager change recently completed                                                                                         |
| Visual status semantics | green = permitted/healthy/complete; amber = review/wait; red = investigation/material problem; gray = neutral/draft/unavailable        |

Mockup dates are scenario time, not current production time. Screens that show
effective/as-of state must remain internally chronological.

## Design sequence

### 1. Public and access surfaces

| Order | Page                           | Primary job                                             | Floorplan/state      | Design status |
| ----- | ------------------------------ | ------------------------------------------------------- | -------------------- | ------------- |
| 01    | Public landing                 | Understand the product promise and choose a next step   | Marketing landing    | Concept v1    |
| 02    | Sign in                        | Enter through the correct trusted identity path         | Focused access       | Concept v1    |
| 03    | Organization discovery         | Resolve company/tenant without revealing membership     | Focused access       | Queued        |
| 04    | SSO and authentication handoff | Continue through the configured identity provider       | Focused access       | Queued        |
| 05    | Step-up verification           | Satisfy stronger assurance for a sensitive action       | Guided task          | Queued        |
| 06    | Session expired/interrupted    | Recover safely without losing a governed draft          | Recovery             | Queued        |
| 07    | Access denied/not found        | Explain the safe next step without resource disclosure  | Error/recovery       | Queued        |
| 08    | Request a demo                 | Qualify an organization and arrange contact             | Public form          | Queued        |
| 09    | Platform overview              | Understand the intent-to-outcome operating model        | Marketing editorial  | Queued        |
| 10    | Solutions overview             | Find role- and workflow-relevant value                  | Marketing collection | Queued        |
| 11    | Security and trust             | Evaluate architecture, governance and evidence controls | Trust center         | Queued        |
| 12    | Resources                      | Find documentation, guides and product material         | Marketing collection | Queued        |

### 2. Shared authenticated product

| Order | Page                          | Primary job                                                   | Floorplan       |
| ----- | ----------------------------- | ------------------------------------------------------------- | --------------- |
| 13    | Home                          | See relevant attention, recent work and safe starts           | Launch          |
| 14    | My Work                       | Triage assigned tasks, approvals, drafts and tracked requests | Collection      |
| 15    | Action Finder                 | Discover an authorized action by human-language goal          | Launch/search   |
| 16    | Global search results         | Find permitted people, work and configuration                 | Collection      |
| 17    | Notification/attention detail | Understand why attention is required and continue safely      | Guided detail   |
| 18    | Draft Center                  | Resume, compare or discard governed drafts                    | Collection      |
| 19    | User preferences              | Configure locale, timezone, density and accessibility         | Object/settings |
| 20    | Delegated acting context      | Enter, inspect and leave an authorized representation         | Guided control  |

### 3. People and governed change

| Order | Page                         | Primary job                                              | Floorplan        |
| ----- | ---------------------------- | -------------------------------------------------------- | ---------------- |
| 21    | People directory             | Find an authorized worker without enumeration leakage    | Collection       |
| 22    | Worker overview              | Understand a worker and discover allowed actions         | Object           |
| 23    | Worker employment            | Inspect assignments, job, manager and effective history  | Object section   |
| 24    | Worker pay and benefits      | Inspect permitted compensation and benefit summaries     | Object section   |
| 25    | Worker activity              | Inspect authorized changes and evidence chronology       | Timeline         |
| 26    | Intent Workspace: draft      | Collect a proposed workforce change                      | Intent workspace |
| 27    | Intent Workspace: simulation | Compare impact, policy, conflicts and uncertainty        | Intent workspace |
| 28    | Intent Workspace: review     | Confirm exact proposal and consequences                  | Intent workspace |
| 29    | Decision page                | Approve, reject or return proposal-bound work            | Decision         |
| 30    | Request status               | Track lifecycle, waiting conditions and next actions     | Monitor          |
| 31    | Evidence timeline            | Explain request, decisions, execution and reconciliation | Timeline         |
| 32    | Correction/repair start      | Begin an authorized correction without rewriting history | Guided task      |

### 4. Organization, work and workflow families

| Order | Page                           | Primary job                                                  | Floorplan        |
| ----- | ------------------------------ | ------------------------------------------------------------ | ---------------- |
| 33    | Organization explorer          | Navigate effective-dated organization relationships          | Object/explorer  |
| 34    | Position detail                | Understand capacity, occupancy, vacancy and planned change   | Object           |
| 35    | Headcount request              | Propose and review position capacity                         | Intent workspace |
| 36    | Hire and onboarding workspace  | Coordinate offer-to-worker activation                        | Intent workspace |
| 37    | Time and leave hub             | See balances, requests, exceptions and team coverage         | Launch           |
| 38    | Leave request                  | Request ordinary or protected leave with appropriate privacy | Guided task      |
| 39    | Compensation cycle workbench   | Manage a frozen population, budget and proposals             | Expert workbench |
| 40    | Benefits enrollment            | Compare plans and submit an evidence-complete election       | Guided task      |
| 41    | Growth home                    | Navigate goals, feedback, reviews, skills and learning       | Launch           |
| 42    | Performance review             | Complete a review while separating evidence and judgment     | Guided task      |
| 43    | HR help hub                    | Find guidance or start the correct service request           | Launch/search    |
| 44    | Participant case status        | Track a case without exposing restricted internal work       | Monitor          |
| 45    | Specialist Case Center         | Triage and resolve confidential case work                    | Expert workbench |
| 46    | Exit and offboarding workspace | Coordinate decision, obligations and reconciliation          | Intent workspace |

### 5. Insights, configuration and operations

| Order | Page                         | Primary job                                            | Floorplan          |
| ----- | ---------------------------- | ------------------------------------------------------ | ------------------ |
| 47    | Insights home                | Find certified analysis and understand freshness       | Launch             |
| 48    | Report catalog               | Select an authorized certified or customer report      | Collection         |
| 49    | Analysis view                | Explore governed metrics, lineage and suppression      | Analysis           |
| 50    | Admin home                   | See permitted configuration and operational attention  | Launch             |
| 51    | Configuration center         | Inspect versioned configuration and publication state  | Collection/object  |
| 52    | Policy Studio                | Simulate, review and publish governed policy           | Expert workbench   |
| 53    | Experience Studio            | Compose, preview, validate and publish a customer page | Composition studio |
| 54    | Brand and localization       | Manage safe tokens, assets, translations and scope     | Object/settings    |
| 55    | Integration operations       | Inspect connector work, observations and redrive state | Monitor/workbench  |
| 56    | Reconciliation queue         | Triage material mismatches and deadlines               | Collection         |
| 57    | Repair workbench             | Diagnose, propose, approve, execute and verify repair  | Expert workbench   |
| 58    | Audit/evidence inspector     | Trace provenance and retained evidence safely          | Expert workbench   |
| 59    | Release and migration status | Inspect version, rollout, skew and recovery readiness  | Monitor            |

## Broad HR capability routing

The stable shell does not grow one menu item per HR module. Capabilities are
routed by human job, authorization and operating mode:

| Stable destination | Capabilities reached from it                                                                                                                                  |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Home               | personal profile, schedule, time clock, timecard, PTO, pay, benefits, growth, recruiting/onboarding tasks and role-relevant starts                            |
| My Work            | approvals, assignments, drafts, workflow runs, hiring tasks, payroll exceptions, time corrections, scheduled reports, communications and alerts               |
| People             | worker profiles, employment, assignments, schedules, time, compensation, benefits, skills, experience, documents and authorized people analysis               |
| Organization       | organization graph, positions, requisitions, headcount, schedules, workforce plans, scenarios, costs and effective-dated changes                              |
| Insights           | workforce metrics, scheduling analysis, employee experience, retention, recruiting, time, payroll, workflow effectiveness and governed reports                |
| Admin              | default/custom workflows, AI controls, policies, security, page composition, payroll/time/recruiting configuration, integrations and communication operations |
| Help               | product guidance, HR service intake, cases, support requests and system status                                                                                |
| Settings           | profile, preferences, notifications, accessibility, sessions, delegated access and personal work defaults                                                     |

## Expanded HR page inventory

These pages specialize the shared floorplans without becoming new permanent
sidebar destinations.

| Order | Page                         | Route from            | Primary purpose                                                     |
| ----- | ---------------------------- | --------------------- | ------------------------------------------------------------------- |
| 60    | Employee profile             | Home / People         | Maintain personal identity, contact and preference facts            |
| 61    | Work schedule                | Home / People         | Understand assigned shifts, availability and effective schedule     |
| 62    | Time clock                   | Home                  | Clock in/out with location, device and exception transparency       |
| 63    | Timecard                     | Home / My Work        | Review hours, breaks, corrections and approval state                |
| 64    | PTO balance and calendar     | Home                  | Understand balances, plans, holidays and team coverage              |
| 65    | PTO request                  | Home / Action Finder  | Request time away and review exact balance/coverage impact          |
| 66    | PTO approval and coverage    | My Work               | Decide a request with policy and team context                       |
| 67    | Payroll home                 | Home / Admin          | Orient employees or specialists to current payroll work             |
| 68    | Pay statement                | Home / People         | Explain earnings, deductions, taxes, net pay and provenance         |
| 69    | Payroll run workbench        | My Work / Admin       | Validate, calculate, approve and transmit a bounded pay run         |
| 70    | Payroll exceptions           | My Work               | Triage calculation, input and delivery problems                     |
| 71    | Payroll reconciliation       | My Work / Admin       | Compare expected and observed payroll outcomes                      |
| 72    | Recruiting home              | Home / People         | Orient recruiters and hiring managers to active hiring work         |
| 73    | Requisition collection       | People / Organization | Manage authorized openings and hiring demand                        |
| 74    | Candidate pipeline           | People                | Move candidates through configured stages without losing consent    |
| 75    | Candidate profile            | People                | Inspect candidate evidence, consent, history and allowed actions    |
| 76    | Interview scheduling         | My Work / People      | Coordinate availability, panels and candidate communication         |
| 77    | Offer workspace              | My Work               | Draft, simulate, approve, issue and observe an offer                |
| 78    | Onboarding workspace         | My Work / People      | Coordinate tasks, documents, provisioning and worker activation     |
| 79    | Workflow catalog             | Admin                 | Discover default, customer and AI-enabled workflow definitions      |
| 80    | Workflow definition          | Admin                 | Inspect versions, owners, triggers, steps and evidence requirements |
| 81    | Workflow Studio              | Admin                 | Compose and validate a governed custom workflow                     |
| 82    | AI-assisted workflow         | My Work / Admin       | Review AI-drafted summaries, recommendations or next steps          |
| 83    | Autonomous workflow control  | Admin                 | Govern delegated AI actions, limits, checkpoints and kill switch    |
| 84    | Workflow run monitor         | My Work / Admin       | Track execution, waits, decisions, effects and obligations          |
| 85    | Workflow exception workbench | My Work / Admin       | Diagnose, repair or escalate a blocked workflow                     |
| 86    | Worker analysis              | People / Insights     | Explore authorized schedule, growth and experience context          |
| 87    | Scheduling analysis          | Insights              | Assess coverage, overtime, fatigue and demand alignment             |
| 88    | Employee-experience analysis | Insights              | Understand journey friction, sentiment provenance and outcomes      |
| 89    | Retention signal review      | People / Insights     | Review explainable uncertainty and supportive interventions         |
| 90    | Retention cohort analysis    | Insights              | Analyze aggregated stay/leave patterns with disclosure controls     |
| 91    | Report builder               | Insights              | Compose an authorized semantic report without arbitrary SQL         |
| 92    | Report detail                | Insights              | Inspect results, definition, freshness, lineage and suppression     |
| 93    | Scheduled reports            | Insights / My Work    | Configure governed delivery, recipients and effective schedule      |
| 94    | Report export                | Insights              | Produce a classified, authorized and evidenced export               |
| 95    | Communications center        | Admin                 | Govern email, notification and alert operations                     |
| 96    | Email composer               | My Work / Admin       | Draft, review and send contextual communication safely              |
| 97    | Message-template library     | Admin                 | Version localized email, notification and letter templates          |
| 98    | Notification center          | Home / My Work        | Triage attention without exposing protected content                 |
| 99    | Alert center                 | My Work / Admin       | Investigate operational, policy and deadline alerts                 |
| 100   | Delivery log                 | Admin                 | Observe message delivery, acknowledgement, failure and redrive      |
| 101   | Communication preferences    | Settings              | Control permitted channels, quiet hours and purpose consent         |
| 102   | AI and communication audit   | Admin                 | Trace AI input/output, human review, delivery and evidence          |

## Workflow automation classes

| Class                  | Product behavior                                                                        | Required control                                                                                   |
| ---------------------- | --------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Default workflow       | Platform-owned deterministic workflow shipped with a domain pack                        | Versioned definition, policy binding and upgrade compatibility                                     |
| Custom workflow        | Customer-composed workflow using admitted steps and capabilities                        | Validation, simulation, review, publication and rollback                                           |
| AI-enabled workflow    | AI summarizes, drafts, classifies or recommends while a human/domain capability decides | Source citations, uncertainty, editable output and explicit human action                           |
| AI-controlled workflow | AI may invoke a bounded capability under a time-limited delegation                      | Exact grant, budgets, prohibited actions, checkpoints, kill switch, revalidation and full evidence |

An AI signal never becomes an employment fact or adverse decision by itself.
Individual retention or experience analysis must expose sources, uncertainty,
authorized purpose, prohibited uses and a human-support route; cohort views use
minimum-population and disclosure controls.

## Landing-page concept v1

![HCM Next public landing page concept](mockups/public-landing-v1.png)

The first concept establishes the public visual language and positioning:

- promise: **Make every people decision explainable**;
- product proof: a realistic current-versus-proposed Promotion review;
- operating sequence: draft, simulate, review and complete;
- trust proof: policy checks and request-to-outcome evidence;
- primary conversion: Request a demo;
- existing-customer route: Sign in.

## Enterprise sign-in concept v1

![HCM Next enterprise sign-in concept](mockups/enterprise-login-v1.png)

The first access concept keeps authentication deliberately focused:

- work-email discovery routes the principal to organization-controlled SSO;
- organization ID provides a safe alternate route;
- no consumer identity providers or premature password collection;
- account-existence privacy is explicit;
- organization control, MFA and purpose-aware access establish enterprise trust;
- visible labels, focus treatment and large controls establish the accessibility
  baseline.

## Authenticated Home concept v1

![HCM Next authenticated Home concept](mockups/authenticated-home-v1.png)

The first authenticated concept establishes the production shell and default
attention hierarchy:

- stable navigation exposes Home, My Work, People, Organization, Insights and
  Admin without turning every HCM function into a top-level module;
- tenant, role scope and representation context remain visible;
- work needing human attention dominates the page;
- Start an action uses outcome language instead of exposing workflow internals;
- amber, red and green communicate review, investigation and completion states;
- recent outcomes and freshness make the system observable without decorative
  dashboard charts.

## Primary navigation concepts v1

### My Work

![HCM Next My Work concept](mockups/authenticated-my-work-v1.png)

### People

![HCM Next People concept](mockups/authenticated-people-v1.png)

### Organization

![HCM Next Organization concept](mockups/authenticated-organization-v2.png)

### Insights

![HCM Next Insights concept](mockups/authenticated-insights-v1.png)

### Admin

![HCM Next Admin concept](mockups/authenticated-admin-v1.png)

### Help

![HCM Next Help concept](mockups/authenticated-help-v1.png)

### Settings

![HCM Next Settings concept](mockups/authenticated-settings-v1.png)

The expanded corpus now includes concepts for Workflow Studio, Time/PTO,
Recruiting, Payroll, worker profile/analysis, communications and recovery.
For the latest visual direction, start with [Visual language](visual-language.md)
and [Customer branding and layout](customer-branding-and-layout.md). Boards 18–21
emphasize component craft, workbench hierarchy, theme variants and configuration.
These are design concepts, not completed production implementations.
