# Interaction design board prompts

Generated September 5, 2026 with the built-in image generation tool. All records are synthetic. These prompts request interaction concepts, not implemented product behavior. Reference for every board: `mockups/authenticated-home-v1.png`.

## Shared prompt prefix

Use case: ui-mockup. High-fidelity HCM Next enterprise UI design board, landscape 1800x1200 or larger for legibility. The reference is the approved visual style and desktop shell. Preserve warm off-white, deep ink, flat money-green, pale sage selection, fine borders, readable humanist type, consistent spacing. Desktop shell: HCM Next, Northstar Group, sidebar Home/My Work/People/Organization/Insights/Admin and Help/Settings; top search and Maya Chen MC except employee views use Noah Williams NW. Keep secondary panels uncluttered; prioritize legible key labels over verbose text. All data synthetic, design scenario Sep5 2026 unless a stage label explicitly advances it. Jordan current Senior Analyst, manager Alex Morgan, Strategy; PROPOSED Senior Manager annual base USD92000->USD112000 effectiveSep15 2026. Avery reports to Elena Ruiz, not Alex. Pending proposals never called scheduled. No medical details in general lists. No browser chrome, no watermark, no decorative 3D, no photo. Board can simplify shell for multi-screen readability. This is an interaction concept, not proof of implemented behavior. Requested board:

## 01-foundations: Design system and component states

A large legible design-system board. Sections: typography scale; swatches with labels Brand green #006B57, Ink #102238, Surface #FAFAF7, Selection #EAF3EF, Warning #925400, Error #B42318; buttons default/hover/focus/disabled/loading; labelled text inputs default/error/read-only; status chips Proposed, Awaiting review, Scheduled, Completed, Needs attention; table row selection always sage independent of amber status. Include settings save-state strip Unchanged / Edited / Saving / Saved / Failed. Flat green buttons. Use clear specimens, not tiny prose.

## 02-role-homes: Employee and manager Home

Two substantial side-by-side home screen mockups. Employee Noah Williams: Home/My Work/People menu, Today's shift 9 AM–5 PM, Clock in, Timecard, Request PTO, Pay statement, My profile, one assigned task. Manager Alex Morgan: team coverage, Jordan Lee promotion PROPOSED Sep 15 2026, Review request button, open position; Avery Patel coverage dates only in cross-team coverage, no medical details. Organization Northstar Group in both. Explicit title above each Employee / Manager. Money green consistency.

## 03-worker-profile: Worker profile and action discovery

One full screen worker profile Jordan Lee, Senior Analyst, Strategy, manager Alex Morgan, New York, Active. Breadcrumb People / Jordan Lee. Sections Overview, Employment, Pay & benefits, Schedule, Growth, Documents, Activity. Selected Overview shows current job and proposed promotion separately: Proposed Senior Manager, USD 92,000 to 112,000 annual base, Effective Sep 15 2026, Awaiting review. Main button Start an action opens a small menu Promote worker / Change manager / Update details. Sidebar scope People Operations · North America. Confidential pay summary has Restricted label for this authorized HR viewer. Avoid portraying a proposal as committed.

## 04-promotion-draft: Promotion draft form

One full screen Promotion workspace, Jordan Lee. Stepper Details / Simulate / Review. Draft saved status. Main form labelled Title Senior Manager, Annual base pay 112000 USD, Effective date Sep 15 2026, Change reason Expanded leadership scope. Side summary Current role Senior Analyst / Annual base 92000 USD / Manager Alex Morgan. Scope North America. Footer Save draft secondary and Simulate change primary, Cancel link. Add readable inline error example as a separate small inset labelled Validation variant: Effective date is required. No approve button at draft.

## 05-simulation-review: Simulation and submission review

Two substantial side-by-side stages of same Promotion workspace. Simulation: Current/Proposed table Senior Analyst/Senior Manager, USD 92000/USD112000 annual base, effective Sep15 2026; three checks passed, no conflicts as of Sep5 2026 10:42AM, required approvals HR partner and compensation reviewer; badge Simulation — no changes applied. Review: proposal revision3, requester Alex Morgan, Jordan Lee, exact same values, explanatory consequences, primary Submit for approval, secondary Back to details. Clear distinction passed checks versus approval. Tight but readable text.

## 06-approval-decision: Proposal-bound approval decision

