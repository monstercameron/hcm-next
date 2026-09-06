# The maximal organizational shape of an hcm-next customer, 2026 research

Read date for web sources referenced below: 2026-09-06. This document defines the worst-case organizational complexity the account and organization tables in this repository must be able to represent, and proposes a PostgreSQL table design for it in the companion file `planning/research/organization-structure-tables.sql`. Claims that depend on a named product's data model or on a regulatory text are cited by name and date. Claims that are ordinary domain reasoning about HCM/payroll systems in general, without one specific citable source, are marked "general knowledge" so a reviewer can tell the two apart.

## 1. Method and what was read in this repository first

Before reasoning from outside knowledge, the following repository material was read end to end, because the task is to fit the design to this codebase's own conventions rather than to import a foreign schema:

- `migrations/00002_tenant_primitives.sql`: the `tenant_ref`, `semantic_key`, `cas_version`, `digest_algorithm`, `content_digest` domains, the `tenant` table, and the half-open `[effective_from, effective_to)` business-time convention used everywhere else.
- `migrations/00008_tenant_isolation.sql`: the `hcmnext_app` least-privilege role, the row-level-security predicate `tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid` applied with `FORCE ROW LEVEL SECURITY` on every tenant-scoped table, and the house rule that `hcmnext_app` is never granted `DELETE` because "this data plane has no delete semantics, only append and state transition."
- `migrations/00050_tenant_placement.sql`: an epoch-fenced mutable control table (`tenant_placement`), an append-only event table (`tenant_provisioning_event`), and an append-only revision table (`government_authorization_profile`) -- the three shapes reused throughout this design.
- `migrations/00061`-`00064` (promotion evidence tables) and `migrations/00078_commercial.sql`: the append-only `_evidence`/`_revision` shape with `forbid_mutation` triggers, used as the template for every new table below.
- `internal/domains/org`, `internal/domains/organization`, `internal/domains/people`, `internal/domains/position`, `internal/domains/location`, `internal/domains/tenant` (including `govauth`), `internal/domains/taxprofile`, `internal/commercial`, and `migrations/00012_organization_aggregates.sql`, `00072_benefits.sql`, `00076_cba.sql`, `00049_location.sql`.
- `definitions/storage/storage-disposition.yaml`, for the registry row schema every table must satisfy.
- `planning/specs/organization-scope-and-authz.md`, `planning/specs/organization-and-relationship-domain.md`, `planning/specs/people-employment-assignment-domain.md`, and the relevant sections of `planning/specs/platform-architecture-catalog.md` (corporate scope and inheritance, person/employment scope, cross-company transactions, billing and corporate hierarchy, privacy/GDPR roles).

Two findings from that reading shaped everything below:

1. **The organizational vocabulary already exists in the specs, but is not yet backed by tables for the harder cases.** `planning/specs/organization-scope-and-authz.md` already declares a typed, extensible organization vocabulary (`enterprise, legal_entity, employing_entity, payroll_unit, business_unit, division, department, team, location, region, cost_center, project, program, clinic, store, franchisee, supplier, joint_venture, board, committee, works_council, union, volunteer_group, member_group`) and typed edges (`part_of, reports_to, owns, controls, employs, operates, funds, governs, represents, franchises, supplies, located_in, allocated_to`), and `internal/domains/organization/contracts.go` already defines an `EdgeType` enum (`HIERARCHY, CORPORATE_OWNERSHIP, COST_CENTER, LEGAL_ENTITY_STRUCTURE, GEOGRAPHIC, PROJECT_MATRIX`) with no persistence behind it. This document treats that vocabulary as authoritative and designs storage that can hold it, rather than inventing a competing one.
2. **`migrations/00012_organization_aggregates.sql`'s `legal_entity` and `organization_unit` are deliberately thin P1A aggregates**, not the full corporate model: `legal_entity` is a flat row with no parent, no jurisdiction, and no dissolution; `organization_unit` mixes `BUSINESS_UNIT`, `DIVISION`, `DEPARTMENT`, `TEAM` and `COST_CENTER` into one single-parent tree. The task's own design principle -- keep the legal-entity tree, the management tree, the cost/financial tree, and the service-relationship graph separate -- is a genuine refinement over 00012, not a restatement of it. Section 6 explains how the new tables relate to these two existing ones without editing them.

## 2. External sources and regulatory drivers

The organizational complexity described here is not hypothetical; it is the ordinary worst case for HR service providers and their clients. The following named sources and regulatory drivers shape specific pieces of the design. Anything not listed here and not otherwise footnoted is general knowledge about how enterprise HCM/payroll platforms (Workday, SAP SuccessFactors, Oracle HCM, ADP, Rippling, Gusto, Deel, TriNet, Justworks) commonly model this space, reasoned from their publicly documented product behavior rather than from access to their internal schemas.

