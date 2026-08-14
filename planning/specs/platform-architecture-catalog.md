# Platform Architecture Catalog

Extracted from the HCM Next architecture constitution so this contract can evolve independently. The master delivery scope remains governed by [../execution-plan.md](../execution-plan.md).

## Canonical Plane Atlas

The catalog is organized by one dependency spine and several attached planes:

```text
Experience/API -> Identity/Trust -> Governance -> Control resolution
                                                  |
                                                  v
                                               Workflow
                                                  |
                                                  v
                                     Domain + Regulatory capabilities
                                                  |
                                                  v
                           Data truth / serving / distribution
                                                  |
                                      durable effect intents
                                  +---------------+---------------+
                                  v                               v
                         Integration connectivity          Human messaging
                                  +---------------+---------------+
                                                  |
                                     observations / signals
                                                  |
                                                  v
                                         Data observation path
                                                  |
                                                  v
                                Workforce Intelligence

Operations / Assurance ================================= wraps all planes
Billing / Metering ====================================== observes usage
Agents = governed overlay across Control, Intelligence, Workflow, and Domain
```

Workflow owns orchestration, never domain persistence. Domain capabilities own
commands, queries, validation, invariants, authoritative transactions, events,
and critical projections. Connectivity owns delivery to systems and humans.
Derived intelligence normally consumes committed truth asynchronously and
cannot become an accidental dependency for unrelated material commands.

The authoritative plane definitions, dependency exceptions, data subplanes,
agent attachment, and Go-only realization are maintained in [the Canonical
Platform Plane Model](platform-plane-model.md).

### 9.8 API and Capability Plane

The API and Capability Plane makes the full Workforce OS machine-addressable without weakening its business semantics or safety controls.

```text
Humans        Product UI       Customer Apps       Agents
   │               │                │                 │
   └───────────────┴────────────────┴─────────────────┘
                            │
                  API / CAPABILITY PLANE
                            │
                    HTTP | gRPC | events
                            │
             ┌──────────────┴──────────────┐
             ▼                             ▼
       Identity + AuthZ            Capability Registry
             │                             │
             └──────────────┬──────────────┘
                            ▼
                 Workflow Control Plane
                            │
       ┌──────────┬─────────┼─────────┬──────────┐
       ▼          ▼         ▼         ▼          ▼
     People    Workforce  Talent   Rewards   Experience/Access
       │          │         │         │          │
       └──────────┴─────────┴─────────┴──────────┘
                            │
                     Business Ledger
                            │
              Projections + Operations + Repair
```

#### Capability-First Rule

Every meaningful platform behavior is exposed as a typed capability. The interface used to invoke it does not change its validation, authorization, workflow, ledger, idempotency, failure, or reconciliation semantics.

```text
No UI-only business function
No agent-only business function
No hidden admin mutation
No ordinary direct database repair

UI ───────────┐
Agent ────────┤
CLI ──────────┼──► Governed capability
Partner ──────┤
Customer app ─┘
```

Representative capability namespaces include:

| Namespace    | Examples                                                                                         |
| ------------ | ------------------------------------------------------------------------------------------------ |
| People       | `workers.get`, `workers.search`, `employments.change`, `positions.fill`, `orgs.restructure`      |
| Compensation | `compensation.propose`, `compensation.simulate`, `compensation.approve`, `compensation.execute`  |
| Workforce    | `timecards.read`, `punches.create`, `schedules.assign`, `leave.request`, `leave.approve`         |
| Recruiting   | `requisitions.create`, `applications.advance`, `interviews.schedule`, `offers.approve`           |
| Access       | `entitlements.read`, `access.request`, `access.grant`, `access.revoke`, `access.review`          |
| Workflow     | `workflows.describe`, `workflows.simulate`, `workflows.start`, `workflows.transition`            |
| Operations   | `incidents.search`, `reconciliation.run`, `drift.inspect`, `repairs.simulate`, `repairs.execute` |
| Platform     | `policies.publish`, `metadata.modify`, `integrations.configure`, `webhooks.configure`            |

The namespace expresses product meaning rather than deployment topology. A capability may coordinate several internal services while remaining one stable business contract.

#### One Semantic Contract, Multiple Transports

HTTP/JSON, gRPC, events, SDKs, and future transports are adapters over the same domain capability contract.

```text
                  Domain Capability Contract
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
          HTTP/JSON       gRPC         Events
          browsers       internal     subscriptions
          customers      services     and automation
```

For example, `worker.compensation.propose` defines one input, output, authorization action, validation model, workflow behavior, risk class, idempotency contract, and error taxonomy. An HTTP route and a gRPC method may expose it differently, but neither creates independent business semantics.

Transport parity does not require every capability to support every transport. It requires every supported transport to invoke the same semantic contract.

#### Semantic Commands Over Generic CRUD

Business mutations should normally express intent:

```text
worker.job.promote
worker.manager.change
worker.compensation.change
worker.terminate
worker.personal.legal_name.change
```

This is stronger than a broad `worker.update` permission or an unrestricted record patch. Semantic commands tell humans, policy engines, agents, approvers, and auditors which business action is occurring and which workflow, risk, and reconciliation rules apply.

CRUD remains appropriate for low-risk supporting resources. Material worker-state changes are commands that produce governed Change Requests and business events.

#### Capability Manifest

Every capability publishes a machine-readable manifest containing:

- Stable name, version, and human description
- Input and output schemas
- Business purpose and domain ownership
- Data and relationships read
- Fields and relationships written
- Possible downstream systems and side effects
- Required authorization action
- Supported purposes and channels
- Risk and audit classification
- Whether it supports simulation
- Whether direct execution is permitted
- Required workflow and approval behavior
- Idempotency and concurrency behavior
- Reversibility or compensation model
- Bulk and agent eligibility
- Failure and reconciliation behavior
- Deprecation and compatibility policy

This is the external equivalent of the typed workflow-block contract. An agent or customer developer should not have to infer risk and behavior from prose documentation alone.

#### Capability Registry and Discovery

The Capability Registry is the searchable catalog of the platform's business surface. It prevents users and agents from loading or reasoning over thousands of irrelevant API definitions.

Discovery follows this pattern:

```text
Intent
  -> capability search
  -> identity- and purpose-filtered results
  -> relevant manifests
  -> plan or direct action
  -> simulation
  -> execution-time authorization
  -> governed execution
```

Registry search respects planning authorization. A subject that cannot know an employee-relations investigation capability exists for a record must not discover it through semantic search.

The registry also exposes compatibility, lifecycle, ownership, and usage status so deprecated or unsafe capabilities are not selected for new automation.

#### Authorization Decision Model

Authorization is a platform capability, not a collection of route guards.

```text
Decision = Subject
         × Action
         × Resource
         × Relationship
         × Fields
         × Purpose
         × Context
         × Channel
         × Policy
```

The model combines:

- Role-based access for broad job responsibilities
- Relationship-based access for manager, employee, HRBP, recruiter, payroll, and compensation relationships
- Attribute-based access for geography, legal entity, worker type, organization, pay group, and jurisdiction
- Field-level access for compensation, identity, medical, investigation, bank, tax, immigration, and other sensitive data
- Action-level access separating read, propose, approve, execute, export, and administer
- Purpose-bound access such as payroll processing, recruiting, investigation, benefits administration, or workforce planning
- Channel and assurance context such as human UI, partner integration, service, or agent

Authorization results are richer than allow or deny. A decision may be:

- Allowed
- Denied
- Conditionally allowed
- Step-up authentication required
- Additional approval required
- Allowed with field filtering
- Allowed with scope, value, volume, time, or retention constraints

A decision includes visible and hidden fields, obligations, constraints, reasons, and policy version. Material decisions become part of the transaction's reproducibility envelope and ledger history.

#### Human, Service, and Agent Identity

Agent identity is separate from human identity.

```text
Human identity:
    user_earl

Agent identity:
    acme.compensation_assistant

Delegation:
    agent acting on behalf of user_earl
```

Effective agent authority is an intersection:

```text
human authority
∩ agent authority
∩ workflow authority
∩ tenant policy
∩ current context
```

An agent never inherits a human's complete token or authority by default. A user who may propose, approve, and execute compensation changes may delegate an agent that can only read and propose.

The ledger records the human principal, agent or service principal, delegation grant, purpose, channel, assurance level, and effective authorization decision separately.

#### Capabilities, Not Credentials

Agents, workflow blocks, and customer automations receive capability grants rather than raw database or external-system credentials.

```text
Agent or workflow
       │
       ▼
Capability Gateway
       │
   authorize
       │
       ▼
Domain service
       │
       ▼
Integration service
       │
resolve governed secret
       │
       ▼
External system
```

The model may invoke `workday.worker.sync`; it does not receive a Workday OAuth secret. Credential ownership, rotation, tenant isolation, and external invocation remain inside the integration boundary.

#### Planning and Execution Authorization

Agents and workflow designers pass two distinct authorization stages.

Planning authorization determines which capabilities, schemas, fields, and records may be discovered or used while constructing and simulating a plan.

Execution authorization reevaluates the actor, agent, resource, fields, purpose, current state, policy version, approvals, effective date, volume, and risk immediately before a side effect.

```text
Planning authorization
          ≠
Execution authorization

Approval
  + current authorization
  + current-state validation
  + valid proposal
  = executable action
```

Authorization, approval, and validation remain independent controls. A valid approval does not override a current denial, and a current authorization does not substitute for a required approval.

#### Capability Risk Classes

Every capability has a default risk class that tenant policy may strengthen:

| Class | Meaning  | Example                                                    | Typical agent posture                       |
| ----- | -------- | ---------------------------------------------------------- | ------------------------------------------- |
| R0    | Read     | Employee directory lookup                                  | Autonomous within field and query policy    |
| R1    | Low      | Update preferred name                                      | Autonomous with audit or lightweight review |
| R2    | Moderate | Change manager or schedule                                 | Simulate and use a governed workflow        |
| R3    | High     | Change compensation or benefit election                    | Human approval required                     |
| R4    | Critical | Terminate worker, release payroll, grant privileged access | Dual approval and step-up authentication    |

Risk is contextual. A nominally low-risk action may escalate because it is bulk, cross-border, near a payroll cutoff, affects a protected population, changes privileged access, or has unusual financial exposure.

#### Bulk and Scale-Amplified Permissions

Single-record and bulk capabilities have distinct permissions and risk treatment.

```text
worker.compensation.change
workers.bulk.compensation.change

access.revoke
access.bulk_revoke

worker.message
workers.mass_message
```

Authorization considers population size, selection method, estimated impact, rate limits, rollback strategy, approval requirements, and blast radius. Permission to change one valid worker does not imply permission to change 70,000 workers.

Large operations should be compiled into versioned, resumable workflows with checkpoints and aggregate reconciliation rather than executed as unrestricted agent API loops.

#### Query, Aggregation, and Export Authorization

Read authorization covers more than whether a subject can view one field on one record. It governs:

- Record and relationship scope
- Filterable and sortable fields
- Sensitive field combinations
- Aggregation and minimum cohort size
- Population size and enumeration
- Export and download
- Purpose and retention
- Cross-tenant and cross-region boundaries
- Reidentification risk

A subject may be allowed to view compensation for direct reports, see organization-level aggregates above a minimum cohort size, and have no export permission. Agents receive the same constraints.

This protects against composition attacks where individually permitted fields or calls create an impermissible sensitive population or inference.

#### Derived-Data Authorization Lineage

Derived outputs inherit authorization and classification lineage from their source data. A summary, score, list, AI response, report, or workflow output does not become less sensitive merely because it omits the original fields.

The platform propagates metadata such as:

- Data classification
- Permitted purposes
- Record and population scope
- Derived-data restrictions
- Retention and expiration
- Export restrictions
- Source decision and policy versions

The effective output classification is at least as restrictive as its material inputs unless an explicit, governed declassification rule applies.

This lineage travels through agent context, temporary storage, workflow outputs, reports, analytics, and downstream integrations.

#### Policy Introspection

The authorization plane exposes safe introspection capabilities:

- `authz.capabilities.list`
- `authz.capability.check`
- `authz.resource.explain`
- `authz.decision.explain`

These APIs explain whether a subject may read, propose, approve, execute, export, or administer; which obligations remain; and which policy produced the result. They do not reveal hidden records, sensitive fields, or restricted capability existence merely through probing.

Policy introspection allows an agent to adapt its plan—for example, adding Compensation VP approval—rather than repeatedly attempting forbidden actions.

#### Authorization Observability and Threat Detection

Authorization decisions feed security and operational projections while preserving privacy.

The platform monitors:

- Allowed, denied, conditional, and step-up-required decisions
- Denials by capability, field, population, purpose, agent, and policy
- Unusual record enumeration or export behavior
- Sudden changes in an agent's volume or capability mix
- Repeated attempts to access sensitive domains
- Cross-region, bulk, or privilege-escalation patterns
- Delegation misuse and expired authority

Detectors can reduce or suspend an agent's capability grants and create a governed security incident. A surge from 40 worker reads per day to 17,000 is treated as a behavioral condition, not simply thousands of unrelated authorization decisions.

#### Composable Customer Behavior

Capabilities are the stable primitives from which customers build differentiated HR behavior.

```text
Promotion workflow
  ├── worker.job.change
  ├── compensation.adjust
  ├── equity.eligibility.evaluate
  ├── learning.assignment.create
  ├── iam.entitlements.recalculate
  ├── payroll.worker.refresh
  └── communications.send
```

Different customers may compose different governed behaviors without requiring an HCM Next product release for each variation. Composition remains type-checked, permission-checked, simulated, versioned, approved, and auditable.

Customer configuration cannot weaken the underlying capability's authority, risk, privacy, or reconciliation requirements.

#### Agent Plane

Agents should compose and propose deterministic workflows rather than execute unrestricted API loops for material or large-scale operations.

```text
User intent
  -> agent identity and delegation
  -> capability discovery
  -> authorized capability subset
  -> typed plan or workflow generation
  -> static validation
  -> simulation
  -> human review when required
  -> publication or transaction approval
  -> execution-time authorization
  -> deterministic workflow runtime
  -> ledger, telemetry, reconciliation, and repair
```

The agent interprets intent and designs behavior. The platform validates and executes behavior. This boundary makes complex and bulk operations scalable, deterministic, reviewable, and recoverable.

#### API Lifecycle and Compatibility

A dense capability fabric requires disciplined lifecycle governance:

- Stable capability identities independent of transport routes
- Explicit schema and semantic versions
- Compatibility and deprecation policies
- Consumer discovery and migration windows
- Idempotency and retry contracts
- Standard error and obligation taxonomy
- Tenant-aware rate and concurrency limits
- Sandbox and simulation support
- Contract and policy tests across transports
- Usage, risk, failure, and adoption telemetry

No transport adapter or internal service may silently reinterpret a capability. Material semantic change creates a new version and an auditable migration path.

The combined platform thesis is:

> The ledger gives HCM Next memory and truth. The workflow engine gives it deterministic behavior. The capability plane gives it composability. Authorization makes that composability safe. Agents turn governed capabilities into customer-specific software.

### 9.9 Corporate Scope and Inheritance

A corporate hierarchy exists inside a tenant; it does not replace tenancy.

```text
Tenant / customer security boundary
        │
        ▼
Enterprise group
        │
        ├── Company A
        │     ├── Legal entity A-US
        │     └── Legal entity A-CA
        │
        ├── Company B
        └── Company C
              │
              └── business units, departments,
                  cost centers, locations, and teams
```

The tenant remains the hard isolation, identity, encryption, and customer-contract boundary. Parent companies, subsidiaries, legal entities, and organizational structures are governed scopes within that boundary. A subsidiary is not forced to become a separate SaaS tenant merely because it needs local policy, payroll, access, or data restrictions.

#### Scoped Resources

Every configurable resource declares an owner scope and availability scope. Scope types may include:

- Tenant
- Enterprise group
- Company
- Legal entity
- Organization unit
- Location

Resources requiring scope include:

- Workflow definitions and templates
- Policies and permission policies
- Metadata and reference data
- Job families, skills, and learning content
- Pay bands and benefit plans
- Integrations and credentials
- Agent definitions and capability grants
- Reports, dashboards, and analytics
- Documents, branding, communications, and UI configuration

The owner controls publication and evolution. Consumers receive only the explicitly granted ability to read, execute, extend, aggregate, or administer.

#### Inheritance and Overrides

Child scopes inherit eligible resources unless they define a permitted local override.

```text
Enterprise termination workflow
          │
          ▼
     company defaults
       /          \
     US         Colombia
     │              │
 local policy   local policy
 override       + legal review
```

Inheritance avoids cloning complete workflows and policies for every company. Local configuration expresses a typed variation from the inherited resource, retains lineage to its source version, and can be revalidated when the parent changes.

Not every resource supports the same inheritance behavior:

| Sharing posture      | Typical resources                                                                            |
| -------------------- | -------------------------------------------------------------------------------------------- |
| Naturally shareable  | Workflow templates, job families, skills, learning content, branding, capability definitions |
| Optionally shareable | Pay bands, benefit plans, candidate pools, integrations, reports, documents                  |
| Normally isolated    | Compensation, payroll, bank data, investigations, medical data, disciplinary records         |

A resource manifest declares whether it may be inherited, overridden, extended, referenced, aggregated, or shared at all.

#### Directional Sharing

Sharing is never a single boolean. It identifies:

- Owner scope
- Consumer scopes
- Direction of visibility
- Permitted actions
- Field and record boundaries
- Purpose and retention
- Whether derived or aggregate access is allowed
- Whether downstream resharing is prohibited

Examples:

```text
Parent-owned workflow
  consumers: Subsidiary A, Subsidiary B
  permissions: read, execute
  prohibited: modify

Subsidiary-owned workforce data
  consumer: Parent
  permissions: aggregate only
  prohibited: worker-level compensation read
```

This permits corporate reporting without automatically granting parent administrators access to every sensitive subsidiary record.

#### Configuration and Security Precedence

Configuration and security do not use identical inheritance rules.

Configuration generally resolves toward the most specific valid scope:

```text
company or legal-entity override
  -> enterprise-group default
  -> tenant default
  -> platform default
```

Security restrictions generally accumulate from broader to narrower scopes. A child cannot weaken a parent denial or mandatory obligation unless the parent policy explicitly marks that rule as delegable.

```text
most specific valid configuration wins

but

broader mandatory security restriction survives
```

Every resolution result identifies the selected resource, inherited sources, overrides, denied candidates, and policy versions so behavior remains explainable.

#### Person and Employment Scope

Person and employment are scoped differently.

```text
Enterprise person: Jane
        │
        ├── Employment A
        │     Company US
        │
        └── Employment B
              Company Canada
```

The person identity may exist at the tenant or enterprise-group level. Each employment belongs to a company and legal entity. Permissions may expose one employment, both employments, or selected shared person fields without treating Jane as two unrelated humans.

This supports dual employment, cross-company transfers, acquisitions, international assignments, contractor conversion, rehire, and former-worker continuity while preserving entity-specific compensation, payroll, benefits, tax, and case isolation.

#### Cross-Company Transactions

Cross-company workflows are first-class governed transactions:

```text
Intercompany transfer
  ├── close or modify Company A employment
  ├── finalize Company A payroll obligations
  ├── revoke Company A-specific access
  ├── preserve permitted corporate identity
  ├── create Company B employment
  ├── enroll Company B payroll and benefits
  └── provision Company B access and schedule
```

The transaction may span isolated resources while preserving one causal chain, explicit authority for every participating scope, separate approvals, effective-dated coordination, and cross-company reconciliation.

#### Hierarchical Agent Configuration

Agent definitions, tools, and capability grants follow the same hierarchy.

A subsidiary agent may inherit a corporate template, add local labor or payroll capabilities, and remain prohibited from other subsidiaries' worker-level data. Agent inheritance cannot expand effective authority beyond the intersection of parent restrictions, local policy, delegated human authority, purpose, and current context.

Scoped resolution is exposed through governed capabilities such as:

- `scope.resources.resolve`
- `scope.resources.share`
- `scope.resources.override`
- `scope.resources.explain`
- `scope.inheritance.validate`

### 9.10 Organization-Scoped Authorization

Organization scope is a first-class authorization input, not an incidental attribute on a user role.

The model keeps these concepts distinct:

| Concept           | Meaning                                                      |
| ----------------- | ------------------------------------------------------------ |
| Tenant            | Hard customer, isolation, identity, and encryption boundary  |
| Organization      | Business scope within the tenant                             |
| Legal entity      | Statutory, contractual, payroll, and employment scope        |
| Organization unit | Department, division, business unit, team, or similar node   |
| Person            | Human identity across lifecycle and eligible relationships   |
| Employment        | A person's relationship to a company and legal entity        |
| Principal         | Authenticated human, service, agent, or integration identity |

The same model supports:

```text
Single organization
Tenant A
└── Company A

Multi-organization
Tenant B
├── Company A
├── Company B
└── Company C

Parent and subsidiaries
Tenant C
└── Parent Corp
    ├── Subsidiary US
    │   ├── Legal Entity US-1
    │   └── Legal Entity US-2
    ├── Subsidiary Colombia
    └── Subsidiary UK
```

Single-company customers receive sensible defaults and do not need to manage a complex hierarchy. The hierarchy becomes visible only when their operating model requires it.

#### Authentication Establishes Principal Identity

Authentication answers who or what is making the request. It does not decide what that principal may do.

A `PrincipalContext` includes:

- Principal ID and type: human, service, agent, or integration
- Tenant ID
- Upstream identity provider and subject
- Session and credential identity
- Authentication method and strength
- Session age and step-up status
- Acting-as or on-behalf-of relationship
- Delegating principal and delegation grant
- Organization memberships and asserted relationships
- Device, workload, network, or risk context when applicable

```text
Authentication
  -> tenant resolution
  -> canonical PrincipalContext
  -> authorization evaluation
```

A parent-company identity provider may authenticate a human whose authorization is limited to one subsidiary. Role and business authority are resolved by authorization policy, not hardcoded into authentication.

#### Multidimensional Decision

The effective decision evaluates:

```text
Decision = Principal
         × Tenant
         × Organization scope
         × Capability
         × Resource
         × Data domain and fields
         × Relationship
         × Purpose
         × Context
         × Policy
```

Decision outcomes include:

- Allow
- Deny
- Conditional allow
- Step-up authentication required
- Approval required

The result may also contain authorized scope, visible fields, filter constraints, population limits, export rules, required workflow obligations, explanation, and all policy versions used.

#### Six Authorization Layers

Authorization is enforced through six connected layers.

##### 1. Capability-Level Authorization

Determines whether the principal may invoke an operation at all. Read, propose, approve, execute, repair, override, release, export, and administer are independent actions.

```text
compensation.read
compensation.propose
compensation.approve
compensation.execute

workflow.repair
workflow.override
payroll.release
```

##### 2. Resource and Record Authorization

Determines which individual workers, employments, positions, cases, plans, workflows, or other records the capability may target.

Relationship and graph rules may grant access to self, direct reports, indirect reports, an assigned organization, a requisition, a pay group, or another governed population.

##### 3. Organization-Scope Authorization

Represents business scope explicitly:

```text
tenant
organization
organization_tree
legal_entity
location
department
cost_center
```

Examples include one company, descendants of a regional holding company, one legal entity, or corporate-wide aggregate access without underlying employee-row access.

An `AuthorizationScope` is evaluated against effective-dated organization relationships. Moving a worker or principal between organizations can therefore invalidate authority.

##### 4. Data-Domain Authorization

Public policy refers to stable business domains rather than physical database tables:

```text
worker.core
worker.contact
worker.compensation
worker.tax
worker.bank
worker.performance
worker.medical
worker.employee_relations
worker.immigration
```

One domain may span several physical tables, and one table may contain fields from several policy domains. This prevents schema refactoring from silently changing authorization meaning.

Database and storage controls may still enforce domain boundaries as defense in depth.

##### 5. Field Authorization

The decision identifies fields that may be read, filtered, written, approved, exported, or returned. API serialization applies the authorized field mask automatically.

For example, a manager may receive name, job, location, and manager fields; compensation staff may additionally receive base salary and bonus target; payroll may receive bank, tax, and deduction data while remaining unable to access performance or investigation notes.

A capability cannot use a hidden field for policy, filtering, AI context, or derived output unless the decision explicitly grants that use.

##### 6. Population and Query Authorization

Query authorization governs the power created by combining permitted records and fields. It includes:

- Allowed filters, joins, and sort keys
- Sensitive attribute combinations
- Row and population limits
- Aggregation permission and minimum cohort size
- Enumeration and pagination limits
- Export size, format, and destination
- Purpose, retention, and reuse
- Reidentification and inference risk

A manager may search direct and indirect reports up to a bounded population. An analytics agent may receive aggregates only above a minimum cohort size. Payroll may receive an approved bulk export for one pay group. Individual record access does not automatically grant arbitrary population analysis.

#### Customer-Configurable Policies

Customers configure versioned authorization policy without deploying code. A policy declares:

- Owner and applicable organization scope
- Subject roles, principal types, groups, relationships, or named identities
- Capabilities and actions
- Resource types and organization scope
- Data domains and fields
- Purpose and contextual conditions
- Allow, deny, or conditional effect
- Inheritance and delegation behavior
- Obligations and limits
- Effective period and version

Example policy intent:

```text
Colombia HRBP compensation access

scope: Colombia company
subjects: HRBP
actions: compensation.read, compensation.propose
resources: workers within subject organization scope
fields: base salary, bonus target
deny: bank and tax data
purpose: annual review
effective version: 7
```

Policies are drafted, validated, simulated, reviewed, published, monitored, and rolled back through governed capabilities. Policy simulation must show affected principals, resources, capabilities, fields, and obligations before publication.

#### Role Templates and Policy Composition

HCM Next provides standard role templates such as Employee, Manager, HRBP, Recruiter, Payroll Admin, Compensation Partner, Benefits Admin, HRIS Admin, Security Admin, and Auditor.

Customers compose those templates with organization scopes and policy changes:

```text
Base role: HRBP
  + scope: Colombia
  + immigration.read
  - employee_relations.case.read
```

Effective authorization may combine:

```text
platform safety policy
  -> tenant policy
  -> enterprise-group policy
  -> company policy
  -> legal-entity policy
  -> role grants
  -> relationship rules
  -> individual delegation
  -> purpose and request context
  -> explicit denies
  -> obligations
```

The engine produces one explainable decision without treating the last evaluated policy as an implicit winner.

#### Allow, Deny, and Delegation Inheritance

Permission grants are scoped and inherit only when explicitly configured. Deny rules and mandatory obligations inherit downward by default.

```text
ALLOW
  inheritable: explicit

DENY
  inheritable: default
  delegable: explicit
```

A parent policy denying agents access to medical accommodation data cannot be weakened by a subsidiary allow unless the parent explicitly delegates that policy authority.

Conflicting rules resolve through documented precedence, with explicit non-delegable denies taking priority. The decision explanation identifies every grant, restriction, inherited rule, and override considered.

#### Authorization Obligations Drive Workflow

Authorization can permit an action while adding obligations that the workflow must satisfy.

For a compensation proposal, obligations might include:

- Finance approval above 10%
- Compensation VP approval above 20%
- Local HR review for a specified country
- Human review when initiated by an agent
- Step-up authentication before execution
- Reconciliation before closure

The workflow runtime consumes these obligations as typed requirements. Policy can therefore influence approval and control behavior without silently rewriting the workflow graph.

Obligations are versioned, ledgered, and tracked to completion. Workflow configuration may add stronger obligations but cannot discard mandatory authorization obligations.

#### Cross-Organization Resource Access

Every shareable resource may distinguish:

- `owner_scope`
- `visibility_scope`
- `execution_scope`
- `administration_scope`
- `aggregation_scope`

These scopes do not need to be identical. A parent-owned workflow may be visible and executable by subsidiaries but modifiable only by corporate HRIS. A subsidiary compensation report may expose aggregates to the parent while keeping worker-level rows local. A payroll connector may remain usable only inside one legal entity.

#### Data-Layer Defense in Depth

API-level authorization is authoritative, but sensitive storage also carries tenant and organization identifiers where appropriate.

Data access requires an evaluated `AuthorizationScope`; repositories do not accept only an unconstrained record ID for sensitive operations.

```text
authorized repository request
  = PrincipalContext
  + AuthorizationScope
  + resource identifier
  + permitted domains and fields
  + purpose
```

Defense-in-depth controls include tenant and organization predicates, scoped database roles where practical, field encryption, audit, policy-aware serializers, and tests proving that missing scope fails closed.

The data layer does not independently invent business authorization policy. It enforces the scope and restrictions produced by the authorization plane and prevents accidental bypass by application code.

#### Agent Effective Authority

For agents, authorization resolves as:

```text
human or service authority
  ∩ agent configuration
  ∩ organization scope
  ∩ capability policy
  ∩ data-domain and field policy
  ∩ purpose
  ∩ current context
```