One full screen My Work / Promotion review for Maya Chen, assigned HR reviewer. Jordan Lee proposed Senior Manager USD112000 from USD92000 annual base, effective Sep15 2026, Revision3. Current/proposed comparison. Evidence links Role justification / Compensation policy. Checks evaluated 10:42AM. Decision area primary Approve proposal, secondary Return for changes, text Reject. Include inset confirmation dialog Approve revision3? Shows amounts/date, button Confirm approval. Subtext Other required reviews remain. No execution implication from one approval.

## 07-tracking-repair: Tracking and reconciliation repair

Two screens side by side in same shell. Left Promotion request PR1042 after approval: Decision Approved, Execution Committed, Effective Sep15 2026, External sync Needs attention, Obligations Open. Timeline Submitted / HR approved / Compensation approved / Change recorded / Payroll verification pending. Right Payroll reconciliation OP204: two field differences for this same transaction; expected annual base112000USD observed92000USD; expected effectiveSep15 observedSep16 2026. Human-readable source timestamps; proposed targeted repair, Compare evidence, primary Review repair, no blind Retry all. Visual labels distinguish local commit from external verification.

## 08-recovery-states: Failure and recovery pattern library

Six large readable UI panels in a 3 by2 design board. Loading skeleton with Loading work; Empty queue All caught up and Browse actions; Search empty No matching people and Clear filters; Connection lost Last updated10:42AM and Reconnect; Proposal changed Revision4 now available and Review changes, old approval disabled; Session expired Draft saved and Sign in to continue. Each panel includes concise next action, same green design. Never a success toast before durable outcome. Name panels clearly.

## 09-responsive-accessibility: Responsive and keyboard layouts

Three device-sized product screens on one board: 390px mobile Home for Noah with compact header, Menu control, Clock in, PTO and My Work; 768px tablet My Work list with selected Jordan and full-page-detail link; narrow desktop Promotion form at enlarged text size, clear labelled fields, visible green keyboard focus, stacked current/proposed summary. Bottom small focus-return diagram text Open review / Dialog / Return to trigger. No decorative phones, just flat viewport borders. Show comfortable touch controls and wrapped long text.

## 10-page-composition: Customer Experience Studio

One full desktop Admin / Experience Studio, draft Team home v2. Three panels: left accessible page outline Header locked / Attention locked / Team coverage / Quick actions / Report with up/down controls; center realistic live page preview using approved green shell; right inspector Audience Managers, Scope North America, data binding Team coverage, Visibility authorized audience. Top Preview as Manager and viewport switch Desktop/Mobile. Bottom Validation2 issues, issue missing alternative text; Publish disabled until resolved, Request review. One inline badge Platform required on locked regions. No arbitrary code editor.

## 11-time-pto-scheduling: Time clock PTO and scheduling

Three substantial mobile/tablet screens on board with consistent green. Noah time clock: Today's shift9AM–5PM, status Not clocked in, button Clock in, last recorded event Yesterday5PM; PTO request Sep14–15 2026, 16hours, available40hours after24hours, primary Review request; manager coverage calendar weekSep14 with Noah pending PTO and uncovered 2hour block, no medical labels. Small disconnected variant Clock event not yet recorded / Check connection. Distinguish request from approval.

## 12-recruiting: Recruiting candidate and interview journey

Two connected desktop workbench panels. Recruiting / Requisition HR Operations Lead REQ301, open role from org chart. Pipeline counts Applied4 Screening2 Interview1 Offer0; selected candidate Sam Rivera in Interview, no ranking score. Candidate side detail relevant experience, interview panel, consent document. Second pane Interview scheduling with candidate timezone America/Chicago and panel timezone America/New_York, proposed Sep10 2026 2PM Eastern /1PM Central, availability, button Review invitation. Explicit Invitation draft — not sent. No automated rejection.

## 13-payroll: Payroll workbench and employee statement

Two screens connected by Northstar payroll Sep1–15 2026. Specialist run PAY0915 Draft calculation, 238 workers, 2 exceptions, 236 ready, primary Review exceptions, Approve run disabled; one exception Jordan Lee proposed promotion effectiveSep15 requires verified input. Employee Noah pay statement clearly illustrative: Gross3000USD, Taxes600, Benefits150, Net2250, periodSep1–15, Payment pending, expandable line items and Report an issue. Flat green, no confusing net/gross. No send-money action on overview.