- **PEO co-employment.** A Professional Employer Organization enters a co-employment relationship with a client: the PEO becomes an employer of record for tax and insurance purposes while the client remains the worksite employer directing day-to-day work. The U.S. IRS's Certified Professional Employer Organization (CPEO) program (established by the Tax Increase Prevention Act of 2014, IRC section 7705, and administered under 26 CFR part 301) formalizes this so a CPEO can be treated as the employer for federal employment tax purposes; see [IRS: Certified Professional Employer Organization](https://www.irs.gov/tax-professionals/certified-professional-employer-organization). This is the direct source for this design's `PEO_CO_EMPLOYER` / `WORKSITE_EMPLOYER` distinct roles in `service_relationship_revision` and `co_employment_relationship_revision`.
- **Common paymaster.** IRC section 3121(s) lets related corporations that concurrently employ the same worker designate one of them as a "common paymaster" so combined wages are tracked once for Social Security and FUTA wage-base purposes instead of restarting per entity; see [26 U.S.C. 3121(s)](https://www.law.cornell.edu/uscode/text/26/3121) and IRS Publication 15 (Circular E)'s common-paymaster discussion. This is the direct source for `pay_agent_revision`'s `COMMON_PAYMASTER` kind and for `payroll_group_legal_entity` allowing one payroll group to span several legal entities.
- **Successor employer.** IRS guidance on successor employers (also documented in Publication 15) lets an acquiring employer that meets the statutory predecessor/successor test continue the acquired workers' wage base for FICA/FUTA rather than resetting it, which only makes sense if the acquired legal entity's identity, tax registrations, and worker history survive the acquisition as first-class facts -- the direct motivation for `legal_entity_event`'s `ACQUIRED`/`MERGED_INTO` kinds carrying a `successor_entity_id` rather than deleting the acquired entity's row.
- **FLSA joint employment.** The U.S. Department of Labor's joint-employer guidance under the Fair Labor Standards Act (see [DOL Fact Sheet #35: Joint Employment under the FLSA](https://www.dol.gov/agencies/whd/fact-sheets/35-flsa-joint-employment)) recognizes that two or more entities can simultaneously be an employer of the same worker for wage-and-hour liability, distinct from which one issues the paycheck. This is a second, independent citation (alongside CPEO) for keeping worker-level co-employment as its own governed fact rather than inferring it from Employment.legal_entity alone.
- **State unemployment insurance (SUI) account structure.** Each U.S. state issues its own employer SUI account number per legal entity per state, and multi-state employers commonly need one active SUI account per entity per state of operation, independent of federal EIN; state workforce agencies (e.g., the California Employment Development Department, the Texas Workforce Commission) each publish their own registration requirements. This general regulatory pattern (not one single citable federal source) motivates `legal_entity_tax_registration_revision` being keyed by (legal entity, jurisdiction, registration kind) rather than one tax-id field per entity.
- **ACA applicable large employer (ALE) aggregation.** Under the Affordable Care Act's employer shared-responsibility provisions, related entities under a controlled group (IRC section 414(b)/(c)/(m)) are aggregated to determine ALE status even though each entity still files its own Forms 1094-C/1095-C; see [IRS: Determining if an Employer is an Applicable Large Employer](https://www.irs.gov/affordable-care-act/employers/determining-if-an-employer-is-an-applicable-large-employer). This is the direct source for needing `legal_entity_closure` and `legal_entity_ownership_edge`: ALE aggregation is exactly an ancestor/descendant question over the ownership graph, evaluated per calendar year.
- **EU works councils and GDPR controller/processor roles.** The EU Works Councils Directive (2009/38/EC) requires employee information and consultation bodies that can span the operations of a single "controlling undertaking" across national subsidiaries, and the GDPR (Regulation (EU) 2016/679, see the [official consolidated text](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng/)) distinguishes controller, joint controller, and processor roles that can differ per legal entity within one corporate group. This motivates `legal_entity_data_residency_revision` carrying its own `controller_role` per entity rather than inheriting one tenant-wide privacy posture, consistent with `planning/specs/platform-architecture-catalog.md`'s own `ProcessingContext` (controller/joint controller/processor/subprocessors) already declared there.
- **UK PAYE references.** UK employers register for a PAYE reference (and, if unionized under a group PAYE scheme, one PAYE reference can cover several related employers) through HMRC; see [HMRC: Register as an employer](https://www.gov.uk/register-employer). This is the source for `PAYE_REFERENCE` as a distinct registration kind in `legal_entity_tax_registration_revision`, and for allowing a `pay_agent_revision` row to serve more than one legal entity the same way a group PAYE scheme does.
- **Franchise structure (general knowledge).** A franchisor commonly licenses its brand, systems, and operating standards to a franchisee that is its own separately incorporated, separately taxed employer; the franchisor typically has no employment relationship with the franchisee's workers (a distinction litigated repeatedly in U.S. joint-employer cases such as the National Labor Relations Board's 2015-2020 browning-ferris/McDonald's joint-employer rulemaking history). This is general knowledge about franchise law rather than one citable source, and is the reason `FRANCHISE_AGREEMENT` in `legal_entity_ownership_edge` is a licensing/branding relationship, not an ownership percentage, while `FRANCHISE_UNIT` entities remain fully independent nodes in the legal-entity tree.
- **PEO/ASO/EOR/staffing/reseller platform behavior (general knowledge, informed by public product documentation).** Rippling, Deel, TriNet, Justworks and Gusto each publicly describe multi-entity, multi-country employment support (Rippling's "Employer of Record" and multi-EIN payroll features, Deel's "Deel EOR" and "Deel HR" split, TriNet's and Justworks' PEO co-employment model, Gusto's multi-state, single-entity focus by contrast). Workday and SAP SuccessFactors both publicly document a "Company/Business Unit hierarchy separate from a Cost Center hierarchy" pattern and an "instance of Foundation Objects effective-dated over time" pattern; Oracle HCM Cloud publicly documents "Legal Employer" as distinct from "Business Unit" and from "Department," and ADP Workforce Now documents FEIN-based "Client" groupings for its own PEO/ASO lines. These are used here as evidence that the enterprise-market pattern of separating legal, management, financial, and service-relationship dimensions is standard practice, not an invention of this document; no specific field name or table structure from any of these products is reproduced, since none of their internal schemas are public.

## 3. The worst-case dimensions, one at a time

### 3.1 Provider tenant serving many client organizations

The platform itself can be operated by an HR service provider -- a PEO, an ASO (administrative services organization, which unlike a PEO does not become co-employer), an EOR, a staffing agency, or a subcontracted HR shop -- that uses one hcm-next deployment to serve many unrelated client businesses. Two shapes are both real and both must be supported:

- The client is small enough, or wants isolation shallow enough, that it can live as a "company"/legal entity inside the provider's own tenant (the `platform-architecture-catalog.md` "Tenant / Enterprise Group / Company / Legal Entity" model, section 9.9). This case needs no new cross-tenant machinery: it is `legal_entity_ownership_edge` plus `service_relationship_revision` inside one tenant.
- The client needs its own hard isolation boundary -- its own encryption keys, its own data residency, its own admins who must never see another client's tenant even in aggregate -- and becomes its own tenant. The provider-client relationship then has to be expressed _between_ tenants, which 00002/00008's tenant model does not have a table for today. `tenant_relationship_revision` (Section 8 of the SQL) closes that gap.

A single provider tenant realistically manages a portfolio of both shapes at once: some small clients folded in as companies, some larger clients kept as their own tenants, and in the "tenant-of-tenant" case, a white-label reseller tenant that itself resells the platform under its own brand to further sub-clients, each of those in turn either a company or its own tenant. `tenant_relationship_revision` is deliberately not restricted to one hop so a `RESELLER` edge and a `PEO_PLATFORM_PROVIDER` edge can both exist and be walked transitively by application code (the table does not attempt to enforce a maximum depth; see Open Question 4).

### 3.2 Businesses, legal entities, shells, holding companies, joint ventures, franchises, brands and trade names

A single client organization commonly owns more than one operating business, and each business is itself built from a mix of:

- **Operating companies**, the entities that actually employ workers.
- **Holding companies and shell companies**, which own equity in operating companies or hold IP/brand assets but may employ zero workers directly. A shell company still needs a row (it can appear in payroll GL consolidation, in M&A due diligence, and eventually in an ALE aggregation) even with no workers.
- **Joint ventures**, jointly owned by two or more parents, each with a percentage stake, and often with its own separate employer registration despite shared governance.
- **Franchise units**, independently owned and independently employing, licensed to use a brand and operating system but not owned by the franchisor.
- **Brands and trade names (DBAs)**, which are very often _not_ separate legal entities at all -- the same LLC may do business as three different consumer-facing names in three different states, each requiring its own state DBA filing.

`legal_entity_structure_revision.entity_type` gives each node a closed classification (`ENTERPRISE_GROUP, HOLDING_COMPANY, OPERATING_COMPANY, SUBSIDIARY, SHELL_COMPANY, JOINT_VENTURE, FRANCHISE_UNIT, BRANCH, PROFESSIONAL_EMPLOYER_ORGANIZATION, STAFFING_AGENCY, NONPROFIT_AFFILIATE`), `legal_entity_ownership_edge` connects them as a DAG rather than a tree so a joint venture can have two live parent edges, and `legal_entity_trade_name_revision` represents a brand/DBA as a jurisdiction-scoped name asserted by one legal entity rather than inventing a fictitious entity for it.

### 3.3 Multi-country and multi-state legal entities, tax registrations, EINs, and pay agents

Every legal entity operating in more than one tax jurisdiction needs a separate registration per jurisdiction per registration kind: a single U.S. federal EIN, but a distinct state withholding account and a distinct SUI account per state of operation; a distinct VAT/GST number per country; a distinct PAYE reference in the UK (which can itself be a group scheme shared by several related UK entities). `legal_entity_tax_registration_revision` therefore keys on `(legal_entity_id, jurisdiction_country, jurisdiction_subdivision, registration_kind)` rather than one column per tax-id type, and -- following `internal/domains/taxprofile`'s existing discipline of never storing a raw registration number in a governed table -- stores only `registration_id_ref`, a reference into wherever the raw number is vaulted.

Who actually remits and files for a given registration is a separate question from who holds it: `pay_agent_revision` names whether a legal entity files for itself, uses a common-paymaster affiliate, is filed for by its PEO, or uses a third-party payroll bureau, and `payroll_group_revision`/`payroll_group_legal_entity` let one payroll run's processing group span several legal entities the way a common-paymaster arrangement or a group PAYE scheme does in practice.

### 3.4 Co-employment: worker of record, employer of record, worksite employer

`internal/domains/people`'s `Employment` already carries exactly one `LegalEntity` (see `employment_timeline.go`'s `FieldLegalEntity` and `people-employment-assignment-domain.md`'s "Employment: employment_id, legal entity, worker type, contract interval"). That one field is correct and sufficient for the ordinary case, but it collapses three distinct real-world roles into one:

- the **employer of record**, who bears the statutory employer obligations (payroll tax remittance, workers' compensation, unemployment insurance);
- the **worksite employer**, who directs the work day to day and is liable under FLSA joint-employment doctrine even without being the employer of record; and
- in a staffing-agency context, the agency that is the **worker's employer of record while a totally separate client is the worksite employer**, sometimes called the "worker of record" side of a staffing placement.

`co_employment_relationship_revision` is the satellite table that names all three roles for one worker's one employment without changing the existing `Employment.legal_entity` field's meaning: it is additional evidence layered on top of Employment, not a replacement for it, exactly matching this task's instruction to design for the worst case without editing `internal/domains/people`.

### 3.5 Shared-service centers serving several entities

A shared-service center (payroll operations, recruiting, IT helpdesk, finance) is organizationally a single team but functionally serves every legal entity in the group. `shared_service_center_scope` records, per shared-service `management_unit`, which legal entities it serves and for which functions, so a shared-service worker's assignment can sit in one management unit while the cost and access implications of that unit fan out across every entity it serves.

### 3.6 Matrix and dotted-line reporting crossing entity boundaries

`internal/domains/org/manager.go` already models `RelationshipDirectManager` and `RelationshipDottedLine` at the worker level, and explicitly never treats a dotted line as a chain parent. `internal/domains/organization/contracts.go`'s `EdgeType` already reserves `PROJECT_MATRIX` for exactly this. Nothing in this design duplicates that worker-level relationship graph; the organizational tables here instead make sure the _entities and management units the graph refers to_ can themselves cross legal-entity lines (a shared-service management unit's `home_legal_entity_id` need not equal the legal entity of every worker who dotted-lines into it), which is what `planning/specs/organization-and-relationship-domain.md` means by "legal-entity crossing edges cannot imply employment transfer or data access."

### 3.7 Mergers, acquisitions, divestitures, renames, and re-parenting with effective dates

`legal_entity_event` gives each of these its own typed, append-only entry (`INCORPORATED, RENAMED, REPARENTED, MERGED_INTO, ACQUIRED, DIVESTED, DISSOLVED, REINSTATED`) with an `occurred_at` timestamp independent of `recorded_at`, and `legal_entity_structure_revision.successor_entity_id` lets a dissolved or merged-out entity point forward to whichever entity's tax registrations and worker history continue its identity, which is exactly what the IRS successor-employer rule (Section 2) requires be traceable. A re-parenting (a subsidiary moving from one holding company to another) is a new revision of the relevant `legal_entity_ownership_edge` row (closing the old edge's `effective_to` and opening a new edge), which automatically requires a `legal_entity_closure` rebuild for every affected ancestor/descendant pair rather than a single-row update.

### 3.8 Entity dissolution with retained records

A dissolved entity's row is never deleted: `lifecycle_state = 'DISSOLVED'` with a `dissolution_date` is simply the terminal state of its own revision chain, and every table that referenced it by `legal_entity_id` (tax registrations, worksite assignments, payroll group membership, ownership edges) keeps its historical rows exactly as they were, satisfying retention obligations (Form I-9, FLSA payroll records, state wage-and-hour statutes) that outlive the entity itself.

### 3.9 Cost centers, departments, divisions and business units as a separate dimension from legal structure

This is the design principle most in tension with `migrations/00012_organization_aggregates.sql`'s existing `organization_unit`, which mixes `BUSINESS_UNIT, DIVISION, DEPARTMENT, TEAM` and `COST_CENTER` into one single-parent tree via `org_type`. In the worst case these two dimensions diverge: a shared-service department (one management unit) can split its cost 40/60 across two cost centers that belong to two different businesses, and a single cost center can receive allocated overhead from several management units. `management_unit_revision`/`management_unit_closure` and `cost_center_revision`/`cost_center_closure` are therefore two independent trees, connected only by the many-to-many `cost_center_allocation` table, matching Workday's and SAP SuccessFactors' publicly documented pattern of keeping "Supervisory Organization" and "Cost Center" as separate Foundation/Org objects rather than one hierarchy (general knowledge, informed by public product documentation, Section 2).

### 3.10 Locations and worksites

`internal/domains/location` already owns `Address`, `work_location_revision`, `worksite_revision`, and a jurisdiction table; this design does not duplicate any of it. `legal_entity_worksite_assignment` is the one new join needed: which legal entity operates out of which worksite, effective-dated, because ALE aggregation and SUI worksite reporting both need to count locations by controlled group across entity lines (Section 2), which a worksite table scoped only to a tenant cannot answer on its own.

### 3.11 Union and bargaining units crossing entities

`migrations/00076_cba.sql`'s `cba_bargaining_unit_revision` already exists and is not duplicated here. What is missing is a bargaining unit that covers workers employed by more than one separately incorporated legal entity under one master agreement (common in multi-employer building-trades and hospitality bargaining) -- `cba_bargaining_unit_legal_entity` is that one join table.

### 3.12 Benefits plans sponsored by one entity and adopted by others

`migrations/00072_benefits.sql`'s `benefit_plan_revision` already carries a single `sponsor_ref`. `benefit_plan_adoption_revision` adds the missing many side: every other legal entity that has formally adopted the sponsor's plan for its own workers, each under its own adoption agreement reference, which is exactly how a multi-employer welfare arrangement (MEWA) or a parent-sponsored 401(k) adopted by subsidiaries typically works in practice (general knowledge).

### 3.13 Payroll groups and pay agents crossing entities

Covered in Section 3.3 above (`payroll_group_revision`, `payroll_group_legal_entity`, `pay_agent_revision`).

### 3.14 Access and authorization scoped at any level of the tree

`planning/specs/organization-scope-and-authz.md` already owns the `RoleBinding`/`scope_expression` model (`organization + descendants, legal_entity, location/country, cost_center, project/assignment`) and explicitly treats organization scope as an authorization input rather than a storage concern of the organization tables themselves. This design deliberately does not add a parallel authorization table: every node in every tree here (`legal_entity_id`, `management_unit_id`, `cost_center_id`) is a stable UUID that an existing or future `scope_expression` can reference directly, and the closure tables (Sections 1, 3, 4 of the SQL) are exactly what makes "organization + descendants" a fast query once a scope names a root. See Open Question 2.

### 3.15 Data residency and privacy boundaries per entity

Covered in Section 2 (GDPR controller/processor citation) and by `legal_entity_data_residency_revision`.

### 3.16 Billing and commercial scoping (who pays for whom)

`internal/commercial`'s `Contract`/`Snapshot` and `migrations/00078_commercial.sql`'s `commercial_contract_revision`/`entitlement_snapshot` already govern _what_ a tenant is entitled to. Neither owns _who within a multi-entity tenant is charged for what_, which `planning/specs/platform-architecture-catalog.md` section on "Billing Accounts and Corporate Hierarchy" explicitly separates from entitlement (payer scope need not equal usage scope). `billing_party_revision` is the payer identity and `billing_scope_assignment_revision` maps a usage scope (a legal entity, a management unit, or the whole tenant) to a payer, with an allocation percentage so a shared corporate overhead can be split, exactly the "consolidated invoice with subsidiary attribution" pattern the catalog describes.

### 3.17 Tenant-of-tenant and white-label reseller cases

Covered in Section 3.1 and by `tenant_relationship_revision`.

### 3.18 One human employed by several entities under one tenant, or across tenants

Within one tenant this needs no new table: `people-employment-assignment-domain.md` already states a Person may have multiple simultaneous Employments, each with its own legal entity, and `internal/domains/people/facts.go`'s field model already carries `FieldLegalEntity` per Employment. The genuinely new case is the same human under **two different tenants** -- a contractor engaged by two unrelated staffing agencies that are each their own tenant, or a worker migrating between a reseller's client tenants. `cross_tenant_identity_link_revision` records that linkage as evidence (a purpose, a consent reference, a status) without copying any person fact across the tenant boundary, mirroring the discipline `people-employment-assignment-domain.md` already applies to identity-resolution claims within one tenant. See Open Question 5.

## 4. The worst-case example organization

```text
Tenant: Meridian Workforce Solutions          (platform tenant; a PEO/ASO/EOR provider)
  |
  +-- tenant_relationship: PEO_PLATFORM_PROVIDER, billed PLATFORM_BILLS_PROVIDER_CONSOLIDATED
        |
        v
Tenant: Solstice Hospitality Group            (client tenant; its own hard isolation boundary)
  |
  +-- Legal entity: Solstice Holdings                    [ENTERPRISE_GROUP, no workers]
  |     |
  |     +-- owns (WHOLLY_OWNED_SUBSIDIARY) --> Solstice Hotels US LLC        [OPERATING_COMPANY, Delaware]
  |     |         trade name: "Solstice Inn"     (jurisdiction: CA)
  |     |         trade name: "Solstice Suites"  (jurisdiction: NV)
  |     |         tax registrations: FEDERAL_EIN, CA STATE_WITHHOLDING, CA STATE_UNEMPLOYMENT_INSURANCE,
  |     |                             NV STATE_UNEMPLOYMENT_INSURANCE
  |     |         worksites: Solstice Inn - Anaheim CA; Solstice Suites - Reno NV
  |     |         acquired: Bayview Resorts LLC  [legal_entity_event: ACQUIRED, then REPARENTED under this entity]
  |     |
  |     +-- owns (WHOLLY_OWNED_SUBSIDIARY) --> Solstice Hotels Canada ULC    [OPERATING_COMPANY, Ontario]
  |     |         tax registrations: CRA business number, ON employer health tax
  |     |
  |     +-- owns (JOINT_VENTURE_PARTNER, 50%) --> Solstice-Marbella JV S.L.  [JOINT_VENTURE, Spain]
  |     |         (other 50% owned by an external party outside this tenant, not modeled)
  |     |
  |     +-- owns (WHOLLY_OWNED_SUBSIDIARY) --> Solstice Staffing Solutions   [STAFFING_AGENCY]
  |     |         co_employment: WORKER_OF_RECORD for workers whose worksite employer is
  |     |                        Solstice Hotels US LLC (banquet/event staff pool)
  |     |
  |     +-- FRANCHISE_AGREEMENT (brand license, no ownership) --> 12x independently owned
  |             "Solstice Inn" franchise units [FRANCHISE_UNIT, each its own EIN, each its own
  |              employer -- Solstice Holdings has no employment relationship with their workers]
  |
  +-- Management tree (separate from the legal tree above):
  |     Solstice Shared Services Center [SHARED_SERVICE_CENTER]
  |       shared_service_center_scope: serves Solstice Hotels US LLC, Solstice Hotels Canada ULC,
  |                                     and Solstice-Marbella JV S.L. for payroll, IT, and recruiting
  |
  +-- Cost tree (separate again):
  |     Corporate Overhead [cost center]
  |       cost_center_allocation: 70% <- Solstice Shared Services Center, 30% <- Solstice Hotels Canada ULC
  |
  +-- Bargaining: UNITE HERE Local 11 bargaining unit
  |     cba_bargaining_unit_legal_entity: covers housekeeping staff at Solstice Hotels US LLC
  |                                       AND at Solstice-Marbella JV S.L.
  |
  +-- Benefits: "Solstice National PPO" (sponsored by Solstice Hotels US LLC)
  |     benefit_plan_adoption: adopted by Solstice Staffing Solutions;
  |                             Solstice Hotels Canada ULC runs a separate, jurisdiction-appropriate plan
  |
  +-- Payroll: "US Biweekly Hourly" payroll group
  |     payroll_group_legal_entity: Solstice Hotels US LLC + Solstice Staffing Solutions
  |     pay_agent: Solstice Hotels US LLC acts as COMMON_PAYMASTER for both (IRC 3121(s))
  |
  +-- Billing: billing_party "Solstice Corporate AP"
  |     billing_scope_assignment: usage at every Solstice legal entity and management unit
  |                                is charged 100% to this one payer (consolidated invoice)
  |
  +-- International remote hire via the provider's EOR entity:
  |     Worker: Elena Petrova, hired in Poland
  |     co_employment_relationship: employer_of_record = Meridian EOR Poland Sp. z o.o.
  |                                  (a legal entity inside the PROVIDER tenant, Meridian Workforce
  |                                   Solutions), worksite_employer = Solstice Hotels US LLC
  |                                  (marketing function, fully remote)
  |     service_relationship: EOR_SERVICE_AGREEMENT, provider_tenant = Meridian Workforce Solutions,
  |                            client_legal_entity = Solstice Hotels US LLC
  |
  +-- One human, two employers, one tenant:
  |     Person: Maria Reyes
  |       Employment A: Solstice Hotels US LLC, front desk, 0.6 FTE
  |       Employment B: Solstice Staffing Solutions, banquet server pool, 0.4 FTE
  |
  `-- One human, two tenants:
        Maria Reyes also picks up freelance banquet shifts through an unrelated staffing tenant,
        "GigStaff" -- linked only via cross_tenant_identity_link_revision, with her consent, for
        purpose-limited W-2 aggregation; no person fact is copied across the tenant boundary.
```

## 5. Entity-relationship description

The design keeps four structures independent, connected only by shared UUIDs and satellite join tables, rather than one tree carrying every meaning:

1. **The legal-entity graph** (`legal_entity_structure_revision`, `legal_entity_trade_name_revision`, `legal_entity_ownership_edge`, `legal_entity_closure`, `legal_entity_event`). This is a DAG, not a tree, because a joint venture or a franchise/brand-license relationship can give one entity more than one live parent edge. It answers "who owns whom, and under what kind of relationship."
2. **The management (reporting/business) tree** (`management_unit_revision`, `management_unit_closure`, `management_unit_event`). Single-parent per live instant, like `organization_unit` in 00012, but deliberately not carrying `COST_CENTER` as one of its node types. It answers "who reports to whom, organizationally."
3. **The cost/financial dimension tree** (`cost_center_revision`, `cost_center_closure`, `cost_center_allocation`). Also single-parent per live instant, but structurally independent of tree 2; `cost_center_allocation` is the only bridge, and it is many-to-many. It answers "whose budget pays for this."
4. **The service-relationship graph** (`service_agreement_revision`, `service_relationship_revision`, `co_employment_relationship_revision`, `shared_service_center_scope`, `pay_agent_revision`, `payroll_group_revision`/`payroll_group_legal_entity`). This is a bipartite-ish graph between provider parties (which may be legal entities in this tenant or a whole provider tenant) and client parties. It answers "who serves whom, and who is the statutory employer of whom."

Two further satellite groups attach facts to nodes in the graphs above without becoming a fifth tree: tax/residency/worksite facts on a legal entity (`legal_entity_tax_registration_revision`, `legal_entity_data_residency_revision`, `legal_entity_worksite_assignment`), and cross-entity coverage facts (`cba_bargaining_unit_legal_entity`, `benefit_plan_adoption_revision`). Billing (`billing_party_revision`, `billing_scope_assignment_revision`) is deliberately generic over "usage scope kind" so it can point at a legal entity, a management unit, or the tenant itself without needing its own tree.

Two tables are the deliberate exception to "every row has one `tenant_id`": `tenant_relationship_revision` and `cross_tenant_identity_link_revision` each name two tenants and are governed by an OR-of-two-columns row-level-security policy instead of the house's usual single-column predicate (Open Question 3 and Hardest Decision 4 below).

## 6. Relationship to the existing `legal_entity` and `organization_unit` tables

This draft does not edit `migrations/00012_organization_aggregates.sql`. Its `legal_entity` and `organization_unit` tables remain valid, minimal, tenant-scoped aggregates, and every table added here is a satellite that can be adopted incrementally:

- `legal_entity_structure_revision` uses its own `legal_entity_id`, not 00012's `legal_entity.entity_id`, because the maximal case includes nodes (shell companies, trade-name-only wrappers, franchise units with no headcount yet) that may never need 00012's `aggregate_entity`-backed identity, which was built for entities that participate in the People/Position aggregate graph. A future migration could add a nullable `legacy_entity_id` column pointing at `aggregate_entity` for entities that need both identities, or could migrate 00012's rows into this table outright; this document does not decide which, and flags it as Open Question 1.
- `management_unit_revision` is a parallel, narrower tree (four node types instead of five, no `COST_CENTER`) rather than a replacement for `organization_unit`; existing code that reads `organization_unit` is unaffected until a product decision is made to migrate it.

## 7. Table list

New tables (see `planning/research/organization-structure-tables.sql` for full DDL):

| Table                                    | One-sentence purpose                                                                                                                                                                                                                                            |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `legal_entity_structure_revision`        | The corporate/statutory facts about one legal entity across its life: entity type, jurisdiction, incorporation and dissolution dates, and successor on exit.                                                                                                    |
| `legal_entity_structure_pointer`         | REBUILDABLE convenience naming the current revision row per legal entity, mirroring `definition_active_pointer`.                                                                                                                                                |
| `legal_entity_trade_name_revision`       | A jurisdiction-scoped DBA/brand name asserted by one legal entity.                                                                                                                                                                                              |
| `legal_entity_ownership_edge`            | The corporate ownership/control DAG: subsidiary, joint-venture, franchise, brand-license and management-agreement edges between entities.                                                                                                                       |
| `legal_entity_closure`                   | REBUILDABLE ancestor/descendant closure over live ownership edges, for fast "everything under holding company X" reads.                                                                                                                                         |
| `legal_entity_event`                     | Append-only lifecycle log: incorporated, renamed, reparented, merged, acquired, divested, dissolved, reinstated.                                                                                                                                                |
| `legal_entity_tax_registration_revision` | One governed tax registration (EIN, state withholding, SUI, VAT/GST, PAYE reference, social insurance) for one entity in one jurisdiction, referencing the responsible pay agent.                                                                               |
| `legal_entity_data_residency_revision`   | The required storage region and GDPR-style controller/joint-controller/processor role for one entity's workforce data.                                                                                                                                          |
| `legal_entity_worksite_assignment`       | Which physical worksites a legal entity operates out of, for ALE and SUI worksite aggregation.                                                                                                                                                                  |
| `pay_agent_revision`                     | Who remits and files for a legal entity in a jurisdiction: itself, a common-paymaster affiliate, a PEO, or a third-party payroll provider.                                                                                                                      |
| `payroll_group_revision`                 | A pay-frequency processing group that a payroll run executes against, independent of legal-entity boundaries.                                                                                                                                                   |
| `payroll_group_legal_entity`             | Which legal entities one payroll group currently covers.                                                                                                                                                                                                        |
| `management_unit_revision`               | One node of the management/reporting tree (business unit, division, department, team, shared-service center, program, project), independent of the legal and cost trees.                                                                                        |
| `management_unit_closure`                | REBUILDABLE ancestor/descendant closure over the management tree.                                                                                                                                                                                               |
| `management_unit_pointer`                | REBUILDABLE current-revision pointer per management unit.                                                                                                                                                                                                       |
| `management_unit_event`                  | Append-only lifecycle log: created, renamed, reparented, merged, split, frozen, closed.                                                                                                                                                                         |
| `cost_center_revision`                   | One node of the financial rollup tree, independent of the management tree it often parallels.                                                                                                                                                                   |
| `cost_center_closure`                    | REBUILDABLE ancestor/descendant closure over the cost-center tree.                                                                                                                                                                                              |
| `cost_center_pointer`                    | REBUILDABLE current-revision pointer per cost center.                                                                                                                                                                                                           |
| `cost_center_allocation`                 | The many-to-many bridge from a management unit to the cost center(s) that fund it, with a percentage split.                                                                                                                                                     |
| `service_agreement_revision`             | The commercial/legal agreement under which a provider delivers HR, payroll, benefits, or tax-filing services to a client entity.                                                                                                                                |
| `service_relationship_revision`          | The typed entity-level relationship instance created under an agreement (PEO co-employer, ASO administrator, EOR employer of record, staffing worker-of-record, worksite employer, shared-service provider, payroll agent provider, subcontracted HR provider). |
| `co_employment_relationship_revision`    | The worker-level fact naming both the employer of record and the worksite employer for one employment, layered on top of `Employment.legal_entity`.                                                                                                             |
| `shared_service_center_scope`            | Which legal entities a shared-service management unit serves, and for which functions.                                                                                                                                                                          |
| `benefit_plan_adoption_revision`         | Legal entities other than a benefit plan's sponsor that have formally adopted it for their own workers.                                                                                                                                                         |
| `cba_bargaining_unit_legal_entity`       | Legal entities covered by one bargaining unit, for multi-employer bargaining.                                                                                                                                                                                   |
| `billing_party_revision`                 | The payer identity for commercial charges, which may or may not coincide with a workforce legal entity.                                                                                                                                                         |
| `billing_scope_assignment_revision`      | Maps a usage scope (legal entity, management unit, or tenant) to a payer scope, with an allocation percentage.                                                                                                                                                  |
| `tenant_relationship_revision`           | A governed relationship between two whole tenants: reseller, white-label, or platform-level PEO/ASO/EOR/payroll-bureau provider and client.                                                                                                                     |
| `cross_tenant_identity_link_revision`    | Purpose-limited, consent-evidenced linkage of one human's identity across two different tenants, with no person fact copied across the boundary.                                                                                                                |

Existing tables this design deliberately reuses rather than duplicates: `tenant` (00002), `legal_entity` and `organization_unit` (00012, kept as-is, see Section 6), `cba_bargaining_unit_revision` (00076), `benefit_plan_revision` (00072), `commercial_contract_revision`/`entitlement_snapshot` (00078), `work_location_revision`/`worksite_revision`/`jurisdiction_table` (00049).

## 8. Queries the platform must answer

### 8.1 Every worker a client admin may see (bounded by legal-entity scope)

```sql
-- Every worker currently employed by any legal entity under (and including)
-- the legal entity the admin's scope names, as of a given instant.
WITH scoped_entities AS (
    SELECT descendant_id AS legal_entity_id
    FROM legal_entity_closure
    WHERE tenant_id = $1 AND ancestor_id = $2  -- admin's scope root
)
SELECT DISTINCT e.employment_id, e.worker_id
FROM employment_projection e            -- authorized People projection, not raw storage
JOIN scoped_entities se ON se.legal_entity_id = e.legal_entity_id
WHERE e.tenant_id = $1
  AND e.effective_from <= $3 AND (e.effective_to IS NULL OR e.effective_to > $3);
```

### 8.2 Every entity a payroll run covers

```sql
-- Legal entities in scope for payroll group $2's run dated $3, including the
-- common-paymaster/agent responsible for filing on each.
SELECT pgle.legal_entity_id, pa.agent_kind, pa.agent_legal_entity_id
FROM payroll_group_legal_entity pgle
LEFT JOIN legal_entity_tax_registration_revision r
    ON r.tenant_id = pgle.tenant_id AND r.legal_entity_id = pgle.legal_entity_id
    AND r.effective_from <= $3 AND (r.effective_to IS NULL OR r.effective_to > $3)
LEFT JOIN pay_agent_revision pa
    ON pa.tenant_id = pgle.tenant_id AND pa.pay_agent_id = r.pay_agent_id
    AND pa.effective_from <= $3 AND (pa.effective_to IS NULL OR pa.effective_to > $3)
WHERE pgle.tenant_id = $1 AND pgle.payroll_group_id = $2
  AND pgle.effective_from <= $3 AND (pgle.effective_to IS NULL OR pgle.effective_to > $3);
```

### 8.3 The effective reporting chain for a worker on a date

This is intentionally answered by the existing `internal/domains/org.ResolveManagerRelationships` worker-level graph, not by a new query over these tables: the organizational tables here only need to make sure the units that chain refers to resolve correctly across entity lines. For the management-unit-level analogue (a unit's own chain of organizational parents, as distinct from a worker's manager chain):

```sql
-- The full management-unit ancestor chain (root first) for unit $2 as of "now".
SELECT c.ancestor_id, c.depth, m.name, m.unit_type
FROM management_unit_closure c
JOIN management_unit_pointer p ON p.tenant_id = c.tenant_id AND p.management_unit_id = c.ancestor_id
JOIN management_unit_revision m ON m.tenant_id = p.tenant_id AND m.row_id = p.current_row_id
WHERE c.tenant_id = $1 AND c.descendant_id = $2
ORDER BY c.depth DESC;
```

### 8.4 All entities a single human is employed by

```sql
-- Within one tenant: every legal entity across every current Employment for
-- one person, via the authorized People projection.
SELECT DISTINCT emp.legal_entity_id
FROM employment_projection emp
WHERE emp.tenant_id = $1 AND emp.person_id = $2
  AND emp.effective_from <= now() AND (emp.effective_to IS NULL OR emp.effective_to > now());

-- Across tenants: confirmed cross-tenant links for this person, either side.
SELECT tenant_a_id, person_a_ref, tenant_b_id, person_b_ref, purpose
FROM cross_tenant_identity_link_revision
WHERE status = 'CONFIRMED'
  AND (effective_to IS NULL OR effective_to > now())
  AND ((tenant_a_id = $1 AND person_a_ref = $2) OR (tenant_b_id = $1 AND person_b_ref = $2));
```

### 8.5 Which provider is responsible for a client entity's filings

```sql
-- The pay agent responsible for each of a client legal entity's live tax
-- registrations, and the provider tenant/entity behind any EOR/PEO service
-- relationship covering that entity.
SELECT r.jurisdiction_country, r.jurisdiction_subdivision, r.registration_kind,
       pa.agent_kind, pa.agent_legal_entity_id,
       sr.relationship_kind, sr.provider_tenant_id, sr.provider_legal_entity_id
FROM legal_entity_tax_registration_revision r
LEFT JOIN pay_agent_revision pa
    ON pa.tenant_id = r.tenant_id AND pa.pay_agent_id = r.pay_agent_id
    AND pa.effective_to IS NULL
LEFT JOIN service_relationship_revision sr
    ON sr.tenant_id = r.tenant_id AND sr.client_legal_entity_id = r.legal_entity_id
    AND sr.effective_to IS NULL
    AND sr.relationship_kind IN ('EOR_EMPLOYER_OF_RECORD', 'PEO_CO_EMPLOYER', 'PAYROLL_AGENT_PROVIDER')
WHERE r.tenant_id = $1 AND r.legal_entity_id = $2 AND r.effective_to IS NULL;
```

## 9. Storage-disposition entries

All 30 new tables are tenant-scoped `AGGREGATE`-plane rows except where noted, permanently retained (append-only, `forbid_mutation`-guarded), platform-managed encryption, and reuse `internal/data/tenancy` as their isolation package, matching the pattern every existing revision table in `definitions/storage/storage-disposition.yaml` already follows. `*_pointer` and `*_closure` tables are `REBUILDABLE`/`OPERATIONAL`, mirroring `definition_active_pointer`'s existing entry. The two cross-tenant tables have no single `tenant_scoping_column` and are called out individually. Owner packages below are proposed, not yet created.

| Table                                  | data_role | tenant_scoping_column                                   | append_only | retention_class | rebuild_source                  |
| -------------------------------------- | --------- | ------------------------------------------------------- | ----------- | --------------- | ------------------------------- |
| legal_entity_structure_revision        | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| legal_entity_structure_pointer         | AGGREGATE | tenant_id                                               | false       | REBUILDABLE     | legal_entity_structure_revision |
| legal_entity_trade_name_revision       | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| legal_entity_ownership_edge            | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| legal_entity_closure                   | AGGREGATE | tenant_id                                               | false       | REBUILDABLE     | legal_entity_ownership_edge     |
| legal_entity_event                     | LEDGER    | tenant_id                                               | true        | PERMANENT       | null                            |
| legal_entity_tax_registration_revision | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| legal_entity_data_residency_revision   | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| legal_entity_worksite_assignment       | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| pay_agent_revision                     | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| payroll_group_revision                 | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| payroll_group_legal_entity             | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| management_unit_revision               | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| management_unit_closure                | AGGREGATE | tenant_id                                               | false       | REBUILDABLE     | management_unit_revision        |
| management_unit_pointer                | AGGREGATE | tenant_id                                               | false       | REBUILDABLE     | management_unit_revision        |
| management_unit_event                  | LEDGER    | tenant_id                                               | true        | PERMANENT       | null                            |
| cost_center_revision                   | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| cost_center_closure                    | AGGREGATE | tenant_id                                               | false       | REBUILDABLE     | cost_center_revision            |
| cost_center_pointer                    | AGGREGATE | tenant_id                                               | false       | REBUILDABLE     | cost_center_revision            |
| cost_center_allocation                 | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| service_agreement_revision             | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| service_relationship_revision          | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| co_employment_relationship_revision    | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| shared_service_center_scope            | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| benefit_plan_adoption_revision         | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| cba_bargaining_unit_legal_entity       | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| billing_party_revision                 | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| billing_scope_assignment_revision      | AGGREGATE | tenant_id                                               | true        | PERMANENT       | null                            |
| tenant_relationship_revision           | AGGREGATE | none (provider_tenant_id + client_tenant_id, see notes) | true        | PERMANENT       | null                            |
| cross_tenant_identity_link_revision    | AGGREGATE | none (tenant_a_id + tenant_b_id, see notes)             | true        | PERMANENT       | null                            |

Notes column for the two cross-tenant rows (elided above for width): "Spans two tenants; governed by a two-column OR row-level-security policy rather than the single tenant_id predicate every other table in this registry uses. See planning/research/organization-structure-maximal-2026.md section 3.1/3.18 and Open Question 3."

## 10. Open questions for the product owner

1. **Identity reconciliation.** Should `legal_entity_structure_revision.legal_entity_id` eventually become the same identity as `migrations/00012_organization_aggregates.sql`'s `legal_entity.entity_id` (via a migration that backfills a `legacy_entity_id` column, or a wholesale replacement of 00012's table), or are these permitted to stay two different identity spaces indefinitely, reconciled only by application code? This document takes no position; it is a schema-ownership decision, not a data-modeling one.
2. **Authorization node references.** Section 3.14 assumes `planning/specs/organization-scope-and-authz.md`'s `scope_expression` model will reference `legal_entity_id`/`management_unit_id`/`cost_center_id` directly once it has physical storage. Is that the intended integration point, or does Organization-Scoped AuthZ expect its own copy of these identifiers behind a projection, the way it already does for People/Position facts?
3. **Cross-tenant RLS pattern.** `tenant_relationship_revision` and `cross_tenant_identity_link_revision` are the first tables in this codebase (outside `tenant` itself) that cannot carry one `tenant_id` column. Is an OR-of-two-columns policy the house-approved pattern for this going forward, or should cross-tenant facts instead live in a platform-control-plane schema outside RLS entirely, gated by a service that enforces the two-party check in application code?
4. **Reseller chain depth.** `tenant_relationship_revision` does not cap how many hops a reseller/white-label chain may have. Is an unbounded chain actually a supported commercial product, or should a maximum depth (or a rule against a tenant appearing twice in one chain) be enforced, and if so, at the database layer or only in application code?
5. **Cross-tenant identity consent lifecycle.** `cross_tenant_identity_link_revision.status` moves `PROPOSED -> CONFIRMED -> REVOKED`, but who is authorized to propose a link between two tenants that, by construction, no single tenant administrator can see both sides of? Does this require a platform-level (not tenant-scoped) approval capability that does not yet exist in this repository?
6. **Franchise and JV worker visibility.** When a franchise unit or joint-venture entity is fully independent for employment purposes but shares a brand or a shared-service center with its franchisor/parent, should the franchisor ever have read access to the franchisee's worker-level data by default, or is `legal_entity_ownership_edge`'s `FRANCHISE_AGREEMENT`/`JOINT_VENTURE_PARTNER` kind meant to carry zero implied data access (matching `organization-and-relationship-domain.md`'s "legal-entity crossing edges cannot imply employment transfer or data access") until a separate, explicit grant exists?
7. **Closure table maintenance.** These closure tables are documented as `REBUILDABLE` and app-maintained, like `projection_checkpoint`, rather than trigger-maintained. Is a synchronous trigger-based closure update (accepting the write-amplification cost on every ownership-edge change) preferred instead, given how rarely the corporate ownership graph actually changes compared to how often it is read?

## 11. Companion file

The DDL implementing this design is in `planning/research/organization-structure-tables.sql`, a draft goose migration (not numbered into `migrations/`, not applied) with 30 new tables, their constraints, indexes, row-level-security policies, and comments.