A corporate HR director may use a Colombia recruiting agent that is limited to Colombian candidates, requisitions, and offer proposals. The director's broader payroll or compensation rights do not flow into that agent.

Organization scope, population limits, derived-data lineage, planning authorization, and execution authorization all remain in force for agents.

#### Authorization Decision Ledger and Operations

Sensitive decisions record:

- Principal, agent, service, and delegation context
- Tenant and resolved organization scope
- Capability, action, resource, and relationship
- Requested and permitted data domains and fields
- Purpose and request context
- Policy and role-template versions
- Allow, deny, conditional, step-up, or approval-required result
- Constraints, obligations, and explanation

Operational projections monitor decision volume, denials, scope expansion, unusual population queries, export behavior, step-up failures, unfulfilled obligations, and changes in agent behavior.

The security architecture is:

```text
Identity plane
    │
    ▼
Authentication -> PrincipalContext
    │
    ▼
Authorization plane
    ├── capability policy
    ├── organization graph
    ├── resource and relationship rules
    ├── data-domain and field policy
    ├── query and population policy
    ├── purpose and context
    ├── inherited denies
    └── obligations
              │
              ▼
        AuthZ decision
         ├── API execution
         ├── workflow behavior
         └── query filtering
              │
              ▼
            Ledger
```

The typed organization-unit, relationship, worker-assignment, role-binding,
current/proposed scope, and repository-enforcement details are maintained in
[the Organization Scope and Authorization Contract](organization-scope-and-authz.md).

### 9.11 Globalization Platform

Globalization is a horizontal platform capability used by People, Workforce, Talent, Rewards, Experience, Access, workflows, documents, analytics, integrations, and agents.

```text
People   Payroll   Time   Talent   Access   Experience
   │        │        │      │       │          │
   └────────┴────────┴──────┴───────┴──────────┘
                         │
               Globalization Platform
                         │
       ┌─────────────────┼──────────────────┐
       ▼                 ▼                  ▼
  Localization        Financial          Calendar
  language            currency           timezone
  formatting          FX and rounding    holidays
  names/addresses     rate purpose       workdays/pay periods
       │                 │                  │
       └─────────────────┼──────────────────┘
                         ▼
                    Jurisdiction
                  policy and rules
```

#### LocaleContext

Every API request, workflow execution, worker transaction, notification, and document render can resolve a `LocaleContext`.

```text
LocaleContext

language
locale
country
currency
timezone
calendar
number_format
date_format
name_format
address_format
measurement_system
```

These values are independent. A worker may prefer Spanish, use an English manager interface, be employed by a Colombian legal entity, be paid in COP, and work temporarily from Florida.

The platform must never assume:

```text
language = country
country = currency
currency = payroll country
timezone = legal-entity timezone
current location = legal jurisdiction
```

Locale resolution is concern-specific. A single transaction may resolve different contexts for UI presentation, employee communication, document rendering, reporting, and scheduling. `LocaleContext` may provide a non-authoritative regional hint for presentation, but legal authority comes only from `LegalContext` and `JurisdictionContext`; locale never selects law.

#### Scope and Precedence

Globalization configuration may originate from:

```text
platform default
  -> tenant default
  -> enterprise group
  -> company
  -> legal entity
  -> location
  -> employment
  -> worker preference
  -> request override when permitted
```

The resolver does not blindly merge this hierarchy into one locale. It resolves each concern independently:

| Concern               | Likely controlling scopes                                        |
| --------------------- | ---------------------------------------------------------------- |
| UI language           | Principal or worker preference, tenant-supported languages       |
| Business language     | Company or organization policy                                   |
| Legal-document locale | Legal entity, jurisdiction, document policy, worker language     |
| Payroll currency      | Employment, pay group, legal entity                              |
| Reporting currency    | Company, enterprise group, report definition                     |
| Working timezone      | Work location, schedule, employment arrangement                  |
| Payroll timezone      | Pay group or payroll calendar                                    |
| Legal jurisdiction    | Employment, work location, tax facts, and effective-dated policy |

Every resolution result records source scopes, fallback path, effective date, and relevant configuration versions.

#### Language and Content

The platform distinguishes:

- `preferred_language` — language a person wants for communication
- `business_language` — default language used by the company or team
- `content_language` — language of a specific document, template, course, policy, or message

Translatable resources use versioned localized variants rather than embedding display strings in workflow logic:

- Job and position titles
- Workflow and task labels
- Policy names and explanations
- Notification templates
- Courses and learning paths
- Benefit descriptions
- Help and knowledge articles
- Forms, validation messages, and field labels
- Employee communications

Locale fallback is explicit and observable:

```text
es-CO
  -> es
  -> tenant default
  -> platform default
```

Missing required legal or employee-facing content may block execution instead of silently falling back. Fallback permissibility is declared by resource type, jurisdiction, and purpose.

#### Canonical Values and Presentation

Canonical APIs return structured semantic values rather than locale-formatted strings.

```json
{
  "salary": {
    "amount": "145000.00",
    "currency": "USD"
  },
  "effectiveDate": "2026-09-01"
}
```

They do not return canonical monetary data as `"$145,000.00"` or rewrite dates based on the caller's locale.

Clients may provide presentation hints such as language and timezone for translated labels, human-readable explanations, notifications, and document rendering. Those hints never change the underlying domain value or legal context.

This stable representation is required for integrations, agents, deterministic workflows, replay, and reconciliation.

#### Money and Currency

Every monetary value is explicit and uses fixed-precision decimal arithmetic:

```text
Money
  amount_decimal
  currency_code
```

ISO 4217 currency codes identify currencies. The platform distinguishes:

- Transaction currency
- Worker pay currency
- Payroll currency
- Company base currency
- Reporting currency
- Budget currency
- Settlement currency where relevant

The original amount and currency remain authoritative. A converted amount is a derived value with provenance.

Financial rules also declare currency-specific scale, rounding mode, minimum unit, and domain purpose. Payroll, tax, compensation planning, and presentation may require different rounding rules.

#### Exchange-Rate Semantics

No material conversion silently uses today's exchange rate. Every conversion identifies:

- Source and target amounts
- Source and target currencies
- Exchange rate
- Rate direction and precision
- Rate source
- Rate date or effective interval
- Rate type or purpose
- Conversion reason
- Rounding rule

Rate purposes may include:

- Spot estimate
- Monthly corporate reporting rate
- Payroll-period rate
- Budget rate
- Historical accounting rate
- Compensation comparison rate

The ledger records the conversion context when financially material. Historical replay uses the rate actually applied. Current presentation may show a new reference estimate, clearly labeled as current rather than historical truth.

#### Business Time Types

The platform does not represent every temporal concept as a generic timestamp. It distinguishes:

| Type          | Meaning                                                      |
| ------------- | ------------------------------------------------------------ |
| Instant       | An exact point in universal time                             |
| LocalDate     | A business date without a time or UTC conversion             |
| LocalTime     | A wall-clock time without a date                             |
| ZonedDateTime | A local date and time tied to a timezone                     |
| PayPeriod     | A governed payroll interval with cutoff and processing rules |
| BusinessDay   | A date evaluated under a specific business calendar          |

System instants are stored in UTC while preserving the business timezone that supplied meaning. A promotion effective on `2026-09-01` remains a LocalDate unless the business rule explicitly requires a time and zone.

Daylight-saving transitions, ambiguous local times, nonexistent local times, and timezone-rule version changes must be handled explicitly for schedules, clocks, deadlines, payroll cutoffs, and audit reconstruction.

#### Calendar Service

Calendars are scoped, versioned resources that may vary by country, region, legal entity, organization, location, pay group, or work schedule.

A calendar can define:

- Week start and weekend pattern
- National and regional holidays
- Company holidays and closures
- Bank holidays
- Business days
- Work schedules
- Payroll periods and cutoffs
- Exception days and temporary closures

Workflows use semantic calendar capabilities such as:

- Add a number of business days
- Resolve the next working day
- Determine a local payroll cutoff
- Calculate an SLA excluding applicable holidays
- Resolve the pay period containing an effective date

Calendar computations record the calendar ID and version so future changes do not rewrite historical deadlines or decisions.

#### Names and Scripts

Person names support different cultural structures and writing systems rather than assuming first, middle, and last name.

```text
PersonName
  given_names[]
  family_names[]
  preferred_name
  legal_name
  display_name
  prefix
  suffix
  locale
  script
  romanized_form
```

Legal, preferred, display, and romanized forms remain distinct. Rendering order and punctuation are locale-sensitive. Search supports normalized forms without discarding the authoritative native-script value.

Name validation is jurisdiction- and document-specific. The core model must not force every person into a Western naming pattern.

#### Addresses and Phone Numbers

Addresses use canonical structured components with country-aware validation and rendering:

```text
country_code
administrative_area
locality
postal_code
address_lines[]
```

Required fields, ordering, labels, postal validation, and rendering come from address rules for the relevant country or jurisdiction. The model does not assume US street, city, state, and ZIP semantics.

Phone numbers are stored in canonical international form where possible and rendered for human consumption according to locale. Presentation punctuation is not authoritative data.

#### Numbers, Percentages, and Measurements

Canonical APIs carry semantic numeric values, units, currencies, and percentages without localized separators or symbols. Presentation handles:

- Decimal and grouping separators
- Percent formatting
- Measurement systems and units
- Localized negative and accounting formats
- Appropriate precision and rounding display

Parsing human input is locale-aware and must reject ambiguous values rather than silently interpreting them under the wrong locale.

#### Localization Versus Jurisdiction

Localization affects presentation and communication. Jurisdiction changes business and legal semantics.

```text
Localization
  language, labels, formatting, rendering

Jurisdiction
  eligibility, required data, policy, approval,
  retention, documents, payroll, tax, labor rules
```

A workflow graph uses stable language-neutral identifiers. Localized labels and instructions are separate resources. Jurisdiction-specific policies may add, remove, or constrain business steps through governed scoped configuration.

Changing UI language must never change business policy. Changing a worker's effective legal or work jurisdiction may require preflight, approval, document, payroll, retention, or workflow reevaluation.

#### Localized and Legal Documents

Document definitions have versioned locale and jurisdiction variants:

```text
Offer Letter v12
  ├── en-US / United States
  ├── es-CO / Colombia
  └── pt-BR / Brazil
```

The document ledger records:

- Template ID and version
- Language and locale
- Jurisdiction
- LocaleContext and source configuration versions
- Rendering engine version
- Input-data fingerprint
- Rendered-document hash
- Delivery and acknowledgment evidence

This makes it possible to prove which exact document the worker received and why that variant was selected.

#### Agents and Globalization

Agents reason over canonical structured values and receive explicit context for each audience and concern.

For example, an agent may analyze a Colombian worker's canonical compensation facts, prepare an employee message in `es-CO`, and prepare a manager summary in `en-US` without converting or reformatting authoritative data internally.

Capability manifests may declare:

- Supported languages and locales
- Supported currencies
- Jurisdiction scope
- Calendar requirements
- Localized-resource requirements
- Whether fallback is allowed
- Whether agent-generated translation requires human review

Agent translation does not automatically become an approved legal or policy translation. Regulated documents and high-risk communication use reviewed, versioned localized resources unless policy explicitly permits another path.

#### Globalization Capabilities

The capability plane exposes typed globalization operations:

```text
locale.resolve
locale.languages.list
locale.formats.get

currency.convert
currency.rates.get
currency.round

calendar.business_days.add
calendar.holidays.list
calendar.pay_period.resolve

jurisdiction.resolve

translation.resources.get
translation.resources.publish
translation.fallback.explain
```

These operations use the same authorization, scope, versioning, simulation, ledger, and observability rules as every other platform capability.

#### Globalization Governance

Globalization resources have named owners and review requirements. Corporate teams may own global defaults, while legal entities own local documents, calendars, payroll rules, and jurisdiction-specific content.

Publication must validate:

- Supported locale and jurisdiction combinations
- Required translations and legal variants
- Fallback safety
- Currency scale and rounding
- FX source and staleness
- Timezone and calendar consistency
- Effective dates
- Downstream integration compatibility
- Worker-facing and legal-document completeness

The globalization layer preserves both the canonical value and the contextual facts necessary to explain its use.

### 9.12 Legal and Compliance Plane

The Legal and Compliance Plane evaluates the organization's authority and obligations independently from actor authorization.

```text
Business intent
      │
      ├──────────────► AuthZ Plane
      │                 may this principal act?
      │
      └──────────────► Legal and Compliance Plane
                        may the organization act or process?
                        what obligations and prohibitions apply?
                              │
                              ▼
                    Workflow and Transaction Plan
                              │
                       simulation + human gates
                              │
                              ▼
                           execution
                              │
                              ▼
                            ledger
```

Law does not implement business transactions itself. It constrains and modifies generic capabilities and workflows through typed effects.

#### LegalContext

Every material transaction and processing operation resolves a `LegalContext` separately from `LocaleContext`.

```text
LegalContext

tenant
enterprise_group
parent_company
company
legal_entity

worker_country_of_work
worker_residence
employment_jurisdiction
payroll_jurisdiction
tax_jurisdictions[]

work_location
remote_work_location

data_subject_jurisdiction
data_processing_regions[]
data_storage_regions[]

worker_type
employment_type
contract_type

collective_bargaining_agreement?
works_council?

effective_date
execution_date
```

These scopes resolve independently:

```text
worker location
  ≠ employing entity
  ≠ payroll jurisdiction
  ≠ tax jurisdiction
  ≠ privacy jurisdiction
  ≠ data-processing region
  ≠ data-storage region
```

A single activity may involve several applicable jurisdictions. Resolution records the facts, source systems, effective dates, confidence, and rule versions used. Ambiguous jurisdiction must block or route to legal review rather than silently choose one country.

#### Legal Rule Effects

Given an action and LegalContext, a rule pack returns typed effects:

```text
ALLOW
DENY

REQUIRE_STEP
REQUIRE_APPROVAL
REQUIRE_HUMAN_REVIEW

REQUIRE_NOTICE
REQUIRE_DOCUMENT
REQUIRE_SIGNATURE
REQUIRE_ACKNOWLEDGEMENT

REQUIRE_LEGAL_BASIS
REQUIRE_DPIA
REQUIRE_CONSULTATION

RESTRICT_FIELDS
RESTRICT_PURPOSE
RESTRICT_PROCESSING
RESTRICT_TRANSFER

REQUIRE_RETENTION
REQUIRE_DELETION
LEGAL_HOLD

REQUIRE_WAIT_PERIOD
REQUIRE_DEADLINE

REQUIRE_REPORTING
REQUIRE_EVIDENCE
```

The workflow engine consumes effects as obligations and prohibitions. Mandatory legal effects cannot be removed by a workflow author, agent, subsidiary override, or ordinary administrator.

The same global promotion or hiring workflow can therefore acquire jurisdiction-specific notices, documents, approvals, consultations, waiting periods, evidence, and human gates without cloning the entire graph.

#### Example: Employment Background Checks

A generic hiring flow may be augmented with disclosure, authorization, pre-adverse-action, review, and adverse-action steps when applicable.

```text
Candidate
  -> privacy notice
  -> interview
  -> background-check disclosure
  -> candidate authorization
  -> consumer report
       ├── clear -> continue
       └── adverse information
             -> pre-adverse notice and report
             -> review or waiting period
             -> final decision
             -> adverse-action notice when applicable
  -> offer or close
```

The U.S. Federal Trade Commission describes FCRA duties for employers using consumer reports, including disclosure and authorization before obtaining a report and pre-adverse- and adverse-action procedures when report information contributes to an employment decision. The applicable pack must still account for the transaction's facts and additional state or local requirements. See [FTC guidance for employers](https://www.ftc.gov/business-guidance/resources/using-consumer-reports-what-employers-need-know).

This illustrates the model: a legal pack injects executable states and evidence requirements rather than displaying a reminder.

#### ProcessingContext

Every material personal-data operation can carry a `ProcessingContext`:

- Purpose
- Lawful basis and authority reference
- Data categories and special categories
- Data subjects
- Controller, joint controller, processor, and subprocessors
- Recipients
- Data source
- Storage and processing regions
- Retention policy
- Automated-decision and profiling role
- Cross-border transfer status and mechanism
- Legal rule versions

Authorization to access data does not by itself establish a lawful processing purpose. Effective permission is the intersection of identity authority, organization scope, capability, resource and field access, legal processing authority, purpose limitation, and transfer restrictions.

For example, compensation access granted to an HRBP does not permit an agent to use compensation data for an employee-birthday message when that data is unnecessary for the declared purpose.

#### Lawful Basis and Processing Authority

The platform does not reduce privacy law to a `consent = true` flag. Processing authority records:

- Legal basis category
- Purpose
- Authority or contract reference
- Required assessment, such as a legitimate-interest assessment
- Consent evidence when consent is actually appropriate
- Scope, expiration, withdrawal, and review status