## 14-workflows-ai: Workflow Studio and AI controls

One large desktop Admin / Workflow Studio for Promotion v4 draft. Left definition outline trigger / validate / AI summarize / HR review / Compensation review / Commit / Reconcile. Badge Derived from default template. Center selected AI summarize step, source Proposal revision3, output Draft reviewer summary, Human review required, never authoritative. Right automation controls mode AI-assisted selected, bounded autonomous option shown with settings panel Allowed actions Draft summary and Create review task only; Approval and pay changes require human decision; run limit20, expiresSep30, Pause automation. Top Simulate / Request publication review. Bottom run history with actors and evidence. Clear visual difference template origin vs automation mode.

## 15-analysis-reports: Employee experience retention and report design

Two substantial analysis screens, green enterprise shell. Left Insights / Retention trends, cohort North America238 workers, periodlast90days, voluntary exits6 of average240 workforce gives2.5%, sources and data quality. Selected people analysis Jordan Lee: Career discussion recommended based on employee-stated growth interest, no personal flight-risk probability, unknowns and request correction. No health or protected traits. Right Report builder semantic fields Team / Workforce count / Completed changes, preview totals Completed18 In progress6 Needs reconciliation2 transactions; delivery Draft, recipient scope checked, Generate preview primary. Explicit AI interpretation label separate from facts. No invented accuracy numbers.

## 16-communications: Email notifications and alerts

One enterprise Communications workspace three readable panels. Draft email for interview invitation Sam Rivera REQ301 with recipient, subject, timezone Sep10 2026 2PM Eastern/1PM Central, template version2, AI drafted label, Edit / Preview / Send invitation controls, nothing sent yet. Notification center Jordan promotion ready for review uses safe subject and Open request; leave item Coverage review requested without medical detail. Alert detail Payroll OP204 two differences, owner Maya, acknowledge distinct from resolve, Investigate. Footer delivery statuses Draft/Queued/Sent/Delivered separate and no assertion Delivered means Read.

## 17-auth-preferences: Access interruption and preference states

Four readable focused panels: enterprise organization sign-in Work email / Continue, no password and neutral help; step-up Confirm it's you to approve revision3 / Continue with organization sign-in, Draft preserved; denied access This item is unavailable / Return to My Work without names; Settings Preferences form edited with Compact density and Save changes, adjacent saved variant Changes saved and disabled Save. Same restrained ink/green/warm-white, accessible focus and labels. Small secondary Session expired draft saved example allowed. No alarming security marketing slogans.

## Visual craft and customer adaptation prompts

Each following prompt was used without the original shared prefix, with the approved Home as style reference.

### 18-visual-language: Visual language and component craft

Create a beautifully art-directed enterprise design-system sheet, NOT an application dashboard. Title HCM Next / Visual language. Warm white #FAFAF7 background, dark ink #102238. Generous40px outer margins, crisp vector-like geometry, exceptionally legible humanist sans type. Exact brand palette: emerald #006B57, hover #005344, sage #EAF3EF, white #FFFFFF, muted #526171, line #D5DDD8, amber #925400, red #B42318. Three vertical zones: left25% color swatches and type specimens 32/24/20/16/14px; center45% high-quality reusable controls with solid green primary 'Review request', outline secondary 'Save draft', focus ring state, 44px tall field, labels above values, avatar initials, white card with12px radius and hairline border; right30% a compact task list with statuses Awaiting review amber, Complete green, Draft gray, selected row sage; no individual names, no fabricated business figures. Bottom horizontal strip demonstrates comfortable versus compact table density and minimal elevation for popover only. Green primary must be a perfectly uniform FLAT fill, no gradient or mottled texture. NO purple/blue status colors, no huge pill cards, no fake shadows under every card, no photography or3D. This is a precise premium product UI design kit, not a marketing poster. All specimens aligned to a4px spacing grid. Show purposeful breathing room and disciplined typography.

### 19-workbench-craft: Workbench hierarchy and interaction features