GDPR recognizes several lawful bases, and employment processing commonly relies on legal obligation or contractual necessity rather than consent. Consent in employment requires particular caution because it may not be freely given. Rule packs must preserve that distinction and route uncertain uses to privacy review. See the [official GDPR text](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng/) and [EDPB guidance](https://www.edpb.europa.eu/system/files/2026-04/edpb-summary-consent_en.pdf).

#### Data Classification and Domain Separation

HCM Next classifies sensitive domains explicitly:

```text
PUBLIC_WORKFORCE
INTERNAL
CONFIDENTIAL_HR
RESTRICTED_COMPENSATION
RESTRICTED_PAYROLL
SPECIAL_CATEGORY
MEDICAL
LEGAL_PRIVILEGED
INVESTIGATION
IMMIGRATION
BANKING
IDENTITY_SECRET
```

Classification drives API exposure, encryption boundaries, storage, query combinations, agent eligibility, export, transfer, retention, logging, and audit.

Restricted data is not merely hidden by UI serialization. Where law or risk requires it, domains use separate storage and cryptographic boundaries. EEOC guidance states that covered applicant and employee medical information must be kept confidential and separately maintained, subject to limited disclosure circumstances. See [EEOC medical-information guidance](https://www.eeoc.gov/laws/guidance/enforcement-guidance-disability-related-inquiries-and-medical-examinations-employees).

#### Ledger Privacy and Sensitive Payload Vault

Immutable business chronology must not require indefinite retention of raw sensitive content.

```text
Ledger event
  event type
  subject and field references
  minimal business facts
  actor, policy, and timestamps
  sensitive_payload_ref
           │
           ▼
Encrypted sensitive payload vault
  classification
  retention policy
  legal holds
  geographic restrictions
  access purpose
  per-subject or per-domain key
```

The ledger may preserve that a medical document was received, which workflow used it, and which authorized decision followed without embedding the full document permanently in event JSON.

When law permits or requires destruction, the platform may delete the payload, anonymize it, or cryptographically destroy its key while retaining a non-sensitive, tamper-evident event skeleton. Legal holds and required audit facts remain enforceable.

#### Rule-Driven Retention

Retention is resolved by resource type, jurisdiction, legal entity, purpose, and applicable rule—not by one global employee-data duration.

A `RetentionPolicy` includes:

- Resource and classification
- Jurisdiction and legal entity
- Minimum and maximum retention
- Trigger event such as creation, termination, case closure, or payroll completion
- Suspension and legal-hold behavior
- Destruction review
- Delete action: delete, anonymize, crypto-shred, or archive
- Rule and authority references

For illustration, U.S. Department of Labor guidance describes at least three years for specified FLSA payroll records and two years for certain wage-computation records; USCIS describes Form I-9 retention as three years after hire or one year after employment ends, whichever is later. See [DOL Fact Sheet #21](https://www.dol.gov/agencies/whd/fact-sheets/21-flsa-recordkeeping) and [USCIS Form I-9 retention guidance](https://www.uscis.gov/i-9-central/form-i-9-resources/handbook-for-employers-m-274/100-retaining-form-i-9).

These examples become versioned pack rules, not universal defaults for all records.

#### Legal Holds

A legal hold suspends otherwise-applicable deletion for an explicit case and scope.

Legal holds can cover:

- People and employments
- Documents and communications
- Cases and investigations
- Workflow and approval history
- AI prompts, outputs, and evaluations
- Audit and integration records

The ledger records hold creation, authority, scope, changes, release, and resumed retention. Applying or releasing a hold is a high-risk legal capability with segregation of duties and complete evidence.

A hold restricts destruction; it does not automatically broaden who may access the held data.

#### Data-Subject Rights Workflows

Privacy rights become governed workflows rather than manual administrative checklists.

`DataSubjectRequest` types may include:

- Access
- Rectification
- Erasure
- Restriction
- Portability
- Objection
- Automated-decision review

An access workflow can verify identity, discover data and recipients, apply legal exclusions, redact third-party information, require review, package the response, deliver it securely, and prove completion.

An erasure workflow discovers relevant data and classifies each item as erase, retain under authority, restrict, anonymize, or place on hold. It coordinates processors and recipients, verifies outcomes, and records unresolved exceptions.

Deadlines, identity assurance, jurisdiction, request status, extensions, correspondence, evidence, and decisions are first-class data.

#### Automated Processing Registry

Capability, block, workflow, integration, agent, and ledger metadata can derive an always-current processing inventory.

```text
Capability manifests
  + workflow definitions
  + integration and processor manifests
  + agent definitions
  + observed ledger execution
            │
            ▼
    Processing Registry
```

The registry describes purposes, categories of data subjects and data, recipients, international transfers, retention, regions, security measures, controllers, processors, and actual execution volume.

This supports records of processing activities and reduces dependence on disconnected compliance spreadsheets. Human privacy owners review and approve the resulting inventory; automation supplies evidence rather than declaring legal completeness.

#### Residency and Cross-Border Transfer

Data and processing policies declare:

- Required storage regions
- Allowed processing regions
- Approved processors and subprocessors
- Permitted transfer destinations
- Required transfer mechanisms and assessments
- Encryption and key-location requirements
- Data classification and purpose

A capability request is denied before data transfer when the selected processor, model, integration, or region is not approved for the data and LegalContext.

For example, a US-hosted model cannot receive German employee medical data merely because the agent has field permission. Legal transfer authority and processing-region policy must also allow the operation.

#### Processing Parties and Corporate Relationships

Privacy roles attach to a processing activity, not permanently to a company.

`ProcessingParty` roles include:

- Controller
- Joint controller
- Processor
- Subprocessor
- Recipient

A parent, subsidiary, HCM Next, and payroll provider may occupy different roles for different purposes. Each activity records responsibilities, instructions, agreements, regions, and contact or escalation ownership.

This connects corporate hierarchy to compliance without assuming the parent automatically controls or may inspect every subsidiary processing activity.

#### AI Legal Policy

Every AI use declares:

- Purpose
- Decision role: suggest, score, recommend, prepare, or decide
- Employment outcome affected
- Data and special-category inputs
- Model, provider, and processing regions
- Jurisdictions and affected people
- Human oversight
- Notice and contestability
- Evaluation, bias, and risk controls
- Retention and audit policy

The legal plane can classify an AI use and return obligations such as human oversight, approved models and datasets, notices, impact assessments, enhanced logging, evaluation controls, and prohibition on autonomous final action.

The EU AI Act treats specified employment uses as high risk. Current European Commission guidance states that high-risk rules for areas including employment apply from December 2, 2027, while other provisions have separate dates. The timeline itself must be maintained as effective-dated legal content, not hardcoded forever. See the [European Commission AI Act timeline](https://digital-strategy.ec.europa.eu/en/faqs/navigating-ai-act) and [official AI Act text](https://eur-lex.europa.eu/legal-content/EN/TXT/PDF/?uri=OJ%3AL_202401689).

For material employment actions:

```text
AI suggests, scores, recommends, or prepares
          │
          ▼
Recommendation recorded
          │
          ▼
Human decision required when policy says so
          │
          ▼
Separate governed employment transaction
```

An agent recommendation never silently becomes the final hiring, promotion, discipline, scheduling, compensation, or termination decision.

#### Modular Legal Packs

Legal content is organized into composable packs rather than a single `GDPR mode` or country switch.

Examples include:

```text
Privacy
  EU.GDPR
  UK.UK_GDPR
  US.CA.CCPA
  BR.LGPD
  CA.PIPEDA
  CA.QC.Law25
  JP.APPI

Employment
WageAndHour
Payroll
Recruiting
Immigration
Benefits
LaborRelations
Safety
AI
```

Employment packs may cover hiring, termination, notice, probation, protected leave, accommodations, classification, discrimination, wage and hour, breaks, scheduling, deductions, pay transparency, work authorization, statutory benefits, collective labor obligations, safety reporting, privacy, biometrics, monitoring, and automated decisions.

California's privacy law applies to covered businesses' employee-personal-information processing, and the California Privacy Protection Agency is actively examining employee-data rules. The applicable pack must follow enacted requirements and separately track proposals or preliminary rulemaking. See [CPPA employee-data materials](https://cppa.ca.gov/regulations/employee_data.html).

#### Obligation Resolver

Applicable requirements may come from several sources:

```text
federal or national law
  + state, province, region, or local law
  + regulation and regulator guidance
  + collective bargaining agreement
  + works-council agreement
  + employment contract
  + company policy
  = effective obligation set
```

There is no universal precedence rule across every jurisdiction and source. Each pack defines conflict, minimum-standard, cumulative-obligation, exception, and consultation behavior. Ambiguity becomes a counsel-review obligation.

Contracts, CBAs, and company policies use the same versioned rule interface while preserving their distinct source type, owner, and confidentiality.

#### Effective Dating and Legal Time

Legal rules are versioned and effective-dated.

```text
Rule v7
  effective 2026-01-01 through 2026-12-31

Rule v8
  effective 2027-01-01 onward
```

Evaluation distinguishes:

- Law and contract applicable to the business effective date
- Current authorization and security policy at execution
- Rule publication and customer approval date
- Transitional and grandfathering provisions
- Retroactive correction requirements

A future-dated transaction is revalidated when relevant legal rules change before execution. A retroactive transaction may require law as of the historical effective date plus present-day correction, notice, reporting, and processing obligations.

#### Rule Provenance

Every legal rule records:

- Stable rule ID and version
- Jurisdiction
- Source type: statute, regulation, regulator guidance, CBA, contract, or customer policy
- Authority reference and legal citation
- Effective interval
- Interpretation status: vendor baseline, counsel approved, or customer defined
- Owner and approving authority
- Last review and next review due
- Superseded and related rules
- Test cases and affected capabilities

Users can inspect why an obligation exists, which facts triggered it, which rule version applied, whether it is a baseline or counsel-approved interpretation, and whether an override is legally permitted.

Rules must not appear as unexplained system magic.

#### Customer Counsel Control

HCM Next provides baseline packs, update feeds, tooling, test cases, and provenance. The customer's authorized legal and privacy teams review, approve, modify, or replace the final tenant interpretation.

Ambiguous requirements may be published with:

```text
REQUIRES_CUSTOMER_COUNSEL_CONFIGURATION
```

The platform does not guarantee legal compliance merely because a pack is enabled. It proves which configured interpretation governed the transaction and whether its obligations were executed.

#### Legal Change Management

Legal change is itself a workflow:

```text
Law or guidance change detected
  -> affected pack identified
  -> affected tenants and scopes identified
  -> workflows, capabilities, data, documents, agents,
     integrations, retention, and reports analyzed
  -> legal and privacy review
  -> customer approval
  -> future effective publication
  -> activation monitoring
```

Simulation answers:

- Which workflows gain or lose steps?
- Which approvals and notices become required?
- Which document variants need updates?
- Which data uses or transfers become prohibited?
- Which retention dates change?
- Which agents or integrations become ineligible?
- Which pending and future-dated transactions require revalidation?

Legal publication follows the same draft, validate, simulate, approve, publish, activate, quarantine, and rollback discipline as other high-risk platform configuration.

#### Breach Response

Security incidents can initiate legally governed breach-response workflows:

```text
Security incident detected
  -> affected tenants, people, fields, regions, and processors
  -> encryption and exposure analysis
  -> legal breach classification
  -> notification obligations and clocks
  -> counsel, privacy, and security review
  -> authority and individual notifications when required
  -> containment, remediation, evidence, and closure
```

GDPR includes supervisory-authority notification duties for qualifying breaches and, where feasible, a 72-hour period after awareness. The pack must determine applicability and other jurisdictional clocks based on actual facts. See [GDPR Article 33 in the official text](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng/).

The ledger and authorization records support forensic answers about which people and fields were exposed, through which capabilities, to which principals or processors, and when.

#### DPIA and High-Risk Processing Workflows

New agents, capabilities, workflows, integrations, or analytics uses can trigger privacy and legal impact assessment.

```text
New performance-prediction agent
  -> manifest inspection
  -> reads performance and absence data
  -> influences promotion recommendation
  -> DPIA or legal review required
  -> assessment workflow
  -> mitigations and approval
  -> activation or denial
```

The assessment records scope, necessity, proportionality, risks, safeguards, consultation, residual risk, approval, review date, and change triggers. Material expansion of capabilities, data, purpose, model, region, or decision role can require reassessment.

#### Legal and Compliance Capabilities

The capability plane exposes governed legal operations:

```text
legal.context.resolve
legal.rules.evaluate
legal.obligations.explain
legal.packs.simulate
legal.packs.publish

privacy.processing.authority.check
privacy.processing.registry.read
privacy.processing.registry.reconcile
privacy.transfer.check

privacy.requests.start
privacy.requests.fulfill

retention.resolve
retention.execute
legal_holds.apply
legal_holds.release

compliance.dpias.start
compliance.breaches.assess
```

Capability manifests declare applicable packs, required LegalContext inputs, data classifications, processing purpose, supported jurisdictions, evidence, and whether counsel review is mandatory.

The defining contract is:

> Law is another versioned constraint system that actors, agents, APIs, and workflows must compose around. It cannot be bypassed through a different channel or hidden inside code.

### 9.13 Workforce Intelligence and Analytics Plane

The Workforce Intelligence Plane combines historical workforce state, business events, workflow behavior, decisions, meaningful product interactions, and optional semantic observations without weakening the authority of canonical HCM facts.

Its governing objective is:

> Know what the workforce looks like now, what it looked like at any prior point, how and why it changed, who made each decision, what information was available, and what humans or systems believed at the time.

#### Five Data Classes

The analytical model preserves five distinct classes:

| Class       | Meaning                                              | Example                                     |
| ----------- | ---------------------------------------------------- | ------------------------------------------- |
| Fact        | Authoritative business assertion or event            | Worker promoted from L4 to L5               |
| Observation | A sourced assessment or reported belief              | Manager rates readiness as high             |
| Decision    | An actor's explicit choice with rationale and inputs | Promotion approved                          |
| Interaction | A meaningful action inside HCM Next                  | Manager opened compensation then simulation |
| Inference   | A model-derived hypothesis or score                  | Possible flight risk with medium confidence |

```text
Business facts       Observations       Interactions
      │                    │                  │
      └────────────────────┼──────────────────┘
                           ▼
                        Decisions
                           │
                           ▼
                 Analytics and semantics
```

The categories may reference and correlate with one another, but they do not mutate one another implicitly. An inference cannot promote itself to a fact; a repeated interaction does not prove intent; a manager assessment does not overwrite a worker record.

#### Semantic Observations

Domain objects can have multiple semantic observations rather than one mutable semantic blob.

```text
Worker
  ├── canonical facts
  └── semantic observations
       ├── manager: readiness = high
       ├── HRBP: readiness = medium
       ├── employee: mobility = limited
       └── model: readiness = 0.74
```

A `SemanticObservation` includes:

- Subject and optional object references
- Schema ID and version
- Typed values
- Source type: human, agent, algorithm, or imported
- Source identity and organization scope
- Confidence and uncertainty
- Purpose
- Observed, effective-from, and effective-to times
- Provenance and supporting references
- Visibility and data classification
- Retention and legal context
- Supersession or withdrawal lineage

Different observations can disagree without creating data corruption. Analysis can compare their source, purpose, timing, and later outcomes.

#### Governed Semantic Schemas

Customer-defined semantic schemas provide extensibility without recreating spreadsheet chaos.

A `SemanticSchema` defines:

- Stable ID, name, description, and business owner
- Tenant and organization scope
- Fields, types, allowed values, units, and validation
- Allowed subject and object types
- Permitted authors and agents
- Purpose and supported use cases
- Visibility, classification, retention, and export policy
- Aggregation and comparison rules
- Version, lifecycle status, and compatibility

Example schemas include succession readiness, critical-role risk, warehouse-supervisor assessment, skills evidence, mobility preference, or customer-specific safety leadership.

Schemas follow draft, review, publish, deprecate, and retire governance. Naming and overlap checks prevent uncontrolled duplication such as `potential`, `potential_score`, and `future_potential` representing indistinguishable concepts.

Semantic capabilities include:

```text
semantics.observe
semantics.read
semantics.search
semantics.supersede
semantics.schemas.create
semantics.schemas.publish
semantics.aggregate
semantics.compare
```

#### Decision Model

Decisions are first-class analytical and audit objects rather than implicit workflow transitions.

A `Decision` records:

- Decision ID, subject, object, and type
- Actor and actor type: human, agent, or system
- Delegation and organization scope
- Workflow, step, proposal, and capability
- Input-snapshot references
- Semantic observations considered
- Policy, legal-rule, and authorization versions
- Recommendations and simulations visible
- Choice: approve, reject, defer, modify, escalate, or another typed outcome
- Reason code and optional governed reason text
- Confidence when meaningful
- Decision time and effective time
- Resulting actions and causal event references

Human, agent, and deterministic rule decisions use the same core structure. Agent provenance additionally records model, prompt, tools, retrieval sources, configuration, confidence, and policy constraints.

This creates a Decision Ledger that supports audit, evaluation, workflow improvement, and outcome analysis without rewriting the authoritative business-event ledger.

#### Decision Input Snapshots

A decision references what the decision-maker could actually see at the time.

`DecisionInputSnapshot` can identify:

- Visible canonical facts and their versions
- Visible semantic observations
- Policy and legal context
- AI recommendations and explanations
- Reports, documents, and simulations
- Field masks and organization scope
- Data that existed but was not authorized
- Authorized data that was not presented
- Presented content that the actor actually opened or acknowledged when tracked legitimately

This distinguishes:

```text
information existed
  ≠ actor was authorized to see it
  ≠ interface presented it
  ≠ actor opened or acknowledged it
```

Snapshots use immutable references, hashes, or minimized retained content according to privacy and retention policy. They must not become an excuse to duplicate sensitive data indefinitely.

#### Decision-to-Outcome Linkage

Decisions link causally to resulting transactions and correlatively to later outcomes.

```text
Promotion decision
      │
      ▼
Promotion transaction
  ├── compensation change
  ├── team and manager change
  └── access change
      │
      ▼
Observed outcomes
  ├── later performance
  ├── retention
  ├── employee sentiment
  ├── team measures
  └── subsequent mobility
```

The platform preserves linkage but does not automatically claim that the decision caused a later outcome. Analytical products must label correlation, prediction, experiment, and causal inference distinctly.

#### Activity Ledger

Meaningful product interactions belong in a separate Activity Ledger rather than the authoritative business ledger or raw telemetry.

An `ActivityEvent` may record:

- Actor and actor type
- Tenant and organization scope
- Session and client type
- Capability
- Resource type and identifier
- Semantic action
- Timestamp and duration where useful
- Declared purpose
- Workflow or decision context
- Result: success, denied, canceled, abandoned, or error
- Classification and retention

Examples include:

- Worker profile viewed
- Compensation details opened
- Candidate search performed
- Report exported
- Promotion simulation started
- Workflow abandoned
- Repair reviewed
- Policy changed
- Agent capability invoked

Activity events answer product and process questions without polluting business truth.

#### Anti-Surveillance Boundary

Activity collection means meaningful actions performed inside HCM Next. It does not mean keylogging, arbitrary mouse tracking, screenshots, unrelated browser monitoring, or covert worker surveillance.

The Legal and Compliance Plane governs:

- Purpose and necessity
- Notice and transparency
- Permitted actors and populations
- Fields and metadata collected
- Retention and aggregation
- Worker-monitoring restrictions
- Agent and model use
- Access and export

Collection must be minimized to the analytical or security purpose. A compensation view can be a meaningful security and workflow event; recording everything a manager types elsewhere is neither necessary nor acceptable.

#### Inferences and Behavioral Signals

Usage patterns may generate low-authority semantic signals, never canonical facts.

For example, repeatedly viewing several workers in a succession-planning context may support a restricted inference that those workers are being considered. It does not make any worker the designated successor.

Every inferred signal records:

- Derivation method and version
- Source activity population
- Purpose
- Confidence and uncertainty
- Required human confirmation
- Classification and access restrictions
- Effective and expiration times
- Prohibited downstream uses

Inferences are clearly labeled in UI, APIs, reports, and agent context. A confirmed observation or business decision is a new object with lineage to the inference, not an in-place reclassification.

#### Operational-to-Analytical Architecture

Cross-domain analytics runs outside transactional stores.

```text
Operational domains
People  Recruiting  Time  Payroll  Access
Talent  Benefits    WFM   Comp     Learning
   │        │         │     │          │
   └────────┴─────────┴─────┴──────────┘
                       │
              Event and CDC fabric
                       │
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
    Warehouse       Lakehouse    Semantic index
        │              │              │
        └──────────────┼──────────────┘
                       ▼
                Semantic model
                       │
        Reports | BI | Agents | Forecasting
```

Transactional services remain optimized for current governed operations. Analytical systems handle large historical, cross-domain, cohort, behavioral, and model-evaluation workloads.

The authoritative ledger establishes business facts. Decision and Activity Ledgers add distinct governed evidence. The analytical platform receives all classes through controlled distribution and preserves their class and provenance.

#### Events and Temporal State

Analytics requires event facts plus historically versioned state.

Representative facts include:

```text
fact_worker_change
fact_payroll
fact_time
fact_recruiting
fact_workflow
fact_decision
fact_interaction
fact_access
fact_semantic_observation
```

Representative temporal dimensions include:

```text
dim_worker
dim_employment
dim_job
dim_position
dim_org
dim_location
dim_manager
dim_legal_entity
dim_time
dim_skill
dim_workflow
dim_agent
dim_policy
```

Dimensions retain historical versions so analysis does not join old facts to today's organization, manager, job, or policy accidentally.

#### Bitemporal Analytics

Effective time and recorded time propagate through the analytical model.

```text
effective time
  when the business fact applies

recorded time
  when HCM Next learned or recorded it
```

This supports two different questions:

- What was the correct effective state on June 15?
- What did the organization believe the state was on June 15?

The distinction is essential for retroactive compensation, payroll error analysis, delayed manager changes, benefits corrections, access incidents, and historical reporting.

Standard temporal query modes include:

```text
CURRENT
AS_OF
BETWEEN
CHANGE_HISTORY
KNOWN_AS_OF
EFFECTIVE_AS_OF
```

Every metric and report declares which temporal mode and effective/recorded-time semantics it uses.

#### Unified Analytical Event Envelope

All analytical inputs use a common envelope while retaining domain-specific payloads:

```text
AnalyticEvent

event_id
tenant_id
organization_scopes[]
domain

event_class
  business
  observation
  decision
  interaction
  inference
  operational

event_type

actor and actor_type
subject_type and subject_id
object_type and object_id

workflow_id
decision_id

effective_at
recorded_at

dimensions {}
measures {}
semantic_tags {}

classification
purpose
retention_policy

correlation_id
causation_id
```

New product modules join the analytics ecosystem by publishing this contract and their semantic mappings.

#### Workforce Knowledge Graph

The analytical semantic model forms a Workforce Knowledge Graph conceptually, regardless of physical database technology.

```text
Person
  ├── Candidate and recruiting
  ├── Employment, job, position, and organization
  ├── Compensation, payroll, time, and benefits
  ├── Talent, performance, learning, and skills
  ├── Identity, applications, and access
  └── Experience, cases, and communications

Surrounding each relationship:
  facts
  events
  observations
  decisions
  interactions
  inferences
  workflow history
```

The graph gives business users stable concepts and relationships without requiring knowledge of projection tables, event storage, warehouse schemas, or service boundaries.

#### Semantic Query Model

The semantic layer exposes business entities such as Worker, Employment, Job, Organization, Compensation, Time, Performance, Recruiting, Learning, Access, Decision, Workflow, Interaction, and Observation.

It defines:

- Relationships and valid joins
- Grain and cardinality
- Current and historical meaning
- Organization and legal scope
- Data classification
- Allowed dimensions and filters
- Aggregation behavior
- Cohort privacy rules
- Fact, observation, decision, interaction, and inference labels

Users and agents can ask cross-domain questions without writing joins against internal storage. The query engine produces an explainable semantic plan before execution.

#### Governed Metrics

Metrics are versioned semantic objects rather than formulas hidden in dashboards.

A `Metric` includes:

- Stable ID, name, and description
- Formula and source facts
- Grain, dimensions, and allowed filters
- Numerator, denominator, and exclusions
- Temporal semantics
- Organization and population scope
- Data classification and minimum cohort rules
- Owner, version, certification, and lifecycle

Examples include voluntary turnover rate, promotion cycle time, access-removal latency, payroll repair rate, critical-role readiness, time-to-productivity, or agent recommendation override rate.

Dashboards, reports, APIs, and agents use the same certified metric definition. Customer metrics follow the same governance as semantic schemas.

#### Cross-Domain Analysis

The unified model supports questions spanning:

- Recruiting source and performance after hire
- Offer decisions and retention
- Compensation position, performance, and promotion rates
- Learning completion, skill evidence, and mobility
- Overtime, schedule volatility, absence, and turnover
- Termination timing and access-removal latency
- Workflow variations, completion time, bypass, and repair
- Human and agent recommendations, final decisions, and later outcomes
- Policy changes and payroll or reconciliation errors

These analyses are valuable because every domain shares worker, organization, time, decision, and causal identifiers. Access remains governed at query time; physical co-location does not imply universal analytical permission.

#### Capability-Generated Analytics

Every capability manifest declares the analytical events and semantic entities it produces.

```text
Capability: worker.promote

domain: people
business_action: promotion
analytics_events:
  PromotionProposed
  PromotionSimulated
  PromotionExecuted
```

Product analytics therefore does not depend on each frontend engineer remembering to instrument the flow. UI, API, workflow, agent, and partner invocations emit consistent semantic events through the capability layer.

Read capabilities may emit classified ActivityEvents when necessary for security, audit, or workflow analytics. Collection remains subject to minimization and legal policy.

#### Workflow and Process Intelligence

Every workflow automatically exposes process events and measures:

- Started, completed, canceled, failed, and stuck
- Repaired, bypassed, short-circuited, superseded, and migrated
- Time in state and total completion time
- Approval and human-task latency
- Retry, repair, and compensation counts
- Safe-point and integration delay
- Legal and authorization obligations added
- Version, policy, and organization variation

This creates a process-mining source that can explain why onboarding, hiring, promotion, payroll repair, or access provisioning differs across jurisdictions, companies, teams, workflow versions, and approver groups.

#### Workflow Optimization From Usage

Meaningful interaction sequences can reveal missing context or inefficient workflow design.

If HR administrators repeatedly open worker, compensation, organization history, and performance before starting a promotion, the platform may propose a governed promotion-preparation view or preflight capability.

If managers abandon transfers to open headcount reports, the transfer workflow may need that context embedded.

Optimization follows this lifecycle:

```text
pattern detected
  -> privacy and statistical validation
  -> product or customer hypothesis
  -> proposed experience or workflow change
  -> simulation and review
  -> controlled rollout
  -> outcome measurement
```

Behavioral correlation never silently rewrites a workflow or creates an employee fact.

#### Analytical Retention and Deidentification

Analytical retention is independent from operational retention.

```text
raw identifiable events
  -> short governed retention
  -> pseudonymized events
  -> aggregated facts
  -> long-term certified metrics
```

The Legal and Compliance Plane determines purpose, retention, legal hold, deletion, anonymization, and reuse. A worker-level activity event may expire while non-identifying aggregate process measures remain.

Deidentification must address small cohorts, linkage attacks, rare attributes, longitudinal reidentification, and model memorization. Removing a name alone is not sufficient.

#### API-Native Reports and Analytics

Analytics follows the platform's capability-first rule:

```text
reports.define
reports.query
reports.schedule
reports.export

analytics.query
analytics.explain
analytics.compare

metrics.read
metrics.evaluate
dimensions.list

decisions.query
activities.query
semantics.query
```

Definitions, queries, schedules, and exports have organization, purpose, field, cohort, retention, and destination controls. Bulk export remains a distinct high-risk capability.

#### Safe Natural-Language Analytics

Agents do not receive unrestricted SQL or warehouse credentials.

```text
User question
  -> agent identity and purpose
  -> semantic query plan
  -> AuthZ and organization scope
  -> legal and processing check
  -> metric and dimension resolution
  -> cohort and inference controls
  -> query execution
  -> classified result
  -> explanation and provenance
```

The plan shows selected metrics, dimensions, filters, temporal mode, exclusions, aggregation, cohort protections, and policy decisions before or alongside results.

Managers may query their authorized organizations, corporate HR may receive approved aggregates, restricted domains may be unavailable, and cohorts below policy thresholds may be suppressed. The model sees only the permitted result and associated classification.

#### Intelligence Governance

Workforce Intelligence resources have named owners and lifecycle controls:

- Semantic schemas
- Certified metrics
- Dimensions and relationships
- Decision taxonomies
- Observation and inference types
- Activity-event collection
- Analytical models and forecasts
- Reports, dashboards, and exports

Publication requires data-quality tests, temporal validation, classification, bias and fairness review where relevant, privacy and legal review, access-policy tests, lineage, owner approval, and rollback.

Analytical output must label:

- Fact versus observation, decision, interaction, or inference
- Current versus historical state
- Effective versus known-at-the-time state
- Correlation versus causal claim
- Human versus agent versus system source
- Confidence, completeness, and known limitations

The deeper product thesis is:

> HCM Next does not merely store workforce records and run workflows. It accumulates governed institutional knowledge about how the workforce operates, how the organization makes decisions, what information those decisions used, and what outcomes followed—without sacrificing the epistemic integrity of authoritative HR facts.

### 9.14 Agent Runtime Plane

The Agent Runtime is a first-class platform plane, not a chatbot attached to a conventional HCM suite.

```text
Experience
Chat | Voice | UI | Mobile | API | External Agents
                         │
                         ▼
Agent Plane
Intent | Discovery | Planning | Semantic Reasoning
Memory | Prediction | Explanation | Workflow Composition
                         │
                    Model Router
                         │
                         ▼
Governance Plane
AuthN | AuthZ | Legal | Privacy | Purpose | Risk
                         │
                         ▼
Capability Plane
HTTP | gRPC | Queries | Reports | Workflow Blocks
                         │
                         ▼
HCM Domain Plane
People | Workforce | Talent | Rewards | Experience | Access
                         │
                         ▼
Data Plane
Ledger | Projections | Analytics | Semantics | Decisions | Activity
```

The traditional interface remains available, but it is another presentation over the same capability fabric. The agent can become the primary interface for intent that crosses navigation, modules, policies, and data domains.

#### Agent Interaction Model

A manager can express a business outcome such as:

> Promote Sarah to Senior Engineer on September 1, increase her salary by 12%, verify band position, and explain downstream effects.

The agent resolves that intent through governed steps:

```text
Intent
  -> discover relevant semantic entities
  -> discover eligible capabilities
  -> retrieve authorized facts and observations
  -> resolve policy, LegalContext, and obligations
  -> construct plan
  -> simulate compensation, organization, payroll, and access effects
  -> explain uncertainty and required approvals
  -> draft Change Request or workflow
  -> current authorization and human approval
  -> deterministic execution
  -> ledger, reconciliation, and outcome analysis
```

No sentence maps directly to an unrestricted mutation. Natural language initiates planning; it does not bypass the transaction-control model.

#### Universal Capability Substrate

Agents use the same semantic capabilities as UI, customer applications, workflows, and partners. They receive no hidden application or database access.

HTTP, gRPC, events, SDKs, and internal calls remain transports over one capability contract. The agent reasons about the stable capability name, inputs, outputs, domains, risk, scope, legal constraints, simulation, cost, and latency rather than route syntax.

The runtime chooses an eligible transport without changing authorization, validation, idempotency, workflow, ledger, error, or reconciliation semantics.

#### Capability Registry as Agent Nervous System

The Capability Registry supplies a context-efficient, searchable inventory across:

```text
people.*        workforce.*      talent.*
rewards.*       payroll.*        benefits.*
recruiting.*    learning.*       access.*
workflow.*      analytics.*      semantics.*
legal.*         repair.*         communications.*
```

Every manifest advertises:

- Semantic description and domain
- Typed inputs and outputs
- Read and write domains
- Capability and field authorization
- Organization and jurisdiction scope
- Risk and autonomy requirements
- Legal, privacy, and purpose constraints
- Simulation, batching, rollback, and compensation support
- Agent eligibility and required human approval
- Cost and latency characteristics
- Freshness and temporal semantics

Planning retrieves only relevant manifests, then filters them through planning authorization, LegalContext, purpose, agent definition, and autonomy policy before entering them into model context.

#### Semantic Domain Discovery

Agents discover Worker, Employment, Job, Position, Manager, Organization, Compensation, Time, Payroll, Skill, Performance, Candidate, Learning, Access, Decision, Workflow, and related concepts through the Workforce Knowledge Graph.

They do not reason over physical table and column names. The semantic catalog exposes relationships, time semantics, classification, valid joins, metrics, and permitted query operations while physical storage may remain distributed across operational databases, OLAP, lakehouse, search, and vector systems.

This allows agents to explore cross-domain questions not anticipated as fixed reports while remaining constrained to governed semantic relationships.

#### AnalysisPlan

Agents produce a typed `AnalysisPlan` before executing nontrivial analysis.

An AnalysisPlan includes:

- Original question and normalized analytical intent
- Population and organization scope
- Period and temporal mode
- Facts, dimensions, metrics, and semantic observations
- Comparison groups and exclusions
- Temporal alignment rules
- Hypotheses to test
- Aggregation and minimum-cohort requirements
- AuthZ, purpose, privacy, and legal constraints
- Data freshness and completeness expectations
- Estimated cost and latency
- Output classification and retention

```text
Question
  -> AnalysisPlan
  -> semantic/type validation
  -> AuthZ and organization scope
  -> legal and purpose validation
  -> query-cost and cohort controls
  -> analytical execution
  -> explanation, evidence, uncertainty, and lineage
```

The plan is reproducible and can be inspected, approved, cached, compared, or saved. The model never receives unrestricted SQL access.

#### Hypotheses and Semantic Assessments

Agent conclusions are first-class `Hypothesis` or `SemanticAssessment` objects, never silent changes to canonical records.

A hypothesis records:

- Subject or population
- Type and proposed value
- Confidence and uncertainty
- Supporting and conflicting evidence
- Model and agent provenance
- Generation, effective, and expiration times
- Purpose and allowed uses
- Organization scope and classification
- Required human confirmation
- Outcome or evaluation linkage

Individual and organizational hypotheses use the same semantics. Examples include worker flight-risk estimates, readiness assessments, or a population-level hypothesis that schedule instability contributes to turnover.

The platform always displays and returns the object as an inference. Confirmation creates a new human observation, decision, metric, rule, or business transaction with lineage; it does not relabel the original inference as fact.

#### Prediction Lifecycle

Predictions expire and are evaluated against later outcomes.

```text
Prediction generated
  -> used or not used in a decision
  -> prediction horizon closes
  -> actual outcome resolved
  -> PredictionEvaluation
  -> calibration and drift analysis
```

Evaluation includes calibration, precision, recall, false-positive and false-negative rates, outcome coverage, drift, and performance by permitted population, jurisdiction, model, provider, agent, and task.

Policy determines whether a prediction may be created, surfaced, used in a decision, retained, or analyzed for protected or sensitive populations.

#### Workflow Authoring by Agents

Agents can translate natural-language business intent into typed workflow definitions by composing capabilities and policy obligations.

The agent authors behavior; the deterministic runtime executes it.

```text
Natural-language intent
  -> capability and semantic discovery
  -> candidate graph synthesis
  -> compiler and governance pipeline
  -> simulation and generated tests
  -> human review and publication
  -> immutable workflow version
  -> deterministic execution
```

An agent-created transfer workflow may resolve LegalContext, simulate compensation, payroll, and access, branch on cost increase, insert Finance approval, consume legal obligations, execute domain changes, and reconcile outcomes without receiving authority to perform those mutations during authoring.

#### Workflow Compiler Pipeline

Agent-created workflows are treated like governed code:

```text
Generated graph
  -> schema and type checking
  -> capability existence and version checks
  -> organization-scope validation
  -> authorization validation
  -> legal and purpose validation
  -> graph reachability and termination analysis
  -> side-effect and conflict analysis
  -> safe-point, retry, and idempotency analysis
  -> compensation and repair-path analysis
  -> privacy and data-flow analysis
  -> simulation
  -> deterministic test suite
  -> human approval
  -> immutable publication
```

The agent may be creative; the compiler remains deterministic, conservative, explainable, and fail-closed.

#### Agent-Generated Workflow Tests

Agents generate test scenarios from the workflow graph, manifests, policies, integrations, and legal packs.

Test families include:

- Jurisdiction and legal-entity variants
- Threshold and approval boundaries
- Missing, stale, conflicting, and unauthorized data
- Approver absence and delegation
- Integration outage and partial execution
- Duplicate and concurrent requests
- Future-dated and retroactive changes
- Mid-workflow policy or legal-rule changes
- Quarantine, migration, repair, compensation, and cancellation
- Bulk and high-blast-radius behavior

Generated tests do not replace curated regression and compliance suites. They expand scenario coverage and are executed by deterministic simulators before publication.

#### Specialized Agent Architecture

Multi-agent architecture uses bounded specialization rather than unrestricted agent-to-agent conversation.

Representative agents include:

- HCM Concierge for general interaction and delegation
- HRIS Agent for configuration, workflow, and repair assistance
- Recruiting Agent for requisitions, candidates, and offers
- Compensation Agent for pay analysis and planning
- Payroll Agent for diagnostics and reconciliation
- Workforce Agent for time and scheduling analysis
- Access Agent for identity and entitlement operations
- Talent Agent for performance, learning, skills, and succession
- Legal Agent for sourced compliance interpretation assistance
- Analytics Agent for cross-domain semantic analysis

Each agent has separate capabilities, data domains, organization scope, models, purposes, autonomy ceiling, risk tolerance, memory, legal eligibility, and budget.

Delegation is explicit and typed. The Concierge may delegate an analytical subtask to the Analytics Agent, but effective authority remains the intersection of the human, initiating agent, delegated agent, organization scope, purpose, and policy. Subagents cannot expand authority through delegation chains.

#### AgentDefinition

Agents are versioned, customer-configurable resources.

An `AgentDefinition` includes:

- Agent ID, name, owner, tenant scope, and organization scope
- Instructions and instruction version
- Capabilities and data domains
- Semantic schemas and knowledge sources
- Allowed providers and models
- Maximum capability risk and autonomy level
- Delegation rules and allowed subagents
- Legal purpose and jurisdiction scope
- Memory policy
- Cost and latency budget
- Invocation modes
- Evaluation suite and quality thresholds
- Lifecycle: draft, validated, active, quarantined, deprecated, retired

Customer agents follow the same draft, simulation, review, publication, version pinning, quarantine, and rollback governance as workflows and legal policies.

#### Model and Provider Router

The Model Router is provider-neutral infrastructure. It selects from HCM-hosted, external SaaS, customer-VPC, on-premises, or other approved models according to a typed `TaskProfile`.

A TaskProfile includes:

- Task and domain
- Reasoning and context requirements
- Structured-output and tool-use needs
- Data classification and legal purpose
- Required processing and storage region
- Provider and customer approvals
- Employment-use eligibility
- Quality floor
- Latency target
- Cost priority and maximum cost
- Reliability and fallback requirements

Routing returns the chosen provider, model and version, eligibility evidence, fallback plan, and estimated cost. A cheap model is ineligible if its region, retention, contract, security, or employment-use posture conflicts with policy.

Private and local models are first-class providers when their capability profiles satisfy the task.

#### Complexity Tiers and Non-AI Paths

Tasks use the least complex eligible mechanism:

| Tier | Mechanism                   | Typical work                                     |
| ---- | --------------------------- | ------------------------------------------------ |
| 0    | Deterministic, no LLM       | PTO balance, calculations, authorization, lookup |
| 1    | Small model                 | Classification, extraction, simple summarization |
| 2    | General model               | Routine HR reasoning and workflow assistance     |
| 3    | Frontier reasoning model    | Cross-domain analysis and workflow generation    |
| 4    | Ensemble or critic pipeline | High-risk global or organizational planning      |

Agent-first never means sending deterministic payroll, tax, overtime, authorization, or ledger computation to a language model. AI coordinates and explains deterministic capabilities.

#### High-Risk Ensembles

High-risk reasoning may use structured reviewer roles:

```text
Planner
  -> candidate plan
  -> legal critic
  -> security/privacy critic
  -> workflow compiler
  -> deterministic simulation
  -> human specialist or counsel
```

Critics return typed findings and cannot independently authorize execution. Ensemble use is driven by capability risk, jurisdiction, blast radius, novelty, and customer policy rather than applied to every request.

#### Semantic Caching

Caching reduces repeat inference without hiding stale context.

Cache classes include:

- Exact deterministic tool-result cache
- Semantic query-plan cache for equivalent questions
- Analysis cache keyed by plan, data versions, policy versions, model version, and temporal mode
- Agent-plan templates for common intents
- Retrieved capability and schema context cache

Every cache declares scope, classification, purpose, TTL, invalidation inputs, policy versions, and whether reuse crosses users or organizations. Sensitive results are never shared merely because prompts appear semantically similar.

#### Inference Budgets

Tenants control AI economics through budgets:

- Monthly and daily limits
- Per-agent and per-purpose limits
- Maximum request and workflow cost
- Preferred and fallback providers
- Quality floors
- Latency targets
- Reserved budget for critical operations
- Alert and approval thresholds

Budget policy may downgrade an eligible model, reuse a valid cache, choose a deterministic path, defer work, request approval, or deny a nonessential operation. It cannot choose a model that violates privacy, legal, security, or minimum-quality requirements.

#### Layered Agent Memory

Agent memory is not one permanent conversation transcript.

```text
Interaction memory
  current conversation

User preference memory
  approved non-sensitive preferences

Task memory
  current workflow or analysis

Organizational memory
  approved shared semantic knowledge

Case memory
  governed prior resolutions

Learned patterns
  aggregate operational knowledge
```

Every memory item has source, scope, purpose, classification, organization, retention, authorization, LegalContext, confidence, and expiration. Memory retrieval is reauthorized at use time.

Conversation content does not automatically become organizational memory. Promotion requires review, provenance, and an explicit knowledge object.

#### Operational Knowledge

Validated human repair experience can become scoped `OperationalKnowledge`.

It records:

- Problem signature
- Applicable organization, domain, integration, and jurisdiction
- Recommended resolution and prerequisites
- Validator and evidence
- Success and failure counts
- Confidence
- Effective and review periods
- Related RepairPlans and outcomes

The next matching incident may retrieve this knowledge and propose a RepairPlan. It does not silently execute or override current policy.

#### Learning Through Proposals

Agents learn from workflow behavior only by proposing governed changes.

```text
Pattern or outcome observed
  -> agent recommendation
  -> WorkflowDiff, policy diff, or experience proposal
  -> simulation and generated tests
  -> human review
  -> new immutable version
```

A step bypassed in most executions may be a candidate for a policy short circuit, but the agent cannot change production behavior autonomously.

#### AgentExecution Provenance

Every material agent run has an `AgentExecution` record containing:

- Agent and definition version
- Human, service, delegation, and organization context
- Model provider, model, and version
- Instruction and prompt-policy versions
- Capabilities discovered and called
- Data domains, semantic queries, and sources read
- AnalysisPlan, workflow plan, hypotheses, and decisions produced
- Governance decisions and obligations
- Tokens, cost, latency, retries, and cache use
- Result, confidence, and error status
- Correlation, decision, workflow, and trace identifiers

The agent trace and business ledger remain separate. Model calls, discarded plans, tool attempts, token use, and retries belong in agent traces. Material business proposals, decisions, approvals, executions, and outcomes belong in the business and Decision Ledgers. Shared identifiers connect them.

#### Agent Evaluation Service

Every agent and material use case has a versioned evaluation suite measuring:

- Task completion
- Capability and tool selection accuracy
- Factual grounding and unsupported claims
- Authorization, purpose, privacy, and legal violations
- Structured-output validity
- Workflow compile and simulation failures
- Human correction and override rates
- Prediction calibration and drift
- Cost, latency, and reliability
- Outcome quality where attribution is appropriate

Results are sliced by provider, model, agent, tenant, organization, domain, country, language, task, risk class, and relevant population subject to privacy controls.

Promotion of an agent, model, prompt, router rule, or provider requires passing defined thresholds. Evaluation evidence feeds routing but cannot override hard eligibility constraints.

#### Model Shadowing

Model alternatives can be evaluated in shadow mode using appropriately governed, minimized, or synthetic tasks.

The shadow model never controls production behavior. Its output is compared for accuracy, plan quality, policy compliance, cost, latency, and consistency. Sensitive production data is used only when LegalContext, customer approval, provider eligibility, and retention permit it.

Routing changes are promoted based on evidence and can be rolled back.

#### Semantic Bridges and Temporary Dimensions

Agents can formulate new relationships that deterministic reports did not predefine.

A readiness assessment might combine job requirements, skills, performance, leadership observations, training, prior team outcomes, mobility, compensation implications, and legal eligibility. The result remains a SemanticAssessment with evidence and uncertainty.

Agents may also define temporary analytical dimensions. For example:

```text
AccidentalManager :=
  acquired direct reports
  AND no manager training
  AND no management compensation change within 90 days
```

The definition is a temporary, inspectable semantic plan—not a hidden new worker field. The customer may discard it or graduate it through governance.

#### Analysis-to-Automation Lifecycle

Ad hoc understanding can evolve into durable customer behavior:

```text
Question
  -> ad hoc AnalysisPlan
  -> saved semantic definition
  -> certified metric
  -> monitor
  -> governed rule
  -> workflow
```

Each graduation is explicit and requires stronger ownership, tests, scope, classification, purpose, policy, simulation, and approval. A natural-language question never becomes production automation in one invisible step.

#### Dynamic Contextual Workspaces

Agents can compose task-specific workspaces from authorized capabilities and semantic views.

A merit-review request may produce a temporary workspace containing employee, current pay, band position, performance, prior increase, suggested range, budget impact, warnings, and actions. The user can filter or reorganize the view conversationally.

Dynamic UI definitions:

- Use the same capabilities and field masks as static UI
- Declare source, purpose, scope, and classification
- Cannot expose undiscovered or unauthorized fields
- Separate calculated, observed, inferred, and canonical values visually
- Preserve action risk and approval requirements
- Expire or become governed reusable experiences through explicit publication

The future interface combines stable navigation with agent-generated contextual workspaces rather than requiring a permanent page for every possible task.

The renderer-independent page, widget, brand, provenance, safety,
accessibility, and GoWebComponents migration rules are maintained in [the
Experience, Dynamic UI, and Branding Contract](experience-ui-and-branding.md).

#### Autonomy Ladder

Agents have explicit maximum autonomy levels:

| Level | Meaning                                          |
| ----- | ------------------------------------------------ |
| A0    | Explain only                                     |
| A1    | Read and analyze                                 |
| A2    | Recommend                                        |
| A3    | Draft a transaction, workflow, report, or policy |
| A4    | Execute a low-risk action                        |
| A5    | Execute a governed workflow with mandatory gates |
| A6    | Perform autonomous bounded operations            |

Every agent has a ceiling; every capability and context has a required autonomy level. Tenant policy may lower autonomy. Legal rules, capability risk, bulk scale, sensitive data, novel context, low confidence, or unusual behavior may require escalation.

A5 does not mean the agent may satisfy its own human approval. A6 is limited to defined populations, capabilities, volumes, time windows, budgets, error thresholds, and shutdown conditions.

#### Agent Invocation Types

The runtime supports:

- **Interactive agents** that work directly with employees, managers, and specialists
- **Embedded agents** that perform bounded analysis or drafting inside workflows
- **Operational agents** that monitor event streams and propose or execute bounded responses

All three use the same identity, capability, policy, model, memory, budget, provenance, evaluation, and autonomy infrastructure.

#### Event-Driven Operational Agents

Operational agents subscribe to semantic event classes through governed subscriptions, not by mutating the ledger.

```text
Worker terminated
  -> expected deprovision window
  -> Access Agent checks observed state
  -> privileged access remains
  -> reconciliation issue
  -> proposed RepairPlan
  -> approval or bounded execution
```

```text
Payroll completed
  -> Payroll Agent executes approved AnalysisPlan
  -> anomalies identified
  -> hypotheses and evidence
  -> specialist review
```

Subscriptions declare tenant and organization scope, event classes, purpose, retention, maximum autonomy, rate and blast-radius limits, and stop conditions. Agents append proposals and governed business outcomes through capabilities; they never write directly to the ledger.

#### Agent Runtime Capabilities

The capability plane exposes:

```text
agents.define
agents.validate
agents.simulate
agents.publish
agents.invoke
agents.delegate
agents.quarantine

agents.capabilities.discover
agents.memory.read
agents.memory.propose

analysis.plan
analysis.execute
hypotheses.create
hypotheses.evaluate
predictions.evaluate

models.eligible.list
models.route
models.shadow.evaluate

agents.evaluations.run
agents.evaluations.compare
agents.budgets.read
agents.budgets.configure

workspaces.compose
workspaces.publish
```

These are governed semantic operations, not raw access to prompts, provider keys, databases, or execution internals.

The combined agent-first thesis is:

```text
Dense capabilities give agents tools.
The semantic layer gives agents understanding.
Workforce Intelligence gives agents context.
The workflow model gives agents a language for behavior.
AuthZ and Legal constrain behavior.
The deterministic runtime makes execution safe.
The ledger preserves truth.
Outcomes and evaluations reveal what worked.
```

### 9.15 Physical Data Plane

The logical contracts defined elsewhere in this plan require a polyglot physical data plane. No single database should be forced to provide transactional integrity, current-state serving, full-text and semantic retrieval, cross-domain analytics, large-object retention, event distribution, caching, and software telemetry equally well.

The target architecture is:

```text
                         COMMAND / WRITE
                               |
                               v
                       Domain Transaction
                               |
                          ACID commit
                               |
              +----------------+----------------+
              |                |                |
              v                v                v
       Business Ledger   Critical Projection   Outbox
         PostgreSQL          PostgreSQL       PostgreSQL
              |                                 |
              +----------------+----------------+
                               v
                         Event Fabric
                       Kafka-class bus
                               |
       +-------------+---------+---------+--------------+
       |             |                   |              |
       v             v                   v              v
  Operational      Search             Analytics      Semantic
  Projections      Plane                Plane          Plane
  PostgreSQL    OpenSearch-class    ClickHouse-class Search/vector
       |             |                   |              |
       +-------------+---------+---------+--------------+
                               |
                               v
                     Semantic Query Layer
                               |
                            Redis-class
                              cache

 Authoritative artifact plane: content-addressed object storage + WORM evidence
 Separate correlated plane: OpenTelemetry logs, metrics, and traces
```

These names describe reference technology classes, not irreversible product commitments. Phase 1 should retain the existing simple PostgreSQL ledger, projections, and outbox. Kafka-class distribution, OpenSearch-class retrieval, ClickHouse-class analytics, Redis-class caching, and WORM storage should be introduced only when measured scale, isolation, latency, or regulatory requirements justify them.

#### Governing Durability Contract

The durability hierarchy is:

```text
Business ledger                 authoritative chronology and material facts
Content-addressed object plane  authoritative verbatim artifacts referenced by facts

Checkpoints                     rebuild acceleration
Operational projections         rebuildable current and temporal state
Search indexes                  rebuildable retrieval representation
Semantic fragments/indexes      rebuildable discovery representation
Embeddings                      model-specific disposable projection
Analytical tables               replayable analytical representation
Caches                          disposable serving optimization
Telemetry                       operational evidence under its own retention policy
```

The phrase “the ledger is the one canonical truth source” therefore means that it is the one canonical history of business events. It does not require a 50 MB signed contract, medical file, resume, transcript, or generated export to live inside a ledger row. The ledger records the authoritative reference, hash, classification, ownership, and governing context for such content.

Every derived plane must publish:

- Its source event or stream watermark
- The projector, schema, mapping, and model versions used
- When it was last successfully updated
- Whether it is current, stale, rebuilding, quarantined, or unverifiable
- How it can be replayed, reconciled, shadow-built, and replaced

#### Narrow, Human-Understandable Ledger Envelope

The physical ledger should contain the minimum sufficient immutable business fact and provenance. Its conceptual envelope is:

```text
LedgerEvent

Identity
  event_id                 UUIDv7 or equivalent sortable unique ID
  tenant_id
  organization_scope_id
  stream_type
  stream_id
  stream_sequence
  event_type
  event_schema_version

Time
  occurred_at
  effective_at
  recorded_at

Causality
  correlation_id
  causation_id
  workflow_instance_id?
  transaction_id?
  decision_id?
  agent_execution_id?
  trace_id?

Actor
  principal_id
  principal_type
  delegated_by?

Integrity
  previous_event_hash
  payload_hash
  event_hash
  integrity_epoch

Governance
  authorization_decision_id
  policy_fingerprint
  legal_context_id
  classification
  retention_class

Data
  payload
  payload_ref?
```

Events remain semantically legible:

```json
{
  "eventType": "WorkerCompensationChanged",
  "previous": { "amount": "130000.00", "currency": "USD" },
  "next": { "amount": "145000.00", "currency": "USD" },
  "reason": "PROMOTION"
}
```

Human readability is an operational and audit feature. Compact physical encodings may be used internally, but they cannot erase the typed business meaning, schema identity, or ability to render an intelligible historical timeline.

#### Mutation Prohibition and Corrective History

“Impossible to corrupt” is not a credible promise. The enforceable promise is:

> Historical mutation is prohibited through normal interfaces, privileged alteration is tightly separated and monitored, and unauthorized changes are designed to become cryptographically and operationally evident.

The application ledger role receives only the minimum privileges required to append and read events. It does not receive ordinary `UPDATE`, `DELETE`, or `TRUNCATE` authority over history. Repair services operate under the same rule.

```text
Incorrect business fact
  -> CorrectionRequested
  -> CorrectiveBusinessEvent
  -> affected projections replayed
  -> external state reconciled
```

```text
Correct ledger fact + incorrect projection
  -> DriftDetected
  -> ProjectionRebuildRequested
  -> ProjectionRebuilt
  -> RepairVerified
```

Administrative database access, disaster recovery, infrastructure credentials, key custody, and backup restoration remain separate threat surfaces. They require dual control, monitoring, periodic restore tests, and evidence review rather than being hand-waved away by application immutability.

#### Per-Stream Ordering, Hash Chains, and Concurrency

HCM Next should not create one global event sequence or hash chain. That would impose an unnecessary global serialization point.

Ordering belongs at the consistency boundaries where causality matters:

```text
tenant configuration stream
worker stream
employment stream
position stream
payroll-run stream
workflow-instance stream
repair-case stream
```

Each stream receives a monotonically increasing sequence and a hash that covers the canonical event representation plus the previous event hash in that stream.

```text
Worker worker_123

sequence 884  CompensationChanged  hash H884
sequence 885  ManagerChanged       hash H885(H884 + event)
sequence 886  AccessRecalcRequested hash H886(H885 + event)
```

The same sequence provides optimistic concurrency:

```text
command expects stream sequence 884
actual stream sequence is 885
-> CONCURRENCY_CONFLICT
-> refresh, revalidate, merge, supersede, or reject under domain policy
```

Hashing without protected key custody proves less than it appears to: an attacker able to rewrite all events may also recompute plain hashes. Where stronger assurance is required, HCM Next should use keyed integrity protection or externally signed integrity roots whose signing authority is outside the transactional database.

#### Integrity Epochs and WORM Evidence

The Integrity Service periodically groups committed event hashes into an integrity epoch, computes a Merkle-style root or equivalent aggregate, signs it outside the database trust boundary, and records:

```text
IntegrityEpoch
  epoch_id
  tenant_or_shard_scope
  first_event
  last_event
  event_count
  root_hash
  signature
  signing_key_id
  signed_at
  verification_status
```

Material manifests and snapshots may be copied to object storage under write-once-read-many retention or legal hold. This supplies an independent verification anchor without introducing a blockchain or forcing all transactions through one global chain.

The plan must define:

- Signing-key custody and rotation
- Epoch closure and late-event rules
- Backup and restore verification
- Missing, duplicated, or reordered event detection
- What happens when an epoch fails verification
- Which tenants, event classes, and retention periods require WORM evidence
- How legal holds and privacy deletion interact with evidence manifests and encrypted payload references

#### Horizontal Ledger Scale

The first scaling boundary should be the tenant, not an arbitrary employee hash. A routing service maps each tenant to a ledger shard. Within a shard, recorded-time partitions provide pruning and lifecycle control.

```text
tenant_ACME -> shard_17

shard_17.ledger_events
  |- 2026_07
  |- 2026_08
  |- 2026_09
  `- ...
```

Most tenants may share a shard. A sufficiently large or regulated tenant may move to a dedicated cluster without changing the semantic append, stream-read, timeline, replay, or verification APIs.

Shard design must preserve:

- Tenant isolation and residency
- Stream-local ordering
- Transactional append plus outbox behavior
- Backup, point-in-time recovery, and restore testing
- Cross-shard analytical access through derived planes rather than distributed OLTP joins
- A governed tenant-migration process with integrity verification before and after movement

#### Content-Addressed Object Plane

Large verbatim content belongs in an object plane, not inline in event rows.

```text
DocumentReceived
  document_id
  object_ref
  content_hash
  size
  mime_type
  classification
  encryption_key_ref
  retention_policy
```

The object identifier should be bound to a cryptographic content hash or an immutable version plus verified hash. A changed artifact produces a new version and hash. The original is not overwritten.

The object plane holds:

- Employment and benefits documents
- Offer letters and signed agreements
- Resumes and attachments
- Images and large transcripts
- Large AI artifacts and evidence packages
- Exports
- Cold ledger snapshots and signed integrity manifests

It must support domain-specific encryption, residency, retention, legal hold, malware inspection, access evidence, and crypto-shredding where legally appropriate. Search indexes store searchable metadata and permitted extracted representations, not the authority-bearing artifact itself.

#### Operational Projection Plane

Normal product reads should not replay the ledger or aggregate raw event history. They should use purpose-built projections.

```text
worker_core_projection
employment_projection
organization_projection
compensation_projection
benefit_projection
time_balance_projection
schedule_projection
payroll_worker_projection
talent_projection
identity_projection
candidate_projection
learning_projection
```

Each projection stores at least:

```text
source_stream_sequence
last_applied_event_id
projection_schema_version
projector_version
projected_at
rebuild_id?
```

Sensitive domains remain physically and logically separable. Payroll should not load performance reviews to calculate pay, and a manager directory view should not incidentally join medical or investigation data.

Highly frequented experiences may use deliberate super-projections:

```text
manager_team_summary_projection
  manager_id
  worker_id
  display_name
  job
  location
  today_status
  next_shift
  leave_balance
  performance_cycle_status
  training_due_count
  open_workflow_count
```

Duplication here is intentional. A super-projection is disposable acceleration, never a new authority source.

#### Cache Plane

A Redis-class cache can serve the smallest, hottest set of reconstructable data:

- Organization graph fragments
- Evaluated authorization and policy snapshots within strict validity windows
- Locale and calendar configuration
- Capability Registry metadata
- Current worker summaries
- Feature and tenant configuration
- Agent semantic metadata and session context

Cache entries require tenant scope, classification, version-aware keys, bounded lifetime, and explicit invalidation semantics. Sensitive negative authorization results must not become reusable across principals or context changes. Cache loss may reduce performance; it cannot cause business-data loss or bypass current authorization.

#### Secure Search and Retrieval Plane

Search is a dedicated rebuildable projection supporting:

```text
exact identifiers
verbatim terms
prefix and full text
fuzzy names and titles
facets and structured filters
semantic retrieval
hybrid lexical + vector retrieval
```

A conceptual search document is:

```text
SearchDocument
  document_id
  tenant_id
  organization_scopes[]
  legal_entity_id?
  resource_type
  resource_id
  canonical_text
  keywords[]
  exact_fields
  semantic_text?
  embedding_refs[]
  epistemic_class
  data_classification
  authorization_scope_tokens[]
  effective_from
  effective_to
  source_event_id
  source_stream_sequence
  search_schema_version
```

Authorization constraints must participate in candidate generation. Searching globally and filtering unauthorized results afterward damages relevance and creates side channels.

```text
semantic query
AND tenant scope
AND organization scope
AND classification eligibility
AND resource type
-> secure candidate set
-> canonical AuthZ verification
-> field-filtered response
```

Indexed scope tokens are coarse, fast security filters; they do not replace authoritative, current AuthZ. Counts, facets, suggestions, snippets, timing, and error behavior must be tested for disclosure risks too.

#### Semantic Fragments and Embeddings

Search must retain the distinction among fact, observation, decision, interaction, and inference. Retrieval returns provenance and epistemic type, not an undifferentiated claim.

One entity should not be compressed into one vector. A worker may have separate fragments for skills, career history, learning, manager observations, and governed semantic assessments.

```text
SemanticFragment
  fragment_id
  subject_type
  subject_id
  domain
  semantic_type
  epistemic_class
  source_type
  source_id
  text_or_source_ref
  effective_at
  classification
  organization_scope
  source_hash
  embedding_model_id?
  embedding_model_version?
  embedding_ref?
```

Source facts and governed observations are durable under their own retention policies. Embeddings are projections. When a model, dimension, or chunking strategy changes, HCM Next can build a new embedding generation, compare retrieval quality, switch an index alias, and discard the old vectors without losing business meaning.

#### Analytical Plane

Cross-domain historical analysis belongs in a columnar OLAP plane rather than the transactional ledger database.

```text
fact_business_event
fact_workflow_event
fact_payroll
fact_time
fact_decision
fact_activity
fact_agent_execution
fact_semantic_observation

dim_worker
dim_organization
dim_job
dim_location
dim_legal_entity
dim_manager
dim_time
dim_policy
dim_workflow
dim_agent
```

Effective time and recorded time must survive ingestion so the analytical system supports both “what was effective then?” and “what did the organization know then?”

Incremental materialized views or equivalent ingestion-time aggregation may maintain hot analytical products such as:

```text
manager_metrics_daily
organization_turnover_monthly
payroll_anomaly_daily
workflow_health_hourly
overtime_weekly
iam_deprovision_latency
agent_cost_daily
```

The analytical store is rebuildable from retained event and reference inputs. It must preserve lineage to source events, analytical schema versions, late-arriving corrections, and bitemporal semantics. Analytical access remains behind the governed semantic query layer, never direct unrestricted SQL for agents.

#### Event Fabric and Replay-Safe Consumers

The authoritative transaction ends after one database commit:

```text
BEGIN
  append ledger events
  update required critical projections
  append outbox records
COMMIT
```

It must not synchronously require successful writes to search, analytics, embeddings, cache, archive indexing, notifications, or every external system.

The committed outbox feeds an event fabric whose independent consumers include:

- Operational projectors
- Search and semantic projectors
- Analytical ingestion
- Cache invalidation
- Integration workers
- Reconciliation services
- Operational-agent subscriptions
- Evidence archival

Kafka-class idempotent and transactional features can strengthen guarantees inside the event fabric. They do not create magical exactly-once effects in external databases and vendors. Every consumer remains idempotent and replay-safe:

```text
consume event 991
apply event 991
checkpoint 991
crash
consume event 991 again
-> detect already applied
-> no duplicate business effect
```

Consumer contracts define ordering key, deduplication identity, retry policy, poison-event handling, replay behavior, schema compatibility, watermark reporting, and backfill isolation.

#### Provenance and Watermarks Everywhere

Every serving plane exposes enough provenance to measure freshness rather than infer it.

```text
ledger head       9,128
hot projection    9,128  current
search            9,127  lag 1
analytics         9,100  lag 28
semantic vectors  9,033  lag 95
```

The exact watermark may be per tenant, partition, domain, or stream. A single global number is not required. The platform must make the scope explicit and translate lag into business meaning such as “three recent compensation events are not searchable yet.”

Every returned representation should be able to expose or internally trace:

- Source event and sequence
- Projection or index schema version
- Projector or ingestion version
- Embedding model and source hash where relevant
- Generated or indexed time
- Current freshness state

#### Reconciliation and Shadow Rebuild

An Integrity and Reconciliation Service continuously compares derived planes with authoritative inputs.

```text
Ledger + authoritative artifacts
              |
              v
       expected representation
              |
     +--------+--------+---------+
     v                 v         v
 projection          search   analytics
     |                 |         |
     +--------+--------+---------+
              v
         reconciler
          /      \
       match      drift
                    |
                    v
             governed RepairPlan
```

Verification methods include:

- Watermark and sequence checks
- Count and completeness reconciliation
- Content hashes and checksum sampling
- Random independent replay
- Sensitive-transaction verification
- Partition or tenant rebuild
- Full and shadow rebuilds

A new projector, search schema, analytical model, or embedding generation is built beside the live version:

```text
build shadow
  -> replay authoritative inputs
  -> compare with live and invariants
  -> investigate expected and unexpected differences
  -> approve atomic alias or routing switch
  -> retain rollback window
  -> retire old representation
```

Rebuildability is a tested recovery objective, not an architectural slogan. The roadmap must define maximum rebuild time, replay capacity, dependency availability, and evidence that retained source data is sufficient.

#### Separate but Correlated Telemetry

OpenTelemetry-class logs, metrics, and traces remain outside the business ledger. They carry trace and resource context and are correlated using:

```text
tenant_id
workflow_instance_id
transaction_id
event_id
decision_id
agent_execution_id
correlation_id
trace_id
span_id
```

An operator can navigate from a business event such as `PayrollWriteFailed` to the distributed trace and back to the transaction timeline. Technical stack traces, SQL duration, CPU, retry latency, and network timing follow operational retention rather than permanent business-history rules.

#### Physical Event Classes

The system should not hash-chain every interaction or mouse click into the same storage path as payroll execution.

| Class                           | Examples                                                        | Integrity and retention posture                             |
| ------------------------------- | --------------------------------------------------------------- | ----------------------------------------------------------- |
| Business ledger                 | Promotion, payroll, termination, IAM grant, approval, repair    | Strongest integrity, ordered streams, long governed history |
| Decision and semantic histories | Assessments, recommendations, decisions, hypotheses             | Strong provenance, versioning, purpose-specific retention   |
| Activity stream                 | Profile viewed, search run, report exported, workflow abandoned | High volume, selective evidence, shorter retention          |
| Telemetry                       | Request trace, CPU, SQL timing, stack trace                     | Operational retention and trace correlation                 |

These are **logical evidence classes**, not a requirement for four independent ledger products or databases. Phase 1 may store business, decision, and selected activity events in partitioned PostgreSQL tables with distinct schemas, privileges, integrity, and retention policies. Agent traces and software telemetry may use operational stores. Physical separation occurs only when volume, privilege, residency, retention, or blast-radius evidence justifies it.

Material activity such as a sensitive-data export may also produce a business audit event. Classification depends on business and legal significance, not merely on which component emitted it.

#### Governed Query Router

Clients and agents call semantic query capabilities rather than choosing databases.

| Question                                     | Preferred serving plane                              |
| -------------------------------------------- | ---------------------------------------------------- |
| Get Jane's current job                       | Cache or hot operational projection                  |
| Find “Jennifer Rodrigez”                     | Fuzzy search projection                              |
| Find profiles semantically similar to a role | Filtered hybrid/semantic retrieval                   |
| Show five-year turnover by organization      | Analytical plane                                     |
| Explain what happened to Jane                | Timeline projection and ledger                       |
| Retrieve Jane's signed offer                 | Authorized object plane                              |
| What was effective or known on June 1?       | Bitemporal projection or ledger-derived temporal API |

The router applies current AuthZ, LegalContext, purpose, residency, query/population controls, freshness requirements, and evidence type before selecting a plan. A stale search index cannot satisfy a strongly current payroll or termination question simply because it responds quickly.

#### Storage Responsibility Matrix

| Plane                   | Reference technology class | Truth status                         | Primary responsibility                             |
| ----------------------- | -------------------------- | ------------------------------------ | -------------------------------------------------- |
| Business ledger         | PostgreSQL                 | Authoritative event chronology       | Immutable material business history                |
| Critical projections    | PostgreSQL                 | Rebuildable, transactionally current | Immediate domain correctness and reads             |
| Operational projections | PostgreSQL                 | Rebuildable                          | Domain and experience read models                  |
| Object plane            | S3-compatible object store | Authoritative for referenced content | Large artifacts, evidence, archive, legal hold     |
| Event fabric            | Kafka-class                | Durable transport/replay buffer      | Independent fan-out and backfill                   |
| Cache                   | Redis-class                | Disposable                           | Ultra-low-latency hot reads                        |
| Search                  | OpenSearch-class           | Rebuildable                          | Exact, full-text, fuzzy, faceted, hybrid retrieval |
| Analytics               | ClickHouse-class           | Rebuildable                          | Cross-domain and temporal OLAP                     |
| Semantic/vector index   | Search/vector engine       | Rebuildable                          | Governed semantic discovery                        |
| Telemetry               | OpenTelemetry backend      | Operational                          | Logs, metrics, traces, performance diagnosis       |

The core physical-data rule is:

> Every important business mutation, material failure, decision, reconciliation result, and repair becomes a governed immutable fact. Large authority-bearing artifacts are content-addressed and independently protected. Every serving representation is attributable to those sources, observable for lag and drift, and replaceable through tested replay.

### 9.16 Entitlement, Metering, Billing, and Cost Plane

Billing is a first-class platform plane. It is separate from AuthZ and the business ledger, but it consumes the same tenant, organization, capability, workflow, agent, and provenance concepts.

The governing separation is:

```text
Entitlements    what the customer purchased and may activate
Authorization   which principal may act on which resource
Metering        what billable or cost-bearing usage occurred
Rating          what that usage is worth under an effective contract
Billing         charges, credits, tax inputs, statements, and invoices
Cost accounting what HCM Next paid to provide the usage
```

All six may evaluate the same capability execution, but no one substitutes for another. Purchasing Payroll does not authorize every employee to run it. Having authorization does not mean the tenant purchased it. Recording an HTTP request does not prove a billable unit was delivered.

#### Target Billing Architecture

```text
                 Customer Contract
                         |
                         v
                 Product + Price Book
                         |
                         v
                   Entitlements
                         |
                         v
Human / App / Agent -> Capability Gateway
                         |
       +-----------------+------------------+
       |                 |                  |
       v                 v                  v
    AuthZ             Legal              Limits
       |                 |             rate / quota
       +-----------------+------------------+
                         |
                         v
                 Deterministic Execution
                         |
                         v
                  Semantic Usage Events
                         |
                         v
                    Metering Plane
                         |
              +----------+----------+
              |                     |
              v                     v
         Rating Engine          Cost Engine
              |                     |
              v                     v
        Billing Ledger          COGS Ledger
              |                     |
              +----------+----------+
                         v
             Invoice / Showback / Margin
```

This is a logical architecture. Early commercial operations may rely on an external invoicing and payment provider. HCM Next still owns the semantic contracts, usage evidence, entitlement decisions, contract versions, rating provenance, and reconciliation required to explain a charge.

#### Do Not Bill From Telemetry

Technical telemetry may observe:

```text
HTTP request
gRPC invocation
agent tool call
workflow transition
vector query
analytics scan
```

The commercial unit may instead be:

```text
active worker month
employee paid
completed premium workflow
AI credit
API compute unit
storage GB-month
connector month
```

A capability produces a `UsageEvent` only when its meter contract declares that a customer-visible or cost-bearing event occurred. Logs and traces can diagnose the execution but cannot be the invoice source.

```text
UsageEvent
  usage_event_id
  tenant_id
  billing_account_id
  usage_scope_id?
  payer_scope_id?

  capability_id
  capability_version
  meter_id
  meter_version
  quantity
  unit

  occurred_at
  recorded_at

  business_execution_id
  workflow_instance_id?
  agent_execution_id?
  correlation_id

  contract_id
  entitlement_version
  pricing_context_ref

  idempotency_key
  source_event_ref
```

Usage events are canonical metering evidence, not automatically charges. Rating may determine that usage was included, discounted, free, credited, committed, overage, or not customer-billable.

#### Append-Only Billing Ledger

Invoices cannot be reconstructed safely from current product configuration. Billing requires its own append-only financial history:

```text
SubscriptionActivated
EntitlementGranted
EntitlementRevoked
ContractChanged
UsageRecorded
UsageAdjusted
ChargeRated
CreditGranted
InvoiceIssued
PaymentObserved
RefundIssued
DisputeOpened
DisputeResolved
```

Historical pricing is immutable:

```text
August usage -> contract v7 -> price book v12
September amendment -> contract v8 -> price book v13
```

September changes do not rerate August silently. If an error is found:

```text
ChargeRated       +500 USD
BillingAdjustment -100 USD
Net                400 USD
```

The correction preserves the original calculation, correcting authority, reason, approval, tax implication, and customer-visible explanation.

#### Billing Accounts and Corporate Hierarchy

Billing follows the corporate hierarchy without equating payer scope with usage scope.

```text
Tenant MegaCorp
  |
  +-- Billing Account: Global Headquarters
  |      pays platform and People Core
  |
  +-- Company US
  |      attributed AI, Payroll, and API usage
  |
  +-- Company Colombia
  |      attributed Payroll and storage usage
  |
  `-- Company Germany
         local billing account for selected modules
```

Every commercial resource may define:

```text
contract_scope
entitlement_scope
usage_scope
payer_scope
invoice_scope
allocation_scope
```

This supports consolidated invoices with subsidiary attribution, split contracts, parent-funded products, locally funded overages, and aggregate-only corporate showback without broadening HR-data access.

Billing access follows the same organization and field-governance rules as other domains. A parent finance administrator may view aggregate subsidiary charges without seeing which workers caused sensitive Employee Relations or medical-document activity.

#### Product Catalog, Entitlements, and Packages

Customers should buy understandable products, not dozens of infrastructure meters.

```text
HCM Next Platform
  + People
  + Workforce
  + Talent
  + Rewards
  + Experience
  + Access
  + Workforce Intelligence
  + Agent Platform
  + Developer/API Platform
```

Each versioned product definition declares:

- Included capability sets
- Enabled tenant and organization scopes
- Population basis
- Included allowances
- Overage policy
- Limits and service levels
- Dependencies and incompatible combinations
- Effective dates
- Commercial display and invoice grouping

Example:

```text
Product: Workforce Intelligence Pro

includes
  analytics.cross_domain.*
  analytics.semantic.*
  analytics.prediction.*

allowances
  analytics units per month
  AI credits per month

scope
  selected enterprise group
```

Entitlement resolution follows explicit corporate inheritance. A product may be purchased tenant-wide, enabled for selected subsidiaries, or delegated for local purchase. Parent restrictions and contract limits cannot be weakened by child configuration unless the contract expressly permits it.

#### Entitlement Is Not Authorization

The capability gateway evaluates:

```text
authentication
  -> tenant resolution
  -> entitlement
  -> authorization
  -> legal and purpose policy
  -> quota and rate limits
  -> execution
```

These stages return distinct explanations:

```text
NOT_ENTITLED
  Payroll is not enabled for Company B.

NOT_AUTHORIZED
  Payroll is enabled, but this principal cannot release a run.

QUOTA_EXCEEDED
  Included analytics allowance is exhausted and overage is disabled.

RATE_LIMITED
  The current consumption rate exceeds the contracted minute limit.
```

Commercial policy must not masquerade as a security denial, and security cannot be bypassed by purchasing a higher product tier.

#### Reusable Meter Classes

The platform supports several semantic meter families.

| Meter family   | Illustrative units                                                                | Typical commercial role                     |
| -------------- | --------------------------------------------------------------------------------- | ------------------------------------------- |
| Population     | active-worker month, contractor month, payroll-worker month, recruiter-seat month | Predictable module subscription             |
| Capability/API | standard API unit, compute unit, bulk unit, webhook delivery                      | Developer and integration platform          |
| Workflow       | completed premium workflow, batch execution, premium step                         | Selected packaged automation                |
| AI             | agent request, AI credit, deep analysis, embedding generation                     | Variable agent consumption                  |
| Data           | hot, document, vector, or archive GB-month                                        | Retention and data-scale overage            |
| Analytics      | analytical compute unit, bytes-scanned band, scheduled report                     | Workforce Intelligence consumption          |
| Integration    | connector month, premium connector, integration execution                         | Connector packaging and variable operations |

Meter definitions are versioned semantic objects with unit, aggregation period, rounding, minimum increment, deduplication rules, late-arrival policy, adjustment policy, and customer-display behavior.

Core HCM should generally use predictable subscription or population pricing. Metering ordinary internal workflow steps creates confusing incentives and unpredictable invoices. Consumption pricing is best reserved for economically variable surfaces such as AI, external APIs, high-scale analytics, exceptional storage, and premium integrations.

#### Hybrid Commercial Model

The likely commercial architecture has three layers:

```text
Platform subscription
       |
       v
HCM modules
PEPM / employee paid / licensed population
       |
       v
Variable platform usage
AI / external API / analytics / storage / premium integrations
```

Enterprise contracts may convert these into a committed annual spend, minimum, negotiated allowance, or all-inclusive tier. The underlying meters remain necessary for contract compliance, internal economics, forecasting, and explainable chargeback.

PEPM remains a plausible primary unit for People, Workforce, Talent, Benefits, and Access. Payroll may use employees paid or payroll populations. The plan treats exact prices and packaging as commercial hypotheses to validate, not technical truths.

#### Included Allowances and Overage

Variable products should support predictable included capacity:

```text
Enterprise HCM
  licensed population: 10,000 workers

included each month
  standard API units
  AI credits
  document storage
  semantic/vector storage
  analytical compute units

overage
  explicit unit price or disabled
```

Contracts declare:

- Hard or soft quota behavior
- Whether overage is automatic, approval-gated, or prohibited
- Alert thresholds
- Rollover or expiration policy
- Burst rules
- Prepaid and promotional credits
- Currency, tax treatment inputs, discounts, and minimum commitments

Customers must be warned before cost-affecting thresholds whenever practical. An emergency product operation must never be silently blocked solely by a commercial limit when doing so would create a greater payroll, security, legal, or worker-safety risk; the contract and policy must define emergency behavior and subsequent review.

#### API Compute Units and Bulk Work

Request count is an inadequate pricing unit:

```text
GET one worker                       small
search across a permitted org       moderate
export 500,000 workers               large
stream millions of gRPC records      large
```

The external API platform may normalize work into stable API compute units based on customer-visible semantic complexity, result scale, and resource bands. It should not expose raw database query count, internal RPC count, or deployment topology.

Bulk capabilities have distinct meters and entitlements because one request can have enormous cost and blast radius. Meter calculation must be deterministic, published, testable, and included in pre-execution estimates when the final quantity can be known or bounded.

#### Stable AI Credits Over Provider Tokens

Provider tokens, GPU seconds, embedding calls, and model-specific charges are important internal cost measures. They are a poor primary customer SKU because routing, model economics, and tokenization can change.

Customer-facing AI credits should represent stable task or compute bands:

```text
simple summarization        low credit band
normal agent task           standard band
deep analysis               high band
workflow generation         high band + compilation work
```

Internally, the same consumption is linked to:

```text
provider
model and version
input and output tokens
cached tokens
embedding work
tool iterations
compute time
provider price version
actual cost
```

This lets the Model Router improve cost and margin without unexpectedly changing the customer's commercial unit. Any router optimization remains subordinate to quality, privacy, residency, legal eligibility, and customer model commitments.

#### Customer Usage and Provider Cost Are Separate

The system maintains two related but distinct records:

```text
Customer usage and revenue
  tenant ACME
  400 AI credits
  customer charge 12.00 USD

Provider and infrastructure cost
  inference          2.11 USD
  embeddings         0.34 USD
  analytics compute  0.09 USD
  vector retrieval   0.04 USD
  total COGS         2.58 USD
```

This enables contribution and gross-margin analysis by:

- Tenant and billing account
- Product and capability
- Organization and legal entity
- Workflow and workflow version
- Agent and AgentDefinition version
- Provider and model
- Integration
- Data domain
- Time period

Actual cost cannot modify an already contracted customer rate unless the contract explicitly provides a variable pass-through mechanism.

#### Capability Billing Metadata

The Capability Registry declares billing behavior at the semantic boundary:

```text
Capability: analytics.cross_domain.query

billing
  meter: analytics_compute_unit
  pricing_class: premium
  included_in: workforce_intelligence_pro
  quota_behavior: soft_limit
  bill_to: calling_tenant
  estimation: supported
```

```text
Capability: worker.read

billing
  meter: none
  included_in: people_core
```

```text
Capability: agent.deep_workforce_analysis

billing
  meter: ai_credit
  base_units: 20
  variable_band: bounded
  estimate_before_execute: required
```

Internal service calls inherit the parent business execution for cost lineage but do not independently create customer charges unless the public meter contract explicitly says they do.

#### Agent Budgets

Agent budgets are both economic and safety controls.

```text
AgentBudget
  tenant_scope
  organization_scope
  agent_id
  period
  included_credits
  daily_limit
  per_run_limit
  cost_limit
  allow_overage
  warning_thresholds
  exhaustion_behavior
```

Effective agent capacity is constrained by the intersection of tenant allowance, organizational allocation, AgentDefinition policy, workflow budget, user authority, and current risk controls.

An agent loop, retry storm, or recursive delegation must not create unbounded provider cost. The runtime reserves or estimates a bounded amount before expensive execution, tracks actual usage, stops at safe boundaries, and produces a clear partial-result or escalation state when a budget is exhausted.

#### Workflow Cost Simulation

Agent-authored and high-volume workflows require a cost plan before publication:

```text
WorkflowCostPlan
  workflow_definition_version
  expected executions per period
  population assumptions
  API units
  AI credits
  analytical units
  document generation and storage
  connector usage
  estimated customer consumption
  estimated provider and infrastructure cost
  low / expected / high range
  assumptions and data versions
```

The simulation distinguishes:

- Included usage
- Forecast overage
- Internal HCM Next cost
- External customer-paid vendor cost
- Unknown or unmodeled cost

A workflow cost estimate is not a guaranteed invoice. Its assumptions, price-book version, model-routing policy, confidence, and expected volume remain visible.

#### Cost Lineage by Workflow and Agent

Every cost-bearing execution links customer-visible usage and internal cost to its semantic cause:

```text
New Manager Onboarding v12
  execution 991
    workflow completion      1
    API compute units       42
    AI credits              18
    document generation      1
    provider cost         0.37 USD
```

Customers can ask:

- Which agents consumed the AI allowance?
- Which workflows drove the increase?
- What usage was included versus overage?
- Which organization or cost center should receive showback?
- Which failed or repaired executions still generated cost?
- Which optimization changed usage or price?

Failed executions are not automatically free or billable. Each meter contract defines when value is considered delivered, when retries are included, and when service failures produce credits.

#### Chargeback, Showback, and Cost Analytics

The analytical model adds billing and cost dimensions to the Workforce Knowledge Graph:

```text
Billing
  x organization
  x legal entity
  x cost center
  x agent
  x workflow
  x capability
  x model
  x integration
  x product
  x time
```

One consolidated invoice can include internal allocation views:

```text
MegaCorp August
  Corporate HR       18,400 USD
  US Retail          27,100 USD
  Colombia            4,200 USD
  Germany             7,800 USD
  Recruiting         11,300 USD
```

Allocation rules are versioned and explainable. They may use direct attribution, population proportions, cost-center mapping, shared-service allocation, or customer-defined formulas. Showback does not create a legal invoice or move money unless a separate chargeback process authorizes it.

Useful metrics include cost per hire, onboarding, payroll employee, resolved HR case, workflow completion, API integration, agent outcome, and successful repair. These are descriptive unit economics unless the metric contract explicitly establishes a charge.

#### Versioned Contracts and Price Books

```text
CustomerContract
  contract_id
  tenant_id
  billing_accounts[]
  contract_scope
  product_versions[]
  price_book_version
  effective_from
  effective_to
  billing_currency
  commit_amount
  minimum_spend
  discounts[]
  entitlements[]
  meters[]
  allowances[]
  overages[]
  credits[]
  billing_cycle
  tax_context_ref
```

A usage event is rated under the contract and price book applicable to its defined usage time, subject to explicit late-arrival and amendment policy. Retroactive contract changes require a governed rerating or adjustment process; they do not mutate prior charges invisibly.

Contract publication uses validation, approval, effective dating, simulation, diff, and audit controls comparable to workflow and legal-policy publication.

#### Idempotent Metering and Reconciliation

Every billable semantic operation has a stable key:

```text
tenant
+ meter
+ business_execution_id
+ usage_dimension
-> usage idempotency key
```

API retry, workflow retry, duplicate wakeup, event redelivery, or projector replay cannot produce a duplicate billable unit. Legitimate repeated consumption receives distinct business-execution identities.

Billing reconciliation compares:

```text
semantic executions
  vs usage events
  vs rated charges
  vs invoice lines
  vs payments / credits
  vs provider-cost records
```

Mismatches produce a billing incident and governed adjustment or replay. They are never silently repaired through direct table edits.

#### Rate Limits, Quotas, Budgets, and Charges

The following controls may share usage counters but have different meanings:

```text
rate limit  how quickly usage may occur
quota       how much is permitted in a period
budget      how much cost or consumption an owner permits
billing     how much the customer owes
```

Example:

```text
included allowance  10 million API units / month
rate limit           20,000 units / minute
budget alert         80% and 95%
overage              contracted rate per million units
```

Rate-limit failure must not create a charge. A billable operation must not be double-counted because it crossed several counters. Current counters, final rated usage, and invoice evidence must reconcile while retaining their distinct semantics.

#### Billing Capabilities

Billing participates in the dense API fabric:

```text
entitlements.read
entitlements.explain
entitlements.simulate

billing.usage.query
billing.usage.explain
billing.cost.query
billing.forecast

billing.invoices.list
billing.invoices.read
billing.credits.read

billing.budgets.read
billing.budgets.configure

billing.allocations.preview
billing.allocations.publish
billing.showback.query

billing.contracts.validate
billing.contracts.simulate
billing.contracts.publish
```

Agents use these capabilities rather than direct billing tables or payment-provider credentials. An agent explaining a bill must cite the product, meter, usage events, rating version, allowance, adjustment, and allocation that produced the amount.

Changes that affect legal invoices, refunds, credits, contract terms, payer identity, payment instruments, or tax treatment receive high-risk capability classifications and appropriate human approval, separation of duties, and step-up authentication.

#### Billing Privacy and Security Boundary

Billing records are sensitive, but they should minimize replicated worker data. Usage events prefer stable subject, workflow, organization, and cost-allocation references over employee names or raw HR payloads.

Billing policy governs:

- Who can see contracts, invoices, usage, internal cost, or margin
- Whether parent organizations receive detail, aggregate, or no subsidiary visibility
- Which HR domain labels may appear on invoices and showback
- Retention, residency, taxation evidence, and legal hold
- Segregation between customer support, finance, engineering, and provider-cost access
- Redaction of sensitive workflow purposes in commercial explanations

Internal provider cost and margin are HCM Next-confidential by default and are not automatically exposed through customer billing APIs.

#### Commercial Explanation Contract

Every material invoice line should be explainable as:

```text
product or meter
+ usage scope
+ measured quantity
+ included allowance
+ effective rate and discount
+ credits or adjustments
+ allocation
+ currency and tax inputs
= invoice result
```

This supports questions such as:

> Why did AI spend rise 32% this month?

with an evidence-backed answer grouped by agent, workflow, organization, model-routing effect, allowance, and price version rather than raw infrastructure logs.

The central billing rule is:

> Entitle at the product boundary, authorize at the principal and resource boundary, meter at the semantic usage boundary, rate against immutable effective contracts, bill through append-only financial history, and preserve provider cost separately for margin and routing decisions.

### 9.17 Common Workflow Simulation Contract

Every material workflow must expose one common simulation contract before it can execute. The contract is the bridge among user intent, agent planning, workflow compilation, authorization, legal obligations, billing, deterministic execution, reconciliation, and repair.

```text
Intent
  -> WorkflowSimulationRequest
  -> immutable baseline and proposed state
  -> authority + legal + entitlement resolution
  -> conflict and effective-time analysis
  -> planned reads, writes, decisions, and side effects
  -> risk, confidence, cost, completion, and repair model
  -> WorkflowSimulationResult
  -> human or system review
  -> executable TransactionPlan
```

The request contains:

```text
WorkflowSimulationRequest
  tenant and organization scope
  actor, agent, delegation, and purpose
  workflow definition and version
  intent and immutable proposal revision
  subjects and target resources
  requested effective dates and times
  known baseline versions
  execution mode and requested autonomy
  legal and locale hints
  customer assumptions
```

The result contains:

| Contract area           | Required output                                                                                       |
| ----------------------- | ----------------------------------------------------------------------------------------------------- |
| Identity and proposal   | Immutable proposal hash, subjects, person/employment resolution, baseline versions                    |
| Planned reads           | Resources, fields, source authority, freshness, known-at and effective-at time                        |
| Planned writes          | Resource, fields, before/after state, effective range, owning authority, atomic grouping              |
| Conflicts               | Overlapping transactions, reservations, stale baselines, mergeability, supersession policy            |
| Authorization           | Current decision, policy versions, fields, scope, conditions, step-up, and required reevaluation      |
| Legal and privacy       | LegalContext, rules, prohibitions, obligations, documents, notices, retention, residency, human gates |
| Regulatory computation  | JurisdictionContext, Rule Packs, composition traces, calculations, filings, deadlines, and evidence   |
| Entitlements and limits | Product eligibility, quota, rate-limit, budget, and emergency-limit behavior                          |
| Decisions and approvals | Required decision types, routing, immutable binding, separation of duties, expiry, invalidators       |
| External effects        | System, operation, ordering, payload intent, idempotency, confidence, and observation strategy        |
| Time                    | Business dates, instants, timezones, calendars, deadlines, payroll cutoffs, and safe points           |
| Risk                    | Capability risk, financial, worker, legal, privacy, security, bulk, and reversibility classifications |
| Cost                    | Included and variable usage, customer estimate, provider-cost range, assumptions, and price versions  |
| Expected events         | Business, decision, usage, reconciliation, incident, and evidence events                              |
| Completion model        | Business completion, external consistency, reconciliation, operational, and obligation dimensions     |
| Repair and compensation | Retry, compensate, roll-forward, rebuild, supersede, escalation, and impossible-to-reverse paths      |
| Confidence and unknowns | Deterministic, policy-derived, connector-derived, estimated, and unmodeled effects                    |
| Revalidation            | Facts, policies, authority, laws, budgets, conflicts, and contexts that must be checked again         |
| Execution fingerprint   | Workflow, blocks, schemas, policies, mappings, metadata, prompts, runtime, and simulation versions    |

Simulation does not reserve authority or guarantee execution. It may create explicit position, headcount, budget, or capacity reservations when the workflow and policy require them, but those reservations have their own identity, scope, expiry, and release behavior.

The result classifies each effect:

```text
DETERMINISTIC
  governed HCM Next calculation or state transition

POLICY_DERIVED
  result from versioned AuthZ, Legal, workflow, or customer policy

CONNECTOR_DERIVED
  intended payload or documented connector behavior

EXTERNAL_ESTIMATE
  predicted result in a system HCM Next does not control

UNKNOWN
  downstream automation or circumstance the platform cannot model
```

Simulation must never describe an unknown external outcome as guaranteed.

#### Planned Side-Effect Semantics

Every side effect declares:

```text
effect_id
domain and target system
semantic operation
reads and writes
effective time
ordering dependencies
parallel group?
atomic region?
idempotency identity
retry policy
reversible?
compensation operation?
irreversible boundary?
observation and reconciliation plan
business criticality
```

This lets the runtime distinguish work that can happen in parallel from security-critical or financially ordered work and prevents a generic DAG from obscuring business semantics.

#### Multidimensional Completion

Every reference workflow reports at least:

```text
BusinessState
  NOT_STARTED / IN_PROGRESS / COMPLETED / CANCELLED / CORRECTED

ExternalConsistency
  NOT_REQUIRED / PENDING / CONSISTENT / DEGRADED / CONFLICTED

ReconciliationState
  NOT_STARTED / RUNNING / MATCHED / MISMATCHED / REPAIR_REQUIRED

OperationalState
  HEALTHY / WAITING / RETRYING / INCIDENT / PAUSED / QUARANTINED

ObligationState
  PENDING / SATISFIED / OVERDUE / WAIVED_WITH_AUTHORITY
```

One field must not collapse these dimensions into `SUCCEEDED` or `FAILED`.

#### Scenario Matrix

Each reference workflow supplies deterministic simulation fixtures for:

- Happy path
- Duplicate submission and replay
- Stale proposal or approval
- Same-field and mergeable cross-workflow conflicts
- Future-dated and retroactive change
- Policy, legal, authorization, or entitlement change before execution
- Missing or ambiguous person and resource identity
- Partial external failure and delayed observation
- Pause, cancellation, bypass, repair, correction, and reinstatement
- Sensitive-field redaction by role, purpose, organization, and agent
- Budget, quota, rate-limit, and estimated-cost boundary
- Workflow version migration and quarantine
- Timezone, locale, currency, and business-calendar edge cases
- Ledger, projection, search, and analytical replay

The five lifecycle workflows and payroll-correction stress test form a standing architecture conformance suite. A change to Worker Graph semantics, workflow runtime, AuthZ, Legal, regulatory computation, billing, integration, data-plane, or agent behavior is incomplete until affected reference scenarios compile, simulate, execute in a controlled environment, reconcile, and preserve expected evidence.

### 9.18 Regulatory Computation Platform

When HCM Next becomes authoritative for global HCM domains, jurisdictional computation becomes a core substrate alongside AuthZ, workflow, ledger, globalization, data, billing, and agents.

It should not become one giant “legal rules engine.” Tax, wage and hour, leave, privacy, immigration, statutory benefits, payroll reporting, labor agreements, and government notices have materially different calculation and composition semantics. They share a jurisdiction graph, versioned Rule Pack lifecycle, obligation vocabulary, provenance model, effective-time model, and execution boundary.

```text
                         Proposed Action
                                |
                                v
                     Jurisdiction Resolver
                                |
       +------------------------+------------------------+
       |                        |                        |
       v                        v                        v
 Supranational / National   State / Province        Local Authority
       |                        |                        |
       +------------------------+------------------------+
                                |
                                v
                       Applicable Rule Packs
                                |
       +---------------+--------+--------+---------------+
       |               |                 |               |
       v               v                 v               v
 Tax and Payroll   Wage / Leave       Privacy       Reporting
 Calculations      Obligations         Rules         Definitions
       |               |                 |               |
       +---------------+--------+--------+---------------+
                                v
                       Obligation Resolver
                                |
                   +------------+------------+
                   |            |            |
                   v            v            v
               Calculate      Require      Prohibit
                   |            |            |
                   +------------+------------+
                                v
                  Workflow Simulation / Transaction Plan
                                |
                                v
                              Ledger
```

#### Relationship to the Legal and Globalization Planes

The planes have distinct responsibilities:

```text
Globalization
  canonical values, language, formatting, money, timezones, calendars

Legal and Compliance
  lawful action, purpose, interpretation, prohibitions, privacy, counsel control

Regulatory Computation
  jurisdiction resolution, deterministic calculation, obligations, filings, deadlines

Workflow
  sequence, approvals, human tasks, side effects, repair

Ledger
  exact facts, rule versions, calculations, decisions, submissions, acknowledgements
```

The Regulatory Platform consumes LegalContext and LocaleContext but does not replace them. A language or UI locale does not determine tax jurisdiction. A deterministic tax result does not by itself establish that the organization may lawfully process every input used to produce it.

#### Jurisdiction Graph

Government and regulatory authority is modeled as a versioned, effective-dated graph rather than a flat country code.

```text
United States
  +-- Federal
  +-- California
  |     +-- Los Angeles County
  |     `-- City of Los Angeles
  +-- Florida
  |     `-- applicable local authorities
  `-- New York
        +-- New York State
        `-- New York City

European Union
  `-- Germany
        +-- Federal
        +-- State
        `-- local or authority-specific scope

Canada
  +-- Federal
  +-- Ontario
  `-- Quebec
```

Graph edges declare the relationship rather than assuming every child simply overrides a parent:

```text
contains
overlaps
implements
delegates_to
reports_to
shares_authority_with
supersedes_for_domain
```

Jurisdiction boundary changes are themselves effective-dated. Historical calculations must resolve the boundary and authority graph that applied at the transaction's relevant time.

#### JurisdictionContext

No single address or `country` column can resolve global obligations. Every material transaction may require:

```text
JurisdictionContext
  person_residence_jurisdictions[]
  employment_jurisdictions[]
  physical_work_jurisdictions[]
  remote_work_jurisdictions[]
  payroll_establishment_jurisdictions[]
  tax_residence_jurisdictions[]
  tax_work_jurisdictions[]
  benefits_jurisdictions[]
  privacy_and_data_subject_jurisdictions[]
  data_processing_and_storage_jurisdictions[]
  legal_entity_registration_jurisdictions[]
  immigration_jurisdictions[]
  collective_agreements[]
  employment_contracts[]
  company_policies[]
  effective_at
  known_at
  evidence_refs[]
```

Each resolved jurisdiction records source evidence and confidence. A missing or ambiguous work location cannot silently fall back to the legal-entity country for payroll or tax computation.

Remote and mobile work require temporal location evidence:

```text
worker resides in Florida
employed by Delaware entity
works 40 days in California
travels 20 days in New York
paid from US payroll
```

The platform must explicitly determine which facts are authoritative, which are reported, which remain unverified, and which engines require human or specialist resolution.

#### Common Rule Pack Lifecycle

Regulatory content is packaged by authority, domain, and effective period:

```text
RulePack
  rule_pack_id
  jurisdiction_id
  authority_id
  domain
  effective_from
  effective_to
  rules[]
  dependencies[]
  composition_strategy
  source_references[]
  legal_citations[]
  interpretation_status
  owner
  version
  status
  reviewed_at
  next_review_due
```

Illustrative identities are:

```text
US.FED.PAYROLL.2026
US.CA.WAGE_HOUR.2026
US.CA.LA.LOCAL.2026
EU.GDPR
DE.EMPLOYMENT.2026
CO.PAYROLL.2026
ACME.GLOBAL.POLICY
ACME.CO.CBA.17
```

Rule Pack status includes:

```text
draft
under_review
validated
approved
future_effective
active
quarantined
superseded
retired
```

Interpretation provenance includes:

```text
statutory_source
regulator_guidance
vendor_maintained_baseline
customer_counsel_approved
customer_defined_policy
collective_agreement
employment_contract
benefit_plan
```

Rules execute only after schema validation, deterministic tests, impact simulation, appropriate legal or specialist review, and effective-dated publication.

#### Domain-Specific Composition Strategies

“Most specific wins” is not a safe universal policy. Rule families declare how applicable results compose.

```text
ADDITIVE
  federal + state + local tax

MOST_PROTECTIVE
  worker receives the strongest applicable minimum standard

MOST_RESTRICTIVE
  all applicable restrictions constrain the action

PRECEDENCE
  an authoritative hierarchy selects the controlling rule

CONCURRENT
  entitlements run together under defined coordination rules

OFFSET
  one benefit or payment offsets another

STACK
  entitlements accumulate

EXCLUSIVE
  exactly one classified rule path applies

CUSTOM
  a domain-specific deterministic composition function
```

Composition is part of the versioned rule family, not handwritten workflow logic. The result explains every included, excluded, overridden, offset, and unresolved rule.

#### Specialized Deterministic Engines

The common framework supports distinct engines:

```text
Tax Engine
Wage and Hour Engine
Leave and Statutory Entitlement Engine
Benefits and Social Contribution Engine
Privacy and Transfer Constraint Engine
Immigration and Work Authorization Engine
Government Reporting Engine
Obligation Engine
Statutory Calendar Engine
Regulatory Change Manager
```

These engines share types and provenance but retain domain ownership. A privacy prohibition is not modeled as a negative tax amount, and a statutory filing deadline is not represented as an approval role.

#### Tax Engine

The Tax Engine is a deterministic financial calculation kernel.

```text
Gross payroll inputs
        |
        v
Taxability classification
        |
        +-- employee withholding
        +-- employer tax and social contribution
        +-- jurisdictional wage bases
        +-- YTD and period accumulators
        |
        v
TaxCalculationResult
```

Inputs include:

- Worker tax profile and classification
- Employment and legal entity
- Residence and physical work locations
- Payroll establishment and applicable jurisdictions
- Gross earnings by typed earning code
- Pretax and post-tax deductions
- Tax elections and certificates
- Pay frequency and period
- Year-to-date accumulators and wage bases
- Retroactive and supplemental-pay context
- Currency, rounding, and conversion policy
- Tax Rule Pack versions

Outputs include:

- Taxable wages by authority and category
- Employee withholding by authority
- Employer taxes and social contributions
- Wage-base consumption and year-to-date accumulators
- Rounding and currency results
- Warnings, unresolved inputs, and calculation status
- Complete rule and formula trace

Worker classification is a governed calculation input. Employee, independent contractor, statutory employee, contingent worker, and other jurisdiction-specific classifications can materially change withholding, employer contribution, reporting, and benefit obligations. Classification therefore requires evidence, decision provenance, review, effective dating, and correction—not merely a UI label.

Every result must be explainable:

```text
Tax result
  authority and jurisdiction
  tax type
  engine version
  Rule Pack and rule versions
  permitted input snapshot
  formula stages
  wage bases and accumulators
  rounding
  final amount
  warnings and assumptions
```

Agents explain this deterministic trace; they do not invent tax calculations.

#### Government Reporting Engine

Calculation and reporting are separate. A tax amount may feed several periodic, event-driven, correction, and worker-facing reports.

```text
GovernmentReportDefinition
  authority
  jurisdiction
  report_type
  reporting_period_rule
  source_facts[]
  source_watermark_policy
  calculations[]
  validation_rules[]
  reconciliation_rules[]
  artifact_templates[]
  submission_format
  submission_channel
  due_date_rule
  acknowledgement_protocol
  correction_process
  version
```

A filing is a governed workflow:

```text
reporting period closed
  -> reporting projection finalized
  -> report generated
  -> validation
  -> reconciliation to payroll and tax ledgers
  -> controller approval
  -> submission
  -> government acknowledgement
  -> accepted, rejected, or pending
  -> repair or amendment if required
```

For example, US Form 941 reporting includes federal income, Social Security, and Medicare taxes withheld and the employer share of Social Security and Medicare taxes. That specificity belongs in a versioned US federal report definition, not in the generic workflow runtime.

#### Immutable Filing Packages

Every submission produces an authority-bearing `FilingPackage`:

```text
FilingPackage
  filing_id
  tenant and legal_entity
  jurisdiction and authority
  report_definition_version
  reporting_period
  source_data_watermarks[]
  input_snapshot_hash
  calculated_values
  validation_results
  reconciliation_results
  rendered_artifact_ref and hash
  machine_payload_ref and hash
  approval_decisions[]
  submitted_at
  external_submission_id
  acknowledgement_ref
  status
  correction_of?
```

Original filings are not overwritten:

```text
OriginalFiling
  -> Rejection or CorrectionRequired
  -> AmendedFiling
  -> new acknowledgement
```

Documents and payloads live in the governed object plane; the filing timeline, hashes, versions, decisions, and authority responses are preserved in the ledger.

#### Obligation Engine

Calculation engines answer “how much?” or “what value?” The Obligation Engine answers “what must, may, or must not happen?”

```text
Obligation
  obligation_id
  type
  authority
  jurisdiction
  subject and related transaction
  trigger
  effective_at
  due_at
  required_action
  prohibited_action?
  evidence_required[]
  responsible_party
  satisfaction_condition
  dependency_obligations[]
  risk and penalty classification
  rule_source and version
  state
```

Typed outcomes include:

```text
CALCULATE
REQUIRE_STEP
REQUIRE_APPROVAL
REQUIRE_NOTICE
REQUIRE_DOCUMENT
REQUIRE_SIGNATURE
REQUIRE_REGISTRATION
REQUIRE_PAYMENT
REQUIRE_FILING
REQUIRE_WAIT_PERIOD
REQUIRE_EVIDENCE
REQUIRE_HUMAN_REVIEW
PROHIBIT
RESTRICT_DATA
RESTRICT_PURPOSE
RESTRICT_TRANSFER
```

Obligations have a lifecycle:

```text
identified
assigned
pending
satisfied
waived_with_authority
overdue
disputed
superseded
cancelled
```

HCM Next can therefore answer which obligations exist, what created them, who owns them, what evidence satisfies them, which are due soon, which are overdue, and which worker or company transactions remain blocked.

#### Statutory Calendar Engine

The existing calendar platform gains a regulatory purpose layer:

```text
tax deposit deadlines
government filing periods
employee notice periods
election windows
benefit deadlines
leave certification windows
visa and work-authorization expiry
training and license renewal
retention and destruction dates
```

A rule expresses semantic time:

```text
due 10 business days after termination
```

The engine resolves the applicable jurisdiction, authority calendar, weekend definition, holidays, cutoff time, timezone, extension rule, and version. The computed due date preserves a trace that can be reproduced later.

#### Country and Regional Packs

International support is delivered as modular regulatory packs over a shared global kernel:

```text
Global Regulatory Kernel
  + US Pack
      + California
          + Los Angeles
  + Colombia Pack
  + Germany Pack
  + UK Pack
  + Canada Pack
      + Ontario
      + Quebec
```

A country pack may include:

- Tax, payroll, wage, and social-contribution rules
- Employment and worker-classification rules
- Leave and statutory benefit programs
- Government report definitions and connectors
- Required documents, notices, and evidence
- Regulatory calendars
- Privacy, residency, transfer, and retention rules
- Currency, rounding, localization, and validation
- Test fixtures and regulatory source references

Child packs extend or compose with parent content under domain-specific strategies. They do not clone the entire national implementation.

Coverage is declared precisely. “Supports the United States” is not sufficient. A support manifest lists jurisdictions, domains, worker types, calculation and filing features, effective periods, known exclusions, connector status, validation level, and whether customer-counsel configuration remains required.

#### Statute, Contract, CBA, Plan, and Company Policy

Government rules are not the worker's only authority sources. Effective requirements may combine:

```text
supranational law
national law
state, province, and local law
regulator guidance
collective bargaining agreement
works council agreement
employment contract
benefit plan
company policy
```

Each source joins the same applicability and provenance framework but retains its authority type. Precedence and composition are declared by rule family and jurisdiction rather than assumed globally.

Customer counsel controls the final production interpretation layer for ambiguous or fact-sensitive content:

```text
government source
  -> HCM Next baseline content
  -> customer legal or specialist review
  -> customer-approved Rule Pack
  -> future-effective publication
```

The product can explain which approved rule ran. It does not claim to replace legal, tax, payroll, benefits, or immigration professionals.

#### Regulatory Change Management

Regulatory content change is itself a governed workflow:

```text
change detected
  -> source and effective date verified
  -> affected Rule Packs identified
  -> customer and jurisdiction coverage mapped
  -> workflow, calculation, filing, document, agent, and obligation impact simulated
  -> tests and shadow calculations
  -> specialist and customer review
  -> future-effective publication
  -> affected in-flight work revalidated
  -> production outcomes monitored
```

Impact analysis reports:

- Affected workers, companies, jurisdictions, and payroll populations
- Changed calculations and expected financial ranges
- New, changed, or removed obligations
- Filing definitions and government connectors
- Workflow steps, approvals, documents, and deadlines
- Existing future-effective transactions and long-running instances
- Required employee or administrator communications
- Agent capabilities, prompts, and knowledge that must change
- Historical correction or rerating implications

A defective Rule Pack can be quarantined. New calculations stop or fall back only under an approved continuity policy. Affected calculations, reports, transactions, and filings are identified from the execution fingerprint.

#### Reference Workflow Integration

The Regulatory Platform is exercised through the reference suite:

```text
Hire
  registration, elections, eligibility, notices, payroll setup,
  statutory benefits, work authorization, reports, and documents

Promotion
  pay rules, transparency, contract and CBA requirements,
  notice, tax, payroll, and benefit consequences

Cross-Company Transfer
  source and destination obligations, immigration, new tax and
  payroll jurisdictions, privacy transfer, benefits, filings

Leave and Return
  national + state/provincial + local + CBA + company programs,
  concurrent entitlement, pay, benefits, deadlines, evidence

Termination and Offboarding
  notice, final pay, tax reporting, benefits continuation,
  authority notifications, retention, access timing

Payroll Correction
  historical Rule Packs, bitemporal inputs, tax and contribution
  deltas, amended filings, accounting effects, reconciliation
```

These are jurisdiction-aware executable processes, not generic workflows with country-specific comments.

#### Agent Role

Agents reason over deterministic regulatory capabilities and evidence:

```text
“What changes if 27 workers move from Miami to Bogotá?”
  -> resolve affected people, employments, locations, entities
  -> query jurisdiction and Rule Pack coverage
  -> execute deterministic impact simulations
  -> compose People, compensation, payroll, tax, benefits,
     immigration, privacy, IAM, reporting, and cost results
  -> expose gaps, unknowns, and required specialist review
```

```text
“Why was this filing rejected?”
  -> government acknowledgement
  -> FilingPackage and definition
  -> report field and validation
  -> tax/payroll calculation trace
  -> source worker transaction and Rule Pack
```

Agents never become the unversioned source of regulatory truth. They may discover, compare, explain, draft impact analyses, and propose Rule Pack or workflow changes. Deterministic engines calculate and validate; authorized specialists approve ambiguous interpretations and material filings.

#### Regulatory Capabilities

The dense API fabric exposes:

```text
jurisdictions.resolve
jurisdictions.explain

regulations.evaluate
regulations.explain
regulations.coverage.read

tax.calculate
tax.explain

obligations.resolve
obligations.query
obligations.explain
obligations.satisfy

government_reports.generate
government_reports.validate
government_reports.reconcile
government_reports.submit
government_reports.correct

legal_calendar.resolve
legal_calendar.query

rulepacks.read
rulepacks.validate
rulepacks.simulate
rulepacks.publish
rulepacks.quarantine

regulatory_changes.impact_analyze
regulatory_changes.publish
```

Capability manifests identify required professional authority, supported jurisdictions, Rule Pack types, deterministic status, data domains, evidence, risk, cost, filing side effects, simulation support, and whether human approval is mandatory.

#### Reproducibility and Ledger Evidence

Every material regulatory result preserves:

```text
JurisdictionContext version and evidence
Rule Pack and rule versions
composition strategy and resolution trace
engine and calculation version
effective-at and known-at times
permitted input snapshot hash
calculation or obligation trace
workflow and approval versions
filing definition and artifact hashes
external submission and acknowledgement
correction or supersession lineage
```

A calculation performed under `US.FED.PAYROLL.2025.v8` remains reproducible after `US.FED.PAYROLL.2026.v3` becomes current. Authorization is still reevaluated at access and execution time, and applicable current law may require revalidation of pending work.

The central regulatory rule is:

> Resolve every applicable authority and source for the particular worker, employment, action, place, purpose, and time; compose each rule family under its declared semantics; execute calculations and filings deterministically; let specialists control interpretation; and ledger the exact evidence and versions that governed the result.

### 9.19 Foundation Gap-Closure Program

The platform vision now identifies seventy system-level capabilities. They should not become seventy independent services or simultaneous projects. HCM Next will close them through shared contracts, cohesive capability portfolios, and thin end-to-end slices through the reference workflows.

The immediate priority is ten foundations whose absence would force expensive redesign later:

1. Identity resolution
2. Cross-workflow conflict control
3. External mastering and source authority
4. Schema registry and data contracts
5. Data classification and DLP
6. Secrets, keys, and certificates
7. Configuration promotion and tenant sandboxing
8. Connector framework
9. Data quality and invariant correctness
10. Provenance graph

These foundations are related:

```text
Schema + reference data
        |
        v
Identity + authority + provenance
        |
        v
Conflict + reservation + approval binding + revalidation
        |
        v
Deterministic execution + connectors + compensation
        |
        v
Quality + invariants + reconciliation + repair

Across every layer:
classification + secrets + environment promotion + sandbox evidence
```

#### Foundation 1: Identity Resolution System

The Worker Graph requires a governed identity layer spanning candidate, person, worker, contractor, dependent, former-worker, service, and application identities.

Core objects:

```text
IdentityClaim
  claim_type
  normalized_value or protected reference
  source
  confidence
  verified_at
  classification

IdentityCandidate
  proposed Person links
  match features
  confidence
  explanation

IdentityResolution
  MATCH / CREATE / POSSIBLE_MATCH / DO_NOT_MERGE
  decision maker
  visible evidence
  policy and algorithm version

IdentityMergePlan
IdentitySeparationPlan
```

The subsystem must support:

- Deterministic identifiers and normalized claims
- Probabilistic candidate generation without autonomous merge
- Exact, fuzzy, phonetic, locale-aware, and former-identity matching where lawful
- Field-level privacy and purpose restrictions on identity evidence
- Human review for uncertain or high-impact matches
- Explicit do-not-merge and known-distinct markers
- Merge simulation showing ownership, conflicts, permissions, retention, and downstream effects
- Split or separation when an incorrect merge is discovered
- Stable historical references after merge, separation, rehire, or acquisition
- Reconciliation with external person and workforce identifiers

No subsystem may silently equate shared name, email, phone, or address with one person. The first conformance path is Candidate → Person → Worker → Employment in Recruit → Hire → Onboard.

Exit evidence:

- Duplicate submission is idempotent
- Former worker, contractor, and candidate scenarios resolve explainably
- False merge can be separated without erasing causal history
- Sensitive claims never leak through match explanations
- External identifiers retain source authority and correction lineage

#### Foundation 2: Cross-Workflow Conflict Engine

Every proposal declares a normalized write intent:

```text
AffectedResourceSet
AffectedFieldSet
EffectiveTimeRange
AuthorityDomain
ConflictPolicy
```

The engine indexes proposed, approved, reserved, executing, future-effective, and repair transactions. It detects:

- Same field and overlapping effective range
- Parent-child semantic conflict such as employment end versus compensation change
- Position capacity and occupancy conflict
- Budget and headcount conflict
- Relationship changes that invalidate authority or routing
- External-authority observations that changed the baseline
- Mergeable independent fields
- Explicit supersession and dependency

Results are typed:

```text
NO_CONFLICT
MERGEABLE
SERIALIZE
REQUIRES_REBASE
REQUIRES_REAPPROVAL
SUPERSEDE_CANDIDATE
BLOCKING_CONFLICT
```

Conflict detection is deterministic and re-runs at proposal, approval, scheduling, safe points, and execution. Agents may explain or suggest a resolution; domain policy decides whether work may merge, serialize, rebase, supersede, or stop.

The Promotion + Compensation and Transfer reference workflows form the first conflict matrix.

#### Foundation 3: Source Authority and External Mastering

Authority is resolved separately for transaction truth, employee-domain facts, integration execution, and observed external state.

```text
AuthorityPolicy
  tenant and organization scope
  domain / resource / field
  effective period
  authoritative system
  permitted writers
  observation sources
  source ranking
  conflict strategy
  promotion and rollback state
```

The engine answers:

- Which system may originate a value?
- Which system is authoritative for the current and future-effective fact?
- Is HCM Next authoritative, transactional, observational, or derived?
- Does an external change represent drift, legitimate concurrent work, or a new authority fact?
- May HCM Next repair the target, import the target value, open a dispute, or require a human decision?
- What happens during source migration or temporary authority handoff?

Authority changes are simulated, approved, effective-dated, reversible where possible, and reconciled before promotion. “Last write wins” is never the default business policy.

#### Foundation 4: Schema Registry and Data Contract System

All platform boundaries require registered, versioned contracts:

```text
Protobuf services and messages
HTTP and event representations
ledger event schemas
workflow inputs and outputs
capability manifests
projection schemas
integration contracts and mappings
semantic schemas and metrics
Rule Packs and regulatory results
agent tools and structured outputs
```

Every contract records:

```text
contract_id
kind
owner
source definition
version
compatibility class
effective status
classification metadata
deprecation timeline
consumers and adoption
generated artifacts and hashes
test fixtures
```

Compatibility classes include backward-compatible, conditionally compatible, migration-required, and forbidden. Publication runs linting, compilation, fixture validation, breaking-change analysis, generated-code drift checks, and consumer impact analysis.

SchemaFlux will be evaluated as the design-time structured-data compiler for domain metadata, capability manifests, semantic definitions, Rule Pack metadata, documentation, catalogs, test fixtures, and generated indexes. Protobuf remains the authoritative wire contract for gRPC. SchemaFlux custom backends may emit or validate Protobuf-adjacent metadata, OpenAPI descriptions, Go registries, reference documentation, and evaluation datasets, but generated output cannot silently diverge from the authoritative source definition.

#### Foundation 5: Data Classification and DLP

Classification is attached to schemas, fields, events, objects, search fragments, analytical columns, agent context, exports, and derived outputs.

```text
Classification
  public_workforce
  internal
  confidential_hr
  restricted_compensation
  restricted_payroll
  banking
  medical
  special_category
  employee_relations
  investigation
  immigration
  legal_privileged
  identity_secret
```

The DLP contract evaluates:

```text
data categories
+ source classification
+ derived-data lineage
+ principal and organization scope
+ purpose and LegalContext
+ destination and processor
+ region and residency
+ channel and export type
+ retention and redaction policy
```

It returns allow, deny, redact, tokenize, aggregate, minimum-cohort, require approval, require notice, or restrict destination. Deterministic schema classification is primary. Content inspection supplements it for uploads, documents, free text, agent output, and exports but never downgrades known classifications.

The first conformance cases are Leave medical evidence, bulk worker export, agent-derived risk assessment, billing explanations, and cross-border Transfer analysis.

#### Foundation 6: Secrets, Keys, and Certificate Plane

Agents, workflows, blocks, and customer code receive capabilities, not raw credentials. A dedicated plane manages:

- Connector credentials and OAuth clients
- Database and service identities
- TLS and mTLS certificates
- Ledger integrity signing keys
- Object and domain encryption keys
- Webhook signing keys
- Government filing certificates
- Customer-managed key references
- Rotation, revocation, expiry, and compromise response

```text
SecretReference
  owner and tenant scope
  purpose
  allowed workloads
  environment and region
  version
  rotation policy
  expiry
  key custody provider
  access evidence
```

Runtime secret resolution uses short-lived identities and least privilege. Secret values never enter ledger events, traces, agent prompts, configuration exports, generated code, or customer support bundles.

Open-source secret management is preferred where operationally credible, with external KMS/HSM support for high-assurance signing and customer-managed keys. The architecture must support provider replacement, sealed backup, dual control, emergency rotation, and cryptographic erasure.

#### Foundation 7: Configuration Promotion and Tenant Sandbox

Workflows, policies, AgentDefinitions, Rule Packs, schemas, connectors, mappings, capability configuration, feature flags, pricing, and localized content share one promotion lifecycle:

```text
author in development
  -> validate and compile
  -> deploy to tenant sandbox
  -> run reference and customer fixtures
  -> impact and cost simulation
  -> review and approval
  -> staged production rollout
  -> verify and observe
  -> promote, pause, quarantine, or rollback
```

An `EnvironmentBundle` identifies exact versions and dependencies. Promotion is a signed manifest operation, not a copy-and-paste process.

Tenant sandboxes require:

- Synthetic or policy-approved masked data
- No accidental production side effects
- Stub and certified test connectors
- Frozen time, legal calendars, and exchange rates
- Workflow and agent determinism controls
- Production-scale replay samples where safe
- Expected ledger, decision, usage, filing, and repair evidence
- Expiry and destruction of copied data

The sandbox is the common proving ground for schema upgrades, connector versions, agent changes, Rule Packs, migrations, reference workflows, and emergency procedures.

#### Foundation 8: Connector Framework

Connectors are governed packages, not arbitrary integration code.

The focused definition, connection, operation-journal, mapping, synchronization, webhook, capacity, maturity, and Phase 1 contracts are maintained in the [Integration Platform specification](integration-platform.md).

```text
ConnectorPackage
  connector_id and version
  supported systems and API versions
  capabilities
  schemas and mappings
  credential requirements
  rate and quota behavior
  idempotency and retry guarantees
  polling, webhook, and streaming modes
  authority and reconciliation support
  residency and data classifications
  health, certification, and deprecation status
  test harness and fixtures
```

The SDK supplies:

- Typed request, response, observation, and error envelopes
- Credential references and rotation hooks
- Idempotency, retry, backoff, circuit breaking, and rate control
- Pagination, batching, checkpointing, and resumable import/export
- Mapping versions and source-field provenance
- Webhook validation and replay protection
- Reconciliation probes and external-state fingerprints
- Local fake server, contract tests, and certification suite
- Health, SLO, usage, cost, and incident telemetry

Connector marketplace publication requires security review, license review, capability scope, compatibility evidence, operational ownership, and signed artifacts. Third-party connector code does not execute with unrestricted tenant or platform authority.

#### Foundation 9: Data Quality and Invariant Correctness

Data quality and correctness are related but distinct:

```text
Data quality
  completeness, format, freshness, consistency, plausibility, duplication

Business invariants
  states that must never or must always be true
```

Checks are versioned and scoped:

```text
QualityRule
InvariantRule
  rule_id and version
  domain and jurisdiction
  severity
  evaluation mode
  required sources and freshness
  affected entities
  remediation policy
```

They run at ingestion, proposal, projection, execution, post-execution, reconciliation, and continuous sampling. A failure may warn, block, quarantine, open a case, create a RepairPlan, or escalate an incident.

Examples:

```text
active worker has an active employment
position occupancy does not exceed capacity
compensation has valid currency and effective range
terminated privileged worker has no active privileged access
payroll worker belongs to a valid pay group
no illegal timecard overlap
filled position references the successful candidate and worker
projection and search provenance do not exceed their ledger source
```

The engine preserves evaluation inputs, rule versions, false-positive overrides, repair outcomes, and trend metrics. Customer-authored rules require sandbox and performance safeguards.

#### Foundation 10: Provenance Graph

Every material value, decision, analysis, report, filing, projection, invoice line, and external observation should be traceable through a common provenance model.

```text
Source fact or artifact
  -> transformation / calculation / model
  -> observation or recommendation
  -> decision
  -> workflow and transaction
  -> ledger event
  -> projection or external effect
  -> reconciliation
  -> later outcome
```

Core edges include:

```text
derived_from
observed_from
calculated_by
authorized_by
required_by
decided_by
caused
superseded_by
corrected_by
projected_to
submitted_as
acknowledged_by
reconciled_with
evaluated_against
```

The provenance graph is a logical contract, not necessarily a graph database. Edges may be projected from ledger, decision, activity, schema, agent, billing, filing, and integration identifiers into purpose-built read models.

It must answer:

- Where did this value come from?
- Which system and authority owned it?
- Which schema, mapping, policy, rule, model, and reducer versions transformed it?
- What did the decision maker actually see?
- Which transaction and external effects followed?
- Was the result corrected, superseded, reconciled, or used in a later outcome?
- Which values and decisions are affected by a defective source or version?

#### Cross-Cutting Contract: Master and Reference Data

Shared enterprise concepts require lifecycle governance even when the first implementation is a small PostgreSQL catalog:

```text
Canonical Reference
  create -> validate -> publish -> effective-date -> alias/map
       -> deprecate -> retire -> merge/split -> preserve lineage

External mappings:
  Workday ENG4 <-> HCM job://engineering/software/L4
  Payroll 88142 <-> same canonical job
  Finance CC-ENG-04 <-> governed cost-center relationship
```

`ReferenceDefinition` records identity, type, owner scope, version, effective interval, lifecycle state, localization keys, aliases, hierarchy, classifications, external mappings, source authority, and replacement lineage. Jobs, skills, locations, legal entities, cost centers, currencies, countries, pay bands, classifications, and code sets use this contract. Phase 1 implements only the reference types required by Promotion + Compensation and the first connector.

#### Cross-Cutting Contract: Data Onboarding and Migration

Customer implementation requires a replayable pipeline rather than direct production inserts:

```text
extract -> immutable staging -> profile -> validate -> normalize
        -> resolve identity -> map reference data -> dry run
        -> approved import -> reconcile -> cutover -> verify
```

Every imported record retains source system, source object and identifier, extract/import batch, original value hash or protected payload reference, mapping version, transformation lineage, validation result, target canonical identifier, and correction history. The initial product needs a bounded CSV/API onboarding path for the pilot domain; high-volume historical migration, SFTP orchestration, and suite-wide cutover remain later implementation.

#### Four Distinct Correctness Questions

```text
DATA VALIDATION
  Does an incoming value conform to its type and declared rule?

DATA QUALITY
  Is the value complete, plausible, current, non-duplicated, and usable?

INVARIANT
  Does the combined system state obey a rule that must always hold?

RECONCILIATION
  Do two representations or an intended and observed outcome agree?
```

One Go rules package may execute several of these initially, but their result types, timing, authority, severity, remediation, and evidence remain distinct.

#### Shared Human Work, Document Execution, and Communications Contracts

The focused message-intent, audience, endpoint, template, inbox, conversation, delivery, provider-routing, bulk, subscription, evidence, and workflow-signal contracts are maintained in the [Messaging and Notification Plane specification](messaging-and-notification-plane.md).

The reusable task/queue/assignment, form/questionnaire, and deterministic expression/decision-table contracts are maintained in [Human Work, Forms, and Business Rules](human-work-forms-and-rules.md). Overall backend responsibility status is tracked in the [Platform Capability Coverage Matrix](platform-capability-coverage-matrix.md).

Workflow domains reuse three minimal contracts instead of inventing task and delivery behavior independently:

```text
WorkItem
  queue -> assign/claim -> delegate -> escalate -> complete + evidence

DocumentExecution
  render -> consent -> signer authentication -> sign/countersign
         -> timestamp -> evidence package -> verify

Delivery
  channel + audience + locale + priority + guarantee
  queue -> deliver -> bounce/fail -> retry/escalate -> acknowledge
```

Phase 1 implements approval/review work items, semantic notification intent, deterministic templates, transactional email, and a secure inbox required by the first workflow. General case management, provider-neutral e-signature ceremonies, omnichannel communications, statutory-delivery guarantees, inbound conversations, and bulk messaging are later shared-platform work.

#### Remaining Gap Portfolios

The other sixty capabilities are grouped into portfolios so they reuse the ten foundations rather than becoming isolated systems.

| Portfolio                          | Included gaps                                                                                                                                                                                           |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Worker and organization foundation | Master/reference data; position/headcount; organization modeling; relationship graph; credential/certification; acquisition/divestiture                                                                 |
| Transaction correctness            | Reservations; approval binding; execution revalidation; side-effect orchestration; saga/compensation; delegation/proxy; policy impact simulation                                                        |
| Data security and resilience       | Cryptographic attestation; migration/upgrades; backup/DR; multi-region/residency; records management; audit/evidence export; fraud and insider risk; business continuity; support diagnostics           |
| Integration and human work         | Bulk import/export; documents and e-signature; notifications; human work queues; case management; consent/preferences; webhooks/event subscriptions; developer SDKs; API lifecycle; tenant provisioning |
| Workforce domain engines           | Benefits eligibility; payroll calculation; scheduling optimization; timekeeping and clock integrity                                                                                                     |
| Delivery and customer governance   | Feature flags; experimentation; customer configuration authority; accessibility; mobile/offline operations                                                                                              |
| Agent, knowledge, and semantics    | Agent evaluation; prompt/model/agent registry; memory governance; RAG; ontology/taxonomy; metric registry; outcome tracking; organizational knowledge; process mining; business simulation/digital twin |
| Operations and platform economics  | Reconciliation; Repair Operations Center; incident management; SLO management; rate/quota/abuse controls; search/index governance; cost/capacity management                                             |

The system inventory remains explicit even when one implementation serves several gaps. For example, the task engine may support approval work, HR cases, filing review, and repair assignments while those domains retain separate capability contracts and access policies.

#### Gap Dependency Sequence

The recommended sequence is:

```text
Wave A — Define truth and boundaries
  schema/data contracts
  master/reference data
  classification
  source authority
  provenance identifiers

Wave B — Protect transaction correctness
  identity resolution
  conflict detection
  reservations
  proposal/approval binding
  execution revalidation

Wave C — Control distributed effects
  connector SDK
  side-effect dependency model
  compensation/saga semantics
  reconciliation and repair
  secrets and keys

Wave D — Make change safe
  environment bundles
  tenant sandbox
  configuration promotion
  schema/data migration
  feature rollout

Wave E — Prove ongoing correctness
  quality and invariants
  provenance graph
  evidence export
  incident and SLO operations
  backup, restore, and regional continuity
```

These waves overlap through one thin vertical slice. Promotion + Compensation remains the first executable reference, but it must progressively adopt each foundation rather than waiting for ten horizontal platforms to be completed in isolation.

#### Go-Only Target Architecture

The intended language and protocol topology is:

```text
Browser
  |
  v
GoWebComponents application
Go + WebAssembly, SSR/hydration where appropriate
  |
  v
grpcbridge edge
HTTP / JSON | gRPC-Web | WebSocket | SSE
  |
  v
Protobuf capability contracts
  |
  v
Go control plane and domain services
  +-- workflow and transaction runtime
  +-- AuthZ / policy adapters
  +-- regulatory and financial engines
  +-- integration workers
  +-- projectors and reconcilers
  +-- agent orchestration
  `-- operational tools
  |
  v
PostgreSQL ledger / projections / outbox
object, search, analytics, and event planes when justified
```

Go-only rules:

- All product, UI, service, workflow, integration, agent, operations, and
  developer-tooling code is authored in Go
- Shared domain types and deterministic logic live in Go packages, not duplicated across UI and services
- Protobuf defines service semantics; HTTP is an adapter, not a second implementation
- Generated clients are wrapped by stable domain-facing packages
- Background jobs and projectors use the same capability, identity, telemetry, and error contracts as request paths
- Avoid microservices until isolation, scale, residency, deployment, or ownership creates a clear boundary
- Prefer a modular Go monolith plus separately scalable workers during early phases
- Use the Go standard library where it is clear and sufficient

#### GoWebComponents Role

GWC/GoWebComponents is the console and product UI framework because it provides a Go + WebAssembly component model, typed HTML and CSS, routing, state, data fetching, SSR/hydration, accessibility, i18n, feature flags, PWA/offline support, and Go-native testing.

Adoption requirements:

- Begin with the Phase 1 vertical workspace and do not carry the legacy React runtime into the release
- Use server rendering, hydration, static islands, and virtualization deliberately to control WASM startup and bundle cost
- Define a shared design-token, accessibility, localization, form, error, loading, and field-redaction system
- Generate or wrap Protobuf clients behind capability-aware query and command hooks
- Preserve browser security boundaries; WASM does not receive secrets or bypass server AuthZ
- Test native Go logic, WASM behavior, browser accessibility, keyboard navigation, screen readers, performance, offline replay, and hydration
- Maintain a plain HTML or server-rendered recovery path for critical emergency workflows where practical

Performance gates include initial download, time to interactive, memory, large-table rendering, slow-device behavior, screen-reader correctness, and intermittent-connectivity recovery. Framework ownership does not waive product accessibility or performance requirements.

#### gRPC and grpcbridge Role

Protobuf and gRPC are the canonical service and generated-client contracts. `grpc-go` is the default implementation. `grpcbridge` is the preferred edge adapter for gRPC proxying, HTTP transcoding, gRPC-Web, WebSocket, and server-sent-event access where those modes are required.

The bridge is responsible for transport adaptation, routing, deadlines, cancellation, metadata propagation, and protocol-specific errors. It is not responsible for business authorization or domain semantics.

Before production authority, the gateway path must prove:

- Unary, client-streaming, server-streaming, and bidirectional behavior used by the product
- Cancellation, deadlines, backpressure, reconnect, and large-message limits
- AuthN context and tenant-safe metadata propagation
- Stable HTTP error and field-mask semantics
- CORS, CSRF, origin, WebSocket, and SSE security
- Rate limiting, quotas, request sizing, and abuse protection
- Reflection exposure policy and production routing configuration
- OpenTelemetry propagation and redaction
- Compatibility, load, fuzz, and failure-injection tests
- A pinned release, maintained fork, or replacement path if upstream maturity or response is insufficient

External REST APIs may remain intentionally curated rather than exposing every internal RPC through automatic transcoding.

#### SchemaFlux Role

SchemaFlux is the core compiler for structured, repository-owned platform definitions that benefit from parse, normalize, enrich, relate, validate, and emit phases.

Target inputs include:

```text
capability manifests
data-domain and classification catalogs
event and projection metadata
semantic ontology and metric definitions
workflow block manifests
connector manifests
Rule Pack metadata and coverage manifests
agent and tool catalogs
reference workflow fixtures
```

Target outputs include:

```text
Go registries and validators
documentation and searchable catalogs
OpenAPI-adjacent descriptions
policy and test fixtures
compatibility reports
dependency and provenance graphs
agent discovery indexes
evaluation datasets
```

Adoption begins with one bounded proof: compile the Capability Registry and classification catalog from a shared source into Go code, documentation, and conformance fixtures. The gate is successful round-trip reproducibility, deterministic output, meaningful diagnostics, custom-backend maintainability, incremental build performance, and clean interaction with Protobuf and CI.

SchemaFlux does not replace Protobuf, database migrations, or runtime validation by declaration alone. It coordinates structured definitions and generated artifacts around those authoritative formats.

#### Open-Source and Low-Cost Reference Stack

The default evaluation set is intentionally portable:

| Concern                     | Preferred direction                                                                                  |
| --------------------------- | ---------------------------------------------------------------------------------------------------- |
| Language and runtime        | Go only, standard library, small focused Go packages                                                 |
| UI                          | GWC/GoWebComponents with Go/WASM and selective SSR/hydration; no React or TypeScript                 |
| Service contracts           | Protobuf, `grpc-go`, generated Go clients                                                            |
| Web protocol bridge         | `grpcbridge`, maintained as a core HCM Next library                                                  |
| Structured definition build | SchemaFlux core pipeline plus authoritative Protobuf and migration sources                           |
| Transactional data          | PostgreSQL with direct SQL, `pgx`, generated queries where valuable, transactional outbox            |
| Local and test data         | PostgreSQL-compatible containers; SQLite only for bounded tooling where semantics are not misleading |
| Authorization               | Open policy/relationship engine evaluated behind HCM Next's AuthZ contract                           |
| Policy expressions          | CEL or another sandboxed open expression language where domain code is unnecessary                   |
| Secrets                     | Open-source vault candidate plus cloud KMS/HSM adapters for stronger custody                         |
| Object storage              | S3-compatible API with open-source local/test implementation                                         |
| Event distribution          | PostgreSQL outbox first; NATS-, Kafka-, or Redpanda-class open event fabric only when justified      |
| Search and vectors          | OpenSearch-class open search plane when PostgreSQL search no longer satisfies evidence               |
| Analytics                   | ClickHouse-class open analytical plane when OLTP isolation or scale requires it                      |
| Observability               | OpenTelemetry with Prometheus/Grafana-class metrics and open log/trace backends                      |
| Local development           | Go tools only, containers, deterministic fakes, generated fixtures, and one-command startup          |

Specific packages remain replaceable behind contracts. Selection considers:

```text
license and governance
maintenance and security response
protocol and data portability
operational burden
upgrade and migration path
performance under reference workloads
tenant and regional isolation
ecosystem interoperability
total cost at expected scale
exit cost
```

Self-hosting is not automatically cheaper. HCM Next should pay for managed PostgreSQL, key custody, object durability, or other infrastructure when the operational risk avoided is worth more than the premium and no strategic data or protocol lock-in is created.

#### Extraction From the Historical TypeScript Foundation

The historical Node/TypeScript API, workflow runtime, and React console remain useful only for behavioral discovery, fixtures, and contract extraction. They are not extended or deployed in the target platform.

Cutover follows a Go-only sequence:

```text
1. Freeze semantic behavior in Protobuf and reference fixtures.
2. Implement the Phase 1 capability, workflow, data, and connector path in Go.
3. Implement required web transports with grpcbridge.
4. Implement the workspace with GWC against generated Go contracts.
5. Compile selected structured catalogs with SchemaFlux.
6. Compare ledger events, projections, errors, AuthZ, and performance through
   fixtures, deterministic replay, and isolated test environments.
7. Move write authority only after conformance, recovery, and pilot gates pass.
8. Exclude Node, npm, TypeScript, React, and Vite from builds, release images,
   deployed processes, and production SBOMs.
```

The implementation is a bounded clean Go slice, not a platform-wide feature rewrite. No dual-write without reconciliation and no legacy compatibility service are permitted.

The binding language and library rules are maintained in [the Go-Only
Technology Constitution](go-only-technology-constitution.md).

#### Foundation Definition of Done

A foundation is not complete because a service exists. It is complete when:

- Its semantic contract and authority boundary are explicit
- It works through UI, gRPC, HTTP, workflow, agent, and operational paths as applicable
- Tenant, organization, field, purpose, legal, and classification controls are enforced
- Events, provenance, metrics, usage, errors, and repair behavior are defined
- Versioning, migration, configuration promotion, and rollback are tested
- Reference workflow scenarios pass
- Failure modes and emergency procedures are rehearsed
- Open-source dependencies have pinned versions, license records, security ownership, and replacement plans
- Cost, capacity, performance, SLO, RPO, and RTO evidence meets the phase gate

The gap-closure principle is:

> Build the smallest shared foundation that makes the next reference-workflow slice correct, then prove that foundation across a second domain before promoting it into a general platform service.

### 9.20 ASCII Architecture Atlas

This atlas gives the major HCM Next systems a shared visual language. The diagrams are explanatory architecture views, not substitutes for versioned schemas, capability manifests, workflow definitions, policies, or executable contracts.

Legend:

```text
-->  command, call, or governed transition
==>  authoritative fact or authority-bearing result
~~>  asynchronous event, observation, or derived update
-X>  denial, block, quarantine, or failed effect
[A]  authoritative state
[D]  derived and rebuildable state
[E]  external system or authority
[H]  human decision or task
```

#### Complete Platform Plane Map

```text
                         HUMANS / APPS / AGENTS
                                  |
                                  v
+----------------------------------------------------------------+
| EXPERIENCE                                                     |
| GoWebComponents | HTTP | gRPC-Web | mobile | accessibility     |
+-------------------------------+--------------------------------+
                                |
                                v
+----------------------------------------------------------------+
| CAPABILITY AND GOVERNANCE                                      |
| AuthN | Entitlement | AuthZ | Legal | Regulatory | DLP | Limits|
+-------------------------------+--------------------------------+
                                |
                                v
+----------------------------------------------------------------+
| CONTROL AND INTELLIGENCE                                       |
| Workflow | Simulation | Agent Runtime | Decisions | Billing    |
+-------------------------------+--------------------------------+
                                |
                                v
+----------------------------------------------------------------+
| HCM DOMAINS                                                    |
| People | Workforce | Talent | Rewards | Experience | Access    |
+-------------------------------+--------------------------------+
                                |
                                v
+----------------------------------------------------------------+
| EXECUTION AND INTEGRATION                                      |
| Go runtime | connectors | side effects | compensation | repair |
+-------------------------------+--------------------------------+
                                |
                                v
+----------------------------------------------------------------+
| DATA AND EVIDENCE                                              |
| Ledger[A] | Objects[A] | Projections[D] | Search[D] | OLAP[D]  |
+-------------------------------+--------------------------------+
                                |
                                v
+----------------------------------------------------------------+
| OPERATIONS                                                     |
| Telemetry | SLO | incidents | reconciliation | DR | support   |
+----------------------------------------------------------------+
```

#### Governed Transaction Spine

```text
Intent
  |
  v
Proposal v3 ==> Simulation ==> Approval bound to proposal v3
  |                |                         |
  |                +--> conflicts            +--> authority snapshot
  |                +--> legal obligations    +--> visible inputs
  |                +--> planned effects      +--> expiry/invalidators
  |                `--> estimated cost        `--> decision evidence
  |                                          |
  +-----------------------> REVALIDATE <------+
                                 |
                 +---------------+---------------+
                 |                               |
                PASS                            STALE
                 |                               |
                 v                               v
          TransactionPlan                 ReapprovalRequired
                 |
                 v
          deterministic execution
                 |
        +--------+---------+
        |                  |
        v                  v
   Ledger facts[A]     Outbox effects ~~> [E]
        |                                  |
        v                                  v
   Projections[D] <~~ observations <~~ reconciliation
```

#### Person, Worker, and Identity Graph

```text
                         PERSON
                            |
          +-----------------+------------------+
          |                 |                  |
          v                 v                  v
 Candidate role     Employment A         Employment B
 Applications       Company US           Company CO
                          |                    |
                  +-------+-------+    +-------+-------+
                  |               |    |               |
                  v               v    v               v
              Position      Workforce ID Payroll ID   Local IAM

Derived labels:
  Worker        = person with applicable current engagement/employment
  Former worker = person with historical employment and no qualifying current one

External identities:
ATS candidate ID --> Person <-- contractor ID
HRIS employee ID --> Employment/Person <-- identity-provider subject
```

#### Identity Resolution, Merge, and Separation

```text
Incoming identity claims
  name | email | phone | prior ID | documents
                   |
                   v
          normalize + protect
                   |
                   v
        candidate generation[D]
                   |
        +----------+-----------+
        |          |           |
        v          v           v
    exact match  possible    no candidate
        |        match           |
        |          |             v
        |          v         Create Person
        |     [H] review
        |       /     \
        |    MATCH   DO_NOT_MERGE
        |       |        |
        +-------+        +--> durable distinction evidence
                |
                v
        IdentityMergePlan
                |
         simulate conflicts
                |
                v
          approved merge ==> ledger
                |
                v
        error found later
                |
                v
      IdentitySeparationPlan ==> repaired references
```

#### Master and Reference Data Flow

```text
Vendor standards     Customer definitions     Regulatory packs
       |                      |                       |
       +----------+-----------+-----------+-----------+
                  |                       |
                  v                       v
           Reference Registry       Mapping Registry
                  |                       |
        jobs / skills / codes       external <-> canonical
        locations / currencies             |
        cost centers / classes              |
                  |                         |
                  +------------+------------+
                               v
                       Effective Reference
                               |
               +---------------+---------------+
               v               v               v
          validation       workflow rules   integrations

Changes: draft -> impact -> approve -> future effective -> supersede
```

#### Position and Headcount Engine

```text
Workforce Plan
     |
     v
Approved Headcount Budget
     |
     v
Position
  capacity: 1.0 FTE
  cost center: ENG
  effective range
     |
     +--> vacancy ------> requisition ------> candidate
     |
     +--> reservation --> pending hire/transfer
     |
     `--> occupancy ----> Employment assignment

capacity check:

occupied FTE + reserved FTE + proposed FTE <= position capacity
                         |
                 +-------+-------+
                 |               |
                YES             NO
                 |               |
                 v               v
               allow       BLOCKING_CONFLICT
```

#### Organization and Relationship Graph

```text
Tenant
  |
  v
Enterprise Group
  +-- Company A -- Legal Entity A -- Department -- Team
  +-- Company B -- Legal Entity B -- Department -- Team
  `-- Shared Services

Worker relationships:

Worker Jane --reports_to--------> Manager Alice
Worker Jane --dotted_line-------> Director Bob
Worker Jane --supported_by------> HRBP Carol
Requisition --owned_by----------> Recruiter Dan
Worker Jane --represented_by----> Union Rep Erin
Project X --led_by--------------> Worker Frank

Principal + relationship + org scope + action + fields + purpose
                              |
                              v
                         AuthZ Decision
```

#### Conflict Detection and Reservation

```text
Promotion A                    Transfer B
salary 130 -> 145              org US -> CO
effective Oct 1                effective Oct 1
      |                              |
      v                              v
 normalized write set          normalized write set
      |                              |
      +---------------+--------------+
                      v
                Conflict Index
                      |
       +--------------+------------------+
       |              |                  |
       v              v                  v
   same field      semantic parent     independent
   overlap         conflict            fields
       |              |                  |
       v              v                  v
   BLOCK/REBASE   SERIALIZE/          MERGEABLE
                  SUPERSEDE

Reservation:
proposal --> reserve(position, budget, range) --> expires/releases
                         |
                  execution consumes
```

#### Proposal, Approval, and Revalidation

```text
Proposal v4
  payload hash H4
  baseline worker seq 91
  policy fingerprint P8
  context fingerprint C3
          |
          v
Approval
  binds H4 + P8 + C3
  approver authority snapshot
  invalidation conditions
          |
          v
wait until effective time
          |
          v
execution revalidation
  worker seq still 91? -------- no --+
  policy still acceptable? ---- no --+--> invalidate approval
  approver still authorized? --- no --+    return to simulation
  reservation valid? ---------- no --+
  conflicts absent? ----------- no --+
          |
         yes
          |
          v
        execute
```

#### Side-Effect Dependency and Saga Flow

```text
                    TransactionPlan
                          |
                +---------+---------+
                |                   |
                v                   v
          Atomic Region A      Parallel Group B
          People -> Comp       IAM      Learning
                |               |          |
                v               v          v
           Payroll write       OK        FAIL
                |                          |
                v                          v
             observed                 Retry policy
                                           |
                            +--------------+-------------+
                            |                            |
                         succeeds                     exhausted
                            |                            |
                            v                            v
                       reconcile                  RepairPlan

If irreversible external effect already occurred:
  compensate when valid | roll forward | accept + document | escalate
```

#### Source Authority and Reconciliation

```text
                       Authority Policy
                    salary -> HCM Next
                    tax ID -> Payroll
                    title  -> Workday
                           |
        +------------------+------------------+
        |                  |                  |
        v                  v                  v
 HCM Next intent[A]   External actual[E]  Prior observation
        |                  |                  |
        +------------------+------------------+
                           v
                       Reconciler
                           |
       +-------------------+-------------------+
       |                   |                   |
       v                   v                   v
     MATCH           LEGITIMATE CHANGE       DRIFT
                           |                   |
                           v                   v
                   import/dispute       RepairPlan or
                   by authority         authority review
```

#### Schema Registry and SchemaFlux Pipeline

```text
Repository-owned definitions
  Protobuf | capability metadata | classifications | semantics
                         |
                         v
                  SchemaFlux passes
 parse -> normalize -> enrich -> relate -> validate -> emit
                         |
       +-----------------+------------------+----------------+
       |                 |                  |                |
       v                 v                  v                v
   Go registry      documentation      test fixtures   discovery index
       |                 |                  |                |
       +-----------------+------------------+----------------+
                         v
                compatibility gate
                         |
              +----------+----------+
              |                     |
            compatible          migration required
              |                     |
              v                     v
           publish            MigrationPlan

Protobuf remains authoritative for wire contracts.
```

#### Data Classification and DLP

```text
Data / document / query / agent output
                 |
                 v
       Classification Resolver
 schema label + content detection + lineage
                 |
                 v
             DLP Decision
                 |
  +------+-------+--------+---------+---------+
  |      |                |         |         |
  v      v                v         v         v
ALLOW  REDACT          TOKENIZE  AGGREGATE   DENY
  |      |                |         |         |
  |      +--> field mask  |      cohort N     |
  |                       |                   |
  +-----------------------+-------------------+
                          |
                          v
                  destination check
        region | processor | purpose | retention
```

#### Secrets, Keys, and Certificates

```text
Workload identity
      |
      v
Capability request ---> policy ---> SecretReference
                                      |
                           +----------+----------+
                           |                     |
                           v                     v
                    Open vault/KMS          External HSM/KMS
                           |                     |
                           +----------+----------+
                                      v
                             short-lived material
                                      |
                                      v
                              connector / signer

Rotation: new version -> dual-read window -> switch -> revoke old
Compromise: quarantine -> revoke -> rotate -> identify use -> repair

Never: secret -> ledger / prompt / trace / support bundle
```

#### Configuration Promotion and Tenant Sandbox

```text
Git / Authoring
      |
      v
EnvironmentBundle
schemas + workflows + policies + agents + rule packs + mappings
      |
      v
compile / lint / compatibility
      |
      v
Tenant Sandbox
masked data + frozen clock + fake connectors + reference fixtures
      |
      v
impact / cost / security / regulatory simulation
      |
      v
[H] approval
      |
      v
canary tenant/org --> staged cohort --> full activation
      |                    |
      v                    v
verify evidence        anomaly detected
                           |
                           v
                    pause / rollback / quarantine
```

#### Connector Framework and Marketplace

```text
Connector Package
  manifest + schemas + mappings + capabilities + tests + signature
                           |
                           v
                 Certification Pipeline
        security | replay | limits | failures | reconciliation
                           |
                 +---------+---------+
                 |                   |
                pass                fail
                 |                   |
                 v                   v
           Registry version       quarantine
                 |
                 v
         Isolated Connector Runtime
 credentials by reference | egress policy | rate control
                 |
      +----------+----------+
      |                     |
      v                     v
 requests to [E]      observations from [E]
      |                     |
      +----------+----------+
                 v
             reconcile
```

#### Import, Export, and Bulk Data

```text
Upload / feed / customer extract
               |
               v
      object storage[A]
               |
               v
 scan -> classify -> parse -> schema validate -> normalize
               |
               v
      dry-run + error report
               |
       +-------+-------+
       |               |
    approve          reject/fix
       |               |
       v               |
 chunked idempotent processing
       |
       v
 ledger facts ==> projections ~~> export package
                                   |
                              DLP + AuthZ
                                   |
                                   v
                              signed delivery
```

#### Documents and E-Signature

```text
Template + version + locale + jurisdiction
                    |
                    v
              Render Plan
             data + clauses
                    |
                    v
              PDF/artifact[A]
              hash + object ref
                    |
                    v
        signer routing and identity
                    |
         +----------+----------+
         |                     |
         v                     v
    employee signs        countersigner
         |                     |
         +----------+----------+
                    v
             evidence package
 timestamp + signatures + consent + rendered hash
                    |
                    v
                  ledger
```

#### Human Work, Cases, and Notifications

Detailed Communications Plane contracts live in [messaging-and-notification-plane.md](messaging-and-notification-plane.md); this atlas view preserves only the portfolio relationship.

```text
Workflow / obligation / incident
               |
               v
           Work Item
 owner | queue | SLA | required authority
               |
      +--------+---------+
      |                  |
      v                  v
 [H] task             Case
 approval/form       HR service / ER / legal
      |                  |
      +--------+---------+
               v
          state transition
               |
               v
        Notification Policy
 channel + locale + preference + urgency + privacy
               |
    email | SMS | push | chat | inbox
               |
               v
 delivery observation -> retry/escalate -> evidence
```

#### Consent and Preference Management

```text
Notice version[A] ---> presented to Person
                             |
                             v
                       Consent Decision
                       grant / decline
                             |
                  +----------+----------+
                  |                     |
                  v                     v
              active use            no processing
                  |
                  v
             withdrawal
                  |
                  v
      stop future use + downstream obligations

Communication preferences are separate:
purpose x channel x urgency x jurisdiction x accessibility need
```

#### Records, Retention, and Legal Hold

```text
Record / object / event payload
              |
              v
       Retention Resolver
type + jurisdiction + trigger + policy version
              |
              v
       destruction_due_at
              |
        +-----+------+
        |            |
   no legal hold   LegalHold[A]
        |            |
        v            v
 delete/anonymize   retain + restrict
 archive/shred          |
        |               v
        |          hold released
        +-------+-------+
                v
          destruction evidence
```

#### Credential and Certification Lifecycle

```text
Required by Job / Position / Regulation
                   |
                   v
           Credential Requirement
                   |
                   v
Worker submits evidence --> verify issuer/status
                   |
             +-----+------+
             |            |
           valid        rejected
             |            |
             v            v
      eligible to work   remediation task
             |
             v
        expiry monitor
       90d -> 30d -> due -> expired
             |
             v
 renewal workflow / schedule or access restriction
```

#### Benefits Eligibility and Enrollment

```text
Employment + life event + jurisdiction + plan rules
                         |
                         v
                  Eligibility Engine
                         |
            +------------+------------+
            |                         |
         eligible                  ineligible
            |                         |
            v                         v
     enrollment window          explanation
            |
            v
 worker elections + dependents + evidence
            |
            v
 coverage + payroll deductions + carrier writes
            |
            v
       cross-system reconciliation
```

#### Payroll Calculation and Correction

```text
Earnings + time + compensation + elections + benefits
                         |
                         v
                  Gross-to-Net Kernel
       gross -> taxability -> deductions -> tax -> net
                         |
          +--------------+--------------+
          |                             |
          v                             v
     employee pay                 employer cost
          |                             |
          +--------------+--------------+
                         v
                  Payroll Ledger[A]
                         |
             pay / file / report / reconcile

Retro correction:
known-as + effective-as -> expected historical result -> delta
-> approval -> corrective pay/tax/filing -> reconciliation
```

#### Scheduling, Timekeeping, and Clock Integrity

```text
Demand forecast + labor rules + skills + availability
                         |
                         v
                 Schedule Optimizer
                         |
                         v
                  Published Schedule
                         |
         +---------------+----------------+
         |                                |
         v                                v
   shift swap / leave                clock events
                                          |
                              device + location + attestation
                                          |
                                          v
                                  Timecard Projection
                                          |
                         exceptions / overlap / anomaly
                                          |
                                          v
                                   approval + payroll

Offline clock: signed local event -> sync -> dedupe -> verify -> reconcile
```

#### Delegation and Proxy Authority

```text
Delegator authority
       |
       v
Delegation Grant
scope + actions + resources + fields + purpose + start/end
       |
       v
Delegate Principal
       |
       v
effective authority = delegator current authority
                    INTERSECT delegation
                    INTERSECT delegate restrictions
                    INTERSECT current policy
       |
       v
allow / deny / step-up / approval

Revocation or delegator role loss invalidates future use immediately.
```

#### Agent, Knowledge, and RAG Governance

```text
Approved knowledge sources[A]
 policies | handbooks | contracts | cases | operational knowledge
                         |
            ingest -> classify -> chunk -> cite
                         |
                         v
                 Knowledge Index[D]
                         |
User intent -> Agent -> authorized retrieval
                         |
                         v
                 cited context set
                         |
                         v
             analyze / hypothesize / plan
                         |
                         v
        deterministic capability or proposal
                         |
                         v
              outcome + evaluation loop

Memory: interaction | task | user preference | case | organizational
Each has scope, purpose, retention, provenance, and revocation.
```

#### Agent Evaluation and Version Promotion

```text
AgentDefinition v8
prompt + model policy + tools + memory + autonomy
                    |
                    v
              Evaluation Suite
 grounding | tool use | AuthZ | legal | cost | latency | outcomes
                    |
        +-----------+-----------+
        |                       |
      pass                    regress
        |                       |
        v                       v
   shadow/canary             quarantine
        |
        v
 production v8 ~~> traces + decisions + outcomes
        |                       |
        +-----------+-----------+
                    v
             continuous evaluation
```

#### Semantic Ontology, Metrics, and Outcomes

```text
Ontology
Worker --occupies--> Position --belongs_to--> Org
Worker --has_skill-> Skill    --required_by-> Job
Decision --causes--> Transaction --produces-> Result
                                      |
                                      v
                                   Outcome

Metric Registry
definition + grain + temporal mode + dimensions + owner + version
                     |
                     v
              Semantic Query Plan
                     |
         AuthZ + Legal + cohort controls
                     |
                     v
                  Analytics

Correlation may be measured; causality is not silently asserted.
```

#### Billing, Usage, and Cost Lineage

```text
Contract --> Entitlement --> Capability execution
                                  |
                                  v
                            UsageEvent[A]
                                  |
                     +------------+------------+
                     |                         |
                     v                         v
              Rating Engine               Cost Engine
             price book version       provider + infra cost
                     |                         |
                     v                         v
              Billing Ledger[A]           COGS Ledger
                     |                         |
                     +------------+------------+
                                  v
                    invoice | showback | margin

Retries share one semantic usage idempotency key.
```

#### Regulatory Computation and Filing

```text
Worker action + JurisdictionContext
                   |
                   v
           Applicable Rule Packs
                   |
        +----------+-----------+
        |                      |
        v                      v
 deterministic engines   Obligation Resolver
 tax / wage / leave      notices / deadlines
        |                      |
        +----------+-----------+
                   v
            TransactionPlan
                   |
                   v
          calculation evidence[A]
                   |
                   v
         Government Report Definition
                   |
                   v
              FilingPackage[A]
                   |
             submit to [E]
                   |
                   v
          acknowledgement / correction
```

#### Physical Data Plane and Query Routing

```text
Command --> PostgreSQL transaction
              | ledger[A] + projection[D] + outbox
              |
              +~~> event fabric
                     +~~> search[D]
                     +~~> analytics[D]
                     +~~> semantic vectors[D]
                     +~~> cache invalidation[D]
                     `~~> object evidence[A]

Query Router
  current worker ------> cache / hot projection
  fuzzy person --------> secure search
  five-year trend -----> analytics
  causal timeline -----> timeline projection / ledger
  signed artifact -----> object plane

Every derived answer carries source watermark and version.
```

#### Data Quality, Invariants, Reconciliation, and Repair

```text
                    Authoritative inputs[A]
                             |
          +------------------+------------------+
          |                  |                  |
          v                  v                  v
     quality rules      invariants        replay expected state
          |                  |                  |
          +------------------+------------------+
                             v
                         compare
                             |
                  +----------+----------+
                  |                     |
                valid                 violation
                  |                     |
                  v                     v
               health              Incident
                                        |
                                        v
                              diagnose -> RepairPlan
                                        |
                              simulate -> approve
                                        |
                                        v
                                  execute repair
                                        |
                                        v
                                      verify
```

#### Incident, SLO, and Operations Center

```text
ledger events + reconciliation + invariants + telemetry
                         |
                         v
                     Detectors
                         |
                         v
              incident fingerprinting
                         |
          +--------------+--------------+
          |                             |
     new incident                  existing incident
          |                             |
          v                             v
    severity model                aggregate impact
business + technical + legal + security + workers
                         |
                         v
                 Operations Center
 health | incidents | repair | SLO | backlog | evidence
                         |
                         v
               owner / escalation / status
```

#### Multi-Region, Residency, Backup, and DR

```text
Tenant Residency Policy
          |
          v
   Region Router
      /       \
 Region A    Region B
 ledger[A]   standby / permitted services
 objects[A]  replicated under policy
      |          |
      +----+-----+
           v
 encrypted backups + signed manifests
           |
           v
  isolated restore environment
           |
       verify hashes
       replay projections
       reconcile objects
       test tenant access
           |
           v
  promote recovery or discard test

No cross-region processing without Legal + residency approval.
```

#### Business Continuity and Emergency Mode

```text
Critical dependency outage
 payroll | IdP | connector | region | key service
                    |
                    v
           Continuity Policy
                    |
        +-----------+-----------+
        |           |           |
        v           v           v
   read-only     queued work   emergency manual path
        |           |           |
        +-----------+-----------+
                    v
             obligations created
             limited authority
             expiry + review
                    |
                    v
           service restored
                    |
                    v
      replay -> reconcile -> repair -> close
```

#### Acquisition and Divestiture

```text
Company graph before                Company graph after
Parent A                            Parent A       Parent B
  `-- Company X       plan          (history)       `-- Company X
         |        ------------>                         |
         v                                               v
workers + policies + data + contracts + integrations + authority
                         |
                         v
             Separation / Migration Plan
 identity continuity | ownership | legal holds | residency
 access cutoff | historical visibility | billing | connectors
                         |
                         v
               staged move + reconciliation

History remains attributable to the company and authority that existed then.
```

#### Customer Support and Diagnostic Access

```text
Customer request / incident
          |
          v
Support Case + customer consent
          |
          v
time-bound SupportGrant
tenant + fields + actions + purpose + expiry
          |
          v
safe diagnostic view
redacted traces + provenance + configuration + health
          |
       no raw secrets
       no silent impersonation
          |
          v
action proposal -> customer approval -> governed capability
          |
          v
complete access and outcome evidence
```

#### Developer Platform and Event Subscriptions

```text
Capability Registry + Protobuf + event schemas
                       |
          +------------+-------------+
          |                          |
          v                          v
     generated SDKs              developer portal
 Go | HTTP | gRPC               docs | examples | limits
          |                          |
          +------------+-------------+
                       v
                 Customer App
                       |
                       v
             Webhook Subscription
 filter + scope + secret + endpoint + version
                       |
event -> sign -> deliver -> ack
          |         |
          |      retry / DLQ
          |         |
          +---- replay window

Deprecation: announce -> measure adoption -> migrate -> sunset.
```

#### Mobile and Offline Operations

```text
Online sync ==> signed local snapshot[D]
                     |
                connectivity lost
                     |
                     v
           bounded offline capabilities
        view schedule | clock | draft | approve?
                     |
                     v
             local append queue
idempotency + device time + monotonic order + attestation
                     |
              connectivity returns
                     |
                     v
     authenticate -> upload -> dedupe -> revalidate
                     |
             +-------+-------+
             |               |
           accept          conflict
             |               |
             v               v
          ledger          human/repair
```

#### Business Simulation and Digital Twin

```text
Current workforce snapshot[D]
              |
              v
       Scenario Branch
reorg | hiring | policy | compensation | schedule | workflow
              |
              v
     deterministic simulations
              +-- headcount and position
              +-- payroll and benefits
              +-- regulatory obligations
              +-- IAM and integration effects
              +-- cost and capacity
              +-- predicted outcomes / uncertainty
              |
              v
       Scenario Results[D]
              |
         compare A / B / C
              |
              v
      [H] approve selected plan
              |
              v
 generate governed workflows --X> never mutate production directly
```

#### Go-Only Legacy Extraction

```text
Historical TypeScript behavior
            |
            v
extract Protobuf contract + fixtures
            |
            v
clean Go capability + workflow implementation
            |
    GWC -> grpcbridge -> Go
            |
            v
offline fixture / ledger / error comparison
            |
     +------+------+
     |             |
   parity       mismatch
     |             |
     v             v
release gate     fix Go implementation
     |
     v
Go-only build and deployment
```

#### Gap-Closure Feedback Loop

```text
Reference workflow failure
          |
          v
missing contract or foundation behavior
          |
          v
smallest shared design
          |
          v
implement in one Go vertical slice
          |
          v
prove in original workflow
          |
          v
prove in second domain
          |
     +----+----+
     |         |
 reusable   too specific
     |         |
     v         v
platform    keep domain-owned
service     and documented
```

### 9.21 Production Infrastructure and Platform Correctness

The next architecture layer protects the platform that protects HCM state. It is not a list of 46 new microservices. It is eight cohesive control portfolios implemented initially as Go modules, sidecars or shared interceptors, policy bundles, CI controls, and operational workflows.

```text
                       HCM NEXT PRODUCTION

                     Global Tenant Directory
                               |
                   Placement + Residency Policy
                               |
          +--------------------+--------------------+
          |                    |                    |
          v                    v                    v
       CELL A               CELL B              CELL C
   tenants 1..n         tenants n..m       dedicated/regional
          |                    |                    |
          +--------------------+--------------------+
                               |
             +-----------------+-----------------+
             |                 |                 |
             v                 v                 v
        OVERLOAD          ZERO TRUST         AI SECURITY
       GOVERNANCE          + EGRESS          + CONTAINMENT
             |                 |                 |
             +-----------------+-----------------+
                               v
                    SIGNED SOFTWARE SUPPLY CHAIN
                               |
                               v
                     TRUSTED TIME + CRYPTO
                               |
               +---------------+---------------+
               v                               v
       GOVERNED TELEMETRY              ISOLATED RECOVERY
```

#### 9.21.1 Tenant Cells, Placement, and Relocation

A **cell** is the maximum intended production blast radius for tenant-serving state and execution. A cell owns an API/runtime slice and its queues, transactional shards, caches, projectors, search capacity, and tenant-scoped operational telemetry. Globally shared control services contain routing and metadata, not ordinary employee payloads.

```text
Request tenant_ABC
       |
       v
Global Edge -- authenticate tenant --> Placement Directory
                                          |
                               tenant_ABC -> us-east/cell-17
                                          |
                                          v
                                   Cell 17 Gateway
                                          |
                  +-----------------------+----------------------+
                  |              |              |               |
                  v              v              v               v
              Go Runtime     PostgreSQL      Queue/Bus      Search/Cache
                  |              |              |               |
                  +--------------+--------------+---------------+
                                          |
                                   cell-local failure
                                          X
                                does not cross into Cell 18
```

The first deployment may physically use one cell, but it must already expose a logical `CellPlacement` contract:

```text
CellPlacement

tenant_id
placement_version
cell_id
region
residency_class
isolation_tier       SHARED | ISOLATED_POOL | DEDICATED
sla_tier
capacity_profile
status               ACTIVE | DRAINING | MOVING | QUARANTINED
effective_at
```

The **Tenant Placement Controller** chooses only from eligible cells, using residency, tenant size, SLA, encryption/key requirements, product dependencies, current capacity, and isolation tier. Placement decisions are versioned and explained. Domain code never derives physical storage from `tenant_id` by itself; it resolves a short-lived signed placement lease.

Live relocation is a governed saga, not a DNS trick:

```text
Source Cell A                       Target Cell B
     |                                   |
     |-- compatibility preflight ------->|
     |-- base snapshot ----------------->|
     |== ordered change replication ====>|
     |                                   |-- shadow verify
     |<--------- checksum/watermark -----|
     |
 [brief write fence at safe point]
     |-- final delta ------------------->|
     |                                   |-- reconcile
     +-------- atomic placement switch --+
                                         |
                                 reads/writes on Cell B
                                         |
                              retain rollback window
```

Relocation preserves logical tenant, resource, event, and correlation identifiers. It requires dual-read or shadow verification, a bounded write fence, rollback criteria, complete audit evidence, and no weakening of residency or key policy. Shared-to-dedicated and region-to-region movement use the same protocol.

The **Tenant Resource Governor** meters CPU time, concurrent work, DB connections, queue backlog, integration slots, analytics units, search/vector work, storage, and model inference by tenant and criticality. It enforces fairness before shared infrastructure saturates.

```text
Incoming Work
     |
     v
tenant + cell + capability + criticality + estimated cost
     |
     v
Resource Governor ----> capacity / quota / fairness state
     |
     +-- ADMIT
     +-- ADMIT_DEGRADED
     +-- QUEUE
     +-- THROTTLE
     +-- REJECT_RETRYABLE
     +-- REJECT_FINAL
```

Tenant-aware capacity planning consumes the same usage dimensions as billing but serves a different purpose. It forecasts cell exhaustion and proposes expansion or relocation. A **Tenant Degradation Controller** can disable or reduce expensive features for one tenant while preserving authentication, payroll release, ledger writes, termination access revocation, and repair operations.

#### 9.21.2 Overload Survival Protocol

Every request and queued task carries a propagated `WorkloadContext`:

```text
WorkloadContext

tenant_id
cell_id
criticality       P0 | P1 | P2 | P3 | P4
deadline
estimated_cost
retry_attempt
retry_budget_id
idempotency_key
degradation_policy_id
```

Criticality is set at the semantic entry boundary and normally propagates through gRPC metadata, events, workflow steps, and connector calls:

```text
P0  payroll release / privileged IAM revoke / integrity repair
P1  material employee transaction / interactive approval
P2  ordinary reads / reports / notifications
P3  analytics backfill / semantic reindex / bulk import
P4  experiments / model shadowing / optional enrichment

capacity falls
     |
     v
shed P4 -> pause P3 -> degrade P2 -> protect P1 -> reserve P0
```

The admission, retry, and backpressure controls form one protocol:

```text
Caller
  |
  | request + deadline + criticality + retry budget
  v
Admission Control -- capacity exhausted --> typed overload response
  |                                      retry_after / do_not_retry
  v
Service / Queue / Connector
  |
  +-- healthy --------------------------> result
  |
  +-- pressure signal --> upstream throttle --> workflow safe-point pause
                                      |
                                      v
                              agent revises or defers plan
```

Only the layer immediately above a failing dependency retries it. Retry attempts consume a shared budget, use jittered backoff, preserve idempotency, respect deadlines, and stop on `DO_NOT_RETRY`. Agents cannot create independent retry loops around workflow or transport retries.

Every capability registers a degradation contract:

| Mode              | Meaning                                                         |
| ----------------- | --------------------------------------------------------------- |
| `FAIL_CLOSED`     | Refuse when authority, legal, integrity, or security is unknown |
| `FAIL_OPEN`       | Continue only for explicitly approved non-sensitive behavior    |
| `USE_STALE_DATA`  | Serve labeled bounded-age data                                  |
| `QUEUE_FOR_LATER` | Durably defer before side effects begin                         |
| `DISABLE_AI`      | Use deterministic capability or human path                      |
| `READ_ONLY`       | Preserve safe reads while blocking writes                       |
| `MANUAL_PROCESS`  | Create a governed human task with evidence                      |

`FAIL_OPEN` is forbidden by default for AuthZ, legal restrictions, tenant placement, ledger integrity, money movement, privileged access, egress, and final employment decisions.

The **Maintenance and Drain Coordinator** stops new work, advances workflows to declared safe points, drains leases and queues, verifies replicas/capacity, performs the change, and restores routing. It enforces disruption budgets at the HCM capability and cell level, not merely the pod level.

#### 9.21.3 Workload Zero Trust, JIT Access, and Egress

Every workload receives an attestable, short-lived identity. Network location is never identity.

```text
Payroll Service                     Tax Engine
      |                                  |
workload identity                  workload identity
      |                                  |
      +------ mutually authenticated ----+
                         |
                         v
               Service AuthZ Decision
        subject=payroll-service:v12
        action=tax.calculate
        tenant=ACME
        purpose=payroll_run
        contract=TaxCalculate:v4
                         |
                  allow + obligations
                         |
                         v
                   typed gRPC call
```

The preferred open-source starting point is SPIFFE-compatible identity, with SPIRE evaluated as an implementation. Go gRPC interceptors enforce workload identity, tenant binding, capability authorization, deadline/criticality propagation, and evidence capture. OPA/CEL or OpenFGA may provide policy components where they fit, but HCM Next retains one explainable service authorization decision contract.

East-west traffic is default-deny. Runtime network policy constrains service ingress and egress, while application AuthZ remains authoritative. A compromised Search Indexer may read its authorized projection feed; it cannot call `payroll.execute` merely because the network is reachable.

Privileged human access follows a JIT workflow:

```text
Engineer request
 reason + tenant/cell + capability + duration + incident
                         |
                         v
                policy + approval + step-up
                         |
                  short-lived grant
                         |
              recorded constrained session
                         |
             automatic expiry / emergency revoke
                         |
                  evidence + review
```

Support impersonation is a separate, consent-aware delegated principal. It does not expose customer secrets, cannot erase its own evidence, and is unavailable for prohibited sensitive domains unless a stronger approved procedure applies.

All outbound data crosses a **Data Egress Gateway**:

```text
API export | email | webhook | SFTP | agent/model | filing
                         |
                         v
                  Egress Intent
 destination + fields + purpose + residency + classification
                         |
          +--------------+--------------+
          v              v              v
        AuthZ           Legal           DLP
          +--------------+--------------+
                         v
              allow / redact / transform / deny
                         |
                  signed delivery evidence
```

Connectors and agents receive destination-scoped, expiring credentials or tokens, never broad reusable platform credentials.

#### 9.21.4 Secure Software Supply Chain

No production artifact is trusted by tag or repository location alone.

```text
Reviewed source + lockfiles
             |
             v
       isolated builder
             |
      +------+------+----------------+
      |             |                |
      v             v                v
 tests/ASVS     SBOM (SPDX/       build provenance
 security         CycloneDX)      source+builder+deps
      |             |                |
      +-------------+----------------+
                    v
          immutable artifact digest
                    |
              sign / attest
                    |
                    v
            Deployment Admission
       signature + provenance + policy + vulnerability
                    |
             +------+------+
             |             |
           deploy        reject
```

The low-cost open-source toolchain should evaluate:

- Syft for SBOM generation and CycloneDX or SPDX output
- Grype or Trivy for dependency and image vulnerability scanning
- Cosign/Sigstore for artifact signatures and attestations
- SLSA-compatible provenance emitted by isolated CI builders
- Go module checksums, pinned tools, reproducible-build checks, and license policy
- Admission verification through a small Go policy service or established admission policy tooling

The exact tools remain replaceable; the evidence contracts do not.

A dependency impact graph joins component -> artifact -> deployment -> cell -> tenant/workload exposure. A new vulnerability can therefore produce a prioritized affected set rather than a repository search. External vulnerability intake becomes a case workflow linked to the affected SBOM components, releases, mitigations, customer communications, and final verification.

#### 9.21.5 Continuous Security Assurance

Controls must continuously produce evidence:

```text
Threat Registry
 capability | trust boundary | asset | threat | control | owner
                         |
                         v
             Security Verification Pipeline
 SAST | dependency | fuzz | authz | cross-tenant | DLP | tamper | abuse
                         |
              +----------+----------+
              |                     |
            pass                  failure
              |                     |
              v                     v
       signed release evidence   block / incident
              |
              v
        Runtime Control Monitor
              |
     configuration drift? control silent? evidence stale?
              |
              v
       remediation workflow
```

The attack-surface registry covers capabilities, public and internal APIs, agents, tools, integrations, stores, queues, identities, build systems, and trust boundaries. OWASP ASVS-style verification criteria are mapped to executable tests. An adversarial harness continuously exercises cross-tenant object access, field inference, bulk export, privilege escalation, malicious files, stolen workload identity, ledger tampering, prompt injection, and tool abuse.

The **Continuous Control Evidence System** stores test identity, control version, scope, observation time, result, evidence hash, owner, and expiry. Absence or staleness of evidence is itself a failed control. Drift detection compares desired configuration, deployed configuration, runtime observation, and signed release provenance.

#### 9.21.6 Agent Security and Containment

Agent security is a mediation pipeline around the model, not a better system prompt:

```text
Human intent + retrieved content + documents
                         |
                         v
              Trust / Taint Classification
 FACT(HIGH) | POLICY(HIGH) | HUMAN(MEDIUM)
 EXTERNAL(UNTRUSTED) | MODEL(DERIVED)
                         |
                         v
               Prompt-Injection Firewall
        delimit content / strip active affordances /
        detect conflicts / constrain retrieval purpose
                         |
                         v
                       Model
                         |
                 proposed tool call
                         |
                         v
               Tool-Use Security Gateway
 schema -> taint -> AuthZ -> legal -> DLP -> risk -> budget
                         |
              +----------+----------+
              |                     |
            allow                  deny
              |                     |
              v                     v
      deterministic capability   evidence/incident
              |
              v
             typed result -> Output Validation Gateway
                               schema + business invariants
```

Untrusted content may supply data but never gains instruction authority. Taint labels survive retrieval, summarization, planning, and tool arguments. A tool call using tainted content in a sensitive argument either requires explicit validation/human confirmation or is denied. Canonical facts, policies, observations, and model inferences remain distinguishable in both prompts and outputs.

The **Model and Provider Eligibility Registry** is evaluated before cost routing:

```text
data class x jurisdiction x purpose x agent x risk x region
                              |
                              v
                     eligible model set
                              |
                   quality / latency / cost router
```

The **Agent Containment Plane** can independently disable one execution, agent definition, agent version, model, provider, tool, capability, tenant agent fleet, or every write-capable AI path. Deterministic HCM remains available.

```text
AI anomaly / operator / policy breach
                 |
                 v
            Kill Switch
                 |
    +------------+-------------+--------------+
    v            v             v              v
stop run    revoke tool   block provider   read-only AI
                 |
                 v
           AI Incident Case
 preserve trace -> assess exposure -> notify -> repair -> eval gate
```

AI incidents include prompt injection, unexpected agency, tool misuse, leakage, systematic bad recommendation, bias, provider behavior change, and model regression. Re-enablement requires an explicit remediation and evaluation gate.

#### 9.21.7 Cyber Recovery, Trusted Time, and Crypto Agility

Backups are not healthy until restored and verified as a functioning system. The recovery plane preserves ledger shards, object artifacts, tenant placement, schemas, workflows, policies, configuration bundles, key references, and required infrastructure definitions in immutable or isolation-protected repositories.

```text
Production Cells
      |
      +-- encrypted backup + signed manifest --> immutable repository
      |                                               |
      |                                               X no prod delete role
      |                                               |
      +-----------------------------------------------+
                                                      v
                                         Isolated Recovery Environment
                                                      |
                                restore -> integrity verify -> replay projections
                                                      |
                                AuthZ/legal/invariant/reference-workflow tests
                                                      |
                                               measured RPO/RTO
                                                      |
                                         recovery attestation / failure
```

The isolated recovery environment uses separate administrative authority and credentials. Restore validation runs regularly on representative full stacks, not only database dumps. Game days rehearse database loss, region loss, corrupt shard, signing-key compromise, malicious administration, payroll-day failure, identity-provider outage, and cloud-provider outage.

Time is a governed dependency:

```text
Trusted Time Sources
      |
 authenticated/monitored synchronization
      |
      v
Host / DB / signer / workflow worker
      |
      +-- skew within bound --> timestamp + time authority evidence
      |
      +-- skew exceeded -----> quarantine sensitive writes
                                alert + incident + resynchronize
```

Wall-clock time never substitutes for per-stream sequence or database concurrency control. Sensitive actions record the trusted-time source/epoch and observed skew class where necessary. Clock uncertainty is surfaced rather than silently manufacturing precision.

Crypto agility requires algorithm and key identifiers in every cryptographic envelope:

```text
CryptoProfile v7
 TLS | envelope encryption | ledger HMAC | epoch signature | document signature
              |
        inventory usage
              |
 new algorithm/profile
              |
 dual-read / dual-sign / rewrap / verify
              |
 retire old only after compatibility + recovery proof
```

Algorithms, key providers, signature formats, certificate profiles, and rotation schedules are policy data. Migration supports parallel verification, key rewrapping, signed evidence, rollback, and compromised-key response without rewriting the meaning of historical events.

#### 9.21.8 Governed Observability

Telemetry must not become a second uncontrolled HR database.

```text
App / gRPC / Workflow / Agent instrumentation
                       |
                       v
              Telemetry Privacy Gateway
 classify -> redact/hash/drop -> destination policy
                       |
               Cardinality Governor
 bounded dimensions; IDs in logs/traces, not metric labels
                       |
                Sampling Policy Engine
 100% security/money/failure | adaptive success sampling
                       |
                       v
              OpenTelemetry Collectors
                       |
           +-----------+-----------+
           v           v           v
         metrics      traces       logs
                       |
                 pipeline health
 queue/drop/export lag/config/version/coverage
```

Raw prompt, tool, document, medical, compensation, bank, and identity-secret content is off by default. Tenant, worker, workflow, and request identifiers are not unbounded metric labels; they belong in appropriately retained traces/logs or controlled exemplars. Sampling decisions consider risk and outcome so security denials, financial mutations, cross-tenant attempts, agent tool calls, and failures retain sufficient evidence.

The telemetry pipeline has its own SLOs. Dropped spans, exporter failures, queue saturation, redaction failures, configuration drift, and missing instrumentation produce incidents. Business evidence never depends exclusively on sampled telemetry.

#### 9.21.9 Consolidated System Inventory

| Portfolio                | Systems addressed                                                                                                           |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------- |
| Tenant infrastructure    | Cell architecture, placement controller, live relocation, noisy-neighbor governor, capacity planner, tenant degradation     |
| Overload survival        | Admission/load shedding, retry budgets, backpressure, priority scheduler, graceful-degradation registry, drain coordination |
| Zero trust               | Workload identity, service AuthZ, east-west segmentation, JIT/PAM, egress gateway                                           |
| Software supply chain    | SBOM, build provenance, artifact signing/admission, vulnerability impact graph, isolated builds, disclosure intake          |
| Security assurance       | Continuous evidence, threat registry, verification pipeline, adversarial harness, posture drift                             |
| Agent security           | Prompt-injection firewall, tool gateway, semantic taint, kill switch, AI incidents, provider eligibility, output validation |
| Recovery and trust       | Isolated recovery, immutable backup, restore validation, game days, trusted time, skew monitoring, crypto agility           |
| Observability governance | Cardinality limits, telemetry privacy, sampling policy, telemetry pipeline health                                           |

#### 9.21.10 Initial Go and Open-Source Shape

```text
cmd/
  hcm-api              hcm-worker            hcm-operator

internal/platform/
  placement            admission             workloadidentity
  serviceauthz         egress                degradation
  retrybudget          backpressure          aitoolgateway
  taint                containment           trustedtime
  cryptoprofile        evidence              recovery
  telemetrypolicy      supplychainmetadata

api/proto/
  platform/v1/placement.proto
  platform/v1/workload.proto
  platform/v1/egress.proto
  platform/v1/agent_security.proto
  platform/v1/recovery.proto

policy/
  authz/ legal/ egress/ degradation/ model-eligibility/

deploy/
  cells/ identity/ network-policy/ telemetry/ recovery/
```

Candidate dependencies must pass the open-source dependency charter. Likely starting points are SPIFFE/SPIRE for workload identity, Kubernetes NetworkPolicy and disruption controls where Kubernetes is used, OpenTelemetry Collector for telemetry routing, Sigstore/Cosign plus Syft and Grype/Trivy for supply-chain evidence, and Prometheus/Grafana/Loki/Tempo-class components for low-cost operations. These are replaceable implementations behind HCM Next contracts, not permanent semantic dependencies.

SchemaFlux may compile workload manifests, degradation policies, telemetry classifications, capability risk metadata, and provider-eligibility definitions when its IR is suitable. Protobuf remains authoritative for runtime contracts. grpcbridge exposes only intentionally public control APIs. GoWebComponents provides operations consoles for placement, capacity, JIT access, AI containment, recovery exercises, and control evidence.

#### 9.21.11 Non-Postponable Production Baseline

Before production authority expands beyond limited pilots, HCM Next must demonstrate:

1. Logical cell placement on every request and event, even if only one physical cell exists.
2. Tenant budgets and per-criticality admission/load shedding with bounded retries.
3. Distinct workload identities and authorized service-to-service calls on material paths.
4. Default-deny egress for agents, connectors, exports, and model providers.
5. SBOM, isolated build provenance, artifact signature, and deployment verification for every release.
6. Expiring, approved, recorded JIT production access.
7. Taint-aware agent input handling and a validating tool-use gateway.
8. Tested kill switches that leave deterministic HCM operational.
9. An isolated restore that passes ledger integrity, projection replay, and reference-workflow checks.
10. Monitored time synchronization, skew blocking for sensitive writes, and versioned crypto profiles.

The expansion sequence is:

```text
PHASE 1: contracts + single physical cell
  placement metadata | tenant quotas | workload IDs | signed builds
  JIT access | agent gateway | kill switch | restore test | time checks
                         |
                         v
PHASE 2: two-cell rehearsal
  relocation shadow copy | priority shedding | backpressure | retry budgets
  isolated recovery | control evidence | adversarial tests
                         |
                         v
PHASE 3: multi-cell production
  live relocation | tenant-specific degradation | capacity forecasting
  regional recovery | crypto migration rehearsal | continuous game days
```

Success is measured by isolation and recovery evidence, not component count: zero cross-tenant authorization success, bounded overload amplification, protected P0 completion, relocation reconciliation, signed-release coverage, JIT expiry, agent containment time, restore success rate, achieved RPO/RTO, clock-skew detection time, crypto-profile coverage, telemetry redaction coverage, and control-evidence freshness.

> The platform is production-correct only when one tenant, workload, build, agent, operator, dependency, region, clock, or telemetry pipeline can fail without silently corrupting another tenant's business truth or destroying the evidence needed to recover it.