Create one high-quality HCM Next My Work desktop page in the approved warm white and money-green enterprise visual direction. Preserve slim240px left sidebar and light topbar, high quality readable typography, but improve craft: perfectly flat solid emerald #006B57 accents, no texture or gradients,12px cards8px fields, crisp hairline borders, generous24px gutters. Header My Work with short subtitle 'Requests and decisions that need your attention'. Scope pill 'People Operations · North America'; Maya Chen MC top right. My Work active sage sidebar; Home People Organization Insights Admin plus Help Settings. Main area two-thirds width quiet compact work queue and one-third contextual detail panel. NO grid of vanity KPI cards. Queue toolbar Search work, Filters, Saved view; underline tabs All / Ready for review / Waiting / Completed. Six clean rows with varied readable business tasks (Promotion review; Time off request; New hire setup; Interview feedback; Payroll investigation; Update worker details); indicate type, owner initials, due time, plain-language status with icon. Selected Promotion review row uses pale sage independent of amber Awaiting review badge. Detail panel heading Promotion review, small Draft proposal label, 'Current → Proposed' two-column role comparison, evidence summary '3 checks passed', muted chronology, footer ONE flat green primary 'Review proposal' and quiet 'Open full page'. Selection is not approval. Modest consistent line icons, no charts, no giant metric numerals, no photographs, no shadows except slight popover. Include a small visibly open Filters popover anchored to toolbar demonstrating checkboxes and Apply filters button; do not cover detail primary. The result should feel comfortable, highly legible, elegant, calm and operational at production quality. No browser frame.

### 20-brand-layout-variants: Customer branding and layout variants

Use case: ui-mockup. High-quality HCM Next visual design comparison, three evenly spaced desktop Home panels on neutral white background. Input image is visual reference only. Title: One platform, your identity. Panels labelled Default / Customer A / Customer B. Default uses emerald #006B57 warmwhite rounded8px controls and12px cards. Customer A uses cobalt #2159B5 white background4px controls8px cards. Customer B uses plum #6A3D7C warmwhite12px controls16px cards. All readable ink typography, identical semantic status colors amber for Awaiting review, green for Completed; brand color MUST NOT change status meaning. Neutral simple abstract logo placeholders marked Your logo for customer panels. Show real layout reorganization: Default left nav Home, My Work, People, Organization, Insights, Admin and Home with attention queue above team coverage; Customer A grouped nav WORKSPACE (Overview, Requests), TEAM (Directory, Coverage), TOOLS (Reports) and Home with coverage left and requests right; Customer B grouped nav MY DAY (Start, Tasks), PEOPLE (Team, Schedule), INSIGHTS (Reports), Home has requests full width then company news and shortcuts side by side. Keep visible tenant identity, search, user identity, Help and Settings in each. Required attention region identified with small lock icon in editor-style annotations, not hidden. Minimal illustrative task labels only, no named employees, no compensation data, no fake KPI counts. Large enough clean panels for legibility, perfectly flat colors, no textured buttons, no3D. Bottom note 'Brand, layout and density can change. Permissions and status meaning do not.' This must look like premium enterprise software with meaningful structural variation, not three cosmetic screenshots.

### 21-branding-studio: Branding and navigation studio

Use case: ui-mockup. Premium HCM Next customer branding configuration screen in warmwhite, ink and emerald. Reference is approved shell style, not data. Full desktop landscape screen. Page title Brand & navigation, subtitle Configure your company experience. Header draft version v3, Save draft secondary and Review changes primary. Layout left settings rail Brand / Typography / Shape & spacing / Navigation / Page layouts / Review & publish. Central settings pane Brand selected with company logo upload area showing neutral placeholder Your logo, Accent color emerald #006B57 field plus small cobalt and plum preset swatches, Heading/body font dropdowns, Shape preset Segmented selector Square / Balanced selected / Soft, Density Comfortable / Compact. Below visible mini Navigation tree with groups Workspace (Home, Requests), People (Directory, Coverage), Reports; row grips AND Move up / Move down buttons, Add group, Rename controls. Right live Home preview with synthetic generic content and banner Preview only; tabs Desktop/Mobile; dropdown Preview as Manager; lock on required Attention section. A clear validation card 'Contrast checks passed' and note 'Preview does not change permissions'. Bottom publishing bar Draft changes only, Version history, Reset section to inherited, Review changes. Keep settings readable and uncluttered, consistent8px controls12px cards, subtle boundaries, flat fills, no gradients, no real people or HR data. This is editable design composition not production proof.

## Applied targeted corrections

The original renders remain in the generated-image archive. The workspace selects the corrected variants below. Unapplied draft correction prompts are not included.

### 02-role-homes

Use case: ui-mockup. Targeted correction to this HCM Next design board. Preserve the two-panel layout, all styling, employee panel and Jordan proposal. Correct only the manager panel: scope dropdown must say My team, not People Operations. Cross-team coverage card for Avery Patel MUST NOT disclose any medical or leave reason: replace Medical Leave with Coverage needed, replace date with Begins Sep 8, 2026, retain Covered by Taylor Kim. No medical text anywhere.

### 03-worker-profile

Use case: ui-mockup. Targeted correction to this HCM Next design board. Preserve layout, green style and the Jordan current-versus-proposed separation. Fix only these details: sidebar active selection must be People, not Home. The right Reports to card must say Alex Morgan with initials AM and title Strategy Manager (not Elena Ruiz). Remove the invented Team 6 direct reports card and replace with Work context showing Strategy, New York, NY, Current role: Senior Analyst. Pay summary badge must read Sensitive · authorized instead of Restricted, since this authorized principal can view it.

### 04-promotion-draft

Use case: ui-mockup. Targeted correction to this HCM Next design board. Preserve this entire form, values, validation inset and style. Correct the acting user in top right from Noah Williams NW to Alex Morgan AM, the authorized requester. Highlight People in sidebar instead of Home. Add a small readable identity line beneath Promotion draft: PR-1042 · Revision 3 · Draft. Nothing is submitted or executed.

### 08-recovery-states

Use case: ui-mockup. Targeted editing of this enterprise UI board. Preserve six-card design but correct unsafe recovery promises. In Connection lost replace 'Work offline' with 'View last loaded data'; add 'Read-only · actions unavailable until reconnected'. In Session expired REMOVE all worker names and compensation details, remove 'Any unsaved changes were saved as a draft' and 'Saved just now'. Show 'Sign in to check your last saved draft. Recent edits may not have been saved.' No claim that interrupted or unsaved input was persisted. Keep sign-in button. In loading replace 'This won’t take long' with 'Loading the latest authorized data'. Use Maya Chen MC at top.

### 10-page-composition

Use case: ui-mockup. Targeted editing of this enterprise UI board. Preserve three-pane Studio layout and blocked Publish. Outer top-right is Maya Chen MC, add 'Experience administrator · scoped role'. Preview canvas has a visible banner 'Synthetic preview · Manager persona · No impersonation', greeting Good morning, Alex, initials AM, scope My team; remove Acting as yourself. Replace 'Avery Patel · Medical leave' with 'Avery Patel · Cross-team coverage' and make coverage text 'Coverage needed from Sep 8, 2026'. Never display medical text. Team coverage table header must say 'Authorized coverage participants (3)', not Direct reports (5). Rows: Jordan Lee Senior Analyst; Avery Patel Product Designer; Maya Chen HR Business Partner. Remove unsupported direct reporting relationships. Inspector source description 'Authorized coverage projection; preview cannot grant access'. Preserve locked required sections and validation.

### 13-payroll

Use case: ui-mockup. Targeted editing of this enterprise UI board. Preserve two-panel payroll design and exact calculations238=236+2,3000-600-150=2250. Left signed-in identity must be Noah Williams NW with 'Payroll specialist · authorized scope', not Maya. Right Noah role Payroll Specialist · People Operations, not Senior Analyst. The right screen is a draft statement preview of an UNAPPROVED payroll run: title 'Pay statement preview'; status 'Draft · not issued'; replace 'Payment will be deposited...' with 'Preview only. This run is not approved and no payment has been released.' Arrow label 'Draft preview from PAY0915'. No guaranteed payment. Keep exceptions blocking approval.

### 17-auth-preferences

Use case: ui-mockup. Targeted editing of this enterprise UI board. Keep four-panel board layout and green design. Public sign-in panel MUST NOT show an authenticated sidebar, tenant switcher, search, bell or Noah identity: remove that shell entirely in panel1, keep neutral HCM Next logo, work-email field, Continue and generic sign-in help. Email placeholder name@example.com. No tenant membership confirmation. Step-up panel2 principal Maya Chen MC, text confirming stronger assurance to review PR-1042 revision3; replace Draft preserved text with 'Last confirmed save: Sep5,2026 8:41AM. Newer edits may need review.' Access denied panel3 remove Draft saved line completely, retain generic item-unavailable wording. Preferences panel4 stays Noah with edited/saved states. Never expose resource details on denied screen.
